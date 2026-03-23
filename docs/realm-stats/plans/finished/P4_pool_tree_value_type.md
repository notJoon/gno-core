# P4-2: Pool `*avl.Tree` × 3 + `*ObservationState` → value type 전환

**Priority:** 2
**예상 절감:** 모든 Pool write에서 ~400 bytes 추가 절감
**변경 범위:** `pool/pool.gno` 1파일 + `pool/v1/oracle.gno` nil 체크 수정
**패턴:** UintTree `*avl.Tree` → `avl.Tree` 전환과 동일 (`UINT_TREE_MIGRATION.md` 참조)
**의존:** P4-1과 독립. 동시 적용 가능.

---

## 사전 완료 사항

> **P4-5 (Observation map value type) 적용 완료.**
> `ObservationState.observations`가 `map[uint16]*Observation` → `map[uint16]Observation`으로 전환됨.
> 본 문서는 **`*ObservationState` 포인터 자체**를 value type으로 전환하는 작업을 다룬다.

---

## 문제 정의

Pool struct에 4개의 포인터 필드가 남아있다:

```go
type Pool struct {
    // ... (P4-1에서 다루는 sub-struct 필드들)
    ticks            *avl.Tree          // ← 포인터 (1 Object)
    tickBitmaps      *avl.Tree          // ← 포인터 (1 Object)
    positions        *avl.Tree          // ← 포인터 (1 Object)
    observationState *ObservationState  // ← 포인터 (1 Object)
}
```

각 포인터는 별도 Object(HeapItemValue)로 직렬화된다. Pool이 dirty marking될 때마다 이 4개 Object에 대한 참조가 함께 직렬화된다.

### avl.Tree value type의 안전성

`avl.Tree`는 다음과 같이 정의된다:

```go
type Tree struct {
    node *Node
    size int
}
```

- 내부 `node *Node` 포인터를 통해 실제 tree 데이터에 접근
- 모든 mutation 메서드가 pointer receiver (`*Tree`) 사용
- `self.ticks.Set(...)` 호출 시 Go가 `(&self.ticks).Set(...)` 로 자동 변환
- **value copy 시에도 node 포인터 공유** — 의도적으로 하나의 Pool만 tree를 소유하므로 문제 없음

이 안전성은 `UINT_TREE_MIGRATION.md`에서 이미 검증되었다.

### ObservationState value type의 특이사항

```go
// 현재 상태 (P4-5 적용 후)
type ObservationState struct {
    observations    map[uint16]Observation  // ✅ 이미 value type으로 전환 완료
    index           uint16
    cardinality     uint16
    cardinalityNext uint16
}
```

- `map`은 Go에서 reference type → ObservationState를 value type으로 변환해도 observations map 자체는 포인터 시맨틱스 유지
- **nil 체크 패턴 변경 필요:** 현재 `p.ObservationState() == nil` 체크가 2곳 존재

---

## 사용 패턴 분석

### ticks, tickBitmaps, positions (`*avl.Tree`)

**공통 패턴: getter로 포인터를 받아 메서드 호출**

```go
// 읽기 — tree 메서드 호출 (Get, Has, Iterate, Size)
p.ticks.Get(tickKey)          // pool.gno:162
p.ticks.Has(tickKey)          // pool.gno:156
p.ticks.Iterate(...)          // pool.gno:189
p.positions.Get(key)          // pool.gno (via getter)
p.tickBitmaps.Get(wordPosStr) // v1/tick_bitmap.gno:96

// 쓰기 — tree 메서드 호출 (Set, Remove)
p.ticks.Set(tickKey, tickInfo)   // pool.gno:177
p.ticks.Remove(tickKey)          // pool.gno:182
p.positions.Set(posKey, info)    // v1/position.gno:197
p.tickBitmaps.Set(wordPos, bm)  // v1/tick_bitmap.gno:109
```

**nil 체크:** 없음. 항상 `NewPool()`에서 `avl.NewTree()`로 초기화.

**re-assignment (SetTicks 등):** `Pool.SetTicks(ticks *avl.Tree)` 존재하나, 코드에서 사용 빈도 낮음 (주로 Clone 시).

### observationState (`*ObservationState`)

**nil 체크 패턴 (2곳):**

```go
// v1/oracle.gno:23
if p.ObservationState() == nil { ... }

// v1/oracle.gno:73-74
if p.ObservationState() == nil {
    p.SetObservationState(pl.NewObservationState(currentTime))
}
```

**메서드 호출:**
```go
os := p.ObservationState()
os.Index()                    // getter
os.Cardinality()              // getter
os.SetCardinality(...)        // setter
os.Observations()[index] = ob // map 직접 접근 (Observation value type으로 저장)
```

---

## 전환 계획

### Step 1: avl.Tree × 3 → value type

```go
// BEFORE
type Pool struct {
    ticks       *avl.Tree
    tickBitmaps *avl.Tree
    positions   *avl.Tree
}
func (p *Pool) Ticks() *avl.Tree           { return p.ticks }
func (p *Pool) SetTicks(t *avl.Tree)       { p.ticks = t }
func (p *Pool) TickBitmaps() *avl.Tree     { return p.tickBitmaps }
func (p *Pool) SetTickBitmaps(t *avl.Tree) { p.tickBitmaps = t }
func (p *Pool) Positions() *avl.Tree       { return p.positions }
func (p *Pool) SetPositions(t *avl.Tree)   { p.positions = t }

// AFTER
type Pool struct {
    ticks       avl.Tree   // value type
    tickBitmaps avl.Tree   // value type
    positions   avl.Tree   // value type
}
func (p *Pool) Ticks() *avl.Tree           { return &p.ticks }
func (p *Pool) SetTicks(t *avl.Tree)       { p.ticks = *t }
func (p *Pool) TickBitmaps() *avl.Tree     { return &p.tickBitmaps }
func (p *Pool) SetTickBitmaps(t *avl.Tree) { p.tickBitmaps = *t }
func (p *Pool) Positions() *avl.Tree       { return &p.positions }
func (p *Pool) SetPositions(t *avl.Tree)   { p.positions = *t }
```

**내부 직접 접근 수정:**

`pool.gno`에서 `p.ticks.Get(...)` 등으로 직접 접근하는 코드는 `p.ticks`가 value type이어도 pointer receiver 메서드가 자동 호출되므로 **변경 불필요**.

```go
// pool.gno:162 — 변경 없음
iTickInfo, ok := p.ticks.Get(tickKey)  // (&p.ticks).Get(tickKey) 자동 변환
```

**NewPool() 수정:**
```go
// BEFORE
pool := &Pool{
    ticks:       avl.NewTree(),   // *avl.Tree
    tickBitmaps: avl.NewTree(),   // *avl.Tree
    positions:   avl.NewTree(),   // *avl.Tree
}

// AFTER
pool := &Pool{
    ticks:       *avl.NewTree(),  // avl.Tree (dereference)
    tickBitmaps: *avl.NewTree(),  // avl.Tree
    positions:   *avl.NewTree(),  // avl.Tree
}
// 또는 더 간결하게:
pool := &Pool{
    ticks:       avl.Tree{},
    tickBitmaps: avl.Tree{},
    positions:   avl.Tree{},
}
```

**Clone() 수정:**
```go
// BEFORE (pool.gno:219-221)
ticks:       avl.NewTree(),       // *avl.Tree
tickBitmaps: avl.NewTree(),       // *avl.Tree
positions:   avl.NewTree(),       // *avl.Tree

// AFTER
ticks:       avl.Tree{},          // 빈 value type
tickBitmaps: avl.Tree{},
positions:   avl.Tree{},
```

### Step 2: ObservationState → value type

```go
// BEFORE
type Pool struct {
    observationState *ObservationState
}
func (p *Pool) ObservationState() *ObservationState { return p.observationState }
func (p *Pool) SetObservationState(os *ObservationState) { p.observationState = os }

// AFTER
type Pool struct {
    observationState ObservationState  // value type
}
func (p *Pool) ObservationState() *ObservationState { return &p.observationState }
func (p *Pool) SetObservationState(os *ObservationState) {
    if os == nil {
        p.observationState = ObservationState{}
        return
    }
    p.observationState = *os
}
```

**nil 체크 대체:**

ObservationState가 value type이 되면 `p.ObservationState() == nil` 비교가 불가능하다. 초기화 여부를 판단할 새로운 방법이 필요하다.

**방법: `observations` map nil 체크**

```go
// BEFORE (v1/oracle.gno:23)
if p.ObservationState() == nil { ... }

// AFTER — map이 nil이면 미초기화
if p.ObservationState().Observations() == nil { ... }
```

또는 명시적 메서드 추가:
```go
func (os *ObservationState) IsInitialized() bool {
    return os.observations != nil
}
```

**writeObservationByPool 수정:**
```go
// BEFORE (v1/oracle.gno:73-74)
if p.ObservationState() == nil {
    p.SetObservationState(pl.NewObservationState(currentTime))
}

// AFTER
if p.ObservationState().Observations() == nil {
    p.SetObservationState(pl.NewObservationState(currentTime))
}
```

**NewPool(), Clone() 수정:**
```go
// BEFORE
observationState: NewObservationState(time.Now().Unix()),  // *ObservationState

// AFTER
observationState: *NewObservationState(time.Now().Unix()),  // ObservationState (dereference)
```

---

## 위험 요소

### avl.Tree value copy 시 node 공유

`avl.Tree`를 value copy하면 두 Tree가 동일 node를 공유한다. 그러나:
- Pool은 항상 하나의 소유자(store)에서만 접근
- Clone()은 빈 tree를 생성 (node 공유 없음)
- `SetTicks(t *avl.Tree)` 호출 시 `p.ticks = *t`로 node 포인터가 복사되나, 원본 tree는 더 이상 사용되지 않음

**결론: 안전.**

### ObservationState nil 체크 패턴 변경

2곳의 nil 체크를 `Observations() == nil` 로 변경해야 한다. v1 코드 변경이 필요하지만 범위가 작다.

---

## 예상 영향

| Operation | P4-1 후 (B) | P4-2 후 (B) | 추가 절감 |
|-----------|------------|------------|----------|
| ExactInSwapRoute (1st) | ~4,500 | ~4,100 | ~400 |
| Mint #1 | ~31,500 | ~31,100 | ~400 |
| CreatePool | ~17,850 | ~17,450 | ~400 |

> P4-1과 P4-2를 함께 적용하면 Pool write당 총 ~900 bytes 절감.
> Swap (1st) 기준: 5,021 → ~4,100 = **-18%**

---

## 변경 파일

| 파일 | 변경 내용 |
|------|----------|
| `pool/pool.gno` | 4필드 타입 변경 + getter/setter + NewPool() + Clone() |
| `pool/v1/oracle.gno` | nil 체크 2곳 → `Observations() == nil` 변경 |

> `pool/oracle.gno`는 P4-5 (Observation map value type) 적용 시 이미 수정 완료.
> `ObservationState`의 구조 자체는 변경 불필요 — Pool 필드의 포인터만 value type으로 전환.

---

## 측정 방법

P4-1과 동일:
```bash
go test ./gno.land/pkg/integration/ -run TestTxtar/pool_create_pool_and_mint -v
go test ./gno.land/pkg/integration/ -run TestTxtar/pool_swap_wugnot_gns_tokens -v
```
