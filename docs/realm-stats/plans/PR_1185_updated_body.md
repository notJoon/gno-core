### Applied Optimization Patterns

#### 1. Pointer to Value Type Conversion (`*T` → `T`)

The Gno VM creates a separate `HeapItemValue` Object for every pointer field and persists it independently.
Converting to value types inlines the Object into the parent struct, reducing both Object count and storage.

| Target | Fields Converted | Objects Removed |
|---|---:|---|
| Pool body (pool realm) | feeGrowthGlobal, liquidity, etc. | TickInfo -10, PositionInfo -5 obj |
| Pool sub-structs (Slot0, TokenPair) | sqrtPriceX96, balance/protocolFee token0/token1 | -5 obj/pool |
| Position (position realm) | 7 `*u256.Uint` fields | -7 obj/position |
| Observation (pool/oracle) | 2 `*u256.Uint` fields | -2 obj/observation |
| ObservationState map values | `map[uint16]*Observation` → `map[uint16]Observation` | -N obj/pool (N=cardinality) |
| Tick (staker realm) | `*u256.Uint`, `*i256.Int`, `*UintTree` | -3 obj/tick |
| Pool (staker realm) | 4 `*UintTree` + incentives | -5N+1 obj/pool |
| Deposit.liquidity | `*u256.Uint` → `u256.Uint` | -1 obj/deposit |
| PoolTierState.Membership | `*avl.Tree` → `avl.Tree` | -1 obj (global) |
| `[]*DelegationWithdraw` | pointer slice → value slice | -N obj/delegation |

**Measured:** Pool realm Mint -25.4% bytes, Undelegate -82.3% storage (5,166 → 915 bytes), CreatePool -21.2%

#### 2. UintTree Internal `*avl.Tree` → `avl.Tree`

Converted the internal `*avl.Tree` pointer inside the UintTree wrapper struct to a value type.
Preserves encapsulation while removing unnecessary indirection Objects.

- staker: SetPoolTier **-1,965 bytes (-8.1%)**
- gov/staker: Delegate **-419 bytes (-2.6%)**

#### 3. seqid (cford32) Key Encoding

Replaced zero-padded decimal AVL tree keys (10~20B) with seqid cford32 encoding (7B).

- `pool/utils.gno`: `EncodeTickKey` / `DecodeTickKey`
- `staker/tree.gno`: `EncodeInt` / `DecodeInt`
- Sequential IDs (position ID, etc.) were **skipped** as small IDs would increase from 1B to 7B

**Measured:** StakeToken -9.5~13.2%, CollectReward -11.8% gas

#### 4. PoolTier 8-Key → 1-Key Consolidation

Consolidated 8 separate store keys in `updatePoolTier()` into a single `PoolTierState` struct.
Reduced from 8 store.Get/Set calls to 1.

**Measured:** 80% finalize reduction in SetPoolTier

#### 5. Deposit 3 AVL Trees → 1 Tree Merge

Merged `collectedExternalRewards`, `externalRewardLastCollectTimes`, and `externalIncentiveIds`
into a single `externalIncentives` tree with an `ExternalIncentiveState` value type struct.

| Operation | Before | After | Delta |
|---|---:|---:|---:|
| CreateExternalIncentive ×3 | 53,132 B | 31,513 B | **-40.7%** |
| StakeToken | 35,301 B | 31,215 B | **-11.6%** |
| Lifecycle Total | 94,139 B | 68,434 B | **-27.3%** |

CollectReward gas **-16.0%** (the most frequently called operation).

#### 6. Store Access Caching (Eliminate Redundant Loads)

Removed patterns where the same data was loaded via store.Get() multiple times within a single transaction.
Data is loaded once at operation entry and passed as parameters to helpers.

| Operation | Finalize Before | After | Reduction |
|---|---:|---:|---:|
| StakeToken | 272 | 122 | -55.1% |
| CollectReward | 332 | 79 | -76.2% |
| UnStakeToken | 338 | 186 | -45.0% |

#### 7. CollectReward Early Return (Zero Reward Case)

When reward=0, finalize calls reduced from 12 to 0~1.

Gas savings of **-153K~204K**.

#### 8. decreaseLiquidity() Duplicate Save Removal

Removed the intermediate `mustUpdatePosition()` call at `burn.gno` line 88 (kept only the final save).
No storage delta change (Gno dirty tracking deduplicates same-key writes), gas **-135,187**.

#### 9. Pool Container Pointer Inlining (`*avl.Tree`, `*ObservationState`)

Converted 3 `*avl.Tree` fields (ticks, tickBitmaps, positions) and `*ObservationState` in the pool realm Pool struct from pointers to value types. The `avl.Tree` struct internally holds a `*Node` pointer, so value-type embedding is safe — only the Tree header is inlined, node data remains heap-allocated.

| Change | Objects Removed |
|---|---|
| `ticks *avl.Tree` → `avl.Tree` | -1 obj/pool |
| `tickBitmaps *avl.Tree` → `avl.Tree` | -1 obj/pool |
| `positions *avl.Tree` → `avl.Tree` | -1 obj/pool |
| `observationState *ObservationState` → `ObservationState` | -1 obj/pool |

**Measured:** CreatePool **-1,587 bytes (-10.0%)** consistently across fee tiers. Mutation operations (Swap, Mint, CollectFee) show zero delta change — savings are one-time at pool creation.

#### 10. Staker Pool Container Pointer Inlining (`*UintTree`, `Ticks.tree`)

Converted 4 `*UintTree` fields (stakedLiquidity, rewardCache, globalRewardRatioAccumulation, historicalTick) and `Ticks.tree *avl.Tree` in the staker Pool struct from pointers to value types.

| Change | Objects Removed |
|---|---|
| 4× `*UintTree` → `UintTree` | -4 obj/pool |
| `Ticks.tree *avl.Tree` → `avl.Tree` | -1 obj/pool |

**Measured:** SetPoolTier **-2,001 bytes (-9.5%)**. One-time saving at pool creation. Net staker savings with external incentives lifecycle: **-2,386 bytes**.

---

### Skipped/Dropped Items and Rationale

| Item | Rationale |
|---|---|
| Large struct splitting | PoC confirmed no meaningful storage savings |
| Secondary index addition | Increases storage (new tree creation); only reduces gas — misaligned with goal |
| Fee protocol global variable | Same-size rewrites have no deposit impact + extremely low governance call frequency |
| Swap hot-loop clone | Only affects gas (heap allocation), not storage |
| Sequential ID seqid conversion | Small IDs increase from 1B to 7B |
| Halving cache | Regression risk introduced by prior optimizations, dropped |

---

### Scope of Changes

**Pool realm:** pool.gno, utils.gno, oracle.gno, v1/oracle.gno
**Staker realm:** types, store, deposit, tree, pool, getter_utils, v1/ (all files)
**Position realm:** position.gno, store.gno, v1/burn.gno
**Gov modules:** gov/staker/delegation, tree, store, types, v1/ (all files)
**Launchpad:** store.gno
