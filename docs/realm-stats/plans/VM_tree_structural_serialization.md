# VM 레벨 최적화: Tree 구조적 직렬화 분석

**목표:** avl.Tree 노드의 per-Object 오버헤드 (~300-500 bytes/node) 제거
**예상 효과:** Mint 32KB → ~10-15KB (-50~70%), Swap 5KB → ~2KB (-60%)

---

## 현재 모델: Node-per-Object

### Object 생성 경로

avl.Tree의 `Set(key, value)` 호출 시:

```
1. Go/Gno 런타임: *Node 생성 (heap allocation)
2. VM 소유권 추적: HeapItemValue 생성 → Object로 등록
3. Node StructValue도 Object로 등록
4. FinalizeRealmTransaction:
   a. processNewCreatedMarks → ObjectID 할당
   b. markDirtyAncestors → root까지 모든 조상 dirty 표시
   c. saveUnsavedObjects → 각 Object를 개별 KV entry로 저장
```

### 직렬화 경로 (store.go:619-714)

```go
func (ds *defaultStore) SetObject(oo Object) int64 {
    o2 := copyValueWithRefs(oo)       // 자식 Object → RefValue 교체
    bz := amino.MustMarshalAny(o2)    // amino 직렬화
    hash := HashBytes(bz)             // SHA256 해시
    // KV에 저장: key="oid:X", value=hash(32)+bz(N)
    ds.baseStore.Set(key, hashbz)
}
```

### copyValueWithRefs의 핵심 분기 (realm.go:1776-1787)

```go
func refOrCopyValue(tv TypedValue) TypedValue {
    if obj, ok := tv.V.(Object); ok {
        tv.V = toRefValue(obj)    // ← Object → RefValue(ObjectID+Hash, 72 bytes)
        return tv
    } else {
        tv.V = copyValueWithRefs(tv.V)  // ← 비-Object는 inline 복사
        return tv
    }
}
```

**이 분기가 핵심.** `tv.V`가 Object이면 무조건 RefValue로 교체. avl.Node가 Object이므로 항상 별도 KV entry로 저장됨.

### avl.Node의 Object 체인 (5개 entry tree 예시)

```
Tree StructValue (Object #1)
  └─ node field: HeapItemValue (Object #2) → StructValue (Object #3, root Node)
       ├─ key: "c" (inline)
       ├─ value: TickInfo (inline if value type)
       ├─ leftNode: HeapItemValue (Object #4) → StructValue (Object #5, left Node)
       │    ├─ key: "a"
       │    └─ leftNode: HeapItemValue (Object #6) → StructValue (Object #7)
       └─ rightNode: HeapItemValue (Object #8) → StructValue (Object #9, right Node)
            ├─ key: "e"
            └─ rightNode: HeapItemValue (Object #10) → StructValue (Object #11)

저장 결과: 11개 Object × (112B ObjectInfo + ~60B data + 32B hash) = ~2,200+ bytes
실제 유효 데이터: 5 entries × ~50B = ~250 bytes
효율: ~11%
```

---

## 제안: Owned Object Inlining

### 핵심 아이디어

`refOrCopyValue`에서 **작고, 소유된, escape되지 않은 Object**를 RefValue로 교체하지 않고 inline으로 직렬화.

```
현재:  Object → RefValue(ObjectID, Hash)  → 별도 KV entry (112B+ overhead)
개선:  Object → InlineValue(data)         → 부모 KV entry에 포함 (0 overhead)
```

### 수정 대상: `refOrCopyValue` (realm.go:1776)

```go
// 현재
func refOrCopyValue(tv TypedValue) TypedValue {
    if obj, ok := tv.V.(Object); ok {
        tv.V = toRefValue(obj)     // 항상 RefValue
        return tv
    }
    tv.V = copyValueWithRefs(tv.V)
    return tv
}

// 개선
func refOrCopyValue(tv TypedValue) TypedValue {
    if obj, ok := tv.V.(Object); ok {
        oi := obj.GetObjectInfo()

        // 조건: 소유됨 + escape 안됨 + 작음
        if oi.RefCount <= 1 && !oi.IsEscaped && !oi.isNewEscaped &&
           estimateInlineSize(obj) <= INLINE_THRESHOLD {
            // RefValue 대신 inline 직렬화
            tv.V = copyValueInline(obj)  // 재귀적으로 자식도 inline
            return tv
        }

        tv.V = toRefValue(obj)  // 기존 경로
        return tv
    }
    tv.V = copyValueWithRefs(tv.V)
    return tv
}
```

### 수정 대상: `getSelfOrChildObjects` (realm.go:1057)

inline된 Object는 부모의 일부로 취급되므로, 자식 열거에서 제외해야 함:

```go
// 현재
func getSelfOrChildObjects(val Value, more []Value) []Value {
    if _, ok := val.(Object); ok {
        return append(more, val)  // 모든 Object를 별도 등록
    }
    return getChildObjects(val, more)
}

// 개선
func getSelfOrChildObjects(val Value, more []Value) []Value {
    if obj, ok := val.(Object); ok {
        if shouldInline(obj) {
            // inline 대상은 자식으로 재귀 탐색 (별도 Object로 등록하지 않음)
            return getChildObjects(val, more)
        }
        return append(more, val)
    }
    return getChildObjects(val, more)
}
```

### 수정 대상: `saveUnsavedObjects` (realm.go:794)

inline된 Object는 별도 저장하지 않음. 부모 Object 저장 시 자동 포함됨.

```go
func (rlm *Realm) saveUnsavedObjects(store Store) {
    for _, uo := range rlm.created {
        if shouldInline(uo) {
            continue  // 부모에 포함됨, 별도 저장 skip
        }
        rlm.saveObject(store, uo)
    }
    // updated도 동일
}
```

### 수정 대상: `SetObject` → deserialization 대응 (store.go:619)

inline 데이터를 읽을 때 Object로 복원해야 함:

```go
// GetObject에서 inline된 자식을 Object로 복원
func restoreInlinedObjects(parent Object, val Value) {
    // amino 디코딩 후, InlineValue를 만나면 Object로 재구성
    // ObjectID는 부모의 ID + field path로 결정론적 생성
    // 또는 별도 InlineValue marker를 사용
}
```

---

## 직렬화 포맷 변경

### 현재 포맷

```
Parent Object KV entry:
  key:   "oid:PARENT_ID"
  value: hash(32) + amino{
    ObjectInfo{ID, Hash, OwnerID, ModTime, RefCount}  // ~80 bytes
    StructValue{
      Fields: [
        {T: avl.Tree, V: RefValue{ObjectID: CHILD_ID, Hash: ...}}  // 72 bytes
      ]
    }
  }

Child Object KV entry:  (별도!)
  key:   "oid:CHILD_ID"
  value: hash(32) + amino{
    ObjectInfo{...}  // 80 bytes
    HeapItemValue{
      Value: RefValue{ObjectID: NODE_ID, Hash: ...}  // 72 bytes
    }
  }

Node Object KV entry:  (또 별도!)
  key:   "oid:NODE_ID"
  value: hash(32) + amino{
    ObjectInfo{...}  // 80 bytes
    StructValue{
      Fields: [key, value, leftRef, rightRef, height, size]  // ~200 bytes
    }
  }
```

### 개선 포맷

```
Parent Object KV entry:  (단일!)
  key:   "oid:PARENT_ID"
  value: hash(32) + amino{
    ObjectInfo{...}  // 80 bytes
    StructValue{
      Fields: [
        {T: avl.Tree, V: InlineStruct{  // inline!
          Fields: [
            {key: "a", value: TickInfo{...}},   // inline!
            {key: "b", value: TickInfo{...}},   // inline!
            {key: "c", value: TickInfo{...}},   // inline!
          ]
        }}
      ]
    }
  }
```

Node ObjectInfo (~80B × 3) + hash (32B × 3) + RefValue (72B × 4) = **~620 bytes 절감** (3 nodes).

---

## Dirty Tracking 영향

### 현재: Node 수정 → 해당 Node + 모든 조상 dirty

```
Node#5 수정 → Node#5 dirty → Node#3 dirty → HeapItem#2 dirty → Tree#1 dirty
= 4 Objects 재직렬화
```

### 개선: Node 수정 → Tree(parent) dirty → Tree만 재직렬화

```
Node#5 수정 → Tree#1 dirty (all nodes inline)
= 1 Object 재직렬화 (but larger)
```

| tree 크기 | 현재 (재직렬화 bytes) | inline (재직렬화 bytes) |
|:---------:|-------------------:|---------------------:|
| 5 nodes, 1 수정 | ~1,600 (4 Objects) | ~500 (1 Object) |
| 50 nodes, 1 수정 | ~1,600 (4 Objects) | ~3,000 (1 Object) |
| 500 nodes, 1 수정 | ~2,000 (5 Objects) | ~30,000 (1 Object) |

**임계값이 중요.** 큰 tree에서는 1 node 수정에 전체 재직렬화가 발생하면 역효과.

---

## 임계값 기반 하이브리드 전략

```go
const (
    INLINE_THRESHOLD    = 256   // bytes: 개별 Object inline 임계값
    TREE_INLINE_MAX     = 32    // nodes: tree 전체 inline 최대 노드 수
)

func shouldInline(obj Object) bool {
    oi := obj.GetObjectInfo()

    // 기본 조건
    if oi.RefCount > 1 || oi.IsEscaped {
        return false  // 공유 Object는 inline 불가
    }

    // 크기 조건
    if estimateInlineSize(obj) > INLINE_THRESHOLD {
        return false  // 큰 Object는 별도 저장
    }

    return true
}
```

### 임계값별 예상 효과

| TREE_INLINE_MAX | Mint 절감 | 수정 시 overhead | 적합 시나리오 |
|:-:|:-:|:-:|---|
| 8 | -30% | 낮음 | 대부분의 position/tick tree |
| 32 | -50% | 중간 | 소규모 DEX |
| 128 | -70% | 높음 | 읽기 위주 application |
| ∞ (전부 inline) | -85% | 매우 높음 | write 빈도가 극히 낮은 경우 |

**권장: 32 nodes.** GnoSwap 기준 pool당 tick 수가 보통 2-20개이므로 대부분 inline 범위에 들어옴.

---

## 구현 단계

### Phase 1: 인프라 (estimateSize + shouldInline)

**파일: `realm.go`**

```go
// Object의 inline 직렬화 크기 추정 (재귀적)
func estimateInlineSize(obj Object) int {
    switch v := obj.(type) {
    case *HeapItemValue:
        return 40 + estimateTypedValueSize(v.Value)
    case *StructValue:
        size := 32 // struct overhead
        for _, f := range v.Fields {
            size += estimateTypedValueSize(f)
        }
        return size
    case *ArrayValue:
        if v.Data != nil {
            return 16 + len(v.Data)
        }
        size := 16
        for _, item := range v.List {
            size += estimateTypedValueSize(item)
        }
        return size
    default:
        return INLINE_THRESHOLD + 1 // inline 불가
    }
}

func estimateTypedValueSize(tv TypedValue) int {
    switch v := tv.V.(type) {
    case nil:
        return 1
    case StringValue:
        return 8 + len(string(v))
    case Object:
        if shouldInline(v) {
            return estimateInlineSize(v)
        }
        return 72 // RefValue 크기
    default:
        return 16 // primitive
    }
}
```

### Phase 2: 직렬화 수정 (copyValueWithRefs)

**파일: `realm.go`**

```go
// inline 직렬화: ObjectInfo를 포함하지 않고 순수 데이터만
func copyValueInline(obj Object) Value {
    switch cv := obj.(type) {
    case *HeapItemValue:
        return &HeapItemValue{
            // ObjectInfo 생략!
            Value: refOrCopyValueWithInline(cv.Value),
        }
    case *StructValue:
        fields := make([]TypedValue, len(cv.Fields))
        for i, ftv := range cv.Fields {
            fields[i] = refOrCopyValueWithInline(ftv)
        }
        return &StructValue{
            // ObjectInfo 생략!
            Fields: fields,
        }
    // ...
    }
}
```

### Phase 3: 역직렬화 수정 (GetObject)

**파일: `store.go`**

amino 디코딩 후 inline된 자식을 Object로 복원:

```go
func (ds *defaultStore) GetObject(oid ObjectID) Object {
    // ... 기존 로직 ...
    obj := amino.MustUnmarshalAny(bz)

    // inline된 자식들에 대해 Object 복원
    restoreInlinedChildren(obj)
    return obj
}

func restoreInlinedChildren(parent Object) {
    // inline된 HeapItemValue/StructValue를 발견하면:
    // 1. ObjectInfo를 부모 기반으로 재구성
    // 2. owner 참조 설정
    // 3. cache에 등록
}
```

### Phase 4: amino 포맷 확장

inline Object를 구분하기 위한 마커:

```go
// 기존: RefValue{ObjectID, Hash} → "이 자리에 별도 Object 있음"
// 추가: InlineValue{Data}         → "이 자리에 Object 데이터가 inline됨"

type InlineValue struct {
    TypeID byte   // StructValue=1, HeapItemValue=2, ArrayValue=3
    Data   []byte // amino-encoded value (ObjectInfo 제외)
}
```

### Phase 5: 마이그레이션

기존 데이터와의 호환:

```go
func (ds *defaultStore) GetObject(oid ObjectID) Object {
    bz := ds.baseStore.Get(key)
    obj := amino.MustUnmarshalAny(bz)

    // RefValue → 기존 방식으로 로드 (하위 호환)
    // InlineValue → 새 방식으로 inline 복원
    // 두 포맷 모두 지원

    return obj
}
```

기존 Object는 수정될 때 자동으로 새 포맷으로 저장됨 (점진적 마이그레이션).

---

## 예상 결과

### Mint #1 (wide range, 현재 32KB)

| 구성요소 | 현재 Objects | inline 후 Objects | 현재 bytes | inline 후 bytes |
|---------|:-----------:|:----------------:|----------:|---------------:|
| pool realm | ~45 | ~10 | 17,600 | ~5,000 |
| gnft realm | ~12 | ~3 | 5,200 | ~1,500 |
| position realm | ~12 | ~4 | 5,200 | ~2,000 |
| token transfers | ~8 | ~3 | 4,000 | ~1,500 |
| **합계** | **~77** | **~20** | **32,000** | **~10,000** |

**Mint: 32KB → ~10KB (3.2 gnot → ~1.0 gnot)**

### Swap (현재 5KB)

| | 현재 | inline 후 |
|---|---:|---:|
| pool realm (재직렬화) | ~3,000 | ~800 |
| token transfer | ~2,000 | ~800 |
| **합계** | **~5,000** | **~1,600** |

**Swap: 5KB → ~1.6KB (0.5 gnot → ~0.16 gnot)**

---

## 리스크 및 고려사항

### 1. 큰 tree에서의 재직렬화 비용

inline 범위를 넘는 tree는 기존 방식 유지. 임계값(32 nodes)으로 제어.

### 2. amino 포맷 하위 호환

InlineValue를 amino에 등록해야 함. 기존 RefValue와 공존.
genesis export/import 시 포맷 변환 필요.

### 3. Hash 계산 변경

inline된 자식의 데이터가 부모 hash에 포함됨.
Merkle proof 구조가 변경될 수 있음 → iavl 연동 검토 필요.

### 4. 메모리 사용량

inline 직렬화 시 부모 Object의 amino 버퍼가 커짐.
32 nodes × ~50B = ~1.6KB 추가 메모리는 허용 범위.

### 5. GC/삭제 경로

inline된 Object 삭제 시 부모 재직렬화가 필요.
기존 개별 삭제보다 비용이 높을 수 있으나, 삭제는 드문 operation.

---

## 대안: avl 패키지 레벨 최적화

VM 수정 없이 `p/nt/avl` 패키지를 수정하여 유사한 효과를 얻는 방법:

```go
// avl/tree.gno — 하이브리드 구조
type Tree struct {
    // 소규모 (≤ flatMax): 정렬된 배열
    flatKeys   []string
    flatValues []interface{}

    // 대규모 (> flatMax): 기존 node 기반
    node *Node
    size int
}

const flatMax = 16
```

**장점:** VM 수정 불필요, avl 패키지만 변경
**단점:** 임계값 전환 로직 복잡, `[]interface{}` 내부의 pointer 값은 여전히 별도 Object

**절감 효과:** VM inline보다 작음 (HeapItemValue overhead는 그대로).
16 entries 기준 Node overhead만 제거: ~2,400 bytes 절감 (vs VM inline ~3,500 bytes).
