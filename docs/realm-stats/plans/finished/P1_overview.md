---
marp: false
---

# P1 Overview: Key Consolidation + Store Access Caching

## 배경 및 목표

### 문제

Gno VM에서 `r/gnoswap/staker/v1` (impl)이 `r/gnoswap/staker` (proxy) 소유의 데이터에
접근할 때마다 **implicit_cross finalize**가 트리거된다. 이는 versioned proxy 패턴의
구조적 특성이며, 각 `store.Get()`/`store.Set()` 호출이 별도의 finalize를 발생시킨다.

계측 결과:
- StakeToken: 272회 FinalizeRealmTransaction (실질 데이터 기록 ~50회, 나머지 no-op)
- CollectReward: 332회
- UnStakeToken: 338회

### 목표

두 가지 컨트랙트 레벨 최적화를 적용하여 store 접근 횟수를 줄인다:

1. **Key Consolidation**: PoolTier 관련 8개 store key를 1개로 통합
2. **Store Access Caching**: 오퍼레이션 내에서 동일 store key의 반복 읽기 제거

PoC 검증 결과:
- Key Consolidation 단독: finalize **71% 감소** (28→8), gas **5.0% 절감**
- 전체 조합: finalize **77% 감소** (52→12), gas **6.4% 절감**

---

## 아키텍처 개요

### Realm 구조

```
r/gnoswap/staker (proxy realm)
├── state.gno      — kvStore, versionManager, implementation 변수
├── store.gno      — stakerStore 구현 (KVStore 래퍼)
├── types.gno      — IStakerStore 인터페이스, IStaker 인터페이스
├── proxy.gno      — public crossing 함수들 (StakeToken, CollectReward, ...)
├── accessor.gno   — PoolAccessor, EmissionAccessor, NFTAccessor
├── deposit.gno    — Deposit 타입 정의
├── pool.gno       — Pool 타입 정의
└── upgrade.gno    — RegisterInitializer, UpgradeImpl

r/gnoswap/staker/v1 (impl realm)
├── instance.gno       — stakerV1 struct, getPoolTier/updatePoolTier
├── staker.gno         — StakeToken, CollectReward, UnStakeToken 본체
├── init.gno           — registerStakerV1, initStoreData
├── calculate_pool_position_reward.gno  — calcPositionReward
├── reward_calculation_pool_tier.gno    — PoolTier 타입, cacheReward
├── manage_pool_tier_and_warmup.gno     — setPoolTier, changePoolTier, removePoolTier
└── ...
```

### Finalize 트리거 메커니즘

```
v1 코드에서 s.store.GetPools() 호출
  → stakerStore.GetPools() 실행
    → s.kvStore.Get("pools") 실행
      → proxy realm 소유의 map[string]any 접근
        → m.Realm이 proxy로 전환
  → 함수 반환 시: m.Realm(proxy) ≠ cfr.LastRealm(v1)
    → implicit_cross → FinalizeRealmTransaction 트리거
```

**핵심**: 매 store.Get()/store.Set() 호출마다 1회의 finalize가 발생한다.

---

## 멘탈 모델: DB N+1 문제와의 구조적 유사성

이 최적화는 관계형 DB에서의 N+1 쿼리 문제 해결과 구조적으로 동일한 패턴이다.
작업 방향을 잡을 때 이 대응 관계를 참고하면 도움이 된다.

### 개념 대응

| DB (N+1 문제) | Staker (현재 문제) |
|:-------------:|:-----------------:|
| 개별 SQL 쿼리 | `store.Get(key)` 호출 |
| DB 라운드트립 비용 (network I/O) | implicit_cross finalize 비용 (재직렬화 + KV Write) |
| 루프 내 lazy loading | helper 함수 내부에서 매번 `s.getDeposits()` 등 재호출 |
| Eager loading / Preload | 진입 시점에서 일괄 로드 후 파라미터 전달 |
| JOIN / IN clause (쿼리 구조 통합) | Key Consolidation (8개 key → 1개 struct) |

### 비용 구조 비교

DB의 N+1에서 병목은 **네트워크 라운드트립**이다.
Gno VM에서의 병목은 **FinalizeRealmTransaction에 의한 재직렬화와 KV Write**이다.

두 경우 모두 "접근 1회의 고정 비용"이 높기 때문에, 접근 횟수 자체를 줄이는 것이
가장 효과적인 최적화이다.

```
DB:   SELECT * FROM incentives WHERE id = ?  ← N번 실행 (라운드트립 N회)
Gno:  s.getExternalIncentives().get(id)      ← N번 실행 (finalize N회)

해결:
DB:   SELECT * FROM incentives WHERE id IN (?, ?, ...)  ← 1번 실행
Gno:  externalIncentives := s.getExternalIncentives()   ← 1번 로드
      externalIncentives.get(id)                        ← in-memory (finalize 없음)
```

### B → A 작업 순서와의 관계

DB 최적화에서도 일반적으로 같은 순서를 따른다:

1. **먼저 쿼리 횟수를 줄인다** (Eager loading / Preload)
   → B 작업: Store Access Caching. 기존 스키마를 변경하지 않고 접근 패턴만 개선.

2. **그 다음 스키마를 최적화한다** (비정규화, 컬럼 통합)
   → A 작업: Key Consolidation. 8개 개별 key를 1개 struct로 통합.

이 순서가 안전한 이유: 1단계는 로직 변경 없이 변수 재사용만으로 구현되므로
회귀 위험이 낮다. 2단계에서 스키마(store key 구조)를 변경할 때도 1단계의
캐싱 코드 위에서 작업하므로 변경 범위가 명확하다.

### 한계: DB 최적화와 다른 점

- **캐시 무효화가 불필요하다**: DB의 경우 캐시된 데이터가 다른 트랜잭션에 의해
  변경될 수 있어 TTL이나 무효화 전략이 필요하다. Gno에서는 단일 트랜잭션 내에서만
  캐싱하고, 트랜잭션 종료 시 로컬 변수가 사라지므로 stale data 문제가 없다.

- **같은 object의 포인터 공유**: `s.getPools()`가 반환하는 것은 proxy realm의
  `*avl.Tree` 포인터이다. 캐싱 후 수정하면 원본이 직접 수정된다.
  DB의 ORM에서 detached entity를 수정하는 것과는 다르다.

- **finalize 비용이 object 크기에 비례**: DB의 라운드트립은 데이터 크기와 무관하게
  고정 비용이지만, Gno의 finalize는 대상 object의 직렬화 크기에 비례한다.
  따라서 Key Consolidation(A 작업)으로 object를 합치면 1회의 finalize 비용은
  증가하지만, 총 finalize 횟수 감소 효과가 이를 압도한다.

---

## 파일 변경 요약

| 파일 | 작업 | 변경 내용 |
|------|------|----------|
| `r/gnoswap/staker/types.gno` | A-1 | `PoolTierState` struct 추가, `IStakerStore` 확장 |
| `r/gnoswap/staker/store.gno` | A-2 | `StoreKeyPoolTierState` 추가, Get/Set 구현 |
| `r/gnoswap/staker/v1/instance.gno` | A-3 | `getPoolTier`/`updatePoolTier` 단순화 |
| `r/gnoswap/staker/v1/init.gno` | A-4, A-5 | 개별 key 초기화 삭제 + 통합 초기화 |
| `r/gnoswap/staker/v1/staker.gno` | B-1, B-2, B-5, B-7 | StakeToken/CollectReward/UnStakeToken 캐싱 + cross-realm 값 캐싱 |
| `r/gnoswap/staker/v1/manage_pool_tier_and_warmup.gno` | B-4 | setPoolTier 등 캐싱 |
| `r/gnoswap/staker/v1/calculate_pool_position_reward.gno` | B-3, B-7 | calcPositionRewardWith 추가 (currentTime, blockHeight 파라미터 포함) |
| `r/gnoswap/staker/v1/assert.gno` | B-2 | assertIsDepositorWith 추가 (deposits 파라미터 전달) |

---

## 테스트 계획

### 1. 기존 테스트 유지

```bash
# 기존 통합 테스트가 모두 통과해야 함
cd gno.land/pkg/integration
go test -v -run "TestTestdata/staker_storage_staker_lifecycle" -timeout 600s
```

### 2. Finalize 트리거 수 검증

계측 코드(`op_call.go`의 `LogFinalizeTrigger`)를 사용하여 before/after 비교:

```bash
# Before
GNO_REALM_STATS_LOG=/tmp/before_p1.log \
  go test -v -run "TestTestdata/staker_storage_staker_lifecycle" -timeout 600s

# After
GNO_REALM_STATS_LOG=/tmp/after_p1.log \
  go test -v -run "TestTestdata/staker_storage_staker_lifecycle" -timeout 600s

# 비교
grep -c "finalize-trigger" /tmp/before_p1.log
grep -c "finalize-trigger" /tmp/after_p1.log
```

**검증 기준**:
- StakeToken의 총 finalize 수가 감소해야 함
- GAS USED가 감소해야 함
- 모든 트랜잭션의 실행 결과가 동일해야 함

### 3. PoC 검증

```bash
# PoolTier Key Consolidation PoC
go test -v -run "TestTestdata/poc_sticky_last_realm" -timeout 120s
```

---

## 작업 순서

> **순서 근거**: Store Access Caching을 먼저 적용하면 기존 store 키 구조를 변경하지 않고도
> finalize 횟수를 줄일 수 있어 안전하다. 이후 Key Consolidation을 적용하면 캐싱된
> 코드 위에서 store 접근 횟수 자체를 줄이는 구조 개선이 되므로, 변경 충돌 없이
> 순차적으로 효과를 누적할 수 있다.

```
Phase 1: Store Access Caching (B 작업)
  1. assert.gno — assertIsDepositorWith 추가 (deposits 파라미터 전달)
  2. calculate_pool_position_reward.gno — calcPositionRewardWith 추가 (currentTime, blockHeight 포함)
  3. staker.gno — StakeToken 캐싱
  4. staker.gno — CollectReward 캐싱 (assertIsDepositorWith 사용, poolTier는 emission 이후 로드)
  5. manage_pool_tier_and_warmup.gno — setPoolTier 등 캐싱
  6. staker.gno — UnStakeToken (collectRewardInternal 분리)
  7. cross-realm 값 캐싱 (time.Now().Unix(), runtime.ChainHeight() 1회 호출 후 전달)
  8. 테스트 실행 → 통과 확인

  ** Phase 1 중간 검증 **
  9. finalize 트리거 수 측정 (baseline 대비)
  10. GAS 사용량 측정
  11. 결과 기록 → docs/realm-stats/measurements/phase1_caching.log

Phase 2: Key Consolidation (A 작업)
  12. types.gno — PoolTierState 구조체, AllTierCount 상수 추가
  13. store.gno — StoreKeyPoolTierState 추가, 개별 8개 키/메서드 삭제
  14. instance.gno — getPoolTier/updatePoolTier를 단일 Get/Set으로 변경
  15. init.gno — 개별 key 초기화 삭제 + 통합 초기화
  16. 테스트/mock 파일 업데이트 (mock_store.gno, types_test.gno 등)
  17. 테스트 실행 → 통과 확인

  ** Phase 2 중간 검증 **
  18. finalize 트리거 수 측정 (Phase 1 결과 대비 누적 개선)
  19. GAS 사용량 측정
  20. 결과 기록 → docs/realm-stats/measurements/phase2_consolidation.log
  21. 클로저 직렬화 검증: getEmission(), getHalvingBlocksInRange() 정상 동작 확인
  22. PoolTierState 관련 created/updated object 수 확인 (ancestor propagation 포함)

Phase 3: 최종 검증
  23. Phase 1, 2 측정 결과 종합 비교 (baseline → Phase 1 → Phase 2)
  24. 오퍼레이션별 finalize 횟수 / GAS 변화 테이블 작성
  25. 결과 문서화 → docs/realm-stats/measurements/final_report.md
```

---

## 관련 문서

- [작업 A: PoolTier Key Consolidation](./P1_task_A_key_consolidation.md) — Phase 2
- [작업 B: Store Access Caching](./P1_task_B_store_access_caching.md) — Phase 1
- [Finalize 트레이스 분석](../analysis/staker_finalize_trace_analysis.md)
- PoC: `gno.land/pkg/integration/testdata/poc_sticky_last_realm.txtar`
- PoC: `gno.land/pkg/integration/testdata/poc_contract_level_optimizations.txtar`
