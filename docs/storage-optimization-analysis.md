# Gno Storage 최적화 분석 가이드

## 1. Storage 아키텍처 개요

Gno의 storage는 다층 구조로 구성되어 있다.

```
Gno 컨트랙트 코드 (realm)
       |
   Store Interface (gnolang/store.go)
       |
   defaultStore (in-memory 캐시)
       |
   +---+---+
   |       |
baseStore  iavlStore
(객체/타입) (escaped 해시, MemPackage)
   |       |
 cache.Store  iavl.Store
   |       |
 gas.Store  gas.Store (가스 미터링)
   |       |
 dbadapter   IAVL 머클 트리
   |
  RocksDB/LevelDB
```

### 핵심 컴포넌트

| 컴포넌트 | 위치 | 역할 |
|----------|------|------|
| `Store` 인터페이스 | `gnovm/pkg/gnolang/store.go:42-86` | VM과 persistence 브릿지 |
| `defaultStore` | `gnovm/pkg/gnolang/store.go:140-172` | 캐시 + 직렬화 + 가스 미터링 |
| `Realm` | `gnovm/pkg/gnolang/realm.go` | 트랜잭션별 dirty 추적/flush |
| `ObjectInfo` | `gnovm/pkg/gnolang/ownership.go:149-178` | 객체 메타데이터 (ID, hash, owner, refcount) |
| KV gas.Store | `tm2/pkg/store/gas/store.go` | 저수준 KV 작업 가스 미터링 |

---

## 2. Storage 동작 파이프라인

### 2.1 객체가 저장되는 전체 과정

```
1. 컨트랙트에서 변수 생성/수정
       |
2. Realm.DidUpdate() - dirty 마킹
       |
3. OpReturn / FinalizeRealmTransaction()
   |-- processNewCreatedMarks(): ObjectID 할당
   |-- markDirtyAncestors(): 소유자 체인 dirty 전파
   |-- saveUnsavedObjects():
   |   |-- copyValueWithRefs(): 자식 객체 -> RefValue 변환
   |   |-- amino.MustMarshalAny(): 바이너리 직렬화
   |   |-- HashBytes(): SHA256 해시 (20바이트)
   |   |-- baseStore.Set(key, hash+bytes): 실제 저장
   |   '-- 가스 소비: GasSetObject * len(bytes)
   |-- removeDeletedObjects(): 삭제 처리
   '-- clearMarks(): 상태 초기화
       |
4. Storage deposit 정산 (100 ugnot/byte)
```

### 2.2 저장 키 형식

```
객체:          "oid:{PkgID_hex}:{NewTime}"  -> [hash(20b) + amino_bytes]
Realm 메타:    "oid:{PkgID_hex}:{NewTime}#realm" -> amino(Realm)
타입:          "tid:{TypeID}"               -> amino(Type)
BlockNode:     "node:{Location}"            -> amino(BlockNode)
MemPackage:    "pkg:{path}"                 -> amino(MemPackage) (iavlStore)
```

### 2.3 직렬화 변환 (copyValueWithRefs)

위치: `gnovm/pkg/gnolang/realm.go:1324-1482`

```
메모리 상태:
  ArrayValue{ List: [StructObj1, StructObj2, StructObj3] }

직렬화 후:
  ArrayValue{ List: [RefValue{oid1}, RefValue{oid2}, RefValue{oid3}] }
```

- **Primitive 값** (string, int, bigint): 인라인으로 복사
- **Object** (Array, Struct, Map, Func, Block 등): `RefValue`로 변환되어 별도 저장
- 각 Object는 독립적인 key-value 쌍으로 저장됨

---

## 3. 가스 비용 구조 (이중 과금)

### 3.1 VM 레벨 가스 (gnolang Store)

위치: `gnovm/pkg/gnolang/store.go:126-138`

| 작업 | 가스 비용 | 단위 |
|------|----------|------|
| GetObject | 16 | per byte |
| SetObject | 16 | per byte |
| GetType | 5 | per byte |
| SetType | 52 | per byte |
| GetPackageRealm | 524 | per byte |
| SetPackageRealm | 524 | per byte |
| AddMemPackage | 8 | per byte |
| GetMemPackage | 8 | per byte |
| DeleteObject | 3,715 | flat |

**공식**: `gas = cost_per_byte * len(amino_serialized_bytes)`

### 3.2 KV Store 레벨 가스 (tm2 Store)

위치: `tm2/pkg/store/types/gas.go:229-238`

| 작업 | 가스 비용 | 단위 |
|------|----------|------|
| Has | 1,000 | flat |
| Delete | 1,000 | flat |
| Read (Get) | 1,000 + 3/byte | flat + per byte |
| Write (Set) | 2,000 + 30/byte | flat + per byte |
| Iterator.Next | 30 + 3/byte | flat + per byte |

### 3.3 총 비용 계산 예시

100바이트 객체를 저장(SetObject)할 때:

```
VM 레벨:    16 * 100 = 1,600 gas
KV 레벨:    2,000 + (30 * 120) = 5,600 gas  (hash 20b + amino 100b = 120b)
---------------------------------------
총 가스:    ~7,200 gas (단일 객체 저장)
```

### 3.4 Storage Deposit (별도 비용)

위치: `docs/resources/storage-deposit.md`, `gno.land/pkg/sdk/vm/params.go`

```
100 ugnot per byte (= 1 GNOT per 10KB)
```

- 저장 시 GNOT 잠금, 삭제 시 GNOT 환불
- Realm 단위로 추적 (사용자 단위가 아님)

---

## 4. Storage 비용이 비싼 근본 원인

### 4.1 Dirty Ancestor 전파

위치: `gnovm/pkg/gnolang/realm.go` - `markDirtyAncestors()`

하나의 필드를 수정하면 **소유자 체인 전체**가 dirty로 마킹되어 재직렬화된다.

```
PackageValue (root owner)
  └── Block (package block)
      └── StructValue (변수 A)
          └── ArrayValue (필드 X) ← 이것만 수정해도
              └── StructValue (요소 1)

결과: 요소1 + ArrayValue + StructValue + Block + PackageValue 전부 재직렬화
```

이는 소유자 체인의 hash가 자식의 hash에 의존하기 때문이다 (머클 트리 특성).

### 4.2 Amino 직렬화 오버헤드

- 모든 객체에 타입 정보가 포함됨 (`amino.MustMarshalAny`)
- `RefValue`가 ObjectID 문자열을 포함하여 비교적 큼
- ObjectID 형식: `"{32byte_hex}:{uint64}"` → 약 70+ bytes per reference

### 4.3 Object 단위 저장의 비효율성

각 Object가 독립적인 KV 쌍이므로:
- Object마다 KV Write flat cost (2,000 gas) 발생
- Object마다 독립적인 hash 계산
- 작은 struct가 많으면 오버헤드 비율이 매우 높음

### 4.4 이중 가스 과금

VM 레벨 가스(SetObject per-byte)와 KV Store 레벨 가스(Write flat + per-byte)가 **모두** 과금됨.

---

## 5. Storage 최적화 전략

### 5.1 컨트랙트 레벨 최적화 (개발자가 할 수 있는 것)

#### A. 값 타입 슬라이스로 Object 수 최소화

포인터(`*Struct`)를 사용하면 각 항목이 독립 Object가 되어 저장 시 KV Write flat cost(2,000 gas)가 매번 발생한다. 값 타입(`Struct`)으로 슬라이스에 저장하면 전체가 하나의 Object로 인라인 직렬화된다.

**실제 코드 패턴** (출처: `examples/gno.land/p/archive/groups/vote_set.gno`):

```go
// GOOD: 값 타입 슬라이스 — 전체가 단일 Object로 저장됨
type Vote struct {
    Voter std.Address
    Value string
}

type VoteList []Vote

func (vlist *VoteList) SetVote(voter std.Address, value string) {
    for i, vote := range *vlist {
        if vote.Voter == voter {
            (*vlist)[i] = Vote{Voter: voter, Value: value}
            return
        }
    }
    *vlist = append(*vlist, Vote{Voter: voter, Value: value})
}
```

```go
// BAD: 포인터 슬라이스 — 각 Vote가 별도 Object
var votes []*Vote  // N개의 Object + 1개의 slice Object
```

**또 다른 패턴** — 관련 데이터를 단일 구조체에 팩킹 (출처: `examples/gno.land/r/sys/validators/v2/`):

```go
// GOOD: 블록별 변경 이력을 값 타입 슬라이스로 묶어 AVL tree에 저장
type change struct {
    blockNum  int64
    validator validators.Validator
}

func saveChange(ch change) {
    id := getBlockID(ch.blockNum)
    setRaw, exists := changes.Get(id)
    if !exists {
        changes.Set(id, []change{ch})  // 값 타입 슬라이스
        return
    }
    set := setRaw.([]change)
    set = append(set, ch)
    changes.Set(id, set)  // 같은 블록의 변경을 하나의 entry로 팩킹
}
```

**비트 팩킹으로 플래그 압축** (출처: `examples/gno.land/p/morgan/chess/engine.gno`):

```go
// GOOD: 여러 boolean 플래그를 1바이트에 압축
type PositionFlags byte  // 캐슬링 권리 + 앙파상 등을 비트로 관리

// BAD: 각 플래그를 별도 bool 필드로 저장
type PositionFlags struct {
    WhiteKingSide  bool
    WhiteQueenSide bool
    BlackKingSide  bool
    BlackQueenSide bool
    EnPassant      bool
}
```

**트레이드오프**: 값 타입 슬라이스가 매우 크면(수백 개 이상), 하나의 원소만 수정해도 전체 슬라이스가 재직렬화된다. 이 경우 `avl.Tree`를 사용하는 것이 더 효율적이다 (섹션 F 참조).

#### B. 키/값 직렬화 크기 최소화

Amino 직렬화에서 문자열은 그대로 저장되므로, 문자열 길이가 곧 바이트 비용이다.
Gno에서 영속 저장은 `avl.Tree`를 통해 이루어지며, 키는 반드시 `string` 타입이다.
(`map`은 트랜잭션 간 영속되지 않는다.)

`seqid` 패키지를 사용하면 숫자 ID를 정렬 가능한 짧은 문자열로 변환할 수 있다.

**실제 코드 패턴** (출처: `examples/gno.land/p/nt/seqid/v0/seqid.gno`):

```go
import "gno.land/p/nt/seqid/v0"

var (
    idCounter seqid.ID
    items     avl.Tree  // seqid key -> *Item
)

func AddItem(data string) {
    id := idCounter.Next()  // overflow 체크 포함
    // Binary(): 고정 8바이트, 가장 컴팩트, 정렬 유지
    items.Set(id.Binary(), &Item{Data: data})
}
```

**키 형식별 크기 비교**:

| 형식 | 예시 | 크기 |
|------|------|------|
| `seqid.Binary()` | 8바이트 바이너리 | **8 bytes** (가장 컴팩트) |
| `seqid.String()` | cford32 인코딩 | 7-13 bytes |
| `padZero(id, 10)` | `"0000000042"` | 10 bytes |
| 풀 경로 문자열 | `"gno.land/r/very/long/path"` | 25+ bytes |

**주의**: `map` 타입은 Gno에서 트랜잭션 간 영속되지 않는다. 영속 저장이 필요하면 반드시 `avl.Tree`를 사용해야 한다.

#### C. 저장 vs 계산 트레이드오프

"계산 가능한 값은 저장하지 않는다"는 조언은 **항상 옳지 않다**. 읽기 빈도와 계산 비용에 따라 판단해야 한다.

**저장이 맞는 경우 — 자주 읽히는 핫패스 집계값**

(출처: `examples/gno.land/p/demo/tokens/grc20/types.gno`):

```go
type PrivateLedger struct {
    totalSupply int64   // 저장된 집계값
    balances    avl.Tree
    allowances  avl.Tree
}

// Mint/Burn 시 O(1)로 업데이트
func (led *PrivateLedger) mint(address std.Address, amount int64) {
    led.totalSupply += amount
    // ...
}

// TotalSupply 조회도 O(1) — RenderHome 등에서 빈번히 호출됨
func (led *PrivateLedger) totalSupply() int64 {
    return led.totalSupply
}
```

만약 `totalSupply`를 저장하지 않으면, 조회할 때마다 모든 balance를 순회해야 한다 (O(n)).
GRC20 토큰에서 `TotalSupply()`는 매우 자주 호출되므로 저장하는 것이 효율적이다.

**계산이 맞는 경우 — 드물게 읽히거나 계산이 저렴한 값**

(출처: `examples/gno.land/p/demo/tokens/grc20/token.gno`):

```go
// KnownAccounts는 TotalSupply보다 호출 빈도가 낮으므로 계산으로 처리
func (tok Token) KnownAccounts() int {
    return tok.ledger.balances.Size()  // AVL tree Size()는 O(log n)
}
```

**판단 기준**:

| 조건 | 저장 | 계산 |
|------|------|------|
| 읽기 빈도 높음 (RenderHome 등) | O | X |
| 계산 비용 O(n) 이상 | O | X |
| 읽기 빈도 낮음 | X | O |
| 계산 비용 O(1) 또는 O(log n) | X | O |
| 쓰기 시 항상 같이 변경됨 | O | X |

**이중 추적 패턴** (출처: `examples/gno.land/p/moul/ulist/ulist.gno`):

```go
type List struct {
    root       *treeNode
    totalSize  int  // 전체 원소 수 (삭제 포함) — 인덱스 범위 검증에 사용
    activeSize int  // 활성 원소 수 — 삭제된 노드를 세지 않기 위해 저장
}

// 삭제 시 activeSize만 감소
func (l *List) Delete(indices ...int) error {
    // ...
    node.data = nil
    l.activeSize--
}
```

`activeSize`를 저장하지 않으면 매번 전체 노드를 순회하며 `nil`이 아닌 것만 세야 한다 (O(n)).

#### D. 변경 범위 최소화

독립적으로 변경되는 데이터를 별도 변수로 분리하면, 수정 시 dirty 전파 범위가 줄어든다.
**단, 함께 업데이트되어야 하는 관련 데이터를 분리하면 일관성 버그가 발생할 수 있다.**

**안전한 분리 — 진정으로 독립적인 데이터**

```go
// GOOD: config는 관리자만 변경, counters는 매 트랜잭션마다 변경
// 두 데이터 사이에 일관성 제약이 없으므로 분리 가능
var config   Config      // 관리자 설정 (드물게 변경)
var counters [100]uint64 // 사용자 카운터 (빈번하게 변경)
```

**위험한 분리 — 함께 업데이트되어야 하는 데이터**

(출처: `examples/gno.land/r/morgan/chess/chess.gno`에서 관찰된 패턴):

```go
// RISKY: gameStore와 user2Games는 항상 함께 업데이트되어야 한다
var (
    gameStore     avl.Tree  // gameID -> *Game
    gameIDCounter seqid.ID
    user2Games    avl.Tree  // address -> []*Game
)

func newGame(opponent std.Address) {
    id := gameIDCounter.Next()       // 1) ID 할당
    gameStore.Set(id.String(), game) // 2) 게임 저장
    addToUser2Games(caller, game)    // 3) 호출자 인덱스 업데이트
    addToUser2Games(opponent, game)  // 4) 상대방 인덱스 업데이트
    // 만약 3)은 성공하고 4)에서 panic이 발생하면?
    // → caller는 게임을 볼 수 있지만 opponent는 볼 수 없는 불일치 상태
}
```

**안전한 대안 — 관련 데이터를 구조체로 묶기**:

```go
// GOOD: 관련 인덱스를 하나의 구조체에 묶어 일관성 보장
type GameRegistry struct {
    games      avl.Tree  // gameID -> *Game
    idCounter  seqid.ID
    byUser     avl.Tree  // address -> []*Game
}

var registry GameRegistry

func (r *GameRegistry) NewGame(caller, opponent std.Address, game *Game) {
    id := r.idCounter.Next()
    r.games.Set(id.String(), game)
    r.addToUserGames(caller, game)
    r.addToUserGames(opponent, game)
    // 모든 상태가 하나의 Object 소유권 아래에 있으므로
    // 트랜잭션 실패 시 전체가 롤백됨
}
```

**분리 판단 기준**:

| 조건 | 분리 가능 | 분리 위험 |
|------|----------|----------|
| 서로 다른 시점에 독립적으로 변경됨 | O | - |
| 하나만 변경해도 다른 쪽은 영향 없음 | O | - |
| 항상 함께 업데이트되어야 함 | - | O |
| 하나는 다른 하나의 인덱스/요약임 | - | O |
| 일관성 제약이 존재함 (예: count == len(items)) | - | O |

#### E. AVL 트리(avl.Tree) 활용

Gno에서 `avl.Tree`는 트리 노드 단위로 저장되므로, 대량의 데이터를 다룰 때 전체 재직렬화를 피할 수 있다. 접근 시 검색 경로의 노드만 로드되므로 (O(log n)), 대규모 컬렉션에 효율적이다.

**Map vs AVL Tree 비교** (1,000개 항목 기준):

| 특성 | `map` | `avl.Tree` |
|------|-------|-----------|
| 영속성 | 트랜잭션 간 유지 안 됨 | 영속 저장됨 |
| 1개 값 접근 시 로드 | 전체 1,000개 | ~10개 노드 (log₂1000) |
| 수정 시 재직렬화 | 전체 map | 경로상 ~10개 노드 |
| 키 타입 | 임의 타입 | `string`만 가능 |
| 정렬/범위 조회 | 불가 | 키 순서로 정렬, 범위 조회 가능 |

**실제 사용 패턴** (출처: `examples/gno.land/r/gnoland/blog/gnoblog.gno`):

```go
// 여러 인덱스 트리로 같은 데이터에 다양한 방식으로 접근
type Blog struct {
    Title             string
    Prefix            string
    Posts             avl.Tree  // slug -> *Post
    PostsPublished    avl.Tree  // publish-date -> *Post (시간순 조회)
    PostsAlphabetical avl.Tree  // title -> *Post (알파벳순 조회)
}
```

**페이지네이션 지원** (출처: `examples/gno.land/p/nt/avl/v0/`):

```go
import "gno.land/p/nt/pager"

// 읽기 전용 트리를 래핑하여 안전한 페이지네이션 제공
pub := rotree.Wrap(&items, nil)
pg := pager.NewPager(pub, 20, false)
page := pg.GetPage(pageNumber)
```

**크기별 적합한 저장 방식**:

| 항목 수 | 권장 방식 | 이유 |
|---------|----------|------|
| ~10개 이하 | `[]Struct` (값 타입 슬라이스) | AVL 노드 오버헤드가 더 큼 |
| 10~100개 | 상황에 따라 판단 | 수정 빈도가 높으면 AVL, 낮으면 슬라이스 |
| 100개 이상 | `avl.Tree` | 접근/수정 시 O(log n)만 로드/재직렬화 |

**주의**: AVL tree의 값은 `any` 타입이므로, 헬퍼 함수로 타입 단언을 감싸는 것이 안전하다.

```go
func getItem(key string) *Item {
    v, exists := items.Get(key)
    if !exists {
        return nil
    }
    return v.(*Item)
}
```

### 5.2 프로토콜 레벨 최적화 (코어 개발)

#### A. ObjectID 직렬화 압축

현재 ObjectID는 hex 문자열로 직렬화됨 (`"{32byte_hex}:{uint64}"` → ~70+ bytes).
바이너리 인코딩으로 전환하면 **~40 bytes**로 줄일 수 있다.

#### B. Dirty Ancestor 재직렬화 최적화

현재: 자식 수정 → 모든 ancestor 재직렬화
개선 가능: hash 업데이트만 수행하고 전체 재직렬화 생략 (hash-only propagation)

#### C. Amino 대체/개선

Amino는 타입 정보를 매번 포함하므로 오버헤드가 크다. 스키마 기반 인코딩(protobuf 등)으로 전환하거나, 타입 프리픽스를 더 짧게 만드는 것을 고려할 수 있다.

#### D. 배치 Write 최적화

여러 Object를 동시에 저장할 때 KV Write flat cost를 한 번만 부과하는 배치 모드를 고려할 수 있다.

---

## 6. Storage 비용 측정 방법

### 6.1 가스 시뮬레이션 (`-simulate only`)

```bash
gnokey maketx call \
  -pkgpath gno.land/r/your/realm \
  -func YourFunction \
  -args "arg1" \
  -gas-wanted 10000000 \
  -gas-fee 1000000ugnot \
  -remote https://rpc.gno.land:443 \
  -broadcast \
  -simulate only \
  YOUR_KEY
```

출력에서 `GAS USED`를 확인한다. 동일 함수를 storage 사용량이 다른 여러 버전으로 작성하여 비교할 수 있다.

### 6.2 로컬 `gnodev`로 테스트

```bash
gnodev ./your-realm
```

로컬 환경에서 빠르게 반복 테스트할 수 있다.

### 6.3 Store Operation 로그 (`opslog`)

위치: `gnovm/pkg/gnolang/store.go:163`

`defaultStore.opslog`에 writer를 설정하면 모든 store 작업이 기록된다:
- `c[oid](diff)=...` — 새 객체 생성 (created)
- `u[oid](diff)=...` — 객체 업데이트 (updated, diff 포함)

이 로그를 활성화하면 어떤 객체가 몇 바이트로 저장/수정되는지 정확히 볼 수 있다.

### 6.4 Realm Storage 조회

```bash
gnokey query vm/qstorage -data "gno.land/r/your/realm" -remote https://rpc.gno.land:443
```

현재 realm이 사용 중인 storage를 확인할 수 있다.

### 6.5 벤치마크 프레임워크 (`benchops`)

위치: `gnovm/pkg/benchops/`

코드에 `bm.OpsEnabled`/`bm.StorageEnabled` 플래그가 있어, 빌드 시 활성화하면 각 store 작업의 시간과 크기를 측정할 수 있다.

```go
// store.go:620-630
if bm.OpsEnabled {
    bm.PauseOpCode()
    defer bm.ResumeOpCode()
}
if bm.StorageEnabled {
    bm.StartStore(bm.StoreSetObject)
    defer func() { bm.StopStore(size) }()
}
```

### 6.6 직접 직렬화 크기 확인

테스트 코드에서 `amino.MustMarshalAny()`를 직접 호출하여 객체의 직렬화 크기를 확인할 수 있다:

```go
bz := amino.MustMarshalAny(yourObject)
fmt.Printf("serialized size: %d bytes\n", len(bz))
fmt.Printf("estimated gas (SetObject): %d\n", 16 * len(bz))
fmt.Printf("estimated gas (KV Write): %d\n", 2000 + 30 * (len(bz) + 20))
fmt.Printf("storage deposit: %d ugnot\n", 100 * (len(bz) + 20))
```

---

## 7. 비용 비교 요약표

| 항목 | 읽기 (Get) | 쓰기 (Set) | 비율 |
|------|-----------|-----------|------|
| VM 가스 (per byte) | 16 | 16 | 1:1 |
| KV flat 가스 | 1,000 | 2,000 | 1:2 |
| KV per-byte 가스 | 3 | 30 | 1:10 |
| Storage deposit | 없음 | 100 ugnot/byte | - |

**핵심 인사이트**:
- KV per-byte 쓰기 비용이 읽기의 **10배**
- 작은 객체일수록 flat cost(2,000 gas) 비율이 높아 비효율적
- Storage deposit은 영구적으로 잠기므로 가장 비싼 비용

---

## 8. 최적화 체크리스트

**Object 수 줄이기**:
- [ ] 포인터 슬라이스(`[]*Struct`) → 값 타입 슬라이스(`[]Struct`)로 변경 가능한지 검토
- [ ] 비트 팩킹으로 압축할 수 있는 boolean/enum 필드가 있는지 확인

**저장 vs 계산 판단**:
- [ ] 핫패스에서 자주 읽히는 집계값(count, total)은 저장하고 있는지 확인
- [ ] 드물게 읽히거나 O(log n)으로 계산 가능한 값은 계산으로 전환 가능한지 확인

**상태 구조**:
- [ ] 함께 업데이트되어야 하는 데이터가 분리되어 일관성 위험이 있지 않은지 확인
- [ ] 독립적으로 변경되는 데이터가 불필요하게 같은 구조체에 묶여있지 않은지 확인
- [ ] 100개 이상의 항목을 다루는 컬렉션에 `avl.Tree`를 사용하고 있는지 확인
- [ ] `avl.Tree` 키에 `seqid.Binary()` 등 컴팩트한 형식을 사용하고 있는지 확인
- [ ] 영속 저장에 `map` 대신 `avl.Tree`를 사용하고 있는지 확인

**측정**:
- [ ] `-simulate only`로 가스 사용량을 측정하여 최적화 전후 비교
- [ ] `vm/qstorage`로 실제 storage 사용량 모니터링

---

## 9. 참고 파일 목록

| 파일 | 핵심 내용 |
|------|----------|
| `gnovm/pkg/gnolang/store.go` | Store 인터페이스, GasConfig, SetObject/GetObject |
| `gnovm/pkg/gnolang/realm.go` | Realm 트랜잭션, dirty 추적, copyValueWithRefs |
| `gnovm/pkg/gnolang/ownership.go` | ObjectID, ObjectInfo, Object 인터페이스 |
| `tm2/pkg/store/types/gas.go` | KV Store 가스 설정 (DefaultGasConfig) |
| `tm2/pkg/store/gas/store.go` | 가스 미터링 래퍼 |
| `tm2/pkg/store/iavl/store.go` | IAVL 머클 트리 저장소 |
| `gno.land/pkg/sdk/vm/params.go` | Storage deposit 가격 (100 ugnot/byte) |
| `docs/resources/storage-deposit.md` | Storage deposit 설명 |
| `docs/resources/gas-fees.md` | 가스 비용 설명 |
