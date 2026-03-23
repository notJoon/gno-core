# P4-4: ExternalIncentive 포인터 저장 → value type 저장

**Priority:** 4
**상태:** 완료
**실측 절감:** StakeToken -2,396 bytes (-11.9%), CreateExternalIncentive ×3 -2,617 bytes (-8.3%)
**변경 범위:** `staker/pool.gno`, `staker/v1/` 6개 파일 (type assertion + store-back 변경)
**패턴:** avl.Tree에 `*Struct` 대신 `Struct` value로 저장하여 HeapItemValue 제거
**의존:** P4-1~3과 독립

---

## 문제 정의

`ExternalIncentive` struct는 모든 필드가 primitive type (int64, string, bool, address)이다:

```go
type ExternalIncentive struct {
    incentiveId             string
    startTimestamp          int64
    endTimestamp            int64
    createdHeight           int64
    createdTimestamp        int64
    depositGnsAmount        int64
    targetPoolPath          string
    rewardToken             string
    totalRewardAmount       int64
    rewardAmount            int64
    rewardPerSecond         int64
    distributedRewardAmount int64
    refundee                address
    refunded                bool
}
```

포인터 필드가 **전혀 없으므로**, value type으로 저장해도 내부 구조 변경이 불필요하다.

### 변환 전 저장 방식

```go
// avl.Tree에 *ExternalIncentive (pointer)로 저장
func (i *Incentives) SetIncentive(id string, incentive *ExternalIncentive) {
    i.incentives.Set(id, incentive)  // interface{} 안에 *ExternalIncentive
}
```

Gno VM에서 `interface{}`에 pointer를 저장하면:
```
avl.Tree node → interface{} → *ExternalIncentive (HeapItemValue) → ExternalIncentive (StructValue)
```

value로 저장하면:
```
avl.Tree node → interface{} → ExternalIncentive (StructValue, inline)
```

**HeapItemValue 1개 제거/incentive.**

---

## 적용된 변경 사항

### 변경 파일 (6개)

| 파일 | 변경 내용 |
|------|----------|
| `staker/pool.gno` | `SetIncentive()` → `*incentive`, `Incentive()` → value assertion + `&v`, `IterateIncentives()` → value assertion + `&v` |
| `staker/v1/staker.gno` | `ExternalIncentives.get()` → value assertion + `&v`, `.set()` → `*incentive`; `collectRewardInternal()`에서 per-pool tree 동기화 추가 |
| `staker/v1/reward_calculation_incentives.gno` | `retrieveIncentive()` → value assertion + `&v`, `create()` / `update()` → `*incentive` |
| `staker/v1/reward_calculation_pool.gno` | `IsExternallyIncentivizedPool()` iteration → value assertion + `&v` |
| `staker/v1/getter.gno` | `getIncentive()` → value assertion + `&v`, `GetExternalIncentiveByPoolPath()` → value assertion |
| `staker/v1/external_incentive.gno` | `CreateExternalIncentive()` store-level `Set()` → `*incentive` |

### 변경 패턴

**저장 시** — dereference하여 value로 저장:
```go
// BEFORE
i.incentives.Set(id, incentive)   // stores *ExternalIncentive

// AFTER
i.incentives.Set(id, *incentive)  // stores ExternalIncentive (value)
```

**조회 시** — value assertion 후 address 취득:
```go
// BEFORE
incentive, ok := value.(*ExternalIncentive)

// AFTER
v, ok := value.(ExternalIncentive)
incentive := &v
```

### collectRewardInternal per-pool tree 동기화

value type 저장에서는 전역 tree와 pool별 tree가 **독립 복사본**을 가진다.
`collectRewardInternal`에서 incentive를 mutation한 후 전역 tree만 업데이트하면
pool별 tree에 변경이 반영되지 않는 문제가 발생한다.

```go
// v1/staker.gno — collectRewardInternal
externalIncentives.set(incentiveId, incentive)  // 전역 tree 업데이트

// 추가: per-pool tree도 동기화
if pool, ok := pools.Get(deposit.TargetPoolPath()); ok {
    NewPoolResolver(pool).IncentivesResolver().update(incentive.Refundee(), incentive)
}
```

---

## 측정 결과

**Baseline:** `baseline_tick_incentives_value_type.md` (P4 전환 전)

### T1: staker_storage_staker_lifecycle

| Operation | Baseline | After P4-4 | 차이 | % |
|-----------|---------|-----------|------|---|
| SetPoolTier | +22,384 B | +21,179 B | **-1,205** | -5.4% |
| StakeToken | +20,199 B | +17,803 B | **-2,396** | -11.9% |
| CollectReward #1 (즉시) | 0 B | 0 B | 0 | — |
| CollectReward #2 (1 block later) | +5,706 B | +5,706 B | 0 | — |
| UnStakeToken | -917 B | -917 B | 0 | — |

#### T1 Staker-only Breakdown

| Operation | Baseline | After P4-4 | 차이 |
|-----------|---------|-----------|------|
| SetPoolTier (staker) | +22,384 | +21,179 | -1,205 |
| StakeToken (staker) | +19,147 | +16,751 | **-2,396** |
| UnStakeToken (staker) | -895 | -895 | 0 |

### T2: staker_storage_staker_stake_only

| Operation | Baseline | After P4-4 | 차이 | % |
|-----------|---------|-----------|------|---|
| StakeToken (total) | +20,199 B | +17,803 B | **-2,396** | -11.9% |
| StakeToken (staker) | +19,147 | +16,751 | **-2,396** | -12.5% |

### T3: staker_storage_staker_stake_with_externals

| Operation | Baseline | After P4-4 | 차이 | % |
|-----------|---------|-----------|------|---|
| CreateExtIncentive #1 (BAR) | +11,236 B | +11,745 B | +509 | — |
| CreateExtIncentive #2 (FOO) | +12,196 B | +8,575 B | -3,621 | — |
| CreateExtIncentive #3 (WUGNOT) | +8,071 B | +8,566 B | +495 | — |
| **3개 합계** | **+31,503 B** | **+28,886 B** | **-2,617** | **-8.3%** |
| StakeToken (w/ 3 externals) | +36,167 B | +33,771 B | **-2,396** | -6.6% |

> CreateExternalIncentive 개별 값이 들쭉날쭉한 이유: avl.Tree rebalancing으로 노드 이동 위치가 달라짐. 3개 합산이 의미있는 비교 단위.

#### T3 Staker-only Breakdown

| Operation | Baseline | After P4-4 | 차이 |
|-----------|---------|-----------|------|
| CreateExtIncentive #1 (staker) | +7,151 | +7,660 | +509 |
| CreateExtIncentive #2 (staker) | — | +6,541 | — |
| CreateExtIncentive #3 (staker) | — | +6,537 | — |
| StakeToken w/ ext (staker) | +35,115 | +32,719 | **-2,396** |

### T4: staker_staker_create_external_incentive (first for pool)

| Operation | Baseline | After P4-4 | 차이 | % |
|-----------|---------|-----------|------|---|
| CreateExternalIncentive (total) | +28,742 B | +28,043 B | **-699** | -2.4% |
| CreateExternalIncentive (staker) | +24,662 | +23,963 | **-699** | -2.8% |

### T5: staker_collect_reward_immediately_after_stake_token

| Operation | Baseline | After P4-4 | 비고 |
|-----------|---------|-----------|------|
| SetPoolTier | +22,384 B | +21,179 B | -1,205 |
| StakeToken | +20,199 B | +17,803 B | -2,396 |
| CollectReward | 0 B | +5,706 B | ¹ |

> ¹ CollectReward 증가는 P4-4와 무관. Baseline은 동일 블록에서 실행(HEIGHT 동일 → dirty 없음),
> 현재는 블록이 전진하여 T1 CollectReward #2와 동일한 5,706B 패턴.
> T1에서 동일 조건의 CollectReward #2는 baseline/after 모두 5,706B로 일치.

---

## 분석

### 예상 대비 실측

초기 예상은 HeapItemValue 제거로 incentive당 ~100 bytes 절감이었으나, 실측 결과는 훨씬 큰 절감을 보였다.

| 지표 | 초기 예상 | 실측 | 배율 |
|------|---------|------|------|
| StakeToken (no ext) | ~100 B | -2,396 B | ~24× |
| StakeToken (3 ext) | ~295 B | -2,396 B | ~8× |
| CreateExtIncentive ×3 | ~300 B | -2,617 B | ~9× |

### 예상보다 큰 절감의 원인

HeapItemValue wrapper 제거(~100B/incentive) 외에, value type 저장이 dirty marking 전파에도 영향:

1. **HeapItemValue 제거**: pointer 저장 시 `interface{}` → `*ExternalIncentive` → `ExternalIncentive` 3단계가 `interface{}` → `ExternalIncentive` 2단계로 축소
2. **Dirty marking 감소**: pointer 저장 시 HeapItemValue가 별도 heap object로 존재하여 dirty marking 대상이 됨. value 저장 시 해당 object가 사라져 dirty 전파 체인이 짧아짐
3. **StakeToken 경로의 incentive 조회**: StakeToken이 `IsExternallyIncentivizedPool()`을 통해 incentive tree를 순회하므로, 읽기 과정에서도 dirty marking 대상이 줄어들어 절감 효과 증폭

---

## 핵심 요약

| 지표 | 절감 |
|------|------|
| StakeToken (no externals) | **-2,396 bytes (-11.9%)** |
| StakeToken (w/ 3 externals) | **-2,396 bytes (-6.6%)** |
| CreateExternalIncentive ×3 합계 | **-2,617 bytes (-8.3%)** |
| CreateExternalIncentive (first, single) | **-699 bytes (-2.4%)** |
| SetPoolTier | **-1,205 bytes (-5.4%)** |
| CollectReward / UnStakeToken | 변화 없음 |

---

## 측정 방법

```bash
GNO_REALM_STATS_LOG=stderr go test ./gno.land/pkg/integration/ -run "TestTestdata/staker_storage_staker_lifecycle" -v -count=1
GNO_REALM_STATS_LOG=stderr go test ./gno.land/pkg/integration/ -run "TestTestdata/staker_storage_staker_stake_only" -v -count=1
GNO_REALM_STATS_LOG=stderr go test ./gno.land/pkg/integration/ -run "TestTestdata/staker_storage_staker_stake_with_externals" -v -count=1
GNO_REALM_STATS_LOG=stderr go test ./gno.land/pkg/integration/ -run "TestTestdata/staker_staker_create_external_incentive" -v -count=1
GNO_REALM_STATS_LOG=stderr go test ./gno.land/pkg/integration/ -run "TestTestdata/staker_collect_reward_immediately_after_stake_token" -v -count=1
```
