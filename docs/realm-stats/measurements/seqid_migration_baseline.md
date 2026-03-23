# Seqid Migration: Baseline & Results

Date: 2026-03-16
Branch: `perf/object-dirty-log`
Commit: `668f4174a`

## Environment

- `GNO_REALM_STATS_LOG=stderr`
- All tests run with `-timeout 5m`

---

## Baseline Measurements (Pre-Migration)

### pool_create_pool_and_mint

| Step | STORAGE DELTA (bytes) |
|------|----------------------|
| Deploy pool (init) | 5,768 |
| Deploy wugnot | 1,081 |
| CreatePool | 19,150 |
| Approve tokens | 1,037 |
| Approve tokens | 1,074 |
| Approve tokens | 2,118 |
| Mint (full) | 32,032 |
| Mint (additional) | 2,217 |
| Misc | 4 |
| Misc | 2,133 |
| Swap | 5,688 |
| Final | 55 |

### pool_swap_wugnot_gns_tokens

| Step | STORAGE DELTA (bytes) |
|------|----------------------|
| Deploy pool | 5,768 |
| Deploy gov/staker | 2,043 |
| Deploy gov/staker | 2,049 |
| Misc | 1 |
| Deploy | 1,082 |
| CreatePool | 19,154 |
| Approve | 1,037 |
| Approve | 1,074 |
| Approve | 2,118 |
| Mint | 32,031 |
| Position setup | 2,133 |
| Swap | 19,863 |

### position_storage_poisition_lifecycle

| Step | STORAGE DELTA (bytes) |
|------|----------------------|
| Deploy | 5,767 |
| Approve | 1,037 |
| Deploy wugnot | 1,081 |
| Approve | 1,074 |
| Approve | 2,118 |
| CreatePool | 19,150 |
| Mint | 32,018 |
| Misc | 1 |
| Swap (via position) | 30,728 |
| CollectFee | 10,289 |
| Position setup | 2,133 |
| Misc | 6 |
| DecreaseLiquidity | 2,216 |
| Misc | 50 |
| Final | 14 |

### staker_storage_staker_stake_only

| Step | STORAGE DELTA (bytes) |
|------|----------------------|
| Deploy staker (x4) | 2,029 / 2,034 / 2,029 / 2,034 |
| Approve (x4) | 1,074 / 1,074 / 2,118 / 2,118 |
| Deploy wugnot | 1,081 |
| CreatePool | 19,139 |
| Mint | 31,986 |
| StakeToken | 24,349 |
| Misc | 1,061 |
| StakeToken (2nd) | 23,421 |

### staker_storage_staker_lifecycle

| Step | STORAGE DELTA (bytes) |
|------|----------------------|
| Deploy staker (x2) | 2,029 / 2,029 |
| Approve (x4) | 1,074 / 1,074 / 2,118 / 2,118 |
| Deploy wugnot | 1,081 |
| CreatePool | 19,139 |
| Mint | 31,986 |
| StakeToken | 24,349 |
| Misc | 1,061 |
| CollectReward + UnStake | 32,982 |
| Final cleanup | 3,350 |

### router_exact_in_single_swap_route

| Step | STORAGE DELTA (bytes) |
|------|----------------------|
| Deploy router (x2) | 2,043 / 2,048 |
| Misc | 2 |
| Deploy | 1,082 |
| CreatePool | 19,155 |
| Approve (x3) | 1,037 / 1,074 / 2,118 |
| Mint | 32,020 |
| Position setup | 2,118 |
| Swap (router) | 5,696 |

### gov_governance_create_text_proposal

| Step | STORAGE DELTA (bytes) |
|------|----------------------|
| Deploy gov (x2) | 2,043 / 2,049 |
| Misc | 1 |
| Deploy | 1,082 |
| GNS init | 19,388 |
| Delegate | 10,832 |
| Delegate | 2,580 |
| Misc | -6 |
| CreateTextProposal | 11,191 |
| Vote | 2,022 |

### launchpad_create_new_launchpad_project

| Step | STORAGE DELTA (bytes) |
|------|----------------------|
| Deploy launchpad (x2) | 2,029 / 2,034 |
| Approve | 1,074 |
| CreateProject | 28,120 |

---

## Migration Results

### Applied: Task 1 (pool EncodeTickKey) + Task 2 (staker EncodeInt)

Both tasks convert 10-byte zero-padded decimal tick keys to 7-byte seqid (cford32) encoding.
Task 2 also fixes a bug where `IterateTicks` used `strconv.Atoi(key)` instead of a proper decoder.

| Operation | Test | Before | After | Delta |
|-----------|------|--------|-------|-------|
| Mint | pool_create_pool_and_mint | 32,032 | 32,022 | **-10** |
| Mint | position_lifecycle | 32,018 | 32,008 | **-10** |
| Mint | router_exact_in_single_swap | 32,020 | 32,011 | **-9** |
| Mint | pool_swap_wugnot_gns | 32,031 | 32,021 | **-10** |
| Swap (via position) | position_lifecycle | 30,728 | 30,716 | **-12** |
| Mint (staker) | staker_stake_only | 31,986 | 31,978 | **-8** |
| StakeToken (2nd) | staker_stake_only | 23,421 | 23,411 | **-10** |

### Reverted: Task 3 (position uint64ToString)

Position IDs are small sequential values (1, 2, 3...). Decimal encoding produces 1-byte keys,
while seqid always produces 7-byte keys. Net effect was **+12 bytes** on CollectFee.

### Reverted: Task 8 (tick_bitmap wordPos)

wordPos values range -3466 to 3466. `strconv.Itoa` produces 1-5 byte keys,
while seqid always produces 7 bytes. Net effect was **+15 to +84 bytes** per operation.

### Skipped: Task 4-7 (staker/gov/launchpad uint64/int64 keys)

Same issue as Task 3 -- small sequential IDs where decimal encoding is shorter than seqid.

---

## Key Insight

seqid migration is only beneficial when the **current encoding is longer than 7 bytes**.
This applies to:

- **10-byte zero-padded tick keys** (Task 1, 2): always 10 bytes -> 7 bytes = **-3 bytes/key**
- **Variable-length decimal IDs** with small values: 1-6 bytes -> 7 bytes = **net negative**

The optimization is structurally correct but the absolute savings are modest (~10 bytes per
Mint/Swap operation) because tick keys are a small fraction of total storage cost.
The main value of Task 2 is the **bug fix** (replacing fragile `strconv.Atoi` with proper `DecodeInt`).
