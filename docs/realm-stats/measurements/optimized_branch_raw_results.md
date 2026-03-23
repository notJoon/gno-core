# Optimized Branch Raw Measurement Results

**Date:** 2026-03-17
**Branch:** `refactor/convert-value-type-pool` (gnoswap) on `dev/jae/gas-model-improvements` (gno)
**Applied:** All optimizations (P1~P3 + P4-1, P4-2, P4-3, P4-5)

---

## 1. pool_create_pool_and_mint

| Step | Operation | GAS USED | STORAGE DELTA | TOTAL TX COST |
|------|-----------|----------|---------------|---------------|
| 1 | CreatePool (fee 3000) | 33,654,597 | 14,345 B | 11,434,500 |
| 2 | Mint #1 (wide) | 56,758,636 | 32,023 B | 103,202,300 |
| 3 | DecreaseLiquidity | 59,038,166 | 2,217 B | 100,221,700 |
| 4 | IncreaseLiquidity | 54,457,367 | 4 B | 100,000,400 |
| 5 | Swap (fee 3000) | 43,026,285 | 5,688 B | 100,568,800 |
| 6 | CollectFee | 44,532,463 | 55 B | 100,005,500 |

---

## 2. pool_swap_wugnot_gns_tokens

| Step | Operation | GAS USED | STORAGE DELTA | TOTAL TX COST |
|------|-----------|----------|---------------|---------------|
| 1 | CreatePool (fee 500) | 34,048,121 | 14,349 B | 11,434,900 |
| 2 | Mint #1 | 57,039,538 | 32,022 B | 103,202,200 |
| 3 | ExactIn Swap (fee 500) | 71,861,407 | 19,863 B | 101,986,300 |

---

## 3. position_storage_poisition_lifecycle

| Step | Operation | GAS USED | STORAGE DELTA | TOTAL TX COST |
|------|-----------|----------|---------------|---------------|
| 1 | CreatePool | 33,654,597 | 14,345 B | 11,434,500 |
| 2 | Mint #1 (wide) | 55,919,679 | 32,008 B | 103,200,800 |
| 3 | Mint #2 (narrow) | 55,522,300 | 30,717 B | 103,071,700 |
| 4 | Mint #3 (same ticks) | 53,899,048 | 10,289 B | 101,028,900 |
| 5 | Swap (wrapper) | 44,023,017 | 6 B | 100,000,600 |
| 6 | CollectFee #1 (wide) | 44,877,110 | 2,216 B | 100,221,600 |
| 7 | CollectFee #2 (narrow) | 44,778,590 | 50 B | 100,005,000 |
| 8 | DecreaseLiquidity | 55,183,039 | 14 B | 100,001,400 |

---

## 4. position_storage_increaase_liquidity

| Step | Operation | GAS USED | STORAGE DELTA | TOTAL TX COST |
|------|-----------|----------|---------------|---------------|
| 1 | IncreaseLiquidity (1st) | 54,488,638 | 61 B | 100,006,100 |
| 2 | IncreaseLiquidity (steady) | 53,950,874 | 10 B | 100,001,000 |

---

## 5. router_storage_swap_lifecycle

| Step | Operation | GAS USED | STORAGE DELTA | TOTAL TX COST |
|------|-----------|----------|---------------|---------------|
| 1 | ExactInSwapRoute (1st) | 56,011,723 | 5,021 B | 100,502,100 |
| 2 | ExactInSwapRoute (reverse) | 58,152,673 | 2,845 B | 100,284,500 |
| 3 | ExactInSwapRoute (steady) | 55,873,848 | 33 B | 100,003,300 |

---

## 6. staker_storage_staker_stake_only

| Step | Operation | GAS USED | STORAGE DELTA | TOTAL TX COST |
|------|-----------|----------|---------------|---------------|
| 1 | SetPoolTier | 38,196,576 | 19,178 B | 11,917,800 |
| 2 | StakeToken (stake only) | 53,934,705 | 27,364 B | 12,736,400 |

---

## 7. staker_storage_staker_lifecycle

| Step | Operation | GAS USED | STORAGE DELTA | TOTAL TX COST |
|------|-----------|----------|---------------|---------------|
| 1 | SetPoolTier | 38,196,576 | 19,178 B | 11,917,800 |
| 2 | StakeToken | 53,934,705 | 27,364 B | 12,736,400 |
| 3 | CollectReward (#1) | 37,410,294 | 5,712 B | 10,571,200 |
| 4 | CollectReward (#2) | 32,373,151 | 0 B | — |
| 5 | UnStakeToken | 51,431,839 | 64 B | 10,006,400 |

---

## 8. staker_storage_staker_stake_with_externals

| Step | Operation | GAS USED | STORAGE DELTA | TOTAL TX COST |
|------|-----------|----------|---------------|---------------|
| 1 | SetPoolTier | 38,196,576 | 19,178 B | 11,917,800 |
| 2 | CreateExternalIncentive (BAR) | 42,467,082 | 11,745 B | 101,174,500 |
| 3 | CreateExternalIncentive (FOO) | 42,640,170 | 8,575 B | 100,857,500 |
| 4 | CreateExternalIncentive (WUGNOT) | 42,568,850 | 8,566 B | 100,856,600 |
| 5 | StakeToken (3 ext) | 54,585,425 | 33,771 B | 13,377,100 |

---

## 9. gov_staker_delegate_and_undelegate

| Step | Operation | GAS USED | STORAGE DELTA | TOTAL TX COST |
|------|-----------|----------|---------------|---------------|
| 1 | Delegate | 29,001,657 | 17,871 B | 101,787,100 |
| 2 | Undelegate | 25,217,597 | 915 B | 100,091,500 |
| 3 | CollectUndelegatedGns | 17,096,421 | — | — |

---

## 10. gov_staker_delegate_and_redelegate

| Step | Operation | GAS USED | STORAGE DELTA | TOTAL TX COST |
|------|-----------|----------|---------------|---------------|
| 1 | Delegate | 29,010,915 | 17,871 B | 101,787,100 |
| 2 | Redelegate | 25,210,471 | 10,398 B | 101,039,800 |
