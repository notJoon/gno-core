# Storage Comparison: gnoswap main vs optimized (on gas-model branch)

> **Measured:** 2026-03-17
> **Base (gno):** `dev/jae/gas-model-improvements` ([`ce7f30aba`](https://github.com/gnolang/gno/commit/ce7f30aba))
> **Baseline (gnoswap):** `main` | **Optimized (gnoswap):** `refactor/convert-value-type-pool` ([`4376ce79`](https://github.com/gnoswap-labs/gnoswap/commit/4376ce79))
> **Test source:** gnoswap-labs/gnoswap [#1183](https://github.com/gnoswap-labs/gnoswap/pull/1183), [#1185](https://github.com/gnoswap-labs/gnoswap/pull/1185)

## Storage Delta (bytes) — User Entry Functions

### Tier 1 (High Frequency)

| Function | main (B) | optimized (B) | delta | % |
|----------|----------|---------------|-------|---|
| ExactInSwapRoute (1st) | 5,027 | 5,021 | -6 | -0.1% |
| ExactInSwapRoute (reverse) | 2,845 | 2,845 | 0 | 0% |
| ExactInSwapRoute (steady) | 33 | 33 | 0 | 0% |
| Mint #1 (wide) | 40,763 | 32,019 | -8,744 | **-21.4%** |
| Mint #2 (narrow) | 39,512 | 30,705 | -8,807 | **-22.3%** |
| Mint #3 (same ticks) | 13,094 | 10,289 | -2,805 | **-21.4%** |
| CollectFee #1 (wide) | 2,259 | 2,216 | -43 | -1.9% |
| CollectFee #2 (narrow) | 64 | 50 | -14 | **-21.9%** |
| StakeToken | 23,447 | 17,803 | -5,644 | **-24.1%** |
| CollectReward (#1) | 5,758 | 0 | -5,758 | **-100%** |
| CollectReward (#2) | 0 | 5,712 | +5,712 | — |
| UnStakeToken | -3,304 | 64 | +3,368 | — |

### Tier 2 (Moderate Frequency)

| Function | main (B) | optimized (B) | delta | % |
|----------|----------|---------------|-------|---|
| IncreaseLiquidity (1st) | 93 | 61 | -32 | **-34.4%** |
| IncreaseLiquidity (steady) | 26 | 10 | -16 | **-61.5%** |
| DecreaseLiquidity | 5 | 14 | +9 | +180%* |
| Delegate | 18,312 | 17,871 | -441 | -2.4% |
| Undelegate | 1,331 | 915 | -416 | **-31.3%** |
| Redelegate | 10,826 | 10,398 | -428 | -4.0% |

### Tier 3 (Setup / Admin)

| Function | main (B) | optimized (B) | delta | % |
|----------|----------|---------------|-------|---|
| CreatePool | 20,746 | 18,352 | -2,394 | **-11.5%** |
| SetPoolTier | 24,427 | 21,179 | -3,248 | **-13.3%** |
| StakeToken (3 ext) | 33,099 | 31,295 | -1,804 | -5.5% |

> \* DecreaseLiquidity: absolute values are 5B vs 14B — negligible in practice.

## Gas Used — User Entry Functions

### Tier 1 (High Frequency)

| Function | main | optimized | delta | % |
|----------|------|-----------|-------|---|
| ExactInSwapRoute (1st) | 44,102,975 | 44,462,037 | +359,062 | +0.8% |
| ExactInSwapRoute (reverse) | 45,266,182 | 45,626,289 | +360,107 | +0.8% |
| ExactInSwapRoute (steady) | 43,769,848 | 44,347,876 | +578,028 | +1.3% |
| Mint #1 (wide) | 45,240,610 | 45,705,738 | +465,128 | +1.0% |
| Mint #2 (narrow) | 45,182,373 | 45,315,373 | +133,000 | +0.3% |
| Mint #3 (same ticks) | 44,157,693 | 44,443,948 | +286,255 | +0.6% |
| CollectFee #1 | 39,722,047 | 39,955,876 | +233,829 | +0.6% |
| CollectFee #2 | 39,618,751 | 39,854,221 | +235,470 | +0.6% |
| StakeToken | 48,728,157 | 46,096,462 | -2,631,695 | **-5.4%** |
| CollectReward (#1) | 38,358,384 | 29,698,426 | -8,659,958 | **-22.6%** |
| CollectReward (#2) | 32,413,311 | 33,748,429 | +1,335,118 | +4.1% |
| UnStakeToken | 49,159,151 | 46,012,965 | -3,146,186 | **-6.4%** |

### Tier 2 (Moderate Frequency)

| Function | main | optimized | delta | % |
|----------|------|-----------|-------|---|
| IncreaseLiquidity (1st) | 43,338,548 | 43,179,544 | -159,004 | -0.4% |
| IncreaseLiquidity (steady) | 43,124,827 | 42,866,486 | -258,341 | -0.6% |
| DecreaseLiquidity | 45,032,926 | 44,951,589 | -81,337 | -0.2% |
| Delegate | 26,808,983 | 27,202,194 | +393,211 | +1.5% |
| Undelegate | 23,573,574 | 23,680,861 | +107,287 | +0.5% |
| Redelegate | 23,433,120 | 23,685,738 | +252,618 | +1.1% |

### Tier 3 (Setup / Admin)

| Function | main | optimized | delta | % |
|----------|------|-----------|-------|---|
| CreatePool | 30,669,790 | 30,599,233 | -70,557 | -0.2% |
| SetPoolTier | 38,148,207 | 36,368,590 | -1,779,617 | **-4.7%** |
| StakeToken (3 ext) | 52,390,847 | 48,920,877 | -3,469,970 | **-6.6%** |

## Total TX Cost (ugnot) — User Entry Functions

Total TX Cost = gas-fee + storage fee. Lower gas-fee tiers (10M ugnot) are used for staker tests; higher (100M ugnot) for position/swap/gov tests.

### Tier 1 (High Frequency)

| Function | main | optimized | delta | % |
|----------|------|-----------|-------|---|
| ExactInSwapRoute (1st) | 100,502,700 | 100,502,100 | -600 | 0% |
| ExactInSwapRoute (reverse) | 100,284,500 | 100,284,500 | 0 | 0% |
| ExactInSwapRoute (steady) | 100,003,300 | 100,003,300 | 0 | 0% |
| Mint #1 (wide) | 104,076,300 | 103,201,900 | -874,400 | **-0.8%** |
| Mint #2 (narrow) | 103,951,200 | 103,070,500 | -880,700 | **-0.8%** |
| Mint #3 (same ticks) | 101,309,400 | 101,028,900 | -280,500 | -0.3% |
| CollectFee #1 (wide) | 100,225,900 | 100,221,600 | -4,300 | 0% |
| CollectFee #2 (narrow) | 100,006,400 | 100,005,000 | -1,400 | 0% |
| StakeToken | 12,344,700 | 11,780,300 | -564,400 | **-4.6%** |
| UnStakeToken | 9,669,600 | 10,006,400 | +336,800 | +3.5% |

### Tier 2 (Moderate Frequency)

| Function | main | optimized | delta | % |
|----------|------|-----------|-------|---|
| IncreaseLiquidity (1st) | 100,009,300 | 100,006,100 | -3,200 | 0% |
| IncreaseLiquidity (steady) | 100,002,600 | 100,001,000 | -1,600 | 0% |
| DecreaseLiquidity | 100,000,500 | 100,001,400 | +900 | 0% |
| Delegate | 101,831,200 | 101,787,100 | -44,100 | 0% |
| Undelegate | 100,133,100 | 100,091,500 | -41,600 | 0% |
| Redelegate | 101,082,600 | 101,039,800 | -42,800 | 0% |

### Tier 3 (Setup / Admin)

| Function | main | optimized | delta | % |
|----------|------|-----------|-------|---|
| CreatePool | 12,074,600 | 11,835,200 | -239,400 | **-2.0%** |
| SetPoolTier | 12,442,700 | 12,117,900 | -324,800 | **-2.6%** |
| StakeToken (3 ext) | 13,309,900 | 13,129,500 | -180,400 | -1.4% |

## Summary

### Storage Reduction

| Category | Reduction | Bytes Saved |
|----------|-----------|-------------|
| **Mint (position creation)** | **-21.4% ~ -22.3%** | 2.8KB ~ 8.8KB |
| **StakeToken** | **-24.1%** | 5.6KB |
| **IncreaseLiquidity** | **-34.4% ~ -61.5%** | 16B ~ 32B |
| **Undelegate** | **-31.3%** | 416B |
| **SetPoolTier** | **-13.3%** | 3.2KB |
| **CreatePool** | **-11.5%** | 2.4KB |
| **CollectFee** | **-1.9% ~ -21.9%** | 14B ~ 43B |
| **ExactInSwapRoute** | ~0% | negligible |

### Gas Impact

| Category | Change |
|----------|--------|
| **CollectReward (#1)** | **-22.6%** (8.7M gas saved) |
| **StakeToken (3 ext)** | **-6.6%** (3.5M gas saved) |
| **StakeToken** | **-5.4%** (2.6M gas saved) |
| **UnStakeToken** | **-6.4%** (3.1M gas saved) |
| **SetPoolTier** | **-4.7%** (1.8M gas saved) |
| **Swap/Mint/Gov** | +0.3% ~ +1.5% (minor overhead from value-type conversion) |

### Key Observations

- Storage optimization primarily reduces **Mint** (-21%) and **StakeToken** (-24%) deposit costs
- Gas savings concentrate in **staker module** (-5% ~ -23%) due to reduced serialization overhead
- Swap and position read-heavy operations show **negligible storage change** with slight gas overhead (+0.3~1.3%)
- **CollectReward timing shift**: main charges storage on 1st call, optimized on 2nd — total is similar

<details>
<summary>Raw Data</summary>

### Position Lifecycle

| Step | Function | main GAS | opt GAS | main STORAGE | opt STORAGE | main TX COST | opt TX COST |
|------|----------|----------|---------|-------------|------------|-------------|------------|
| 1 | CreatePool | 30,669,790 | 30,599,233 | 20,746 B | 18,352 B | 12,074,600 | 11,835,200 |
| 2 | Mint #1 (wide) | 45,240,610 | 45,705,738 | 40,763 B | 32,019 B | 104,076,300 | 103,201,900 |
| 3 | Mint #2 (narrow) | 45,182,373 | 45,315,373 | 39,512 B | 30,705 B | 103,951,200 | 103,070,500 |
| 4 | Mint #3 (same ticks) | 44,157,693 | 44,443,948 | 13,094 B | 10,289 B | 101,309,400 | 101,028,900 |
| 5 | Swap (wrapper) | 33,923,568 | 34,239,127 | 12 B | 6 B | 100,001,200 | 100,000,600 |
| 6 | CollectFee #1 | 39,722,047 | 39,955,876 | 2,259 B | 2,216 B | 100,225,900 | 100,221,600 |
| 7 | CollectFee #2 | 39,618,751 | 39,854,221 | 64 B | 50 B | 100,006,400 | 100,005,000 |
| 8 | DecreaseLiquidity | 45,032,926 | 44,951,589 | 5 B | 14 B | 100,000,500 | 100,001,400 |

### Swap Lifecycle

| Step | Function | main GAS | opt GAS | main STORAGE | opt STORAGE | main TX COST | opt TX COST |
|------|----------|----------|---------|-------------|------------|-------------|------------|
| M1 | ExactInSwapRoute (1st) | 44,102,975 | 44,462,037 | 5,027 B | 5,021 B | 100,502,700 | 100,502,100 |
| M2 | ExactInSwapRoute (reverse) | 45,266,182 | 45,626,289 | 2,845 B | 2,845 B | 100,284,500 | 100,284,500 |
| M3 | ExactInSwapRoute (steady) | 43,769,848 | 44,347,876 | 33 B | 33 B | 100,003,300 | 100,003,300 |

### Staker Lifecycle

| Step | Function | main GAS | opt GAS | main STORAGE | opt STORAGE | main TX COST | opt TX COST |
|------|----------|----------|---------|-------------|------------|-------------|------------|
| 1 | SetPoolTier | 38,148,207 | 36,368,590 | 24,427 B | 21,179 B | 12,442,700 | 12,117,900 |
| 2 | StakeToken | 48,728,157 | 48,571,827 | 23,447 B | 27,364 B | 12,344,700 | 12,736,400 |
| 3 | CollectReward #1 | 38,358,384 | 29,698,426 | 5,758 B | 0 B | 10,575,800 | — |
| 4 | CollectReward #2 | 32,413,311 | 33,748,429 | 0 B | 5,712 B | — | 10,571,200 |
| 5 | UnStakeToken | 49,159,151 | 46,012,965 | -3,304 B | 64 B | 9,669,600 | 10,006,400 |

### Staker Stake Only

| Step | Function | main GAS | opt GAS | main STORAGE | opt STORAGE | main TX COST | opt TX COST |
|------|----------|----------|---------|-------------|------------|-------------|------------|
| 1 | StakeToken | 48,728,187 | 46,096,462 | 23,447 B | 17,803 B | 12,344,700 | 11,780,300 |

### Staker With External Incentives

| Step | Function | main GAS | opt GAS | main STORAGE | opt STORAGE | main TX COST | opt TX COST |
|------|----------|----------|---------|-------------|------------|-------------|------------|
| 1 | StakeToken (3 ext) | 52,390,847 | 48,920,877 | 33,099 B | 31,295 B | 13,309,900 | 13,129,500 |

### IncreaseLiquidity

| Step | Function | main GAS | opt GAS | main STORAGE | opt STORAGE | main TX COST | opt TX COST |
|------|----------|----------|---------|-------------|------------|-------------|------------|
| M1 | IncreaseLiquidity (1st) | 43,338,548 | 43,179,544 | 93 B | 61 B | 100,009,300 | 100,006,100 |
| M2 | IncreaseLiquidity (steady) | 43,124,827 | 42,866,486 | 26 B | 10 B | 100,002,600 | 100,001,000 |

### Gov Delegate/Undelegate

| Step | Function | main GAS | opt GAS | main STORAGE | opt STORAGE | main TX COST | opt TX COST |
|------|----------|----------|---------|-------------|------------|-------------|------------|
| 1 | Delegate | 26,808,983 | 27,202,194 | 18,312 B | 17,871 B | 101,831,200 | 101,787,100 |
| 2 | Undelegate | 23,573,574 | 23,680,861 | 1,331 B | 915 B | 100,133,100 | 100,091,500 |
| 3 | CollectUndelegatedGns | 16,593,423 | 16,560,205 | — | — | — | — |

### Gov Delegate/Redelegate

| Step | Function | main GAS | opt GAS | main STORAGE | opt STORAGE | main TX COST | opt TX COST |
|------|----------|----------|---------|-------------|------------|-------------|------------|
| 1 | Delegate | 26,811,206 | 27,199,971 | 18,312 B | 17,871 B | 101,831,200 | 101,787,100 |
| 2 | Redelegate | 23,433,120 | 23,685,738 | 10,826 B | 10,398 B | 101,082,600 | 101,039,800 |

</details>
