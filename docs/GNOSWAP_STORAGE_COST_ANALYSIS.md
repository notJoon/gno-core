# GnoSwap 스토리지 비용 분석 보고서

## 현황

| 작업 | 현재 비용 | 목표 비용 |
|------|----------|----------|
| Mint / Add Liquidity | 4~7 GNOT | < 1 GNOT |
| CollectFee / CollectReward | 4~7 GNOT | < 1 GNOT |
| Add Incentive | 6~7 GNOT | < 1 GNOT |

> 4 GNOT = 40,000 bytes 신규 스토리지 (100 ugnot/byte)

---

## 근본 원인 분석

### 원인 1: KVStore `map[string]any` 아키텍처 (Critical)

**파일:** `examples/gno.land/p/gnoswap/store/kv_store.gno:11-15`

```go
type kvStore struct {
    data              map[string]any           // ← 이것이 핵심 문제
    authorizedCallers map[address]Permission
    domainAddress     address
}
```

Gno에서 `map`은 단일 Object입니다. map 내부의 **어떤 값이든** 변경되면 **전체 map이 재직렬화**됩니다.

각 도메인의 KVStore가 하나의 map에 모든 데이터를 저장하고 있으므로:

| Domain | map 내 키 수 | Set() 호출마다 재직렬화되는 내용 |
|--------|------------|-------------------------------|
| pool | 8+ | pools tree, feeAmountTickSpacing, slot0FeeProtocol, ... |
| staker | 20+ | deposits, externalIncentives, pools, poolTier* × 8, ... |
| position | 2 | positions tree, positionNextID |

**비용 구조:** 키 하나를 수정해도 map의 모든 키 × (key_string + RefValue ~70 bytes)가 재직렬화됩니다. staker 도메인의 경우 20개 키 × ~110 bytes = **~2,200 bytes**의 map Object가 매번 재직렬화됩니다.

또한 `authorizedCallers map[address]Permission`도 별도 Object로 존재하여, 부모 kvStore struct가 dirty되면 이 map도 재직렬화됩니다.

---

### 원인 2: `emission.MintAndDistributeGns()` — 모든 작업에서 호출 (Critical)

**파일:** `examples/gno.land/r/gnoswap/emission/emission.gno:64-153`

**모든** 사용자 작업(Mint, CollectFee, CollectReward, AddIncentive)에서 호출되며, 한 번의 호출로 다음 realm들의 상태를 변경합니다:

#### GNS Realm 쓰기 (gns.gno)
```
1. privateLedger.Mint()     → GRC20 balances avl.Tree 수정 (O(log n) 노드)
2. setLastMintedTimestamp()  → package-level var
3. setMintedEmissionAmount() → package-level var
4. setLeftEmissionAmount()   → package-level var
5. calculateAmountToMint()   → HalvingData.accumAmount[], leftAmount[] 슬라이스 수정
```

#### Emission Realm 쓰기 (distribution.gno)
```
6.  setLeftGNSAmount(0)           → package-level var
7.  setLeftGNSAmount(remainder)   → package-level var (두 번째)
8.  setLastExecutedTimestamp()     → package-level var
9.  distributedToStaker += ...    → package-level var
10. accuDistributedToStaker += ...→ package-level var
11. distributedToDevOps += ...    → package-level var
12. accuDistributedToDevOps += ...→ package-level var
13. distributedToCommunityPool    → package-level var
14. accuDistributedToCommunityPool→ package-level var
15. distributedToGovStaker        → package-level var (pct=0이면 스킵)
16. accuDistributedToGovStaker    → package-level var
```

#### GNS Transfer 호출 (최대 4회)
```
17. gns.Transfer(staker, amount)       → balances tree: sender -=, receiver += (2 경로)
18. gns.Transfer(devops, amount)       → balances tree: 2 경로
19. gns.Transfer(communityPool, amount)→ balances tree: 2 경로
20. gns.Transfer(govStaker, amount)    → balances tree: 2 경로 (pct=0이면 스킵)
```

**결과:** MintAndDistributeGns 한 번 호출로:
- **GNS realm**: 3 package-level vars + HalvingData(7 slices) + GRC20 balance tree × 최대 9 경로 수정
- **Emission realm**: 10+ package-level vars 수정
- 총 **~50+ Objects** 수정

#### HalvingData의 숨겨진 비용

**파일:** `examples/gno.land/r/gnoswap/gns/halving.gno:5-13`

```go
type HalvingData struct {
    startTimestamps []int64  // 12개 원소
    endTimestamps   []int64  // 12개 원소
    maxAmount       []int64  // 12개 원소
    mintedAmount    []int64  // 12개 원소
    leftAmount      []int64  // ← 매번 수정됨
    accumAmount     []int64  // ← 매번 수정됨
    amountPerSecond []int64  // 12개 원소
}
```

- `*HalvingData`는 포인터 → 별도 Object
- 7개 슬라이스 각각이 별도 Object (7 × 12 × 8 bytes = 672 bytes + 7 × Object overhead)
- `accumAmount`와 `leftAmount`만 수정해도 **7개 슬라이스 모두 재직렬화** (dirty ancestor propagation)

---

### 원인 3: Pool 구조체의 `*u256.Uint` 포인터 필드 (High)

**파일:** `examples/gno.land/r/gnoswap/pool/pool.gno:15-33`

Pool 구조체에 **9개의 `*u256.Uint` 포인터 필드**가 있습니다:

```go
type Pool struct {
    // ...
    slot0.sqrtPriceX96     *u256.Uint  // Object #1
    balances.token0        *u256.Uint  // Object #2
    balances.token1        *u256.Uint  // Object #3
    protocolFees.token0    *u256.Uint  // Object #4
    protocolFees.token1    *u256.Uint  // Object #5
    maxLiquidityPerTick    *u256.Uint  // Object #6
    feeGrowthGlobal0X128   *u256.Uint  // Object #7
    feeGrowthGlobal1X128   *u256.Uint  // Object #8
    liquidity              *u256.Uint  // Object #9
}
```

각 포인터가 별도 Object = **9 × 2,000 gas flat cost** per pool write.

또한 TickInfo에도 5개 포인터 (`*u256.Uint` × 4, `*i256.Int` × 1), PositionInfo에 5개 포인터가 있습니다. Mint 시 2개 tick + 1개 position = 추가 **15 Objects**.

---

### 원인 4: `ObservationState`의 `map[uint16]*Observation` (High)

**파일:** `examples/gno.land/r/gnoswap/pool/oracle.gno:8-13`

```go
type ObservationState struct {
    observations    map[uint16]*Observation  // map + pointer = 이중 비용
    index           uint16
    cardinality     uint16
    cardinalityNext uint16
}
```

- `map[uint16]*Observation`은 단일 Object → 모든 관찰 기록의 key/RefValue 포함
- 각 `*Observation`은 별도 Object
- 각 `Observation`에 `*u256.Uint` × 2 = 추가 2 Objects per observation
- Cardinality가 증가하면 map Object도 선형으로 증가
- **Cardinality N일 때**: map Object ~N × 70 bytes + N개 Observation Objects

---

### 원인 5: 다중 realm dirty 전파 (High)

단일 Mint 작업이 dirty하는 realm 목록:

| # | Realm | 원인 | 예상 Block 크기 |
|---|-------|------|---------------|
| 1 | pool | savePool() | 대형 (8+ kvStore entries) |
| 2 | position | SetPosition, SetNextID | 중형 (2 kvStore entries) |
| 3 | gns | MintGns + 4 Transfers | 대형 (token, privateLedger, emissionState, 3+ vars) |
| 4 | emission | MintAndDistributeGns | 대형 (15+ vars, distributionBpsPct map) |
| 5 | gnft | NFT Mint | 중형 |
| 6 | token0 | GRC20 TransferFrom | 중형 |
| 7 | token1 | GRC20 TransferFrom | 중형 |
| 8 | referral | TryRegister | 소형 |

**각 realm이 dirty되면** 해당 realm의 `PackageValue → Block → 모든 package-level var의 RefValues`가 재직렬화됩니다. 8개 realm의 Block 재직렬화 오버헤드만으로도 상당한 비용이 발생합니다.

---

## 작업별 콜 스택 및 Object 쓰기 분석

### Mint / Add Liquidity

```
position.Mint()
├─ emission.MintAndDistributeGns(cross)     ← ~50 Objects (원인 2)
│  ├─ gns.MintGns()                          (GNS realm dirty)
│  └─ gns.Transfer() × 3-4                  (GNS realm 추가 dirty)
│
├─ pool.Mint()                               ← ~30 Objects (원인 3,4)
│  ├─ modifyPosition()
│  │  ├─ tickUpdate() × 2                    (2 × TickInfo: 5 *u256.Uint each)
│  │  ├─ tickBitmapFlipTick() × 0-2          (*u256.Uint per bitmap word)
│  │  ├─ positionUpdateWithKey()             (PositionInfo: 5 *u256.Uint)
│  │  └─ writeObservation()                  (Observation: 2 *u256.Uint)
│  └─ savePool()                             (kvStore.Set → map dirty)
│
├─ gnft.Mint()                               ← ~5 Objects
│
├─ position.store.SetPosition()              ← ~10 Objects
│  └─ kvStore.Set("positions", tree)         (kvStore map dirty)
│
├─ position.store.SetPositionNextID()        ← kvStore map 이미 dirty
│
├─ token0.TransferFrom()                     ← ~5 Objects
└─ token1.TransferFrom()                     ← ~5 Objects

총 Object 쓰기 추정: ~100-110 Objects
```

### CollectFee

```
position.CollectFee()
├─ emission.MintAndDistributeGns(cross)     ← ~50 Objects
│
├─ pool.Burn()                               ← ~25 Objects
│  ├─ modifyPosition()
│  └─ savePool()                             ① pool kvStore map dirty
│
├─ pool.Collect()                            ← ~15 Objects
│  └─ savePool()                             ② pool kvStore map 이미 dirty (중복)
│
├─ position.mustUpdatePosition()             ← ~10 Objects
│  └─ kvStore.Set("positions", tree)
│
├─ protocolFee.AddToProtocolFee() × 2        ← ~5 Objects
│  └─ kvStore.Set("tokenListWithAmount:...")  × 2
│
├─ token0.TransferFrom() × 2                ← ~10 Objects (user + protocol fee)
└─ token1.TransferFrom() × 2                ← ~10 Objects

총 Object 쓰기 추정: ~120-130 Objects
```

### CollectReward

```
staker.CollectReward()
├─ emission.MintAndDistributeGns(cross)     ← ~50 Objects
│
├─ calcPositionReward()                      (계산만, 쓰기 없음)
│
├─ LOOP: external incentive별
│  ├─ incentive.SetRewardAmount()            (in-memory)
│  ├─ externalIncentives.set(id, incentive)  (in-memory tree update)
│  ├─ token.Transfer() × N                  ← 인센티브 수 × ~5 Objects
│  └─ deposit 업데이트                        (in-memory)
│
├─ staker.store.SetTotalEmissionSent()       ← staker kvStore map dirty (20+ entries!)
│
├─ gns.Transfer() × 1-3                     ← ~15 Objects
│  (toUser + penalty + unclaimable)
│
└─ deposits.set(positionId, deposit)         (in-memory - 그러나 kvStore map 이미 dirty)

총 Object 쓰기 추정: ~90-110 Objects
(외부 인센티브 수에 따라 증가)
```

### Add Incentive

```
staker.CreateExternalIncentive()
├─ emission.MintAndDistributeGns(cross)     ← ~50 Objects
│
├─ gns.TransferFrom(deposit)                ← ~5 Objects (GNS 보증금)
├─ token.TransferFrom(reward)               ← ~5 Objects (보상 토큰)
│
├─ pools.set(poolPath, pool)                 staker kvStore map dirty (20+ entries)
├─ externalIncentives.Set(id, incentive)     kvStore map 이미 dirty
├─ externalIncentivesByCreationTime.Set()    kvStore map 이미 dirty
│
├─ staker.store.SetExternalIncentivesByCreationTime()  ← ~10 Objects
│
└─ store.NextIncentiveID()                   ← counter update

총 Object 쓰기 추정: ~80-90 Objects
```

---

## 데이터 누적에 따른 비용 증가 요인

| 요인 | 영향 | 증가 패턴 |
|------|------|----------|
| pool 내 positions 수 | position tree 경로 길이 증가 | O(log n) — 완만 |
| pool 내 tick 수 | tick tree 경로 길이 증가 | O(log n) — 완만 |
| ObservationState cardinality | map Object 크기 선형 증가 | **O(n) — 위험** |
| staker deposits 수 | deposits tree 경로 증가 | O(log n) — 완만 |
| external incentives 수 | externalIncentives tree 증가 | O(log n) — 완만 |
| GNS 계정 수 | balances tree 경로 증가 | O(log n) — 완만 |
| **KVStore map 크기** | **map entry 추가 시 전체 재직렬화** | **O(n) — 위험** |

주요 위험:
- `ObservationState.observations` map은 cardinality 증가 시 비용이 선형으로 증가
- KVStore map에 새 키가 추가되면 (예: 새로운 store key 도입) 모든 작업의 비용이 영구 증가

---

## 최적화 권장사항

### Phase 1: 즉각적인 고영향 변경 (KVStore 아키텍처)

#### 1-1. KVStore `map[string]any` → `avl.Tree` 전환

**영향:** 모든 작업에서 즉각적인 비용 감소
**난이도:** Medium (인터페이스는 유지, 내부 구현만 변경)

```go
// BEFORE: map dirty = 전체 재직렬화
type kvStore struct {
    data              map[string]any
    authorizedCallers map[address]Permission
    domainAddress     address
}

// AFTER: avl.Tree dirty = O(log n) 경로만 재직렬화
type kvStore struct {
    data              *avl.Tree  // string key → any
    authorizedCallers *avl.Tree  // address → Permission
    domainAddress     address
}
```

**절감 효과:**
- Staker realm (20+ entries): map 재직렬화 ~2,200 bytes → avl.Tree 경로 ~400 bytes (**80% 감소**)
- Pool realm (8+ entries): map 재직렬화 ~880 bytes → avl.Tree 경로 ~300 bytes (**65% 감소**)
- 이 변경은 모든 도메인에 영향을 미치므로, 모든 작업의 비용이 감소합니다

#### 1-2. Emission 분배 일괄 처리

**영향:** 모든 작업에서 GNS Transfer 횟수 감소
**난이도:** Medium

```go
// BEFORE: 4개 대상에 개별 Transfer (4회 balance tree 수정)
for target, pct := range distributionBpsPct {
    gns.Transfer(cross, targetAddr, distAmount)
}

// AFTER: 단일 계정에 축적, 필요 시 인출
// Option A: 축적만 하고 실제 전송은 Claim 시
func distributeToTarget(amount int64) (int64, error) {
    for target, pct := range distributionBpsPct {
        distAmount := calculateAmount(amount, pct)
        pendingDistributions[target] += distAmount  // 메모리에만 기록
    }
    // gns.Transfer는 하지 않음 → GNS realm dirty 방지
}

// 각 대상이 ClaimDistribution() 호출 시 실제 전송
func ClaimDistribution(cur realm, target int) {
    amount := pendingDistributions[target]
    pendingDistributions[target] = 0
    gns.Transfer(cross, callerAddr, amount)
}
```

**절감 효과:**
- 매 작업에서 3-4회의 GNS Transfer 제거
- GNS realm의 balance tree 수정 횟수: 4 경로 → 1 경로 (MintGns만)
- 추정 **~30 Objects/작업 감소**

### Phase 2: 포인터 → 값 타입 전환

#### 2-1. Pool의 `*u256.Uint` → 값 타입

**영향:** Pool 수정 시 Object 수 감소
**난이도:** High (u256 패키지 변경 필요)

```go
// BEFORE: 9 포인터 = 9 Objects + Pool Object = 10 Objects
type Pool struct {
    liquidity            *u256.Uint  // Object: 32 bytes data + ~70 bytes RefValue
    feeGrowthGlobal0X128 *u256.Uint  // Object: 32 bytes data + ~70 bytes RefValue
    // ... 7개 더
}

// AFTER: 0 포인터 = 1 Object
type Pool struct {
    liquidity            u256.Uint  // inline: 32 bytes
    feeGrowthGlobal0X128 u256.Uint  // inline: 32 bytes
    // ...
}
```

**절감:**
- Pool 당: 9 × (2,000 gas flat + ~100 bytes overhead) = **18,000 gas + ~900 bytes 감소**
- TickInfo 당: 5 포인터 → 값 = **10,000 gas + ~500 bytes 감소**
- PositionInfo 당: 5 포인터 → 값 = **10,000 gas + ~500 bytes 감소**

> **주의:** `u256.Uint`는 현재 `[4]uint64` = 32 bytes. 값 타입으로 바꾸려면 할당 패턴(`u256.Zero()` 등)을 모두 수정해야 합니다. u256 패키지가 값 의미론을 지원하는지 확인 필요.

#### 2-2. ObservationState `map` → 고정 배열

**영향:** Observation 쓰기 비용 감소 + 누적 비용 증가 방지
**난이도:** Medium

```go
// BEFORE: map + pointer = O(cardinality) 재직렬화 + cardinality개 Objects
type ObservationState struct {
    observations map[uint16]*Observation
}

// AFTER: 고정 배열 + 값 타입 = 1 Object
type ObservationState struct {
    observations [MAX_CARDINALITY]Observation  // 값 타입, inline
}

// Observation도 값 타입으로
type Observation struct {
    blockTimestamp                    int64
    tickCumulative                    int64
    liquidityCumulative               u256.Uint  // 값 타입
    secondsPerLiquidityCumulativeX128 u256.Uint  // 값 타입
    initialized                       bool
}
```

**절감:**
- map Object 제거: cardinality × ~70 bytes 절약
- Observation Objects 제거: cardinality × (2,000 gas flat + ~200 bytes)
- **cardinality 100**: ~200,000 gas + ~20,000 bytes 절약

### Phase 3: HalvingData 최적화

#### 3-1. HalvingData 7 슬라이스 → 구조체 배열

**영향:** MintGns 호출 시 Object 수 감소
**난이도:** Low-Medium

```go
// BEFORE: 7 슬라이스 = 7 Objects + 1 struct Object = 8 Objects
type HalvingData struct {
    startTimestamps []int64  // Object
    endTimestamps   []int64  // Object
    maxAmount       []int64  // Object
    mintedAmount    []int64  // Object
    leftAmount      []int64  // Object (매번 수정)
    accumAmount     []int64  // Object (매번 수정)
    amountPerSecond []int64  // Object
}

// AFTER: 1 배열 = 1 Object + 1 struct Object = 2 Objects
type HalvingYear struct {
    StartTimestamp int64
    EndTimestamp   int64
    MaxAmount      int64
    MintedAmount   int64
    LeftAmount     int64
    AccumAmount    int64
    AmountPerSec   int64
}

type HalvingData struct {
    years [12]HalvingYear  // 12 × 56 bytes = 672 bytes, 단일 Object
}
```

**절감:**
- 7 slice Objects → 0: **7 × 2,000 = 14,000 gas 감소**
- 직렬화 크기도 감소 (슬라이스 헤더 × 7 제거)

### Phase 4: Emission 호출 최적화

#### 4-1. Lazy Emission 패턴

**영향:** 모든 작업에서 emission 오버헤드 제거
**난이도:** High (프로토콜 설계 변경)

현재: 모든 사용자 작업 → MintAndDistributeGns → ~50 Objects 쓰기

제안: Emission을 독립적인 cron 작업으로 분리하거나, 일정 시간 간격으로만 실행

```go
// BEFORE: 매 작업마다 emission 실행
func (p *positionV1) Mint(...) {
    emission.MintAndDistributeGns(cross)  // 매번 실행
    // ...
}

// AFTER: N초 이상 경과했을 때만 실행
func (p *positionV1) Mint(...) {
    emission.MintAndDistributeGnsIfNeeded(cross, MIN_INTERVAL)
    // ...
}

// 또는: emission을 제거하고 별도 트랜잭션으로 분리
func (p *positionV1) Mint(...) {
    // emission 호출 없음 — 별도 MintAndDistribute 트랜잭션으로 처리
    // ...
}
```

**절감:** 최대 **~50 Objects/작업**, 즉 비용의 **~40-50%** 감소

---

## 비용 절감 추정 요약

| 최적화 | 난이도 | 예상 Object 감소 | 예상 비용 절감 |
|--------|--------|----------------|--------------|
| KVStore map → avl.Tree | Medium | 모든 작업에서 ~5-10 | 10-15% |
| Emission 분배 일괄처리 | Medium | ~30/작업 | 25-30% |
| Pool *u256.Uint → 값 타입 | High | ~24/pool 쓰기 | 15-20% |
| ObservationState map → 배열 | Medium | ~cardinality/pool | 5-15% |
| HalvingData 슬라이스 통합 | Low | ~7/emission | 5-8% |
| Lazy Emission | High | ~50/작업 | 40-50% |

**Phase 1 (KVStore + Emission 일괄처리)만 적용해도 ~35-45% 절감이 가능합니다.**

**Phase 1 + Phase 2 + Phase 3 적용 시 ~60-70% 절감을 기대할 수 있습니다.**

현재 4-7 GNOT → **Phase 1만 적용: ~2-4 GNOT**, **Phase 1+2+3: ~1.5-2.5 GNOT**, **전체 적용: < 1 GNOT**

---

## 부록: 데이터 누적 시 비용 증가 시나리오

### 1년 운영 시 예상 데이터 규모

| 데이터 | 규모 | 작업별 추가 비용 |
|--------|------|---------------|
| 총 풀 수 | ~50-100개 | pools tree 경로 +1-2 노드 (미미함) |
| 풀 당 position 수 | ~100-1,000개 | position tree O(log n) — 완만 |
| 풀 당 tick 수 | ~50-500개 | tick tree O(log n) — 완만 |
| Observation cardinality | 최대 65,535 | **map 재직렬화 O(n) — 위험** |
| GNS 계정 수 | ~10,000-100,000 | balances tree O(log n) — 완만 |
| External incentives | ~100-1,000 | tree O(log n) — 완만 |

**최대 위험: ObservationState의 map 크기.** Cardinality가 1,000까지 증가하면:
- map Object: ~70,000 bytes (1,000 entries × 70 bytes/entry)
- Observation Objects: 1,000 × ~200 bytes = 200,000 bytes
- **매 swap마다 1개 observation 수정 → 70,000 bytes map 재직렬화**

이 문제는 Phase 2-2 (고정 배열 전환)로 해결 가능합니다.
