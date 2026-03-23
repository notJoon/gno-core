# Phantom Array Inlining — 디버깅 인수인계 문서

**Branch:** `feat/phantom-array-inlining`
**현재 HEAD:** `b5c4d0e55` (fix: bypass inline array skip for PointerValue/SliceValue bases)
**상태:** genesis 단계에서 `panic: unexpected unreal object` 발생. 테스트 실패.
**Working tree:** clean (커밋과 일치)

---

## 1. 프로젝트 목표

`u256.Uint = [4]uint64`가 GnoVM에서 독립 KV 엔트리(339 bytes)로 저장되는 오버헤드를 제거하기 위해, 작고 primitive한 고정 크기 배열을 부모 Object의 KV 엔트리에 inline 직렬화한다.

**기대 효과:** First Mint 32KB → ~22KB (30% 절감), storage deposit 약 1 GNOT 절감/트랜잭션.

---

## 2. 구현된 변경사항 (7곳)

| # | 파일 | 위치 | 변경 |
|---|------|------|------|
| 1 | `realm.go` | line ~1858 | `shouldInlineArray` 함수: `!GetIsReal()`, `!escaped`, `RC≤1`, `len≤8`, primitive elements |
| 2 | `realm.go` | line ~1895 | `copyArrayInline` 함수: zero ObjectInfo로 복사 |
| 3 | `realm.go:1127` | `getSelfOrChildObjects` | inline array를 Object 등록에서 skip |
| 4 | `realm.go:1858` | `refOrCopyValue` | inline array를 RefValue 대신 copyArrayInline으로 직렬화 |
| 5 | `ownership.go:375` | `GetFirstObject` | inline array에 대해 nil 반환 (co/xo 추적 차단) |
| 6 | `realm.go:209` | `DidUpdate` | non-real inline array가 po일 때 owner walk-up → MarkDirty(ancestor) |
| 7 | `values.go:233` | `Assign2` | 할당 후 inline array에 owner 설정 |

**추가:**
- `store.go:512`: `restoreInlineArrayOwners` — 역직렬화 후 inline array owner 복원
- `realm.go:1154-1179`: `getChildObjects`의 PointerValue/SliceValue case — inline skip bypass (직접 append)

---

## 3. 현재 실패 상태

### 증상

```
gnoland start → genesis tx 처리 중:
panic: unexpected unreal object

Stack:
  realm.go:1801  toRefValue: !oo.GetIsReal() → panic
  realm.go:1434  copyValueWithRefs → PointerValue → toRefValue(cv.Base)
  realm.go:1936  refOrCopyValue (부모 필드)
  realm.go:1560  copyValueWithRefs (부모 object)
  store.go:664   SetObject
  realm.go:885   saveObject
  realm.go:851   saveUnsavedObjectRecursively (child 저장)
  realm.go:840   saveUnsavedObjectRecursively (child의 child 저장)
  realm.go:782   saveUnsavedObjects
  realm.go:415   FinalizeRealmTransaction
```

### 디버그로 확인된 정보

PointerValue case에 진단 코드를 추가하여 확인한 결과:

```
type=*gnolang.HeapItemValue
isNewReal=false
rc=0
id=0000000000000000000000000000000000000000:0
shouldInline=false  ← ArrayValue가 아니므로 해당 없음
```

**핵심 발견: 문제의 Base는 `*HeapItemValue`이지 `*ArrayValue`가 아니다.**

HeapItemValue는 `shouldInlineArray`의 대상이 아니므로 phantom array inlining 로직과 **직접적 관련이 없어 보인다.** 그러나 이 panic은 phantom 변경이 없는 원본 코드(`perf/object-dirty-log` 브랜치)에서는 발생하지 않는다.

### 원본 코드와의 차이에서 발생 가능한 원인

`getChildObjects`의 PointerValue case가 변경되었다:

**원본:**
```go
case PointerValue:
    more = getSelfOrChildObjects(cv.Base, more)
    return more
```

**현재 (bypass):**
```go
case PointerValue:
    if ref, ok := cv.Base.(RefValue); ok {
        more = append(more, ref)
    } else if obj, ok := cv.Base.(Object); ok {
        more = append(more, obj)
    }
    return more
```

두 경로의 차이: `getSelfOrChildObjects`는 Object가 아닌 경우 `getChildObjects`로 재귀한다. bypass는 `RefValue`이거나 `Object`인 경우만 처리하고, **둘 다 아닌 경우를 무시**한다.

PointerValue.Base는 항상 Object 또는 RefValue여야 하므로 이론상 차이 없다. 하지만 **edge case가 있을 수 있다.**

---

## 4. 검증된 사실들

### 작동 확인됨

- 원본 코드 (`perf/object-dirty-log` 브랜치, phantom 변경 없음): **PASS**
- phantom 변경 후 커밋된 코드: **FAIL** (genesis panic)

### 확인된 설계 결함과 수정 이력

| 발견 시점 | 결함 | 수정 |
|----------|------|------|
| v1 계획서 | element-level assignment (`z[0]=v`) 시 DidUpdate가 non-real po로 즉시 return → silent data loss | DidUpdate에 owner walk-up 로직 추가 (커밋 `1d131d0`) |
| 코드 리뷰 | `getSelfOrChildObjects` 중앙 skip이 MapValue/FuncValue/BoundMethodValue 경로 누락 | `getSelfOrChildObjects`에 중앙 skip + `getChildObjects`의 context-aware skip 제거 (커밋 `c8401f70e`) |
| 테스트 실패 | `[6]T` slice backing store가 inline skip되어 ObjectID 미할당 → toRefValue panic | PointerValue/SliceValue case에서 getSelfOrChildObjects bypass (커밋 `b5c4d0e55`) |
| 테스트 실패 | PointerValue.Base가 HeapItemValue(non-real, RC=0)인 경우 toRefValue panic | **미해결** — 현재 실패 지점 |

### 확인된 설계 원칙

1. **Pointer/Slice의 Base는 반드시 real Object여야 한다** — `toRefValue`가 ObjectID를 요구
2. **`shouldInlineArray`는 `!GetIsReal()`을 체크** — 이미 real인 배열은 inline하지 않음 (마이그레이션 안전성)
3. **genesis blocker는 ArrayValue에 해당하지 않음** — 다른 Object가 ArrayValue의 ObjectID를 RefValue로 참조하는 경로가 없음
4. **`processNewCreatedMarks`는 `saveUnsavedObjects`보다 먼저 실행** — 타이밍 보장

---

## 5. 미해결 문제: HeapItemValue non-real panic

### 현상

genesis에서 어떤 Object가 저장될 때, 그 내부에 `PointerValue{Base: *HeapItemValue}`가 있고, 이 HeapItemValue가 non-real(ObjectID=zero, RC=0, isNewReal=false)이다.

### 가설 A: bypass가 간접적으로 HeapItemValue 처리를 방해

`getChildObjects`의 PointerValue bypass가 `getSelfOrChildObjects`를 거치지 않으므로, HeapItemValue가 child list에 추가되는 경로가 **기존과 다를 수 있다.**

기존: `getSelfOrChildObjects(HeapItemValue)` → Object → `append(more, HeapItemValue)` ✓
bypass: `if obj, ok := cv.Base.(Object); ok { append(more, obj) }` → Object → `append(more, HeapItemValue)` ✓

**표면적으로 동일하다.** 하지만 `getSelfOrChildObjects`에서 inline skip 조건 (`shouldInlineArray`)이 HeapItemValue에 적용되지 않으므로 (`*ArrayValue` 체크), 이 경로에서도 동일하게 추가된다.

### 가설 B: PointerValue bypass가 다른 경로의 HeapItemValue에 영향

bypass는 `getChildObjects`의 PointerValue case에만 적용된다. 하지만 PointerValue는 다른 Object의 필드로도 존재한다. 예를 들어 FuncValue의 Captures에 PointerValue가 있으면, `getChildObjects` → FuncValue case → `getSelfOrChildObjects(capture.V)` → PointerValue는 Object가 아니므로 `getChildObjects(PointerValue)` → bypass case 진입.

이 경로에서 PointerValue.Base가 HeapItemValue이면 bypass로 직접 append된다. **기존과 동일하므로 문제없어야 한다.**

### 가설 C: phantom 변경의 다른 부분이 간접적으로 HeapItemValue에 영향

`getSelfOrChildObjects`의 inline skip이 예상치 못한 경로에서 HeapItemValue 내부의 ArrayValue를 skip하고 있을 가능성:

```go
case *HeapItemValue:
    more = getSelfOrChildObjects(cv.Value.V, more)
```

HeapItemValue 내부에 ArrayValue가 있고, `shouldInlineArray`가 true를 반환하면, 이 ArrayValue가 skip된다. ArrayValue가 skip되면 HeapItemValue의 자식이 없어지지만, **HeapItemValue 자체의 처리에는 영향 없다** (HeapItemValue는 `getSelfOrChildObjects`에서 Object로 등록됨).

하지만 HeapItemValue가 `incRefCreatedDescendants`에서 처리될 때, 내부 ArrayValue가 skip되면 ArrayValue에 IncRefCount/SetOwner가 호출되지 않는다. **이 자체는 phantom 설계의 의도된 동작이다.**

### 가설 D: HeapItemValue가 DidUpdate를 통해 MarkNewReal되지 않은 근본 원인

디버그에서 HeapItemValue의 `isNewReal=false, rc=0`은 이 HeapItemValue가 **DidUpdate를 통해 한 번도 co로 전달되지 않았음**을 의미한다.

이것은 HeapItemValue가 composite literal이나 함수 반환값의 일부로 생성되어, 개별 필드 할당이 아닌 구조체 전체 할당으로 소유권 트리에 합류한 경우 발생할 수 있다.

기존 코드에서는: `incRefCreatedDescendants(parent)` → `getChildObjects2(parent)` → ... → PointerValue → `getSelfOrChildObjects(HeapItemValue)` → append → child list → IncRefCount → MarkNewReal → ObjectID 할당.

phantom 코드에서: 동일한 경로... **이론상 동일해야 한다.**

### 디버깅 제안

1. **bypass 제거 테스트:** PointerValue/SliceValue bypass를 원래 `getSelfOrChildObjects` 호출로 복원하고 테스트. 만약 통과하면 bypass가 원인.

2. **shouldInlineArray 비활성화 테스트:** `shouldInlineArray`가 항상 false를 반환하도록 하고 테스트. 만약 통과하면 inline skip이 간접적으로 HeapItemValue에 영향.

3. **단계별 비활성화:** 각 변경(#1~#7)을 하나씩 비활성화하여 어떤 변경이 panic을 유발하는지 bisect.

4. **panic 시점의 전체 Object 트리 덤프:** `toRefValue` panic 직전에 부모 Object와 문제의 PointerValue의 full context를 출력.

---

## 6. 코드 위치 참조

### 핵심 함수 위치 (현재 코드 기준)

| 함수 | 파일:라인 | 역할 |
|------|----------|------|
| `shouldInlineArray` | `realm.go:~1858` | inline 대상 판별 |
| `copyArrayInline` | `realm.go:~1895` | zero ObjectInfo로 배열 복사 |
| `getSelfOrChildObjects` | `realm.go:1127` | 중앙 inline skip |
| `getChildObjects` PointerValue | `realm.go:1154-1167` | bypass (직접 append) |
| `getChildObjects` SliceValue | `realm.go:1173-1180` | bypass (직접 append) |
| `refOrCopyValue` | `realm.go:~1916` | inline 분기 |
| `GetFirstObject` | `ownership.go:375` | nil 반환 |
| `DidUpdate` phantom handler | `realm.go:209-228` | owner walk-up |
| `Assign2` owner 설정 | `values.go:233-238` | inline array에 owner 부여 |
| `restoreInlineArrayOwners` | `store.go:514-536` | 역직렬화 후 owner 복원 |
| `toRefValue` | `realm.go:~1798` | panic 발생 지점 |
| `copyValueWithRefs` PointerValue | `realm.go:~1422-1436` | toRefValue(cv.Base) 호출 |
| `incRefCreatedDescendants` | `realm.go:469` | ObjectID 할당 + 자식 순회 |
| `processNewCreatedMarks` | `realm.go:416` | 생성 마크 처리 |
| `saveUnsavedObjectRecursively` | `realm.go:800` | bottom-up 저장 |

### 테스트 명령

```bash
# 빌드
go build ./gnovm/pkg/gnolang/

# 실패하는 테스트 (genesis 포함)
go test -v -run "TestTestdata/position_storage_poisition_lifecycle" \
    ./gno.land/pkg/integration/ -timeout 600s

# 기본 브랜치에서 동일 테스트 (PASS 확인)
git stash
git checkout perf/object-dirty-log -- gnovm/pkg/gnolang/
go test -v -run "TestTestdata/position_storage_poisition_lifecycle" \
    ./gno.land/pkg/integration/ -timeout 600s
git checkout -- gnovm/pkg/gnolang/
git stash pop

# shouldInlineArray 비활성화 테스트 (빠른 bisect)
# realm.go의 shouldInlineArray 첫 줄에 `return false` 추가 후 테스트
```

### 관련 문서

- `docs/realm-stats/plans/phantom_array_inlining_impl_plan.md` — 전체 구현 계획서 (v2)
- `docs/realm-stats/plans/phantom_array_getChildObjects_fix.md` — PointerValue/SliceValue bypass 분석
- `docs/realm-stats/GnoVM_Object_Persistence_Deep_Dive.md` — 객체 영속성 시스템 참조
- `docs/realm-stats/GnoVM_Cross_Realm_Mechanism.md` — cross-realm 메커니즘 참조

---

## 7. 빠른 시작 가이드

```bash
# 1. 현재 상태 확인
git log --oneline -8
# b5c4d0e55 가 HEAD여야 함

# 2. 빌드 확인
go build ./gnovm/pkg/gnolang/

# 3. 실패 재현
go test -v -run "TestTestdata/position_storage_poisition_lifecycle" \
    ./gno.land/pkg/integration/ -timeout 600s 2>&1 | \
    grep -E "(PASS|FAIL|panic|unreal)"

# 4. 가장 빠른 bisect: shouldInlineArray 비활성화
# realm.go에서 shouldInlineArray 함수의 첫 줄을 `return false`로 변경
# → 통과하면 inline skip이 간접적으로 원인
# → 실패하면 다른 변경(DidUpdate, Assign2 등)이 원인
```
