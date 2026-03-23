# 작업 A: PoolTier Key Consolidation

> Phase 2에서 수행. [Overview](./P1_overview.md) 참조.

## 현재 상태

PoolTier 데이터가 8개 별도 store key에 분산 저장:

| # | Store Key | 타입 | 용도 |
|---|-----------|------|------|
| 1 | `poolTierMemberships` | `*avl.Tree` | poolPath → tier 매핑 |
| 2 | `poolTierRatio` | `TierRatio` | 티어별 보상 비율 |
| 3 | `poolTierCounts` | `[4]uint64` | 티어별 풀 개수 |
| 4 | `poolTierLastRewardCacheTimestamp` | `int64` | 마지막 보상 캐시 시각 |
| 5 | `poolTierLastRewardCacheHeight` | `int64` | 마지막 보상 캐시 높이 |
| 6 | `poolTierCurrentEmission` | `int64` | 현재 emission 양 |
| 7 | `poolTierGetEmission` | `func() int64` | emission 조회 클로저 |
| 8 | `poolTierGetHalvingBlocksInRange` | `func(start, end int64) ([]int64, []int64)` | halving 조회 클로저 |

`getPoolTier()` = 8 store.Get → `updatePoolTier()` = 8 store.Set → **총 16 finalize/cycle**

## 목표 상태

1개 store key에 통합: `getPoolTier()` = 1 Get, `updatePoolTier()` = 1 Set → **총 2 finalize/cycle**

---

## 변경 파일 및 상세 내용

### A-1. `r/gnoswap/staker/types.gno` — PoolTierState 타입 추가 + 인터페이스 확장

**추가할 타입**:

```go
// PoolTierState는 PoolTier의 모든 상태를 하나의 객체로 통합한다.
// KVStore에 단일 key로 저장되어 store 접근 횟수를 줄인다.
type PoolTierState struct {
    Membership              *avl.Tree                                     // poolPath -> tier(1,2,3)
    TierRatio               TierRatio                                     // 티어별 보상 비율
    Counts                  [4]uint64                                     // AllTierCount=4
    LastRewardCacheTimestamp int64
    LastRewardCacheHeight   int64
    CurrentEmission         int64
    GetEmission             func() int64                                  // emission 조회 클로저
    GetHalvingBlocksInRange func(start, end int64) ([]int64, []int64)     // halving 조회 클로저
}
```

**IStakerStore 인터페이스 변경**:

기존 24개 PoolTier 개별 메서드를 삭제하고 3개 통합 메서드로 교체한다.

```go
type IStakerStore interface {
    // ... 기존 PoolTier 외 메서드 유지 ...

    // PoolTierState — 통합 key (8개 개별 key 대체)
    HasPoolTierStateStoreKey() bool
    GetPoolTierState() *PoolTierState
    SetPoolTierState(state *PoolTierState) error

    // 삭제: HasPoolTierMembershipsStoreKey, GetPoolTierMemberships, SetPoolTierMemberships
    // 삭제: HasPoolTierRatioStoreKey, GetPoolTierRatio, SetPoolTierRatio
    // 삭제: HasPoolTierCountsStoreKey, GetPoolTierCounts, SetPoolTierCounts
    // 삭제: HasPoolTierLastRewardCacheTimestampStoreKey, GetPoolTierLastRewardCacheTimestamp, SetPoolTierLastRewardCacheTimestamp
    // 삭제: HasPoolTierLastRewardCacheHeightStoreKey, GetPoolTierLastRewardCacheHeight, SetPoolTierLastRewardCacheHeight
    // 삭제: HasPoolTierCurrentEmissionStoreKey, GetPoolTierCurrentEmission, SetPoolTierCurrentEmission
    // 삭제: HasPoolTierGetEmissionStoreKey, GetPoolTierGetEmission, SetPoolTierGetEmission
    // 삭제: HasPoolTierGetHalvingBlocksInRangeStoreKey, GetPoolTierGetHalvingBlocksInRange, SetPoolTierGetHalvingBlocksInRange
}
```

기존 8개 PoolTier 개별 메서드(GetPoolTierMemberships, SetPoolTierMemberships, ...)는
인터페이스에서 **제거한다**. 아직 테스트넷 배포 전이므로 마이그레이션을 고려할 필요가 없다.
기존 개별 메서드를 참조하는 모든 코드를 통합 메서드로 전환한다.

**[4]uint64 상수 처리**: v1에 `AllTierCount = 4`로 정의되어 있으나 proxy에서는 직접 참조
불가. proxy에서 리터럴 `[4]uint64`을 사용하거나, proxy에도 동일 상수를 정의한다.

```go
const AllTierCount = 4
```

### A-2. `r/gnoswap/staker/store.gno` — 통합 Get/Set 구현

**추가할 store key**:

```go
StoreKeyPoolTierState StoreKey = "poolTierState"
```

**추가할 메서드**:

```go
func (s *stakerStore) HasPoolTierStateStoreKey() bool {
    return s.kvStore.Has(StoreKeyPoolTierState.String())
}

func (s *stakerStore) GetPoolTierState() *PoolTierState {
    result, err := s.kvStore.Get(StoreKeyPoolTierState.String())
    if err != nil {
        panic(err)
    }

    state, ok := result.(*PoolTierState)
    if !ok {
        panic(ufmt.Sprintf("failed to cast result to *PoolTierState: %T", result))
    }

    return state
}

func (s *stakerStore) SetPoolTierState(state *PoolTierState) error {
    return s.kvStore.Set(StoreKeyPoolTierState.String(), state)
}
```

**기존 8개 개별 메서드**: `store.gno`에서 삭제한다. 개별 store key 상수
(`StoreKeyPoolTierMemberships`, `StoreKeyPoolTierRatio`, ...)도 함께 삭제한다.

### A-3. `r/gnoswap/staker/v1/instance.gno` — getPoolTier/updatePoolTier 단순화

**변경 전** (현재 코드):

```go
func (s *stakerV1) getPoolTier() *PoolTier {
    return NewPoolTierBy(
        s.store.GetPoolTierMemberships(),           // store.Get #1
        s.store.GetPoolTierRatio(),                 // store.Get #2
        s.store.GetPoolTierCounts(),                // store.Get #3
        s.store.GetPoolTierLastRewardCacheTimestamp(), // store.Get #4
        s.store.GetPoolTierLastRewardCacheHeight(),    // store.Get #5
        s.store.GetPoolTierCurrentEmission(),       // store.Get #6
        s.store.GetPoolTierGetEmission(),           // store.Get #7
        s.store.GetPoolTierGetHalvingBlocksInRange(), // store.Get #8
    )
}
```

**변경 후**:

```go
func (s *stakerV1) getPoolTier() *PoolTier {
    state := s.store.GetPoolTierState() // store.Get ×1
    return NewPoolTierBy(
        state.Membership,
        state.TierRatio,
        state.Counts,
        state.LastRewardCacheTimestamp,
        state.LastRewardCacheHeight,
        state.CurrentEmission,
        state.GetEmission,
        state.GetHalvingBlocksInRange,
    )
}
```

**변경 전** (updatePoolTier):

```go
func (s *stakerV1) updatePoolTier(poolTier *PoolTier) {
    err := s.store.SetPoolTierMemberships(poolTier.membership)     // store.Set #1
    // ... (패닉 처리) ...
    err = s.store.SetPoolTierRatio(poolTier.tierRatio)             // store.Set #2
    // ... 총 8개 Set 호출 ...
}
```

**변경 후**:

```go
func (s *stakerV1) updatePoolTier(poolTier *PoolTier) {
    err := s.store.SetPoolTierState(&sr.PoolTierState{ // store.Set ×1
        Membership:              poolTier.membership,
        TierRatio:               poolTier.tierRatio,
        Counts:                  poolTier.counts,
        LastRewardCacheTimestamp: poolTier.lastRewardCacheTimestamp,
        LastRewardCacheHeight:   poolTier.lastRewardCacheHeight,
        CurrentEmission:         poolTier.currentEmission,
        GetEmission:             poolTier.getEmission,
        GetHalvingBlocksInRange: poolTier.getHalvingBlocksInRange,
    })
    if err != nil {
        panic(err)
    }
}
```

### A-4. `r/gnoswap/staker/v1/init.gno` — 초기화 로직 변경

**변경 범위**: `initStoreData()` 함수 내 PoolTier 초기화 부분.

**변경 전** (lines 138-200): 8개 key 개별 초기화

```go
if !stakerStore.HasPoolTierMembershipsStoreKey() {
    err := stakerStore.SetPoolTierMemberships(initializedPoolTier.membership)
    // ...
}
if !stakerStore.HasPoolTierRatioStoreKey() {
    err := stakerStore.SetPoolTierRatio(initializedPoolTier.tierRatio)
    // ...
}
// ... 8개 반복 ...
```

**변경 후**: 통합 key 1회 초기화

```go
if !stakerStore.HasPoolTierStateStoreKey() {
    initializedPoolTier, initializedPools := initializePoolTier(stakerStore)

    // pools 초기화
    if !stakerStore.HasPoolsStoreKey() {
        err := stakerStore.SetPools(initializedPools.tree)
        if err != nil {
            return err
        }
    }

    // getEmission, getHalvingBlocksInRange 클로저 생성
    getEmissionFn := func() int64 {
        return emissionAccessor.GetStakerEmissionAmountPerSecond()
    }
    getHalvingBlocksInRangeFn := func(start, end int64) ([]int64, []int64) {
        return emissionAccessor.GetStakerEmissionAmountPerSecondInRange(start, end)
    }

    state := &sr.PoolTierState{
        Membership:              initializedPoolTier.membership,
        TierRatio:               initializedPoolTier.tierRatio,
        Counts:                  initializedPoolTier.counts,
        LastRewardCacheTimestamp: initializedPoolTier.lastRewardCacheTimestamp,
        LastRewardCacheHeight:   initializedPoolTier.lastRewardCacheHeight,
        CurrentEmission:         initializedPoolTier.currentEmission,
        GetEmission:             getEmissionFn,
        GetHalvingBlocksInRange: getHalvingBlocksInRangeFn,
    }
    err := stakerStore.SetPoolTierState(state)
    if err != nil {
        return err
    }
}
```

기존 8개 개별 key 초기화 코드(lines 138-200)는 **전체 삭제**한다.

### A-5. `r/gnoswap/staker/v1/init.gno` — 개별 key 초기화 코드 삭제

기존 `initStoreData()`의 PoolTier 관련 개별 key 초기화 코드를 **전체 삭제**한다:
- `HasPoolTierMembershipsStoreKey` ~ `SetPoolTierGetHalvingBlocksInRange` (lines 138-200)
- `HasPoolTierGetEmissionStoreKey` ~ `SetPoolTierGetHalvingBlocksInRange` (lines 180-200)

이들은 A-4의 통합 초기화(`HasPoolTierStateStoreKey` → `SetPoolTierState`)로 대체된다.
`initializePoolTier()` 함수 자체는 유지한다 (PoolTier 초기값 생성에 필요).

---

## 예상 효과

| 오퍼레이션 | getPoolTier 호출 수 | 절감 finalize (Get) | updatePoolTier 호출 수 | 절감 finalize (Set) | 합계 절감 |
|-----------|:---:|:---:|:---:|:---:|:---:|
| SetPoolTier | 1 | 7 | 1 | 7 | **14** |
| StakeToken | 2 | 14 | 1 | 7 | **21** |
| CollectReward | 1 | 7 | 1 | 7 | **14** |
| UnStakeToken | 1+CR | 7+CR | 1+CR | 7+CR | **14+CR** |

CR = CollectReward 내부 호출 포함

---

## 사이드 이펙트 및 주의사항

### 1. PoolTierState 직렬화 크기

8개 개별 값 → 1개 struct로 통합 시, 단일 object의 직렬화 크기가 커진다.
FinalizeRealmTransaction에서 해당 object를 저장할 때 1회의 KV Write 비용이 증가한다.

**영향**: 미미. 총 저장 데이터량은 동일하고, finalize 횟수 감소 효과가 훨씬 크다.

### 2. 함수 클로저 직렬화

`PoolTierState`에 `func() int64`과 `func(int64, int64) ([]int64, []int64)` 필드가 포함된다.
Gno VM은 함수 클로저를 object로 직렬화할 수 있다. 현재도 8개 개별 key 중 2개가
함수 타입이므로, 통합 struct에 포함해도 직렬화 동작에 차이 없다.

**단, 주의가 필요한 점**: 개별 key로 저장된 클로저와 struct 필드로 저장된 클로저의
직렬화가 동일한지는 Gno VM의 구현에 따른다. 특히 `PoolTierState` struct가 하나의
object로 직렬화될 때, 내부 function value 2개가 별도의 `FuncValue` object로 참조되는지
inline되는지에 따라 dirty marking과 ancestor propagation 동작이 달라질 수 있다.

**검증 (A 작업 완료 후 필수)**:
1. `getEmission()`, `getHalvingBlocksInRange()` 호출이 정상 동작하는지 확인
2. realm stats 로그에서 `PoolTierState` 관련 created/updated object 수가 예상과 일치하는지 확인
3. 클로저가 캡처한 외부 변수(emission 모듈 참조 등)가 직렬화/역직렬화 후에도 유효한지 확인
4. 통합 전후의 finalize 시 ancestor propagation 수를 비교하여 예상치 못한 증가가 없는지 확인

### 3. 부분 업데이트 불가

현재: `SetPoolTierCurrentEmission(val)` → 1개 key만 업데이트
통합 후: 반드시 전체 struct를 읽고 → 필드 수정 → 전체 struct를 다시 저장

**영향**: 현재 코드에서도 `getPoolTier()` → 수정 → `updatePoolTier()`로 전체를 읽고 쓰는
패턴이므로, 부분 업데이트 시나리오가 존재하지 않는다. 실질적 영향 없음.

단, `emissionCacheUpdateHook`에서도 동일 패턴을 따르는지 확인 필요:

```go
func (s *stakerV1) emissionCacheUpdateHook(emissionAmountPerSecond int64) {
    poolTier := s.getPoolTier()    // 전체 로드
    // ...
    s.updatePoolTier(poolTier)     // 전체 저장
}
```

→ 이미 전체 로드/저장 패턴이므로 문제 없음.

### 4. IStakerStore 인터페이스 변경

`IStakerStore`에서 기존 8개 PoolTier 개별 메서드를 **제거**하고 통합 메서드로 대체한다.
테스트넷 배포 전이므로 하위 호환성을 유지할 필요 없이 깔끔하게 전환한다.

삭제 대상 메서드 (인터페이스 + 구현체 모두):
- `HasPoolTierMembershipsStoreKey`, `GetPoolTierMemberships`, `SetPoolTierMemberships`
- `HasPoolTierRatioStoreKey`, `GetPoolTierRatio`, `SetPoolTierRatio`
- `HasPoolTierCountsStoreKey`, `GetPoolTierCounts`, `SetPoolTierCounts`
- `HasPoolTierLastRewardCacheTimestampStoreKey`, `GetPoolTierLastRewardCacheTimestamp`, `SetPoolTierLastRewardCacheTimestamp`
- `HasPoolTierLastRewardCacheHeightStoreKey`, `GetPoolTierLastRewardCacheHeight`, `SetPoolTierLastRewardCacheHeight`
- `HasPoolTierCurrentEmissionStoreKey`, `GetPoolTierCurrentEmission`, `SetPoolTierCurrentEmission`
- `HasPoolTierGetEmissionStoreKey`, `GetPoolTierGetEmission`, `SetPoolTierGetEmission`
- `HasPoolTierGetHalvingBlocksInRangeStoreKey`, `GetPoolTierGetHalvingBlocksInRange`, `SetPoolTierGetHalvingBlocksInRange`

추가 메서드:
- `HasPoolTierStateStoreKey`, `GetPoolTierState`, `SetPoolTierState`

### 5. getter.gno의 public getter 함수들

`r/gnoswap/staker/getter.gno`와 `r/gnoswap/staker/v1/getter.gno`에
개별 PoolTier 필드를 조회하는 public getter가 있을 수 있다:

```go
func GetPoolTier(poolPath string) uint64 { ... }
func GetPoolTierCount(tier uint64) uint64 { ... }
```

이들은 `s.getPoolTier().CurrentTier(poolPath)` 등을 호출하므로,
`getPoolTier()`가 통합 key를 사용하도록 변경되면 자동으로 호환된다.

단, getter 함수 중 `IStakerStore`의 개별 메서드를 **직접** 호출하는 것이 있다면
통합 메서드로 전환해야 한다. 개별 메서드를 삭제하므로 컴파일 시 누락을 발견할 수 있다.
