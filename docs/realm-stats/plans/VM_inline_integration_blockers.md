# VM Inline Integration: 실측 시 발견된 Blockers

**Date:** 2026-03-17
**Status:** 분석 완료, 추가 구현 필요

---

## 실행 시도 결과

`InlineEnabled = true`로 txtar 테스트를 실행하면 **genesis 단계에서 패닉** 발생:

```
gno.land/r/demo/defi/foo20/foo20.gno:12:2-33:
unexpected object with id 0a28cee49785fa8c182a9bf20dc06c6b964886fb:4
```

---

## 에러 발생 경로 (Stack Trace)

```
preprocess.go:400  ← realm import 처리
  → store.go:326   ← 패키지 로드 (GetObjectSafe by pkgPath)
    → store.go:451  ← loadObjectSafe (PackageValue 로드)
      → store.go:521 ← amino 디코딩
        → values.go:842 ← PackageValue.GetBlock → store.GetObject(block.ObjectID)
          → values.go:2705 ← fillValueTV: Block 내 값들 fill
            → values.go:2724 ← RefValue 발견 → store.GetObject(refValue.ObjectID)
              → store.go:435 ← GetObject: KV에 없음 → PANIC
```

## 근본 원인

### 문제의 흐름

1. **Genesis에서 realm A (`r/demo/defi/foo20`) 배포**
2. realm A의 `init()` 실행 → `p/demo/tokens/grc20.NewToken()` 호출 → 내부 Object 생성
3. `FinalizeRealmTransaction`에서 `saveUnsavedObjects` 실행
4. `InlineEnabled = true` 상태에서:
   - **Object X** (HeapItemValue, RefCount=1, 작은 크기)가 `shouldInline=true`
   - `saveUnsavedObjects`에서 **Object X 별도 저장 skip**
   - Object X의 부모가 `SetObject` → `copyValueWithRefs` → `copyValueInline(X)` 으로 inline 직렬화
5. realm A의 **Block** 저장 시, Block의 Values 중 일부가 Object X를 참조
   - Block은 `shouldInline=false` (Block 타입은 inline 미지원)
   - Block 직렬화: `copyValueWithRefs` → X의 부모는 inline이지만, **X가 이미 `SetIsNewReal(false)`** 되어 `toRefValue(X)` 호출 시 문제 발생
   - 또는: X를 포함하는 부모가 inline으로 저장되었지만, **다른 경로에서 X를 `RefValue`로 참조하는 Object가 별도 저장** → 이후 로드 시 X의 ObjectID로 KV 조회 → 없음 → panic

6. **이후 realm B가 realm A를 import** → realm A의 PackageValue 로드 → Block 로드 → fillValueTV → RefValue 따라감 → KV에 없음 → panic

### 핵심 가정의 실패

`shouldInline`은 `RefCount == 1`을 체크하여 "단일 소유자만 참조" 보장을 가정합니다. 그러나:

- **Block의 Values[]에서의 참조**는 ownership tree와 별도로 존재할 수 있음
- Gno의 Block은 패키지 레벨 변수를 보유하며, 이들이 heap Object를 참조
- **같은 Object가 Block.Values[]와 다른 Object의 필드에 동시에 참조**될 수 있으나, RefCount 추적이 이를 정확히 반영하지 않을 수 있음
- 특히 `init()` 실행 중 생성된 Object는 Block에 저장되면서 ownership 체인이 복잡해짐

---

## 시도한 해결책과 결과

| 시도 | 결과 | 실패 원인 |
|------|------|----------|
| `InlineEnabled = true` 전역 | genesis 에서 panic | p/ 패키지 Object도 inline됨 |
| `FinalizeRealmTransaction` 시작시 활성화 | genesis realm에서 panic | genesis addpkg도 FinalizeRealmTransaction 호출 |
| `saveUnsavedObjects` 직전에만 활성화 | genesis realm에서 panic | genesis realm 배포시도 save 호출 |
| `IsRealmPath` 체크 추가 | genesis realm에서 panic | genesis의 realm 배포도 해당 |
| `getSelfOrChildObjects` inline skip 제거 + ObjectID 보존 | genesis realm에서 panic | inline skip된 Object가 다른 경로의 RefValue에서 참조됨 |
| + `SetCacheObject` cache 등록 | genesis realm에서 panic | genesis tx 경계에서 cache 리셋 가능성 |

---

## 해결에 필요한 추가 작업

### 접근 A: `copyValueInline`에서 ObjectInfo 보존 + 별도 저장도 유지

inline Object를 부모에 inline으로 직렬화하되, **별도 KV entry도 유지**:

```go
// saveUnsavedObjects에서:
if InlineEnabled && shouldInline(co) {
    // skip하지 않음 — 별도 저장도 함
    rlm.saveUnsavedObjectRecursively(store, co, tids)
}
```

이 경우 storage 절감 효과가 없음 (inline + 별도 저장 = 오히려 증가). 측정 목적에 부합하지 않음.

### 접근 B: `refOrCopyValue`에서만 inline (저장 skip 없이)

`saveUnsavedObjects`는 변경하지 않고, `refOrCopyValue`에서만 inline 직렬화:

```go
func refOrCopyValue(tv TypedValue) TypedValue {
    if obj, ok := tv.V.(Object); ok {
        if InlineEnabled && shouldInline(obj) {
            tv.V = copyValueInline(obj)  // inline 직렬화
            return tv
        }
        tv.V = toRefValue(obj)
        return tv
    }
    // ...
}
```

별도 저장은 정상 수행하되, 부모의 직렬화에서 inline으로 포함.
**부모의 serialized bytes에 자식 데이터가 포함되므로 부모 Object 크기 증가, 자식 Object도 별도 존재.**
Storage delta는 **부모 Object 크기 증가분 - 자식 Object RefValue→inline 차이**로 측정 가능.
하지만 자식도 별도 저장되므로 **총 storage는 오히려 증가** (중복 저장).

### 접근 C: store.GetObject 수정 — KV miss 시 부모에서 탐색

```go
func (ds *defaultStore) GetObject(oid ObjectID) Object {
    oo := ds.GetObjectSafe(oid)
    if oo == nil && InlineEnabled {
        // KV에 없음 → inline된 Object일 수 있음
        // 부모 ObjectID에서 로드 후 자식에서 탐색
        oo = ds.findInlinedObject(oid)
    }
    if oo == nil {
        panic(...)
    }
    return oo
}
```

부모 ObjectID를 알 수 없으므로 구현 불가 (ObjectID에 부모 정보 없음).

### 접근 D: processNewCreatedMarks에서 inline 대상을 미리 cache에 등록

`processNewCreatedMarks` 이후, 모든 created Object 중 `shouldInline=true`인 것을 cache에 등록:

```go
// FinalizeRealmTransaction에서, saveUnsavedObjects 직전:
for _, co := range rlm.created {
    if shouldInline(co) {
        store.SetCacheObject(co)
    }
}
```

이렇게 하면 `saveUnsavedObjects`에서 skip해도 cache에서 찾을 수 있음.
**하지만 genesis에서 tx 경계를 넘어 다른 realm이 로드할 때 cache가 유효한지 확인 필요.**

### 접근 E: genesis 완료 후에만 inline 활성화 (가장 안전)

Genesis의 모든 tx 완료 후, 사용자 tx부터 inline 활성화:

```go
var genesisComplete = false

func (rlm *Realm) FinalizeRealmTransaction(store Store) {
    if genesisComplete && IsRealmPath(rlm.Path) {
        InlineEnabled = true
        defer func() { InlineEnabled = false }()
    }
    // ...
}
```

`genesisComplete`는 node 시작 시 genesis 처리 완료 후 `true`로 설정.
**txtar 테스트에서는 genesis 후 사용자 tx (Mint, Swap 등)에서만 inline이 적용되므로 측정 가능.**

---

## 권장: 접근 E

Genesis 완료 시점을 표시하는 플래그를 추가하고, 이후 realm tx에서만 inline 활성화.
가장 안전하고, 측정 목적에 충분하며, genesis 호환성 문제를 완전히 회피.
