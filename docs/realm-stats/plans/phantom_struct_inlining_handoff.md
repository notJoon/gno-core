# Phantom Struct Inlining — 디버깅 인수인계 문서

**Branch:** `feat/phantom-array-inlining`
**작업 기반:** Phase 1 (Array Inlining) 완료 상태 위에 Phase 2 (Struct Inlining) 구현 중
**구현 계획서:** `docs/realm-stats/plans/phantom_struct_inlining_impl_plan.md`

---

## 현재 구현 상태

### 적용된 변경 (9곳)

계획서의 9개 변경 지점이 모두 적용됨. 추가로 1개 버그 수정 적용.

| # | 파일 | 위치 | 변경 내용 |
|---|------|------|----------|
| 1 | `realm.go` | `shouldInlineArray` 위 | `shouldInlineObject` 디스패처 + `shouldInlineStruct` 추가 |
| 2 | `realm.go` | `copyArrayInline` 위 | `copyStructInline` + `setInlineChildOwners` 추가 |
| 3 | `realm.go:1131` | `getSelfOrChildObjects` | `shouldInlineArray(av)` → `shouldInlineObject(obj)` |
| 4 | `realm.go:2031` | `refOrCopyValue` | StructValue inline 분기 추가 (type switch) |
| 5 | `ownership.go:382` | `GetFirstObject` | inline struct에 nil 반환 |
| 6 | `realm.go:212` | `DidUpdate` | `*ArrayValue` 타입 단언 → `shouldInlineObject(po)` |
| 7 | `values.go:233` | `Assign2` | inline struct owner 설정 + `setInlineChildOwners` |
| 8 | `store.go:576` | `restoreInlineOwnersTV` | inline struct에 owner 설정 + defensive recurse 유지 |
| 9 | `store.go` | 전체 | `restoreInlineArrayOwners` → `restoreInlineOwners` rename |
| 추가 | `realm.go:397` | `FinalizeRealmTransaction` | `processNewCreatedMarks` 직후 `restoreInlineOwners(co)` 호출 |
| 추가 | `crossrealm.gno` | storage 기댓값 | `crossrealm_b: 8` → `3` |

### 추가 버그 수정: `restoreInlineOwners` 호출 시점

**문제:** 최초 패키지 저장(`saveNewPackageValuesAndTypes`) 시 HeapItemValue 내부의 inline struct에 owner가 설정되지 않음. init() 실행 중 `struct.field = value` 같은 필드 수준 수정에서 DidUpdate → owner walk-up → owner nil → MarkDirty 실패.

**원인:** `restoreInlineOwners`는 `loadObjectSafe`에서만 호출되어 store에서 로드된 객체만 처리. 새로 생성된 객체(init 전 첫 저장)는 owner가 설정되지 않음.

**수정:** `FinalizeRealmTransaction`에서 `processNewCreatedMarks` 직후 모든 created 객체에 `restoreInlineOwners` 호출:

```go
rlm.processNewCreatedMarks(store, 0)
for _, co := range rlm.created {
    restoreInlineOwners(co)
}
```

이로써 `zpersist_valids.gno` 테스트 통과 (struct persistence across init/main boundary).

---

## 남은 테스트 실패 분류

### 카테고리 A — Pre-existing (Phase 2와 무관)

Phase 2 비활성화 (`shouldInlineStruct → return false`) 상태에서도 실패하는 테스트:

```
addressable_1b_err.gno      — 에러 메시지 문구 차이
addressable_1d_err.gno      — 에러 메시지 문구 차이
invalid_labels0.gno         — 에러 메시지 문구 차이
persist_native.gno          — pre-existing
redeclaration_global5.gno   — pre-existing
types/slice_0,2,3,5.gno     — 에러 메시지 문구 차이
zrealm13,14,15,16.gno       — Phase 1 array inlining으로 인한 golden diff (미업데이트)
storage/crossrealm.gno      — Phase 1으로 인한 object count 변경 (이미 업데이트됨)
```

### 카테고리 B — Golden File Diff (기능 정상, 기댓값만 업데이트 필요)

struct inlining으로 `RefValue` → inline `*StructValue` 변경된 테스트:

```
heap_item_value.gno
storage/struct1.gno, struct1b.gno, struct1c.gno
storage/struct2.gno, struct2b.gno
storage/struct3.gno, struct4.gno
storage/reference1.gno
struct63.gno, struct63b.gno, struct63c.gno, struct63d.gno, struct63e.gno
zrealm1.gno, zrealm2.gno, zrealm3.gno, zrealm4.gno, zrealm5.gno
zrealm10.gno, zrealm11.gno
append11c.gno (Realm diff)
map32.gno (Realm diff)
```

`-update-golden-tests` 플래그로 업데이트 가능:
```bash
go test -run 'TestFiles/^heap_item_value.gno' -short -v -update-golden-tests .
```

### 카테고리 C — 기능 버그 (수정 필요)

#### C1: `ptrmap_24.gno` — 포인터 맵 키 identity 손실

**증상:** `m[&i1.key]` lookup이 빈 문자열 반환 (기대: "first key")

**구조:**
```go
type Foo struct { name string }
type MyStruct struct { Name string; Age int; key Foo }
var i1 = MyStruct{..., key: Foo{name: "bob"}}
var m = map[*Foo]string{}
func init() { m[&i1.key] = "first key" }
func main() { println(m[&i1.key]) }  // → "" (should be "first key")
```

**원인 분석:**

1. `MyStruct`가 `shouldInlineStruct` → true (필드: string, int, Foo struct — 모두 inline 가능)
2. MyStruct는 ObjectID 없음 (phantom)
3. `&i1.key` → `PointerValue{Base: MyStruct, Index: 2}`
4. init() 시 이 PointerValue가 map key로 저장
5. persistence 후 main()에서 `&i1.key` 재평가 → 새 `PointerValue{Base: 새_MyStruct, Index: 2}`
6. MyStruct에 ObjectID 없으므로 `ComputeMapKey`가 다른 키 생성 → lookup 실패

**핵심:** inline struct의 필드에 대한 포인터(`&s.field`)를 map key로 사용하면, persistence 경계를 넘을 때 identity가 달라짐.

**panic이 발생하지 않는 이유 (미확인):**
- `copyValueWithRefs(PointerValue)` → `toRefValue(cv.Base)` 에서 Base가 real이 아니면 panic 해야 하지만, 실제로 panic이 발생하지 않음
- 가능한 원인:
  - PointerValue.Base가 MyStruct가 아닌 HeapItemValue일 수 있음 (VM의 selector 구현에 따라)
  - 또는 map key의 PointerValue가 ComputeMapKey 시점에서 별도 경로를 탐
- **이 부분의 정확한 확인이 필요함** — PointerValue.Base가 실제로 무엇인지 디버그 출력으로 확인해야 함

**잠재적 수정 방향:**

1. **보수적 접근:** `shouldInlineStruct`에서 nested struct 필드를 가진 struct를 거부
   ```go
   case *StructValue:
       return false // 중첩 struct 필드가 있으면 inline 불가
   ```
   - 단점: `Slot0{sqrtPriceX96: [4]uint64}` 같은 GnoSwap 대상은 영향 없음 (Array 필드만 가짐)
   - 하지만 struct 필드를 가진 struct (Balances 등)는 inline 불가

2. **정밀 접근:** `shouldInlineStruct`에서 `*StructValue` 필드를 허용하되, `GetFirstObject`에서 inline struct도 Object로 반환하여 ObjectID를 부여
   - `GetFirstObject`가 nil을 반환하면 DidUpdate에서 co/xo 추적 안됨 → 별도 KV 없음
   - `GetFirstObject`가 Object를 반환하면 ObjectID 부여됨 → inline이 아니게 됨
   - 이 접근은 사실상 inline을 포기하는 것

3. **PointerValue.Base 확인 후 판단:** `&s.field`가 실제로 Base=StructValue를 만드는지 확인. 만약 Base=HeapItemValue라면 문제가 다름

#### C2: `map39a.gno` — 출력 표현 변경 (기능 버그 여부 재확인 필요)

```
Expected: (map{(1 int):(ref(...) gno.land/r/map_ref_key.myInt)} ...)
Actual:   (map{(1 int):(struct{(1 int)} gno.land/r/map_ref_key.myInt)} ...)
```

struct가 inline되면서 `ref(...)` 대신 `struct{...}`로 출력됨. 이것이 단순 출력 변경인지 아니면 map key identity 문제와 관련 있는지 확인 필요.

#### C3: `govdao/realm_govdao.gno` — panic (pre-existing 가능성 높음)

```
panic: only members can create new proposals
```

Phase 2 비활성화 시에도 실패하는지 확인 필요. Pre-existing failures 목록에는 없었지만, 테스트 실행 환경 차이일 수 있음.

---

## 디버깅 가이드

### 디버그 프린트 삽입 위치

**PointerValue.Base 확인 (`&s.field` 시):**
```go
// op_expressions.go 또는 values.go의 selector 구현에서
// PointerValue 생성 시 Base 타입 출력:
fmt.Printf("[DEBUG] PointerValue created: Base=%T oid=%v\n", pv.Base, pv.Base.(Object).GetObjectID())
```

**ComputeMapKey 확인:**
```go
// values.go의 ComputeMapKey 함수에서
// PointerValue case에서 Base 정보 출력
```

**toRefValue panic 확인:**
```go
// realm.go:1809
} else if !oo.GetIsReal() {
    fmt.Printf("[DEBUG] toRefValue: unreal %T oid=%v\n", val, oo.GetObjectID())
    panic("unexpected unreal object")
}
```

### shouldInlineStruct 롤백 테스트

```go
func shouldInlineStruct(sv *StructValue) bool {
    return false // Phase 2 비활성화 → Phase 1 동작과 동일
}
```

### 테스트 명령

```bash
# 빌드
go build ./gnovm/pkg/gnolang/

# 특정 테스트
go test ./gnovm/pkg/gnolang/ -v -run "TestFiles/ptrmap_24.gno$" -count=1

# Golden 업데이트 (기능 버그 해결 후)
go test ./gnovm/pkg/gnolang/ -run 'TestFiles/^heap_item_value.gno' -short -v -update-golden-tests .

# 전체 테스트
go test ./gnovm/pkg/gnolang/ -v -run TestFiles -count=1

# Cross-realm
go test ./gnovm/pkg/gnolang/ -v -run TestFiles -count=1 2>&1 | grep -E "(crossrealm|PASS|FAIL)"
```

---

## 구현 흐름 요약

### 정상 경로 (inline struct 저장 → 로드 → 수정)

```
1. 최초 저장 (saveNewPackageValuesAndTypes):
   processNewCreatedMarks → HeapItemValue에 ObjectID 할당
   restoreInlineOwners(HeapItemValue) → 내부 inline struct에 owner 설정  ← 추가된 수정
   saveUnsavedObjects → SetObject(HeapItemValue):
     refOrCopyValue(structTV) → shouldInlineObject → true → copyStructInline
     HeapItemValue의 amino에 struct 직접 포함

2. init() 실행:
   struct.field = value → DidUpdate(struct, nil, nil)
   struct not real → shouldInlineObject → true
   owner walk-up → struct.GetOwner() = HeapItemValue (real) ← restoreInlineOwners 덕분
   MarkDirty(HeapItemValue) ✓

3. 재저장 (resavePackageValues):
   HeapItemValue dirty → SetObject → copyStructInline(struct) → 현재 값으로 저장 ✓

4. 로드 (main):
   loadObjectSafe(HeapItemValue) → restoreInlineOwners → struct.SetOwner(HeapItemValue) ✓
   필드 수정 → DidUpdate → walk-up → MarkDirty ✓
```

### 문제 경로 (ptrmap_24 — &struct.field as map key)

```
1. 최초 저장:
   MyStruct inline (shouldInlineStruct=true) → ObjectID 없음
   MapValue 생성 (empty) → ObjectID 할당

2. init():
   m[&i1.key] = "first key"
   → PointerValue{Base: MyStruct(?), Index: 2} 생성
   → MapValue에 entry 추가, MapValue dirty

3. 재저장:
   SetObject(MapValue) → copyValueWithRefs:
     map entry key = PointerValue → copyValueWithRefs(PointerValue)
     → toRefValue(cv.Base) → Base에 ObjectID 없으면 panic 또는 잘못된 RefValue
   ⚠️ 이 지점에서의 정확한 동작 미확인

4. 로드 (main):
   m[&i1.key] → 새 PointerValue 생성 → ComputeMapKey 다름 → lookup 실패 → ""
```

---

## 수정 파일 위치 참조

| 파일 | 주요 함수 | 라인 (대략) |
|------|----------|-----------|
| `realm.go` | `shouldInlineObject` (신규) | ~1862 |
| `realm.go` | `shouldInlineStruct` (신규) | ~1878 |
| `realm.go` | `copyStructInline` (신규) | ~1991 |
| `realm.go` | `setInlineChildOwners` (신규) | ~2005 |
| `realm.go` | `getSelfOrChildObjects` (수정) | ~1131 |
| `realm.go` | `refOrCopyValue` (수정) | ~2027 |
| `realm.go` | `DidUpdate` (수정) | ~212 |
| `realm.go` | `FinalizeRealmTransaction` (수정) | ~397 |
| `ownership.go` | `GetFirstObject` (수정) | ~382 |
| `values.go` | `Assign2` (수정) | ~233 |
| `store.go` | `restoreInlineOwners` (rename) | ~528 |
| `store.go` | `restoreInlineOwnersTV` (수정) | ~560 |
