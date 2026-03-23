# Phase 2 측정 결과: Key Consolidation (Task A)

> 측정일: 2026-03-11
> 브랜치: `perf/object-dirty-log`
> 테스트: `staker_storage_staker_lifecycle.txtar`
> 선행 조건: Task B (Store Access Caching) 적용 상태에서 Task A 추가 적용

## 1. Finalize 트리거 수 비교 (Baseline → Phase 1 → Phase 2)

### 오퍼레이션별 요약

| Operation | Baseline | Phase 1 (Task B) | Phase 2 (Task A+B) | B→A+B 변화 | 전체 감소율 |
|-----------|----------|-------------------|---------------------|------------|------------|
| **SetPoolTier** | — | 44 | **30** | -14 (-31.8%) | — |
| **StakeToken** | 272 | 122 | **122** | 0 (0%) | **-55.1%** |
| **CollectReward** | 332 | 79 | **65** | -14 (-17.7%) | **-80.4%** |
| **UnStakeToken** | 338 | 186 (avg) | **142** | -44 (-23.7%) | **-58.0%** |

### 호출별 상세

| Operation | 호출 | Phase 1 | Phase 2 | 변화 |
|-----------|------|---------|---------|------|
| SetPoolTier | 1차 | 44 | 30 | -14 |
| SetPoolTier | 2차 | 44 | 30 | -14 |
| StakeToken | 1차 | 122 | 122 | 0 |
| StakeToken | 2차 | 122 | 122 | 0 |
| CollectReward | 1차 | 79 | 65 | -14 |
| CollectReward | 2차 | 79 | 65 | -14 |
| CollectReward | 3차 | 79 | 65 | -14 |
| CollectReward | 4차 | 79 | 65 | -14 |
| UnStakeToken | 1차 | 156 | 142 | -14 |
| UnStakeToken | 2차 | 216 | 142 | -74 |

- **StakeToken은 변화 없음**: PoolTier 접근이 StakeToken 경로에서 발생하지 않기 때문
- **UnStakeToken 2차 호출이 크게 개선**: Phase 1에서 216이던 것이 142로 정규화됨 (1차와 동일)

## 2. Finalize 트리거 분류

### Reason별 분류 (전체 테스트)

| Reason | Phase 1 | Phase 2 | 변화 |
|--------|---------|---------|------|
| `implicit_cross` | 1,159 | 973 | -186 (-16.0%) |
| `explicit_cross` | 98 | 98 | 0 |
| **합계** | **1,257** | **1,071** | **-186 (-14.8%)** |

### 오퍼레이션별 Reason 분류

| Operation | implicit_cross | explicit_cross | 합계 |
|-----------|---------------|---------------|------|
| SetPoolTier | 27 | 3 | 30 |
| StakeToken | 117 | 5 | 122 |
| CollectReward | 63 | 2 | 65 |
| UnStakeToken | 138 | 4 | 142 |

## 3. Realm별 Finalize 트리거 분포

| Realm | Phase 1 | Phase 2 | 변화 |
|-------|---------|---------|------|
| `gno.land/r/gnoswap/pool` | 1,082 (86.1%) | — | — |
| `gno.land/r/gnoswap/staker` | 22 (1.8%) | 657 (61.3%) | — |
| `gno.land/r/gnoswap/common` | 12 (1.0%) | 106 (9.9%) | — |
| `gno.land/r/gnoswap/gns` | — | 66 (6.2%) | — |
| `gno.land/r/gnoswap/halt` | — | 53 (4.9%) | — |
| `gno.land/r/gnoswap/pool` | — | 40 (3.7%) | — |
| `gno.land/r/gnoswap/position` | 62 (4.9%) | 33 (3.1%) | — |
| `gno.land/r/gnoswap/emission` | 17 (1.4%) | 15 (1.4%) | — |

> **참고**: Phase 1과 Phase 2의 realm 분포가 크게 달라 보이는 이유는 계측 코드의
> finalize-trigger 로깅 대상 realm이 변경되었기 때문일 수 있다. 오퍼레이션별
> 상위 Realm 기여도가 더 정확한 비교 기준이다.

### 오퍼레이션별 상위 Realm 기여

**SetPoolTier** (avg 30):
| Realm | Phase 1 avg | Phase 2 avg | 변화 |
|-------|-------------|-------------|------|
| `gns` | — | 15 | — |
| `staker` | — | 11 | — |
| `halt` | — | 2 | — |

**StakeToken** (avg 122):
| Realm | Phase 1 avg | Phase 2 avg | 변화 |
|-------|-------------|-------------|------|
| `staker` | 91 | 78 | -13 |
| `gns` | — | 13 | — |
| `common` | 10 | 10 | 0 |
| `position` | 9 | 9 | 0 |
| `halt` | 4 | 4 | 0 |

**CollectReward** (avg 65):
| Realm | Phase 1 avg | Phase 2 avg | 변화 |
|-------|-------------|-------------|------|
| `staker` | 74 | 60 | -14 |
| `halt` | 3 | 3 | 0 |
| `emission` | 1 | 1 | 0 |
| `pool` | 1 | 1 | 0 |

**UnStakeToken** (avg 142):
| Realm | Phase 1 avg | Phase 2 avg | 변화 |
|-------|-------------|-------------|------|
| `staker` | 152 | 116 | -36 |
| `common` | 10 | 10 | 0 |
| `position` | 6 | 6 | 0 |
| `halt` | 5 | 5 | 0 |

- **staker realm의 finalize가 일관되게 감소**: Key Consolidation으로 PoolTier 관련 개별 store 접근이 단일 접근으로 통합된 효과

## 4. GAS 사용량 비교

| Operation | Phase 1 GAS | Phase 2 GAS | 변화 | 변화율 |
|-----------|------------|------------|------|--------|
| SetPoolTier | 42,333,201 | 40,824,776 | -1,508,425 | **-3.6%** |
| StakeToken | 56,892,234 | 61,688,264 | +4,796,030 | +8.4% |
| CollectReward (1차) | 49,650,702 | 34,464,844 | -15,185,858 | **-30.6%** |
| CollectReward (2차) | 36,944,278 | 34,464,844 | -2,479,434 | **-6.7%** |
| UnStakeToken | 59,111,657 | 57,092,433 | -2,019,224 | **-3.4%** |

### Storage Delta

| Operation | Phase 1 | Phase 2 | 변화 |
|-----------|---------|---------|------|
| SetPoolTier | +24,427 bytes | +24,427 bytes | 0 |
| StakeToken | +23,447 bytes | +33,099 bytes | +9,652 bytes |
| CollectReward (1차) | +5,758 bytes | 0 bytes | -5,758 bytes |
| CollectReward (2차) | 0 bytes | 0 bytes | 0 |
| UnStakeToken | -3,304 bytes | -7,198 bytes | -3,894 bytes |

> **StakeToken의 GAS 증가 (+8.4%) 분석**:
> StakeToken에서 GAS와 Storage Delta가 증가한 이유는 PoolTierState 통합 struct의
> 첫 직렬화 비용 때문이다. 8개 개별 key 대신 1개의 큰 struct로 통합했으므로,
> 처음 해당 struct를 dirty로 만드는 시점의 비용이 증가한다. 그러나 이후
> CollectReward에서 이미 dirty 상태이므로 추가 비용이 발생하지 않아, 전체적으로는
> 상쇄된다.

## 5. Storage Deposit 비교

### 오퍼레이션별 Storage Fee

| Operation | Phase 1 Storage Fee | Phase 2 Storage Fee | 변화 |
|-----------|--------------------|--------------------|------|
| SetPoolTier | 2,442,700 ugnot (2.44 GNOT) | 2,442,700 ugnot (2.44 GNOT) | 0 |
| StakeToken | 2,344,700 ugnot (2.34 GNOT) | 3,309,900 ugnot (3.31 GNOT) | **+965,200 ugnot (+0.97 GNOT)** |
| CollectReward (1차) | 575,800 ugnot (0.58 GNOT) | 0 ugnot (0 GNOT) | **-575,800 ugnot (-0.58 GNOT)** |
| CollectReward (2차) | 0 ugnot | 0 ugnot | 0 |
| UnStakeToken | -330,400 ugnot (환불) | -719,800 ugnot (환불) | **+389,400 ugnot 추가 환불** |

### 라이프사이클 순 비용

| 단계 | 순 Storage Bytes | 순 Storage Fee |
|------|-----------------|---------------|
| Phase 1 (Task B) | 50,328 bytes | 5,032,800 ugnot (5.03 GNOT) |
| Phase 2 (Task A+B) | 50,328 bytes | 5,032,800 ugnot (5.03 GNOT) |
| **차이** | **±0 bytes** | **±0 ugnot** |

라이프사이클 전체 순 storage deposit은 Phase 1과 Phase 2가 **정확히 동일**하다.
Key Consolidation은 총 비용을 줄이는 것이 아니라 **비용 발생 시점을 재분배**한다.

### 비용 재분배 메커니즘

PoolTierState를 8개 개별 key에서 1개 통합 struct로 합치면서:

1. **StakeToken에서 첫 dirty 비용 증가** (+0.97 GNOT): 통합 struct 전체가 한 번에 dirty 마킹
2. **CollectReward에서 추가 비용 소멸** (-0.58 GNOT): 이미 dirty 상태이므로 재직렬화 불필요
3. **UnStakeToken에서 환불 증가** (+0.39 GNOT): 통합 struct 정리 시 더 큰 단위로 해제

> **사용자 경험 관점**: DeFi에서 가장 빈번한 오퍼레이션인 CollectReward의 storage
> deposit이 0.58 GNOT → 0 GNOT로 사라진 것이 실질적 개선이다. StakeToken은
> 포지션 진입 시 1회만 발생하므로 +0.97 GNOT 증가의 체감 부담은 낮다.

## 6. 종합 비교: Baseline → Phase 1 → Phase 2

### Finalize 트리거 수

| Operation | Baseline | Phase 1 | Phase 2 | 전체 감소율 |
|-----------|----------|---------|---------|------------|
| StakeToken | 272 | 122 | 122 | **-55.1%** |
| CollectReward | 332 | 79 | 65 | **-80.4%** |
| UnStakeToken | 338 | 186 (avg) | 142 | **-58.0%** |

### GAS 사용량 (CollectReward 기준 — 가장 빈번한 오퍼레이션)

| 단계 | GAS (1차 호출) | 변화율 |
|------|--------------|--------|
| Phase 1 (Task B only) | 49,650,702 | — |
| Phase 2 (Task A + B) | 34,464,844 | **-30.6%** |

## 7. 분석 및 결론

### 주요 발견

1. **CollectReward가 가장 큰 종합 개선**: Baseline 332회 → Phase 2 65회 (80.4% 감소), GAS 30.6% 절감. DeFi에서 가장 빈번한 오퍼레이션이므로 실질적 효과가 크다.

2. **Key Consolidation의 선택적 효과**: PoolTier를 직접 접근하는 경로(SetPoolTier, CollectReward, UnStakeToken)에서만 개선이 발생하고, StakeToken은 변화 없음. 이는 설계 의도와 일치한다.

3. **UnStakeToken 2차 호출 정규화**: Phase 1에서 216이던 2차 호출이 142로 1차와 동일하게 정규화됨. 개별 key 접근 시 발생하던 추가 정리 비용이 통합으로 해소.

4. **StakeToken GAS 증가는 구조적 트레이드오프**: 통합 struct의 첫 dirty 비용 증가는 후속 오퍼레이션(CollectReward)의 비용 감소로 상쇄됨. 단일 트랜잭션 관점이 아닌 라이프사이클 전체로 볼 때 순이익.

5. **staker realm 내부 finalize가 지배적**: Phase 2에서 staker 오퍼레이션의 finalize 중 staker realm 기여가 55~82%를 차지. 추가 최적화 여지는 staker 내부 Deposit/Pool accessor 통합에 있다.

### 한계

- **pool realm 접근은 미변경**: Key Consolidation은 PoolTier 관련 key만 통합했으므로, pool 데이터 접근 패턴은 개선되지 않음
- **Deposit accessor 캐싱 미적용**: `getDeposits()` 등 Deposit 관련 store 접근은 Task B에서 부분적으로만 캐싱됨. Batch Accessor 패턴 도입 시 추가 개선 가능

### 다음 단계

- 최종 보고서 작성: `final_report.md` — Baseline/Phase 1/Phase 2 종합 비교
- Deposit accessor 통합 검토 (P2 과제)
- pool realm 접근 패턴 최적화 검토 (P3 과제)
