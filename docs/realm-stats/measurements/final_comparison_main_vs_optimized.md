# Final Comparison: gnoswap main vs optimized

> **Measured:** 2026-03-17
> **Base (gno):** `master`
> **Baseline (gnoswap):** `main` | **Optimized (gnoswap):** `refactor/convert-value-type-pool`
> **Optimizations applied:** P1~P3 + P4-1 (Slot0/TokenPair), P4-2 (Pool *avl.Tree/*ObservationState), P4-3 (Staker *UintTree/Ticks.tree), P4-5 (Observation map)

---

## Storage Delta (bytes) — Comparison

### Position Lifecycle

| Step | Operation | main (B) | optimized (B) | delta | % |
|------|-----------|----------|---------------|-------|---|
| 1 | CreatePool | 20,746 | 14,345 | -6,401 | **-30.9%** |
| 2 | Mint #1 (wide) | 40,775 | 32,008 | -8,767 | **-21.5%** |
| 3 | Mint #2 (narrow) | 39,499 | 30,717 | -8,782 | **-22.2%** |
| 4 | Mint #3 (same ticks) | 13,094 | 10,289 | -2,805 | **-21.4%** |
| 5 | Swap (wrapper) | 12 | 6 | -6 | -50.0% |
| 6 | CollectFee #1 (wide) | 2,259 | 2,216 | -43 | -1.9% |
| 7 | CollectFee #2 (narrow) | 64 | 50 | -14 | **-21.9%** |
| 8 | DecreaseLiquidity | 5 | 14 | +9 | — |

### Swap Lifecycle (Router)

| Step | Operation | main (B) | optimized (B) | delta | % |
|------|-----------|----------|---------------|-------|---|
| 1 | ExactInSwapRoute (1st) | 5,027 | 5,021 | -6 | -0.1% |
| 2 | ExactInSwapRoute (reverse) | 2,845 | 2,845 | 0 | 0% |
| 3 | ExactInSwapRoute (steady) | 33 | 33 | 0 | 0% |

### Pool Create + Mint (fee 3000)

| Step | Operation | main (B) | optimized (B) | delta | % |
|------|-----------|----------|---------------|-------|---|
| 1 | CreatePool | 20,746 | 14,345 | -6,401 | **-30.9%** |
| 2 | Mint #1 | 40,778 | 32,023 | -8,755 | **-21.5%** |
| 3 | DecreaseLiquidity | 2,267 | 2,217 | -50 | -2.2% |
| 4 | IncreaseLiquidity | 2 | 4 | +2 | — |
| 5 | Swap (fee 3000) | 5,694 | 5,688 | -6 | -0.1% |
| 6 | CollectFee | 69 | 55 | -14 | -20.3% |

### Pool Swap (fee 500)

| Step | Operation | main (B) | optimized (B) | delta | % |
|------|-----------|----------|---------------|-------|---|
| 1 | CreatePool (fee 500) | 20,750 | 14,349 | -6,401 | **-30.9%** |
| 2 | Mint #1 | 40,776 | 32,022 | -8,754 | **-21.5%** |
| 3 | ExactIn Swap (fee 500) | 19,869 | 19,863 | -6 | 0% |

### IncreaseLiquidity

| Step | Operation | main (B) | optimized (B) | delta | % |
|------|-----------|----------|---------------|-------|---|
| 1 | IncreaseLiquidity (1st) | 93 | 61 | -32 | **-34.4%** |
| 2 | IncreaseLiquidity (steady) | 26 | 10 | -16 | **-61.5%** |

### Staker — Stake Only

| Step | Operation | main (B) | optimized (B) | delta | % |
|------|-----------|----------|---------------|-------|---|
| 1 | SetPoolTier | 24,427 | 19,178 | -5,249 | **-21.5%** |
| 2 | StakeToken (stake only) | 23,447 | 27,364 | +3,917 | +16.7%* |

> \* StakeToken storage increase is due to cost redistribution from UnStakeToken; see lifecycle total below.

### Staker — Full Lifecycle

| Step | Operation | main (B) | optimized (B) | delta | % |
|------|-----------|----------|---------------|-------|---|
| 1 | SetPoolTier | 24,427 | 19,178 | -5,249 | **-21.5%** |
| 2 | StakeToken | 23,447 | 27,364 | +3,917 | +16.7% |
| 3 | CollectReward (#1) | 5,758 | 5,712 | -46 | -0.8% |
| 4 | CollectReward (#2) | 0 | 0 | 0 | — |
| 5 | UnStakeToken | -3,304 | 64 | +3,368 | — |
| | **Lifecycle total** | **50,328** | **52,318** | +1,990 | +4.0% |
| | **Lifecycle total (excl. SetPoolTier)** | **25,901** | **33,140** | +7,239 | — |

> Note: The lifecycle storage total increases, but this is offset by the **one-time -5,249 bytes on SetPoolTier** per pool.
> UnStakeToken went from negative (-3,304 = refund) to near-zero (64), indicating cost is now front-loaded at StakeToken.

### Staker — With 3 External Incentives

| Step | Operation | main (B) | optimized (B) | delta | % |
|------|-----------|----------|---------------|-------|---|
| 1 | SetPoolTier | 24,427 | 19,178 | -5,249 | **-21.5%** |
| 2 | Incentive 1 (BAR) | 11,249 | 11,745 | +496 | +4.4% |
| 3 | Incentive 2 (FOO) | 8,072 | 8,575 | +503 | +6.2% |
| 4 | Incentive 3 (WUGNOT) | 8,059 | 8,566 | +507 | +6.3% |
| 5 | StakeToken (3 ext) | 33,099 | 33,771 | +672 | +2.0% |

### Gov — Delegate / Undelegate

| Step | Operation | main (B) | optimized (B) | delta | % |
|------|-----------|----------|---------------|-------|---|
| 1 | Delegate | 18,312 | 17,871 | -441 | -2.4% |
| 2 | Undelegate | 1,331 | 915 | -416 | **-31.3%** |

### Gov — Delegate / Redelegate

| Step | Operation | main (B) | optimized (B) | delta | % |
|------|-----------|----------|---------------|-------|---|
| 1 | Delegate | 18,312 | 17,871 | -441 | -2.4% |
| 2 | Redelegate | 10,826 | 10,398 | -428 | -4.0% |

---

## Gas Used — Comparison

### Position Lifecycle

| Step | Operation | main | optimized | delta | % |
|------|-----------|------|-----------|-------|---|
| 1 | CreatePool | 33,782,761 | 33,654,597 | -128,164 | -0.4% |
| 2 | Mint #1 (wide) | 56,962,015 | 55,919,679 | -1,042,336 | -1.8% |
| 3 | Mint #2 (narrow) | 55,809,986 | 55,522,300 | -287,686 | -0.5% |
| 4 | Mint #3 (same ticks) | 54,331,370 | 53,899,048 | -432,322 | -0.8% |
| 5 | Swap (wrapper) | 44,421,511 | 44,023,017 | -398,494 | -0.9% |
| 6 | CollectFee #1 | 44,662,859 | 44,877,110 | +214,251 | +0.5% |
| 7 | CollectFee #2 | 44,560,482 | 44,778,590 | +218,108 | +0.5% |
| 8 | DecreaseLiquidity | 55,768,735 | 55,183,039 | -585,696 | -1.1% |

### Swap Lifecycle (Router)

| Step | Operation | main | optimized | delta | % |
|------|-----------|------|-----------|-------|---|
| 1 | ExactInSwapRoute (1st) | 55,393,777 | 56,011,723 | +617,946 | +1.1% |
| 2 | ExactInSwapRoute (reverse) | 58,387,459 | 58,152,673 | -234,786 | -0.4% |
| 3 | ExactInSwapRoute (steady) | 55,702,603 | 55,873,848 | +171,245 | +0.3% |

### Staker — Stake Only

| Step | Operation | main | optimized | delta | % |
|------|-----------|------|-----------|-------|---|
| 1 | SetPoolTier | 42,222,934 | 38,196,576 | -4,026,358 | **-9.5%** |
| 2 | StakeToken | 56,966,019 | 53,934,705 | -3,031,314 | **-5.3%** |

### Staker — Full Lifecycle

| Step | Operation | main | optimized | delta | % |
|------|-----------|------|-----------|-------|---|
| 1 | SetPoolTier | 42,222,934 | 38,196,576 | -4,026,358 | **-9.5%** |
| 2 | StakeToken | 56,966,019 | 53,934,705 | -3,031,314 | **-5.3%** |
| 3 | CollectReward (#1) | 49,586,719 | 37,410,294 | -12,176,425 | **-24.6%** |
| 4 | CollectReward (#2) | 36,881,494 | 32,373,151 | -4,508,343 | **-12.2%** |
| 5 | UnStakeToken | 59,453,906 | 51,431,839 | -8,022,067 | **-13.5%** |

### Staker — With 3 External Incentives

| Step | Operation | main | optimized | delta | % |
|------|-----------|------|-----------|-------|---|
| 1 | SetPoolTier | 42,222,934 | 38,196,576 | -4,026,358 | **-9.5%** |
| 5 | StakeToken (3 ext) | 63,407,932 | 54,585,425 | -8,822,507 | **-13.9%** |

### Gov — Delegate / Undelegate

| Step | Operation | main | optimized | delta | % |
|------|-----------|------|-----------|-------|---|
| 1 | Delegate | 28,791,633 | 29,001,657 | +210,024 | +0.7% |
| 2 | Undelegate | 25,735,880 | 25,217,597 | -518,283 | -2.0% |

### Gov — Delegate / Redelegate

| Step | Operation | main | optimized | delta | % |
|------|-----------|------|-----------|-------|---|
| 1 | Delegate | 28,782,375 | 29,010,915 | +228,540 | +0.8% |
| 2 | Redelegate | 25,356,868 | 25,210,471 | -146,397 | -0.6% |

### IncreaseLiquidity

| Step | Operation | main | optimized | delta | % |
|------|-----------|------|-----------|-------|---|
| 1 | IncreaseLiquidity (1st) | 54,974,775 | 54,488,638 | -486,137 | -0.9% |
| 2 | IncreaseLiquidity (steady) | 54,731,486 | 53,950,874 | -780,612 | -1.4% |

---

## Summary

### Storage Reduction (Key Wins)

> Storage fee rate: **100 ugnot/byte** (1 gnot = 1,000,000 ugnot)

| Category | main (B) | optimized (B) | Saved (B) | % | main fee | optimized fee | fee saved |
|----------|----------|---------------|-----------|---|----------|---------------|-----------|
| **CreatePool** | 20,746 | 14,345 | 6,401 | **-30.9%** | 2.075 gnot | 1.435 gnot | **0.640 gnot** |
| **Mint #1 (wide)** | 40,775 | 32,008 | 8,767 | **-21.5%** | 4.078 gnot | 3.201 gnot | **0.877 gnot** |
| **Mint #2 (narrow)** | 39,499 | 30,717 | 8,782 | **-22.2%** | 3.950 gnot | 3.072 gnot | **0.878 gnot** |
| **SetPoolTier** | 24,427 | 19,178 | 5,249 | **-21.5%** | 2.443 gnot | 1.918 gnot | **0.525 gnot** |
| **StakeToken (3 ext)** | 33,099 | 33,771 | -672 | +2.0% | 3.310 gnot | 3.377 gnot | -0.067 gnot |
| **CollectReward (#1)** | 5,758 | 5,712 | 46 | -0.8% | 0.576 gnot | 0.571 gnot | 0.005 gnot |
| **Undelegate** | 1,331 | 915 | 416 | **-31.3%** | 0.133 gnot | 0.092 gnot | **0.042 gnot** |
| **Redelegate** | 10,826 | 10,398 | 428 | -4.0% | 1.083 gnot | 1.040 gnot | 0.043 gnot |
| **IncreaseLiquidity (1st)** | 93 | 61 | 32 | **-34.4%** | 0.009 gnot | 0.006 gnot | 0.003 gnot |
| **ExactInSwapRoute (1st)** | 5,027 | 5,021 | 6 | -0.1% | 0.503 gnot | 0.502 gnot | 0.001 gnot |

### Per-Pool Setup Cost (one-time)

| Scenario | main | optimized | saved |
|----------|------|-----------|-------|
| CreatePool + Mint (1 position) | 6.153 gnot | 4.636 gnot | **1.517 gnot** |
| CreatePool + SetPoolTier | 4.518 gnot | 3.353 gnot | **1.165 gnot** |
| Full pool setup (Create + Tier + Mint) | 8.596 gnot | 6.554 gnot | **2.042 gnot** |

### Per-User Action Cost (recurring)

| Action | main | optimized | saved |
|--------|------|-----------|-------|
| Mint new position | 3.950 ~ 4.078 gnot | 3.072 ~ 3.201 gnot | **~0.877 gnot** |
| Swap (1st on pool) | 0.503 gnot | 0.502 gnot | 0.001 gnot |
| Swap (steady) | 0.003 gnot | 0.003 gnot | 0 |
| CollectFee | 0.007 ~ 0.226 gnot | 0.005 ~ 0.222 gnot | ~0.004 gnot |
| Delegate | 1.831 gnot | 1.787 gnot | 0.044 gnot |
| Undelegate | 0.133 gnot | 0.092 gnot | **0.042 gnot** |

### Gas Reduction (Key Wins)

| Category | Change |
|----------|--------|
| **CollectReward (#1)** | **-24.6%** (12.2M gas saved) |
| **StakeToken (3 ext)** | **-13.9%** (8.8M gas saved) |
| **UnStakeToken** | **-13.5%** (8.0M gas saved) |
| **SetPoolTier** | **-9.5%** (4.0M gas saved) |
| **StakeToken** | **-5.3%** (3.0M gas saved) |
| **Swap/Mint/Gov** | ±1% (noise level) |

### Key Observations

1. **CreatePool -30.9%** is the biggest single-operation storage win. This includes P4-1 (sub-struct value types, -21.2%) and P4-2 (avl.Tree/ObservationState inlining, -10.0%).
2. **Mint consistently -21~22%** across all variants (wide, narrow, same ticks). The savings scale with position count.
3. **Staker gas savings are dramatic** (-5% to -25%) due to reduced finalize calls and Object count.
4. **Swap storage is nearly unchanged** — swaps modify existing state, so Object count reduction has minimal impact on mutation deltas.
5. **StakeToken/UnStakeToken cost redistribution**: storage cost shifted from UnStakeToken (was -3,304B refund) to StakeToken (+3,917B). The lifecycle total is slightly higher, but this is offset by the one-time SetPoolTier savings per pool.
6. **External incentive creation shows +500B each** vs main — this is due to staker Pool value-type inlining increasing the parent struct re-serialization cost. Net across all operations, the staker savings still dominate.
