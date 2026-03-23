## GnoSwap Storage Deposit Optimization

### Summary

A systematic audit and optimization of storage deposit costs across the GnoSwap contract codebase.
Due to Gno VM's Object-based persistence model, pointer fields, redundant writes, and inefficient key encoding
were identified as the primary cost drivers for storage deposits. This PR structurally eliminates them.

---

### Applied Optimization Patterns

#### 1. Pointer to Value Type Conversion (`*T` → `T`)

The Gno VM creates a separate `HeapItemValue` Object for every pointer field and persists it independently.
Converting to value types inlines the Object into the parent struct, reducing both Object count and storage.

| Target | Fields Converted | Objects Removed |
|---|---:|---|
| Pool (pool realm) | feeGrowthGlobal, liquidity, etc. | TickInfo -10, PositionInfo -5 obj |
| Position (position realm) | 7 `*u256.Uint` fields | -7 obj/position |
| Observation (pool/oracle) | 2 fields | -2 obj/observation |
| Tick (staker realm) | `*u256.Uint`, `*i256.Int`, `*UintTree` | -3 obj/tick |
| Pool (staker realm) | 4 `*UintTree` + incentives | -5N+1 obj/pool |
| Deposit.liquidity | `*u256.Uint` → `u256.Uint` | -1 obj/deposit |
| PoolTierState.Membership | `*avl.Tree` → `avl.Tree` | -1 obj (global) |
| `[]*DelegationWithdraw` | pointer slice → value slice | -N obj/delegation |

**Measured:** Pool realm Mint -25.4% bytes, Undelegate -82.3% storage (5,166 → 915 bytes)

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

### Unresolved (Low Priority — Code Quality, No Storage Deposit Impact)

- Composite string key costs (pool path ~54B, incentive ID ~58B)
- Multiple AVL tree indexes over the same data (staker 7, protocol fee 4, launchpad 3)
- Read-only maps as package-level variables (3~6 entries each, negligible impact)
- Repeated inline type assertions → could be cleaned up with typed getter helpers
- Full-tree cloning via iteration — investigate whether read-only access can replace cloning

---

### Scope of Changes

**Staker realm:** types, store, deposit, tree, pool, getter_utils, v1/ (all files)
**Pool realm:** pool.gno, utils.gno, oracle.gno
**Position realm:** position.gno, store.gno, v1/burn.gno
**Gov modules:** gov/staker/delegation, tree, store, types, v1/ (all files)
**Launchpad:** store.gno

### Test Plan

- [ ] All txtar integration tests pass
- [ ] Storage deposit measurement txtars (base_storage_gas_measurement, etc.) confirm no regressions
- [ ] Lifecycle txtars (staker, position, router, pool) confirm functional correctness
- [ ] Verify gas/storage numbers for CollectReward, StakeToken, and UnStakeToken match expected values
