# Gas Comparison: `master` vs `dev/jae/gas-model-improvements`

> **Measured:** 2026-03-17
> **Baseline:** `master` ([`99662881c`](https://github.com/gnolang/gno/commit/99662881c)) | **Target:** `dev/jae/gas-model-improvements` ([`ce7f30aba`](https://github.com/gnolang/gno/commit/ce7f30aba))
> **Test source:** gnoswap-labs/gnoswap [#1183](https://github.com/gnoswap-labs/gnoswap/pull/1183), [#1185](https://github.com/gnoswap-labs/gnoswap/pull/1185)

## Gas Used — User Entry Functions

### Tier 1 (High Frequency)

| Function | master | gas-model | delta | % |
|----------|--------|-----------|-------|---|
| ExactInSwapRoute (1st) | 55,447,946 | 43,830,833 | -11,617,113 | **-21.0%** |
| ExactInSwapRoute (reverse) | 58,309,597 | 45,304,727 | -13,004,870 | **-22.3%** |
| ExactInSwapRoute (steady) | 55,145,035 | 43,848,116 | -11,296,919 | **-20.5%** |
| Mint #1 (wide range) | 56,111,330 | 45,245,982 | -10,865,348 | **-19.4%** |
| Mint #2 (narrow range) | 55,688,609 | 45,186,601 | -10,502,008 | **-18.9%** |
| Mint #3 (same ticks) | 54,190,064 | 44,378,104 | -9,811,960 | **-18.1%** |
| CollectFee #1 (wide) | 44,662,859 | 39,722,047 | -4,940,812 | **-11.1%** |
| CollectFee #2 (narrow) | 44,560,482 | 39,618,751 | -4,941,731 | **-11.1%** |
| StakeToken | 63,270,474 | 48,571,827 | -14,698,647 | **-23.2%** |
| CollectReward | 35,940,261 | 29,698,426 | -6,241,835 | **-17.4%** |
| UnStakeToken | 58,972,883 | 45,625,978 | -13,346,905 | **-22.6%** |

### Tier 2 (Moderate Frequency)

| Function | master | gas-model | delta | % |
|----------|--------|-----------|-------|---|
| IncreaseLiquidity (steady) | 54,316,312 | 42,904,840 | -11,411,472 | **-21.0%** |
| DecreaseLiquidity | 55,633,689 | 45,032,818 | -10,600,871 | **-19.1%** |
| Undelegate | 25,790,716 | 23,573,574 | -2,217,142 | **-8.6%** |
| Redelegate | 25,358,532 | 23,457,563 | -1,900,969 | **-7.5%** |
| CollectUndelegatedGns | 17,129,302 | 16,593,423 | -535,879 | -3.1% |

### Tier 3 (Setup / Admin)

| Function | master | gas-model | delta | % |
|----------|--------|-----------|-------|---|
| CreatePool | 33,782,761 | 30,669,790 | -3,112,971 | **-9.2%** |
| SetPoolTier | 42,222,934 | 36,368,590 | -5,854,344 | **-13.9%** |
| StakeToken (3 externals) | 63,966,876 | 48,810,328 | -15,156,548 | **-23.7%** |

## Storage Delta (bytes)

Storage delta is unaffected by gas model changes — values are nearly identical across branches.

| Function | master | gas-model | diff |
|----------|--------|-----------|------|
| Mint #1 (wide) | 40,763 | 40,764 | +1 |
| Mint #2 (narrow) | 39,512 | 39,513 | +1 |
| StakeToken | 33,099 | 27,364 | -5,735 |
| StakeToken (3 ext) | 36,214 | 28,819 | -7,395 |
| CreatePool | 20,746 | 20,746 | 0 |
| Mint #3 (same ticks) | 13,093 | 13,094 | +1 |
| ExactInSwapRoute (1st) | 5,033 | 5,027 | -6 |
| ExactInSwapRoute (reverse) | 2,839 | 2,845 | +6 |
| CollectFee #1 | 2,259 | 2,259 | 0 |
| ExactInSwapRoute (steady) | 39 | 39 | 0 |
| IncreaseLiquidity (steady) | 26 | 26 | 0 |
| CollectFee #2 | 64 | 64 | 0 |
| DecreaseLiquidity | 5 | 5 | 0 |
| UnStakeToken | -7,198 | -4,772 | +2,426 |

## Summary

| Category | Reduction | Gas Saved |
|----------|-----------|-----------|
| **Staker (Stake/Unstake)** | **-22.6% ~ -23.7%** | 13.3M ~ 15.2M |
| **Swap (ExactInSwapRoute)** | **-20.5% ~ -22.3%** | 11.3M ~ 13.0M |
| **Position (Mint)** | **-18.1% ~ -19.4%** | 9.8M ~ 10.9M |
| **Liquidity (Increase/Decrease)** | **-19.1% ~ -21.0%** | 10.6M ~ 11.4M |
| **CollectReward** | **-17.4%** | 6.2M |
| **CollectFee** | **-11.1%** | ~4.9M |
| **SetPoolTier** | **-13.9%** | 5.9M |
| **CreatePool** | **-9.2%** | 3.1M |
| **Gov (Undelegate/Redelegate)** | **-7.5% ~ -8.6%** | 1.9M ~ 2.2M |

All user-facing functions show **18–24% gas reduction**. Storage delta remains unchanged.

<details>
<summary>Raw Data</summary>

### Position Lifecycle

| Step | Function | master GAS | gas-model GAS | master STORAGE | gas-model STORAGE |
|------|----------|-----------|--------------|---------------|------------------|
| 1 | CreatePool | 33,782,761 | 30,669,790 | 20,746 B | 20,746 B |
| 2 | Mint #1 (wide) | 56,111,330 | 45,245,982 | 40,763 B | 40,764 B |
| 3 | Mint #2 (narrow) | 55,688,609 | 45,186,601 | 39,512 B | 39,513 B |
| 4 | Mint #3 (same ticks) | 54,190,064 | 44,378,104 | 13,093 B | 13,094 B |
| 5 | Swap (wrapper) | 43,423,756 | 33,923,352 | 12 B | 12 B |
| 6 | CollectFee #1 | 44,662,859 | 39,722,047 | 2,259 B | 2,259 B |
| 7 | CollectFee #2 | 44,560,482 | 39,618,751 | 64 B | 64 B |
| 8 | DecreaseLiquidity | 55,633,689 | 45,032,818 | 5 B | 5 B |

### Swap Lifecycle

| Step | Function | master GAS | gas-model GAS | master STORAGE | gas-model STORAGE |
|------|----------|-----------|--------------|---------------|------------------|
| M1 | ExactInSwapRoute (1st) | 55,447,946 | 43,830,833 | 5,033 B | 5,027 B |
| M2 | ExactInSwapRoute (reverse) | 58,309,597 | 45,304,727 | 2,839 B | 2,845 B |
| M3 | ExactInSwapRoute (steady) | 55,145,035 | 43,848,116 | 39 B | 39 B |

### Staker Lifecycle

| Step | Function | master GAS | gas-model GAS | master STORAGE | gas-model STORAGE |
|------|----------|-----------|--------------|---------------|------------------|
| 1 | SetPoolTier | 42,222,934 | 36,368,590 | 24,427 B | 21,179 B |
| 2 | StakeToken | 63,270,474 | 48,571,827 | 33,099 B | 27,364 B |
| 3 | CollectReward #1 | 35,940,261 | 29,698,426 | 0 B | 0 B |
| 4 | CollectReward #2 | 35,940,261 | 29,698,426 | 0 B | 0 B |
| 5 | UnStakeToken | 58,972,883 | 45,625,978 | -7,198 B | -4,772 B |

### Staker With External Incentives

| Step | Function | master GAS | gas-model GAS | master STORAGE | gas-model STORAGE |
|------|----------|-----------|--------------|---------------|------------------|
| 1 | StakeToken (3 ext) | 63,966,876 | 48,810,328 | 36,214 B | 28,819 B |

### IncreaseLiquidity

| Step | Function | master GAS | gas-model GAS | master STORAGE | gas-model STORAGE |
|------|----------|-----------|--------------|---------------|------------------|
| M1 | IncreaseLiquidity (1st) | — | 43,338,548 | — | 93 B |
| M2 | IncreaseLiquidity (steady) | 54,316,312 | 42,904,840 | 26 B | 26 B |

### Gov Delegate/Undelegate

| Step | Function | master GAS | gas-model GAS | master STORAGE | gas-model STORAGE |
|------|----------|-----------|--------------|---------------|------------------|
| 1 | Delegate | — | 26,808,983 | — | 18,312 B |
| 2 | Undelegate | 25,790,716 | 23,573,574 | 5,166 B | 1,331 B |
| 3 | CollectUndelegatedGns | 17,129,302 | 16,593,423 | — | — |

### Gov Delegate/Redelegate

| Step | Function | master GAS | gas-model GAS | master STORAGE | gas-model STORAGE |
|------|----------|-----------|--------------|---------------|------------------|
| 1 | Delegate | — | 26,811,206 | — | 18,312 B |
| 2 | Redelegate | 25,358,532 | 23,457,563 | 10,826 B | 12,652 B |

</details>
