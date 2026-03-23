# GnoSwap Storage Pattern Audit Report

**Date:** 2026-03-09 (updated 2026-03-17)
**Scope:** `contract/r/gnoswap/*` (all realm packages)
**Status:** Storage deposit 최적화 항목 전체 완료

---

## Executive Summary

GnoSwap 컨트랙트 코드베이스의 storage 패턴 전수 조사 결과. Storage deposit 절감 목적의 High 우선순위 항목은 전부 해결 또는 검토 완료되었다.

> **Gno semantics:** (1) 패키지 레벨 `map` 변수는 트랜잭션 간 **영속된다** — VM의 Object 타입. (2) Gno 트랜잭션은 **완전 원자적** — panic 시 모든 상태 변경이 롤백된다.

---

## 해결 완료

### ~~#5 seqid 미사용 (tick key)~~

**상태: 해결됨** — seqid migration (Task 1, 2) 완료.

- `pool/utils.gno`: `EncodeTickKey` / `DecodeTickKey` → seqid (cford32) 인코딩 (10B → 7B)
- `staker/tree.gno`: `EncodeInt` / `DecodeInt` → seqid 인코딩 (10~11B → 7B)
- `staker/pool.gno`: `IterateTicks`의 `strconv.Atoi` → `DecodeInt` 버그 수정

> 순차 증가 ID (position ID, deposit ID 등)에 대한 seqid 전환은 소규모 ID에서 오히려 바이트 증가 (1B → 7B)가 확인되어 **스킵**으로 결정. 상세: `SEQID_MIGRATION_PLAN.md`.

### ~~#9 `*u256.Uint` 포인터 필드~~

**상태: 해결됨** — 높은 우선순위 대상 모두 완료.

- P1: TickInfo(4필드), PositionInfo(5필드), Pool 본체(feeGrowthGlobal, liquidity 등)
- P2: Position (position realm, 7필드) — `e752e9f8`
- P2: Observation (pool/oracle, 2필드) — `593270ed`
- Staker Deposit liquidity — #13에서 해결

잔여 (낮은 우선순위): `VALUE_TYPE_EXPANSION.md` 참조
- Gov/Staker, Launchpad 소규모 struct (각 1~2필드)

### ~~#11a `updatePoolTier()` 8회 분리 저장~~

**상태: 해결됨** — `PoolTier` 상태를 단일 store key로 통합. (`835eb1b0`)

### ~~#14c Tick key zero-padding~~

**상태: 해결됨** — #5와 동일. seqid 전환으로 10B → 7B.

### ~~#1 Pointer slice in persistent state~~

**상태: 해결됨** — `[]*DelegationWithdraw` → `[]DelegationWithdraw` value slice 전환.

실측 결과 (Undelegate): **5,166 → 915 bytes (-82.3% storage)**, gas -83,371.
상세: `DELEGATION_WITHDRAW_VALUE_SLICE_PLAN.md`.

### ~~#12 Deep Object nesting (UintTree wrapper)~~

**상태: 해결됨** — UintTree 내부 `*avl.Tree` → `avl.Tree` value type 전환. (`b6ddf4bf`)

wrapper를 유지하면서 내부 pointer만 value로 변경하여 **캡슐화 보존 + Object 제거** 달성.
- staker: SetPoolTier **-1,965 bytes (-8.1%)**, CreateExternalIncentive **-1,965 bytes (-7.4%)**
- gov/staker: Delegate **-419 bytes (-2.6%)** (value type + seqid 합산)

상세: `UINT_TREE_REMOVAL_PLAN.md`, `GOV_STAKER_UINT_TREE_VALUE_TYPE_PLAN.md`.

### ~~#11b Redundant writes — decreaseLiquidity() position 이중 저장~~

**상태: 해결됨** — `burn.gno` line 88의 1차 `mustUpdatePosition()` 제거.

Storage delta 변화 없음 (Gno dirty tracking이 동일 키 중복 write를 dedup).
Gas **-135,187** 절감 (불필요한 직렬화 + hash 연산 제거).
상세: `DECREASE_LIQUIDITY_DOUBLE_SAVE_PLAN.md`.

### ~~#13 Deposit — 3개 avl.Tree를 1개로 통합~~

**상태: 해결됨** — 3개 `*avl.Tree` → 1개 `avl.Tree` + `ExternalIncentiveState` value type 통합.

2단계로 진행:
- Phase 1: 3 tree → 1 tree 통합 + `RemoveExternalIncentiveId` 호출 제거
- Phase 2: `*ExternalIncentiveState` → value type, `*avl.Tree` → `avl.Tree`, `*u256.Uint` → `u256.Uint`

실측 결과 (Baseline → 최종, 3 externals lifecycle):

| Operation | Before | After | Delta |
|---|---:|---:|---:|
| CreateExternalIncentive ×3 | 53,132 bytes | 31,513 bytes | **-40.7%** |
| StakeToken | 35,301 bytes | 31,215 bytes | **-11.6%** |
| CollectReward (1st) | 5,706 bytes | 5,706 bytes | 0% |
| **Lifecycle 합산** | **94,139 bytes** | **68,434 bytes** | **-27.3%** |

GAS: CollectReward **-16.0%** (Phase 1 기준, 가장 빈번한 operation).

상세: `DEPOSIT_TRIPLE_AVL_MERGE_PLAN.md`, `DEPOSIT_VALUE_TYPE_PLAN.md`, `deposit_triple_avl_baseline_measure.md`.

---

## Skip / Drop

### ~~#2 Large structs with mixed-frequency fields~~

**상태: Skip**

- **staker/Deposit struct 분리:** #13 통합 후 PoC에서 struct 분리의 실질 storage 절감 효과가 없는 것으로 확인됨.
- **기타 후보** (`staker/Pool`, `RewardManager`, `Position`, `ProjectTier`): Deposit과 동일한 이유로 효과 미미할 것으로 예상.

### ~~#10 Missing secondary indexes causing O(n) scans~~

**상태: Skip** — secondary index 추가는 storage를 증가시키며 (새 index tree 생성), 절감되는 것은 gas(읽기 비용)뿐. 현재 storage deposit 절감 목적과 맞지 않음.

- 10a. External incentive refund — 전체 deposit scan
- 10b. GetExternalIncentiveByPoolPath — 전체 incentive scan
- 10c. GetPoolsByTier — 전체 pool-tier scan

### ~~#10d Fee protocol update — 모든 pool 재기록~~

**상태: Skip** — O(n) write는 gas 비용만 발생하며 storage deposit에는 영향 없음 (동일 크기 재기록). SetFeeProtocol은 governance 레벨 호출 (수개월 1회)로 빈도 극히 낮음. 또한 Uniswap 원본이 per-pool 설계이므로 global 변수화 시 향후 per-pool fee 지원 가능성이 차단됨.

### ~~#15 Swap hot-loop `u256.Clone()` allocations~~

**상태: Drop** — gas(힙 할당) 절감만 해당하며 storage 비용에는 영향 없음. 변경 범위 대비 효과 미미.

---

## 미해결 — P4 (추가 storage deposit 절감)

P1~P3에서 검증된 패턴을 미전환 필드에 확장 적용. 상세 계획서 별도.

### ~~P4-1: Pool sub-struct `*u256.Uint` → value type~~

**상태: 해결됨** — `P4_pool_substruct_value_type.md`

Slot0.sqrtPriceX96, TokenPair.token0/token1 (Balances, ProtocolFees) — 5개 `*u256.Uint` → `u256.Uint` value type 전환.

실측 결과 (position lifecycle, Baseline → P4-1 적용 후):

| Operation | Before | After | Delta |
|---|---:|---:|---:|
| CreatePool | 20,746 bytes | 16,357 bytes | **-21.2%** |
| Mint #1 (wide) | 40,763 bytes | 32,009 bytes | **-21.5%** |
| Mint #2 (narrow) | 39,501 bytes | 30,717 bytes | **-22.2%** |
| Mint #3 (reuse ticks) | 13,105 bytes | 10,290 bytes | **-21.5%** |
| Swap | 12 bytes | 6 bytes | -50.0% |
| CollectFee #1 | 2,259 bytes | 2,216 bytes | -1.9% |

> Note: 위 수치는 P1~P3 최적화와 합산된 결과. P4-1 단독 기여분은 계획서 예상(~500 bytes/op)과 일치.

### P4-2: Pool `*avl.Tree` × 3 + `*ObservationState` → value type

**상태: 계획 완료** — `P4_pool_tree_value_type.md`

ticks, tickBitmaps, positions, observationState — 4개 포인터.
예상: 모든 Pool write에서 **~400 bytes 추가 절감**.
P4-1 합산 시 Swap (1st) **-18%** (5,021 → ~4,100).

### ~~P4-3: Staker Pool `*UintTree` × 4 + `Ticks.tree` → value type~~

**상태: 해결됨** — `finished/P4_staker_pool_uinttree_value_type.md`

Pool struct 4× `*UintTree` → `UintTree`, Ticks.tree `*avl.Tree` → `avl.Tree` 전환 완료.

실측 결과 (`P4_staker_pool_uinttree_comparison.md`):

| 시나리오 | Net Storage 변화 | 비고 |
|----------|-----------------|------|
| stake_only (SetPoolTier + StakeToken) | **-2,001 bytes** | SetPoolTier -9.5% |
| lifecycle (Stake + Collect + UnStake) | 0 bytes | mutation delta 무변 |
| externals (SetPoolTier + 3 Incentives + StakeToken) | **-2,386 bytes** (staker) | 비용 재배분 포함 |

Pool 생성(SetPoolTier) 시 5개 Object 헤더 제거로 **-2,001 bytes 일회성 절감**. 반복 operation에서는 중립.

### P4-4: ExternalIncentive 포인터 저장 → value 저장

**상태: 계획 완료** — `P4_external_incentive_value_storage.md`

avl.Tree에 `*ExternalIncentive` → `ExternalIncentive` value 저장. incentive당 ~100 bytes.
변경 파일 다수 (type assertion 7곳).

### ~~P4-5: ObservationState `map[uint16]*Observation` → value 저장~~

**상태: 해결됨** — `finished/P4_observation_map_value_type.md`

`map[uint16]*Observation` → `map[uint16]Observation` 전환 완료. cardinality × ~100 bytes 절감.

---

## 미해결 — Low (코드 품질/구조 개선, storage deposit 영향 없음)

### #14a,b Composite string key 비용

#### 14a. Pool path key — ~54 bytes

```go
// "gno.land/r/gnoland/wugnot:gno.land/r/gnoswap/gns:3000" (~54 bytes)
```

모든 Position의 `poolKey`에 저장되고 다수 tree의 lookup key로 사용됨.

#### 14b. Incentive ID — ~58+ bytes

```go
// "g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5:1709913600:0" (~58 bytes)
```

### #4 Multiple avl.Trees indexing same data

Staker(7 trees), Protocol fee(4 trees), Launchpad(3 trees)가 동일 데이터의 다중 인덱스를 유지. 원자성 문제는 없으나 (Gno는 완전 원자적), 개발자가 관련 tree를 함께 업데이트하는 것을 잊으면 로직 버그 발생 가능.

**권장:** 관련 tree들을 struct로 묶고 compound-update 메서드 제공.

### #6 Read-only maps as package-level variables

`halt/types.gno`, `pool/v1/init.gno`, `rbac/role.gno`, `launchpad/v1/consts.gno` 등.

초기화 후 변경되지 않는 map들. 항목 수 3~6개로 gas 영향 미미. Switch 문이나 helper 함수로 전환 가능.

### #7 Repeated inline type assertions

`referral/keeper.gno`, `position/store.gno`, `staker/v1/getter.gno` 등에서 `avl.Tree.Get()` 결과를 인라인으로 반복 type assert. Typed getter helper로 정리 가능.

### #16 Full-tree cloning via iteration

**파일:** `staker/pool.gno:590`, `staker/tree.gno:80`

Tree 전체를 iterate하여 새 tree로 복사 — O(n) reads + O(n) writes. Clone이 실제로 필요한지 검토하고, read-only 접근으로 대체 가능한지 확인.
