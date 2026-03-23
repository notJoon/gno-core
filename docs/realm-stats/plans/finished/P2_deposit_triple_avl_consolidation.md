# P2: Deposit Triple AVL Tree 통합

## 개요

`Deposit` 구조체의 외부 인센티브 관련 3개 AVL tree를 1개로 통합하여 Object 수를 줄인다.

### 현재 구조 (3개 트리)

```go
type Deposit struct {
    // ...
    collectedExternalRewards       *avl.Tree  // incentiveID → int64 (누적 수령액)
    externalRewardLastCollectTimes *avl.Tree  // incentiveID → int64 (마지막 수령 시간)
    externalIncentiveIds           *avl.Tree  // incentiveID → bool  (인센티브 ID 존재 여부)
    // ...
}
```

### 변경 후 구조 (1개 트리)

```go
type ExternalIncentiveState struct {
    CollectedReward  int64
    LastCollectTime  int64
}

type Deposit struct {
    // ...
    externalIncentives *avl.Tree  // incentiveID → *ExternalIncentiveState
    // ...
}
```

### 절감 메커니즘

- 3개 `*avl.Tree` 포인터 → 1개로 줄임: HeapItemValue Object 2개 제거
- 동일 incentiveID로 3개 트리에 각각 노드를 생성하던 것이 1개 노드로 통합
- 빈 트리 초기화 2개 제거 (NewDeposit에서 `avl.NewTree()` 3번 → 1번)

---

## 필요조건

1. 새 타입 `ExternalIncentiveState`를 `staker` 패키지에 정의
2. 기존 3개 트리의 모든 접근 메서드를 통합 트리 기반으로 재작성
3. `v1` 패키지의 `DepositResolver`가 사용하는 메서드 시그니처 호환성 유지

---

## 수정 사항

### 1. 새 타입 정의

**파일**: `examples/gno.land/r/gnoswap/staker/deposit.gno` (구조체 정의 앞에 추가)

```go
// ExternalIncentiveState stores per-incentive state for a deposit.
type ExternalIncentiveState struct {
    CollectedReward int64
    LastCollectTime int64
}
```

### 2. 구조체 변경

**파일**: `examples/gno.land/r/gnoswap/staker/deposit.gno` (라인 10~24)

```go
// Before
type Deposit struct {
    warmups                        []Warmup
    liquidity                      *u256.Uint
    targetPoolPath                 string
    owner                          address
    stakeTime                      int64
    internalRewardLastCollectTime  int64
    collectedInternalReward        int64
    collectedExternalRewards       *avl.Tree  // 제거
    externalRewardLastCollectTimes *avl.Tree  // 제거
    externalIncentiveIds           *avl.Tree  // 제거
    lastExternalIncentiveUpdatedAt int64
    tickLower                      int32
    tickUpper                      int32
}

// After
type Deposit struct {
    warmups                        []Warmup
    liquidity                      *u256.Uint
    targetPoolPath                 string
    owner                          address
    stakeTime                      int64
    internalRewardLastCollectTime  int64
    collectedInternalReward        int64
    externalIncentives             *avl.Tree  // incentiveID → *ExternalIncentiveState (통합)
    lastExternalIncentiveUpdatedAt int64
    tickLower                      int32
    tickUpper                      int32
}
```

### 3. 헬퍼 메서드: incentive state 가져오기

```go
// getOrCreateIncentiveState returns the ExternalIncentiveState for the given incentive ID.
// Creates a new zero-state entry if it doesn't exist.
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

// getIncentiveState returns the ExternalIncentiveState for the given incentive ID.
// Returns nil if the incentive ID does not exist.
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

### 4. 기존 메서드 재작성

#### CollectedExternalRewards 관련

```go
// Before: CollectedExternalRewards() *avl.Tree
// After: 제거 (직접 트리 접근 불필요)

// Before: SetCollectedExternalRewards(tree *avl.Tree)
// After: 제거

// GetCollectedExternalReward — 기존 시그니처 유지
func (d *Deposit) GetCollectedExternalReward(incentiveID string) (int64, bool) {
    state := d.getIncentiveState(incentiveID)
    if state == nil {
        return 0, false
    }
    return state.CollectedReward, true
}

// SetCollectedExternalReward — 기존 시그니처 유지
func (d *Deposit) SetCollectedExternalReward(incentiveID string, reward int64) {
    state := d.getOrCreateIncentiveState(incentiveID)
    state.CollectedReward = reward
}
```

#### ExternalRewardLastCollectTimes 관련

```go
// Before: ExternalRewardLastCollectTimes() *avl.Tree
// After: 제거

// Before: SetExternalRewardLastCollectTimes(tree *avl.Tree)
// After: 제거

// GetExternalRewardLastCollectTime — 기존 시그니처 유지
func (d *Deposit) GetExternalRewardLastCollectTime(incentiveID string) (int64, bool) {
    state := d.getIncentiveState(incentiveID)
    if state == nil {
        return 0, false
    }
    return state.LastCollectTime, true
}

// SetExternalRewardLastCollectTime — 기존 시그니처 유지
func (d *Deposit) SetExternalRewardLastCollectTime(incentiveID string, currentTime int64) {
    state := d.getOrCreateIncentiveState(incentiveID)
    state.LastCollectTime = currentTime
}
```

#### ExternalIncentiveIds 관련

```go
// Before: ExternalIncentiveIds() *avl.Tree
// After: 제거

// Before: SetExternalIncentiveIds(tree *avl.Tree)
// After: 제거

// AddExternalIncentiveId — 기존 시그니처 유지
func (d *Deposit) AddExternalIncentiveId(incentiveId string) {
    d.getOrCreateIncentiveState(incentiveId)  // 존재하지 않으면 생성
}

// HasExternalIncentiveId — 기존 시그니처 유지
func (d *Deposit) HasExternalIncentiveId(incentiveId string) bool {
    if d.externalIncentives == nil {
        return false
    }
    return d.externalIncentives.Has(incentiveId)
}

// RemoveExternalIncentiveId — 기존 시그니처 유지
func (d *Deposit) RemoveExternalIncentiveId(incentiveId string) {
    if d.externalIncentives == nil {
        return
    }
    d.externalIncentives.Remove(incentiveId)
}

// GetExternalIncentiveIdList — 기존 시그니처 유지
func (d *Deposit) GetExternalIncentiveIdList() []string {
    if d.externalIncentives == nil {
        return []string{}
    }
    ids := make([]string, 0, d.externalIncentives.Size())
    d.externalIncentives.Iterate("", "", func(key string, _ any) bool {
        ids = append(ids, key)
        return false
    })
    return ids
}

// IterateExternalIncentiveIds — 기존 시그니처 유지
func (d *Deposit) IterateExternalIncentiveIds(fn func(incentiveId string) bool) {
    if d.externalIncentives == nil {
        return
    }
    d.externalIncentives.Iterate("", "", func(key string, _ any) bool {
        return fn(key)
    })
}
```

### 5. NewDeposit 변경

**파일**: `examples/gno.land/r/gnoswap/staker/deposit.gno` (라인 256~279)

```go
// Before
func NewDeposit(...) *Deposit {
    return &Deposit{
        // ...
        externalRewardLastCollectTimes: avl.NewTree(),
        collectedExternalRewards:       avl.NewTree(),
        externalIncentiveIds:           avl.NewTree(),
        // ...
    }
}

// After
func NewDeposit(...) *Deposit {
    return &Deposit{
        // ...
        externalIncentives:             avl.NewTree(),
        // ...
    }
}
```

### 6. Clone 변경

**파일**: `examples/gno.land/r/gnoswap/staker/deposit.gno` (라인 234~254)

```go
// Before
func (d *Deposit) Clone() *Deposit {
    // ...
    return &Deposit{
        // ...
        collectedExternalRewards:       cloneAvlTree(d.collectedExternalRewards),
        externalRewardLastCollectTimes: cloneAvlTree(d.externalRewardLastCollectTimes),
        externalIncentiveIds:           cloneAvlTree(d.externalIncentiveIds),
        // ...
    }
}

// After
func (d *Deposit) Clone() *Deposit {
    // ...
    return &Deposit{
        // ...
        externalIncentives: cloneExternalIncentives(d.externalIncentives),
        // ...
    }
}

// cloneExternalIncentives deep-copies the external incentives tree.
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

### 7. v1 패키지 변경

**파일**: `examples/gno.land/r/gnoswap/staker/v1/type.gno`

`DepositResolver.updateExternalRewardLastCollectTime()` (라인 119~132)에서 직접 트리 접근 제거:

```go
// Before
func (self *DepositResolver) updateExternalRewardLastCollectTime(incentiveID string, currentTime int64) error {
    if self.ExternalRewardLastCollectTimes() == nil {    // ← 트리 직접 접근
        self.SetExternalRewardLastCollectTimes(avl.NewTree())  // ← 트리 직접 설정
    }
    externalLastCollectTime, exists := self.Deposit.GetExternalRewardLastCollectTime(incentiveID)
    if exists && externalLastCollectTime > currentTime {
        return makeErrorWithDetails(errNotAvailableUpdateCollectTime, ...)
    }
    self.Deposit.SetExternalRewardLastCollectTime(incentiveID, currentTime)
    return nil
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

> `ExternalRewardLastCollectTimes()` getter와 `SetExternalRewardLastCollectTimes()` setter는 제거되므로, 이 nil 체크+초기화 패턴이 불필요. `SetExternalRewardLastCollectTime()`이 내부적으로 lazy init 처리.

---

## 제거되는 메서드 목록

다음 메서드들은 통합 후 불필요해지므로 제거:

| 메서드 | 이유 |
|---|---|
| `CollectedExternalRewards() *avl.Tree` | 트리 직접 노출 불필요 |
| `SetCollectedExternalRewards(*avl.Tree)` | 트리 직접 설정 불필요 |
| `ExternalRewardLastCollectTimes() *avl.Tree` | 트리 직접 노출 불필요 |
| `SetExternalRewardLastCollectTimes(*avl.Tree)` | 트리 직접 설정 불필요 |
| `ExternalIncentiveIds() *avl.Tree` | 트리 직접 노출 불필요 |
| `SetExternalIncentiveIds(*avl.Tree)` | 트리 직접 설정 불필요 |

**주의**: 제거 전 모든 호출 사이트를 검색하여 사용되지 않음을 확인할 것.

---

## 수정 파일 목록

| 파일 | 변경 내용 |
|---|---|
| `examples/gno.land/r/gnoswap/staker/deposit.gno` | 구조체, 새 타입, 모든 accessor 재작성, NewDeposit, Clone |
| `examples/gno.land/r/gnoswap/staker/v1/type.gno` | `updateExternalRewardLastCollectTime()` nil 체크 제거 |

---

## 주의사항

1. **제거 메서드의 외부 사용 검색**: `CollectedExternalRewards()`, `ExternalRewardLastCollectTimes()`, `ExternalIncentiveIds()` 등 raw tree getter가 v1 패키지나 getter.gno 등에서 직접 사용되는지 반드시 `grep`으로 확인. 특히 `v1/type.gno:120`의 `ExternalRewardLastCollectTimes()` 호출은 위 수정에서 처리됨.

2. **`ExternalIncentiveState`를 포인터로 저장**: `*ExternalIncentiveState`로 저장해야 `getOrCreateIncentiveState()`가 반환한 포인터를 통한 수정이 트리에 반영됨. 값 타입으로 저장하면 복사가 발생하여 수정이 반영되지 않음.

3. **인센티브 제거 시 데이터 손실**: `RemoveExternalIncentiveId()`가 노드를 삭제하면 해당 인센티브의 `CollectedReward`, `LastCollectTime` 정보도 함께 사라짐. 기존 코드에서 incentiveId 제거 후 해당 인센티브의 collected reward를 조회하는 패턴이 없는지 확인 필요.
   - 분석 결과: `RemoveExternalIncentiveId()`는 인센티브가 종료/만료된 후에만 호출되며, 이후 해당 ID의 reward/time 데이터는 접근되지 않음. ✅ 안전.

4. **Lazy initialization**: `getOrCreateIncentiveState()`가 `externalIncentives == nil` 체크를 포함하므로, NewDeposit에서 `avl.NewTree()`를 즉시 생성하지 않고 nil로 시작해도 됨. 그러나 일관성을 위해 NewDeposit에서 빈 트리를 생성하는 것을 권장.

---

## 잠재적 사이드 이펙트

1. **Gno VM Object 구조 변경**: 3개 `*avl.Tree` → 1개로 줄이면 Deposit Object의 직렬화 형식이 달라짐. 새 배포에서는 문제 없으나, 기존 저장된 Deposit과의 호환성 없음 (마이그레이션 불필요 — 새 배포 전제).

2. **AVL 트리 노드 수 감소**: 동일 incentiveID에 대해 3개 노드 → 1개 노드. 노드당 ~100-200 bytes의 메타데이터 + 키 직렬화 오버헤드 절감.

3. **`ExternalIncentiveState` 포인터 Object**: 각 state가 `*ExternalIncentiveState`로 저장되므로 HeapItemValue Object 자체는 생성됨. 그러나 기존에는 3개 트리 × N개 노드 = 3N개 값이었으나, 통합 후 N개 `ExternalIncentiveState` Object로 감소. 순 Object 수: `3 + 3N` → `1 + N`.

---

## 측정 결과

### 테스트 환경

- 테스트 txtar: `storage_staker_stake_only`, `storage_staker_stake_with_externals`
- 환경변수: `GNO_REALM_STATS_LOG=stderr`
- 측정 대상: StakeToken → CollectReward → UnStakeToken full lifecycle
- 외부 인센티브: 3개 (BAR, FOO, WUGNOT reward tokens)

### Stake Only (외부 인센티브 없음)

| Metric | Baseline (3 trees) | Triple AVL (1 tree) | Delta | % |
|---|---|---|---|---|
| GAS USED | 63,270,504 | — | — | — |
| STORAGE DELTA | 33,099 | — | — | — |
| staker bytes_delta | 32,047 | — | — | — |

> Stake Only의 경우 triple AVL 측정은 별도 세션에서 수행됨. 아래 full lifecycle 결과 참조.

### Full Lifecycle (3 external incentives)

#### Baseline (3 AVL trees)

| Operation | GAS USED | STORAGE DELTA | staker bytes_delta |
|---|---|---|---|
| StakeToken | 63,407,932 | 33,099 | 32,047 |
| CollectReward | 50,634,743 | 10,965 | 10,965 |
| UnStakeToken | 60,302,039 | -7,511 | -7,489 |
| **Total** | **174,344,714** | **36,553** | **35,523** |

#### Triple AVL (1 tree)

| Operation | GAS USED | STORAGE DELTA | staker bytes_delta |
|---|---|---|---|
| StakeToken | 54,955,053 | 31,348 | 30,296 |
| CollectReward | 39,118,012 | 13,385 | 13,385 |
| UnStakeToken | 52,625,759 | -8,401 | -8,379 |
| **Total** | **146,698,824** | **36,332** | **35,302** |

#### 비교 (Baseline → Triple AVL)

| Metric | Baseline | Triple AVL | Delta | % |
|---|---|---|---|---|
| **StakeToken gas** | 63,407,932 | 54,955,053 | -8,452,879 | **-13.3%** |
| **CollectReward gas** | 50,634,743 | 39,118,012 | -11,516,731 | **-22.7%** |
| **UnStakeToken gas** | 60,302,039 | 52,625,759 | -7,676,280 | **-12.7%** |
| **Total gas** | 174,344,714 | 146,698,824 | **-27,645,890** | **-15.9%** |
| | | | | |
| StakeToken staker bytes | 32,047 | 30,296 | -1,751 | -5.5% |
| CollectReward staker bytes | 10,965 | 13,385 | +2,420 | +22.1% |
| UnStakeToken staker bytes | -7,489 | -8,379 | -890 | — |
| **Total staker bytes** | 35,523 | 35,302 | **-221** | **-0.6%** |

### 분석

1. **Gas 절감이 주된 효과**: 전체 라이프사이클에서 **-15.9% (27.6M gas)** 절감. 3개 트리 탐색/생성이 1개로 줄어 연산 비용이 크게 감소. CollectReward에서 가장 큰 효과 (-22.7%).

2. **Storage 절감은 미미**: 전체 라이프사이클 합산 시 staker bytes_delta는 -221 bytes (-0.6%). `ExternalIncentiveState` 구조체의 HeapItemValue 오버헤드가 키 중복 제거 효과를 상쇄.

3. **Dirty object attribution timing 효과**: StakeToken에서 절감된 bytes가 CollectReward로 이동하는 현상 관찰. 이는 Gno VM의 `FinalizeRealmTransaction`이 dirty object를 해당 TX에 귀속시키는 타이밍이 object graph 구조 변경에 따라 달라지기 때문. 개별 TX별 수치보다 **전체 라이프사이클 합산이 신뢰성 높음**.

---

## 재평가: 적용하지 않는 것을 권장

### `Removed` 필드 도입으로 인한 문제

통합 구현에서 `RemoveExternalIncentiveId()`를 soft delete(`Removed = true`)로 처리하기 위해 `ExternalIncentiveState`에 `Removed bool` 필드를 추가했다. 이 설계는 다음 문제를 초래한다.

**1. Dead entry 영구 누적 (Storage 역효과)**

베이스라인은 `avl.Tree.Remove()`로 노드를 완전히 삭제했다. soft delete 방식은 종료된 인센티브 엔트리가 트리에 영구적으로 잔류하므로, 시간이 지날수록 트리가 비대해진다. 이는 storage 최적화라는 원래 목적과 정면으로 충돌한다.

**2. 순회 오버헤드**

`IterateExternalIncentiveIds`, `GetExternalIncentiveIdList`가 매번 모든 엔트리(removed 포함)를 순회하며 `Removed` 필터링을 수행해야 한다. 오래된 deposit일수록 dead entry가 많아져 불필요한 순회가 증가한다.

**3. 재활성화 버그**

`AddExternalIncentiveId()`는 `getOrCreateIncentiveState()`를 호출하여 기존 state를 반환한다. `state.Removed = false` 재활성화를 명시적으로 추가하지 않으면, 이전에 removed된 ID가 다시 추가되어도 비활성 상태로 남는다. `calculate_pool_position_reward.gno:132-133`의 lazy sync 경로에서 이 시나리오가 발생할 수 있다.

**4. `Removed` 필드의 불필요성**

`Removed` 필드는 통합 트리에서 hard delete 시 `CollectedReward`/`LastCollectTime` 데이터 손실을 방지하기 위해 도입됐다. 그러나 호출 경로 분석 결과, `RemoveExternalIncentiveId()`는 인센티브 종료 후에만 호출되며(`staker.gno:595-596`), 이후 해당 ID의 reward/time 데이터는 다시 접근되지 않는다. 즉 **존재하지 않는 문제를 해결하기 위해 도입된 필드**이다.

### 베이스라인 3-tree 구조의 장점

원래 3개 트리 구조는 각 데이터의 생명주기를 독립적으로 관리할 수 있다:

| 트리 | 데이터 | 생명주기 |
|---|---|---|
| `externalIncentiveIds` | 존재 여부 (bool) | `Remove()`로 완전 삭제 |
| `collectedExternalRewards` | 누적 수령액 (int64) | incentive ID 제거와 독립 |
| `externalRewardLastCollectTimes` | 마지막 수령 시각 (int64) | incentive ID 제거와 독립 |

분리된 구조에서는 soft delete 없이도 각 데이터를 안전하게 관리할 수 있으며, 새로운 상태 플래그나 필터링 로직이 불필요하다.

### 최종 판단

| 관점 | 평가 |
|---|---|
| Storage 절감 | -0.6% — 무의미. soft delete 도입 시 시간 경과에 따라 오히려 역효과 |
| Gas 절감 | -15.9% — 유의미하나 단독으로 구조 변경을 정당화하기 어려움 |
| 코드 복잡도 | 증가 — `Removed` flag, 필터링 로직, 재활성화 처리 |
| 버그 표면적 | 증가 — soft delete 의미론, dead entry 누적, 재활성화 경로 |
| 가역성 | 배포 후 되돌리기 어려움 (직렬화 형식 변경) |

**결론: 이 최적화는 적용하지 않는다.** Gas 절감을 원한다면 트리 구조 변경보다 호출 경로 최적화(불필요한 탐색 제거 등)가 위험이 낮은 접근이다.
