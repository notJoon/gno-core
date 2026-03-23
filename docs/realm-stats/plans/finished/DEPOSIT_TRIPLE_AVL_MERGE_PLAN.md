# Deposit Triple AVL Tree 통합 계획서 (v2)

**대상:** STORAGE_AUDIT_REPORT.md #13
**파일:** `contract/r/gnoswap/staker/deposit.gno`, `contract/r/gnoswap/staker/v1/type.gno`
**목표:** Deposit의 외부 인센티브 관련 3개 avl.Tree를 1개로 통합하여 Deposit당 2 Object 제거

---

## 1. 현재 구조

```go
type Deposit struct {
    // ... 기타 필드 ...
    collectedExternalRewards       *avl.Tree  // incentiveID → int64 (누적 수령액)
    externalRewardLastCollectTimes *avl.Tree  // incentiveID → int64 (마지막 수령 시간)
    externalIncentiveIds           *avl.Tree  // incentiveID → bool  (존재 여부)
    // ...
}
```

3개 tree가 동일한 key space(`incentiveID`)를 공유하며 항상 함께 접근됨.

### 문제

- Deposit 1개당 3개 `*avl.Tree` 포인터 = **3개 HeapItemValue Object**
- 동일 incentiveID에 대해 3개 tree에 각각 노드 생성 (key 직렬화 3x 중복)
- `NewDeposit`에서 `avl.NewTree()` 3회 호출 → 빈 tree Object 3개 생성

### 이전 시도에서의 교훈 (deposit_triple_avl.md)

이전 구현에서 `Removed bool` 필드를 `ExternalIncentiveState`에 추가하여 soft delete를 구현했으나:
- Dead entry 영구 누적 → storage 역효과
- 순회 시 `Removed` 필터링 오버헤드
- 재활성화 버그 가능성

**근본 원인 분석:** `Removed` 필드는 통합 tree에서 hard delete 시 데이터 손실을 방지하기 위해 도입되었으나, 호출 경로 분석 결과 이 보호는 불필요했음.

---

## 2. 수정 전략 (D-2: Remove 호출 제거 + hard delete 불요)

### 핵심 원칙

1. 3개 tree → 1개 tree + `ExternalIncentiveState` struct로 통합
2. `RemoveExternalIncentiveId()` 호출을 **제거** (staker.gno:596)
3. Dead entry는 `IterateExternalIncentiveIds` 내에서 `incentivesResolver.Get()` 실패로 **자연스럽게 skip**

### `Removed` 필드 없이 안전한 이유

현재 코드에서 `RemoveExternalIncentiveId()`는 `staker.gno:596`에서만 호출됨:

```go
// CollectReward 내부
if depositResolver.ExternalRewardLastCollectTime(incentiveId) > incentiveResolver.EndTimestamp() {
    deposit.RemoveExternalIncentiveId(incentiveId)  // ← 제거 대상
}
```

이 제거를 하지 않으면, 종료된 인센티브의 엔트리가 deposit에 잔류한다. 그러나:

- `calculatePositionReward` (calculate_pool_position_reward.gno:142-145)에서 `incentivesResolver.Get(incentiveId)` 호출 시, 종료/refund된 인센티브는 pool에서 제거되어 `ok=false` → **자연스럽게 skip**
- `EndExternalIncentive` (external_incentive.gno:242-244)에서는 `externalIncentiveIds` index를 거치지 않고 직접 `ExternalRewardLastCollectTime()`을 읽음 → **통합 tree에 엔트리가 존재하므로 정상 동작**

### Remove 호출을 제거하지 않으면 발생하는 문제 (통합 tree + hard delete)

`EndExternalIncentive`가 deposit의 `ExternalRewardLastCollectTime`을 직접 읽는데, 통합 tree에서 hard delete하면 이 데이터가 소실됨:

```
1. T=510: CollectReward → lastCollectTime=510 기록 → 510 > 500(endTime) → Remove 호출 → 데이터 삭제
2. T=600: EndExternalIncentive → lastCollectTime 없음 → fallback StakeTime=50 → 50 < 500 → 보상 전구간 재계산
3. → refund 과소 지급 (재무적 버그)
```

D-2는 Remove 호출 자체를 제거하므로 이 문제가 발생하지 않음.

### Dead entry의 실질 영향

- Deposit당 외부 인센티브 수는 실무적으로 수십 개 이하
- 종료된 인센티브의 `ExternalIncentiveState`는 `{CollectedReward int64, LastCollectTime int64}` = 16 bytes + key
- `IterateExternalIncentiveIds`에서 `incentivesResolver.Get()` 실패로 즉시 skip → 순회 오버헤드 O(1) per dead entry
- `EndExternalIncentive` 이후 dead entry의 lastCollectTime이 endTimestamp보다 크면 해당 deposit은 refund 계산에서도 skip → 정상 동작

---

## 3. 변경 사항

### 3a. 새 타입 정의

**파일:** `staker/deposit.gno`

```go
type ExternalIncentiveState struct {
    CollectedReward int64
    LastCollectTime int64
}
```획

### 3b. Deposit 구조체 변경

```go
// Before
type Deposit struct {
    // ...
    collectedExternalRewards       *avl.Tree  // 제거
    externalRewardLastCollectTimes *avl.Tree  // 제거
    externalIncentiveIds           *avl.Tree  // 제거
    // ...
}

// After
type Deposit struct {
    // ...
    externalIncentives *avl.Tree  // incentiveID → *ExternalIncentiveState
    // ...
}
```

### 3c. 헬퍼 메서드

```go
func (d *Deposit) getOrCreateIncentiveState(incentiveID string) *ExternalIncentiveState {
    if d.externalIncentives == nil {
        d.externalIncentives = avl.NewTree()
    }
    v, ok := d.externalIncentives.Get(incentiveID)
    if !ok {
        state := &ExternalIncentiveState{}
        d.externalIncentives.Set(incentiveID, state)
        return state
    }
    return v.(*ExternalIncentiveState)
}

func (d *Deposit) getIncentiveState(incentiveID string) *ExternalIncentiveState {
    if d.externalIncentives == nil {
        return nil
    }
    v, ok := d.externalIncentives.Get(incentiveID)
    if !ok {
        return nil
    }
    return v.(*ExternalIncentiveState)
}
```

### 3d. 기존 메서드 재작성

모든 기존 accessor 메서드의 **외부 시그니처는 유지**하고 내부 구현만 변경:

| 기존 메서드 | 변경 내용 |
|---|---|
| `GetCollectedExternalReward(id) (int64, bool)` | `getIncentiveState(id).CollectedReward` |
| `SetCollectedExternalReward(id, reward)` | `getOrCreateIncentiveState(id).CollectedReward = reward` |
| `GetExternalRewardLastCollectTime(id) (int64, bool)` | `getIncentiveState(id).LastCollectTime` |
| `SetExternalRewardLastCollectTime(id, time)` | `getOrCreateIncentiveState(id).LastCollectTime = time` |
| `AddExternalIncentiveId(id)` | `getOrCreateIncentiveState(id)` |
| `HasExternalIncentiveId(id) bool` | `externalIncentives.Has(id)` |
| `RemoveExternalIncentiveId(id)` | `externalIncentives.Remove(id)` (메서드 유지, 호출부만 제거) |
| `GetExternalIncentiveIdList() []string` | `externalIncentives.Iterate(...)` |
| `IterateExternalIncentiveIds(fn)` | `externalIncentives.Iterate(...)` |

### 3e. 제거되는 메서드

| 메서드 | 이유 |
|---|---|
| `CollectedExternalRewards() *avl.Tree` | raw tree 노출 불필요 |
| `SetCollectedExternalRewards(*avl.Tree)` | raw tree 설정 불필요 |
| `ExternalRewardLastCollectTimes() *avl.Tree` | raw tree 노출 불필요 |
| `SetExternalRewardLastCollectTimes(*avl.Tree)` | raw tree 설정 불필요 |
| `ExternalIncentiveIds() *avl.Tree` | raw tree 노출 불필요 |
| `SetExternalIncentiveIds(*avl.Tree)` | raw tree 설정 불필요 |

### 3f. `RemoveExternalIncentiveId` 호출 제거

**파일:** `staker/v1/staker.gno` (line 593-597)

```go
// Before
if depositResolver.ExternalRewardLastCollectTime(incentiveId) > incentiveResolver.EndTimestamp() {
    deposit.RemoveExternalIncentiveId(incentiveId)
}

// After: 제거 (해당 if 블록 전체 삭제)
```

메서드 자체는 유지 (테스트나 향후 사용을 위해), 호출부만 제거.

### 3g. `updateExternalRewardLastCollectTime` nil 체크 제거

**파일:** `staker/v1/type.gno` (line 119-132)

```go
// Before
func (self *DepositResolver) updateExternalRewardLastCollectTime(incentiveID string, currentTime int64) error {
    if self.ExternalRewardLastCollectTimes() == nil {          // ← 제거
        self.SetExternalRewardLastCollectTimes(avl.NewTree())  // ← 제거
    }
    // ...
}

// After
func (self *DepositResolver) updateExternalRewardLastCollectTime(incentiveID string, currentTime int64) error {
    externalLastCollectTime, exists := self.Deposit.GetExternalRewardLastCollectTime(incentiveID)
    if exists && externalLastCollectTime > currentTime {
        return makeErrorWithDetails(errNotAvailableUpdateCollectTime, ...)
    }
    self.Deposit.SetExternalRewardLastCollectTime(incentiveID, currentTime)
    return nil
}
```

### 3h. NewDeposit, Clone 변경

```go
// NewDeposit: avl.NewTree() 3회 → 1회
func NewDeposit(...) *Deposit {
    return &Deposit{
        // ...
        externalIncentives: avl.NewTree(),
        // ...
    }
}

// Clone: 통합 tree deep copy
func (d *Deposit) Clone() *Deposit {
    return &Deposit{
        // ...
        externalIncentives: cloneExternalIncentives(d.externalIncentives),
        // ...
    }
}

func cloneExternalIncentives(tree *avl.Tree) *avl.Tree {
    if tree == nil {
        return nil
    }
    cloned := avl.NewTree()
    tree.Iterate("", "", func(key string, value any) bool {
        state := value.(*ExternalIncentiveState)
        cloned.Set(key, &ExternalIncentiveState{
            CollectedReward: state.CollectedReward,
            LastCollectTime: state.LastCollectTime,
        })
        return false
    })
    return cloned
}
```

---

## 4. 테스트 커버리지

### 추가 완료된 테스트 (external_incentive_test.gno)

| 테스트 | 커버하는 시나리오 |
|---|---|
| `TestEndExternalIncentive_AfterCollectPastEnd` | CollectReward(past endTime) → Remove → EndExternalIncentive refund 정확성 |
| `TestEndExternalIncentive_NeverCollectedDeposit` | 한 번도 collect하지 않은 deposit의 보상이 refund 계산에 포함되는지 |
| `TestEndExternalIncentive_MixedCollectionStates` | A:removed, B:partial, C:never-collected 혼합 상태에서 refund 정확성 |

이 테스트들은 현재 3-tree 구조에서 통과하며, 통합 후에도 동일하게 통과해야 함.

### 기존 테스트 (수정 필요)

| 테스트 | 필요한 수정 |
|---|---|
| `TestCollectReward_LazyRemoval` | `RemoveExternalIncentiveId` 호출 제거 후, deposit의 incentive list가 줄어들지 않는 것을 검증하도록 변경 |
| `TestCollectReward_LazyRemovalBeforeEnd` | Remove 관련 검증 제거 |
| `TestDepositRemoveExternalIncentiveId` | 메서드 자체는 유지되므로 테스트 유지 가능 |

---

## 5. 작업 순서

### Task 1: Baseline 측정

기존 txtar 테스트로 baseline 수집:
- `tests/integration/testdata/staker/storage_staker_stake_with_externals.txtar`

이전 측정 결과 참고 (deposit_triple_avl.md):
- Full lifecycle gas: 174,344,714 (baseline) → 146,698,824 (통합, -15.9%)
- Storage bytes: 35,523 → 35,302 (-0.6%)

### Task 2: Deposit 구조체 변경

1. `ExternalIncentiveState` 타입 정의
2. Deposit 구조체에서 3개 `*avl.Tree` → 1개 `*avl.Tree` 변경
3. 헬퍼 메서드 (`getOrCreateIncentiveState`, `getIncentiveState`) 추가
4. 기존 accessor 메서드 재작성 (외부 시그니처 유지)
5. raw tree getter/setter 6개 제거
6. `NewDeposit`, `Clone` 수정

### Task 3: 호출부 변경

1. `staker/v1/staker.gno:593-597`: `RemoveExternalIncentiveId` 호출 if 블록 제거
2. `staker/v1/type.gno:120-122`: `ExternalRewardLastCollectTimes()` nil 체크 제거
3. raw tree getter/setter 호출하는 곳이 있으면 개별 accessor로 변경

### Task 4: 테스트 수정 및 검증

1. `TestCollectReward_LazyRemoval` 수정: Remove 제거 후 dead entry가 자연스럽게 skip되는 것을 검증
2. raw tree 접근 테스트 수정
3. `gno test` 전체 통과 확인
4. 신규 3개 테스트 통과 확인

### Task 5: 재측정 및 결과 기록

txtar 테스트로 gas / storage delta 비교.

---

## 6. 예상 효과

| 항목 | 현재 | 수정 후 | 절감 |
|---|---|---|---|
| `*avl.Tree` Object per Deposit | 3 | 1 | **-2 Object** |
| `avl.NewTree()` in NewDeposit | 3회 | 1회 | -2 allocation |
| avl node per incentiveID | 3개 (3 tree × 1 node) | 1개 | key 직렬화 2/3 제거 |
| Gas (이전 측정 기준) | 174.3M | ~146.7M | **~-15.9%** |
| Storage bytes (이전 측정 기준) | 35,523 | ~35,302 | ~-0.6% |

> Gas 절감이 주된 효과. Storage 절감은 미미하나, Object 수 감소로 GC 부하 및 영속 비용은 개선됨.

---

## 7. 주의사항

### 7a. `ExternalIncentiveState`를 포인터로 저장

`*ExternalIncentiveState`로 저장해야 `getOrCreateIncentiveState()`가 반환한 포인터를 통한 수정이 tree에 반영됨. value type으로 저장하면 복사가 발생하여 수정이 반영되지 않음.

### 7b. Dead entry 관리

`RemoveExternalIncentiveId` 호출을 제거하므로, 종료된 인센티브의 엔트리가 deposit에 영구 잔류한다. 이는:
- `calculatePositionReward`에서 `incentivesResolver.Get()` 실패로 skip됨 (정상)
- `EndExternalIncentive`에서 `lastCollectTime > endTimestamp` 체크로 skip됨 (정상)
- deposit당 외부 인센티브 수가 수십 개를 넘기 어려우므로 실질 영향 없음

### 7c. 직렬화 호환성

3개 `*avl.Tree` → 1개로 변경하면 Deposit의 직렬화 형식이 달라짐. 새 배포 전제 (기존 데이터 마이그레이션 불필요).

### 7d. raw tree getter 제거 영향

`CollectedExternalRewards()`, `ExternalRewardLastCollectTimes()`, `ExternalIncentiveIds()` 등 raw tree를 반환하는 getter가 제거됨. 제거 전 모든 호출 사이트를 검색하여 개별 accessor로 대체해야 함. 현재 확인된 사용처:
- `v1/type.gno:120`: `ExternalRewardLastCollectTimes()` nil 체크 → Task 3에서 제거
