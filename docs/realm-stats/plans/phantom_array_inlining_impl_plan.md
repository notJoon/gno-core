# Phantom Array Inlining 구현 계획서 (v2)

**Branch:** `feat/phantom-array-inlining`
**목표:** `[N]T` (T=numeric primitive, N≤8) 형태의 고정 크기 배열을 부모 Object의 KV 엔트리에 inline 직렬화하여, 별도 KV 엔트리 생성을 제거한다.
**주 대상:** `u256.Uint = [4]uint64` (339 bytes/entry → 0 bytes, 부모에 ~40B 추가)

---

## v2 변경 이력

v1에서 누락된 치명적 결함이 발견되어 계획을 전면 수정한다.

**결함:** `pool.liquidity[0] = value` 같은 **element-level assignment**에서 `pv.Base = ArrayValue`이므로 `DidUpdate(ArrayValue, nil, nil)`이 호출된다. Phantom ArrayValue는 `GetIsReal() = false`이므로 DidUpdate가 즉시 return하여 **변경사항이 영속화되지 않는 silent data loss**가 발생한다. `u256.Uint`의 `Add`, `Sub`, `Mul` 등 모든 산술 연산이 `z[0], carry = bits.Add64(...)` 패턴으로 element-level assignment를 수행하므로 이 결함은 치명적이다.

**해결:** DidUpdate에서 non-real phantom array를 po로 받으면 **owner 체인을 따라 real ancestor까지 올라가** `MarkDirty`를 호출한다. 이를 위해 phantom array에 owner를 설정하는 경로(생성 시 + 역직렬화 후)가 필요하다.

---

## 배경: 왜 이전 inline 시도와 다른가

이전 시도 (`VM_inline_integration_blockers.md`)는 **HeapItemValue + StructValue** (avl.Tree 노드)를 inline하려다 genesis 단계에서 panic이 발생했다. 원인: 다른 Object가 inline된 Object의 ObjectID를 RefValue로 참조하고 있어, KV 스토어에서 해당 ObjectID를 찾지 못했다.

**이번 대상인 `[N]T` 고정 배열은 근본적으로 다르다:**

1. 부모 StructValue의 **필드 값**으로만 존재한다 (struct field by-value).
2. 어떤 Object도 이 ArrayValue의 ObjectID를 RefValue로 참조하지 않는다.
3. `&pool.liquidity`는 `PointerValue{Base: Pool, Index: fieldIdx}`를 생성한다 — OID 참조가 아니다.
4. `GetObject(arrayOID)`가 호출될 경로가 **존재하지 않는다**.
5. 따라서 genesis blocker가 **적용되지 않는다**.

---

## 수정 지점 요약 (7곳)

| # | 파일 | 함수/위치 | 변경 내용 |
|---|------|----------|----------|
| 1 | `realm.go` | 신규 함수 | `shouldInlineArray` — inline 대상 판별 |
| 2 | `realm.go` | 신규 함수 | `copyArrayInline` — 직렬화용 inline 복사 |
| 3 | `realm.go:1111` | `getSelfOrChildObjects` | inline 배열을 자식 Object 열거에서 제외 |
| 4 | `realm.go:1830` | `refOrCopyValue` | inline 배열을 RefValue 대신 직접 데이터로 직렬화 |
| 5 | `ownership.go:375` | `GetFirstObject` | inline 배열에 대해 nil 반환 (co/xo 추적 차단) |
| 6 | `realm.go:209` | `DidUpdate` | non-real inline array가 po일 때 real ancestor 탐색 → MarkDirty |
| 7 | `values.go:232` | `Assign2` | 할당 후 inline array에 owner 설정 |

**추가로 필요한 작업:**
- `store.go`: `loadObjectSafe` 또는 post-load hook에서 inline array의 owner 복원

---

## 파일별 상세 변경사항

### 파일 1: `gnovm/pkg/gnolang/realm.go`

#### 변경 1: `shouldInlineArray` 함수 추가

**위치:** `refOrCopyValue` 함수 근처 (line ~1830 부근)에 새 함수 추가

```go
// shouldInlineArray returns true if the given ArrayValue should be
// serialized inline within its parent Object rather than as a
// separate KV entry.
//
// Criteria:
//   - NOT already persisted (real): existing KV entries are preserved
//     for backwards compatibility. avl.Tree copy-on-write naturally
//     migrates old entries to inline over time.
//   - NOT escaped or multi-referenced: shared Objects need independent
//     KV entries for correct Merkle proof and IAVL tracking.
//   - Small (≤ 8 elements): prevents parent KV entry bloat.
//   - All elements are primitives (non-Object): child Objects need
//     independent ownership tracking.
func shouldInlineArray(av *ArrayValue) bool {
    // Already-persisted arrays retain separate KV entries.
    if av.GetIsReal() {
        return false
    }
    if av.GetIsEscaped() || av.GetIsNewEscaped() {
        return false
    }
    if av.GetRefCount() > 1 {
        return false
    }
    // Byte arrays using compact Data encoding.
    if av.Data != nil {
        return len(av.Data) <= 256
    }
    // Only inline small arrays (covers [4]uint64 = u256.Uint).
    if len(av.List) > 8 {
        return false
    }
    // Every element must be a non-Object primitive.
    for _, tv := range av.List {
        if _, isObj := tv.V.(Object); isObj {
            return false
        }
    }
    return true
}
```

**`GetIsReal()` 체크가 필수인 이유 (마이그레이션 안전성):**

기존에 별도 KV 엔트리로 저장된 real ArrayValue를 갑자기 phantom으로 만들면:
- `GetFirstObject` → nil → `DidUpdate`에서 co/xo로 추적 안됨
- 기존 KV 엔트리의 `DecRefCount` → `MarkNewDeleted`가 호출되지 않음
- **기존 KV 엔트리가 영원히 삭제되지 않는 storage leak 발생**

`!GetIsReal()` 조건으로 **새로 생성되는 배열만 inline 대상**으로 하면, avl.Tree의 copy-on-write 덕분에 기존 배열은 삭제되고 새 배열이 inline으로 생성되어 **점진적 마이그레이션**이 자동으로 이루어진다.

#### 변경 2: `copyArrayInline` 함수 추가

**위치:** `shouldInlineArray` 근처에 추가

```go
// copyArrayInline creates a serialization-ready copy of an inline
// ArrayValue. The copy has ZERO ObjectInfo — the signal that this
// array is inlined in the parent and has no independent KV entry.
func copyArrayInline(av *ArrayValue) *ArrayValue {
    if av.Data != nil {
        return &ArrayValue{
            // ObjectInfo intentionally zero.
            Data: cp(av.Data),
        }
    }
    list := make([]TypedValue, len(av.List))
    for i, etv := range av.List {
        // Elements are guaranteed non-Object by shouldInlineArray.
        list[i] = refOrCopyValue(etv)
    }
    return &ArrayValue{
        // ObjectInfo intentionally zero.
        List: list,
    }
}
```

**ObjectInfo를 zero로 두는 이유:**

`copyValueWithRefs`에서 직렬화용 복사본은 원래 ObjectInfo.Copy()를 포함하지만, inline 배열은 독립 identity가 없다. amino 역직렬화 후 zero ObjectInfo를 가진 ArrayValue가 부모의 필드로 복원되며, `shouldInlineArray`는 이를 `!GetIsReal()` (ID=zero)로 인식하여 계속 inline으로 처리한다.

#### 변경 3: `getSelfOrChildObjects` 수정

**위치:** `realm.go:1111-1119`

**현재:**
```go
func getSelfOrChildObjects(val Value, more []Value) []Value {
    if _, ok := val.(RefValue); ok {
        return append(more, val)
    } else if _, ok := val.(Object); ok {
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
        if av, isArr := obj.(*ArrayValue); isArr && shouldInlineArray(av) {
            return more // skip — inlined in parent
        }
        return append(more, val)
    } else {
        return getChildObjects(val, more)
    }
}
```

**영향받는 경로:**

| 호출자 | 효과 |
|-------|------|
| `incRefCreatedDescendants` → `getChildObjects2` | inline 배열에 ObjectID 할당 안됨 |
| `decRefDeletedDescendants` → `getChildObjects2` | 부모 삭제 시 inline 배열은 별도 삭제 불필요 |
| `getUnsavedChildObjects` | inline 배열은 별도 저장 대상에서 제외 |
| `markDirtyAncestors`에서 간접 사용 없음 | 해당 없음 |

**주의: `getChildObjects`의 `*ArrayValue` case (`realm.go:1141-1145`)**

```go
case *ArrayValue:
    for _, ctv := range cv.List {
        more = getSelfOrChildObjects(ctv.V, more)
    }
    return more
```

이 case는 ArrayValue가 **이미 Object로 등록된 후** 그 내부 원소를 순회하는 용도이다. inline 배열은 `getSelfOrChildObjects`에서 skip되므로 이 case에 도달하지 않는다. **수정 불필요.**

#### 변경 4: `refOrCopyValue` 수정

**위치:** `realm.go:1830-1841`

**현재:**
```go
func refOrCopyValue(tv TypedValue) TypedValue {
    if tv.T != nil {
        tv.T = refOrCopyType(tv.T)
    }
    if obj, ok := tv.V.(Object); ok {
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

**직렬화 결과 비교:**

현재: 부모 StructValue의 필드 → `RefValue{ObjectID: "pkg:42", Hash: [20]byte}` (~72 bytes)
수정후: 부모 StructValue의 필드 → `*ArrayValue{List: [4]TypedValue{uint64, uint64, uint64, uint64}}` (~40-50 bytes)

**amino 호환성:** `TypedValue.V`의 concrete type에 따라 amino가 자동 직렬화한다. `RefValue`이든 `*ArrayValue`이든 amino registered type이므로 **포맷 변경이나 등록 변경 불필요.**

#### 변경 5: `DidUpdate` 수정 — element-level dirty 전파

**위치:** `realm.go:209-211`

**현재:**
```go
if po == nil || !po.GetIsReal() {
    return // do nothing.
}
```

**수정:**
```go
if po == nil {
    return
}
if !po.GetIsReal() {
    // Phantom inline array: walk up ownership chain to the
    // nearest real ancestor and mark it dirty. This handles
    // element-level assignment (e.g. z[0] = value in u256.Add)
    // where pv.Base is the ArrayValue, not the parent struct.
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

**이 수정이 14개+ 호출 사이트를 한 곳에서 처리하는 이유:**

`DidUpdate`는 모든 dirty 전파의 진입점이다:
- `Assign2` (`values.go:232`)
- `doOpAddAssign` 외 11개 compound assign (`op_assign.go:49-189`)
- `doOpInc`, `doOpDec` (`op_inc_dec.go:81, 119`)

이 모든 곳에서 `rlm.DidUpdate(base.(Object), ...)` 또는 `m.Realm.DidUpdate(lv.Base.(Object), nil, nil)`을 호출한다. `DidUpdate` 내부에서 phantom array를 처리하면 **호출 사이트 개별 수정이 불필요**하다.

**owner 체인 walk-up이 정상 동작하는 이유:**

phantom 배열의 owner는 부모 StructValue (예: TickInfo, Pool)이다. 부모 StructValue는 정상 Object(real)이므로 한 단계 walk-up으로 real ancestor에 도달한다. 만약 부모도 non-real이면 (예: transient object), `ancestor.GetIsReal() = false` → 계속 올라감 → 최종적으로 real ancestor에 도달하거나 nil → return. transient 객체의 변경은 원래 추적 안되므로 정상.

**PkgID 검사 (`ancestor.GetObjectID().PkgID == rlm.ID`):**

cross-realm 보호를 유지한다. ancestor가 다른 realm에 속하면 MarkDirty하지 않는다 (기존 동작과 동일).

---

### 파일 2: `gnovm/pkg/gnolang/ownership.go`

#### 변경 6: `GetFirstObject` 수정

**위치:** `ownership.go:375-376`

**현재:**
```go
case *ArrayValue:
    return cv
```

**수정:**
```go
case *ArrayValue:
    if shouldInlineArray(cv) {
        return nil
    }
    return cv
```

**이 수정이 필수인 이유:**

`GetFirstObject`는 `Assign2`에서 co/xo를 추출하는데 사용된다:

```go
oo1 := pv.TV.GetFirstObject(store)  // old value
pv.TV.Assign(alloc, tv2, cu)
oo2 := pv.TV.GetFirstObject(store)  // new value
rlm.DidUpdate(pv.Base.(Object), oo1, oo2)
```

만약 `GetFirstObject`가 inline ArrayValue를 반환하면:
- `co = newArrayValue` → `DidUpdate`에서 `co.SetOwner(po)`, `MarkNewReal(co)` → **ObjectID 할당** → 별도 KV 엔트리 생성
- phantom inlining이 무효화됨

nil을 반환하면:
- `DidUpdate(parent, nil, nil)` → `MarkDirty(parent)` only → 부모 재직렬화 시 inline 배열 데이터 포함
- ObjectID 할당 없음, 별도 KV 엔트리 없음 ✓

---

### 파일 3: `gnovm/pkg/gnolang/values.go`

#### 변경 7: `Assign2`에서 inline array에 owner 설정

**위치:** `values.go:217-236`

**현재:**
```go
func (pv PointerValue) Assign2(alloc *Allocator, store Store, rlm *Realm, tv2 TypedValue, cu bool) {
    if pv.TV.T == DataByteType {
        pv.TV.SetDataByte(tv2.GetUint8())
        return
    }
    if rlm != nil {
        if debug && pv.Base == nil {
            panic("expected non-nil base for assignment")
        }
        oo1 := pv.TV.GetFirstObject(store)
        pv.TV.Assign(alloc, tv2, cu)
        oo2 := pv.TV.GetFirstObject(store)
        rlm.DidUpdate(pv.Base.(Object), oo1, oo2)
    } else {
        pv.TV.Assign(alloc, tv2, cu)
    }
}
```

**수정:**
```go
func (pv PointerValue) Assign2(alloc *Allocator, store Store, rlm *Realm, tv2 TypedValue, cu bool) {
    if pv.TV.T == DataByteType {
        pv.TV.SetDataByte(tv2.GetUint8())
        return
    }
    if rlm != nil {
        if debug && pv.Base == nil {
            panic("expected non-nil base for assignment")
        }
        oo1 := pv.TV.GetFirstObject(store)
        pv.TV.Assign(alloc, tv2, cu)
        oo2 := pv.TV.GetFirstObject(store)
        rlm.DidUpdate(pv.Base.(Object), oo1, oo2)
        // Set owner on newly-assigned inline arrays so that
        // element-level DidUpdate can walk up to the real ancestor.
        if baseObj, ok := pv.Base.(Object); ok && baseObj.GetIsReal() {
            if av, ok := pv.TV.V.(*ArrayValue); ok && shouldInlineArray(av) {
                av.SetOwner(baseObj)
            }
        }
    } else {
        pv.TV.Assign(alloc, tv2, cu)
    }
}
```

**Owner 설정 흐름:**

1. `pool.liquidity = u256.Zero()` → Assign2 호출
2. `pv.Base = Pool(StructValue, real)`, `pv.TV = &Pool.Fields[liquidityIdx]`
3. 할당 후 `pv.TV.V = newArrayValue`
4. `shouldInlineArray(newArrayValue)` → true (not real, not escaped, ≤8 elements, all primitives)
5. `newArrayValue.SetOwner(Pool)` → **owner 설정 완료**
6. 이후 `pool.liquidity[0] = value` → `DidUpdate(ArrayValue, nil, nil)` → `av.GetOwner()` = Pool → `MarkDirty(Pool)` ✓

**baseObj.GetIsReal() 체크:**

non-real 부모 (transient struct)에 할당되는 경우 owner를 설정하지 않는다. transient 객체의 변경은 원래 추적 안되므로 불필요. 나중에 부모가 real이 되면 `incRefCreatedDescendants`에서 자식을 순회하는데, 이때 inline 배열은 skip된다. 하지만 `Assign2`가 부모를 real로 만든 후 다시 호출될 때 owner가 설정된다.

---

### 파일 4: `gnovm/pkg/gnolang/store.go`

#### 변경 8: 역직렬화 후 inline array owner 복원

**위치:** `store.go:459-511` `loadObjectSafe` 함수 끝부분

amino 역직렬화 후 inline ArrayValue는 ObjectInfo가 zero (owner=nil)이다. VM이 element-level assignment을 하기 전에 owner가 복원되어야 한다.

```go
// restoreInlineArrayOwners sets the runtime owner field for inline
// ArrayValues deserialized as part of a parent Object. This enables
// DidUpdate to walk up to the real ancestor when array elements are
// modified (e.g. z[0] = value in u256 arithmetic).
func restoreInlineArrayOwners(parent Object) {
    switch pv := parent.(type) {
    case *StructValue:
        for i := range pv.Fields {
            if av, ok := pv.Fields[i].V.(*ArrayValue); ok && shouldInlineArray(av) {
                av.SetOwner(parent)
            }
        }
    case *Block:
        for i := range pv.Values {
            if av, ok := pv.Values[i].V.(*ArrayValue); ok && shouldInlineArray(av) {
                av.SetOwner(parent)
            }
        }
    case *HeapItemValue:
        if av, ok := pv.Value.V.(*ArrayValue); ok && shouldInlineArray(av) {
            av.SetOwner(parent)
        }
    }
}
```

**호출 위치:** `loadObjectSafe`에서 `fillTypesOfValue` 호출 후:

```go
func (ds *defaultStore) loadObjectSafe(oid ObjectID) Object {
    // ... existing load logic ...
    // ... amino decode ...
    // ... fillTypesOfValue ...

    // Restore owner for inline arrays.
    restoreInlineArrayOwners(oo)

    return oo
}
```

**`shouldInlineArray` 판정이 역직렬화 후에도 정확한 이유:**

inline ArrayValue는 zero ObjectInfo를 가진다:
- `GetIsReal()` = false (ID=zero) → 첫 번째 조건 통과
- `GetIsEscaped()` = false → 통과
- `GetRefCount()` = 0 (≤1) → 통과
- elements are primitives (uint64 TypedValues) → 통과
- 결과: `shouldInlineArray` = true ✓

**기존 real ArrayValue (RefValue로 저장된):**

기존 방식으로 저장된 ArrayValue는 부모의 필드에 `RefValue`로 존재한다. `fillValueTV`에서 `store.GetObject(refOID)`로 로드된다. 로드된 ArrayValue는 `GetIsReal() = true`이므로 `shouldInlineArray = false` → `restoreInlineArrayOwners`에서 skip → 기존 동작 유지.

---

## 수정하지 않는 파일들

### `op_assign.go` — 수정 불필요

12개 compound assign 함수는 모두 `m.Realm.DidUpdate(lv.Base.(Object), nil, nil)`을 호출한다. **DidUpdate 내부에서 phantom array를 처리**하므로 개별 수정 불필요.

검증: `lv.Base`가 inline ArrayValue일 때:
1. `DidUpdate(ArrayValue, nil, nil)` 호출
2. `!po.GetIsReal()` → 신규 phantom array 처리 로직 진입
3. `av.GetOwner()` → parent StructValue (Assign2에서 설정됨)
4. `MarkDirty(parent)` ✓

### `op_inc_dec.go` — 수정 불필요

동일한 이유. `m.Realm.DidUpdate(pv.Base.(Object), nil, nil)` 호출.

### `copyValueWithRefs`의 `*ArrayValue` case — 수정 불필요

이 case는 ArrayValue가 **독립 Object로 저장**될 때 사용된다. inline 배열은 `refOrCopyValue`에서 `copyArrayInline`으로 처리되므로 이 case에 도달하지 않는다.

### `incRefCreatedDescendants` — 수정 불필요

inline 배열은 `getChildObjects2` → `getSelfOrChildObjects`에서 skip되므로 이 함수에서 순회되지 않는다. ObjectID 할당, RefCount 증가, SetOwner 모두 발생하지 않는다. Owner는 `Assign2`에서 별도 설정한다.

### `decRefDeletedDescendants` — 수정 불필요

inline 배열은 `getChildObjects2`에서 skip되므로 삭제 cascade에서 순회되지 않는다. inline 배열은 별도 KV 엔트리가 없으므로 삭제할 것도 없다. 부모 Object가 삭제되면 부모의 KV 엔트리가 제거되고 inline 배열 데이터도 함께 사라진다.

---

## amino 직렬화/역직렬화 호환성

### 직렬화 (저장)

**현재:** 부모 StructValue 필드 → `refOrCopyValue` → `toRefValue(ArrayValue)` → `RefValue{OID, Hash}` (amino에 RefValue로 인코딩)

**수정후:** 부모 StructValue 필드 → `refOrCopyValue` → `copyArrayInline(ArrayValue)` → `*ArrayValue{List: [...]}` (amino에 ArrayValue로 인코딩)

amino는 `TypedValue.V`의 concrete type으로 직렬화한다. `RefValue`와 `*ArrayValue`는 모두 amino registered type. **포맷 변경, 등록 변경 불필요.**

### 역직렬화 (로드)

**기존 데이터 (RefValue):** 부모 로드 → 필드에 `RefValue{OID}` → `fillValueTV` → `store.GetObject(OID)` → ArrayValue 로드. 정상 동작.

**신규 데이터 (inline):** 부모 로드 → 필드에 `*ArrayValue{List: [...]}` → 이미 ArrayValue가 있으므로 `fillValueTV`에서 `default` case → 아무것도 안함. **추가 처리 불필요.** `restoreInlineArrayOwners`에서 owner만 복원하면 됨.

### 혼합 환경

같은 StructValue 내에서 일부 필드는 RefValue (기존 real 배열), 일부는 inline ArrayValue (새로 생성된 배열)가 공존할 수 있다. amino는 필드별로 concrete type을 인코딩하므로 문제없다.

---

## 전체 경로 검증

### 경로 1: 새 Pool 생성 + u256 필드 할당

```
pool := &Pool{liquidity: u256.Zero()}
→ Pool.Fields[liquidityIdx].V = *ArrayValue{List: [4]TypedValue{0,0,0,0}}

Pool이 package-level 변수 또는 avl.Tree에 연결됨:
→ Assign2(pv.Base=Block/TreeNode, Pool 관련)
→ DidUpdate → MarkNewReal(Pool) → newCreated에 추가

FinalizeRealmTransaction:
→ processNewCreatedMarks → incRefCreatedDescendants(Pool):
  → getChildObjects2(Pool) → StructValue Fields 순회
  → liquidity 필드: getSelfOrChildObjects(ArrayValue) → shouldInlineArray=true → SKIP
  → ArrayValue에 ObjectID 할당 안됨 ✓
  → ArrayValue는 created 목록에 추가 안됨 ✓

→ saveUnsavedObjects → saveUnsavedObjectRecursively(Pool):
  → getUnsavedChildObjects(Pool) → inline array 미포함 ✓
  → SetObject(Pool) → copyValueWithRefs(Pool):
    → Fields 순회 → refOrCopyValue(liquidityTV):
      → shouldInlineArray=true → copyArrayInline → *ArrayValue{List:[...]}
    → Pool의 amino 데이터에 배열 데이터 직접 포함 ✓
    → 배열 별도 KV 엔트리 없음 ✓
```

**하지만 owner 설정은?** 이 경로에서 Assign2가 호출되는 시점에 Pool 자체가 아직 real이 아닐 수 있다. `baseObj.GetIsReal() = false` → owner 설정 안됨.

이 문제는 **`restoreInlineArrayOwners`에서 해결**된다. Pool이 저장된 후 다음 트랜잭션에서 로드될 때:
1. `loadObjectSafe` → amino decode → Pool StructValue 복원
2. `restoreInlineArrayOwners(Pool)` → inline ArrayValue에 `SetOwner(Pool)` ✓
3. 이후 element-level assignment 정상 동작

**첫 트랜잭션 내에서의 element-level assignment:**

Pool 생성 + `pool.liquidity.Add(x, y)` 가 같은 트랜잭션에서 발생하면:
- Pool이 아직 real이 아님
- ArrayValue도 아직 real이 아님
- `DidUpdate(ArrayValue, nil, nil)` → `!po.GetIsReal()` → phantom handler → `av.GetOwner() = nil` → 아무것도 안함

이것은 **현재 시스템에서도 동일한 동작**이다! 현재 시스템에서도 non-real Object에 대한 DidUpdate는 즉시 return한다 (line 209). 첫 트랜잭션에서 non-real 객체에 대한 element-level 변경은 부모가 real이 될 때 함께 영속화된다 (processNewCreatedMarks에서 부모와 함께 저장). **기존 동작과 동일하므로 문제없다.**

### 경로 2: 기존 Pool의 u256 필드 산술 연산 (Swap)

```
pool.liquidity.Add(x, y)  // z.arr[0], carry = bits.Add64(...)

→ z = &pool.liquidity → PointerValue{Base: Pool, Index: liquidityIdx}
→ z[0] = value → PointerValue{Base: ArrayValue, Index: 0}
→ Assign2 → DidUpdate(ArrayValue, nil, nil)

DidUpdate:
→ po = ArrayValue, !po.GetIsReal()
→ shouldInlineArray(av) → true (not real, not escaped, ≤8, all primitives)
→ ancestor = av.GetOwner()  ← Assign2 또는 restoreInlineArrayOwners에서 설정됨
→ ancestor = Pool(StructValue, real)
→ ancestor.GetObjectID().PkgID == rlm.ID ← cross-realm 검사 통과
→ MarkDirty(Pool) ✓

FinalizeRealmTransaction:
→ markDirtyAncestors(Pool) → Pool의 조상들도 dirty
→ saveUnsavedObjects → SetObject(Pool):
  → copyValueWithRefs → refOrCopyValue(liquidityTV) → copyArrayInline
  → Pool의 amino 데이터에 수정된 배열 데이터 포함 ✓
```

### 경로 3: 중첩 구조체 내 inline array

```
pool.balances.token0 = u256.FromUint64(100)

구조:
  Pool (StructValue, real)
    → balances (Balances = StructValue, real, 별도 KV 엔트리)
      → token0 (u256.Uint = [4]uint64 ArrayValue, inline)

token0[0] = value:
→ DidUpdate(ArrayValue, nil, nil)
→ av.GetOwner() = Balances(StructValue, real)
→ MarkDirty(Balances)
→ markDirtyAncestors: Balances → Pool → ... → Package
→ Balances 재직렬화 시 token0 inline 데이터 포함 ✓
```

### 경로 4: Genesis 패키지 배포

```
realm A 배포:
→ Pool 생성 + u256 필드
→ FinalizeRealmTransaction → inline 배열은 Pool의 KV에 포함

realm B가 realm A를 import:
→ store.GetPackage("gno.land/r/gnoswap/pool")
→ PackageValue → Block → Pool StructValue 로드
→ Pool의 필드에 *ArrayValue가 직접 있음 (RefValue 아님)
→ fillValueTV: ArrayValue는 RefValue가 아니므로 GetObject 호출 없음
→ restoreInlineArrayOwners(Pool) → inline array에 owner 설정
→ ✅ genesis blocker 없음
```

### 경로 5: avl.Tree copy-on-write 마이그레이션

```
기존 TickInfo (real, 기존 방식):
  TickInfo(StructValue, real) → liquidityGross(ArrayValue, real, 별도 KV)

avl.Tree.Set(tickKey, newTickInfo):
→ 기존 TickInfo 삭제: DecRefCount → MarkNewDeleted
  → 기존 ArrayValue도 자식으로 순회됨 (real이므로 shouldInline=false)
  → DecRefCount → MarkNewDeleted → KV에서 삭제 ✓

→ 새 TickInfo 생성: IncRefCount → MarkNewReal
  → 새 ArrayValue: shouldInlineArray=true (not real)
  → getSelfOrChildObjects에서 skip → ObjectID 미할당
  → 새 TickInfo 저장 시 ArrayValue inline ✓

결과: 기존 배열 KV 삭제 + 새 배열 inline = 점진적 마이그레이션 ✓
```

---

## Edge Cases

### Edge Case 1: &pool.liquidity 포인터 저장

```go
var cachedLiquidity *u256.Uint = pool.Liquidity()  // &pool.liquidity
```

GnoVM에서 `&pool.liquidity` → `PointerValue{Base: Pool, Index: liquidityIdx}`.
PointerValue는 Object가 아니므로 직렬화 시 `copyValueWithRefs`에서:
```go
case PointerValue:
    return PointerValue{
        Base:  toRefValue(cv.Base),  // Pool의 RefValue
        Index: cv.Index,             // field index
    }
```
Pool의 RefValue(OID)로 참조. ArrayValue의 OID는 사용되지 않음. ✓

역직렬화 시 `fillValueTV`에서 PointerValue.Base = RefValue → GetObject(Pool OID) → Pool 로드 → cv.Index로 필드 접근 → inline ArrayValue 획득. ✓

### Edge Case 2: escape 시나리오

두 곳에서 같은 ArrayValue를 참조하는 경우:

```go
var a, b *[4]uint64
x := [4]uint64{1,2,3,4}
a = &x
b = &x  // HeapItemValue escape
```

`a`와 `b`는 같은 HeapItemValue를 참조. HeapItemValue의 RefCount > 1 → escaped. 하지만 내부 ArrayValue의 RefCount는 1 (HeapItemValue가 유일 소유자).

`shouldInlineArray(ArrayValue)`:
- `GetIsReal()` = false (새로 생성) → 통과
- `GetRefCount()` = 1 → 통과
- `GetIsEscaped()` = false → 통과

문제: HeapItemValue가 escaped이면, 해당 HeapItemValue의 저장 방식이 IAVL로 바뀐다. 내부 ArrayValue를 inline하더라도 HeapItemValue 자체는 별도 저장된다. inline ArrayValue는 HeapItemValue의 KV 엔트리에 포함된다. 이것은 정상 동작.

하지만 ArrayValue 자체가 두 곳에서 직접 참조되는 경우 (RefCount > 1 또는 IsEscaped):
- `shouldInlineArray` → false → 기존 경로 유지 ✓

### Edge Case 3: debugRealm assertions

- `ensureUniq(rlm.newCreated)`: inline 배열은 newCreated에 추가 안됨 → uniq 검사에 영향 없음 ✓
- `MarkNewReal` assertion (`owner must be real`): inline 배열에 호출 안됨 ✓
- `MarkDirty` assertion: inline 배열에 직접 호출 안됨. ancestor MarkDirty는 정상 경로 ✓

---

## 테스트 전략

### 1단계: 컴파일 + 유닛 테스트

```bash
go build ./gnovm/pkg/gnolang/
go test ./gnovm/pkg/gnolang/ -v -count=1 -timeout 300s
```

### 2단계: Cross-realm 테스트 (genesis blocker 부재 확인)

```bash
go test ./gnovm/pkg/gnolang/ -v -run TestFiles -count=1 \
    -run "crossrealm"
```

### 3단계: 전체 통합 테스트 (genesis 포함)

```bash
go test ./gno.land/pkg/integration/ -v -run TestTestdata -timeout 600s
```

### 4단계: Storage deposit 측정

```bash
GNO_REALM_STATS_LOG=/tmp/phantom_stats.log \
go test -v -run "TestTestdata/position_storage_poisition_lifecycle" \
    ./gno.land/pkg/integration/ -timeout 600s 2>&1 | \
    grep -E "(STEP|STORAGE DELTA:)"
```

### 5단계: Element-level assignment 검증 (가장 중요)

**이 테스트가 반드시 필요하다.** u256 산술 연산 결과가 재시작 후에도 유지되는지 확인:

1. Pool 생성 + Mint (u256 값 설정)
2. Swap 실행 (u256.Add/Sub 등 element-level 연산)
3. 결과 값 확인
4. **노드 재시작** (gnoland restart)
5. 동일한 값이 유지되는지 확인

기존 `position_storage_poisition_lifecycle.txtar`에 restart + query step을 추가하거나, 별도 txtar 테스트 작성.

---

## 구현 순서

1. `realm.go`에 `shouldInlineArray` + `copyArrayInline` 추가
2. `realm.go`의 `DidUpdate` 수정 (phantom array dirty 전파)
3. `realm.go`의 `getSelfOrChildObjects` 수정
4. `realm.go`의 `refOrCopyValue` 수정
5. `ownership.go`의 `GetFirstObject` 수정
6. `values.go`의 `Assign2` 수정 (owner 설정)
7. `store.go`에 `restoreInlineArrayOwners` 추가 + `loadObjectSafe` 호출
8. `go build` 컴파일 확인
9. 유닛 테스트 → cross-realm 테스트 → 통합 테스트
10. storage deposit 측정
11. element-level assignment + restart 검증 테스트

---

## 롤백 전략

모든 변경은 `shouldInlineArray`가 `false`를 반환하면 기존 동작과 100% 동일하다:

```go
func shouldInlineArray(av *ArrayValue) bool {
    return false // 전체 기능 비활성화
}
```

- DidUpdate: phantom handler 진입 안함 → 기존 경로
- getSelfOrChildObjects: skip 안함 → 기존 경로
- refOrCopyValue: copyArrayInline 안함 → toRefValue → 기존 경로
- GetFirstObject: nil 안함 → 기존 경로
- Assign2: owner 설정 안함 (shouldInlineArray=false)
- restoreInlineArrayOwners: 아무것도 안함 (shouldInlineArray=false)

기존 KV 데이터와의 호환: 방법 B (GetIsReal 체크) 덕분에 기존 real 배열은 전혀 영향 없음. 새로 inline된 배열도 amino registered type이므로 역직렬화 정상.
