# P4-3 Comparison: Staker Pool `*UintTree` × 4 + `Ticks.tree *avl.Tree` → value type

**Date:** 2026-03-17
**Base commit:** 99662881c (master)
**Change:** `staker/pool.gno` — Pool struct 4× `*UintTree` → `UintTree`, Ticks.tree `*avl.Tree` → `avl.Tree`

---

## Test 1: staker_storage_staker_stake_only

| Operation | Baseline (B) | After (B) | Delta | % |
|-----------|-------------|-----------|-------|---|
| SetPoolTier | 21,179 | **19,178** | **-2,001** | **-9.5%** |
| StakeToken (stake only) | 17,803 | **17,803** | 0 | 0% |

### SetPoolTier per-realm (staker only)
| | Baseline | After | Delta |
|-|----------|-------|-------|
| staker | 21,179 | 19,178 | **-2,001** |

### StakeToken per-realm
| Realm | Baseline | After | Delta |
|-------|----------|-------|-------|
| gnft | 973 | 973 | 0 |
| position | 79 | 79 | 0 |
| staker | 16,751 | 16,751 | 0 |

---

## Test 2: staker_storage_staker_lifecycle

| Operation | Baseline (B) | After (B) | Delta | % |
|-----------|-------------|-----------|-------|---|
| StakeToken | 17,803 | **17,803** | 0 | 0% |
| CollectReward (1st) | 0 | 0 | 0 | — |
| CollectReward (2nd) | 0 | 0 | 0 | — |
| UnStakeToken | 4,789 | **4,789** | 0 | 0% |

---

## Test 3: staker_storage_staker_stake_with_externals

### Per-operation (total STORAGE DELTA)

| Operation | Baseline (B) | After (B) | Delta |
|-----------|-------------|-----------|-------|
| SetPoolTier | 21,179 | **19,178** | **-2,001** |
| Incentive 1 (BAR) | 8,419 | 11,745 | +3,326 |
| Incentive 2 (FOO) | 8,575 | 8,575 | 0 |
| Incentive 3 (WUGNOT) | 12,702 | **8,566** | **-4,136** |
| StakeToken (3 ext) | 31,295 | 33,771 | +2,476 |

### Per-operation (staker realm only)

| Operation | Baseline (B) | After (B) | Delta |
|-----------|-------------|-----------|-------|
| SetPoolTier | 21,179 | **19,178** | **-2,001** |
| Incentive 1 (BAR) | 6,385 | 7,660 | +1,275 |
| Incentive 2 (FOO) | 6,541 | 6,541 | 0 |
| Incentive 3 (WUGNOT) | 10,673 | **6,537** | **-4,136** |
| StakeToken (3 ext) | 30,243 | 32,719 | +2,476 |
| **Total staker** | **75,021** | **72,635** | **-2,386** |

---

## Analysis

### What happened

Converting `*UintTree` → `UintTree` and `*avl.Tree` → `avl.Tree` eliminates 5 intermediate Object headers per Pool. The savings manifest as:

1. **SetPoolTier (-2,001 bytes)**: Pool creation saves ~2 KB from eliminated Object overhead. Consistent across all tests.
2. **StakeToken (stake only, 0 change)**: Mutation delta is unchanged — the same data is added regardless of layout.
3. **StakeToken (3 ext, +2,476 bytes)**: With 3 external incentives, the Pool Object contains more inline data. Re-serialization of the larger inline Pool during StakeToken shifts cost here.
4. **Incentive 3 (-4,136 bytes)**: The baseline had an anomalously high staker delta for WUGNOT incentive creation. After the change, all 3 incentive creations have similar staker costs (~6,500-7,700).

### Net effect

| Scenario | Net staker saving |
|----------|-------------------|
| stake_only (SetPoolTier + StakeToken) | **-2,001 bytes** |
| externals lifecycle (all ops) | **-2,386 bytes** |
| lifecycle (Stake+Collect+UnStake) | **0 bytes** |

### Cost redistribution

The value-type conversion doesn't reduce total stored data — it reduces Object count. The savings appear during **creation** (SetPoolTier) and are partially offset by **larger re-serialization** during subsequent mutations when the Pool contains significant inline data (externals case).

For the most common production scenario (pool creation + stake), the net saving is ~2,001 bytes per pool — a one-time saving that compounds across all pools.

### GAS comparison

| Operation | Baseline GAS | After GAS | Delta |
|-----------|-------------|-----------|-------|
| SetPoolTier | 38,241,600 | 38,198,424 | -43,176 |
| StakeToken (stake only) | 50,677,544 | 50,655,752 | -21,792 |
| StakeToken (3 ext) | 54,500,663 | 54,589,833 | +89,170 |
