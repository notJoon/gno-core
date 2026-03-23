# GnoSwap Storage Pattern Audit Report

**Date:** 2026-03-09
**Scope:** `examples/gno.land/r/gnoswap/*` (all realm packages)
**Methodology:** Automated pattern search + manual analysis of 8 anti-pattern categories

---

## Executive Summary

A systematic scan of the GnoSwap contract codebase identified **8 distinct issue categories** across high, medium, and low severity levels. The most impactful optimization opportunities are in **struct layout** (Pool with 15 fields re-serialized on every swap) and **pointer slice usage** (unbounded `[]*DelegationWithdraw`).

The codebase is already well-optimized in some areas: count methods consistently use `avl.Tree.Size()`, and package-level slices are bounded/fixed-size.

| Severity | Issues Found |
|----------|-------------|
| High     | 2 |
| Medium   | 4 |
| Low      | 2 |

> **Note on Gno semantics:** In Gno, (1) package-level `map` variables **do persist** across transactions — they are Objects in the VM's type system and are serialized/restored automatically. (2) Gno transactions are **fully atomic** — if a panic occurs at any point, all state changes in the transaction are rolled back. There is no partial-commit scenario.

---

## High

### 1. Pointer slice in persistent state

**File:** `examples/gno.land/r/gnoswap/gov/staker/delegation.gno:25`

```go
type Delegation struct {
    id               int64
    delegateAmount   int64
    unDelegateAmount int64
    collectedAmount  int64
    delegateFrom     address
    delegateTo       address
    createdHeight    int64
    createdAt        int64
    withdraws        []*DelegationWithdraw  // each pointer = separate Object
}
```

The `Delegation` struct is stored in an `avl.Tree` via `SetDelegation()`. Each element in a `[]*Struct` slice becomes a separate Gno Object, incurring a **2,000 gas flat cost per element** on every write to the parent struct.

`DelegationWithdraw` has 8 fields (7 `int64` + 1 `bool` = ~57 bytes serialized). The `withdraws` list grows with each undelegation via `AddWithdraw()` and is only pruned when collected. There is no upper bound on list size.

Additional overhead:
- `Delegation.Clone()` iterates all withdrawals and clones each one individually (O(n))
- Collection operations iterate all withdrawals including already-collected ones

**Impact:** A delegation with N pending withdrawals costs an additional `N * 2,000` gas on every update, even if the withdrawals themselves haven't changed. The Clone overhead compounds this on every delegation operation.

**Fix options:**

| Approach | When to use |
|----------|-------------|
| `[]DelegationWithdraw` (value slice) | If the list stays under ~100 items — eliminates per-element Object overhead |
| Separate `avl.Tree` keyed by withdrawal ID | If the list can grow unbounded or individual withdrawals need independent access |

---

### 2. Large structs with mixed-frequency fields

When independently-changing data lives in the same struct, modifying **any** field re-serializes the **entire** struct (dirty ancestor propagation). The following structs are the strongest candidates for splitting:

#### Pool (15 fields) — `pool/pool.gno:15`

| Category | Fields |
|----------|--------|
| **Static** (set once) | `token0Path`, `token1Path`, `fee`, `tickSpacing`, `maxLiquidityPerTick` |
| **Dynamic** (every swap) | `slot0`, `balances`, `protocolFees`, `liquidity`, `feeGrowthGlobal0X128`, `feeGrowthGlobal1X128` |
| **Collections** (tree refs) | `ticks`, `tickBitmaps`, `positions`, `observationState` |

**Impact:** Every swap re-serializes all 15 fields even though only ~6 change. The 5 immutable fields and 4 collection references are re-serialized unnecessarily. Splitting into `PoolConfig` (immutable) + `PoolState` (dynamic) would eliminate redundant serialization of static fields on every swap.

#### staker/Deposit (13 fields) — `staker/deposit.gno:10`

| Category | Fields |
|----------|--------|
| **Static** | `owner`, `targetPoolPath`, `stakeTime`, `tickLower`, `tickUpper`, `liquidity` |
| **Dynamic** (reward collection) | `collectedInternalReward`, `internalRewardLastCollectTime`, `collectedExternalRewards`, `externalRewardLastCollectTimes`, `lastExternalIncentiveUpdatedAt`, `warmups`, `externalIncentiveIds` |

**Impact:** Reward collection re-serializes immutable position data. Splitting into `DepositInfo` + `DepositRewards` halves write cost.

#### Other candidates

| Struct | File | Fields | Split opportunity |
|--------|------|--------|-------------------|
| `staker/Pool` | `staker/pool.gno:56` | 14 | Config (3) vs State (5) vs History (2) |
| `RewardManager` | `launchpad/reward_manager.gno:25` | 11 | Config (5) vs Accumulation (6) |
| `Position` | `position/position.gno:7` | 12 | Identity (4) vs Accrual (8) |
| `ProjectTier` | `launchpad/project_tier.gno:25` | 10 | Config (5) vs Accounting (5) |

---

## Medium

### 3. Mutable map in access control — performance concern

**File:** `examples/gno.land/r/gnoswap/access/access.gno:11`

```go
var roleAddresses map[string]address  // initialized in init()
```

This map is modified at runtime via `SetRoleAddress()` and `RemoveRole()`.

> **Clarification:** This is **not** a correctness bug. In Gno, package-level maps are Objects and **do persist** across transactions. The `roleAddresses` map will correctly retain modifications between calls.

**The actual issue is performance:** When any entry in a map is accessed, the **entire map** is loaded into memory as a single Object. For `roleAddresses` this is unlikely to be a problem in practice (typically < 10 entries), but using `avl.Tree` would be more consistent with the rest of the codebase and would scale better if the role set grows.

**Impact:** Low — the role set is small. This is a code consistency and future-proofing concern rather than a current gas bottleneck.

**Fix (optional):** Replace with `avl.Tree` for consistency:

```go
var roleAddresses = avl.NewTree() // key: role string, value: std.Address
```

---

### 4. Multiple avl.Trees indexing the same data — maintainability concern

Several modules maintain multiple `avl.Tree` variables that index the same underlying data.

> **Clarification:** This is **not** an atomicity/consistency risk. Gno transactions are fully atomic — if a panic occurs between updating tree A and tree B, **all changes are rolled back**. There is no partial-commit scenario.

The actual concern is **code maintainability**: developers must remember to update all related trees together, and forgetting to do so in new code paths would create a logic bug (not a storage-level consistency issue).

#### 4a. Staker module (7 trees)

**File:** `examples/gno.land/r/gnoswap/staker/store.gno`

| Tree | Purpose |
|------|---------|
| `StoreKeyDeposits` | All position deposits |
| `StoreKeyStakers` | Per-user staking info |
| `StoreKeyExternalIncentives` | Incentive programs |
| `StoreKeyExternalIncentivesByCreationTime` | Time-indexed incentive view |
| `StoreKeyPools` | Pool participation |
| `StoreKeyPoolTierMemberships` | Pool tier assignments |
| `StoreKeyTokenSpecificMinimumRewards` | Per-token reward minimums |

#### 4b. Protocol fee module (4 trees)

**File:** `examples/gno.land/r/gnoswap/protocol_fee/store.gno`

| Tree | Purpose |
|------|---------|
| `StoreKeyAccuToGovStaker` | Accumulated fees for gov/staker |
| `StoreKeyAccuToDevOps` | Accumulated fees for devops |
| `StoreKeyDistributedToGovStakerHistory` | Distribution history (gov/staker) |
| `StoreKeyDistributedToDevOpsHistory` | Distribution history (devops) |

#### 4c. Launchpad module (3 trees)

**File:** `examples/gno.land/r/gnoswap/launchpad/store.gno`

Trees: `Projects`, `ProjectTierRewardManagers`, `Deposits`.

**Recommendation:** Grouping related trees into a struct with compound-update methods would make the relationship explicit and reduce the chance of future logic bugs:

```go
type StakerState struct {
    deposits     *avl.Tree
    stakers      *avl.Tree
    incentives   *avl.Tree
    incentiveIdx *avl.Tree // by creation time
}

func (s *StakerState) AddIncentive(id string, incentive *Incentive) {
    s.incentives.Set(id, incentive)
    s.incentiveIdx.Set(timeKey(incentive.CreatedAt), id)
    // All related updates in one method — harder to forget
}
```

---

### 5. No `seqid` usage for compact tree keys

**Finding:** Zero imports of `seqid` across the entire codebase. All numeric IDs are converted to strings via `strconv.FormatUint()` or custom wrappers.

**Examples:**
- `position/store.gno:125-127` — `uint64ToString()` wrapper around `strconv.FormatUint(id, 10)`
- Deposit IDs, incentive IDs, and delegation IDs all follow this pattern

**Impact:** String-encoded number `"12345"` uses 5 bytes. `seqid.Binary()` produces a fixed 8-byte encoding that maintains sort order. For small IDs the difference is negligible, but for high-volume trees (positions, deposits) the savings accumulate.

| Format | ID "12345" | ID "9999999999" |
|--------|-----------|----------------|
| `strconv.FormatUint()` | 5 bytes | 10 bytes |
| `seqid.Binary()` | 8 bytes (fixed) | 8 bytes (fixed) |

For IDs < 100,000, `strconv` is actually shorter. The benefit of `seqid.Binary()` is **fixed width** and **consistent sort order**, not necessarily smaller size for small IDs.

**Fix:**

```go
import "gno.land/p/nt/seqid/v0"

var idGen seqid.ID

func nextKey() string {
    return idGen.Next().Binary() // overflow-safe, fixed 8 bytes
}
```

---

### 6. Read-only maps as package-level variables

**Files:**
- `halt/types.gno:27` — `haltLevelDescriptions map[HaltLevel]string`
- `halt/types.gno:65` — `opTypes map[OpType]bool`
- `pool/v1/init.gno:25` — `defaultFeeAmountTickSpacing map[uint32]int32`
- `rbac/role.gno:7` — `DefaultRoleAddresses map[prbac.SystemRole]address`
- `launchpad/v1/consts.gno:33,39` — tier duration/reward maps

These maps are initialized with literals and **never mutated** (verified via exhaustive search). They function as constant lookup tables.

**Impact:** Minor gas overhead — the entire map Object is loaded on any access. For small, fixed-size maps (3-6 entries each) the overhead is negligible. The main concern is code clarity: using `var` for data that should be `const` obscures intent and risks accidental mutation in future code changes.

**Fix (optional):** Replace with switch statements or helper functions for the smallest maps. Low priority.

---

## Low

### 7. Repeated inline type assertions on `avl.Tree.Get()`

Files with 3+ inline type assertions, indicating missing helper functions:

| File | Occurrences | Suggested helpers |
|------|-------------|-------------------|
| `referral/keeper.gno` | 6 | `getString()`, `getInt64()` |
| `position/store.gno` | 4 | `getPosition()` |
| `staker/v1/getter.gno` | 4+ | `getPool()`, `getIncentive()` |
| `launchpad/v1/getter.gno` | 3+ | `getProject()` |

**Impact:** Code quality and safety, not gas. Repeated assertions are error-prone and obscure the happy path.

**Fix pattern:**

```go
func (s *Store) getPosition(key string) (*Position, bool) {
    v, ok := s.positions.Get(key)
    if !ok {
        return nil, false
    }
    return v.(*Position), true
}
```

---

### 8. Temporary pointer slice in non-persistent struct

**File:** `examples/gno.land/r/gnoswap/staker/pool.gno:717-728`

```go
type SwapBatchProcessor struct {
    poolPath  string
    pool      *Pool
    crosses   []*SwapTickCross  // pointer slice
    timestamp int64
    isActive  bool
}
```

This struct is only used during swap execution and is not persisted. The pointer slice causes unnecessary heap allocations but has no storage impact.

**Impact:** Minor runtime overhead during swaps. Converting to `[]SwapTickCross` would reduce allocations but the gas savings are minimal since this struct is transient.

---

## Already Optimized (No Action Needed)

The following areas were scanned and found to be well-optimized:

- **Count methods** — `GetTotalStakedUserCount()`, `GetPoolRewardCacheCount()`, `GetPoolIncentiveCount()`, `GetTotalStakedUserPositionCount()` (in `staker/v1/getter.gno`) all use `avl.Tree.Size()` instead of manual counters
- **Package-level slices** — Only one found (`staker/v1/init.gno:22`: `defaultAllowedTokens = []string{GNS_PATH, WUGNOT_PATH}`), which is bounded at 2 elements
- **Tier count arrays** — `staker/v1/reward_calculation_pool_tier.gno` uses `[AllTierCount]uint64` (fixed 4-element array), which is more efficient than tree iteration for this use case

---

## Additional Findings (Supplementary Analysis)

The following findings were identified through deeper analysis beyond the initial 8-pattern scan. They represent significant optimization opportunities not covered in the original report.

### 9. `*u256.Uint` pointer fields creating excessive Objects (High)

In Gno, each `*u256.Uint` pointer field in a struct becomes a **separate Object** with its own ObjectID, hash, and KV storage entry. `u256.Uint` is defined as `[4]uint64` (32 bytes of data), but each pointer incurs ~2,000 gas flat write cost plus ObjectID overhead.

**Object count per struct instance:**

| Struct | `*u256.Uint` fields | Extra Objects per instance |
|--------|---------------------|--------------------------|
| Pool (pool/pool.gno) | 9 (4 direct + 1 Slot0 + 2×2 TokenPair) | **9** |
| Position (position/position.gno) | 7 | **7** |
| PositionInfo (pool/pool.gno) | 5 | **5** |
| TickInfo (pool/pool.gno) | 4 | **4 per tick** |

**Impact:** A single Pool stores 9 u256 Objects + 3 avl.Tree Objects + the Pool itself = **13+ Objects** for one pool. With 100 active ticks, that's 13 + 400 = **413 Objects** for one pool.

Each Position creates 7 additional Objects. A system with 10,000 positions has **70,000 u256 Objects** just for position fields.

**Cost calculation:** Each u256 value is 32 bytes of data, but stored as a separate Object costs:
- VM gas: 16 × ~80 bytes (amino-encoded) = 1,280 gas
- KV gas: 2,000 + 30 × 100 = 5,000 gas
- Total: ~6,280 gas **per u256 field per write**

If these were inline value types instead of pointers, they would be serialized as part of the parent struct with zero additional per-field overhead.

**Note:** This may require changes to the `u256` package itself to support value-type usage, which could be a significant refactor. Evaluate whether `Uint` can be used as a value type (not pointer) in struct fields where the value is always owned by a single parent.

---

### 10. Missing secondary indexes causing O(n) full-tree scans (High)

Several critical operations iterate the **entire global tree** to filter by a secondary attribute, instead of maintaining a dedicated index.

#### 10a. External incentive refund — scans all deposits globally

**File:** `staker/v1/external_incentive.gno:242`

```go
s.getDeposits().IterateByPoolPath(0, math.MaxUint64, incentiveResolver.TargetPoolPath(),
    func(positionId uint64, deposit *sr.Deposit) bool {
        // calculate reward for EACH matching deposit
```

Iterates **all deposits across all pools** (0 to MaxUint64) to find deposits matching a specific pool path. For a system with 10,000 deposits, this loads 10,000 Objects to process one incentive refund.

**Fix:** Add a secondary index `poolPath → []depositId` to enable direct lookup.

#### 10b. GetExternalIncentiveByPoolPath — scans all incentives globally

**File:** `staker/v1/getter.gno:447`

```go
s.store.GetExternalIncentives().Iterate("", "", func(_ string, value any) bool {
    incentive := value.(*sr.ExternalIncentive)
    if incentive.TargetPoolPath() == poolPath {
        incentives = append(incentives, *incentive)
    }
```

Iterates **all incentives** with runtime filtering. Fix: secondary index `poolPath → []incentiveId`.

#### 10c. GetPoolsByTier — scans all pool-tier memberships

**File:** `staker/v1/getter.gno:363`

Iterates all pool-tier mappings to find pools in a specific tier. Fix: reverse index `tier → []poolPath`.

#### 10d. Fee protocol update — rewrites all pools

**File:** `pool/v1/manager.gno:273`

When the protocol fee changes, **every pool's Slot0** is rewritten. With 1,000 pools, this is 1,000 Object writes for a single admin operation.

**Fix:** Store the protocol fee as a single global variable. Pools read from the global on access instead of each storing their own copy.

---

### 11. Redundant writes — same Object serialized multiple times per transaction (High)

#### 11a. updatePoolTier() — 8 separate store writes for one logical update

**File:** `staker/v1/instance.gno:38-77`

```go
func (s *StakerImplementation) updatePoolTier(pt *sr.PoolTier) {
    s.store.SetPoolTierMemberships(pt.Memberships())       // write 1
    s.store.SetPoolTierRatio(pt.Ratio())                   // write 2
    s.store.SetPoolTierCounts(pt.Counts())                 // write 3
    s.store.SetPoolTierLastRewardCacheTimestamp(pt.Last..) // write 4
    s.store.SetPoolTierLastRewardCacheHeight(pt.Last...)   // write 5
    s.store.SetPoolTierCurrentEmission(pt.Current...)      // write 6
    s.store.SetPoolTierGetEmission(pt.GetEmission())       // write 7
    s.store.SetPoolTierGetHalvingBlocksInRange(pt.Get...)  // write 8
}
```

The `PoolTier` is a single logical object, but it's decomposed into 8 separate KV store writes. Each write incurs a 2,000 gas flat cost. **Total overhead: 8 × 2,000 = 16,000 gas** that could be reduced to a single write (2,000 gas) by storing `PoolTier` as one Object.

#### 11b. decreaseLiquidity() — position saved twice

**File:** `position/v1/burn.gno:80-147`

The position is modified and saved at line 88 via `mustUpdatePosition()`, then modified again and saved a second time at line 147. The first write is wasted — the Object is serialized, hashed, and stored only to be immediately overwritten.

**Fix:** Accumulate all modifications before a single final save.

---

### 12. Deep Object nesting in staker Pool (UintTree wrapper anti-pattern) (Medium)

**File:** `staker/pool.gno:56-73`

The staker `Pool` struct contains **6 `*UintTree` fields**, where `UintTree` is a thin wrapper around `*avl.Tree`:

```go
type UintTree struct {
    tree *avl.Tree
}
```

This creates a 4-5 level deep ownership chain:

```
KVStore → pools tree → Pool → stakedLiquidity (UintTree) → avl.Tree → nodes
                             → rewardCache (UintTree) → avl.Tree → nodes
                             → incentives → avl.Tree → ExternalIncentive
                             → ticks → avl.Tree → Tick → outsideAccumulation (UintTree) → avl.Tree
                             → globalRewardRatioAccumulation (UintTree) → avl.Tree
                             → historicalTick (UintTree) → avl.Tree
```

Each `UintTree` wrapper adds one Object to the ownership chain. With 6 wrappers per Pool, that's 6 extra Objects that exist solely to hold a pointer to an `avl.Tree`.

**Worst case:** The `Tick` struct contains `outsideAccumulation *UintTree`, creating per-tick nesting. For a pool with 100 ticks, that's 100 additional `UintTree` wrapper Objects.

**Fix:** Remove `UintTree` wrapper — use `*avl.Tree` directly with helper functions for key conversion.

---

### 13. Deposit struct — 3 correlated avl.Trees that could be 1 (Medium)

**File:** `staker/deposit.gno:10-24`

Each `Deposit` contains 3 separate `*avl.Tree` fields tracking external incentive state:

```go
collectedExternalRewards       *avl.Tree  // incentiveID → int64
externalRewardLastCollectTimes *avl.Tree  // incentiveID → int64
externalIncentiveIds           *avl.Tree  // incentiveID → bool
```

These 3 trees share the same key space (`incentiveID`) and are always accessed together. Each tree is a separate Object.

**Fix:** Combine into a single tree with a composite value:

```go
type ExternalRewardState struct {
    collected     int64
    lastCollectAt int64
    active        bool
}
var externalRewards *avl.Tree  // incentiveID → ExternalRewardState
```

This eliminates 2 Objects per Deposit and reduces the number of tree lookups from 3 to 1 per incentive.

---

### 14. Composite string keys inflating storage costs (Medium)

#### 14a. Pool path keys — ~54 bytes per key

**File:** `pool/utils.gno:15-23`

```go
func GetPoolPath(token0Path, token1Path string, fee uint32) string {
    return token0Path + ":" + token1Path + ":" + strconv.FormatUint(uint64(fee), 10)
}
// Example: "gno.land/r/gnoland/wugnot:gno.land/r/gnoswap/gns:3000" (~54 bytes)
```

This key is stored as `poolKey` in every `Position` struct and used as a lookup key in multiple trees. A hashed or indexed key would be significantly shorter.

#### 14b. Incentive IDs — ~58+ bytes per key

**File:** `staker/store.gno:569-571`

```go
func makeIncentiveID(creator address, timestamp int64, index int64) string {
    return ufmt.Sprintf("%s:%d:%d", creator.String(), timestamp, index)
}
// Example: "g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5:1709913600:0" (~58 bytes)
```

#### 14c. Tick keys — zero-padded to 10 bytes

**File:** `pool/utils.gno:39-53`

```go
func EncodeTickKey(tick int32) string {
    adjustTick := tick + ENCODED_TICK_OFFSET
    s := strconv.FormatInt(int64(adjustTick), 10)
    return strings.Repeat("0", 10 - len(s)) + s
}
```

Every tick key is padded to 10 bytes regardless of value. With `seqid.Binary()` style encoding, tick values could be stored in 4 bytes.

---

### 15. Swap hot-loop `u256.Clone()` allocations (Medium)

**File:** `pool/v1/swap.gno:365-496`

Inside the main swap loop, `Clone()` is called on every iteration:

```go
for shouldContinueSwap(state, comp.SqrtPriceLimitX96) {
    // ...
    sqrtRatioTargetX96 := computeTargetSqrtRatio(step, sqrtPriceLimitX96, zeroForOne).Clone()
    // ...
}
```

Each `Clone()` allocates 64 bytes on the heap. Complex swaps crossing many ticks can iterate 100+ times, creating **6.4 KB+ of temporary allocations** per swap. These allocations increase GC pressure and contribute to gas consumption via the Gno memory allocator.

**Fix:** Pre-allocate a reusable `u256.Uint` outside the loop and use `Set()` / `Copy()` methods instead of `Clone()`.

---

### 16. Full-tree cloning via iteration (Low)

**File:** `staker/pool.gno:590`, `staker/tree.gno:80`

```go
func (t Ticks) Clone() Ticks {
    cloned := avl.NewTree()
    t.tree.Iterate("", "", func(key string, value any) bool {
        cloned.Set(key, value)
        return false
    })
    return Ticks{tree: cloned}
}
```

Tree cloning iterates every node and copies to a new tree — O(n) reads + O(n) writes. If the tree has 1,000 entries, that's 2,000 store operations.

**Fix:** Consider whether cloning is necessary. If the tree is only read after cloning, use the original with read-only access patterns instead.

---

## Recommended Priority Order — Quick Wins Only

The following items are **low-effort, localized changes** that do not require major data structure redesigns, key format migrations, or package-level refactors. Each can be implemented and tested independently.

Items excluded from this list (documented above for future reference): Pool/Deposit struct splitting (#1, #2, #6), `*u256.Uint` value-type conversion (#9), secondary index introduction (#10a-c), `UintTree` wrapper removal (#12), composite key shortening (#14), avl.Tree struct grouping.

| Priority | Action | Scope | Estimated Savings |
|----------|--------|-------|-------------------|
| 1 | Consolidate `updatePoolTier()` into single store write (#11a) | `staker/v1/instance.gno` — 1 function | **14,000 gas saved** per pool tier update (7 redundant writes eliminated) |
| 2 | Fix double-save in `decreaseLiquidity()` (#11b) | `position/v1/burn.gno` — 1 function | **~6,000+ gas saved** per liquidity decrease (1 wasted serialization of 12-field Position) |
| 3 | Convert `[]*DelegationWithdraw` to `[]DelegationWithdraw` (#2) | `gov/staker/delegation.gno` — type change + callsite updates | **N × 2,000 gas saved** per delegation update (N = pending withdrawals) |
| 4 | Merge Deposit's 3 external reward trees into 1 (#13) | `staker/deposit.gno` — struct change + getter/setter updates | **2 Objects eliminated per Deposit** + 2 fewer tree lookups per incentive |
| 5 | Pre-allocate `u256.Uint` in swap loop instead of `Clone()` (#15) | `pool/v1/swap.gno` — loop body change | **64 bytes × iterations** heap allocation eliminated (100+ iterations on complex swaps) |
| 6 | Store protocol fee globally instead of per-pool (#10d) | `pool/v1/manager.gno` + pool read path | **O(pools) writes → O(1)** on admin fee change |
| 7 | Replace mutable map in `access/access.gno` (#3) | `access/access.gno` — 1 file | Code consistency with rest of codebase |
| 8 | Add typed getter helpers for avl.Tree (#7) | `referral/keeper.gno`, `position/store.gno`, etc. | Code quality — reduces error-prone inline type assertions |
| 9 | Convert read-only maps to switch/const (#6) | `halt/types.gno`, `rbac/role.gno`, etc. | Minor gas savings + better immutability intent |

---

## Corrections from Initial Draft

This report corrects the following errors in the initial draft:

| Item | Initial claim | Correction |
|------|--------------|------------|
| #1 (was Critical) | "Maps do not persist across transactions in Gno" — classified as correctness bug | **Wrong.** Package-level maps in Gno are Objects and do persist. Downgraded to Medium (performance/consistency concern). |
| #3 (was High) | "A panic between tree writes leaves inconsistent state" — atomicity risk | **Wrong.** Gno transactions are fully atomic. A panic rolls back all changes. Downgraded to Medium (maintainability concern). |
| #4 (was High) | Read-only maps classified as High severity | **Overstated.** Maps are never mutated and have minimal entries. Downgraded to Medium. |
| #5 Pool field count | "Pool has 15 fields" | **Confirmed correct** (15 fields verified). |
| #6 seqid impact | "seqid.Binary() is shorter" | **Partially misleading.** For small IDs (< 100K), `strconv` is actually shorter. Fixed-width consistency is the real benefit. |
| Already Optimized | "GetDepositCount(), GetProjectCount(), GetPositionCount()" | **Method names incorrect.** Actual methods: `GetTotalStakedUserCount()`, `GetPoolRewardCacheCount()`, etc. (in `staker/v1/getter.gno`). Behavior (using `.Size()`) confirmed correct. |
| File paths | All paths used `contract/r/gnoswap/` prefix | Should be `examples/gno.land/r/gnoswap/` |
| Protocol fee trees | "5 trees" | **Actually 4 trees** (`TokenListWithAmounts` is a map, not an avl.Tree) |
| Staker trees | "6 interdependent trees" | **Actually 7 trees** (missing `TokenSpecificMinimumRewards`) |
| SwapBatchProcessor | "Line 724, 3 fields shown" | **Actually line 717, 5 fields** (poolPath, pool, crosses, timestamp, isActive) |
| Position struct | "11 fields" | **Actually 12 fields** (missing `burned` bool) |
