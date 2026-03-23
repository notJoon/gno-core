# Storage Baseline 측정 결과 (main 브랜치)

**측정일:** 2026-03-16
**브랜치:** `perf/object-dirty-log` (main 기준 코드베이스)
**환경:** `GNO_REALM_STATS_LOG=stderr`

---

## 요약: 유저 엔트리 함수별 Storage 비용

| 함수 | 모듈 | Storage Delta (bytes) | Storage Fee (ugnot) | Gas Used | 비고 |
|------|------|----------------------|---------------------|----------|------|
| **ExactInSwapRoute** (1st) | router | 5,027 | 502,700 | 55,349,909 | 첫 스왑, pool 상태 초기 변경 |
| **ExactInSwapRoute** (reverse) | router | 2,845 | 284,500 | 58,425,464 | 반대 방향 |
| **ExactInSwapRoute** (2nd same) | router | 39 | 3,900 | 55,144,457 | steady-state |
| **Mint** (#1, wide range) | position | 40,763 | 4,076,300 | 56,111,332 | 첫 포지션 |
| **Mint** (#2, narrow range) | position | 39,500 | 3,950,000 | 54,931,184 | 두 번째 포지션 |
| **Mint** (#3, same ticks) | position | 13,106 | 1,310,600 | 54,196,550 | 동일 tick 재사용 |
| **CollectFee** (#1, wide) | position | 2,259 | 225,900 | 44,662,859 | |
| **CollectFee** (#2, narrow) | position | 64 | 6,400 | 44,560,482 | |
| **IncreaseLiquidity** (1st) | position | 81 | 8,100 | 54,215,022 | |
| **IncreaseLiquidity** (2nd) | position | 38 | 3,800 | 54,827,569 | steady-state |
| **DecreaseLiquidity** | position | 5 | 500 | 55,633,212 | |
| **StakeToken** (no externals) | staker | 23,447 | 2,344,700 | 56,966,049 | |
| **StakeToken** (3 externals) | staker | 36,214 | 3,621,400 | 63,966,876 | 외부 인센티브 3개 |
| **CollectReward** (즉시, 0 reward) | staker | 0 | 0 | 35,940,261 | storage 변화 없음 |
| **UnStakeToken** | staker | -7,198 | refund 719,800 | 58,972,883 | storage 감소 |
| **Delegate** | gov/staker | 18,312 | 1,831,200 | 28,791,633 | |
| **Undelegate** | gov/staker | 1,331 | 133,100 | 25,735,880 | |
| **Redelegate** | gov/staker | 10,826 | 1,082,600 | 25,356,868 | |
| **CollectUndelegatedGns** | gov/staker | 0 | 0 | 17,129,302 | storage 변화 없음 |

---

## 셋업/운영 함수 참고 수치

| 함수 | 모듈 | Storage Delta (bytes) | Storage Fee (ugnot) | Gas Used |
|------|------|----------------------|---------------------|----------|
| CreatePool | pool | 20,746 | 2,074,600 | 33,782,761 |
| SetPoolTier | staker | 24,427 | 2,442,700 | 42,222,934 |
| CreateExternalIncentive (BAR) | staker | 11,249 | 1,124,900 | 42,651,955 |
| CreateExternalIncentive (FOO) | staker | 8,072 | 807,200 | 42,844,787 |
| CreateExternalIncentive (WUGNOT) | staker | 12,221 | 1,222,100 | 42,714,680 |

---

## 상세 테스트별 결과

### 1. storage_position_lifecycle

| Step | 설명 | Gas Used | Storage Delta (bytes) | Storage Fee (ugnot) |
|------|------|----------|----------------------|---------------------|
| STEP 1 | CreatePool (WUGNOT:GNS:3000) | 33,782,761 | 20,746 | 2,074,600 |
| STEP 2 | Mint #1 (wide range) | 56,111,332 | 40,763 | 4,076,300 |
| STEP 3 | Mint #2 (narrow range) | 54,931,184 | 39,500 | 3,950,000 |
| STEP 4 | Mint #3 (same ticks as #2) | 54,196,550 | 13,106 | 1,310,600 |
| STEP 5 | Swap (fee 생성용) | 43,422,792 | 12 | 1,200 |
| STEP 6 | CollectFee #1 (wide) | 44,662,859 | 2,259 | 225,900 |
| STEP 7 | CollectFee #2 (narrow) | 44,560,482 | 64 | 6,400 |
| STEP 8 | DecreaseLiquidity #2 | 55,633,212 | 5 | 500 |

### 2. storage_staker_lifecycle

| Step | 설명 | Gas Used | Storage Delta (bytes) | Storage Fee (ugnot) |
|------|------|----------|----------------------|---------------------|
| STEP 1 | SetPoolTier (tier 1) | 42,222,934 | 24,427 | 2,442,700 |
| STEP 2 | StakeToken (#1) | 63,270,474 | 33,099 | 3,309,900 |
| STEP 3 | CollectReward (즉시) | 35,940,261 | 0 | 0 |
| STEP 4 | CollectReward (2회차) | 35,940,261 | 0 | 0 |
| STEP 5 | UnStakeToken (#1) | 58,972,883 | -7,198 | refund 719,800 |

### 3. storage_router_swap_lifecycle

| Step | 설명 | Gas Used | Storage Delta (bytes) | Storage Fee (ugnot) |
|------|------|----------|----------------------|---------------------|
| MEASUREMENT 1 | ExactInSwapRoute (GNS→WUGNOT, 1st) | 55,349,909 | 5,027 | 502,700 |
| MEASUREMENT 2 | ExactInSwapRoute (WUGNOT→GNS, reverse) | 58,425,464 | 2,845 | 284,500 |
| MEASUREMENT 3 | ExactInSwapRoute (GNS→WUGNOT, 2nd) | 55,144,457 | 39 | 3,900 |

### 4. storage_staker_stake_only

| Step | 설명 | Gas Used | Storage Delta (bytes) | Storage Fee (ugnot) |
|------|------|----------|----------------------|---------------------|
| Measured | StakeToken (no externals) | 56,966,049 | 23,447 | 2,344,700 |

### 5. storage_staker_stake_with_externals

| Step | 설명 | Gas Used | Storage Delta (bytes) | Storage Fee (ugnot) |
|------|------|----------|----------------------|---------------------|
| Incentive 1 | CreateExternalIncentive (BAR) | 42,651,955 | 11,249 | 1,124,900 |
| Incentive 2 | CreateExternalIncentive (FOO) | 42,844,787 | 8,072 | 807,200 |
| Incentive 3 | CreateExternalIncentive (WUGNOT) | 42,714,680 | 12,221 | 1,222,100 |
| Measured | StakeToken (3 externals) | 63,966,876 | 36,214 | 3,621,400 |

### 6. storage_position_increase_liquidity

| Step | 설명 | Gas Used | Storage Delta (bytes) | Storage Fee (ugnot) |
|------|------|----------|----------------------|---------------------|
| MEASUREMENT 1 | IncreaseLiquidity (1st) | 54,215,022 | 81 | 8,100 |
| MEASUREMENT 2 | IncreaseLiquidity (2nd) | 54,827,569 | 38 | 3,800 |

### 7. gov_staker_delegate_and_undelegate

| Step | 설명 | Gas Used | Storage Delta (bytes) | Storage Fee (ugnot) |
|------|------|----------|----------------------|---------------------|
| Delegate | Delegate GNS | 28,791,633 | 18,312 | 1,831,200 |
| Undelegate | Undelegate | 25,735,880 | 1,331 | 133,100 |
| Collect | CollectUndelegatedGns | 17,129,302 | 0 | 0 |

### 8. gov_staker_delegate_and_redelegate

| Step | 설명 | Gas Used | Storage Delta (bytes) | Storage Fee (ugnot) |
|------|------|----------|----------------------|---------------------|
| Delegate | Delegate GNS | 28,791,633 | 18,312 | 1,831,200 |
| Redelegate | Redelegate | 25,356,868 | 10,826 | 1,082,600 |

---

## 주요 관찰

1. **Storage 최대 소비자**: Mint (첫 포지션 40KB), StakeToken (23-36KB), SetPoolTier (24KB)
2. **Steady-state 비용 매우 낮음**: 반복 스왑 39B, IncreaseLiquidity 2nd 38B, CollectReward 0B
3. **외부 인센티브 추가 비용**: StakeToken에 인센티브 3개 추가 시 +12.7KB (23,447 → 36,214)
4. **CollectReward 가스 비용**: Storage 0B이지만 35.9M gas — 연산 비용이 지배적
5. **UnStakeToken 환불**: -7,198B storage 감소 → 719,800 ugnot 환불
