# Phantom Array: getChildObjects PointerValue/SliceValue Base bypass 수정

**Branch:** `feat/phantom-array-inlining`
**선행 작업:** `c8401f70e` (centralize inline array skip in getSelfOrChildObjects)
**문제:** genesis 단계에서 `panic: unexpected unreal object` 발생

---

## 발견된 버그

### 증상

```
gnoland start → genesis tx 처리 중 panic:

panic: unexpected unreal object

goroutine 10 [running]:
  realm.go:1800  toRefValue: !oo.GetIsReal() → panic
  realm.go:1433  copyValueWithRefs → PointerValue case → toRefValue(cv.Base)
  realm.go:1929  refOrCopyValue
  realm.go:1559  copyValueWithRefs (parent object)
  store.go:664   SetObject
  realm.go:885   saveObject
  realm.go:851   saveUnsavedObjectRecursively (save created child)
  realm.go:840   saveUnsavedObjectRecursively (save child of child)
  realm.go:782   saveUnsavedObjects
  realm.go:415   FinalizeRealmTransaction
```

이전에 defensive panic을 넣었던 `"cannot persist slice of inline array"` 메시지로도 확인됨:
```
panic: cannot persist slice of inline array (len=0, data=false, list_len=6)
```

`list_len=6`으로, `[6]T` 배열이 slice의 backing store로 사용되는 케이스.

### 근본 원인

`getSelfOrChildObjects`에 centralized inline skip을 넣은 후 (`c8401f70e`), **모든** 경로에서 inline-eligible ArrayValue가 skip된다. 그러나 PointerValue와 SliceValue의 `Base`는 **ObjectID로 참조**되므로 반드시 real Object여야 한다.

```
getChildObjects(parent)
  → case *StructValue: for each field → getSelfOrChildObjects(field.V)
    → field.V = SliceValue → not Object → getChildObjects(SliceValue)
      → case *SliceValue: getSelfOrChildObjects(cv.Base = ArrayValue)
        → shouldInlineArray(ArrayValue) → true (not real, RC=0, len=6≤8, primitive)
        → SKIP!  ← ArrayValue가 자식 목록에서 누락

processNewCreatedMarks:
  → incRefCreatedDescendants(parent):
    → getChildObjects2(parent) → ArrayValue 누락
    → ArrayValue에 ObjectID 할당 안됨

saveUnsavedObjects:
  → saveUnsavedObjectRecursively(parent):
    → SetObject(parent) → copyValueWithRefs:
      → SliceValue → toRefValue(cv.Base = ArrayValue)
      → ArrayValue.GetIsReal() = false
      → panic("unexpected unreal object")
```

---

## 분석: getChildObjects 호출 경로 분류

`getChildObjects` 내에서 `getSelfOrChildObjects`를 호출하는 경로를 두 카테고리로 분류한다.

### 카테고리 A — TypedValue 값 위치 (inline skip 적용 가능)

배열이 부모의 **필드 값 그 자체**이므로 부모의 KV 엔트리에 inline 직렬화 가능. `refOrCopyValue`에서 `copyArrayInline`으로 처리됨.

| getChildObjects case | 코드 위치 | 대상 표현식 |
|---------------------|----------|-----------|
| `*StructValue` | line 1170 | `cv.Fields[i].V` |
| `*Block` | line 1201 | `cv.Values[i].V` |
| `*HeapItemValue` | line 1209 | `cv.Value.V` |
| `*MapValue` | line 1187-88 | `cur.Key.V`, `cur.Value.V` |
| `*FuncValue` (captures) | line 1178 | `cv.Captures[i].V` |
| `*BoundMethodValue` (receiver) | line 1183 | `cv.Receiver.V` |

이 경로에서 inline skip이 적용되면:
- `incRefCreatedDescendants`: ArrayValue에 ObjectID 할당 안됨 → OK (inline이므로 불필요)
- `saveUnsavedObjects`: ArrayValue 별도 저장 안됨 → OK (부모 KV에 포함)
- `copyValueWithRefs`: `refOrCopyValue` → `copyArrayInline` → 부모 amino에 직접 포함 → OK

### 카테고리 B — 참조 Base (inline skip 적용 불가)

배열이 PointerValue/SliceValue의 **backing store**로, 직렬화 시 `toRefValue(Base)` → ObjectID 참조로 저장됨. 반드시 real Object여야 함.

| getChildObjects case | 코드 위치 | 대상 표현식 | 사용처 |
|---------------------|----------|-----------|-------|
| `PointerValue` | line 1158 | `cv.Base` | `&arr[i]`의 arr |
| `*SliceValue` | line 1166 | `cv.Base` | `arr[:]`의 arr, `make([]T, n)` |

이 경로에서 inline skip이 적용되면:
- `incRefCreatedDescendants`: ArrayValue에 ObjectID 할당 안됨 → **문제!** (toRefValue가 ObjectID를 요구)
- `saveUnsavedObjects`: ArrayValue 별도 저장 안됨 → **문제!** (KV에 없으면 GetObject 실패)
- `copyValueWithRefs`: `toRefValue(ArrayValue)` → `!GetIsReal()` → **panic**

---

## 해결: PointerValue/SliceValue case에서 getSelfOrChildObjects bypass

`getChildObjects`의 PointerValue와 SliceValue case에서만 `getSelfOrChildObjects`를 bypass하고 Base를 **직접 append**한다.

### 수정 대상: `realm.go` — `getChildObjects` 함수

**현재 코드 (line 1154-1167):**
```go
case PointerValue:
    if cv.Base == nil {
        panic("should not happen")
    }
    more = getSelfOrChildObjects(cv.Base, more)
    return more
case *ArrayValue:
    for _, ctv := range cv.List {
        more = getSelfOrChildObjects(ctv.V, more)
    }
    return more
case *SliceValue:
    more = getSelfOrChildObjects(cv.Base, more)
    return more
```

**수정 후:**
```go
case PointerValue:
    if cv.Base == nil {
        panic("should not happen")
    }
    // Pointer/slice bases are referenced by ObjectID in
    // serialization (toRefValue), so they MUST remain real
    // Objects. Bypass getSelfOrChildObjects to avoid the
    // inline array skip — these arrays cannot be inlined.
    if ref, ok := cv.Base.(RefValue); ok {
        more = append(more, ref)
    } else if obj, ok := cv.Base.(Object); ok {
        more = append(more, obj)
    }
    return more
case *ArrayValue:
    for _, ctv := range cv.List {
        more = getSelfOrChildObjects(ctv.V, more)
    }
    return more
case *SliceValue:
    // Same rationale as PointerValue above.
    if ref, ok := cv.Base.(RefValue); ok {
        more = append(more, ref)
    } else if obj, ok := cv.Base.(Object); ok {
        more = append(more, obj)
    }
    return more
```

### `getSelfOrChildObjects`는 수정하지 않음

```go
func getSelfOrChildObjects(val Value, more []Value) []Value {
    if _, ok := val.(RefValue); ok {
        return append(more, val)
    } else if obj, ok := val.(Object); ok {
        if av, isArr := obj.(*ArrayValue); isArr && shouldInlineArray(av) {
            return more // skip — inlined in parent
        }
        return append(more, val)
    } else {
        return getChildObjects(val, more)
    }
}
```

이 함수의 inline skip은 카테고리 A 경로에서만 호출되므로 유지한다. 카테고리 B는 `getSelfOrChildObjects`를 거치지 않고 직접 append하므로 영향 없음.

---

## 수정 후 경로 검증

### 경로 1: Slice backing store (`make([]int, 6)`)

```
genesis: init()에서 arr := [6]int{...}; s = arr[:]

FinalizeRealmTransaction:
  processNewCreatedMarks → incRefCreatedDescendants(parent):
    → getChildObjects2(parent) → StructValue fields:
      → field = SliceValue → getChildObjects(SliceValue)
        → case *SliceValue: 직접 append(ArrayValue)  ← bypass
      → ArrayValue가 자식 목록에 포함됨
    → child.IncRefCount() → RC=1
    → child.SetOwner(parent), child.SetIsNewReal(true)
    → incRefCreatedDescendants(ArrayValue) → assignNewObjectID
    → ArrayValue가 real이 됨 ✓

  saveUnsavedObjects → saveUnsavedObjectRecursively(parent):
    → getUnsavedChildObjects → SliceValue Base:
      → case *SliceValue: 직접 append(ArrayValue)
      → isUnsaved(ArrayValue) → GetIsNewReal()=true → yes
    → ArrayValue 먼저 저장 → SetObject → KV 엔트리 생성 ✓
    → parent 저장 → toRefValue(ArrayValue) → real → 성공 ✓
```

### 경로 2: Struct 필드의 [4]uint64 (u256.Uint) — 여전히 inline

```
pool.liquidity = u256.Zero()

FinalizeRealmTransaction:
  processNewCreatedMarks → incRefCreatedDescendants(Pool):
    → getChildObjects2(Pool) → StructValue fields:
      → field.V = ArrayValue → getSelfOrChildObjects(ArrayValue)
        → shouldInlineArray(ArrayValue) → true → SKIP ✓
      → ArrayValue 자식 목록에서 제외됨
    → ObjectID 할당 안됨 → inline 처리 ✓

  saveUnsavedObjects → saveUnsavedObjectRecursively(Pool):
    → getUnsavedChildObjects → StructValue fields:
      → getSelfOrChildObjects(ArrayValue) → SKIP ✓
    → Pool 저장: refOrCopyValue → copyArrayInline → 부모 KV에 inline ✓
```

### 경로 3: PointerValue Base (`&arr[i]`)

```
type Container struct { ptr *[4]int }
arr := [4]int{1,2,3,4}
c.ptr = &arr

FinalizeRealmTransaction:
  processNewCreatedMarks → incRefCreatedDescendants(Container):
    → getChildObjects2(Container) → StructValue fields:
      → field.V = PointerValue → getChildObjects(PointerValue)
        → case PointerValue: 직접 append(ArrayValue)  ← bypass
      → ArrayValue가 자식 목록에 포함됨
    → IncRefCount, SetOwner, MarkNewReal, assignNewObjectID
    → ArrayValue가 real이 됨 ✓
    → shouldInlineArray → GetIsReal()=true → false ✓
```

### 경로 4: Map value의 [4]uint64 — inline

```
map[string]u256.Uint → MapValue → entries → value.V = ArrayValue

getChildObjects(MapValue):
  → for cur: getSelfOrChildObjects(cur.Value.V = ArrayValue)
    → shouldInlineArray → true → SKIP ✓
    → inline 처리 ✓
```

---

## `copyValueWithRefs` pointer/slice 안전망

현재 `copyValueWithRefs`의 PointerValue case에서 inline array 관련 panic은 **제거된 상태**이다.

PointerValue/SliceValue의 Base array는 위 수정 후 항상 real이 되므로 `toRefValue(cv.Base)`가 정상 작동한다. 추가 방어가 필요하면 `toRefValue` 자체의 `!oo.GetIsReal()` panic이 최종 방어선 역할을 한다.

---

## 디버깅 참고사항

### 진단 방법: panic 시 array 정보 출력

SliceValue/PointerValue Base가 문제일 때, 다음 코드로 어떤 배열인지 확인 가능:

```go
case *SliceValue:
    if av, ok := cv.Base.(*ArrayValue); ok && !av.GetIsReal() {
        panic(fmt.Sprintf(
            "slice base array not real: data=%v, list_len=%d, rc=%d, real=%v, newreal=%v",
            av.Data != nil, len(av.List), av.GetRefCount(),
            av.GetIsReal(), av.GetIsNewReal()))
    }
```

### `shouldInlineArray` 판정 추적

문제 발생 시 어떤 ArrayValue가 inline으로 판정되는지 추적하려면:

```go
func shouldInlineArray(av *ArrayValue) bool {
    result := shouldInlineArrayInner(av)
    if result && debugRealm {
        fmt.Printf("[inline-debug] ArrayValue list_len=%d data=%v rc=%d real=%v escaped=%v\n",
            len(av.List), av.Data != nil, av.GetRefCount(),
            av.GetIsReal(), av.GetIsEscaped())
    }
    return result
}
```

### 테스트 실행 명령

```bash
# 빌드 확인
go build ./gnovm/pkg/gnolang/

# position lifecycle (genesis + CreatePool + Mint + Swap + CollectFee)
GNO_REALM_STATS_LOG=/tmp/phantom_stats.log \
go test -v -run "TestTestdata/position_storage_poisition_lifecycle" \
    ./gno.land/pkg/integration/ -timeout 600s 2>&1 | \
    grep -E "(STEP|STORAGE DELTA:|PASS|FAIL|panic)"

# cross-realm 테스트
go test ./gnovm/pkg/gnolang/ -v -run TestFiles -count=1 2>&1 | \
    grep -E "(crossrealm|PASS|FAIL)"

# 전체 통합 테스트
go test ./gno.land/pkg/integration/ -v -run TestTestdata -timeout 600s
```

### 이전 디버깅에서 시도된 접근과 실패 이유

| 시도 | 결과 | 실패 이유 |
|------|------|----------|
| `copyValueWithRefs` SliceValue case에서 panic → pass-through 변경 | `toRefValue` panic | ArrayValue가 real이 아님 |
| `getChildObjects` PointerValue/SliceValue에서 직접 `append(more, obj)` | 여전히 `toRefValue` panic | `append`는 했지만 `getUnsavedChildObjects`의 `isUnsaved` 체크를 통과하지 못함... 이 아니라, `incRefCreatedDescendants`에서 먼저 처리되어 real이 되어야 함. 실제로는 `processNewCreatedMarks` → `saveUnsavedObjects` 순서로 실행되므로, append하면 ObjectID가 먼저 할당되고 이후 save 시 real임. **이 접근은 실제로 작동해야 함.** 이전 시도에서 실패한 이유는 동시에 다른 변경 (revert)도 적용되어 있었기 때문으로 추정. |

### 핵심 타이밍 보장

`FinalizeRealmTransaction`의 실행 순서가 정확성을 보장한다:

```
① processNewCreatedMarks  ← ArrayValue에 ObjectID 할당 (여기서 real이 됨)
② processNewDeletedMarks
③ processNewEscapedMarks
④ markDirtyAncestors
⑤ saveUnsavedObjects      ← 이 시점에서 ArrayValue는 이미 real
```

따라서 `getChildObjects`에서 PointerValue/SliceValue의 Base를 직접 append하면:
- ①에서 `incRefCreatedDescendants` → `getChildObjects2` → ArrayValue 발견 → ObjectID 할당
- ⑤에서 `saveUnsavedObjectRecursively` → `getUnsavedChildObjects` → ArrayValue 발견 → 먼저 저장
- 부모 직렬화 시 `toRefValue(ArrayValue)` 성공
