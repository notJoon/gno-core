# Baseline: Tick/Incentives Value Type 전환 전 측정

**Branch:** `perf/object-dirty-log` @ `668f4174a`
**Date:** 2026-03-16
**Environment:** `GNO_REALM_STATS_LOG=stderr`

---

## T1: storage_staker_lifecycle

| Operation        | GAS USED   | STORAGE DELTA | TOTAL TX COST    |
|------------------|------------|---------------|------------------|
| SetPoolTier      | 38,268,045 | +22,384 bytes | 12,238,400 ugnot |
| StakeToken       | 50,781,887 | +20,199 bytes | 12,019,900 ugnot |
| CollectReward #1 | 31,290,790 | 0 bytes       | 10,000,000 ugnot |
| CollectReward #2 | 37,272,563 | +5,706 bytes  | 10,570,600 ugnot |
| UnStakeToken     | 51,223,240 | -917 bytes    | 9,908,300 ugnot  |

### T1 Per-realm Storage Breakdown (StakeToken)

| Realm | STORAGE DELTA |
|-------|---------------|
| gno.land/r/gnoswap/gnft | +973 bytes |
| gno.land/r/gnoswap/position | +79 bytes |
| gno.land/r/gnoswap/staker | +19,147 bytes |

### T1 Per-realm Storage Breakdown (UnStakeToken)

| Realm | STORAGE DELTA |
|-------|---------------|
| gno.land/r/gnoswap/gnft | +22 bytes |
| gno.land/r/gnoswap/position | -44 bytes |
| gno.land/r/gnoswap/staker | -895 bytes |

---

## T2: storage_staker_stake_only

| Operation  | GAS USED   | STORAGE DELTA | TOTAL TX COST    |
|------------|------------|---------------|------------------|
| StakeToken | 50,781,917 | +20,199 bytes | 12,019,900 ugnot |

### T2 Per-realm Storage Breakdown (StakeToken)

| Realm | STORAGE DELTA |
|-------|---------------|
| gno.land/r/gnoswap/gnft | +973 bytes |
| gno.land/r/gnoswap/position | +79 bytes |
| gno.land/r/gnoswap/staker | +19,147 bytes |

---

## T3: storage_staker_stake_with_externals

| Operation                  | GAS USED   | STORAGE DELTA | TOTAL TX COST     |
|----------------------------|------------|---------------|-------------------|
| CreateExternalIncentive #1 (BAR)    | 42,504,684 | +11,236 bytes | 101,123,600 ugnot |
| CreateExternalIncentive #2 (FOO)    | 42,624,185 | +12,196 bytes | 101,219,600 ugnot |
| CreateExternalIncentive #3 (WUGNOT) | 42,722,665 | +8,071 bytes  | 100,807,100 ugnot |
| StakeToken (w/ 3 externals)         | 54,878,325 | +36,167 bytes | 13,616,700 ugnot  |

### T3 Per-realm Storage Breakdown (StakeToken w/ externals)

| Realm | STORAGE DELTA |
|-------|---------------|
| gno.land/r/gnoswap/gnft | +973 bytes |
| gno.land/r/gnoswap/position | +79 bytes |
| gno.land/r/gnoswap/staker | +35,115 bytes |

### T3 Per-realm Storage Breakdown (CreateExternalIncentive #1, BAR)

| Realm | STORAGE DELTA |
|-------|---------------|
| gno.land/r/gnoswap/gns | +2,051 bytes |
| gno.land/r/gnoswap/staker | +7,151 bytes |
| gno.land/r/onbloc/bar | +2,034 bytes |

---

## T4: staker_create_external_incentive

| Operation               | GAS USED   | STORAGE DELTA | TOTAL TX COST     |
|-------------------------|------------|---------------|-------------------|
| CreateExternalIncentive | 42,749,733 | +28,742 bytes | 102,874,200 ugnot |

### T4 Per-realm Storage Breakdown

| Realm | STORAGE DELTA |
|-------|---------------|
| gno.land/r/gnoland/wugnot | +2,029 bytes |
| gno.land/r/gnoswap/gns | +2,051 bytes |
| gno.land/r/gnoswap/staker | +24,662 bytes |

Note: T4 uses a different pool (BAR:FOO:100) and this is the first external incentive,
so it includes first-time Pool/Incentives object creation overhead (24,662 vs 7,151 in T3).

---

## T5: collect_reward_immediately_after_stake_token

| Operation      | GAS USED   | STORAGE DELTA | TOTAL TX COST    |
|----------------|------------|---------------|------------------|
| SetPoolTier    | 38,268,045 | +22,384 bytes | 12,238,400 ugnot |
| StakeToken     | 50,781,917 | +20,199 bytes | 12,019,900 ugnot |
| CollectReward  | 31,290,826 | 0 bytes       | 10,000,000 ugnot |

---

## Summary: Key Numbers for Comparison

| Operation (Test) | STORAGE DELTA | Staker-only DELTA |
|------------------|---------------|-------------------|
| T1 SetPoolTier | +22,384 | +22,384 |
| T1 StakeToken | +20,199 | +19,147 |
| T1 CollectReward #1 (immediate) | 0 | 0 |
| T1 CollectReward #2 (1 block later) | +5,706 | +5,706 |
| T1 UnStakeToken | -917 | -895 |
| T2 StakeToken (isolated) | +20,199 | +19,147 |
| T3 CreateExtIncentive #1 | +11,236 | +7,151 |
| T3 CreateExtIncentive #2 | +12,196 | +10,162 |
| T3 CreateExtIncentive #3 | +8,071 | +6,042 |
| T3 StakeToken (w/ 3 externals) | +36,167 | +35,115 |
| T4 CreateExtIncentive (first for pool) | +28,742 | +24,662 |
| T5 CollectReward (immediate) | 0 | 0 |
