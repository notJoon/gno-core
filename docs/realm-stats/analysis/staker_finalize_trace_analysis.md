# Staker FinalizeRealmTransaction 트리거 분석

## 계측 방법

`gnovm/pkg/gnolang/op_call.go`의 `maybeFinalize`에 디버그 로깅을 추가하여,
각 FinalizeRealmTransaction 호출 시 다음 정보를 기록:

- `realm`: 현재 machine realm (`m.Realm.Path`)
- `func`: 반환 중인 함수 (`cfr.Func.PkgPath + "." + cfr.Func.Name`)
- `reason`: 트리거 이유 (`explicit_cross` / `implicit_cross` / `machine_exit`)
- `prev_realm`: 이전 프레임의 realm (`cfr.LastRealm.Path`)

테스트: `staker_storage_staker_lifecycle.txtar` (SetPoolTier → StakeToken → CollectReward ×2 → UnStakeToken)

## 오퍼레이션별 finalize 트리거 수

| Operation | Total triggers | explicit_cross | implicit_cross | Non-zero activity |
|-----------|:---:|:---:|:---:|:---:|
| SetPoolTier | 92 | 6 | 86 | 22 |
| StakeToken | 272 | 10 | 262 | 51 |
| CollectReward | 332 | 8 | 324 | 48 |
| UnStakeToken | 338 | 8 | 330 | 50 |

**implicit_cross가 전체의 96%를 차지한다.**

P1 문서에서 추정한 "staker finalize 23회"는 realm-stats 그룹 수였으며,
실제 FinalizeRealmTransaction 호출 횟수는 **272~338회**이다.

## 함수별 finalize 트리거 빈도 (StakeToken 기준)

| Function | Count | Type |
|----------|:---:|------|
| `p/gnoswap/store.Get` | 46 | KVStore 읽기 |
| `p/gnoswap/uint256.IsZero` | 16 | uint256 메서드 |
| `p/gnoswap/store.Set` | 16 | KVStore 쓰기 |
| `r/gnoswap/staker.ReverseIterate` | 14 | Deposit tree 순회 |
| `r/gnoswap/gns.getStartTimestamp` | 12 | GNS realm 접근 |
| `r/gnoswap/staker.Set` | 9 | Deposit 필드 설정 |
| `r/gnoswap/staker.Ticks` | 8 | Deposit accessor |
| `r/gnoswap/staker.StakedLiquidityGross` | 8 | 상태 accessor |
| `r/gnoswap/halt.IsOperationHalted` | 8 | halt 체크 |
| `p/nt/avl/v0.Set` | 8 | avl.Tree 쓰기 |
| `p/gnoswap/uint256.Lt` | 8 | uint256 비교 |

store.Get/Set만으로 **62회**(23%)를 차지한다.

## Realm별 트리거 분포 (StakeToken 기준)

| Realm | Triggers | Non-zero activity |
|-------|:---:|:---:|
| r/gnoswap/staker | 201 (74%) | 47 |
| r/gnoswap/common | 20 (7%) | 0 |
| r/gnoswap/position | 14 (5%) | 2 |
| r/gnoswap/gns | 13 (5%) | 0 |
| r/gnoswap/halt | 8 (3%) | 0 |
| r/gnoswap/referral | 4 | 0 |
| r/gnoswap/pool | 4 | 0 |
| r/gnoswap/gnft | 4 | 2 |
| r/gnoswap/staker/v1 | 2 | 0 |
| r/gnoswap/emission | 2 | 0 |

staker realm의 201 트리거 중 **154회(77%)가 no-op** (activity 없음).

## 근본 원인: Borrow-Realm 패턴에 의한 Finalize 증폭

### 메커니즘

Gno VM의 `isRealmBoundary` 판정 로직 (`op_call.go:226`):

```go
func (m *Machine) isRealmBoundary(cfr *Frame) bool {
    if cfr.WithCross {
        return true  // explicit cross → 항상 finalize
    } else if crlm != prlm {
        return true  // implicit cross → realm이 다르면 finalize
    }
}
```

implicit_cross는 `m.Realm != cfr.LastRealm`일 때 발생한다.

### 핵심: Versioned Proxy 패턴이 원인

staker는 versioned proxy 아키텍처를 사용한다:

```
r/gnoswap/staker      (proxy)  — KVStore, avl.Tree 등 상태 소유
r/gnoswap/staker/v1   (impl)   — 비즈니스 로직 실행, proxy의 데이터 접근
```

이 두 패키지는 **서로 다른 realm**이다. v1 코드가 proxy 소유의 데이터에
접근할 때마다 borrow-realm 메커니즘이 작동한다:

1. v1(impl realm)에서 `store.Get(key)` 호출
2. store.Get이 proxy realm 소유의 `map[string]any`에 접근
3. `m.Realm`이 proxy로 전환됨
4. store.Get 반환 시: `m.Realm(proxy) ≠ cfr.LastRealm(impl)` → **implicit_cross → finalize**

**결과: proxy 소유 데이터에 대한 N번의 패키지 함수 호출 → N번의 finalize**

### PoC 검증 결과

`poc_sticky_last_realm.txtar`로 동일 패턴을 재현:

| Scenario | 설명 | Finalize triggers |
|----------|------|:---:|
| A. DirectOwner | proxy가 직접 자기 Store 접근 (같은 realm) | **1** |
| B. ProxyToImpl | proxy → impl 위임, impl이 proxy의 Store 접근 | **8** (6 implicit + 2 explicit) |
| C. ProxyToImplWithCross | B + target cross-realm call 추가 | **9** (6 implicit + 3 explicit) |

동일한 6회의 store.Set/Get 호출이:
- 같은 realm에서 실행하면 finalize **0회** (Set은 dirty mark만, 최종 1회 finalize)
- 다른 realm에서 실행하면 finalize **6회** (매 호출마다 finalize)

**finalize 증폭은 cross-realm call(pool 호출)이 아니라 proxy-impl 간 데이터 접근이 원인이다.**

### staker에서의 실제 흐름

```
1. proxy(r/staker) → v1.StakeToken(impl realm)으로 위임
2. v1이 proxy 소유의 KVStore에 접근:
   store.Get()  → m.Realm=proxy(store 소유), LastRealm=impl → finalize! (×46)
   store.Set()  → m.Realm=proxy(store 소유), LastRealm=impl → finalize! (×16)
   avl.Get()    → m.Realm=proxy(tree 소유), LastRealm=impl → finalize! (×6)
   ...
3. v1이 pool realm 함수 호출 → explicit_cross → finalize (×3~5)
4. 합계: 272회
```

`prev_realm`이 `pool`로 보이는 것은 pool cross-realm call 이후의 호출에서
impl의 frame context가 pool을 참조하기 때문이다. 하지만 finalize 증폭의
**근본 원인은 proxy-impl 간 borrow-realm**이며, pool 호출은 추가적인
explicit_cross만 기여한다.

## Finalize 비용 구조

### No-op finalize의 비용

Activity가 없는 finalize도 다음을 실행:

```go
func (rlm *Realm) FinalizeRealmTransaction(store Store) {
    bm.PauseOpCode() / defer bm.ResumeOpCode()  // opcode 계측 전환
    store.LogFinalizeRealm(rlm.Path)              // opslog 기록
    rlm.processNewCreatedMarks(store, 0)          // 빈 슬라이스 순회
    rlm.processNewDeletedMarks(store)             // 빈 슬라이스 순회
    rlm.processNewEscapedMarks(store, 0)          // 빈 슬라이스 순회
    rlm.markDirtyAncestors(store)                 // 빈 슬라이스 순회
    rlm.saveUnsavedObjects(store)                 // 빈 슬라이스 순회
    rlm.saveNewEscaped(store)                     // 빈 슬라이스 순회
    rlm.removeDeletedObjects(store)               // 빈 슬라이스 순회
    rlm.clearMarks()                              // 슬라이스 nil 설정
}
```

빈 슬라이스 순회는 저렴하지만, 함수 호출 오버헤드 + opcode 계측 전환이
272~338회 반복되면 무시할 수 없다.

### Active finalize의 비용

store.Set 호출 시 MapValue가 dirty로 마킹되고:
- `markDirtyAncestors`: 소유 체인 전파
- `saveUnsavedObjects`: KV store 쓰기 (flat cost 2,000 gas/object)

CollectReward에서 store.Set 8회 → 각각 finalize → 8회의 독립적인
dirty propagation + KV Write 발생. 이들을 배치로 처리하면 propagation을
1회로 줄일 수 있다.

## 오퍼레이션별 상세 bytes 영향

| Operation | staker bytes | 기타 realm bytes |
|-----------|:---:|:---:|
| SetPoolTier | +48,854 | 0 |
| StakeToken | +54,442 | position +α, gnft +α |
| CollectReward | 0 | 0 |
| UnStakeToken | -14,352 | position -α, gnft +α |

## 최적화 방안

### 방안 1: VM 레벨 — read-only finalize skip

implicit_cross에서 dirty mark가 없으면 finalize를 건너뛰는 최적화.

```go
func (m *Machine) maybeFinalize(cfr *Frame) {
    if m.isRealmBoundary(cfr) {
        rlm := m.Realm
        // dirty mark가 없으면 skip
        if !cfr.WithCross &&
           len(rlm.newCreated) == 0 &&
           len(rlm.newDeleted) == 0 &&
           len(rlm.newEscaped) == 0 &&
           len(rlm.updated) == 0 {
            return
        }
        rlm.FinalizeRealmTransaction(m.Store)
    }
}
```

**예상 효과**: StakeToken 기준 ~154회(77%) no-op finalize 제거.
**위험**: dirty mark 생성 타이밍이 FinalizeRealmTransaction 내부에서도
발생할 수 있으므로 정확한 검증 필요.
**범위**: Gno VM 코어 수정 — 별도 PR, 광범위한 테스트 필요.

### 방안 2: Application 레벨 — KVStore 접근 패턴 최적화

#### 2-A. getPoolTier() 배치화

현재 `getPoolTier()`가 8개 store.Get을 개별 호출:

```go
func (s *stakerV1) getPoolTier() *poolTier {
    m := s.store.GetPoolTierMembership()    // store.Get #1
    r := s.store.GetPoolTierRatio()         // store.Get #2
    c := s.store.GetPoolTierCount()         // store.Get #3
    ...                                      // store.Get #4~#8
}
```

이를 하나의 구조체로 묶어 1회 store.Get으로 로드:

```go
type poolTierData struct {
    membership *avl.Tree
    ratio      *avl.Tree
    ...
}
// 1회 store.Get으로 전체 로드
```

**예상 효과**: SetPoolTier에서 22→~8 store.Get 감소.

#### 2-B. gns.getStartTimestamp 캐싱

CollectReward에서 `gns.getStartTimestamp`가 12~24회 호출된다.
트랜잭션 내에서 값이 변하지 않으므로 캐싱 가능:

```go
var cachedStartTimestamp int64
func getStartTimestampCached() int64 {
    if cachedStartTimestamp == 0 {
        cachedStartTimestamp = gns.GetStartTimestamp()
    }
    return cachedStartTimestamp
}
```

**예상 효과**: 12~24회 cross-realm call → 1회로 감소.

#### 2-C. Deposit accessor 통합

CollectReward에서 `staker.Owner()`, `staker.TargetPoolPath()`,
`staker.TickLower()`, `staker.TickUpper()` 등 개별 accessor가
각각 finalize를 트리거한다. 한 번에 Deposit 전체를 로드하면
finalize 횟수를 줄일 수 있다.

### 방안 3: Application 레벨 — cross-realm call 순서 조정

StakeToken에서 `poolAccessor.GetSlot0Tick()` 호출 위치를 함수 마지막으로
이동하면, 그 이전의 staker 내부 로직에서 implicit_cross가 발생하지 않는다.
다만 로직 의존성(tick 값이 이후 계산에 필요)으로 이동이 제한될 수 있다.

## PoC 검증: 컨트랙트 레벨 최적화

코어 수정 없이 컨트랙트만 변경하여 finalize 횟수를 줄일 수 있는 3가지 패턴을
`poc_contract_level_optimizations.txtar`로 검증하였다.

### PoC 구조

기존 `poc_sticky_last_realm.txtar`과 동일한 proxy-impl 아키텍처를 사용하되,
각 최적화 패턴의 baseline/optimized 쌍을 비교한다:

```
p/demo/pocstore  — Store 타입 + Record 타입 (p/gnoswap/store 모델)
r/demo/extdep    — 외부 realm 의존성 (r/gnoswap/gns 모델)
r/demo/worker    — impl realm (staker/v1 모델)
r/demo/owner     — proxy realm (r/gnoswap/staker 모델)
```

시나리오별로 worker(impl)가 owner(proxy) 소유 데이터에 접근하는 패턴을
baseline(현재)과 optimized(개선)로 나누어 실행한다.

### 측정 결과

#### 최적화 A: Key Consolidation (store key 통합)

`getPoolTier()`가 8개 store key를 개별 Get/Set하는 패턴을
1개 composite struct로 통합하는 최적화.

| Variant | Finalize triggers | implicit_cross | explicit_cross | Gas |
|---------|:---:|:---:|:---:|:---:|
| Baseline (6 Get + 6 Set) | **28** | 24 | 4 | 756,551 |
| Optimized (1 Get + 1 Set) | **8** | 4 | 4 | 718,956 |

- **Finalize 감소**: 28 → 8 (**-71%**, 20회 절감)
- **Gas 절감**: -37,595 (**-5.0%**)
- implicit_cross가 24 → 4로 감소. explicit_cross(proxy→worker 진입/퇴장)는 동일.
- 6회 store.Get에서 각각 트리거되던 implicit_cross finalize가 1회로 줄어듦.

#### 최적화 B: Cross-Realm Value Caching (교차 realm 값 캐싱)

`gns.getStartTimestamp()`를 반복 호출하는 대신, 1회 호출 후 로컬 변수로 재사용.

| Variant | Finalize triggers | implicit_cross | explicit_cross | Gas |
|---------|:---:|:---:|:---:|:---:|
| Baseline (6 cross-realm calls) | **16** | 0 | 16 | 509,965 |
| Optimized (1 call + reuse) | **6** | 0 | 6 | 499,843 |

- **Finalize 감소**: 16 → 6 (**-62%**, 10회 절감)
- **Gas 절감**: -10,122 (**-2.0%**)
- explicit_cross가 16 → 6으로 감소 (extdep 호출 12 → 2, 진입/퇴장 4 유지).

#### 최적화 C: Batch Accessor — Snapshot 패턴

Deposit의 개별 getter(`Owner()`, `PoolPath()`, `TickLower()`, ...)를
`Snapshot()` 1회 호출로 대체. Snapshot은 value type(RecordSnapshot)을 반환하므로
이후 필드 접근은 realm 경계를 넘지 않는다.

| Variant | Finalize triggers | implicit_cross | explicit_cross | Gas |
|---------|:---:|:---:|:---:|:---:|
| Baseline (6 getter calls) | **16** | 12 | 4 | 618,715 |
| Optimized (1 Snapshot call) | **6** | 2 | 4 | 611,983 |

- **Finalize 감소**: 16 → 6 (**-62%**, 10회 절감)
- **Gas 절감**: -6,732 (**-1.1%**)
- implicit_cross가 12 → 2로 감소. 6개 개별 getter 대신 Snapshot 1회.

#### 전체 조합 (Combined)

세 가지 최적화를 모두 적용한 결과:

| Variant | Finalize triggers | implicit_cross | explicit_cross | Gas |
|---------|:---:|:---:|:---:|:---:|
| Baseline (전체 scattered) | **52** | 36 | 16 | 863,453 |
| Optimized (전체 통합) | **12** | 6 | 6 | 808,477 |

- **Finalize 감소**: 52 → 12 (**-77%**, 40회 절감)
- **Gas 절감**: -54,976 (**-6.4%**)

### 실제 staker 적용 시 예상 효과

PoC는 각 패턴당 6회 호출로 테스트하였으나, 실제 staker의 호출 빈도는 훨씬 높다:

| 패턴 | PoC 호출 수 | 실제 staker 호출 수 (StakeToken) | 예상 finalize 절감 |
|------|:---:|:---:|:---:|
| Key Consolidation | 6 Get + 6 Set | 46 Get + 16 Set (PoolTier 8개 key 포함) | ~50회 |
| Cross-Realm Caching | 6 calls | 12~24 gns.getStartTimestamp | ~11~23회 |
| Batch Accessor | 6 getters | ~20+ Deposit/상태 accessor | ~15~18회 |

세 가지를 함께 적용하면 StakeToken 기준 **~76~91회 finalize 절감** 예상
(272회 → ~181~196회, **약 28~33% 감소**).

### 적용 시 사이드 이펙트 평가

#### A. Key Consolidation — 낮은 위험

**변경 내용**: PoolTier 관련 8개 store key(`PoolTierMemberships`, `PoolTierRatio`,
`PoolTierCounts`, ...)를 단일 `PoolTierState` 구조체로 통합.

**사이드 이펙트**:

1. **부분 업데이트 불가**: 현재는 `store.Set("ratio", newRatio)`로 1개 필드만
   변경 가능하지만, 통합 후에는 전체 struct를 읽고 → 필드 수정 → 전체 struct를
   다시 쓰는 read-modify-write 패턴이 필요하다.
   - **영향 범위**: PoolTier 관련 로직은 `getPoolTier()` / `updatePoolTier()`로
     이미 전체를 읽고 쓰는 패턴이므로 실질적 영향 없음.

2. **마이그레이션 필요**: 기존에 8개 key로 저장된 데이터를 1개 key의 struct로
   변환하는 마이그레이션 로직이 필요하다. 버전 업그레이드(v1 → v2) 시
   initializer에서 처리 가능.
   - **위험**: 마이그레이션 실패 시 PoolTier 데이터 유실. 테스트 커버리지 필수.

3. **직렬화 크기 증가 가능성**: 단일 object의 직렬화 크기가 커지면
   `saveUnsavedObjects`에서 쓰는 bytes가 증가할 수 있다. 다만 총 저장 데이터량은
   동일하므로 net bytes 변화는 미미.

4. **동시 접근 패턴**: PoolTier의 특정 필드만 읽는 함수가 있다면 불필요하게
   전체 struct를 로드하게 된다. 하지만 Gno는 단일 스레드 실행이므로
   동시성 이슈는 없음.

**평가**: PoolTier는 이미 함수 진입 시 전체를 로드하고 종료 시 전체를 저장하는
패턴이므로, 통합에 따른 로직 변경이 최소화된다. **적용 권장**.

#### B. Cross-Realm Value Caching — 중간 위험

**변경 내용**: `gns.GetStartTimestamp()` 등 트랜잭션 내 불변 값을 1회 호출 후
로컬 변수에 캐싱.

**사이드 이펙트**:

1. **Stale cache 위험**: Gno에서 realm 변수는 트랜잭션 성공 시 영속화된다.
   패키지 레벨 변수에 캐싱하면 다음 트랜잭션에서 stale value를 사용할 위험이 있다.
   - **완화책**: 로컬 변수로만 캐싱 (함수 스코프 내), 또는 proxy 진입점에서
     매 트랜잭션 시작 시 캐시 초기화.
   - **권장 패턴**: 패키지 레벨 변수 캐시를 사용하되, proxy의 각 public 함수
     진입 시 `resetCaches()`를 호출.

2. **값이 트랜잭션 중 변경될 수 있는 경우**: `getStartTimestamp`는 불변이지만,
   다른 cross-realm 값(예: pool tick, liquidity)은 같은 트랜잭션 내에서
   다른 오퍼레이션에 의해 변경될 수 있다. 이런 값을 잘못 캐싱하면 **논리 오류** 발생.
   - **완화책**: 캐싱 대상을 트랜잭션 내 불변이 보장되는 값으로 엄격히 제한.
     `getStartTimestamp`, `getHalvingInfo` 등 체인 파라미터 성격의 값만 캐싱.

3. **코드 복잡성 증가**: 캐시 초기화 누락 시 미묘한 버그 발생 가능.
   리뷰어가 "이 값은 캐싱해도 안전한가?"를 매번 판단해야 한다.

**평가**: `getStartTimestamp`처럼 명확히 불변인 값에 한정하면 안전하다.
다만 적용 범위를 명확히 문서화하고, 캐시 초기화 메커니즘을 반드시 포함해야 한다.
**제한적 적용 권장**.

#### C. Batch Accessor (Snapshot 패턴) — 높은 변경 비용

**변경 내용**: `Deposit`의 개별 getter(`Owner()`, `PoolPath()`, `TickLower()`,
`TickUpper()`, `Liquidity()`, `StakeTime()`)를 `Snapshot()` 1회 호출로 대체.

**사이드 이펙트**:

1. **API 표면 변경**: 기존에 `deposit.Owner()`로 접근하던 코드를 모두
   `snap := deposit.Snapshot(); snap.Owner`로 변경해야 한다.
   - **영향 범위**: staker/v1의 모든 Deposit 접근 코드. `CollectReward`,
     `UnStakeToken`, `StakeToken` 등 주요 오퍼레이션 전반에 걸침.
   - Deposit뿐 아니라 ExternalIncentive, Pool 등 다른 도메인 객체에도
     동일 패턴을 적용해야 일관성이 유지된다.

2. **Snapshot 시점 고정**: Snapshot()으로 복사한 뒤 원본 Deposit이 변경되면
   snapshot은 stale 상태가 된다. 읽기 전용 컨텍스트에서만 사용해야 하며,
   변경 후 재조회 패턴이 필요할 수 있다.
   - **위험**: snapshot으로 읽은 값을 기반으로 계산 → 원본 수정 → 다시 snapshot의
     값을 참조하는 경우 불일치 발생 가능.

3. **메모리 복사 오버헤드**: Snapshot은 value type으로 전체 필드를 복사한다.
   Deposit에 `*avl.Tree` 등 포인터 필드가 있으면 shallow copy가 되어
   원본과 snapshot이 내부 데이터를 공유하게 된다.
   - Deposit의 `collectedExternalRewards *avl.Tree` 등 mutable 포인터 필드는
     snapshot에 포함하지 않거나, snapshot을 immutable 필드만으로 제한해야 한다.

4. **유지보수 부담**: Deposit 구조체에 필드가 추가되면 RecordSnapshot에도
   반영해야 한다. 두 구조체 간 sync를 놓치면 컴파일 오류 없이 누락 발생 가능.

**평가**: finalize 절감 효과(6→1)는 확실하지만, 변경 범위가 넓고 Snapshot 관리의
복잡성이 높다. 다른 두 최적화를 먼저 적용한 후, 효과가 부족할 경우 점진적으로
도입하는 것이 현실적이다. **우선순위 낮음**.

### 적용 우선순위

| 순위 | 최적화 | 변경 비용 | 위험도 | Finalize 절감 | 권장 |
|:---:|--------|:---:|:---:|:---:|:---:|
| 1 | Key Consolidation | 낮음 | 낮음 | ~50회 | 즉시 적용 |
| 2 | Cross-Realm Caching | 낮음 | 중간 | ~11~23회 | 제한적 적용 |
| 3 | Batch Accessor | 높음 | 중간 | ~15~18회 | 점진적 도입 |

1순위 + 2순위만 적용해도 **~61~73회 finalize 절감** (272 → ~199~211, **22~27% 감소**)이
가능하며, 코드 변경 범위는 PoolTier struct 통합 + getStartTimestamp 캐싱으로 제한된다.

## 결론

staker의 과도한 finalize 횟수는 **versioned proxy 패턴**이 근본 원인이다:

1. **Borrow-Realm 증폭**: proxy가 소유한 데이터를 impl이 접근할 때마다 implicit_cross finalize 발생. store.Get/Set, avl.Get/Set, accessor 메서드 등 **모든 패키지 함수 호출이 1:1로 finalize를 트리거**한다.
2. **잦은 KVStore 접근**: 분산된 store.Get/Set 호출 (46~68회/operation) → 각각 finalize
3. **다수의 accessor 메서드**: Deposit 필드별 개별 접근으로 finalize 누적
4. **반복적 cross-realm 호출**: gns.getStartTimestamp 12~24회 반복 (추가적 explicit_cross)

이 중 #1은 VM 레벨 최적화(방안 1) 또는 proxy-impl 아키텍처 변경으로 해결 가능하며,
#2~#4는 Application 레벨 리팩토링(방안 2)으로 개선 가능하다.

컨트랙트 레벨 최적화 PoC(`poc_contract_level_optimizations.txtar`)에서
**3가지 패턴 조합 시 finalize 77% 감소 (52 → 12), gas 6.4% 절감**을 확인하였다.
실제 staker 적용 시 Key Consolidation + Cross-Realm Caching만으로도
**22~27% finalize 절감**이 가능하며, 코드 변경 범위를 최소화할 수 있다.

## 파일

- `/tmp/staker_finalize_trace.log` — 전체 finalize 트레이스 로그
- `gno.land/pkg/integration/testdata/poc_sticky_last_realm.txtar` — 이슈 검증 PoC
- `gno.land/pkg/integration/testdata/poc_contract_level_optimizations.txtar` — 컨트랙트 레벨 최적화 PoC
- 계측 코드: `gnovm/pkg/gnolang/op_call.go` (maybeFinalize), `realm_stats.go` (LogFinalizeTrigger)
