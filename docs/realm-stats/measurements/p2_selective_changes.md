# P2 측정 결과: 선별 적용 + P3 Halving 캐시 최적화

> 측정일: 2026-03-11 (P2), 2026-03-12 (P3)
> 브랜치: `perf/object-dirty-log`
> 테스트: `staker_storage_staker_lifecycle.txtar`
> 선행 조건: P1 (Task A + Task B) 적용 상태에서 DepositView 실험 후 회귀 확인,
> 감소 효과가 있는 변경만 선별 적용 (P2), 이후 Halving 캐시 최적화 추가 (P3)

---

## 1. Finalize 트리거 수 비교

### 오퍼레이션별 요약

| Operation | P1 (Task A+B) | P2 (선별) | P3 (Halving 캐시) | P2→P3 |
|-----------|---------------|-----------|-------------------|-------|
| **SetPoolTier** | 30 | 32 (avg) | 47 (avg) | **+15** |
| **StakeToken** | 122 | 97 | 97 | 0 |
| **CollectReward** | 65 | 65 (3/4), 125 (이상치) | 65 (3/4), **112** (이상치) | **-13** |
| **UnStakeToken** | 142 | 141 | 141 | 0 |

> P3에서 CollectReward 이상치가 125 → 112로 **-13 감소**. gns realm 접근(getStartTimestamp 12회 +
> getHalvingData 1회)이 halving 경계 캐싱으로 완전 제거됨.
>
> SetPoolTier는 P2 대비 +15 증가. P3 초기화 로직에서 `findNextHalvingTimestamp`가
> `getHalvingBlocksInRange`를 1회 호출하면서 추가 finalize가 발생한 것으로 추정.

### 호출별 상세

| Operation | 호출 | P1 | P2 | P3 | P2→P3 |
|-----------|------|-----|-----|-----|-------|
| SetPoolTier | 1차 | 30 | 30 | 45 | +15 |
| SetPoolTier | 2차 | 30 | 34 | 49 | +15 |
| StakeToken | 1차 | 122 | 97 | 97 | 0 |
| StakeToken | 2차 | 122 | 97 | 97 | 0 |
| CollectReward | 1차 | 65 | 65 | 65 | 0 |
| CollectReward | 2차 | 65 | **125** | **112** | **-13** |
| CollectReward | 3차 | 65 | 65 | 65 | 0 |
| CollectReward | 4차 | 65 | 65 | 65 | 0 |
| UnStakeToken | 1차 | 142 | 141 | 141 | 0 |
| UnStakeToken | 2차 | 142 | 141 | 141 | 0 |

---

## 2. Baseline 대비 누적 감소율

| Operation | Baseline | P1 후 | P2 후 | P3 후 | 전체 감소율 |
|-----------|----------|-------|-------|-------|------------|
| **StakeToken** | 272 | 122 | 97 | **97** | **-64.3%** |
| **CollectReward** | 332 | 65 | 65 | **65** | **-80.4%** |
| **CollectReward (이상치)** | 332 | 65 | 125 | **112** | **-66.3%** |
| **UnStakeToken** | 338 | 142 | 141 | **141** | **-58.3%** |

---

## 3. Finalize 트리거 분류

### Reason별 분류 (전체 테스트)

| Reason | P1 | P2 | 변화 |
|--------|-----|-----|------|
| `implicit_cross` | 973 | 961 | -12 |
| `explicit_cross` | 98 | 98 | 0 |
| **합계** | **1,071** | **1,059** | **-12** |

### 오퍼레이션별 Reason 분류

| Operation | implicit_cross | explicit_cross | 합계 |
|-----------|---------------|---------------|------|
| SetPoolTier | 28 | 4 | 32 |
| StakeToken | 92 | 5 | 97 |
| CollectReward (정상) | 63 | 2 | 65 |
| CollectReward (이상치) | 123 | 2 | 125 |
| UnStakeToken | 137 | 4 | 141 |

---

## 4. Realm별 Finalize 분포

### StakeToken (P1 avg 122 → P2 avg 97)

| Realm | P1 avg | P2 avg | 변화 |
|-------|--------|--------|------|
| `staker` | 78 | 66 | **-12** |
| `common` | 10 | 10 | 0 |
| `position` | 9 | 9 | 0 |
| `halt` | 4 | 4 | 0 |
| `gnft` | 2 | 2 | 0 |
| `referral` | — | 2 | — |
| `pool` | — | 2 | — |
| `emission` | — | 1 | — |

> StakeToken의 주요 감소는 `staker` realm 내부에서 발생 (-12).
> 나머지 -13은 P1에서 집계되지 않던 realm(referral, pool, emission)의
> 계측 범위 차이로, 실제 finalize 수 감소는 staker 내부 접근 최적화에 의한 것이다.

### CollectReward — 정상 호출 vs 이상치

| Realm | CR 정상 (65) | CR 이상치 (125) | 차이 |
|-------|-------------|----------------|------|
| `staker` | 60 | 104 | +44 |
| `gns` | 0 | 13 | +13 |
| `v1` | 0 | 3 | +3 |
| `halt` | 3 | 3 | 0 |
| `emission` | 1 | 1 | 0 |
| `pool` | 1 | 1 | 0 |

### UnStakeToken (P1 avg 142 → P2 avg 141)

| Realm | P1 avg | P2 avg | 변화 |
|-------|--------|--------|------|
| `staker` | 116 | 115 | -1 |
| `common` | 10 | 10 | 0 |
| `position` | 6 | 6 | 0 |
| `halt` | 5 | 5 | 0 |
| `pool` | — | 2 | — |
| `gnft` | — | 2 | — |
| `emission` | — | 1 | — |

---

## 5. GAS 사용량 비교

| Operation | P1 GAS | P2 GAS | P3 GAS | P2→P3 |
|-----------|--------|--------|--------|-------|
| SetPoolTier | 40,824,776 | 40,833,086 | 40,973,569 | +140,483 |
| StakeToken | 61,688,264 | 53,809,477 | 53,834,300 | +24,823 |
| CollectReward (이상치) | 34,464,844 | 48,120,811 | **46,384,424** | **-1,736,387** |
| CollectReward (정상) | 34,464,844 | 35,414,387 | 35,439,210 | +24,823 |
| UnStakeToken | 57,092,433 | 57,580,912 | 57,605,735 | +24,823 |

> P3에서 CollectReward 이상치의 GAS가 48.1M → 46.4M (-1.7M, **-3.6%**) 감소.
> gns realm의 halving 루프 제거에 의한 효과이다.
>
> 나머지 오퍼레이션은 P3 초기화 코드 추가로 인해 미미한 증가(~25K).

### Storage Delta

| Operation | P1 | P2 | P3 | P2→P3 |
|-----------|-----|-----|-----|-------|
| SetPoolTier | +24,427 bytes | +24,427 bytes | +24,427 bytes | 0 |
| StakeToken | +33,099 bytes | +23,447 bytes | +23,447 bytes | 0 |
| CollectReward (이상치) | 0 bytes | +5,758 bytes | +5,758 bytes | 0 |
| CollectReward (정상) | 0 bytes | 0 bytes | 0 bytes | 0 |
| UnStakeToken | -7,198 bytes | -3,304 bytes | -3,304 bytes | 0 |

> Storage delta는 P2와 P3 사이에 변화 없음. P3 최적화는 순수 연산 비용 절감이며
> 저장 패턴에는 영향을 주지 않는다.

---

## 6. CollectReward 2차 호출 이상치 분석

### 6.1 P2 시점 이상치 (125 triggers)

P1에서 모든 CollectReward 호출이 일관되게 65였으나, P2 선별 적용 후
2차 호출에서만 125로 급증했다. 1·3·4차와의 차이를 func 단위로 분석한 결과:

| Realm | Function | 정상 호출 | P2 이상치 | 차이 |
|-------|----------|---------|----------|------|
| `gns` | `getStartTimestamp` | 0 | 12 | **+12** |
| `staker` | `ReverseIterate` | 6 | 16 | **+10** |
| `staker` | `TickLower` | 1 | 5 | +4 |
| `staker` | `Get` (tree) | 2 | 6 | +4 |
| `staker` | `Ticks` | 2 | 6 | +4 |
| `staker` | `OutsideAccumulation` | 2 | 6 | +4 |
| `staker` | `TickUpper` | 1 | 5 | +4 |
| `staker` | `GlobalRewardRatioAccumulation` | 1 | 3 | +2 |
| `staker` | `StakedLiquidity` | 2 | 4 | +2 |
| `staker` | `HistoricalTick` | 0 | 2 | +2 |
| `staker` | `Clone` (u256) | 0 | 2 | +2 |
| `v1` | `IsZero` (u256) | 0 | 2 | +2 |
| `gns` | `getHalvingData` | 0 | 1 | +1 |
| 기타 | (각 1회 증가) | — | — | +7 |

이상치의 +60은 크게 두 구간으로 나뉜다:

| 구간 | 원인 | finalize 수 |
|------|------|------------|
| **A. gns halving 루프** | `getHalvingBlocksInRange` → 12년 순회 | **+13** |
| **B. cacheReward pool 순회** | `applyCacheToAllPools` + 후속 reward 계산 | **+47** |

### 6.2 P3 Halving 캐시 적용 후 이상치 (112 triggers)

P3에서 `nextHalvingTimestamp` 캐싱을 적용하여 구간 A를 제거:

| 구간 | P2 이상치 | P3 이상치 | 변화 |
|------|----------|----------|------|
| **A. gns halving 루프** | +13 | **0** | **-13 (완전 제거)** |
| **B. cacheReward pool 순회** | +47 | +47 | 0 (변화 없음) |
| **합계** | 125 | **112** | **-13** |

P3의 `nextHalvingTimestamp` 캐싱은 halving 경계를 넘지 않는 경우
`getHalvingBlocksInRange` 호출 자체를 건너뛰므로, gns realm 접근
(getStartTimestamp ×12 + getHalvingData ×1)이 완전히 제거되었다.

### 6.3 잔여 이상치 원인 (구간 B, +47)

P3 이후에도 남은 +47 finalize는 block height 변경 시 `cacheReward`가
`applyCacheToAllPools`를 실행하면서 발생하는 pool 데이터 접근이다:

- `ReverseIterate` +10: pool의 StakedLiquidity/RewardCache UintTree 순회
- `Tick*` +16: TickLower, TickUpper, Ticks, OutsideAccumulation 접근
- `GlobalRewardRatioAccumulation` +2, `StakedLiquidity` +2, `HistoricalTick` +2
- `Clone/IsZero` (u256) +4: 보상 계산 중 값 연산

이 접근은 reward cache 갱신에 본질적으로 필요한 비용이며, 컨트랙트 레벨에서는 추가 최적화가 어렵다.

### 6.4 이상치 발생 조건

이상치는 **이전 호출과 다른 block height에서 실행되는 첫 번째 CollectReward**에서
발생한다. `cacheReward`의 early return 조건(`currentTimestamp <= lastTimestamp`)을
통과하여 reward cache를 갱신하기 때문이다.

```
동일 블록 내 호출:  cacheReward → early return (0 추가 finalize)
새 블록 첫 호출:    cacheReward → applyCacheToAllPools (+47 finalize)
```

실제 프로덕션에서는 대부분의 사용자 트랜잭션이 서로 다른 블록에서 실행되므로,
이 비용은 1차 호출(65)이 아닌 이상치(112)가 더 현실적인 수치일 수 있다.

---

## 7. 종합 평가

### 유효한 개선

| 항목 | 수치 | 비고 |
|------|------|------|
| StakeToken finalize | 122 → 97 (-25, **-20.5%**) | staker 내부 접근 최적화 |
| StakeToken GAS | 61.7M → 53.8M (-7.9M, **-12.8%**) | finalize 감소에 비례 |
| StakeToken storage | +33,099 → +23,447 bytes (-9,652) | 비용 재분배 효과 |

### 미변화 / 회귀

| 항목 | 수치 | 비고 |
|------|------|------|
| CollectReward (정상) | 65 → 65 (0%) | 변화 없음 |
| CollectReward (이상치) | 65 → 125 (+60) | 2차 호출에서만 발생, 원인 조사 필요 |
| UnStakeToken | 142 → 141 (-1) | 실질 변화 없음 |

### Baseline 대비 종합 누적

| Operation | Baseline | P1 후 | P2 후 | 전체 감소율 |
|-----------|----------|-------|-------|------------|
| **StakeToken** | 272 | 122 | **97** | **-64.3%** |
| **CollectReward** | 332 | 65 | 65 | **-80.4%** |
| **UnStakeToken** | 338 | 142 | 141 | **-58.3%** |

### 판단

- **StakeToken의 -25 개선은 유효**하며 적용 가치가 있다.
- **CollectReward 2차 이상치는 조사 필요**. block height 변경 시 cacheReward
  재계산이 원인이라면, 이상치는 특정 조건에서만 발생하는 것이므로 평균적
  사용 패턴에서의 영향은 제한적이다. 다만 GAS +39.6% 증가는 무시할 수 없으므로
  원인 파악 후 별도 대응이 필요하다.
- **UnStakeToken은 실질 변화 없음**. 추가 최적화는 VM 레벨에 의존한다.

---

## 관련 문서

- [P2 DepositView 실험 (미적용)](./p2_deposit_view_experiment.md) — 전체 DepositView 패턴 회귀 기록
- [P1 Phase 2 측정 (Task A+B)](./phase2_key_consolidation.md)
- [P1 Phase 1 측정 (Task B)](./phase1_store_access_caching.md)
- [최종 보고서](./final_report.md)
- [P2 작업 계획](../plans/P2_deposit_accessor_optimization.md)
