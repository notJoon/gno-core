# P2 작업 계획: Deposit Accessor 최적화 + StakeToken 잔여 분석

> 작성일: 2026-03-11
> 선행 조건: P1 (Store Access Caching + Key Consolidation) 완료
> 대상: `r/gnoswap/staker/v1`
> 범위 제외: VM 레벨 수정 (dirty mark skip 등)

---

## 1. 개요

P1 완료 후 남은 finalize 트리거(CollectReward 65회, UnStakeToken 142회, StakeToken 122회)의
잔여 원인을 분석하고, 컨트랙트 레벨에서 추가 최적화 가능한 항목을 설계한다.

### 잔여 Finalize 원인 분류

| 원인 카테고리 | CollectReward (65) | UnStakeToken (142) | StakeToken (122) |
|:---|:---:|:---:|:---:|
| **Deposit accessor (tainted object 필드 접근)** | ~25-30 | ~35-40 | ~0 |
| **Pool accessor (tainted Pool 필드 접근)** | ~15-20 | ~25-30 | ~35-40 |
| **Store batch load** | ~3 | ~5 | ~6 |
| **External realm 호출 (halt, gns, emission, common, position, gnft)** | ~5 | ~20-25 | ~27 |
| **AVL Tree 조작 (tainted tree에서의 Get/Set/Iterate)** | ~5-8 | ~10-15 | ~15-20 |
| **기타 (explicit_cross 포함)** | ~2-5 | ~4-5 | ~5 |

**핵심 발견**: Deposit accessor 호출이 CollectReward/UnStakeToken의 잔여 finalize 중
38~28%를 차지한다. StakeToken은 새 deposit을 생성하므로 tainted 접근이 발생하지 않는다.

---

## 2. Item 1: Deposit Accessor 최적화 (DepositView 패턴)

### 2.1 문제 분석

`deposits.get(positionId)`가 반환하는 `*sr.Deposit`는 proxy realm 소유의 tainted 객체이다.
이 객체의 각 accessor 메서드 호출(`Owner()`, `TargetPoolPath()`, `TickLower()` 등)은
cross-realm 접근으로 FinalizeRealmTransaction을 트리거한다.

```
v1 realm에서:
  deposit.Owner()          → implicit_cross finalize
  deposit.TargetPoolPath() → implicit_cross finalize
  deposit.TickLower()      → implicit_cross finalize
  ...
```

현재 `DepositResolver`는 `*sr.Deposit`를 embed하지만 taint를 해소하지 않는다:

```go
type DepositResolver struct {
    *sr.Deposit  // 여전히 tainted — 모든 메서드 호출이 finalize 유발
}
```

### 2.2 Deposit Accessor 호출 추적 (CollectReward 경로)

CollectReward의 전체 실행 경로에서 `*sr.Deposit` accessor 호출을 추적한다:

#### `calculatePositionReward` → `rewardPerWarmup` → `CalculateRewardForPosition` 경로

| 호출 위치 | Accessor | 호출 횟수 | 비고 |
|:---|:---|:---:|:---|
| `calculatePositionReward:90` | `deposits.get(positionId)` | 1 | tainted tree에서 2번째 조회 |
| `calculatePositionReward:92` | `deposit.TargetPoolPath()` | 1 | pool 조회용 |
| `calculatePositionReward:101` | `depositResolver.InternalRewardLastCollectTime()` | 1-2 | StakeTime() fallback 포함 |
| `RewardStateOf:236` | `deposit.Warmups()` | 1 | RewardState 초기화 |
| `calculatePositionReward:112` | `deposit.Warmups()` | 1 | warmupLen 계산 |
| `calculatePositionReward:124` | `depositResolver.LastExternalIncentiveUpdatedAt()` | 1 | |
| `calculatePositionReward:142` | `deposit.IterateExternalIncentiveIds(fn)` | 1+ | tainted tree iterate |
| **`rewardPerWarmup` (per call):** | | | **warmup당 반복** |
| `rewardPerWarmup:396` | `self.deposit.Warmups()` | N | 매 호출마다 |
| `CalculateRawRewardForPosition:544` | `deposit.TickLower()` | 2N | 2×Raw (start+end) |
| `CalculateRawRewardForPosition:545` | `deposit.TickUpper()` | 2N | 2×Raw (start+end) |
| `CalculateRawRewardForPosition:546,548` | `deposit.TickLower()`, `TickUpper()` | 2-4N | 비교 연산 |
| `rewardPerWarmup:411,426` | `self.deposit.Liquidity()` | N | 보상 계산 |
| `applyWarmup:378` | `self.deposit.Warmups()` | 1 | 최종 적용 |

> **N** = rewardPerWarmup 호출 횟수 (rewardCache 구간 수 × warmup 수).
> 4개 warmup, 1개 cache 구간 기준: N ≈ 4.
> `CalculateRawRewardForPosition`은 `CalculateRewardForPosition` 내에서 2번 호출됨.

**`calculatePositionReward` 내 deposit accessor 추정**: ~20-25회

#### `collectRewardInternal` 직접 호출

| 호출 위치 | Accessor | 호출 횟수 | 비고 |
|:---|:---|:---:|:---|
| `:514` | `deposits.get(positionId)` | 1 | 첫 번째 tree 조회 |
| `:607,637,707` (외부 보상 루프 내) | `deposit.Owner()` | 0-3 | external incentive 수에 비례 |
| `:622,637,665` | `deposit.TargetPoolPath()` | 0-3 | " |
| `:627-628` | `deposit.TickLower()`, `TickUpper()` | 0-2 | " |
| `:688,721,738` | `depositResolver.TargetPoolPath()` | 3 | 루프 밖 |
| `:707` | `deposit.Owner()` | 1 | gns.Transfer |
| `:728-729` | `deposit.TickLower()`, `TickUpper()` | 2 | accumulator |
| `:739` | `depositResolver.Owner()` | 1 | 이벤트 |

**`collectRewardInternal` 내 deposit accessor 추정**: 8-15회 (external incentive 수에 따라)

#### **CollectReward 총 deposit accessor 호출: ~28-40회**

외부 인센티브 0개 기준으로도 ~28회, 1개 이상이면 ~35-40회에 달한다.

### 2.3 UnStakeToken 추가 분석

UnStakeToken은 `collectRewardInternal`을 호출한 후 `applyUnStakeWith`를 추가로 호출한다:

| 호출 위치 | Accessor | 호출 횟수 |
|:---|:---|:---:|
| `applyUnStakeWith:852` | `deposits.get(positionId)` | 1 |
| `applyUnStakeWith:854,859` | `depositResolver.TargetPoolPath()` | 2 |
| `applyUnStakeWith:863` | `depositResolver.TargetPoolPath()` | 1 |
| `applyUnStakeWith:864` | `depositResolver.Liquidity()` | 1 |
| `applyUnStakeWith:869` | `depositResolver.TickUpper()` | 1 |
| `applyUnStakeWith:870` | `depositResolver.TickLower()` | 1 |
| `applyUnStakeWith:875` | `deposit.Owner()` | 1 |
| `UnStakeToken:793` | `deposit.TargetPoolPath()` | 1 |
| `UnStakeToken:813,841` | `deposit.Owner()` | 2 |

**`applyUnStakeWith` + `UnStakeToken` 추가**: ~11회

**UnStakeToken 총 deposit accessor 호출**: collectRewardInternal(~28-40) + 추가(~11) = **~39-51회**

### 2.4 Solution: DepositView 패턴

#### 개념

tainted `*sr.Deposit`에서 스칼라 필드를 1회 복사하여 로컬 struct에 캐싱한다.
이후 모든 읽기는 로컬 필드 접근(0 finalize), 쓰기는 원본 tainted 객체에 write-through.

```
기존:  deposit.Owner()  → finalize (매번)
       deposit.Owner()  → finalize (매번)
       deposit.Owner()  → finalize (매번)

개선:  NewDepositView(deposit)  → finalize ×10 (1회)
       view.Owner               → 0 (로컬 필드)
       view.Owner               → 0 (로컬 필드)
       view.Owner               → 0 (로컬 필드)
```

#### 설계

```go
// v1/deposit_view.gno (신규)

type DepositView struct {
    // 로컬 복사 — 읽기 시 finalize 없음
    owner                         address
    targetPoolPath                string
    liquidity                     *u256.Uint
    stakeTime                     int64
    tickLower                     int32
    tickUpper                     int32
    warmups                       []sr.Warmup
    internalRewardLastCollectTime int64
    collectedInternalReward       int64
    lastExternalIncentiveUpdatedAt int64

    // 원본 tainted 객체 참조 — tree 연산 및 write-back용
    Deposit *sr.Deposit
}

func NewDepositView(d *sr.Deposit) *DepositView {
    // 11회 finalize (각 getter 1회 + Liquidity Clone 1회)
    warmups := d.Warmups()
    localWarmups := make([]sr.Warmup, len(warmups))
    copy(localWarmups, warmups)

    return &DepositView{
        owner:                         d.Owner(),
        targetPoolPath:                d.TargetPoolPath(),
        liquidity:                     d.Liquidity().Clone(),  // 방어적 복사 — in-place mutation 방지
        stakeTime:                     d.StakeTime(),
        tickLower:                     d.TickLower(),
        tickUpper:                     d.TickUpper(),
        warmups:                       localWarmups,
        internalRewardLastCollectTime: d.InternalRewardLastCollectTime(),
        collectedInternalReward:       d.CollectedInternalReward(),
        lastExternalIncentiveUpdatedAt: d.LastExternalIncentiveUpdatedAt(),
        Deposit:                       d,
    }
}

// 읽기 — 로컬 필드 접근 (0 finalize)
func (v *DepositView) Owner() address            { return v.owner }
func (v *DepositView) TargetPoolPath() string     { return v.targetPoolPath }
func (v *DepositView) Liquidity() *u256.Uint      { return v.liquidity }
func (v *DepositView) StakeTime() int64           { return v.stakeTime }
func (v *DepositView) TickLower() int32           { return v.tickLower }
func (v *DepositView) TickUpper() int32           { return v.tickUpper }
func (v *DepositView) Warmups() []sr.Warmup       { return v.warmups }
func (v *DepositView) InternalRewardLastCollectTime() int64 {
    return v.internalRewardLastCollectTime
}
func (v *DepositView) CollectedInternalReward() int64 {
    return v.collectedInternalReward
}
func (v *DepositView) LastExternalIncentiveUpdatedAt() int64 {
    return v.lastExternalIncentiveUpdatedAt
}

// 쓰기 — 로컬 캐시 + 원본 write-through (1 finalize per write)
func (v *DepositView) SetInternalRewardLastCollectTime(t int64) {
    v.internalRewardLastCollectTime = t
    v.Deposit.SetInternalRewardLastCollectTime(t)   // write-through
}

func (v *DepositView) SetCollectedInternalReward(r int64) {
    v.collectedInternalReward = r
    v.Deposit.SetCollectedInternalReward(r)   // write-through
}

func (v *DepositView) SetLastExternalIncentiveUpdatedAt(t int64) {
    v.lastExternalIncentiveUpdatedAt = t
    v.Deposit.SetLastExternalIncentiveUpdatedAt(t)   // write-through
}

// Tree 연산은 tainted 객체에 직접 위임 (finalize 발생하지만 불가피)
func (v *DepositView) IterateExternalIncentiveIds(fn func(string) bool) {
    v.Deposit.IterateExternalIncentiveIds(fn)
}
func (v *DepositView) AddExternalIncentiveId(id string) {
    v.Deposit.AddExternalIncentiveId(id)
}
func (v *DepositView) RemoveExternalIncentiveId(id string) {
    v.Deposit.RemoveExternalIncentiveId(id)
}
func (v *DepositView) GetExternalRewardLastCollectTime(id string) (int64, bool) {
    return v.Deposit.GetExternalRewardLastCollectTime(id)
}
func (v *DepositView) SetExternalRewardLastCollectTime(id string, t int64) {
    v.Deposit.SetExternalRewardLastCollectTime(id, t)
}
func (v *DepositView) SetCollectedExternalReward(id string, r int64) {
    v.Deposit.SetCollectedExternalReward(id, r)
}
func (v *DepositView) GetCollectedExternalReward(id string) (int64, bool) {
    return v.Deposit.GetCollectedExternalReward(id)
}
func (v *DepositView) ExternalRewardLastCollectTimes() *avl.Tree {
    return v.Deposit.ExternalRewardLastCollectTimes()
}
```

#### DepositResolver 변경

```go
// 기존
type DepositResolver struct {
    *sr.Deposit
}

// 변경
type DepositResolver struct {
    *DepositView
}

func NewDepositResolver(deposit *sr.Deposit) *DepositResolver {
    return &DepositResolver{
        DepositView: NewDepositView(deposit),
    }
}
```

#### CalculateRawRewardForPosition 시그니처 변경

현재 `*sr.Deposit`를 받아 `TickLower()`, `TickUpper()`를 반복 호출한다.
DepositView 또는 tickLower/tickUpper 파라미터로 변경하여 반복 tainted 접근을 제거한다.

**옵션 A: DepositView 전달** (권장)

```go
// 기존
func (self *PoolResolver) CalculateRawRewardForPosition(
    currentTime int64, currentTick int32, deposit *sr.Deposit,
) *u256.Uint

// 변경
func (self *PoolResolver) CalculateRawRewardForPosition(
    currentTime int64, currentTick int32, view *DepositView,
) *u256.Uint {
    // view.TickLower(), view.TickUpper() → 로컬 필드 접근, 0 finalize
}
```

**옵션 B: 스칼라 파라미터 전달**

```go
func (self *PoolResolver) CalculateRawRewardForPosition(
    currentTime int64, currentTick int32,
    tickLower, tickUpper int32,
) *u256.Uint
```

옵션 B는 호출 체인 변경이 적으나, `CalculateRewardForPosition`도 연쇄 변경 필요.
옵션 A가 일관성 측면에서 더 적합하다.

### 2.5 영향도 추정

#### Finalize 트리거 변화

| Operation | 현재 | DepositView 후 | 감소 | 감소율 |
|:---|:---:|:---:|:---:|:---:|
| **CollectReward** | 65 | ~40-45 | ~20-25 | **31-38%** |
| **UnStakeToken** | 142 | ~105-115 | ~27-37 | **19-26%** |
| **StakeToken** | 122 | 122 | 0 | 0% |

> CollectReward: 28-40회 tainted accessor → 11회(snapshot, Liquidity Clone 포함) + ~3회(tree ops, write-back)
> 절감: ~14-26회. 총 65 - (14-26) ≈ 39-51 → 보수적으로 40-45 추정.

#### Baseline 대비 누적 감소율

| Operation | Baseline | P1 후 | P2 후 (추정) | 전체 감소율 |
|:---|:---:|:---:|:---:|:---:|
| **CollectReward** | 332 | 65 | ~40-45 | **86-88%** |
| **UnStakeToken** | 338 | 142 | ~105-115 | **66-69%** |
| **StakeToken** | 272 | 122 | 122 | 55.1% |

#### GAS 영향 추정

- CollectReward: finalize 20-25회 감소 → GAS ~5-10% 추가 절감 예상
- 라이프사이클 순 storage 비용: 변화 없음 (DepositView는 in-memory 캐시, store 접근 패턴 불변)

### 2.6 구현 단계

#### Step 1: DepositView 타입 생성 (v1/deposit_view.gno)

- `DepositView` struct 정의
- `NewDepositView(d *sr.Deposit)` 생성자
- 스칼라 필드 읽기 메서드 (로컬 반환)
- Write-through setter 메서드
- Tree 연산 위임 메서드

**변경 파일**: `v1/deposit_view.gno` (신규)

#### Step 2: DepositResolver 리팩터링 (v1/type.gno)

- `DepositResolver`의 embedded 타입을 `*sr.Deposit` → `*DepositView`로 변경
- `NewDepositResolver`에서 `NewDepositView` 호출
- DepositResolver의 기존 메서드에서 `self.Deposit.` → `self.DepositView.` 호출 조정
  - `InternalRewardLastCollectTime()`: DepositView의 로컬 필드 사용
  - `ExternalRewardLastCollectTime()`: tree 연산이므로 위임
  - `addCollectedInternalReward()`: write-through setter 사용
  - `FindWarmup()`, `GetWarmup()`: 로컬 warmups 사용

**변경 파일**: `v1/type.gno`

#### Step 3: CalculateRawRewardForPosition 시그니처 변경 (v1/reward_calculation_pool.gno)

- `CalculateRawRewardForPosition(time, tick, *sr.Deposit)` → `CalculateRawRewardForPosition(time, tick, *DepositView)`
- `CalculateRewardForPosition` 동일 변경
- `RewardStateOf(deposit *sr.Deposit)` → `RewardStateOf(view *DepositView)` 변경

**변경 파일**: `v1/reward_calculation_pool.gno`

#### Step 4: 호출 체인 갱신 (v1/calculate_pool_position_reward.gno, v1/staker.gno)

- `calculatePositionReward`에서 `deposit` → `depositView` 사용
- `collectRewardInternal`에서 `deposit.Owner()` 등을 `depositView.Owner()` 로 대체
- `applyUnStakeWith`에서 동일 대체
- `rewardPerWarmup`에서 `self.deposit.Warmups()`, `self.deposit.Liquidity()` → DepositView 접근

**변경 파일**: `v1/calculate_pool_position_reward.gno`, `v1/staker.gno`

#### Step 5: RewardState 리팩터링 (v1/reward_calculation_pool.gno)

- `RewardState.deposit` 타입을 `*DepositResolver` → DepositView 기반으로 변경
- `rewardPerWarmup`에서 deposit.Warmups(), Liquidity() 호출이 로컬 접근으로 전환

**변경 파일**: `v1/reward_calculation_pool.gno`

#### Step 6: 단위 테스트 갱신

- `_mock_test.gno`: 변경 불필요 (IStakerStore 인터페이스 미변경)
- `getter_test.gno`: DepositResolver 사용 패턴 업데이트 (있을 경우)
- 통합 테스트(`staker_storage_staker_lifecycle.txtar`): finalize 수 검증

**변경 파일**: 테스트 파일들

### 2.7 변경 범위 요약

| 파일 | 변경 유형 | 위험도 |
|:---|:---|:---:|
| `v1/deposit_view.gno` | **신규 생성** | 낮음 |
| `v1/type.gno` | DepositResolver embed 타입 변경 | 중간 |
| `v1/reward_calculation_pool.gno` | 시그니처 변경 (3개 함수) | 중간 |
| `v1/calculate_pool_position_reward.gno` | 호출 체인 갱신 | 낮음 |
| `v1/staker.gno` | deposit → depositView 대체 | 중간 |
| `v1/assert.gno` | `assertIsDepositorWith` 내 deposit.Owner() — 개별 DepositView 불요, 유지 | 없음 |

**인터페이스 변경**: IStakerStore 변경 없음. 변경은 v1 패키지 내부에 한정.

### 2.8 리스크 분석

| 리스크 | 심각도 | 완화 방안 |
|:---|:---:|:---|
| DepositView와 원본 Deposit 상태 불일치 | 중간 | Write-through 패턴으로 즉시 동기화. 트랜잭션 내 단일 스레드이므로 경합 없음 |
| CalculateRawRewardForPosition 시그니처 변경으로 인한 호출자 미갱신 | 낮음 | 컴파일 타임 에러로 즉시 발견 |
| Warmup 배열 shallow copy로 인한 공유 참조 | 없음 | **확인 완료**: `Warmup` struct는 `int`, `int64`, `int64`, `uint64` 4개 필드만 가진 순수 value type. 포인터/slice 필드 없음. `copy()` 안전 |
| Liquidity (*u256.Uint) in-place mutation | 낮음 | **확인 완료**: `rewardPerWarmup`에서 `u256.Zero().Mul(rewardAcc, deposit.Liquidity())` 패턴 — receiver `z`에만 쓰고 `y`(Liquidity)는 읽기만. 그러나 **안전 마진으로 `.Clone()` 복사 권장** (1회 추가 finalize, 미묘한 in-place mutation 버그 예방) |
| Tree 필드 (externalIncentiveIds 등) tainted 유지 | 없음 | 설계 의도. Tree 연산은 순회 특성상 캐싱 불가, 위임이 적절 |

### 2.9 Cross-Realm Taint 주의사항

P1 Task A(PoolTier Key Consolidation)에서 발견된 것과 동일한 cross-realm taint 제약이
DepositView에도 적용된다:

1. **`Warmups()` 배열**: proxy realm 소유. 요소별 복사 필요.
   **확인 완료**: `Warmup`은 `{Index int, TimeDuration int64, NextWarmupTime int64, WarmupRatio uint64}`로
   포인터/slice 필드 없는 순수 value type. `copy(localWarmups, warmups)`로 안전하게 deep copy됨.
2. **`Liquidity()` (*u256.Uint)**: proxy realm 소유 포인터. 현재 코드에서 읽기만 수행하지만
   (`u256.Zero().Mul(rewardAcc, liquidity)` — receiver에만 쓰기), **방어적으로 `.Clone()` 복사 권장**.
   비용: 1회 추가 finalize. 효과: u256의 in-place mutation 메서드(`Mul`, `Add` 등이 receiver를
   변경하는 패턴)가 향후 deposit.Liquidity()에 직접 적용될 경우의 미묘한 버그를 사전 차단.
3. **Tree 필드**: 깊은 복사가 비용 대비 효과 없음 (entry당 finalize 발생). 위임이 적절

---

## 3. Item 2: StakeToken 잔여 Finalize 분석

### 3.1 StakeToken 122회 Finalize 분류

StakeToken은 새 deposit을 생성하므로 **Deposit accessor 최적화의 대상이 아니다**.
잔여 122회의 원인을 분류한다:

#### Store Batch Load (6회)

| Store 접근 | finalize |
|:---|:---:|
| `s.store.GetDeposits()` | 1 |
| `s.store.GetPools()` | 1 |
| `s.store.GetStakers()` | 1 |
| `s.store.GetExternalIncentivesByCreationTime()` | 1 |
| `s.store.GetExternalIncentives()` | 1 |
| `s.store.GetPoolTierState()` | 1 |

이미 P1 Task B에서 최적화 완료. 추가 감소 불가.

#### External Realm 호출 (~27회)

| 호출 | Realm | finalize | 유형 |
|:---|:---|:---:|:---|
| `halt.AssertIsNotHaltedStaker()` | halt | 1 | read |
| `en.MintAndDistributeGns(cross)` | emission | 3-5 | write (explicit_cross) |
| `referral.TryRegister(cross, ...)` | referral | 1-2 | write |
| `referral.GetReferral(...)` | referral | 0-1 | read (conditional) |
| `pn.GetPositionPoolKey(positionId)` | position | 1 | read |
| `pn.IsInRange(positionId)` | position | 1 | read |
| `pn.SetPositionOperator(cross, ...)` | position | 1 | write (explicit_cross) |
| `pn.GetPositionLiquidity(positionId)` | position | 1 | read |
| `pn.GetPositionTickLower/Upper(...)` | position | 2 | read |
| `s.nftAccessor.MustOwnerOf(...)` | gnft | 1 | read |
| `s.nftAccessor.TransferFrom(...)` | gnft | 1 | write (explicit_cross) |
| `access.MustGetAddress(...)` | access | 1 | read |
| `s.poolAccessor.GetSlot0Tick(...)` | pool | 1 | read |
| `s.poolAccessor.GetSlot0SqrtPriceX96(...)` | pool | 1 | read |
| `common.TickMathGetSqrtRatioAtTick(...)` | common | 2 | read |
| `common.GetAmountsForLiquidity(...)` | common | 1 | read |
| `common.LiquidityMathAddDelta(...)` | common | 1 | read |
| `s.store.GetWarmupTemplate()` | staker proxy | 1 | read |

이들은 구조적으로 필요한 cross-realm 호출이며, VM 변경 없이는 감소 불가.

#### Pool 객체 Tainted 접근 (~35-40회)

StakeToken에서 Pool 관련 접근의 핵심 경로:

```
pools.Get(poolPath)                    → tainted tree Get
pool.PoolPath()                        → tainted field
poolTier.cacheReward(...)              → 내부적으로 모든 tiered pool 순회:
  ├── membership.Iterate(...)          → tainted tree iterate
  ├── pools.Get(key)                   → tainted tree Get (per pool)
  ├── pool.RewardCache().ReverseIterate → tainted UintTree
  ├── pool.StakedLiquidity().ReverseIterate → tainted UintTree
  └── pool.RewardCache().Set(...)      → tainted UintTree Set
poolResolver.modifyDeposit(...)        → tainted Pool 필드 접근:
  ├── pool.StakedLiquidity().ReverseIterate
  ├── pool.GlobalRewardRatioAccumulation().Set
  ├── pool.HistoricalTick().Set
  └── pool.StakedLiquidity().Set
poolResolver.TickResolver(tickUpper/Lower) → tainted Ticks tree access
  ├── ticks.Get(tickId)                → tainted tree Get
  ├── tick.StakedLiquidityGross()      → tainted field
  ├── tick.StakedLiquidityDelta()      → tainted field
  └── tick.OutsideAccumulation().Set   → tainted UintTree Set
```

#### AVL Tree 조작 (~15-20회)

| 조작 | 대상 | finalize |
|:---|:---|:---:|
| `deposits.Has(positionId)` | tainted deposits tree | 1 |
| `deposits.set(positionId, deposit)` | tainted deposits tree | 1 |
| `stakers.addDeposit(...)` | tainted stakers tree | 2 (Get + Set) |
| `pools.Get(poolPath)` | tainted pools tree | 1 |
| `pools.set(poolPath, pool)` | tainted pools tree | 1 |
| `externalIncentivesByCreationTime.Iterate(...)` | tainted UintTree | 1+ |
| `externalIncentives.get(incentiveId)` | tainted incentives tree | per incentive |
| `incentive.EndTimestamp()` | tainted ExternalIncentive | per incentive |

### 3.2 Pool Accessor 최적화 가능성 분석

#### Pool Snapshot 패턴 (DepositView와 유사)

Pool의 스칼라 필드를 로컬 복사하는 방식을 검토한다:

```go
type PoolView struct {
    poolPath            string   // 복사 가능
    lastUnclaimableTime int64    // 복사 가능
    unclaimableAcc      int64    // 복사 가능

    // Tree 필드 — 복사 불가 (깊은 복사 비용 > 절감 효과)
    stakedLiquidity               *sr.UintTree  // 위임
    rewardCache                   *sr.UintTree  // 위임
    globalRewardRatioAccumulation *sr.UintTree  // 위임
    historicalTick                *sr.UintTree  // 위임
    ticks                         *sr.Ticks     // 위임
    incentives                    *sr.Incentives // 위임

    Pool *sr.Pool  // 원본 참조
}
```

**문제점**:

1. **스칼라 필드가 3개뿐**: `poolPath`, `lastUnclaimableTime`, `unclaimableAcc`.
   나머지는 모두 Tree/구조체로 복사 불가. 캐싱 효과가 미미하다.

2. **Pool의 핵심 연산이 Tree 기반**: `CurrentStakedLiquidity()`, `CurrentReward()`,
   `CurrentGlobalRewardRatioAccumulation()` 등은 UintTree.ReverseIterate를 사용.
   Tree가 tainted인 한 각 iterate가 finalize를 트리거한다.

3. **UintTree 깊은 복사 비용**: `pool.StakedLiquidity()`는 timestamp→*u256.Uint 매핑.
   모든 entry를 복사하려면 iterate(finalize 다수) + 메모리 할당. 복사 비용 > 절감 효과.

4. **cacheReward는 모든 tiered pool 순회**: `applyCacheToAllPools`에서 N개 pool을
   순회하므로, 단일 Pool의 snapshot으로는 전체 비용을 줄일 수 없다.

**결론**: Pool Snapshot 패턴은 **비용 대비 효과가 낮아 권장하지 않는다**.

#### Pool 접근 최적화 대안 검토

| 방안 | 예상 효과 | 복잡도 | 권장 |
|:---|:---:|:---:|:---:|
| Pool 스칼라 필드 캐싱 | ~3회 감소 | 낮음 | 효과 미미, 비권장 |
| UintTree.ReverseIterate 결과 캐싱 | ~5-8회 감소 | 높음 | 복잡도 대비 효과 부족 |
| Pool 전체 snapshot (깊은 복사) | ~15-20회 감소 | 매우 높음 | 복사 비용이 절감을 상쇄, 비권장 |
| cacheReward 중복 호출 제거 | ~2-3회 감소 | 중간 | 제한적 효과 |

### 3.3 StakeToken 최적화 권장사항

**StakeToken의 122회는 컨트랙트 레벨에서 근접 최적(near-optimal)으로 판단한다.**

근거:
1. **External realm 호출 (~27회)**: 구조적으로 필수. 감소 불가.
2. **Pool tainted 접근 (~35-40회)**: Tree 기반 연산이 지배적. 스칼라 캐싱으로는 효과 미미.
3. **Store batch load (~6회)**: 이미 최적화 완료.
4. **AVL tree 조작 (~15-20회)**: tree 자체가 tainted. 접근 패턴 변경 불가.

**추가 개선은 VM 레벨 최적화에 의존**:
- Dirty mark 없는 finalize skip → 77% no-op 제거 → StakeToken ~28회까지 감소 가능
- 이는 P3 범위이며 Gno VM 코어 수정 필요

### 3.4 ExternalIncentive Accessor 최적화 (부수적 효과)

CollectReward의 external 보상 루프에서 `*sr.ExternalIncentive`도 tainted 객체이다.
각 인센티브에 대해 `RewardAmount()`, `RewardToken()`, `TargetPoolPath()`, `EndTimestamp()` 등을
반복 호출한다.

DepositView와 동일한 패턴으로 `ExternalIncentiveView`를 도입할 수 있으나:
- 인센티브당 접근 횟수: ~8-12회
- 인센티브 수: 테스트 시나리오에서 0-2개
- 총 효과: ~0-24회 (인센티브 수에 비례)
- 복잡도: 낮음 (ExternalIncentive는 스칼라 필드만)

**P2에서 DepositView와 함께 구현을 권장하되, 우선순위는 DepositView보다 낮다.**

---

## 4. 구현 우선순위

| 순위 | 항목 | 효과 | 복잡도 | 대상 오퍼레이션 |
|:---:|:---|:---:|:---:|:---|
| **1** | DepositView 패턴 | 높음 (20-37 finalize 감소) | 중간 | CollectReward, UnStakeToken |
| **2** | ExternalIncentiveView | 낮음-중간 (인센티브 수 의존) | 낮음 | CollectReward |
| — | Pool Snapshot | 낮음 | 매우 높음 | **비권장** |
| — | StakeToken Pool 캐싱 | 매우 낮음 | 중간 | **비권장** |

### 권장 실행 순서

1. **DepositView 구현** (Step 1-5)
2. 통합 테스트로 finalize 수 검증
3. 효과가 확인되면 ExternalIncentiveView 추가 검토
4. StakeToken은 현재 수준 유지 (VM 최적화 대기)

---

## 5. 예상 최종 결과 (P2 완료 후)

### Finalize 트리거 수

| Operation | Baseline | P1 후 | P2 후 (추정) | 전체 감소율 |
|:---|:---:|:---:|:---:|:---:|
| **CollectReward** | 332 | 65 | **~40-45** | **86-88%** |
| **UnStakeToken** | 338 | 142 | **~105-115** | **66-69%** |
| **StakeToken** | 272 | 122 | **122** | **55.1%** |

### GAS 영향

| Operation | P1 GAS | P2 GAS (추정) | 변화 |
|:---|:---:|:---:|:---:|
| **CollectReward** | 34,464,844 | ~31-33M | **-5~10%** |
| **UnStakeToken** | 57,092,433 | ~53-55M | **-3~7%** |
| **StakeToken** | 61,688,264 | ~61.7M | **±0** |

### 컨트랙트 레벨 최적화 한계

P2 완료 후 잔여 finalize는 대부분 다음 두 카테고리에 해당한다:

1. **External realm 호출**: 구조적으로 필수 (halt, emission, position, gnft, common)
2. **Tainted tree 연산**: Pool/Tick/UintTree에 대한 tree traverse/modify

이 두 카테고리는 VM 레벨 변경(dirty mark skip, read-only finalize 제거) 없이는
더 이상 줄일 수 없다. 컨트랙트 레벨 최적화는 P2로 실질적 종료 지점에 도달한다.

---

## 관련 문서

- [P1 Overview](./P1_overview.md) — 배경, 아키텍처, 멘탈 모델
- [P1 Task A: Key Consolidation](./P1_task_A_key_consolidation.md)
- [P1 Task B: Store Access Caching](./P1_task_B_store_access_caching.md)
- [P1 최종 보고서](../measurements/final_report.md)
- [Finalize 트레이스 분석](../analysis/staker_finalize_trace_analysis.md)
