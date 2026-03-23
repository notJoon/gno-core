# Phantom Struct Inlining 구현 계획서 (Phase 2) — Reviewed

**Branch:** `feat/phantom-array-inlining`
**선행 작업:** Phase 1 (Array Inlining) — 구현 완료, 테스트 통과
**목표:** by-value StructValue 필드를 부모 Object의 KV 엔트리에 inline 직렬화하여, 별도 KV 엔트리 생성을 제거한다.
**주 대상:** Pool.slot0 (Slot0), Pool.balances (Balances), Pool.protocolFees (ProtocolFees)

> **리뷰 상태:** 코드 리뷰 완료. 아래 ⚠️ 표시된 부분은 리뷰에서 확인된 주의사항.
>
> **실측 기반 절감 예상:** Pool write당 ~300-400B 추가 절감 (Phase 1의 10% 위에 추가 1-2%)
>
> **avl.Tree는 Phase 2 대상이 아님.** DidUpdate co/xo 문제로 Phase 3으로 연기.

---

## Phase 1 현재 상태 (코드 참조)

Phase 1에서 구현된 ArrayValue 인라인의 수정 지점과 현재 코드 위치:

| # | 파일:라인 | 함수 | 역할 |
|---|----------|------|------|
| 1 | `realm.go:1875` | `shouldInlineArray` | inline 대상 판별 (`!real`, `!escaped`, `RC≤1`, `len≤8`, `V==nil` primitive) |
| 2 | `realm.go:1908` | `copyArrayInline` | zero ObjectInfo로 배열 복사 |
| 3 | `realm.go:1127` | `getSelfOrChildObjects` | inline array를 자식 Object 등록에서 skip |
| 4 | `realm.go:1926` | `refOrCopyValue` | inline array → `copyArrayInline` 분기 |
| 5 | `ownership.go:375` | `GetFirstObject` | inline array에 대해 nil 반환 |
| 6 | `realm.go:212-227` | `DidUpdate` | non-real phantom array → owner walk-up → MarkDirty |
| 7 | `values.go:233-239` | `Assign2` | 할당 후 inline array에 owner 설정 |
| 8 | `store.go:513` | `loadObjectSafe` | `restoreInlineArrayOwners` 호출 |
| 9 | `store.go:528-594` | `restoreInlineArrayOwners` / `restoreInlineArrayOwnersTV` | 역직렬화 후 owner 복원 (재귀적) |
| 10 | `realm.go:1154-1179` | `getChildObjects` PointerValue/SliceValue | bypass — Base를 직접 append (inline skip 우회) |

### Phase 1에서 계획 대비 달라진 점

1. **`shouldInlineArray` element check 강화:** 계획서의 `if _, isObj := tv.V.(Object); isObj`가 아닌, `if tv.V != nil { return false }`로 구현. 순수 primitive(V가 nil이고 값이 N 필드에 저장)만 허용. BigintValue, StringValue 등도 거부됨.

2. **PointerValue/SliceValue Base bypass 추가:** 계획서에 없던 수정. Pointer/Slice의 backing array는 `toRefValue`로 직렬화되므로 반드시 real이어야 함. `getSelfOrChildObjects`를 거치면 inline skip될 수 있어, `getChildObjects`에서 직접 append.

3. **`restoreInlineArrayOwners` 대폭 확장:** 단순 StructValue/Block 필드 스캔에서 재귀적 `restoreInlineArrayOwnersTV` 헬퍼로 확장. MapValue, ArrayValue, 중첩 StructValue(zero ObjectID) 등 non-Object 컨테이너 내부의 inline array도 처리.

---

## Phase 2: StructValue Inlining

### 안전성 분석 — ArrayValue와 동일한 속성

StructValue를 inline할 수 있는 근거는 ArrayValue 인라인과 동일한 4가지 안전 속성이다:

1. **RefValue 참조 없음:** `pool.slot0`같은 by-value struct 필드는 어떤 Object도 Slot0의 ObjectID를 RefValue로 참조하지 않는다. Slot0은 Pool의 필드에 직접 존재한다.

2. **PointerValue가 부모+인덱스 방식:** `&pool.slot0` → `PointerValue{Base: Pool, Index: slot0FieldIdx}`. Slot0의 OID를 사용하지 않는다.

3. **`GetObject(slot0OID)` 호출 경로 없음:** 위 1, 2번으로 인해 존재하지 않는다.

4. **Genesis blocker 비적용:** HeapItemValue와 달리, by-value StructValue는 Block.Values[]에서 이중 참조되지 않는다. (`VM_inline_integration_blockers.md` 참조)

### DidUpdate dirty 전파

Phase 1의 owner walk-up 로직이 **수정 없이** StructValue에도 적용된다:

**단일 레벨:**
```
pool.slot0.tick = 42
→ Assign2: pv.Base = Slot0(StructValue)
→ DidUpdate(Slot0, nil, nil)
→ Slot0.GetIsReal() = false (phantom)
→ shouldInlineObject(Slot0) = true
→ ancestor = Slot0.GetOwner() = Pool (real)
→ MarkDirty(Pool) ✓
```

**다단계 (phantom struct 내부의 phantom array):**
```
pool.slot0.sqrtPriceX96[0] = value
→ Assign2: pv.Base = ArrayValue([4]uint64)
→ DidUpdate(ArrayValue, nil, nil)
→ ArrayValue.GetIsReal() = false
→ shouldInlineObject(ArrayValue) = true
→ ancestor = ArrayValue.GetOwner() = Slot0 → !real
→ ancestor = Slot0.GetOwner() = Pool → real
→ MarkDirty(Pool) ✓
```

현재 코드의 walk-up 로직 (`realm.go:218-224`)이 이미 다단계를 지원한다.

---

## 수정 지점 (9곳)

Phase 1의 기존 코드에 **타입 체크 확장** + 새 함수 2개 추가.

### 변경 1: `shouldInlineObject` 디스패처 추가 + `shouldInlineStruct` 추가

**위치:** `realm.go:1875` (`shouldInlineArray` 근처)

```go
// shouldInlineObject returns true if the given Object should be
// serialized inline within its parent rather than as a separate
// KV entry. Dispatches to type-specific checks.
func shouldInlineObject(obj Object) bool {
    switch o := obj.(type) {
    case *ArrayValue:
        return shouldInlineArray(o)
    case *StructValue:
        return shouldInlineStruct(o)
    default:
        return false
    }
}

// shouldInlineStruct returns true if the given StructValue should be
// serialized inline within its parent Object.
//
// Criteria (ArrayValue와 동일 + 재귀적 필드 검사):
//   - NOT already persisted (real)
//   - NOT escaped or multi-referenced
//   - Small (≤ 16 fields)
//   - All fields are either primitives or recursively inlineable Objects
//   - ⚠️ NO PointerValue/SliceValue fields (co/xo 문제 회피)
func shouldInlineStruct(sv *StructValue) bool {
    if sv.GetIsReal() {
        return false
    }
    if sv.GetIsEscaped() || sv.GetIsNewEscaped() {
        return false
    }
    if sv.GetRefCount() > 1 {
        return false
    }
    if len(sv.Fields) > 16 {
        return false
    }
    for _, ftv := range sv.Fields {
        if ftv.V == nil {
            continue // pure primitive (value in N field)
        }
        // ⚠️ CRITICAL: PointerValue/SliceValue fields must be
        // rejected. These hold .Base references to Objects (e.g.
        // HeapItemValue) that require co/xo tracking in DidUpdate.
        // When the parent struct is phantom (non-real), DidUpdate
        // returns early without processing co/xo, which would
        // leave the Base Objects without IncRefCount/MarkNewReal.
        // (See "경로 3: avl.Tree" analysis below.)
        switch ftv.V.(type) {
        case PointerValue, *SliceValue:
            return false
        }
        if obj, isObj := ftv.V.(Object); isObj {
            if !shouldInlineObject(obj) {
                return false
            }
            continue
        }
        // Non-Object, non-nil V with no Base reference:
        // BigintValue, StringValue, etc. — safe to inline.
    }
    return true
}
```

**`shouldInlineArray`는 변경하지 않음.** 기존 함수가 그대로 유지되며, `shouldInlineObject`가 디스패처 역할만 한다.

**PointerValue/SliceValue 필드가 있는 struct (예: avl.Tree):**

avl.Tree의 구조:
```go
type Tree struct {
    node *Node  // field.V = PointerValue{Base: HeapItemValue}
    size int    // field.V = nil (primitive)
}
```

- `node` 필드: `V = PointerValue` (Object가 아님) → isObj 체크 통과
- PointerValue는 `refOrCopyValue` → `copyValueWithRefs` → `toRefValue(Base)` 로 정상 직렬화
- Tree 자체는 inline 가능, 내부 `*Node`는 RefValue로 별도 저장 유지 ✓

**MapValue 필드가 있는 struct (예: ObservationState):**

```go
type ObservationState struct {
    observations map[uint16]Observation  // field.V = *MapValue (Object)
    index        uint16
    // ...
}
```

- `observations` 필드: `V = *MapValue` → `shouldInlineObject(*MapValue)` → default case → false
- `shouldInlineStruct(ObservationState)` → false ✓ (MapValue 포함 struct는 인라인 불가)

### 변경 2: `copyStructInline` 추가

**위치:** `realm.go` (`copyArrayInline` 근처)

```go
// copyStructInline creates a serialization-ready copy of an inline
// StructValue. The copy has ZERO ObjectInfo — the signal that this
// struct is inlined in the parent and has no independent KV entry.
func copyStructInline(sv *StructValue) *StructValue {
    fields := make([]TypedValue, len(sv.Fields))
    for i, ftv := range sv.Fields {
        fields[i] = refOrCopyValue(ftv)
    }
    return &StructValue{
        // ObjectInfo intentionally zero.
        Fields: fields,
    }
}
```

**`refOrCopyValue`의 재귀 호출이 핵심:**
- 필드가 inline ArrayValue → `copyArrayInline` (Phase 1)
- 필드가 inline StructValue → `copyStructInline` (재귀)
- 필드가 non-inline Object → `toRefValue` (기존 경로)
- 필드가 primitive/PointerValue/SliceValue → `copyValueWithRefs` (기존 경로)

### 변경 3: `getSelfOrChildObjects` 수정

**위치:** `realm.go:1127-1138`

**현재:**
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

**수정:**
```go
func getSelfOrChildObjects(val Value, more []Value) []Value {
    if _, ok := val.(RefValue); ok {
        return append(more, val)
    } else if obj, ok := val.(Object); ok {
        if shouldInlineObject(obj) {
            return more // skip — inlined in parent
        }
        return append(more, val)
    } else {
        return getChildObjects(val, more)
    }
}
```

**영향 분석:**

기존 Phase 1에서 inline skip은 ArrayValue에만 적용되었다. 이제 StructValue도 skip 대상이 된다.

| 호출자 경로 | 효과 |
|------------|------|
| StructValue fields → `getSelfOrChildObjects(fieldValue)` | inline struct 필드가 skip됨 |
| Block values → `getSelfOrChildObjects(value)` | Block 내 inline struct가 skip됨 |
| MapValue entries → `getSelfOrChildObjects(value)` | Map entry의 inline struct가 skip됨 |
| HeapItemValue → `getSelfOrChildObjects(value)` | HeapItem 내 inline struct가 skip됨 |

**PointerValue/SliceValue case는 변경 없음:** Phase 1에서 이미 bypass로 구현됨 (`realm.go:1154-1179`). Pointer/Slice의 Base에 StructValue가 오는 경우는 없다 (Pointer.Base는 항상 HeapItemValue 또는 ArrayValue).

### 변경 4: `refOrCopyValue` 수정

**위치:** `realm.go:1926-1941`

**현재:**
```go
func refOrCopyValue(tv TypedValue) TypedValue {
    if tv.T != nil {
        tv.T = refOrCopyType(tv.T)
    }
    if obj, ok := tv.V.(Object); ok {
        if av, isArr := obj.(*ArrayValue); isArr && shouldInlineArray(av) {
            tv.V = copyArrayInline(av)
            return tv
        }
        tv.V = toRefValue(obj)
        return tv
    } else {
        tv.V = copyValueWithRefs(tv.V)
        return tv
    }
}
```

**수정:**
```go
func refOrCopyValue(tv TypedValue) TypedValue {
    if tv.T != nil {
        tv.T = refOrCopyType(tv.T)
    }
    if obj, ok := tv.V.(Object); ok {
        if shouldInlineObject(obj) {
            switch o := obj.(type) {
            case *ArrayValue:
                tv.V = copyArrayInline(o)
            case *StructValue:
                tv.V = copyStructInline(o)
            }
            return tv
        }
        tv.V = toRefValue(obj)
        return tv
    } else {
        tv.V = copyValueWithRefs(tv.V)
        return tv
    }
}
```

**직렬화 결과 비교:**

현재: 부모의 필드 → `RefValue{ObjectID: "pkg:42", Hash: [20]byte}` (~72 bytes)
수정후: 부모의 필드 → `*StructValue{Fields: [...]}` (필드 데이터 직접 포함)

### 변경 5: `GetFirstObject` 수정

**위치:** `ownership.go:379-380`

**현재:**
```go
case *StructValue:
    return cv
```

**수정:**
```go
case *StructValue:
    if shouldInlineStruct(cv) {
        return nil
    }
    return cv
```

**이유:** Phase 1의 ArrayValue와 동일. `GetFirstObject`가 inline StructValue를 반환하면 `Assign2` → `DidUpdate`에서 co로 전달 → `MarkNewReal` → ObjectID 할당 → 별도 KV 엔트리 생성. nil을 반환하면 `DidUpdate(parent, nil, nil)` → `MarkDirty(parent)` only.

### 변경 6: `DidUpdate` 수정 — phantom 타입 일반화

**위치:** `realm.go:212-227`

**현재:**
```go
if !po.GetIsReal() {
    if av, ok := po.(*ArrayValue); ok && shouldInlineArray(av) {
        ancestor := av.GetOwner()
        for ancestor != nil && !ancestor.GetIsReal() {
            ancestor = ancestor.GetOwner()
        }
        if ancestor != nil && ancestor.GetObjectID().PkgID == rlm.ID {
            rlm.MarkDirty(ancestor)
        }
    }
    return
}
```

**수정:**
```go
if !po.GetIsReal() {
    if shouldInlineObject(po) {
        ancestor := po.GetOwner()
        for ancestor != nil && !ancestor.GetIsReal() {
            ancestor = ancestor.GetOwner()
        }
        if ancestor != nil && ancestor.GetObjectID().PkgID == rlm.ID {
            rlm.MarkDirty(ancestor)
        }
    }
    return
}
```

**변경 최소화:** `*ArrayValue` 타입 단언을 `shouldInlineObject(po)` 호출로 교체. `shouldInlineObject`가 내부적으로 `*ArrayValue` / `*StructValue` 디스패치를 수행.

### 변경 7: `Assign2` 수정 — owner 설정 대상 확장

**위치:** `values.go:233-239`

**현재:**
```go
// Set owner on newly-assigned inline arrays so that
// element-level DidUpdate can walk up to the real ancestor.
if baseObj, ok := pv.Base.(Object); ok && baseObj.GetIsReal() {
    if av, ok := pv.TV.V.(*ArrayValue); ok && shouldInlineArray(av) {
        av.SetOwner(baseObj)
    }
}
```

**수정:**
```go
// Set owner on newly-assigned inline objects so that
// element-level DidUpdate can walk up to the real ancestor.
if baseObj, ok := pv.Base.(Object); ok && baseObj.GetIsReal() {
    if obj, ok := pv.TV.V.(Object); ok && shouldInlineObject(obj) {
        obj.SetOwner(baseObj)
        // For inline structs, also set owner on nested
        // inline children (arrays, sub-structs).
        if sv, isSt := obj.(*StructValue); isSt {
            setInlineChildOwners(sv, baseObj)
        }
    }
}
```

**`setInlineChildOwners` 헬퍼 (values.go 또는 realm.go에 추가):**

```go
// setInlineChildOwners recursively sets the owner on inline
// objects within an inline StructValue. This enables multi-level
// DidUpdate walk-up (e.g. pool.slot0.sqrtPriceX96[0] = v).
func setInlineChildOwners(sv *StructValue, owner Object) {
    for i := range sv.Fields {
        switch cv := sv.Fields[i].V.(type) {
        case *ArrayValue:
            if shouldInlineArray(cv) {
                cv.SetOwner(owner)
            }
        case *StructValue:
            if shouldInlineStruct(cv) {
                cv.SetOwner(owner)
                setInlineChildOwners(cv, owner)
            }
        }
    }
}
```

**owner를 real ancestor로 설정하는 이유:**

inline struct와 inline array 모두 **real ancestor를 owner로 설정**한다. 이렇게 하면 walk-up이 한 단계로 끝난다:

```
pool.slot0.sqrtPriceX96[0] = value
→ DidUpdate(ArrayValue)
→ ArrayValue.GetOwner() = Pool (real)  ← 중간의 Slot0을 건너뜀
→ MarkDirty(Pool) ✓
```

**대안 (체인 owner)과 비교:**

체인 방식 (`ArrayValue.owner = Slot0, Slot0.owner = Pool`)도 walk-up으로 동작하지만, owner 설정 지점이 분산되어 관리가 복잡해진다. flat owner (모든 inline 자식 → real ancestor) 방식이 더 단순하고, 현재 walk-up 로직과도 호환된다.

### 변경 8: `restoreInlineArrayOwnersTV` 확장 — StructValue 재귀

**위치:** `store.go:560-594`

**현재 코드에서 이미 StructValue 재귀를 부분적으로 처리 중:**
```go
case *StructValue:
    // Only non-Object embedded structs (zero ObjectID).
    if !cv.GetIsReal() && cv.GetObjectID().IsZero() {
        for i := range cv.Fields {
            restoreInlineArrayOwnersTV(&cv.Fields[i], owner)
        }
    }
```

**수정:** `shouldInlineStruct` 체크를 추가하고, StructValue 자체에도 owner를 설정.

```go
case *StructValue:
    if shouldInlineStruct(cv) {
        cv.SetOwner(owner)
        for i := range cv.Fields {
            restoreInlineArrayOwnersTV(&cv.Fields[i], owner)
        }
    } else if !cv.GetIsReal() && cv.GetObjectID().IsZero() {
        // Defensive: non-inline, non-real struct — still
        // recurse for nested inline arrays.
        for i := range cv.Fields {
            restoreInlineArrayOwnersTV(&cv.Fields[i], owner)
        }
    }
```

### 변경 9: 함수 이름 변경 (선택적)

`restoreInlineArrayOwners` → `restoreInlineOwners` 로 rename하여 array-only가 아님을 반영. `replace_all`로 일괄 변경 가능. 현재 호출 지점은 `store.go:513`의 `loadObjectSafe` 내 1곳.

---

## 수정하지 않는 파일들

### `getChildObjects` PointerValue/SliceValue bypass — 수정 불필요

**현재 코드 (`realm.go:1154-1179`):**
```go
case PointerValue:
    if ref, ok := cv.Base.(RefValue); ok {
        more = append(more, ref)
    } else if obj, ok := cv.Base.(Object); ok {
        more = append(more, obj)
    }
    return more
```

PointerValue.Base가 StructValue인 경우는 없다:
- `&structField` → `PointerValue{Base: parentStruct, Index: fieldIdx}` (Base=부모)
- `&heapVar` → `PointerValue{Base: HeapItemValue}` (Base=HeapItem)
- `&arr[i]` → `PointerValue{Base: ArrayValue}` (Base=배열)

따라서 bypass 로직에 StructValue 관련 변경 불필요.

### `op_assign.go` — 수정 불필요

Phase 1과 동일한 이유. 모든 compound assign이 `m.Realm.DidUpdate(lv.Base.(Object), nil, nil)`을 호출. `DidUpdate` 내부에서 `shouldInlineObject`가 처리.

### `op_inc_dec.go` — 수정 불필요

동일한 이유.

### `copyValueWithRefs`의 `*StructValue` case — 수정 불필요

이 case는 StructValue가 **독립 Object로 저장**될 때 사용된다. inline struct는 `refOrCopyValue`에서 `copyStructInline`으로 처리되므로 이 case에 도달하지 않는다.

---

## GnoSwap 대상 구조체 분석

### Pool — Swap마다 갱신 (가장 빈번)

```
Pool (real, KV)
├── slot0: Slot0 (StructValue)          ← shouldInlineStruct ✓
│   └── sqrtPriceX96: [4]uint64         ← shouldInlineArray ✓ (중첩)
├── balances: Balances (StructValue)    ← shouldInlineStruct ✓
│   └── (embedded TokenPair)
│       ├── token0: [4]uint64           ← shouldInlineArray ✓
│       └── token1: [4]uint64           ← shouldInlineArray ✓
├── protocolFees: ProtocolFees          ← shouldInlineStruct ✓
│   └── (embedded TokenPair)
│       ├── token0: [4]uint64           ← shouldInlineArray ✓
│       └── token1: [4]uint64           ← shouldInlineArray ✓
├── liquidity: [4]uint64               ← shouldInlineArray ✓
├── feeGrowthGlobal0X128: [4]uint64    ← shouldInlineArray ✓
├── feeGrowthGlobal1X128: [4]uint64    ← shouldInlineArray ✓
├── maxLiquidityPerTick: [4]uint64     ← shouldInlineArray ✓
├── ticks: avl.Tree (StructValue)      ← shouldInlineStruct ✓
│   ├── node: PointerValue(*Node)       (non-Object, OK)
│   └── size: int                       (primitive, OK)
├── tickBitmaps: avl.Tree              ← shouldInlineStruct ✓
├── positions: avl.Tree                ← shouldInlineStruct ✓
└── observationState: ObservationState ← shouldInlineStruct ✗ (MapValue 포함)
```

**Phase 1 (array only)로 제거되는 KV:** 9개 ArrayValue
**Phase 2 (+ struct)로 추가 제거되는 KV:** 6개 StructValue (Slot0, Balances, ProtocolFees, 3× avl.Tree)

### TickInfo — Mint/Swap마다 갱신

```
TickInfo (StructValue, avl.Tree value로 저장)
├── liquidityGross: [4]uint64        ← shouldInlineArray ✓
├── liquidityNet: [4]uint64          ← shouldInlineArray ✓ (i256.Int)
├── feeGrowthOutside0X128: [4]uint64 ← shouldInlineArray ✓
├── feeGrowthOutside1X128: [4]uint64 ← shouldInlineArray ✓
├── secondsPerLiquidityX128: [4]uint64 ← shouldInlineArray ✓
└── (3 primitive fields)
```

TickInfo 자체는 avl.Tree의 value로 저장되므로, TreeNode(HeapItemValue)의 value로 존재. TickInfo가 by-value라면 shouldInlineStruct 대상. 단, avl.Tree의 value는 `interface{}` 타입이므로 HeapItemValue 내부에 있을 수 있음 — `restoreInlineArrayOwnersTV`의 HeapItemValue case에서 처리됨.

---

## 절감 추정 (실측 데이터 기반)

### Phase 1 실측 결과 (현재 상태)

| Step | Phase 1 전(B) | Phase 1 후(B) | 절감(B) | 절감률 |
|------|--------:|--------:|--------:|------:|
| CreatePool | 14,345 | 12,915 | -1,430 | 10.0% |
| First Mint | 32,008 | 28,909 | -3,099 | 9.7% |
| Second Mint | 30,716 | 27,617 | -3,099 | 10.1% |
| Third Mint | 10,289 | 9,320 | -969 | 9.4% |
| Swap | 6 | 12 | +6 | — |

### Pool realm 실측 바이트 구성

Pool realm updated StructValues (Second Mint 기준):
- **Pool StructValue: 3,148B** (inline array 포함)
- **Sub-struct 4개: avg 397B** (Slot0, Balances, ProtocolFees, 그 외 1개)

이 sub-struct들은 Phase 1에서 이미 내부 `[4]uint64`가 inline되었다. Phase 2는 이 sub-struct **자체**를 Pool KV에 inline하여 별도 KV 엔트리를 제거한다.

### Phase 2 예상 절감 (보수적 접근)

**inline 대상 (PointerValue 필드 없는 struct만):**

| 구조체 | 현재 KV 크기 | 포함 내용 | inline 시 제거 |
|--------|----------:|---------|-------------|
| Slot0 | ~400B | sqrtPriceX96(inline [4]u64) + 3 primitives | ~113B (ObjectInfo+Hash) |
| Balances (TokenPair) | ~400B | 2× inline [4]u64 | ~113B |
| ProtocolFees (TokenPair) | ~400B | 2× inline [4]u64 | ~113B |

**순 절감:** KV entry 오버헤드 제거 3×~113B = ~339B. 추가로 부모 Pool의 RefValue 3×~72B = ~216B 제거, 대신 inline struct 데이터 ~300B 추가.

**예상: Pool write당 ~250-400B 추가 절감.**

⚠️ 이 절감은 `shouldInlineStruct`의 `!GetIsReal()` 조건으로 인해 **새로 생성되는 struct에만** 적용된다. 기존 real sub-struct는 avl.Tree copy-on-write 시 점진적으로 마이그레이션.

### 전체 예상

| Phase | 누적 절감 (First Mint 기준) | 비고 |
|-------|------------------------:|------|
| Phase 1 (Array) | ~3,100B (10%) | 완료 |
| Phase 2 (Struct, 보수적) | ~3,500B (11%) | Slot0+Balances+ProtocolFees |
| Phase 3 (avl.Tree) | ~5,000B (16%) | co/xo 위임 필요 |

---

## 전체 경로 검증

### 경로 1: 새 Pool 생성 — inline struct + 중첩 inline array

```
pool := &Pool{slot0: Slot0{sqrtPriceX96: u256.Zero()}}

FinalizeRealmTransaction:
→ processNewCreatedMarks → incRefCreatedDescendants(Pool):
  → getChildObjects2(Pool) → StructValue Fields 순회:
    → slot0 필드: getSelfOrChildObjects(Slot0)
      → shouldInlineObject(Slot0) = shouldInlineStruct(Slot0)
        → !real, !escaped, RC≤1, fields 검사:
          → sqrtPriceX96: V=*ArrayValue → shouldInlineObject → shouldInlineArray → true
          → tick: V=nil (primitive) → OK
        → true
      → SKIP ✓
    → Slot0에 ObjectID 할당 안됨 ✓
    → sqrtPriceX96 ArrayValue도 Slot0 내부이므로 도달 안됨 ✓

→ saveUnsavedObjects → saveUnsavedObjectRecursively(Pool):
  → getUnsavedChildObjects(Pool) → inline struct/array 미포함 ✓
  → SetObject(Pool) → copyValueWithRefs(Pool):
    → Fields 순회 → refOrCopyValue(slot0TV):
      → shouldInlineObject(Slot0) = true
      → copyStructInline(Slot0):
        → Fields 순회 → refOrCopyValue(sqrtPriceTV):
          → shouldInlineObject(sqrtPriceArray) = shouldInlineArray = true
          → copyArrayInline(sqrtPriceArray) → inline 데이터
        → refOrCopyValue(tickTV) → copyValueWithRefs(nil) → nil
      → *StructValue{Fields: [inline_array, nil, ...]}
    → Pool의 amino 데이터에 Slot0 + sqrtPriceX96 직접 포함 ✓
    → 별도 KV 엔트리 없음 ✓
```

### 경로 2: 기존 Pool의 sub-struct 필드 수정 (Swap)

```
pool.slot0.sqrtPriceX96[0] = newValue

→ Assign2: pv.Base = ArrayValue, pv.TV = &ArrayValue.List[0]
→ DidUpdate(ArrayValue, nil, nil)
→ !po.GetIsReal() → shouldInlineObject(ArrayValue) → true
→ ancestor = ArrayValue.GetOwner() = Pool (real, Assign2 또는 restoreInlineOwners에서 설정)
→ MarkDirty(Pool) ✓

FinalizeRealmTransaction:
→ markDirtyAncestors(Pool) → 조상 dirty
→ saveUnsavedObjects → SetObject(Pool):
  → copyValueWithRefs → refOrCopyValue(slot0TV) → copyStructInline → copyArrayInline
  → Pool의 KV 데이터에 수정된 sqrtPriceX96 포함 ✓
```

### 경로 3: avl.Tree by-value struct — tree 변경

```
pool.ticks.Set(key, tickInfo)

avl.Tree.Set:
→ Tree.node = newRootNode (PointerValue.Base = HeapItemValue)
→ Assign2: pv.Base = Tree(StructValue, inline)
→ DidUpdate(Tree, oldNodeHeapItem, newNodeHeapItem)

DidUpdate:
→ po = Tree, !po.GetIsReal()
→ shouldInlineObject(Tree) = shouldInlineStruct(Tree) = true
→ ancestor = Tree.GetOwner() = Pool (real)
→ MarkDirty(Pool) ✓

하지만 co = newNodeHeapItem, xo = oldNodeHeapItem:
→ 현재 코드에서 po가 non-real이면 co/xo 처리 없이 return
→ newNodeHeapItem에 IncRefCount, SetOwner, MarkNewReal이 호출되지 않음!
```

**주의: 이것은 문제이다.** `DidUpdate`에서 `po`가 non-real이면 `co`와 `xo` 처리를 건너뛴다. 그러나 avl.Tree.Set은 이전 `*Node`를 삭제하고 새 `*Node`를 추가한다. 이 HeapItemValue들은 반드시 `IncRefCount`/`DecRefCount` 되어야 한다.

### 중요: avl.Tree inline 시 co/xo 처리 문제

**현재 DidUpdate 구조:**
```go
if !po.GetIsReal() {
    if shouldInlineObject(po) {
        // owner walk-up → MarkDirty(ancestor)
    }
    return  // ← co/xo 처리 없이 return!
}
// co/xo는 여기서만 처리됨
```

avl.Tree가 inline되면 `po = Tree (non-real)`, `co = newNode`, `xo = oldNode`. `return` 이후 co/xo가 처리되지 않는다.

**해결:** inline struct가 `po`일 때, co/xo 처리를 real ancestor 기준으로 수행해야 한다.

```go
if !po.GetIsReal() {
    if shouldInlineObject(po) {
        ancestor := po.GetOwner()
        for ancestor != nil && !ancestor.GetIsReal() {
            ancestor = ancestor.GetOwner()
        }
        if ancestor != nil && ancestor.GetObjectID().PkgID == rlm.ID {
            rlm.MarkDirty(ancestor)
            // Delegate co/xo handling to the real ancestor.
            if co != nil {
                co.IncRefCount()
                if co.GetRefCount() > 1 {
                    if !co.GetIsEscaped() {
                        rlm.MarkNewEscaped(co)
                    }
                }
                if co.GetIsReal() {
                    rlm.MarkDirty(co)
                } else {
                    co.SetOwner(ancestor) // owner = real ancestor
                    rlm.MarkNewReal(co)
                }
            }
            if xo != nil {
                xo.DecRefCount()
                if xo.GetRefCount() == 0 {
                    if xo.GetIsReal() {
                        rlm.MarkNewDeleted(xo)
                    }
                } else if xo.GetIsReal() {
                    rlm.MarkDirty(xo)
                }
            }
        }
    }
    return
}
```

**⚠️ 위 co/xo 위임 코드는 Phase 3 참고용이다. Phase 2에서는 사용하지 않는다.**

`shouldInlineStruct`의 보수적 PointerValue/SliceValue 필드 거부 (변경 1에 이미 통합됨)가 이 문제를 완전히 회피한다. avl.Tree inline은 co/xo 위임 로직이 검증된 후 Phase 3에서 도입.

### 보수적 접근 시 대상 구조체

| 구조체 | PointerValue 필드 | Phase 2 대상? |
|--------|-------------------|--------------|
| Slot0 | 없음 | **✓** |
| Balances (TokenPair) | 없음 | **✓** |
| ProtocolFees (TokenPair) | 없음 | **✓** |
| avl.Tree | `*Node` (PointerValue) | ✗ (Phase 3) |
| ObservationState | `map` (MapValue) | ✗ |
| ExternalIncentive | 없음 (all primitives) | **✓** |

보수적 접근으로도 Pool당 3개 StructValue (Slot0 + Balances + ProtocolFees) 제거. 각 ~160B = **~480B 추가 절감**.

---

## 최종 수정 요약 (보수적 Phase 2)

| # | 파일 | 위치 | 변경 |
|---|------|------|------|
| 1 | `realm.go` | `shouldInlineArray` 근처 | `shouldInlineObject`, `shouldInlineStruct` 추가 |
| 2 | `realm.go` | `copyArrayInline` 근처 | `copyStructInline` 추가 |
| 3 | `realm.go:1127` | `getSelfOrChildObjects` | `shouldInlineArray` → `shouldInlineObject` |
| 4 | `realm.go:1926` | `refOrCopyValue` | StructValue inline 분기 추가 |
| 5 | `ownership.go:379` | `GetFirstObject` | inline struct에 nil 반환 |
| 6 | `realm.go:212` | `DidUpdate` | `*ArrayValue` → `shouldInlineObject` |
| 7 | `values.go:233` | `Assign2` | inline struct owner 설정 + `setInlineChildOwners` |
| 8 | `store.go:576` | `restoreInlineArrayOwnersTV` | inline struct에 owner 설정 |
| 추가 | `realm.go` 또는 `values.go` | 신규 | `setInlineChildOwners` 헬퍼 |

---

## 구현 순서

### 사전 조건

- HEAD: `0f6b22adf` (또는 이후 커밋) — Phase 1 완료 상태
- `go build ./gnovm/pkg/gnolang/` 성공
- `go test -count=1 -run "TestTestdata/position_storage_poisition_lifecycle" ./gno.land/pkg/integration/ -timeout 600s` PASS

### 단계

1. `shouldInlineObject` + `shouldInlineStruct` 추가
   - **⚠️ `shouldInlineStruct`는 반드시 PointerValue/SliceValue 필드 거부 포함**
   - `shouldInlineArray`는 변경하지 않음
2. `copyStructInline` 추가
3. `getSelfOrChildObjects` — `shouldInlineArray(av)` → `shouldInlineObject(obj)` 교체
4. `refOrCopyValue` — `shouldInlineArray` 분기를 `shouldInlineObject` + type switch로 교체
5. `GetFirstObject` — `*StructValue` case에 `shouldInlineStruct` 체크 추가
6. `DidUpdate` — `*ArrayValue` + `shouldInlineArray` → `shouldInlineObject` 교체
7. `Assign2` — `*ArrayValue` + `shouldInlineArray` → `shouldInlineObject` + `setInlineChildOwners` 추가
8. `restoreInlineArrayOwnersTV` — `*StructValue` case에 `shouldInlineStruct` 체크 후 owner 설정
9. `go build` 컴파일 확인
10. 유닛 테스트 → cross-realm 테스트 → 통합 테스트
11. storage deposit 측정 (비교 대상: Phase 1 결과)

### Phase 1에서의 교훈 (구현 시 주의)

1. **`shouldInlineStruct`가 `true`를 반환하면 `shouldInlineArray`도 `true`여야 하는 것은 아님.** 각각 독립적으로 판정. `shouldInlineObject`가 디스패처.
2. **PointerValue.Base bypass (realm.go:1154-1179)는 수정 불필요.** PointerValue.Base가 StructValue인 경우는 없음 (Base는 HeapItemValue, ArrayValue, 또는 RefValue).
3. **`restoreInlineArrayOwnersTV`의 `*StructValue` case에서 `shouldInlineStruct` 체크와 기존 `!GetIsReal() && GetObjectID().IsZero()` 방어 코드를 모두 유지.** Phase 1에서 이미 defensive recurse를 하고 있으므로 inline struct owner 설정만 추가.
4. **`setInlineChildOwners`에서 owner를 real ancestor로 flat 설정.** 중첩 inline struct의 모든 자식이 최상위 real Object를 owner로 가짐. walk-up이 한 단계로 끝남.

---

## 테스트 전략

### 1단계: 컴파일 + 유닛 테스트

```bash
go build ./gnovm/pkg/gnolang/
go test ./gnovm/pkg/gnolang/ -v -count=1 -timeout 300s
```

### 2단계: Cross-realm 테스트

```bash
go test ./gnovm/pkg/gnolang/ -v -run TestFiles -count=1 2>&1 | \
    grep -E "(crossrealm|PASS|FAIL)"
```

### 3단계: 전체 통합 테스트

```bash
go test ./gno.land/pkg/integration/ -v -run TestTestdata -timeout 600s
```

### 4단계: Storage deposit 측정

```bash
GNO_REALM_STATS_LOG=/tmp/phantom_struct_stats.log \
go test -v -run "TestTestdata/position_storage_poisition_lifecycle" \
    ./gno.land/pkg/integration/ -timeout 600s 2>&1 | \
    grep -E "(STEP|STORAGE DELTA:)"
```

### 5단계: Phase 1 비교

Phase 1 결과와 비교하여 추가 절감량 측정. 기대값: Pool write당 ~480B 추가 절감 (Slot0 + Balances + ProtocolFees).

---

## 롤백 전략

`shouldInlineStruct`가 항상 false를 반환하면 Phase 1 동작과 100% 동일:

```go
func shouldInlineStruct(sv *StructValue) bool {
    return false // Phase 2 비활성화
}
```

`shouldInlineObject`는 `shouldInlineArray`로 폴백되어 Phase 1 상태가 유지된다.

---

## Phase 3 로드맵 (미래)

avl.Tree inline을 위해 `DidUpdate`에서 inline struct po의 co/xo를 real ancestor 기준으로 위임하는 로직 추가. 이 변경은 co/xo ownership 시맨틱에 깊이 관여하므로 별도 계획서 작성 예정.

---

## 부록: 현재 코드 상태 참조 (Phase 1 완료 후)

### Phase 1 커밋 히스토리

```
0f6b22adf fix(gnolang): nil guard for MapValue.List and complete recursive owner restore
dfcc6c952 fix(gnolang): restore inline array owners in MapValue and ArrayValue containers
a927792af fix(gnolang): recursive inline array owner restore and fix log category
ffa3313a6 fix(gnolang): reject non-primitive array elements in shouldInlineArray
b5c4d0e55 fix(gnolang): bypass inline array skip for PointerValue/SliceValue bases
c8401f70e refactor(gnolang): centralize inline array skip in getSelfOrChildObjects
aa637aeac fix(gnolang): context-aware inline array skip with pointer/slice safety net
de852799b feat(gnolang): restore inline array owners after deserialization
8cc9c7e1a feat(gnolang): inline arrays in serialization and assignment paths
dcf38c8c8 feat(gnolang): skip inline arrays in getSelfOrChildObjects
1d131d058 feat(gnolang): propagate dirty flag through phantom inline arrays in DidUpdate
154273327 feat(gnolang): add shouldInlineArray and copyArrayInline helpers
```

### 현재 수정 파일 위치 요약

| 파일 | 주요 함수 | 라인 (대략) |
|------|----------|-----------|
| `realm.go` | `shouldInlineArray` | ~1875 |
| `realm.go` | `copyArrayInline` | ~1908 |
| `realm.go` | `getSelfOrChildObjects` (중앙 skip) | ~1127 |
| `realm.go` | `getChildObjects` PointerValue/SliceValue bypass | ~1154-1179 |
| `realm.go` | `refOrCopyValue` (inline 분기) | ~1926 |
| `realm.go` | `DidUpdate` (phantom walk-up) | ~212-227 |
| `ownership.go` | `GetFirstObject` (*ArrayValue nil) | ~375 |
| `values.go` | `Assign2` (owner 설정) | ~233-239 |
| `store.go` | `restoreInlineArrayOwners` | ~528 |
| `store.go` | `restoreInlineArrayOwnersTV` (재귀) | ~560 |

### 테스트 명령

```bash
# 빌드
go build ./gnovm/pkg/gnolang/

# 통합 테스트 (genesis + GnoSwap lifecycle)
go test -count=1 -v -run "TestTestdata/position_storage_poisition_lifecycle" \
    ./gno.land/pkg/integration/ -timeout 600s

# Storage delta 측정
GNO_REALM_STATS_LOG=/tmp/phase2_stats.log \
go test -count=1 -v -run "TestTestdata/position_storage_poisition_lifecycle" \
    ./gno.land/pkg/integration/ -timeout 600s 2>&1 | \
    grep -E "(STEP|STORAGE DELTA:)"

# Cross-realm 테스트
go test ./gnovm/pkg/gnolang/ -v -run TestFiles -count=1 2>&1 | \
    grep -E "(crossrealm|PASS|FAIL)"

# 롤백 테스트 (shouldInlineStruct → return false)
# shouldInlineStruct 첫 줄에 `return false` 추가 후 테스트 재실행
# Phase 1과 동일한 결과가 나와야 함
```
