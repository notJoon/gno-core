# P4-2 Baseline: Pool `*avl.Tree` x 3 + `*ObservationState` → value type

**Date:** 2026-03-17
**Commit:** 99662881c (master)
**Test runner:** `TestTestdata`

---

## Test 1: pool_create_pool_and_mint

| # | Operation | GAS USED | STORAGE DELTA | STORAGE FEE | TOTAL TX COST |
|---|-----------|----------|---------------|-------------|---------------|
| 1 | Deploy swap_wrapper | 11,450,086 | 5,768 bytes | 576,800 ugnot | 676,800 ugnot |
| 2 | Approve GNS (pool) | 5,277,168 | 1,081 bytes | 108,100 ugnot | 100,108,100 ugnot |
| 3 | **CreatePool (fee 3000)** | **33,683,610** | **15,932 bytes** | **1,593,200 ugnot** | **11,593,200 ugnot** |
| 4 | Deposit WUGNOT | 6,625,821 | 1,037 bytes | 103,700 ugnot | 100,103,700 ugnot |
| 5 | Approve WUGNOT (pool) | 4,073,981 | 1,074 bytes | 107,400 ugnot | 100,107,400 ugnot |
| 6 | Approve WUGNOT (position) | 4,119,925 | 2,118 bytes | 211,800 ugnot | 100,211,800 ugnot |
| 7 | **Mint #1** | **55,927,363** | **32,009 bytes** | **3,200,900 ugnot** | **103,200,900 ugnot** |
| 8 | **DecreaseLiquidity** | **58,914,624** | **2,229 bytes** | **222,900 ugnot** | **100,222,900 ugnot** |
| 9 | **IncreaseLiquidity** | **54,339,490** | **4 bytes** | **400 ugnot** | **100,000,400 ugnot** |
| 10 | Approve GNS (swap_wrapper) | 5,324,060 | 2,133 bytes | 213,300 ugnot | 100,213,300 ugnot |
| 11 | **Swap (fee 3000, exactIn)** | **42,513,543** | **5,688 bytes** | **568,800 ugnot** | **100,568,800 ugnot** |
| 12 | **CollectFee** | **44,543,882** | **55 bytes** | **5,500 ugnot** | **100,005,500 ugnot** |

### CreatePool per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| gno.land/r/gnoswap/gns | 2,043 | 204,300 |
| **gno.land/r/gnoswap/pool** | **13,759** | **1,375,900** |
| gno.land/r/gnoswap/protocol_fee | 130 | 13,000 |

### Mint #1 per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| gno.land/r/gnoland/wugnot | 2,029 | 202,900 |
| gno.land/r/gnoswap/gnft | 5,177 | 517,700 |
| gno.land/r/gnoswap/gns | 2,051 | 205,100 |
| **gno.land/r/gnoswap/pool** | **17,592** | **1,759,200** |
| gno.land/r/gnoswap/position | 5,160 | 516,000 |

### DecreaseLiquidity per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| gno.land/r/gnoland/wugnot | 2,039 | 203,900 |
| gno.land/r/gnoswap/gns | 7 | 700 |
| **gno.land/r/gnoswap/pool** | **54** | **5,400** |
| gno.land/r/gnoswap/protocol_fee | 129 | 12,900 |

### IncreaseLiquidity per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| gno.land/r/gnoland/wugnot | -5 | (refund 500) |
| gno.land/r/gnoswap/gns | -7 | (refund 700) |
| **gno.land/r/gnoswap/pool** | **6** | **600** |
| gno.land/r/gnoswap/position | 10 | 1,000 |

### Swap (fee 3000) per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| **gno.land/r/gnoswap/pool** | **5,688** | **568,800** |

### CollectFee per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| gno.land/r/gnoland/wugnot | 27 | 2,700 |
| gno.land/r/gnoswap/gns | 6 | 600 |
| **gno.land/r/gnoswap/pool** | **-6** | **(refund 600)** |
| gno.land/r/gnoswap/position | 28 | 2,800 |

---

## Test 2: pool_swap_wugnot_gns_tokens

| # | Operation | GAS USED | STORAGE DELTA | STORAGE FEE | TOTAL TX COST |
|---|-----------|----------|---------------|-------------|---------------|
| 1 | Deploy swap_wrapper | 11,450,086 | 5,768 bytes | 576,800 ugnot | 676,800 ugnot |
| 2 | Transfer GNS #1 | 6,468,873 | 2,043 bytes | 204,300 ugnot | 100,204,300 ugnot |
| 3 | Transfer GNS #2 | 6,589,340 | 2,049 bytes | 204,900 ugnot | 100,204,900 ugnot |
| 4 | Transfer GNS #3 | 6,613,525 | 1 bytes | 100 ugnot | 100,000,100 ugnot |
| 5 | Approve GNS (pool) | 5,279,316 | 1,082 bytes | 108,200 ugnot | 100,108,200 ugnot |
| 6 | **CreatePool (fee 500)** | **34,077,134** | **15,936 bytes** | **1,593,600 ugnot** | **11,593,600 ugnot** |
| 7 | Deposit WUGNOT | 6,625,821 | 1,037 bytes | 103,700 ugnot | 100,103,700 ugnot |
| 8 | Approve WUGNOT (pool) | 4,074,017 | 1,074 bytes | 107,400 ugnot | 100,107,400 ugnot |
| 9 | Approve WUGNOT (position) | 4,119,961 | 2,118 bytes | 211,800 ugnot | 100,211,800 ugnot |
| 10 | **Mint #1** | **57,067,076** | **32,023 bytes** | **3,202,300 ugnot** | **103,202,300 ugnot** |
| 11 | Approve GNS (swap_wrapper) | 5,324,060 | 2,133 bytes | 213,300 ugnot | 100,213,300 ugnot |
| 12 | **ExactIn Swap (fee 500)** | **71,875,180** | **19,863 bytes** | **1,986,300 ugnot** | **101,986,300 ugnot** |

### CreatePool (fee 500) per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| gno.land/r/gnoswap/gns | 2,049 | 204,900 |
| **gno.land/r/gnoswap/pool** | **13,757** | **1,375,700** |
| gno.land/r/gnoswap/protocol_fee | 130 | 13,000 |

### Mint #1 per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| gno.land/r/gnoland/wugnot | 2,029 | 202,900 |
| gno.land/r/gnoswap/gnft | 5,179 | 517,900 |
| gno.land/r/gnoswap/gns | 2,049 | 204,900 |
| **gno.land/r/gnoswap/pool** | **17,607** | **1,760,700** |
| gno.land/r/gnoswap/position | 5,159 | 515,900 |

### ExactIn Swap (fee 500) per-realm breakdown
| Realm | Bytes Delta | Fee Delta |
|-------|-------------|-----------|
| **gno.land/r/gnoswap/pool** | **19,863** | **1,986,300** |

---

## Summary — Key Metrics for P4-2 (pool realm only)

| Operation | Total STORAGE DELTA | Pool-only (bytes) |
|-----------|--------------------|--------------------|
| CreatePool (fee 3000) | 15,932 | **13,759** |
| CreatePool (fee 500) | 15,936 | **13,757** |
| Mint #1 (fee 3000) | 32,009 | **17,592** |
| Mint #1 (fee 500) | 32,023 | **17,607** |
| Swap (fee 3000, 1st) | 5,688 | **5,688** |
| ExactIn Swap (fee 500, 1st) | 19,863 | **19,863** |
| DecreaseLiquidity | 2,229 | **54** |
| IncreaseLiquidity | 4 | **6** |
| CollectFee | 55 | **-6** |

### Expected savings (from plan)
- Pool 당 `*avl.Tree` ×3 + `*ObservationState` ×1 = **4 Objects 제거** ≈ ~400 bytes
- CreatePool: 13,759 → ~13,350 (-3%)
- Mint #1: 17,592 → ~17,200 (-2%)
- Swap (1st): Pool delta의 creation 부분에서 절감 예상

### GAS baseline
| Operation | GAS USED |
|-----------|----------|
| CreatePool (fee 3000) | 33,683,610 |
| CreatePool (fee 500) | 34,077,134 |
| Mint #1 (fee 3000) | 55,927,363 |
| Mint #1 (fee 500) | 57,067,076 |
| Swap (fee 3000) | 42,513,543 |
| ExactIn Swap (fee 500) | 71,875,180 |
| DecreaseLiquidity | 58,914,624 |
| IncreaseLiquidity | 54,339,490 |
| CollectFee | 44,543,882 |
