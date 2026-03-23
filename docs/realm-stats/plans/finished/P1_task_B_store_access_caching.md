# 작업 B: Store Access Caching

> Phase 1에서 수행. [Overview](./P1_overview.md) 참조.

## 문제

동일한 store key가 한 오퍼레이션 내에서 여러 번 읽힌다:

**StakeToken 내 store.Get 호출 추적**:

| 호출 위치 | store key | 비고 |
|-----------|-----------|------|
| line 280: `s.getPools()` | `pools` | 풀 정보 조회 |
| line 290: `s.poolHasIncentives()` → `s.getPoolTier()` | `poolTierState` | 인센티브 체크 |
| line 298: `s.store.GetWarmupTemplate()` | `warmupTemplate` | 워밍업 템플릿 |
| line 313: `s.getExternalIncentivesByCreationTime()` | `externalIncentivesByCreationTime` | 인센티브 ID 조회 |
| line 316: `s.getExternalIncentives()` | `externalIncentives` | 인센티브 상세 (루프 내) |
| line 328: `s.getDeposits()` | `deposits` | 디포짓 저장 |
| line 331: `s.getStakers()` | `stakers` | 스테이커 저장 |
| line 342: `s.getPoolTier()` | `poolTierState` | **재조회** (line 290에서 이미 조회) |
| line 363: `s.getPools()` | `pools` | **재조회** (line 280에서 이미 조회) |

`getPools()` 2회, `getPoolTier()` 2회 = 불필요한 store.Get 2~3회 (Key Consolidation 후)

**CollectReward 내 store.Get 호출 추적**:

| 호출 위치 | store key | 비고 |
|-----------|-----------|------|
| line 467: `s.getDeposits().get(positionId)` | `deposits` | 디포짓 조회 |
| line 477: `s.calcPositionReward()` 내부 | `deposits`, `pools`, `poolTierState` | 보상 계산 |
| line 489: `s.getExternalIncentives().get()` | `externalIncentives` | **루프마다 재조회** |
| line 559: `s.getExternalIncentives().set()` | `externalIncentives` | **루프마다 재조회** |
| line 581: `s.getPools().Get()` | `pools` | **루프마다 재조회** |
| line 631: `s.store.GetTotalEmissionSent()` | `totalEmissionSent` | 1회 |
| line 663: `s.getDeposits()` | `deposits` | **재조회** |
| line 682: `s.getPools().Get()` | `pools` | **재조회** |

`deposits` 3회, `pools` 3+N회, `externalIncentives` 2N회 (N=인센티브 수) = 심각한 반복

## 접근 방법: 로컬 변수 캐싱 + 파라미터 전달

`stakerV1` struct에 캐시 필드를 추가하지 **않는다**.
이유: `stakerV1`는 proxy realm에 저장되는 영속 객체이므로, 캐시 필드가 트랜잭션 간에
잔존하여 stale data 문제를 유발한다.

대신, 각 public 오퍼레이션의 진입점에서 로컬 변수로 로드하고,
helper 함수에 파라미터로 전달한다.

> 이 최적화의 구조적 배경(DB N+1 문제와의 유사성, 작업 순서 근거 등)은
> [P1 Overview](./P1_overview.md#멘탈-모델-db-n1-문제와의-구조적-유사성) 참조.

---

## 변경 파일 및 상세 내용

### B-1. `r/gnoswap/staker/v1/staker.gno` — StakeToken 리팩토링

**변경 원칙**: 오퍼레이션 시작 시 필요한 store 데이터를 모두 로드하고,
이후에는 로컬 변수를 재사용한다.

**변경 전** (주요 부분):

```go
func (s *stakerV1) StakeToken(positionId uint64, referrer string) string {
    // ...
    poolPath := pn.GetPositionPoolKey(positionId)
    pools := s.getPools()              // store.Get #1
    pool, ok := pools.Get(poolPath)
    // ...
    err := s.poolHasIncentives(pool)   // 내부에서 s.getPoolTier() → store.Get #2
    // ...
    deposits := s.getDeposits()        // store.Get #3
    deposits.set(positionId, deposit)
    s.getStakers().addDeposit(...)     // store.Get #4
    // ...
    poolTier := s.getPoolTier()        // store.Get #5 (재조회!)
    poolTier.cacheReward(height, time, pools)
    s.updatePoolTier(poolTier)         // store.Set #1
    // ...
    s.getPools().set(poolPath, pool)   // store.Get #6 (재조회!)
}
```

**변경 후**:

```go
func (s *stakerV1) StakeToken(positionId uint64, referrer string) string {
    // ...

    // === 1. 오퍼레이션 시작: 필요 데이터 일괄 로드 ===
    pools := s.getPools()                                          // store.Get ×1
    poolTier := s.getPoolTier()                                    // store.Get ×1
    deposits := s.getDeposits()                                    // store.Get ×1
    stakers := s.getStakers()                                      // store.Get ×1
    externalIncentivesByCreationTime := s.getExternalIncentivesByCreationTime() // store.Get ×1
    externalIncentives := s.getExternalIncentives()                // store.Get ×1

    poolPath := pn.GetPositionPoolKey(positionId)
    pool, ok := pools.Get(poolPath)
    if !ok {
        panic(makeErrorWithDetails(
            errNonIncentivizedPool,
            ufmt.Sprintf("cannot stake position to non existing pool(%s)", poolPath),
        ))
    }

    err := s.poolHasIncentivesWithTier(pool, poolTier) // poolTier 전달 (재조회 없음)
    if err != nil {
        panic(err.Error())
    }

    // ...

    warmups := s.store.GetWarmupTemplate()                         // store.Get ×1
    // ...

    // 인센티브 ID 조회 (이미 로드된 externalIncentivesByCreationTime 사용)
    currentIncentiveIds := s.getExternalIncentiveIdsByWith(
        externalIncentivesByCreationTime, poolPath, 0, currentTime,
    )

    for _, incentiveId := range currentIncentiveIds {
        incentive := externalIncentives.get(incentiveId)  // 이미 로드된 tree에서 조회
        if currentTime > incentive.EndTimestamp() {
            continue
        }
        deposit.AddExternalIncentiveId(incentiveId)
    }

    deposits.set(positionId, deposit)                              // in-memory
    stakers.addDeposit(caller, positionId, deposit)                // in-memory (이미 로드)

    // ...

    poolTier.cacheReward(runtime.ChainHeight(), currentTime, pools) // 이미 로드된 값 사용
    s.updatePoolTier(poolTier)                                     // store.Set ×1

    // ...

    pools.set(poolPath, pool)                                      // in-memory (이미 로드)

    // ...
}
```

**store 접근 횟수 비교**:
- Before: ~10+ store.Get + 1+ store.Set = ~12 finalize (Key Consolidation 후)
- After: 7 store.Get + 1 store.Set = 8 finalize
- 절감: **~4 finalize** (StakeToken 단독)

### B-2. `r/gnoswap/staker/v1/staker.gno` — CollectReward 리팩토링

**이 변경이 가장 큰 효과를 가진다.** 루프 내 반복 store.Get을 제거한다.

**변경 후** (구조):

```go
func (s *stakerV1) CollectReward(positionId uint64) (string, string, map[string]int64, map[string]int64) {
    halt.AssertIsNotHaltedStaker()
    halt.AssertIsNotHaltedWithdraw()

    caller := runtime.PreviousRealm().Address()

    // === 0. 데이터 일괄 로드 (assertIsDepositor 이전) ===
    // 주의: assertIsDepositor 내부에서 s.getDeposits().get(positionId)를 호출한다.
    // 일괄 로드를 먼저 수행하고 deposits를 파라미터로 전달하면 이중 로드를 방지한다.
    // (존재하지 않는 positionId의 경우 어차피 panic이므로 불필요한 로드 문제 없음)
    deposits := s.getDeposits()                    // store.Get ×1
    pools := s.getPools()                          // store.Get ×1
    externalIncentives := s.getExternalIncentives() // store.Get ×1

    assertIsDepositorWith(deposits, caller, positionId)  // in-memory (store 접근 없음)

    deposit := deposits.get(positionId)            // in-memory
    depositResolver := NewDepositResolver(deposit)

    // === 1. emission 상태 변경 (반드시 poolTier 로드 전에!) ===
    en.MintAndDistributeGns(cross)

    // === 2. poolTier는 emission 반영 후 로드 ===
    poolTier := s.getPoolTier()                    // store.Get ×1 (emission 반영된 값)

    currentTime := time.Now().Unix()
    blockHeight := runtime.ChainHeight()

    // === 2. 보상 계산 (사전 로드된 데이터 전달) ===
    reward := s.calcPositionRewardWith(blockHeight, currentTime, positionId, deposits, pools, poolTier)

    // === 3. 외부 보상 처리 (사전 로드된 externalIncentives 사용) ===
    communityPoolAddr := access.MustGetAddress(prbac.ROLE_COMMUNITY_POOL.String())
    // ...

    for incentiveId, rewardAmount := range reward.External {
        // ...
        incentive := externalIncentives.get(incentiveId)  // in-memory (재조회 없음!)
        // ...
        externalIncentives.set(incentiveId, incentive)     // in-memory
        // ...

        pool, _ := pools.Get(deposit.TargetPoolPath())     // in-memory (재조회 없음!)
        // ...
    }

    // === 4. 내부 보상 처리 ===
    totalEmissionSent := s.store.GetTotalEmissionSent()     // store.Get ×1
    // ...
    err = s.store.SetTotalEmissionSent(totalEmissionSent)   // store.Set ×1
    // ...

    deposits.set(positionId, deposit)                        // in-memory

    // === 5. GNS 전송 (cross-realm) ===
    if toUser > 0 {
        gns.Transfer(cross, deposit.Owner(), toUser)
    }
    // ...

    // === 6. 이벤트용 풀 정보 (이미 로드된 pools 사용) ===
    poolPath := depositResolver.TargetPoolPath()
    pool, _ := pools.Get(poolPath)                           // in-memory
    poolResolver := NewPoolResolver(pool)
    // ...
}
```

**store 접근 횟수 비교**:
- Before: ~5 + 2N (N=인센티브 수) store.Get + store.Set 여러 회
- After: 5 store.Get + 1 store.Set = 6 finalize (인센티브 수 무관)
- 절감: 인센티브 3개 기준 **~7-10 finalize**, 인센티브 10개 기준 **~20+ finalize**

### B-3. 헬퍼 함수 시그니처 변경

다음 함수들의 시그니처를 변경하여 사전 로드된 데이터를 받을 수 있도록 한다:

#### `poolHasIncentives` → `poolHasIncentivesWithTier`

**현재**:

```go
// staker.gno 내 어딘가
func (s *stakerV1) poolHasIncentives(pool *sr.Pool) error {
    hasInternal := s.getPoolTier().IsInternallyIncentivizedPool(poolPath)
    // ...
}
```

**변경**:

```go
func (s *stakerV1) poolHasIncentivesWithTier(pool *sr.Pool, poolTier *PoolTier) error {
    hasInternal := poolTier.IsInternallyIncentivizedPool(pool.PoolPath())
    // ...
}
```

> 기존 `poolHasIncentives` 메서드는 유지하면서 새 메서드를 추가해도 되고,
> 기존 메서드를 직접 수정해도 된다. 호출자가 모두 이 파일 내에 있으므로
> 직접 수정이 더 깔끔하다.

#### `calcPositionReward` → `calcPositionRewardWith`

**현재**:

```go
func (s *stakerV1) calcPositionReward(currentHeight, currentTimestamp int64, positionId uint64) Reward {
    rewards := s.calculatePositionReward(&CalcPositionRewardParam{
        CurrentHeight: currentHeight,
        CurrentTime:   currentTimestamp,
        Deposits:      s.getDeposits(),     // store.Get!
        Pools:         s.getPools(),         // store.Get!
        PoolTier:      s.getPoolTier(),      // store.Get!
        PositionId:    positionId,
    })
    // ...
}
```

**변경**: 기존 `calcPositionReward`를 래퍼로 유지하고, 새 메서드 추가

```go
// 기존 메서드 (하위 호환 — 다른 곳에서 호출 시)
func (s *stakerV1) calcPositionReward(currentHeight, currentTimestamp int64, positionId uint64) Reward {
    return s.calcPositionRewardWith(
        currentHeight, currentTimestamp, positionId,
        s.getDeposits(), s.getPools(), s.getPoolTier(),
    )
}

// 새 메서드 (사전 로드된 데이터 사용)
func (s *stakerV1) calcPositionRewardWith(
    currentHeight, currentTimestamp int64,
    positionId uint64,
    deposits *Deposits,
    pools *Pools,
    poolTier *PoolTier,
) Reward {
    rewards := s.calculatePositionReward(&CalcPositionRewardParam{
        CurrentHeight: currentHeight,
        CurrentTime:   currentTimestamp,
        Deposits:      deposits,
        Pools:         pools,
        PoolTier:      poolTier,
        PositionId:    positionId,
    })
    // ...
}
```

#### `getExternalIncentiveIdsBy` → `getExternalIncentiveIdsByWith`

**현재**:

```go
func (s *stakerV1) getExternalIncentiveIdsBy(poolPath string, startTime, endTime int64) []string {
    incentivesByTime := s.getExternalIncentivesByCreationTime() // store.Get!
    // ...
}
```

**변경**:

```go
// 기존 (하위 호환)
func (s *stakerV1) getExternalIncentiveIdsBy(poolPath string, startTime, endTime int64) []string {
    return s.getExternalIncentiveIdsByWith(s.getExternalIncentivesByCreationTime(), poolPath, startTime, endTime)
}

// 새 메서드
func (s *stakerV1) getExternalIncentiveIdsByWith(incentivesByTime *sr.UintTree, poolPath string, startTime, endTime int64) []string {
    currentIncentiveIds := make([]string, 0)
    incentivesByTime.Iterate(startTime, endTime, func(_ int64, value any) bool {
        // ... (기존 로직 동일)
    })
    return currentIncentiveIds
}
```

### B-4. `r/gnoswap/staker/v1/manage_pool_tier_and_warmup.gno` — 풀 티어 관리 함수 최적화

**setPoolTier, changePoolTier, removePoolTier**에서도 불필요한 재조회가 있다:

**현재** (setPoolTier):

```go
func (s *stakerV1) setPoolTier(poolPath string, tier uint64, currentTime int64) {
    s.emissionAccessor.MintAndDistributeGns()

    pool := s.getPools().GetPoolOrNil(poolPath)    // store.Get #1
    if pool == nil {
        pool = sr.NewPool(poolPath, currentTime)
        s.getPools().set(poolPath, pool)            // store.Get #2 (재조회!)
    }
    poolTier := s.getPoolTier()                     // store.Get #3
    poolTier.changeTier(runtime.ChainHeight(), currentTime, s.getPools(), poolPath, tier) // store.Get #4 (재조회!)
    s.updatePoolTier(poolTier)                      // store.Set #1
}
```

**변경 후**:

```go
func (s *stakerV1) setPoolTier(poolPath string, tier uint64, currentTime int64) {
    s.emissionAccessor.MintAndDistributeGns()

    pools := s.getPools()                           // store.Get ×1
    pool := pools.GetPoolOrNil(poolPath)
    if pool == nil {
        pool = sr.NewPool(poolPath, currentTime)
        pools.set(poolPath, pool)                   // in-memory
    }
    poolTier := s.getPoolTier()                     // store.Get ×1
    poolTier.changeTier(runtime.ChainHeight(), currentTime, pools, poolPath, tier)
    s.updatePoolTier(poolTier)                      // store.Set ×1
}
```

**동일 패턴을 `changePoolTier`, `removePoolTier`에도 적용.**

### B-5. `r/gnoswap/staker/v1/staker.gno` — UnStakeToken 리팩토링

**현재**:

```go
func (s *stakerV1) UnStakeToken(positionId uint64) string {
    // ...
    deposit := s.getDeposits().get(positionId)  // store.Get #1
    poolPath := deposit.TargetPoolPath()
    s.CollectReward(positionId)                 // 내부에서 다시 store.Get 다수 발생
    s.applyUnStake(positionId)                  // 내부에서 또 store.Get 발생
    // ...
    s.getPools().Get(poolPath)                  // store.Get (재조회)
}
```

UnStakeToken은 내부에서 `CollectReward`를 호출하고, 그 후 `applyUnStake`를 호출한다.
CollectReward 리팩토링(B-2)이 완료되면 UnStakeToken도 동일 패턴을 적용한다.

**다만 주의**: UnStakeToken 내부의 `s.CollectReward(positionId)` 호출은
`*stakerV1`의 same-realm 메서드 호출이며, crossing 경계를 형성하지 **않는다**.
(Crossing 경계가 형성되는 것은 proxy가 `implementation.CollectReward()`를 호출할 때이다.)

그러나 `CollectReward` 내부에서 독립적으로 store 데이터를 다시 로드하므로,
UnStakeToken에서 이미 로드한 데이터를 재사용할 수 없다. 이는 crossing 경계 문제가 아니라
**코드 설계상의 문제**(함수 간 로드된 데이터 공유 부재)이다. 두 가지 선택지가 있다:

**선택지 1**: CollectReward의 내부 구현을 별도 private 메서드로 분리

```go
// private 구현 (데이터 전달 가능)
func (s *stakerV1) collectRewardInternal(
    positionId uint64,
    deposits *Deposits,
    pools *Pools,
    poolTier *PoolTier,
    externalIncentives *ExternalIncentives,
) (string, string, map[string]int64, map[string]int64) {
    // CollectReward의 핵심 로직
}

// public 인터페이스
func (s *stakerV1) CollectReward(positionId uint64) (...) {
    deposits := s.getDeposits()
    pools := s.getPools()
    poolTier := s.getPoolTier()
    externalIncentives := s.getExternalIncentives()
    return s.collectRewardInternal(positionId, deposits, pools, poolTier, externalIncentives)
}

// UnStakeToken에서 직접 호출
func (s *stakerV1) UnStakeToken(positionId uint64) string {
    deposits := s.getDeposits()
    pools := s.getPools()
    poolTier := s.getPoolTier()
    externalIncentives := s.getExternalIncentives()

    // ...
    s.collectRewardInternal(positionId, deposits, pools, poolTier, externalIncentives)
    s.applyUnStakeWith(positionId, deposits, pools)
    // ...
}
```

**선택지 2**: UnStakeToken은 그대로 `s.CollectReward()` 호출 (간단하지만 데이터 중복 로드 유지)

**권장**: 선택지 1. UnStakeToken의 finalize가 338회로 가장 높으므로 최적화 효과가 크다.
다만 변경 범위가 넓으므로, 1차로 선택지 2를 적용하고 2차에서 선택지 1을 적용할 수 있다.

### B-6. `r/gnoswap/staker/v1/staker.gno` — applyUnStake에도 동일 패턴

**현재**:

```go
func (s *stakerV1) applyUnStake(positionId uint64) error {
    deposit := s.getDeposits().get(positionId)   // store.Get
    // ...
    pool, ok := s.getPools().Get(...)            // store.Get
    // ...
}
```

**변경**: pools, deposits를 파라미터로 받는 버전 추가.

### B-7. Cross-Realm Value Caching

Store Access Caching(B-1~B-6)이 동일 store key의 반복 읽기를 제거하는 것이라면,
cross-realm 호출 반복 제거는 별도로 다뤄야 한다.

코드 분석에서 확인된 반복 cross-realm/stdlib 호출:

| 호출 | 전체 발생 횟수 | 성격 | 캐싱 가능 여부 |
|------|:-----------:|------|:------------:|
| `time.Now().Unix()` | 17회 | 트랜잭션 내 불변 | O |
| `runtime.ChainHeight()` | 13회 | 블록 내 불변 | O |
| `runtime.PreviousRealm()` | 22회 | 호출 컨텍스트 의존 | △ (동일 함수 내에서만) |
| `en.MintAndDistributeGns(cross)` | 3회 | 부수 효과 있음 | X (1회만 호출) |

**적용 방법**: 각 public 오퍼레이션 진입 시 1회만 호출하고 로컬 변수로 전달한다.

```go
func (s *stakerV1) StakeToken(positionId uint64) string {
    // === cross-realm/stdlib 값 캐싱 ===
    currentTime := time.Now().Unix()       // 1회
    blockHeight := runtime.ChainHeight()   // 1회
    caller := runtime.PreviousRealm().Address()  // 1회

    en.MintAndDistributeGns(cross)         // 1회 (부수 효과)

    // 이후 모든 helper에 currentTime, blockHeight 전달
    // ...
}
```

**`calcPositionRewardWith` 파라미터 확장**:

```go
func (s *stakerV1) calcPositionRewardWith(
    blockHeight int64,
    currentTime int64,    // time.Now().Unix() 캐싱 값
    positionId uint64,
    deposits *Deposits,
    pools *Pools,
    poolTier *PoolTier,
) *CalcPositionRewardResult {
    // blockHeight, currentTime을 내부에서 다시 호출하지 않음
}
```

**주의**:
- `runtime.PreviousRealm()`은 호출 스택에 따라 결과가 달라질 수 있다.
  같은 함수 내에서 여러 번 호출하는 경우만 캐싱하고, 다른 함수로 전달 시에는
  해당 함수의 실제 PreviousRealm과 일치하는지 확인해야 한다.
- `time.Now().Unix()`와 `runtime.ChainHeight()`는 동일 트랜잭션 내에서 불변이므로
  안전하게 캐싱할 수 있다.

---

## 사이드 이펙트 및 주의사항

### 1. Store Access Caching의 데이터 일관성

로컬 변수에 캐싱한 데이터는 **같은 underlying object에 대한 포인터**이다.
예: `pools := s.getPools()` → `pools.tree`는 proxy realm의 `*avl.Tree` 포인터.
이 포인터를 통한 수정(`pools.set(...)`)은 proxy realm의 원본 tree를 직접 수정한다.

**따라서**: 캐싱 후 수정한 값은 다음 `s.getPools()` 호출에서도 반영된다.
이는 기존 동작과 동일하며, 캐싱이 데이터 불일치를 유발하지 않는다.

단, `updatePoolTier()`처럼 `store.Set()`으로 명시적 저장이 필요한 경우에는
여전히 호출해야 한다. PoolTier는 v1에서 새 struct를 생성하므로 포인터가 다르다.

### 2. 테스트 커버리지 확인

변경되는 함수 시그니처:
- `poolHasIncentives` → `poolHasIncentivesWithTier`
- `calcPositionReward` → `calcPositionRewardWith` (또는 기존 메서드 유지 + 새 메서드 추가)
- `getExternalIncentiveIdsBy` → `getExternalIncentiveIdsByWith`
- `setPoolTier`, `changePoolTier`, `removePoolTier` 내부 변경

각 변경에 대해 기존 통합 테스트(`staker_storage_staker_lifecycle.txtar`)가
StakeToken, CollectReward, UnStakeToken을 모두 실행하므로 커버된다.

### 3. Cross-Realm Call 순서 보존

store.Get 호출 순서를 변경해도 비즈니스 로직에 영향이 없어야 한다.
현재 코드에서 store.Get 간에 의존성이 없으므로 (각 Get은 독립된 key를 읽음),
로딩 순서를 자유롭게 변경할 수 있다.

단, `en.MintAndDistributeGns(cross)` 호출은 emission 상태를 변경하므로,
이 호출 **이후에** poolTier를 로드해야 올바른 emission 값을 캐시할 수 있다.

**StakeToken에서의 순서**:

```go
en.MintAndDistributeGns(cross)  // emission 상태 변경 (반드시 먼저)

// 이후에 데이터 로드
pools := s.getPools()
poolTier := s.getPoolTier()     // emission 반영된 값을 읽음
deposits := s.getDeposits()
```

**CollectReward에서도 동일**:

```go
en.MintAndDistributeGns(cross)  // emission 상태 변경 (반드시 먼저)

// 이후에 데이터 로드
deposits := s.getDeposits()
pools := s.getPools()
poolTier := s.getPoolTier()
```

> **중요**: `en.MintAndDistributeGns(cross)` 호출 전에 poolTier를 로드하면,
> emission이 아직 반영되지 않은 상태의 poolTier를 사용하게 된다.
> 이는 보상 계산에 영향을 줄 수 있으므로 **반드시 emission 이후에 로드한다.**

**CollectReward 특이사항**: 현재 코드에서 `assertIsDepositor`와 `getDeposits()`가
`MintAndDistributeGns` **이전에** 호출된다. deposits와 pools는 emission과 무관하므로
이 순서는 문제 없다. 단, **poolTier만은 반드시 emission 이후에 로드해야 한다.**

일괄 로드로 변경 시 다음 패턴을 따른다:

```go
// emission과 무관한 데이터 → MintAndDistributeGns 전에 로드 가능
deposits := s.getDeposits()
pools := s.getPools()
externalIncentives := s.getExternalIncentives()

en.MintAndDistributeGns(cross)  // emission 상태 변경

// emission에 의존하는 데이터 → 반드시 이후에 로드
poolTier := s.getPoolTier()
```

### 데이터 로드 시점 변경 체크리스트

각 함수를 리팩토링할 때 아래 항목을 반드시 확인한다:

- [x] 현재 코드에서 각 store.Get 호출의 실제 위치(MintAndDistributeGns 전/후)를 확인했는가?
- [x] poolTier 로드가 MintAndDistributeGns 이후에 위치하는가?
- [x] deposits, pools, externalIncentives 로드 시점을 이동해도 비즈니스 로직에 영향이 없는가?
- [x] assertIsDepositor가 일괄 로드된 deposits를 재사용하는가? (이중 로드 방지)
- [x] 현재 코드에서 store.Get 결과가 다른 store.Get의 인자로 사용되는 의존 관계가 없는가?

---

## 구현 결과 요약

> 구현일: 2026-03-11
> 측정 결과 상세: [`measurements/phase1_store_access_caching.md`](../measurements/phase1_store_access_caching.md)

### 항목별 완료 상태

| 항목 | 상태 | 비고 |
|------|:----:|------|
| B-1. StakeToken 리팩토링 | ✅ | 계획대로 구현. emission-independent 데이터를 MintAndDistributeGns 전에 로드, poolTier는 후에 로드. `blockHeight` 캐싱 포함. |
| B-2. CollectReward 리팩토링 | ✅ | `collectRewardInternal` private 메서드로 분리 (선택지 1 채택). public `CollectReward`는 thin wrapper. |
| B-3. 헬퍼 함수 시그니처 변경 | ✅ | `poolHasIncentivesWithTier`, `calcPositionRewardWith`, `getExternalIncentiveIdsByWith`, `processUnClaimableRewardWith` 추가. 기존 메서드는 래퍼로 유지 (하위 호환). |
| B-4. 풀 티어 관리 함수 최적화 | ✅ | `setPoolTier`, `changePoolTier`, `removePoolTier` 모두 `pools` 로컬 변수 캐싱 적용. |
| B-5. UnStakeToken 리팩토링 | ✅ | 선택지 1(권장) 채택. `collectRewardInternal` 직접 호출로 데이터 공유. `applyUnStakeWith` 사용. |
| B-6. applyUnStake 파라미터화 | ✅ | `applyUnStakeWith(positionId, currentTime, deposits, pools, stakers)` 추가. |
| B-7. Cross-Realm Value Caching | ✅ | `time.Now().Unix()`, `runtime.ChainHeight()` 캐싱 적용. |

### Finalize 트리거 감소 결과

| Operation | Baseline | 구현 후 | 감소량 | 감소율 |
|-----------|:--------:|:------:|:------:|:------:|
| **StakeToken** | 272 | 122 | -150 | **-55.1%** |
| **CollectReward** | 332 | 79 | -253 | **-76.2%** |
| **UnStakeToken** | 338 | 186 (avg) | -152 | **-45.0%** |

- CollectReward가 가장 큰 개선 효과: 루프 내 반복 `store.Get` 제거로 인센티브 수에 무관하게 일정한 store 접근 횟수 달성.
- UnStakeToken은 `collectRewardInternal` + `applyUnStakeWith`로 데이터 공유에도 불구하고, 2차 호출 시 상태 정리 로직에서 추가 store 접근 발생 (1차: 156, 2차: 216).

### Finalize Reason 분류

전체 테스트에서 `implicit_cross`(proxy↔impl 간 store 접근)가 **92.2%** (1,159/1,257)를 차지.
이는 store key 수 자체를 줄이는 Key Consolidation(Task A)이 추가 개선에 필수적임을 시사한다.

### Realm별 Finalize 분포

`gno.land/r/gnoswap/pool` realm이 전체 finalize의 **86.1%** (1,082/1,257)를 차지.
staker 내부 캐싱만으로는 pool realm 접근을 줄일 수 없으며, pool 데이터 접근 패턴 최적화는 별도 작업 필요.

### 계획 대비 차이점

1. **선택지 1 즉시 채택**: 계획에서는 "1차로 선택지 2를 적용하고 2차에서 선택지 1을 적용할 수 있다"고 했으나, UnStakeToken의 finalize가 338회로 가장 높아 즉시 선택지 1(`collectRewardInternal` 분리)을 적용.
2. **`assertIsDepositor` 래퍼 제거**: `assertIsDepositorWith`만 남기고 기존 `assertIsDepositor` 래퍼 삭제. 모든 호출 지점이 이미 `assertIsDepositorWith`를 사용하므로 불필요한 래퍼 제거.
3. **`assertIsNotStaked` 인라인화**: StakeToken 내에서 `deposits.Has(positionId)` 체크를 직접 수행. 이미 로드된 deposits를 활용하기 위함.

### 다음 단계

- **Task A (Key Consolidation)**: PoolTier 관련 8개 store key → 1개 통합으로 `getPoolTier()`/`updatePoolTier()`의 store 접근 16회 → 2회로 감소 예상.
- **Pool realm 최적화**: 전체 finalize의 86%를 차지하는 pool realm 접근 패턴 분석 및 최적화.
