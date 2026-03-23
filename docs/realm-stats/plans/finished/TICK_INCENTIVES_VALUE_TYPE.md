# Tick / Incentives Value Type 전환 계획서

**날짜:** 2026-03-16
**브랜치:** `refactor/convert-value-type-pool` (기존 value type 작업 브랜치 위에 진행)
**범위:** `contract/r/gnoswap/staker/pool.gno` — Tick, Incentives struct의 포인터 필드 value type 전환

---

## 1. 전환 대상

### 1a. Tick struct (Object 3개 제거/tick)

```go
// Before
type Tick struct {
    id                   int32
    stakedLiquidityGross *u256.Uint   // ← pointer
    stakedLiquidityDelta *i256.Int    // ← pointer
    outsideAccumulation  *UintTree    // ← pointer
}

// After
type Tick struct {
    id                   int32
    stakedLiquidityGross u256.Uint    // value (32B inline)
    stakedLiquidityDelta i256.Int     // value (32B inline)
    outsideAccumulation  UintTree     // value (avl.Tree inline)
}
```

### 1b. Incentives struct (Object 2개 제거/pool)

```go
// Before
type Incentives struct {
    incentives         *avl.Tree     // ← pointer
    targetPoolPath     string
    unclaimablePeriods *UintTree     // ← pointer
}

// After
type Incentives struct {
    incentives         avl.Tree      // value
    targetPoolPath     string
    unclaimablePeriods UintTree      // value
}
```

### 1c. Pool.incentives 필드 (Object 1개 제거/pool)

```go
// Before
type Pool struct {
    ...
    incentives *Incentives  // ← pointer
    ...
}

// After
type Pool struct {
    ...
    incentives Incentives   // value (inline)
    ...
}
```

### 전환 안전성 근거

| 필드 | 내부 구조 | mutation 패턴 | 전환 안전 |
|------|-----------|---------------|:---------:|
| `u256.Uint` | `[4]uint64` — 순수 value | replace-via-setter (새 객체 생성 → 교체) | ✅ |
| `i256.Int` | `[4]uint64` — 순수 value | replace-via-setter (새 객체 생성 → 교체) | ✅ |
| `UintTree` | `struct { tree avl.Tree }` — 순수 value | pointer receiver로 접근 | ✅ |
| `avl.Tree` | gno stdlib value type | pointer receiver로 접근 | ✅ |

선례: pool realm의 TickInfo(4필드), PositionInfo(5필드), Observation(2필드) 동일 전환 완료 (audit #9).

---

## 2. Baseline 측정 대상 txtar 테스트

### Primary — Tick 변경 효과 측정

| # | txtar 파일 | 측정 operation | Tick 관여 이유 |
|---|-----------|----------------|---------------|
| T1 | `staker/storage_staker_lifecycle.txtar` | SetPoolTier, StakeToken, CollectReward ×2, UnStakeToken | StakeToken이 lower/upper tick 생성, CollectReward가 tick 읽기 |
| T2 | `staker/storage_staker_stake_only.txtar` | StakeToken (단독) | StakeToken 격리 측정 — tick 생성 비용만 포착 |
| T3 | `staker/storage_staker_stake_with_externals.txtar` | CreateExternalIncentive ×3, StakeToken | Tick + Incentives 모두 관여 |

### Secondary — Incentives 변경 효과 측정

| # | txtar 파일 | 측정 operation | Incentives 관여 이유 |
|---|-----------|----------------|---------------------|
| T4 | `staker/staker_create_external_incentive.txtar` | CreateExternalIncentive | Incentives 객체 생성 — `*avl.Tree`, `*UintTree` 할당 |
| T5 | `staker/collect_reward_immediately_after_stake_token.txtar` | SetPoolTier, StakeToken, CollectReward | CollectReward가 Incentives 순회 |

### Reference — 전체 lifecycle 맥락

| # | txtar 파일 | 비고 |
|---|-----------|------|
| T6 | `position/stake_position.txtar` | CreatePool → Mint → SetPoolTier → StakeToken → CollectReward end-to-end |

---

## 3. 실행 절차

### Phase 0: Baseline 측정 (수정 전)

**현재 브랜치(`refactor/convert-value-type-pool`)에서 실행.**

```bash
cd tests/integration
export GNO_REALM_STATS_LOG=stderr

# Primary 테스트 (필수)
go test -v -run "TestTestdata/staker_storage_staker_lifecycle" -timeout 5m -count=1 2>&1 | tee /tmp/baseline_T1.txt
go test -v -run "TestTestdata/staker_storage_staker_stake_only" -timeout 5m -count=1 2>&1 | tee /tmp/baseline_T2.txt
go test -v -run "TestTestdata/staker_storage_staker_stake_with_externals" -timeout 5m -count=1 2>&1 | tee /tmp/baseline_T3.txt

# Secondary 테스트
go test -v -run "TestTestdata/staker_staker_create_external_incentive" -timeout 5m -count=1 2>&1 | tee /tmp/baseline_T4.txt
go test -v -run "TestTestdata/staker_collect_reward_immediately_after_stake_token" -timeout 5m -count=1 2>&1 | tee /tmp/baseline_T5.txt
```

**추출할 수치** (각 operation별):
- `GAS USED`
- `STORAGE DELTA` (bytes)
- `TOTAL TX COST` (ugnot)

**Baseline 기록 형식:**

```
=== Baseline (before Tick/Incentives value type) ===
Branch: refactor/convert-value-type-pool @ <commit-hash>

T1 storage_staker_lifecycle:
  SetPoolTier:      GAS=____  STORAGE=____ bytes
  StakeToken:       GAS=____  STORAGE=____ bytes
  CollectReward #1: GAS=____  STORAGE=____ bytes
  CollectReward #2: GAS=____  STORAGE=____ bytes
  UnStakeToken:     GAS=____  STORAGE=____ bytes

T2 storage_staker_stake_only:
  StakeToken:       GAS=____  STORAGE=____ bytes

T3 storage_staker_stake_with_externals:
  CreateExternalIncentive ×3: GAS=____  STORAGE=____ bytes
  StakeToken:                 GAS=____  STORAGE=____ bytes

T4 staker_create_external_incentive:
  CreateExternalIncentive:    GAS=____  STORAGE=____ bytes

T5 collect_reward_immediately:
  SetPoolTier:      GAS=____  STORAGE=____ bytes
  StakeToken:       GAS=____  STORAGE=____ bytes
  CollectReward:    GAS=____  STORAGE=____ bytes
```

---

### Phase 1: Tick value type 전환

**수정 파일 목록:**

| 파일 | 변경 내용 |
|------|----------|
| `staker/pool.gno` | Tick struct: `*u256.Uint` → `u256.Uint`, `*i256.Int` → `i256.Int`, `*UintTree` → `UintTree` |
| `staker/pool.gno` | Tick getter: `return t.stakedLiquidityGross` → `return &t.stakedLiquidityGross` |
| `staker/pool.gno` | Tick setter: dereference 할당 `t.stakedLiquidityGross = *v` |
| `staker/pool.gno` | `Ticks.Get()`: 새 Tick 생성 시 `u256.Zero()` → `*u256.Zero()` 역참조 |
| `staker/pool.gno` | `NewTick()`: 동일 |
| `staker/pool.gno` | `Tick.Clone()`: `.Clone()` → value copy (역참조) |
| `staker/v1/reward_calculation_tick.gno` | setter 호출부 — 시그니처 변경에 맞게 조정 |

**변경 패턴 (반복 적용):**

```go
// Getter: 필드 주소 반환 → 기존 호출부 변경 불필요
func (t *Tick) StakedLiquidityGross() *u256.Uint {
    return &t.stakedLiquidityGross  // was: return t.stakedLiquidityGross
}

// Setter: 역참조 할당
func (t *Tick) SetStakedLiquidityGross(v *u256.Uint) {
    t.stakedLiquidityGross = *v  // was: t.stakedLiquidityGross = v
}

// OutsideAccumulation getter
func (t *Tick) OutsideAccumulation() *UintTree {
    return &t.outsideAccumulation  // was: return t.outsideAccumulation
}
```

**빌드 확인:**

```bash
cd contract/r/gnoswap/staker && gno build ./...
cd contract/r/gnoswap/staker/v1 && gno build ./...
```

**단위 테스트:**

```bash
cd contract/r/gnoswap/staker && gno test -v ./...
```

---

### Phase 2: Incentives value type 전환

**수정 파일 목록:**

| 파일 | 변경 내용 |
|------|----------|
| `staker/pool.gno` | Incentives struct: `*avl.Tree` → `avl.Tree`, `*UintTree` → `UintTree` |
| `staker/pool.gno` | Incentives getter: `return i.incentives` → `return &i.incentives` |
| `staker/pool.gno` | `NewIncentives()`: `avl.NewTree()` → `*avl.NewTree()` 역참조 또는 inline 초기화 |
| `staker/pool.gno` | Pool struct: `incentives *Incentives` → `incentives Incentives` |
| `staker/pool.gno` | Pool getter: `return p.incentives` → `return &p.incentives` |
| `staker/pool.gno` | `NewPool()`: `NewIncentives()` 반환 타입 맞춤 |
| `staker/pool.gno` | `Pool.Clone()`: incentives deep copy 조정 |
| `staker/v1/reward_calculation_incentives.gno` | 호출부 조정 (있다면) |

**빌드 + 단위 테스트:**

```bash
cd contract/r/gnoswap/staker && gno build ./... && gno test -v ./...
cd contract/r/gnoswap/staker/v1 && gno build ./... && gno test -v ./...
```

---

### Phase 3: 수정 후 측정

**Phase 0과 동일한 명령어로 측정.**

```bash
cd tests/integration
export GNO_REALM_STATS_LOG=stderr

go test -v -run "TestTestdata/staker_storage_staker_lifecycle" -timeout 5m -count=1 2>&1 | tee /tmp/after_T1.txt
go test -v -run "TestTestdata/staker_storage_staker_stake_only" -timeout 5m -count=1 2>&1 | tee /tmp/after_T2.txt
go test -v -run "TestTestdata/staker_storage_staker_stake_with_externals" -timeout 5m -count=1 2>&1 | tee /tmp/after_T3.txt
go test -v -run "TestTestdata/staker_staker_create_external_incentive" -timeout 5m -count=1 2>&1 | tee /tmp/after_T4.txt
go test -v -run "TestTestdata/staker_collect_reward_immediately_after_stake_token" -timeout 5m -count=1 2>&1 | tee /tmp/after_T5.txt
```

---

### Phase 4: 비교 테이블 작성

```
=== Tick/Incentives Value Type 전환 결과 ===
Branch: refactor/convert-value-type-pool @ <after-commit-hash>

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

## 4. 예상 절감 효과

| 전환 대상 | 제거 Object 수 | 적용 단위 |
|-----------|:---:|------|
| Tick.stakedLiquidityGross | 1 | per tick |
| Tick.stakedLiquidityDelta | 1 | per tick |
| Tick.outsideAccumulation | 1 | per tick |
| Incentives.incentives | 1 | per pool |
| Incentives.unclaimablePeriods | 1 | per pool |
| Pool.incentives (자체) | 1 | per pool |
| **합계** | **3N + 3** | N = 활성 tick 수 |

StakeToken 1회 기준 tick 2개(lower, upper) 생성 → **최소 6 Objects 제거**.
Object 1개 ≈ 100~200 bytes 오버헤드 → **StakeToken에서 약 600~1,200 bytes 절감 예상**.

---

## 5. 리스크 및 주의사항

| 리스크 | 대응 |
|--------|------|
| Getter가 `&field` 반환 시 caller가 pointer를 캐시하고 field이 덮어써지면 stale 참조 | 현재 코드에서 pointer 캐시 없음 — always "get → compute → set" 패턴. 전환 전 grep으로 재확인 |
| `Ticks.Get()` 내부에서 새 Tick 생성 후 tree에 저장 — value type이면 복사본 저장됨 | Tick을 tree에 `*Tick`으로 저장하는 기존 패턴 유지 (Tick 내부 필드만 value 전환) |
| `Clone()` 메서드의 deep copy 로직 변경 필요 | Phase 1, 2에서 각각 수정 |
| Incentives를 value embed하면 `Pool.Clone()`에서 Incentives도 deep copy 필요 | 현재 `Clone()`이 incentives를 nil로 설정 — 동일 동작 유지 가능 |

---

## 6. 작업 순서 체크리스트

- [ ] Phase 0: Baseline 측정 (T1~T5) → `/tmp/baseline_T*.txt`
- [ ] Phase 1: Tick value type 전환 → 빌드 + 단위 테스트 통과
- [ ] Phase 2: Incentives value type 전환 → 빌드 + 단위 테스트 통과
- [ ] Phase 3: 수정 후 측정 (T1~T5) → `/tmp/after_T*.txt`
- [ ] Phase 4: 비교 테이블 작성 → STORAGE_AUDIT_REPORT.md 업데이트
- [ ] Commit
