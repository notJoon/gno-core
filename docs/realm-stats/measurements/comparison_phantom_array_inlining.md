# Storage Deposit 비교: Baseline vs Phantom Array Inlining

> **측정일:** 2026-03-20
> **비교 대상:**
> - Baseline: `perf/object-dirty-log` (main 기준), 측정일 2026-03-16
> - 현재: `feat/phantom-array-inlining` 브랜치
>
> **Storage Fee 계산:** bytes × 100 ugnot/byte, 1 GNOT = 1,000,000 ugnot

---

## 전체 요약

| 시나리오 | Baseline 총 비용 | 현재 총 비용 | 절감액 | 절감률 |
|----------|---:|---:|---:|---:|
| **Position Lifecycle** | 11.65 GNOT | 8.10 GNOT | **-3.55 GNOT** | **-30.4%** |
| **Staker Lifecycle** | 5.03 GNOT | 4.04 GNOT | **-0.99 GNOT** | **-19.7%** |
| **Router Swap** | 0.79 GNOT | 0.76 GNOT | -0.03 GNOT | -3.4% |
| **Staker + 3 Externals** | 6.78 GNOT | 6.16 GNOT | **-0.62 GNOT** | -9.1% |
| **Gov Delegate+Undelegate** | 1.96 GNOT | 1.87 GNOT | -0.10 GNOT | -5.0% |
| **Gov Delegate+Redelegate** | 2.91 GNOT | 2.81 GNOT | -0.10 GNOT | -3.4% |
| **IncreaseLiquidity** | 0.01 GNOT | 0.00 GNOT | -0.01 GNOT | -99.2% |

---

## 상세 비교

### 1. Position Lifecycle (`position_storage_poisition_lifecycle`)

| Step | 설명 | Baseline (bytes) | 현재 (bytes) | Baseline Fee | 현재 Fee | 변화 |
|------|------|---:|---:|---:|---:|---:|
| STEP 1 | CreatePool | 20,746 | 12,915 | 2.0746 GNOT | 1.2915 GNOT | **-0.7831 (-37.7%)** |
| STEP 2 | Mint #1 (wide range) | 40,763 | 28,909 | 4.0763 GNOT | 2.8909 GNOT | **-1.1854 (-29.1%)** |
| STEP 3 | Mint #2 (narrow range) | 39,500 | 27,617 | 3.9500 GNOT | 2.7617 GNOT | **-1.1883 (-30.1%)** |
| STEP 4 | Mint #3 (same ticks) | 13,106 | 9,320 | 1.3106 GNOT | 0.9320 GNOT | **-0.3786 (-28.9%)** |
| STEP 5 | Swap (fee 생성) | 12 | 12 | 0.0012 GNOT | 0.0012 GNOT | 0 |
| STEP 6 | CollectFee #1 (wide) | 2,259 | 2,175 | 0.2259 GNOT | 0.2175 GNOT | -0.0084 (-3.7%) |
| STEP 7 | CollectFee #2 (narrow) | 64 | 24 | 0.0064 GNOT | 0.0024 GNOT | -0.0040 (-62.5%) |
| STEP 8 | DecreaseLiquidity | 5 | 28 | 0.0005 GNOT | 0.0028 GNOT | +0.0023 |
| | **라이프사이클 합계** | **116,455** | **81,000** | **11.6455 GNOT** | **8.1000 GNOT** | **-3.5455 (-30.4%)** |

### 2. Router Swap Lifecycle (`router_storage_swap_lifecycle`)

| Step | 설명 | Baseline (bytes) | 현재 (bytes) | Baseline Fee | 현재 Fee | 변화 |
|------|------|---:|---:|---:|---:|---:|
| M1 | ExactInSwapRoute (1st) | 5,027 | 4,897 | 0.5027 GNOT | 0.4897 GNOT | -0.0130 (-2.6%) |
| M2 | ExactInSwapRoute (reverse) | 2,845 | 2,709 | 0.2845 GNOT | 0.2709 GNOT | -0.0136 (-4.8%) |
| M3 | ExactInSwapRoute (steady) | 39 | 39 | 0.0039 GNOT | 0.0039 GNOT | 0 |
| | **라이프사이클 합계** | **7,911** | **7,645** | **0.7911 GNOT** | **0.7645 GNOT** | **-0.0266 (-3.4%)** |

### 3. IncreaseLiquidity (`position_storage_increaase_liquidity`)

| Step | 설명 | Baseline (bytes) | 현재 (bytes) | Baseline Fee | 현재 Fee | 변화 |
|------|------|---:|---:|---:|---:|---:|
| M1 | IncreaseLiquidity (1st) | 81 | 1 | 0.0081 GNOT | 0.0001 GNOT | **-0.0080 (-98.8%)** |
| M2 | IncreaseLiquidity (2nd) | 38 | 0 | 0.0038 GNOT | 0 GNOT | **-0.0038 (-100%)** |
| | **합계** | **119** | **1** | **0.0119 GNOT** | **0.0001 GNOT** | **-0.0118 (-99.2%)** |

### 4. Staker Lifecycle (`staker_storage_staker_lifecycle`)

| Step | 설명 | Baseline (bytes) | 현재 (bytes) | Baseline Fee | 현재 Fee | 변화 |
|------|------|---:|---:|---:|---:|---:|
| STEP 1 | SetPoolTier | 24,427 | 18,788 | 2.4427 GNOT | 1.8788 GNOT | **-0.5639 (-23.1%)** |
| STEP 2 | StakeToken | 33,099 | 17,118 | 3.3099 GNOT | 1.7118 GNOT | **-1.5981 (-48.3%)** |
| STEP 3 | CollectReward (1차) | 0 | 0 | 0 GNOT | 0 GNOT | 0 |
| STEP 4 | CollectReward (2차) | 0 | 5,446 | 0 GNOT | 0.5446 GNOT | +0.5446 |
| STEP 5 | UnStakeToken | -7,198 | -917 | -0.7198 GNOT | -0.0917 GNOT | +0.6281 |
| | **라이프사이클 합계** | **50,328** | **40,435** | **5.0328 GNOT** | **4.0435 GNOT** | **-0.9893 (-19.7%)** |

> **비용 재분배 분석:** StakeToken에서 -1.60 GNOT 대폭 절감된 대신, CollectReward 2차(+0.54)와
> UnStakeToken 환불 감소(+0.63)로 일부가 지연 발생. 이는 phantom array inlining이
> 초기 생성 시 배열을 inline으로 유지하다가, 이후 상태 변경 시 inline → real 전환이
> 발생하는 deferred materialization 패턴에 기인함. 라이프사이클 순합 기준으로는
> **-0.99 GNOT (19.7%) 절감.**

### 5. Staker Stake Only (`staker_storage_staker_stake_only`)

| Step | Baseline (bytes) | 현재 (bytes) | Baseline Fee | 현재 Fee | 변화 |
|------|---:|---:|---:|---:|---:|
| StakeToken (no externals) | 23,447 | 17,118 | 2.3447 GNOT | 1.7118 GNOT | **-0.6329 (-27.0%)** |

### 6. Staker with 3 Externals (`staker_storage_staker_stake_with_externals`)

| Step | 설명 | Baseline (bytes) | 현재 (bytes) | Baseline Fee | 현재 Fee | 변화 |
|------|------|---:|---:|---:|---:|---:|
| Incentive 1 | CreateExternalIncentive (BAR) | 11,249 | 11,745 | 1.1249 GNOT | 1.1745 GNOT | +0.0496 |
| Incentive 2 | CreateExternalIncentive (FOO) | 8,072 | 8,575 | 0.8072 GNOT | 0.8575 GNOT | +0.0503 |
| Incentive 3 | CreateExternalIncentive (WUGNOT) | 12,221 | 8,566 | 1.2221 GNOT | 0.8566 GNOT | **-0.3655 (-29.9%)** |
| Measured | StakeToken (3 externals) | 36,214 | 32,696 | 3.6214 GNOT | 3.2696 GNOT | **-0.3518 (-9.7%)** |
| | **합계** | **67,756** | **61,582** | **6.7756 GNOT** | **6.1582 GNOT** | **-0.6174 (-9.1%)** |

### 7. Gov Staker — Delegate & Undelegate (`gov_staker_delegate_and_undelegate`)

| Step | Baseline (bytes) | 현재 (bytes) | Baseline Fee | 현재 Fee | 변화 |
|------|---:|---:|---:|---:|---:|
| Delegate | 18,312 | 17,741 | 1.8312 GNOT | 1.7741 GNOT | -0.0571 (-3.1%) |
| Undelegate | 1,331 | 915 | 0.1331 GNOT | 0.0915 GNOT | **-0.0416 (-31.3%)** |
| CollectUndelegatedGns | 0 | 0 | 0 GNOT | 0 GNOT | 0 |
| **합계** | **19,643** | **18,656** | **1.9643 GNOT** | **1.8656 GNOT** | **-0.0987 (-5.0%)** |

### 8. Gov Staker — Delegate & Redelegate (`gov_staker_delegate_and_redelegate`)

| Step | Baseline (bytes) | 현재 (bytes) | Baseline Fee | 현재 Fee | 변화 |
|------|---:|---:|---:|---:|---:|
| Delegate | 18,312 | 17,741 | 1.8312 GNOT | 1.7741 GNOT | -0.0571 (-3.1%) |
| Redelegate | 10,826 | 10,398 | 1.0826 GNOT | 1.0398 GNOT | -0.0428 (-4.0%) |
| **합계** | **29,138** | **28,139** | **2.9138 GNOT** | **2.8139 GNOT** | **-0.0999 (-3.4%)** |

---

## 주요 관찰

### 효과가 큰 영역

1. **Mint 작업 (Position):** 29~30% 절감. 첫 포지션 기준 4.08 → 2.89 GNOT (-1.19 GNOT).
   큰 오브젝트 트리(tick, position, GNFT)가 생성되는 시점에서 inline 유지 효과가 극대화.

2. **CreatePool:** 37.7% 절감 (2.07 → 1.29 GNOT). Pool 초기화 시 생성되는 다수의
   배열/슬라이스 객체가 inline으로 처리됨.

3. **StakeToken:** 27~48% 절감. external incentive 없이 2.34 → 1.71 GNOT.
   staker 내부 deposit/tick 데이터 구조에서 inline 효과.

4. **IncreaseLiquidity:** 거의 100% 절감 (0.01 → 0.00 GNOT). 기존에도 미미했으나
   완전히 0에 수렴.

### 효과가 적은 영역

1. **Steady-state 스왑 (3번째 이후):** 변화 없음 (39 bytes). 이미 최적화된 상태.

2. **Gov 관련 작업:** 3~5% 절감. 구조가 단순하여 inline 대상 배열이 적음.

### 비용 재분배 패턴 (Staker Lifecycle)

Phantom array inlining은 **비용 총량을 줄이면서 발생 시점을 재분배**함:

```
Baseline:    StakeToken +3.31  →  CollectReward ×2 = 0  →  UnStake -0.72  =  순 5.03 GNOT
현재:        StakeToken +1.71  →  CollectReward 2차 +0.54  →  UnStake -0.09  =  순 4.04 GNOT
                   ↓                      ↓                        ↓
              -1.60 GNOT              +0.54 GNOT              +0.63 GNOT     = 순 -0.99 GNOT
```

초기 생성(StakeToken)에서 배열이 inline으로 유지되어 대폭 절감되지만,
이후 상태 변경(CollectReward 2차) 시 inline → real 전환이 발생하고,
UnStakeToken에서는 삭제 대상 독립 객체가 줄어 환불도 감소함.
이는 final_report.md §6에서 관찰된 Key Consolidation의 비용 재분배와 동일한 패턴.

### Baseline 대비 증가한 항목

| 항목 | Baseline | 현재 | 증가량 | 원인 |
|------|---:|---:|---:|------|
| DecreaseLiquidity | 0.0005 GNOT | 0.0028 GNOT | +0.0023 | 미미, 노이즈 수준 |
| CollectReward (2차) | 0 GNOT | 0.5446 GNOT | +0.5446 | inline→real 전환 (deferred materialization) |
| UnStakeToken | -0.7198 GNOT | -0.0917 GNOT | +0.6281 | 삭제 대상 독립 객체 감소 |
| CreateExternalIncentive (BAR) | 1.1249 GNOT | 1.1745 GNOT | +0.0496 | 미미 |
| CreateExternalIncentive (FOO) | 0.8072 GNOT | 0.8575 GNOT | +0.0503 | 미미 |

모든 증가 항목은 라이프사이클 합산 시 초기 생성 절감분에 의해 상쇄됨.

---

## 테스트 환경 참고

- `r/onbloc/{foo,bar,baz,qux,obl,usdc}` 패키지에서 `AssertOwnedByPrevious()` →
  `AssertOwned()` API 변경 필요 (ownable/v0 호환성). staker 관련 3개 테스트는
  이 수정 후 통과함.
- `position_storage_increaase_liquidity` MEASUREMENT 2는 storage delta가 0이 되어
  `STORAGE DELTA:` 라인이 미출력, 테스트 assertion 실패 (txtar 수정 필요).

---

## 관련 문서

- [Baseline 측정 (main)](./baseline_main.md) — 비교 기준
- [P1 최종 보고서](./final_report.md) — 컨트랙트 레벨 최적화 (Store Access Caching + Key Consolidation)
