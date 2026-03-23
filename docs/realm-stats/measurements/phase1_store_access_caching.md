# Phase 1 측정 결과: Store Access Caching (Task B)

> 측정일: 2026-03-11
> 브랜치: `perf/object-dirty-log`
> 테스트: `staker_storage_staker_lifecycle.txtar`

## 1. Finalize 트리거 수 비교

### 오퍼레이션별 요약

| Operation | Baseline | After Task B | 감소량 | 감소율 |
|-----------|----------|-------------|--------|--------|
| **StakeToken** | 272 | 122 | -150 | **-55.1%** |
| **CollectReward** | 332 | 79 | -253 | **-76.2%** |
| **UnStakeToken** | 338 | 186 (avg) | -152 | **-45.0%** |
| **SetPoolTier** | — | 44 | — | (baseline 없음) |

- Baseline 수치 출처: `docs/realm-stats/plans/P1_overview.md` 계측 결과

### UnStakeToken 상세 (2회 호출)

| 호출 | Finalize 수 | 비고 |
|------|------------|------|
| 1차 | 156 | 정상 unstake |
| 2차 | 216 | 상태 정리 시 추가 store 접근 발생 |

## 2. Finalize 트리거 분류

### Reason별 분류 (전체 테스트)

| Reason | 횟수 | 비율 |
|--------|------|------|
| `implicit_cross` | 1,159 | 92.2% |
| `explicit_cross` | 98 | 7.8% |
| **합계** | **1,257** | 100% |

proxy/impl 간 store 접근에서 발생하는 `implicit_cross`가 절대 다수를 차지한다.

### 오퍼레이션별 Reason 분류

| Operation | implicit_cross | explicit_cross | 합계 |
|-----------|---------------|---------------|------|
| SetPoolTier | 41 | 3 | 44 |
| StakeToken | 117 | 5 | 122 |
| CollectReward | 77 | 2 | 79 |
| UnStakeToken (1차) | 152 | 4 | 156 |
| UnStakeToken (2차) | 212 | 4 | 216 |

## 3. Realm별 Finalize 트리거 분포 (전체 테스트)

| Realm | 횟수 | 비율 |
|-------|------|------|
| `gno.land/r/gnoswap/pool` | 1,082 | 86.1% |
| `gno.land/r/gnoswap/position` | 62 | 4.9% |
| (빈 realm) | 30 | 2.4% |
| `gno.land/r/gnoswap/staker` | 22 | 1.8% |
| `gno.land/r/gnoswap/emission` | 17 | 1.4% |
| `gno.land/r/gnoswap/rbac` | 13 | 1.0% |
| `gno.land/r/gnoswap/common` | 12 | 1.0% |
| `gno.land/r/gnoswap/gnft` | 8 | 0.6% |
| 기타 | 11 | 0.9% |

> **주의: 집계 범위에 따른 해석 차이**
>
> 위 분포는 테스트 전체(CreatePool, Mint, Swap 등 setup 포함)의 집계이다.
> pool realm의 1,082회(86%)는 대부분 pool 자체 오퍼레이션(CreatePool, Mint 등)에서
> 발생한 것이며, **staker 오퍼레이션에서의 pool 기여는 미미하다.**
>
> Baseline 분석([staker_finalize_trace_analysis.md](../analysis/staker_finalize_trace_analysis.md))에서
> StakeToken 272회 중 pool은 **4회(1.5%)**에 불과하다. 아래 오퍼레이션별 상위 Realm 기여
> 테이블에서도 pool은 CollectReward에서 1회만 나타나고, StakeToken/UnStakeToken에서는
> 상위 목록에 포함되지 않는다.
>
> 따라서 staker 최적화 관점에서 pool realm은 추가 최적화 대상이 아니며,
> 남은 개선 여지는 staker 내부의 store 접근 패턴(Task A: Key Consolidation)과
> Deposit accessor 통합(Batch Accessor/Snapshot 패턴)에 있다.

### 오퍼레이션별 상위 Realm 기여

**StakeToken** (avg 122):
| Realm | 평균 횟수 |
|-------|----------|
| `staker` | 91 |
| `common` | 10 |
| `position` | 9 |
| `halt` | 4 |
| `gnft` | 2 |

**CollectReward** (avg 79):
| Realm | 평균 횟수 |
|-------|----------|
| `staker` | 74 |
| `halt` | 3 |
| `emission` | 1 |
| `pool` | 1 |

**UnStakeToken** (avg 186):
| Realm | 평균 횟수 |
|-------|----------|
| `staker` | 152 |
| `common` | 10 |
| `gns` | 13 |
| `position` | 6 |
| `halt` | 5 |

## 4. GAS 사용량

| Operation | GAS USED | STORAGE DELTA |
|-----------|----------|---------------|
| SetPoolTier | 42,333,201 | +24,427 bytes |
| StakeToken | 56,892,234 | +23,447 bytes |
| CollectReward (1차) | 49,650,702 | +5,758 bytes |
| CollectReward (2차) | 36,944,278 | 0 bytes |
| UnStakeToken | 59,111,657 | -3,304 bytes |

- CollectReward 2차 호출은 storage delta 0 (object가 이미 dirty 상태)
- UnStakeToken은 deposit/pool 데이터 삭제로 음수 delta

## 5. 분석 및 다음 단계

### 주요 발견

1. **CollectReward가 가장 큰 개선**: 332 → 79 (76% 감소). 반복적인 `getDeposits()`, `getPools()` 호출이 캐싱으로 제거된 효과.

2. **`pool` realm이 전체 테스트 finalize의 86% 차지**: 다만 이는 CreatePool/Mint 등 pool 자체 오퍼레이션이 지배적이며, staker 오퍼레이션(StakeToken, CollectReward, UnStakeToken)에서의 pool 기여는 1~4회로 미미하다. staker 최적화 관점에서 pool은 추가 대상이 아니다.

3. **`implicit_cross`가 92%**: store 접근 시 realm 전환으로 인한 finalize가 지배적. Task A (Key Consolidation)로 store key 수 자체를 줄이면 추가 개선 가능.

4. **UnStakeToken 2차 호출이 더 비쌈**: 첫 unstake 후 내부 데이터 구조가 변경되어 2차 호출 시 추가 정리 로직 실행.

### 다음 단계

- **Phase 2 (Task A: Key Consolidation)**: PoolTier 관련 8개 store key → 1개 통합
  - 예상: SetPoolTier의 finalize 44 → ~8 수준으로 추가 감소
  - StakeToken/CollectReward에서 poolTier 관련 store 접근 추가 감소
- Phase 2 측정 결과는 `phase2_key_consolidation.md`에 기록 예정
