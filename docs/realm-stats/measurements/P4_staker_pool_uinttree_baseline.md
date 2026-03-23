# P4-3 Baseline: Staker Pool `*UintTree` × 4 + `Ticks.tree *avl.Tree`

**Date:** 2026-03-17
**Commit:** 99662881c (master)
**Test runner:** `TestTestdata`

---

## Test 1: staker_storage_staker_stake_only

| Operation | GAS USED | STORAGE DELTA | STORAGE FEE | TOTAL TX COST |
|-----------|----------|---------------|-------------|---------------|
| Mint position | 52,634,534 | 31,978 bytes | 3,197,800 ugnot | 13,197,800 ugnot |
| SetPoolTier | 38,241,600 | **21,179 bytes** | 2,117,900 ugnot | 12,117,900 ugnot |
| Approve GNFT | 4,413,390 | 1,061 bytes | 106,100 ugnot | 10,106,100 ugnot |
| **StakeToken (stake only)** | **50,677,544** | **17,803 bytes** | **1,780,300 ugnot** | **11,780,300 ugnot** |

### StakeToken per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| gno.land/r/gnoswap/gnft | 973 | 97,300 |
| gno.land/r/gnoswap/position | 79 | 7,900 |
| gno.land/r/gnoswap/staker | **16,751** | 1,675,100 |

---

## Test 2: staker_storage_staker_lifecycle

| Operation | GAS USED | STORAGE DELTA | STORAGE FEE | TOTAL TX COST |
|-----------|----------|---------------|-------------|---------------|
| **StakeToken** | **50,677,514** | **17,803 bytes** | **1,780,300 ugnot** | **11,780,300 ugnot** |
| CollectReward (1st) | 31,268,580 | 0 bytes | — | — |
| CollectReward (2nd) | 31,268,580 | 0 bytes | — | — |
| UnStakeToken | 55,828,022 | 4,789 bytes | 478,900 ugnot | 10,478,900 ugnot |

### StakeToken per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| gno.land/r/gnoswap/gnft | 973 | 97,300 |
| gno.land/r/gnoswap/position | 79 | 7,900 |
| gno.land/r/gnoswap/staker | **16,751** | 1,675,100 |

### UnStakeToken per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| gno.land/r/gnoswap/gnft | 22 | 2,200 |
| gno.land/r/gnoswap/position | -44 | (refund 4,400) |
| gno.land/r/gnoswap/staker | **4,811** | 481,100 |

---

## Test 3: staker_storage_staker_stake_with_externals

| Operation | GAS USED | STORAGE DELTA | STORAGE FEE | TOTAL TX COST |
|-----------|----------|---------------|-------------|---------------|
| CreateExternalIncentive #1 (BAR) | 42,573,946 | 8,419 bytes | 841,900 ugnot | 100,841,900 ugnot |
| CreateExternalIncentive #2 (FOO) | 42,685,150 | 8,575 bytes | 857,500 ugnot | 100,857,500 ugnot |
| CreateExternalIncentive #3 (WUGNOT) | 42,498,493 | 12,702 bytes | 1,270,200 ugnot | 101,270,200 ugnot |
| Approve GNFT | 4,413,426 | 1,061 bytes | 106,100 ugnot | 10,106,100 ugnot |
| **StakeToken (3 ext)** | **54,500,663** | **31,295 bytes** | **3,129,500 ugnot** | **13,129,500 ugnot** |

### StakeToken (3 ext) per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| gno.land/r/gnoswap/gnft | 973 | 97,300 |
| gno.land/r/gnoswap/position | 79 | 7,900 |
| gno.land/r/gnoswap/staker | **30,243** | 3,024,300 |

---

## Summary — Key Metrics for P4-3

| Operation | STORAGE DELTA (bytes) | Staker-only (bytes) |
|-----------|----------------------|---------------------|
| StakeToken (stake only) | 17,803 | 16,751 |
| StakeToken (3 ext) | 31,295 | 30,243 |
| SetPoolTier | 21,179 | 21,179 |
| CollectReward | 0 | 0 |
| UnStakeToken | 4,789 | 4,811 |

### Expected savings (from plan)
- Pool 당 UintTree ×4 + Ticks.tree ×1 = **5 Objects 제거** ≈ ~500 bytes
- StakeToken (stake only): 17,803 → ~17,300 (-3%)
- SetPoolTier: 21,179 → ~20,680 (-2%)
- StakeToken (3 ext): 31,295 → ~30,800 (-2%)
