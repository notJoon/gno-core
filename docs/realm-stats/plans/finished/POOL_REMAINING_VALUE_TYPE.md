# Pool 잔여 포인터 필드 Value Type 전환 계획서

**날짜:** 2026-03-16
**브랜치:** `refactor/convert-value-type-pool`
**선행 작업:** Tick(3필드) + Incentives(2필드) + Pool.incentives 전환 완료 (현재 diff)
**범위:** staker 모듈 내 영속 struct에 남은 모든 포인터 필드

---

## 1. 전체 필드 인벤토리

### 이미 전환 완료

| Struct | 필드 | 변환 | 상태 |
|--------|------|------|:----:|
| Tick | `stakedLiquidityGross` | `*u256.Uint` → `u256.Uint` | ✅ |
| Tick | `stakedLiquidityDelta` | `*i256.Int` → `i256.Int` | ✅ |
| Tick | `outsideAccumulation` | `*UintTree` → `UintTree` | ✅ |
| Incentives | `incentives` | `*avl.Tree` → `avl.Tree` | ✅ |
| Incentives | `unclaimablePeriods` | `*UintTree` → `UintTree` | ✅ |
| Pool | `incentives` | `*Incentives` → `Incentives` | ✅ |
| Deposit | `liquidity` | `*u256.Uint` → `u256.Uint` | ✅ (이전 커밋) |
| Deposit | `externalIncentives` | `*avl.Tree` → `avl.Tree` | ✅ (이전 커밋) |

### 전환 대상 (이번 작업)

| # | Struct | 필드 | 현재 타입 | 목표 타입 | Object 절감 |
|---|--------|------|-----------|-----------|:-----------:|
| A1 | Pool | `stakedLiquidity` | `*UintTree` | `UintTree` | 1/pool |
| A2 | Pool | `rewardCache` | `*UintTree` | `UintTree` | 1/pool |
| A3 | Pool | `globalRewardRatioAccumulation` | `*UintTree` | `UintTree` | 1/pool |
| A4 | Pool | `historicalTick` | `*UintTree` | `UintTree` | 1/pool |
| B1 | Ticks | `tree` | `*avl.Tree` | `avl.Tree` | 1/pool |
| C1 | PoolTierState | `Membership` | `*avl.Tree` | `avl.Tree` | 1 (singleton) |

**합계: Object 6개 제거** (pool당 5개 + singleton 1개)

### 전환 불필요 (ephemeral — 영속 state 아님)

| Struct | 필드 | 이유 |
|--------|------|------|
| SwapTickCross | `delta *i256.Int` | 단일 tx 내 batch 처리용, 폐기됨 |
| SwapBatchProcessor | `crosses []*SwapTickCross` | 단일 tx 내 batch 처리용 |
| SwapBatchProcessor | `pool *Pool` | 참조 포인터, Pool 자체는 영속 |
| v1/tickCrossEventInfo | `*u256.Uint`, `*i256.Int` | 이벤트 발행 전용 |
| v1/RewardState | `*PoolResolver`, `*DepositResolver` | 보상 계산 중 임시 상태 |
| v1/PoolTier | `membership *avl.Tree` | PoolTierState에서 로드한 런타임 참조 (store 통해 영속화) |

### 전환 불가 / 부적합

| Struct | 필드 | 이유 |
|--------|------|------|
| PoolTierState | `GetEmission func() int64` | closure — value type 전환 대상 아님 |
| PoolTierState | `GetHalvingBlocksInRange func(...)` | closure — 동일 |
| Counter | `id int64` | primitive, 이미 value |

---

## 2. 전환 안전성 분석

### A1~A4: Pool의 `*UintTree` 4필드

**UintTree 내부:** `struct { tree avl.Tree }` — 포인터 없음, 순수 value type.

**사용 패턴:**

```go
// 읽기 — getter가 *UintTree 반환, caller가 .Set/.Get/.Iterate 호출
pool.StakedLiquidity().Set(timestamp, value)
pool.GlobalRewardRatioAccumulation().ReverseIterate(...)

// 쓰기 — setter는 테스트/초기화에서만 사용
pool.SetStakedLiquidity(newTree)
```

모든 접근이 getter → pointer receiver method 호출 패턴. `return &p.stakedLiquidity`로 변경하면 호출부 변경 없음.

**Clone() 처리:**
현재 `Clone()`에서 4필드 모두 `nil` 할당. → `UintTree{}` (zero value)로 변경.

### B1: Ticks.tree

**현재:** `tree *avl.Tree`
**사용:** `t.tree.Get(...)`, `t.tree.Set(...)`, `t.tree.Iterate(...)` — 모두 pointer receiver.

`tree avl.Tree`로 변경 시:
- `Tree()` getter: `return &t.tree`
- `SetTree()` setter: `t.tree = *tree`
- `NewTicks()`: `tree: *avl.NewTree()`
- `Ticks.Clone()`: 기존 `cloned := avl.NewTree()` → `cloned := *avl.NewTree()`, 반환 시 `Ticks{tree: cloned}`

### C1: PoolTierState.Membership

**현재:** `Membership *avl.Tree` (exported field)
**사용:** `state.Membership.Set(...)`, `state.Membership` 직접 전달

`Membership avl.Tree`로 변경 시:
- 기존: `state.Membership` (returns `*avl.Tree`) → `&state.Membership`
- PoolTier 생성자에서 `state.Membership` 전달 → `&state.Membership`
- `updatePoolTier`에서 `poolTier.membership` 할당 → 역참조

**주의:** Membership은 exported field이므로, v1 패키지에서 직접 참조하는 코드가 있음. 변경 시 v1 쪽도 수정 필요.

---

## 3. 수정 파일 목록

### Phase 1: Pool `*UintTree` 4필드 (A1~A4)

| 파일 | 변경 |
|------|------|
| `staker/pool.gno` | Pool struct: 4개 `*UintTree` → `UintTree` |
| `staker/pool.gno` | 4개 getter: `return p.field` → `return &p.field` |
| `staker/pool.gno` | 4개 setter: `p.field = v` → `p.field = *v` |
| `staker/pool.gno` | `NewPool()`: `NewUintTree()` → `*NewUintTree()` |
| `staker/pool.gno` | `Clone()`: `nil` → `UintTree{}` (4곳) |

### Phase 2: Ticks.tree (B1)

| 파일 | 변경 |
|------|------|
| `staker/pool.gno` | Ticks struct: `tree *avl.Tree` → `tree avl.Tree` |
| `staker/pool.gno` | `Tree()` getter: `return t.tree` → `return &t.tree` |
| `staker/pool.gno` | `SetTree()` setter: `t.tree = tree` → `t.tree = *tree` |
| `staker/pool.gno` | `NewTicks()`: `tree: avl.NewTree()` → `tree: *avl.NewTree()` |
| `staker/pool.gno` | `Ticks.Clone()`: `cloned := avl.NewTree()` 반환값 조정 |

### Phase 3: PoolTierState.Membership (C1)

| 파일 | 변경 |
|------|------|
| `staker/types.gno` | `Membership *avl.Tree` → `Membership avl.Tree` |
| `staker/v1/instance.gno` | `state.Membership` 전달부 → `&state.Membership` |
| `staker/v1/instance.gno` | `updatePoolTier()`: `poolTier.membership` → `*poolTier.membership` |
| `staker/v1/reward_calculation_pool_tier.gno` | PoolTier struct: `membership *avl.Tree` 유지 (런타임 참조) |
| `staker/v1/init.gno` | 초기화: `Membership: avl.NewTree()` → `Membership: *avl.NewTree()` |
| `staker/store_test.gno` | 테스트 초기화 조정 |
| `staker/v1/getter_test.gno` | 테스트 접근 패턴 조정 |

---

## 4. 실행 절차

### Phase 0: Baseline 측정

TICK_INCENTIVES_VALUE_TYPE_PLAN.md의 Phase 0과 동일한 테스트 사용.
**Tick/Incentives 전환이 커밋된 상태에서 baseline을 측정한다.**

```bash
cd tests/integration
export GNO_REALM_STATS_LOG=stderr

go test -v -run "TestTestdata/staker_storage_staker_lifecycle" -timeout 5m -count=1 2>&1 | tee /tmp/pool_baseline_T1.txt
go test -v -run "TestTestdata/staker_storage_staker_stake_only" -timeout 5m -count=1 2>&1 | tee /tmp/pool_baseline_T2.txt
go test -v -run "TestTestdata/staker_storage_staker_stake_with_externals" -timeout 5m -count=1 2>&1 | tee /tmp/pool_baseline_T3.txt
go test -v -run "TestTestdata/staker_staker_create_external_incentive" -timeout 5m -count=1 2>&1 | tee /tmp/pool_baseline_T4.txt
go test -v -run "TestTestdata/staker_collect_reward_immediately_after_stake_token" -timeout 5m -count=1 2>&1 | tee /tmp/pool_baseline_T5.txt
```

### Phase 1: Pool `*UintTree` 4필드 전환

```bash
# 빌드 확인
cd contract/r/gnoswap/staker && gno build ./...
cd contract/r/gnoswap/staker/v1 && gno build ./...

# 단위 테스트
cd contract/r/gnoswap/staker && gno test -v ./...
```

### Phase 2: Ticks.tree 전환

동일한 빌드 + 테스트 확인.

### Phase 3: PoolTierState.Membership 전환

v1 패키지도 수정하므로 양쪽 빌드 + 테스트 필수:

```bash
cd contract/r/gnoswap/staker && gno build ./... && gno test -v ./...
cd contract/r/gnoswap/staker/v1 && gno build ./... && gno test -v ./...
```

### Phase 4: 수정 후 측정

Phase 0과 동일 명령어로 `/tmp/pool_after_T*.txt`에 저장.

### Phase 5: 비교 테이블

```
=== Pool 잔여 필드 Value Type 전환 결과 ===

| Operation                    | Before (bytes) | After (bytes) | Delta  | %     |
|------------------------------|---------------:|:-------------:|-------:|------:|
| T1 SetPoolTier               |                |               |        |       |
| T1 StakeToken                |                |               |        |       |
| T1 CollectReward #1          |                |               |        |       |
| T1 CollectReward #2          |                |               |        |       |
| T1 UnStakeToken              |                |               |        |       |
| T2 StakeToken (단독)          |                |               |        |       |
| T3 CreateExtIncentive ×3     |                |               |        |       |
| T3 StakeToken (w/ externals) |                |               |        |       |
| T4 CreateExtIncentive        |                |               |        |       |
| T5 CollectReward (즉시)       |                |               |        |       |
```

---

## 5. 예상 효과

| 대상 | Object 수 | 적용 단위 |
|------|:---------:|-----------|
| Pool UintTree ×4 | 4 | per pool |
| Ticks.tree | 1 | per pool |
| PoolTierState.Membership | 1 | singleton (전체 1개) |
| **합계** | **5/pool + 1** | |

Pool은 incentivized pool 수만큼 존재. pool 10개 기준 → **Object 51개 제거**.
Object당 ~100-200B 오버헤드 → **약 5,000~10,000 bytes 절감** (pool 10개 기준).

실제 txtar 측정에서 영향이 가장 크게 나타나는 operation:
- **SetPoolTier** — Pool 최초 생성 시 NewPool() 호출, 모든 UintTree 할당
- **CreateExternalIncentive** — Pool에 접근하여 Incentives 업데이트
- **StakeToken** — Pool + Tick 접근

---

## 6. 리스크

| 리스크 | 대응 |
|--------|------|
| Pool.Clone()에서 UintTree zero value가 이후 로직에서 문제 | 기존에도 nil이었으므로 동일. Clone 후 tree에 값 추가하는 패턴 확인 |
| Ticks.Clone()의 `avl.NewTree()` 반환값 역참조 | `*avl.NewTree()` 역참조 후 value로 저장 — avl.NewTree는 empty tree 반환 |
| PoolTierState.Membership가 exported → v1에서 직접 접근 | v1/instance.gno, v1/init.gno, 테스트 파일 수정 필요 |
| PoolTier(v1)의 `membership *avl.Tree`는 그대로 유지 | PoolTier는 런타임 wrapper, PoolTierState에서 `&state.Membership`로 포인터 전달 |

---

## 7. 체크리스트

- [ ] Phase 0: Baseline 측정 (Tick/Incentives 전환 커밋 후)
- [ ] Phase 1: Pool `*UintTree` 4필드 전환 → 빌드 + 테스트
- [ ] Phase 2: Ticks.tree 전환 → 빌드 + 테스트
- [ ] Phase 3: PoolTierState.Membership 전환 → 빌드 + 테스트 (v1 포함)
- [ ] Phase 4: 수정 후 측정
- [ ] Phase 5: 비교 테이블 → STORAGE_AUDIT_REPORT.md 업데이트
- [ ] Commit
