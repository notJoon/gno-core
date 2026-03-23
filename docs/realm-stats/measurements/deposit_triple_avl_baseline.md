# Deposit Triple AVL Merge — Baseline Measurements

**Date:** 2026-03-16
**Branch:** `perf/object-dirty-log` (pre-optimization)
**Plan:** `DEPOSIT_TRIPLE_AVL_MERGE_PLAN.md`

---

## Test Results Summary

All 11 staker-related txtar tests were executed. Results below focus on key staker operations (GAS USED + STORAGE DELTA).

### 1. `staker_storage_staker_stake_with_externals` (Plan 기본 대상)

> 3개 external incentive 생성 후 StakeToken 실행. Deposit에 external incentive state 3개가 추가됨.

| Operation | GAS USED | STORAGE DELTA | STORAGE FEE |
|---|---:|---:|---:|
| CreateExternalIncentive #1 (BAR) | 42,779,350 | 28,742 bytes | 2,874,200 ugnot |
| CreateExternalIncentive #2 (FOO) | 42,579,564 | 12,195 bytes | 1,219,500 ugnot |
| CreateExternalIncentive #3 (WUGNOT) | 42,634,568 | 12,195 bytes | 1,219,500 ugnot |
| GNFT Approve | 4,413,426 | 1,061 bytes | 106,100 ugnot |
| **StakeToken (3 externals)** | **54,663,657** | **35,301 bytes** | **3,530,100 ugnot** |

### 2. `staker_storage_staker_lifecycle`

> SetPoolTier → StakeToken → CollectReward × 2 → UnStakeToken (no external incentives)

| Step | Operation | GAS USED | STORAGE DELTA | STORAGE FEE |
|---|---|---:|---:|---:|
| 1 | SetPoolTier | 38,324,758 | 22,384 bytes | 2,238,400 ugnot |
| — | GNFT Approve | 4,413,390 | 1,061 bytes | 106,100 ugnot |
| 2 | **StakeToken** | **50,881,234** | **22,625 bytes** | **2,262,500 ugnot** |
| 3 | **CollectReward (1st)** | **37,364,786** | **5,706 bytes** | **570,600 ugnot** |
| 4 | CollectReward (2nd) | 32,407,387 | 0 bytes | 0 ugnot |
| 5 | **UnStakeToken** | **51,348,425** | **-3,343 bytes** | — |

### 3. `staker_storage_staker_stake_only`

> SetPoolTier → StakeToken만 실행 (no external incentives)

| Step | Operation | GAS USED | STORAGE DELTA | STORAGE FEE |
|---|---|---:|---:|---:|
| — | SetPoolTier | 38,324,758 | 22,384 bytes | 2,238,400 ugnot |
| — | GNFT Approve | 4,413,390 | 1,061 bytes | 106,100 ugnot |
| — | **StakeToken** | **50,881,264** | **22,625 bytes** | **2,262,500 ugnot** |

### 4. `staker_collect_reward_immediately_after_stake_token`

> SetPoolTier → StakeToken → CollectReward (즉시)

| Step | Operation | GAS USED | STORAGE DELTA | STORAGE FEE |
|---|---|---:|---:|---:|
| — | SetPoolTier | 38,324,758 | 22,384 bytes | 2,238,400 ugnot |
| — | GNFT Approve | 4,413,390 | 1,061 bytes | 106,100 ugnot |
| — | **StakeToken** | **50,881,264** | **22,625 bytes** | **2,262,500 ugnot** |
| — | **CollectReward** | **37,364,822** | **5,706 bytes** | **570,600 ugnot** |

### 5. `staker_staker_create_external_incentive`

> Pool 생성 → Mint → CreateExternalIncentive (single, fee tier 100)

| Step | Operation | GAS USED | STORAGE DELTA | STORAGE FEE |
|---|---|---:|---:|---:|
| — | CreatePool (fee=100) | 35,056,772 | 18,340 bytes | 1,834,000 ugnot |
| — | Mint | 49,832,246 | 29,948 bytes | 2,994,800 ugnot |
| — | **CreateExternalIncentive** | **42,806,446** | **28,742 bytes** | **2,874,200 ugnot** |

### 6. `staker_end_non_existent_external_incentive_fail`

> CreateExternalIncentive 후 존재하지 않는 incentive에 EndExternalIncentive 호출 → 실패 확인

| Step | Operation | GAS USED | STORAGE DELTA | STORAGE FEE |
|---|---|---:|---:|---:|
| — | CreatePool (fee=100) | 35,056,772 | 18,340 bytes | 1,834,000 ugnot |
| — | Mint | 49,856,281 | 29,950 bytes | 2,995,000 ugnot |
| — | **CreateExternalIncentive** | **42,806,446** | **28,742 bytes** | **2,874,200 ugnot** |
| — | EndExternalIncentive (invalid) | FAIL (expected) | — | — |

### 7. `gov_staker_delegate_and_redelegate`

| Step | Operation | GAS USED | STORAGE DELTA | STORAGE FEE |
|---|---|---:|---:|---:|
| — | **Delegate** | **29,001,657** | **17,871 bytes** | **1,787,100 ugnot** |
| — | **Redelegate** | **25,210,471** | **10,398 bytes** | **1,039,800 ugnot** |

### 8. `gov_staker_delegate_and_undelegate`

| Step | Operation | GAS USED | STORAGE DELTA | STORAGE FEE |
|---|---|---:|---:|---:|
| — | **Delegate** | **29,010,915** | **17,871 bytes** | **1,787,100 ugnot** |
| — | **Undelegate** | **25,217,597** | **915 bytes** | **91,500 ugnot** |
| — | CollectUndelegatedGns | 17,096,421 | 0 bytes | 0 ugnot |

### 9. `upgradable_staker_upgrade`

| Step | Operation | GAS USED | STORAGE DELTA | STORAGE FEE |
|---|---|---:|---:|---:|
| — | Initial deploy | 38,793,803 | 69,970 bytes | — |
| — | Upgrade step 1 | 26,280,669 | 4,613 bytes | — |
| — | Upgrade step 2 | 43,663,389 | -1,225 bytes | — |

### 10. `gov_staker_collect_reward_from_launchpad_invalid_caller`

> 에러 경로 테스트 — GAS/STORAGE 수치 없음 (호출 실패 expected)

| Result |
|---|
| PASS (expected failure verified) |

### 11. `upgradable_gov_staker_upgrade`

> **FAIL** — type check 에러로 테스트 실패 (코드 변경과 무관한 기존 문제)

---

## Key Baseline Numbers for Deposit Triple AVL Optimization

최적화 대상인 Deposit 관련 핵심 수치:

| Operation | GAS USED | STORAGE DELTA |
|---|---:|---:|
| StakeToken (no externals) | 50,881,234 | 22,625 bytes |
| StakeToken (3 externals) | 54,663,657 | 35,301 bytes |
| CollectReward (1st call) | 37,364,786 | 5,706 bytes |
| CollectReward (2nd call) | 32,407,387 | 0 bytes |
| UnStakeToken | 51,348,425 | -3,343 bytes |
| CreateExternalIncentive (1st) | 42,806,446 | 28,742 bytes |
| CreateExternalIncentive (2nd) | 42,579,564 | 12,195 bytes |
| CreateExternalIncentive (3rd) | 42,634,568 | 12,195 bytes |

### External Incentive 오버헤드 (StakeToken 기준)

- StakeToken (3 externals) - StakeToken (no externals) = **3,782,423 GAS** / **12,676 bytes** 추가
- Deposit당 external incentive 3개 추가 시 ~4,225 bytes/incentive 추가 storage

### 이전 측정과 비교 (DEPOSIT_TRIPLE_AVL_MERGE_PLAN.md 참고)

| Metric | 이전 측정 (plan 문서) | 현재 baseline |
|---|---:|---:|
| StakeToken GAS (w/ externals) | ~174,344,714 (full lifecycle) | 54,663,657 (StakeToken only) |
| Storage bytes | 35,523 | 35,301 |

> Note: 이전 측정은 full lifecycle gas (여러 operation 합산)이었으며, 현재는 individual operation 단위로 측정됨.

---

## Post-Optimization Measurements (Triple AVL → Single AVL)

**변경 내용:** Deposit의 3개 `*avl.Tree` → 1개 `*avl.Tree` + `*ExternalIncentiveState` struct 통합
**변경 파일:**
- `staker/deposit.gno`: struct 변경, accessor 재작성, raw tree getter/setter 제거
- `staker/getter_utils.gno`: `cloneAvlTree` → `cloneExternalIncentives`
- `staker/v1/type.gno`: `ExternalRewardLastCollectTimes()` nil 체크 제거, `avl` import 제거
- `staker/v1/staker.gno`: `RemoveExternalIncentiveId` 호출 제거

### GAS 비교

| Operation | Before | After | Delta |
|---|---:|---:|---:|
| StakeToken (no externals) | 50,881,234 | 50,797,113 | -84,121 (-0.2%) |
| StakeToken (3 externals) | 54,663,657 | 54,767,086 | +103,429 (+0.2%) |
| **CollectReward (1st, lifecycle)** | **37,364,786** | **31,397,613** | **-5,967,173 (-16.0%)** |
| CollectReward (2nd, lifecycle) | 32,407,387 | 31,397,613 | -1,009,774 (-3.1%) |
| UnStakeToken | 51,348,425 | 50,764,897 | -583,528 (-1.1%) |
| CreateExternalIncentive #1 | 42,806,446 | 42,517,510 | -288,936 (-0.7%) |

### Storage 비교

| Operation | Before | After | Delta |
|---|---:|---:|---:|
| **StakeToken (no externals)** | **22,625 bytes** | **20,991 bytes** | **-1,634 (-7.2%)** |
| StakeToken (3 externals) | 35,301 bytes | 38,207 bytes | +2,906 (+8.2%) |
| **CollectReward (1st, lifecycle)** | **5,706 bytes** | **0 bytes** | **-5,706 (-100%)** |
| UnStakeToken | -3,343 bytes | -5,564 bytes | -2,221 |
| **CreateExternalIncentive #1** | **28,742 bytes** | **11,236 bytes** | **-17,506 (-60.9%)** |
| CreateExternalIncentive #2 | 12,195 bytes | 8,072 bytes | -4,123 (-33.8%) |
| CreateExternalIncentive #3 | 12,195 bytes | 8,059 bytes | -4,136 (-33.9%) |

### 분석

**개선된 항목:**
- CollectReward GAS **-16%**: 3개 tree 탐색 → 1개로 통합 효과가 가장 큼
- CollectReward Storage **-100%** (1st call): 별도 tree에 대한 초기 쓰기 제거
- CreateExternalIncentive #1 Storage **-60.9%**: 3개 tree 초기화 → 1개로 감소
- StakeToken (no externals) Storage **-7.2%**: Deposit 구조체 자체가 2개 tree pointer 감소

**악화된 항목:**
- StakeToken (3 externals) Storage **+8.2%**: `getOrCreateIncentiveState`가 `*ExternalIncentiveState` struct를 heap에 할당하여 추가 Object 생성. 이전에는 `Set(id, true)`로 bool만 저장.

**총평:** CollectReward의 GAS 절감이 가장 유의미하며 (-16%), 이는 가장 빈번하게 호출되는 operation이므로 실질적 영향이 큼. StakeToken with externals의 storage 증가는 struct pointer 오버헤드로 인한 것이나, 전체 lifecycle (Create + Stake + Collect) 관점에서는 순 절감.

### 전체 Lifecycle 비교 (3 externals 시나리오)

| Phase | Before Storage | After Storage |
|---|---:|---:|
| CreateExternalIncentive ×3 | 53,132 bytes | 27,367 bytes |
| StakeToken | 35,301 bytes | 38,207 bytes |
| CollectReward (1st) | 5,706 bytes | 0 bytes |
| **Total** | **94,139 bytes** | **65,574 bytes** |
| **Delta** | | **-28,565 (-30.3%)** |

---

## Post-Optimization Phase 2: Value Type 전환 (Pointer → Value)

**추가 변경:** `DEPOSIT_VALUE_TYPE_PLAN.md` 적용
- `*ExternalIncentiveState` → `ExternalIncentiveState` (value type)
- `*avl.Tree` → `avl.Tree` (value type, zero value 사용)
- `*u256.Uint` → `u256.Uint` (value type)
- setter에 Set-back 패턴 적용, nil 체크 제거, NewDeposit에서 `avl.NewTree()` 제거

### Baseline vs Value Type: GAS 비교

| Operation | Baseline | Value Type | Delta |
|---|---:|---:|---:|
| StakeToken (no externals) | 50,881,234 | 50,781,887 | -99,347 (-0.2%) |
| StakeToken (3 externals) | 54,663,657 | 54,443,289 | -220,368 (-0.4%) |
| CollectReward (1st, lifecycle) | 37,364,786 | 37,272,563 | -92,223 (-0.2%) |
| CollectReward (2nd, lifecycle) | 32,407,387 | 32,328,463 | -78,924 (-0.2%) |
| UnStakeToken | 51,348,425 | 51,223,240 | -125,185 (-0.2%) |
| CreateExternalIncentive #1 | 42,806,446 | 42,517,794 | -288,652 (-0.7%) |

### Baseline vs Value Type: Storage 비교

| Operation | Baseline | Value Type | Delta |
|---|---:|---:|---:|
| **StakeToken (no externals)** | **22,625** | **20,199** | **-2,426 (-10.7%)** |
| **StakeToken (3 externals)** | **35,301** | **31,215** | **-4,086 (-11.6%)** |
| CollectReward (1st, lifecycle) | 5,706 | 5,706 | 0 (0%) |
| UnStakeToken | -3,343 | -917 | +2,426 |
| **CreateExternalIncentive #1** | **28,742** | **11,241** | **-17,501 (-60.9%)** |
| CreateExternalIncentive #2 | 12,195 | 12,201 | +6 (~0%) |
| CreateExternalIncentive #3 | 12,195 | 8,071 | -4,124 (-33.8%) |

### 전체 Lifecycle 비교 (3 externals 시나리오): Baseline vs Value Type

| Phase | Baseline | Value Type | Delta |
|---|---:|---:|---:|
| CreateExternalIncentive ×3 | 53,132 bytes | 31,513 bytes | -21,619 (-40.7%) |
| StakeToken | 35,301 bytes | 31,215 bytes | -4,086 (-11.6%) |
| CollectReward (1st) | 5,706 bytes | 5,706 bytes | 0 |
| **Total** | **94,139 bytes** | **68,434 bytes** | **-25,705 (-27.3%)** |

### Pointer-only (Phase 1) vs Value Type (Phase 2): Storage 비교

| Operation | Phase 1 (Pointer) | Phase 2 (Value) | Delta |
|---|---:|---:|---:|
| StakeToken (no externals) | 20,991 | 20,199 | -792 (-3.8%) |
| **StakeToken (3 externals)** | **38,207** | **31,215** | **-6,992 (-18.3%)** |
| CollectReward (1st) | 0 | 5,706 | +5,706 |
| UnStakeToken | -5,564 | -917 | +4,647 |
| CreateExternalIncentive #1 | 11,236 | 11,241 | +5 (~0%) |
| CreateExternalIncentive #2 | 8,072 | 12,201 | +4,129 |
| CreateExternalIncentive #3 | 8,059 | 8,071 | +12 |

### 분석

**Phase 1 (Pointer) → Phase 2 (Value) 핵심 변화:**
- **StakeToken (3 externals) -18.3%**: `*ExternalIncentiveState` pointer Object 제거 + `*avl.Tree` / `*u256.Uint` Object 제거 효과
- **CollectReward (1st) +5,706**: Phase 1에서 0이었던 것이 다시 원래 수준으로 복원됨. value type tree에서는 첫 mutation 시 tree 내부 노드가 새로 생성되어 storage가 발생
- **CreateExternalIncentive #2 +4,129**: value type tree에서 노드 추가 시 copy semantics로 인한 추가 allocation

**총평:** Value type 전환의 주된 효과는 StakeToken with externals에서의 storage 절감(-18.3%). Phase 1에서 `*ExternalIncentiveState` pointer 때문에 baseline보다 악화되었던 부분(+8.2%)이 해소되어, baseline 대비 **-11.6%** 절감으로 전환됨.

---

## Notes

- `staker_storage_staker_stake_with_externals.txtar`: timestamp을 `1773180800` → `2773180800`으로 수정 (현재 날짜 이후로)
- `upgradable_gov_staker_upgrade`: type check 에러로 기존 실패 상태 (이번 최적화와 무관)
- 모든 측정은 `GNO_REALM_STATS_LOG=stderr` 환경변수로 realm stats 활성화 후 수집
