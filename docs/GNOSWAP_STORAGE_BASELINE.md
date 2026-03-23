# GnoSwap Storage Cost Baseline (Pre-Optimization)

> Measured: 2026-03-09
> Branch: `master` @ `99662881c`
> Test files:
> - `gno.land/pkg/integration/testdata/position_storage_poisition_lifecycle.txtar`
> - `gno.land/pkg/integration/testdata/staker_storage_staker_lifecycle.txtar`

## Summary

| Category | Representative Operation | Storage Delta | Estimated Cost | Target |
|----------|--------------------------|---------------|----------------|--------|
| Pool Creation | CreatePool | 20,746 bytes | ~2.07 GNOT | < 1 GNOT |
| Position Mint (1st) | Mint (wide range, new pool) | 40,763 bytes | ~4.08 GNOT | < 1 GNOT |
| Position Mint (2nd) | Mint (narrow range, new ticks) | 39,501 bytes | ~3.95 GNOT | < 1 GNOT |
| Position Mint (3rd) | Mint (reuse existing ticks) | 13,105 bytes | ~1.31 GNOT | < 1 GNOT |
| Staker SetPoolTier | SetPoolTier | 24,427 bytes | ~2.44 GNOT | < 1 GNOT |
| Staker StakeToken | StakeToken | 23,447 bytes | ~2.34 GNOT | < 1 GNOT |
| Staker CollectReward (1st) | CollectReward (0 reward) | 5,758 bytes | ~0.58 GNOT | < 0.5 GNOT |
| Staker CollectReward (2nd) | CollectReward (repeat) | 0 bytes | 0 GNOT | - |
| Staker UnStakeToken | UnStakeToken | -3,304 bytes | refund 0.33 GNOT | - |
| CollectFee (1st) | CollectFee (wide pos) | 2,259 bytes | ~0.23 GNOT | < 0.5 GNOT |
| CollectFee (2nd) | CollectFee (narrow pos) | 64 bytes | ~0.01 GNOT | - |
| DecreaseLiquidity | DecreaseLiquidity | 5 bytes | ~0 GNOT | - |
| Swap | Swap (via wrapper) | 12 bytes | ~0 GNOT | - |

> Cost estimate: `STORAGE DELTA * 100 ugnot/byte = deposit cost`. 1 GNOT = 1,000,000 ugnot = 10,000 bytes.

---

## 1. Position Lifecycle (WUGNOT:GNS:3000)

Test file: `position_storage_poisition_lifecycle.txtar`

### Step-by-step measurements

| Step | Operation | Storage Delta | GAS USED | TX Cost |
|------|-----------|---------------|----------|---------|
| 1 | **CreatePool** (WUGNOT:GNS:3000, tick=0) | 20,746 bytes | 33,782,761 | 12,074,600 ugnot |
| 2 | **Mint #1** (wide range -887220~887220, 50M liq) | 40,763 bytes | 56,117,084 | 104,076,300 ugnot |
| 3 | **Mint #2** (narrow range -60~60, 10M liq) | 39,501 bytes | 54,941,454 | 103,950,100 ugnot |
| 4 | **Mint #3** (same ticks as #2, 5M liq) | 13,105 bytes | 54,195,161 | 101,310,500 ugnot |
| 5 | **Swap** (30M GNS, zeroForOne=false) | 12 bytes | 43,422,792 | 100,001,200 ugnot |
| 6 | **CollectFee** (position #1, wide range) | 2,259 bytes | 44,671,803 | 100,225,900 ugnot |
| 7 | **CollectFee** (position #2, narrow range) | 64 bytes | 44,569,426 | 100,006,400 ugnot |
| 8 | **DecreaseLiquidity** (position #2, 100K liq) | 5 bytes | 55,637,684 | 100,000,500 ugnot |

### Analysis

- **Mint #1 vs Mint #2**: Both ~40K bytes despite Mint #2 being a narrow range in existing pool. The cost is dominated by KVStore map re-serialization and emission overhead, NOT by new tick/pool creation.
- **Mint #3 (reused ticks)**: Drops to 13K bytes — shows that ~27K bytes of the Mint cost comes from tick data creation (2 new TickInfo entries + bitmap updates).
- **CollectFee #1 vs #2**: First CollectFee costs 2,259 bytes (new fee accounting entries), second costs only 64 bytes (updating existing entries). Once the fee collection path is established, subsequent calls are cheap.
- **Swap**: Almost zero storage delta (12 bytes) — swap modifies existing state in-place without creating new entries.

---

## 2. Staker Lifecycle (BAR:FOO:3000)

Test file: `staker_storage_staker_lifecycle.txtar`

### Step-by-step measurements

| Step | Operation | Storage Delta | GAS USED | TX Cost |
|------|-----------|---------------|----------|---------|
| Setup | CreatePool (BAR:FOO:3000) | 20,735 bytes | 33,800,729 | 12,073,500 ugnot |
| Setup | Mint #1 (-60~60, 5M liq) | 40,733 bytes | 53,023,920 | 14,073,300 ugnot |
| 1 | **SetPoolTier** (tier 1) | 24,427 bytes | 42,222,970 | 12,442,700 ugnot |
| 2 | **StakeToken** (position #1) | 23,447 bytes | 56,970,527 | 12,344,700 ugnot |
| 3 | **CollectReward** (1st call, 0 reward) | 5,758 bytes | 49,586,755 | 10,575,800 ugnot |
| 4 | **CollectReward** (2nd call, repeat) | 0 bytes | 36,881,530 | ~10,000,000 ugnot |
| 5 | **UnStakeToken** (position #1) | -3,304 bytes | 59,458,414 | 9,669,600 ugnot |

### Analysis

- **SetPoolTier (24K bytes)**: Creating a pool tier writes 8+ keys in the staker KVStore. Each key update triggers full map re-serialization (20+ keys total). The majority of the 24K bytes is re-serialization overhead.
- **StakeToken (23K bytes)**: Creates deposit entry, updates staked liquidity tracking, NFT transfer. Similar overhead to SetPoolTier due to KVStore map.
- **CollectReward 1st vs 2nd**: First call costs 5,758 bytes (emission state initialization + staker internal accounting). Second call costs 0 bytes — no new state created, everything already exists.
- **UnStakeToken (-3,304 bytes)**: Successfully releases storage. The negative delta shows that deposit removal + NFT transfer back results in a net refund.

---

## 3. Cost Breakdown by Root Cause

Based on call stack analysis (see `GNOSWAP_STORAGE_COST_ANALYSIS.md`):

### 3.1. KVStore `map[string]any` Re-serialization

The primary cost driver. Each domain's KVStore uses a Go `map[string]any` as backing store. Modifying ANY value re-serializes the ENTIRE map including all keys and RefValues.

| Domain | Approx. Keys | Impact |
|--------|-------------|--------|
| staker | 20+ | Highest — every staker operation re-serializes all 20+ entries |
| pool | 8+ | High — every pool modification re-serializes all pool entries |
| position | 2 | Moderate — positions tree + nextID |
| emission | 10+ | High — called on every user operation via `MintAndDistributeGns()` |

### 3.2. `emission.MintAndDistributeGns()` Overhead

Called in EVERY user-facing operation (Mint, CollectFee, CollectReward, StakeToken, etc.):
- GNS minting: modifies 3+ package vars + HalvingData (7 slices)
- 4x GNS Transfer (staker, devops, communityPool, govStaker)
- 8 distribution tracking vars updated
- Estimated: ~50 Objects modified per call

### 3.3. Pointer-heavy Struct Fields

Pool struct uses 9 `*u256.Uint` pointer fields — each is a separate Object with 2,000 gas flat cost per write. TickInfo has 5, PositionInfo has 5.

### 3.4. Dirty Ancestor Propagation

Modifying a child Object causes the entire owner chain (up to PackageValue) to be re-serialized. Multi-realm operations (Mint touches 8+ realms) compound this effect.

---

## 4. Optimization Priority

| Priority | Optimization | Expected Reduction | Affected Operations |
|----------|-------------|-------------------|---------------------|
| P0 | KVStore `map` -> `avl.Tree` | 35-45% | All operations |
| P0 | Emission batching / lazy evaluation | 15-20% | Mint, CollectFee, CollectReward, StakeToken |
| P1 | Pool struct pointer -> value types | 10-15% | Pool write operations |
| P1 | Staker KVStore key reduction | 5-10% | Staker operations |
| P2 | ObservationState optimization | Risk mitigation | Swap (long-term growth) |

### Target: All operations < 1 GNOT (< 10,000 bytes storage delta)

| Operation | Current | Target | Required Reduction |
|-----------|---------|--------|--------------------|
| Mint (1st) | 40,763 bytes | < 10,000 bytes | **-75%** |
| Mint (reuse ticks) | 13,105 bytes | < 10,000 bytes | **-24%** |
| SetPoolTier | 24,427 bytes | < 10,000 bytes | **-59%** |
| StakeToken | 23,447 bytes | < 10,000 bytes | **-57%** |
| CollectReward | 5,758 bytes | < 5,000 bytes | **-13%** |
| CollectFee | 2,259 bytes | < 2,000 bytes | **-11%** |

---

## 5. How to Reproduce

```bash
# Remove unit tests first (required)
cd gno.land/pkg/integration

# Position lifecycle
go test -v -run TestTestdata/position_storage_poisition_lifecycle -timeout 600s

# Staker lifecycle
go test -v -run TestTestdata/staker_storage_staker_lifecycle -timeout 600s
```

After applying optimizations, re-run and compare STORAGE DELTA values against this baseline.
