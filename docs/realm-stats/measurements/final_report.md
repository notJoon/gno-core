# P1 최종 보고서: Staker Finalize 최적화

> 작성일: 2026-03-11
> 브랜치: `perf/object-dirty-log`
> 테스트: `staker_storage_staker_lifecycle.txtar`

## 요약

Gno VM의 versioned proxy 패턴에서 발생하는 FinalizeRealmTransaction 과다 호출을
두 가지 컨트랙트 레벨 최적화로 개선하였다.

| 지표 | Baseline | 최종 (Task A+B) | 변화 |
|------|:---:|:---:|:---:|
| CollectReward finalize | 332 | 65 | **-80.4%** |
| UnStakeToken finalize | 338 | 142 | **-58.0%** |
| StakeToken finalize | 272 | 122 | **-55.1%** |
| CollectReward GAS | — | Phase 1 대비 **-30.6%** | |
| 라이프사이클 순 storage 비용 | 5.03 GNOT | 5.03 GNOT | ±0 |

---

## 1. 배경

### 문제

`r/gnoswap/staker/v1` (impl realm)이 `r/gnoswap/staker` (proxy realm) 소유의
데이터에 접근할 때마다 implicit_cross finalize가 트리거된다. 이는 versioned proxy
패턴의 구조적 특성으로, DB의 N+1 쿼리 문제와 동일한 비용 구조를 가진다.

```
store.Get(key) 1회 호출 = finalize 1회 = 재직렬화 + KV Write 비용
→ N번 호출하면 N번의 고정 비용 발생
```

Baseline 계측에서 주요 오퍼레이션당 272~338회의 FinalizeRealmTransaction이
발생하고 있었으며, 이 중 77%가 실질 데이터 변경 없는 no-op finalize였다.

### 근본 원인 분석

StakeToken 기준 272회 finalize의 원인 분포:

| 원인 | 횟수 | 비율 | 설명 |
|------|:---:|:---:|------|
| store.Get/Set | 62 | 23% | KVStore 읽기/쓰기 (proxy 소유 map 접근) |
| uint256 메서드 | 24 | 9% | IsZero, Lt 등 (proxy 소유 포인터 접근) |
| avl.Tree 조작 | 22 | 8% | ReverseIterate, Set 등 (proxy 소유 tree 접근) |
| Deposit accessor | 17 | 6% | Ticks, StakedLiquidityGross 등 개별 getter |
| gns cross-realm | 12 | 4% | getStartTimestamp 반복 호출 |
| halt 체크 | 8 | 3% | IsOperationHalted 반복 호출 |
| 기타 | 127 | 47% | 기타 implicit/explicit cross |

96%가 implicit_cross (proxy-impl 간 데이터 접근), 4%만 explicit_cross (외부 realm 호출)이다.

### 적용한 최적화

| 최적화 | Task | 내용 |
|--------|:---:|------|
| **Store Access Caching** | B | 오퍼레이션 진입 시 store 데이터를 일괄 로드, 로컬 변수로 재사용 |
| **Key Consolidation** | A | PoolTier 관련 8개 store key를 1개 `PoolTierState` struct로 통합 |

---

## 2. Finalize 트리거 수 변화

### 오퍼레이션별 3단계 비교

| Operation | Baseline | Phase 1 (Task B) | Phase 2 (A+B) | 전체 감소율 |
|-----------|:---:|:---:|:---:|:---:|
| **CollectReward** | 332 | 79 | **65** | **-80.4%** |
| **UnStakeToken** | 338 | 186 (avg) | **142** | **-58.0%** |
| **StakeToken** | 272 | 122 | **122** | **-55.1%** |
| **SetPoolTier** | — | 44 | **30** | — |

### Phase별 기여

```
Baseline ──────────────── Phase 1 (Task B) ──────── Phase 2 (Task A) ────── 최종

CollectReward:  332 ──(-253, -76.2%)──▶ 79 ──(-14, -17.7%)──▶ 65   (총 -80.4%)
UnStakeToken:   338 ──(-152, -45.0%)──▶186 ──(-44, -23.7%)──▶142   (총 -58.0%)
StakeToken:     272 ──(-150, -55.1%)──▶122 ──(  0,   0.0%)──▶122   (총 -55.1%)
SetPoolTier:      — ─────────────────▶ 44 ──(-14, -31.8%)──▶ 30
```

- **Task B가 주요 기여**: 모든 오퍼레이션에서 대부분의 감소가 Phase 1에서 발생
- **Task A는 선택적 기여**: PoolTier를 접근하는 경로(CollectReward, UnStakeToken, SetPoolTier)에서만 추가 감소. StakeToken은 PoolTier를 접근하지 않으므로 변화 없음
- **UnStakeToken 2차 호출 정규화**: Phase 1에서 216이던 2차 호출이 142로 1차와 동일하게 정규화됨 (개별 key 정리 비용 해소)

### 호출별 상세

| Operation | 호출 | Baseline | Phase 1 | Phase 2 |
|-----------|:---:|:---:|:---:|:---:|
| SetPoolTier | 1차 | — | 44 | 30 |
| SetPoolTier | 2차 | — | 44 | 30 |
| StakeToken | 1차 | 272 | 122 | 122 |
| StakeToken | 2차 | 272 | 122 | 122 |
| CollectReward | 1~4차 | 332 | 79 | 65 |
| UnStakeToken | 1차 | 338 | 156 | 142 |
| UnStakeToken | 2차 | 338 | 216 | 142 |

---

## 3. Finalize 트리거 분류

### Reason별 변화 (전체 테스트)

| Reason | Phase 1 | Phase 2 | 변화 |
|--------|:---:|:---:|:---:|
| `implicit_cross` | 1,159 | 973 | -186 (-16.0%) |
| `explicit_cross` | 98 | 98 | 0 |
| **합계** | **1,257** | **1,071** | **-186 (-14.8%)** |

implicit_cross가 전체의 91%를 차지하며, Task A에 의해 186회 감소했다.
explicit_cross(외부 realm 호출)는 구조적으로 동일하므로 변화 없음.

### 오퍼레이션별 Reason 분류 (Phase 2 최종)

| Operation | implicit_cross | explicit_cross | 합계 |
|-----------|:---:|:---:|:---:|
| SetPoolTier | 27 | 3 | 30 |
| StakeToken | 117 | 5 | 122 |
| CollectReward | 63 | 2 | 65 |
| UnStakeToken | 138 | 4 | 142 |

---

## 4. Realm별 Finalize 분포

### 오퍼레이션별 상위 Realm 기여 (Phase 2 최종)

**StakeToken** (122):

| Realm | Phase 1 | Phase 2 | 변화 |
|-------|:---:|:---:|:---:|
| staker | 91 | 78 | -13 |
| gns | — | 13 | — |
| common | 10 | 10 | 0 |
| position | 9 | 9 | 0 |
| halt | 4 | 4 | 0 |

**CollectReward** (65):

| Realm | Phase 1 | Phase 2 | 변화 |
|-------|:---:|:---:|:---:|
| staker | 74 | 60 | -14 |
| halt | 3 | 3 | 0 |
| emission | 1 | 1 | 0 |
| pool | 1 | 1 | 0 |

**UnStakeToken** (142):

| Realm | Phase 1 | Phase 2 | 변화 |
|-------|:---:|:---:|:---:|
| staker | 152 | 116 | -36 |
| common | 10 | 10 | 0 |
| position | 6 | 6 | 0 |
| halt | 5 | 5 | 0 |

**SetPoolTier** (30):

| Realm | Phase 2 |
|-------|:---:|
| gns | 15 |
| staker | 11 |
| halt | 2 |

> **참고: pool realm의 기여**
>
> 전체 테스트 집계에서 pool realm이 86%를 차지하지만, 이는 CreatePool/Mint 등
> pool 자체 오퍼레이션이 포함된 수치이다. staker 오퍼레이션에서의 pool 기여는
> CollectReward에서 1회, 그 외 0~2회로 미미하다
> (Baseline 분석: StakeToken 272회 중 pool은 4회, 1.5%).
>
> staker 최적화 관점에서 pool realm은 추가 대상이 아니며, 남은 finalize는
> staker 내부의 store 접근 패턴에 기인한다.

---

## 5. GAS 사용량

### 오퍼레이션별 GAS 비교

| Operation | Phase 1 GAS | Phase 2 GAS | 변화 | 변화율 |
|-----------|:---:|:---:|:---:|:---:|
| SetPoolTier | 42,333,201 | 40,824,776 | -1,508,425 | **-3.6%** |
| StakeToken | 56,892,234 | 61,688,264 | +4,796,030 | +8.4% |
| CollectReward (1차) | 49,650,702 | 34,464,844 | -15,185,858 | **-30.6%** |
| CollectReward (2차) | 36,944,278 | 34,464,844 | -2,479,434 | **-6.7%** |
| UnStakeToken | 59,111,657 | 57,092,433 | -2,019,224 | **-3.4%** |

> **StakeToken GAS 증가 (+8.4%) 분석**:
> PoolTierState 통합 struct의 첫 직렬화 비용 때문이다. 8개 개별 key 대신
> 1개의 큰 struct로 통합했으므로, 처음 dirty로 만드는 시점의 비용이 증가한다.
> 그러나 후속 CollectReward에서 이미 dirty 상태이므로 추가 비용이 발생하지 않아,
> 라이프사이클 전체로는 상쇄된다.

### Storage Delta

| Operation | Phase 1 | Phase 2 | 변화 |
|-----------|:---:|:---:|:---:|
| SetPoolTier | +24,427 bytes | +24,427 bytes | 0 |
| StakeToken | +23,447 bytes | +33,099 bytes | +9,652 |
| CollectReward (1차) | +5,758 bytes | 0 bytes | -5,758 |
| CollectReward (2차) | 0 bytes | 0 bytes | 0 |
| UnStakeToken | -3,304 bytes | -7,198 bytes | -3,894 |

---

## 6. Storage Deposit (사용자 비용)

### 오퍼레이션별 Storage Fee

| Operation | Phase 1 | Phase 2 | 변화 |
|-----------|:---:|:---:|:---:|
| SetPoolTier | 2.44 GNOT | 2.44 GNOT | 0 |
| StakeToken | 2.34 GNOT | 3.31 GNOT | **+0.97 GNOT** |
| CollectReward (1차) | 0.58 GNOT | **0 GNOT** | **-0.58 GNOT** |
| CollectReward (2차) | 0 GNOT | 0 GNOT | 0 |
| UnStakeToken | -0.33 GNOT (환불) | -0.72 GNOT (환불) | **+0.39 GNOT 추가 환불** |

### 라이프사이클 순 비용

| 단계 | 순 Storage | 순 Fee |
|------|:---:|:---:|
| Phase 1 (Task B) | 50,328 bytes | 5.03 GNOT |
| Phase 2 (Task A+B) | 50,328 bytes | 5.03 GNOT |
| **차이** | **±0** | **±0** |

Key Consolidation은 총 비용을 변경하지 않고 **비용 발생 시점을 재분배**한다:

1. StakeToken에서 첫 dirty 비용 증가 (+0.97 GNOT)
2. CollectReward에서 추가 비용 소멸 (-0.58 GNOT)
3. UnStakeToken에서 환불 증가 (+0.39 GNOT)

> **사용자 경험 관점**: 가장 빈번한 오퍼레이션인 CollectReward의 storage deposit이
> 0.58 GNOT → 0 GNOT로 사라진 것이 실질적 개선이다. StakeToken은 포지션 진입 시
> 1회만 발생하므로 +0.97 GNOT 증가의 체감 부담은 낮다.

---

## 7. PoC 예측 vs 실측 비교

PoC (`poc_contract_level_optimizations.txtar`)에서의 예측과 실측을 비교한다.

### Finalize 감소율

| 최적화 | PoC 예측 | 실측 | 비고 |
|--------|:---:|:---:|------|
| Task B (Store Access Caching) | — | -55~76% | PoC에는 단독 시나리오 없음 |
| Task A (Key Consolidation) | -71% (28→8) | -18~32% | PoolTier 경로에서만 효과 |
| 전체 조합 | -77% (52→12) | -55~80% | 오퍼레이션별 편차 |

PoC는 각 패턴당 6회 호출의 단순 시나리오였으므로, 실제 staker의 복잡한 호출 패턴과는
차이가 있다. 그러나 방향성(implicit_cross 감소가 주요 효과)은 일치한다.

### GAS 절감

| 최적화 | PoC 예측 | 실측 (CollectReward 기준) |
|--------|:---:|:---:|
| 전체 조합 | -6.4% | **-30.6%** (Phase 1→2) |

실측이 PoC보다 높은 이유: 실제 staker에서 PoolTier 관련 store 접근이
PoC의 6회보다 훨씬 많아 통합 효과가 증폭되었기 때문이다.

---

## 8. 잔여 Finalize 분석

Phase 2 이후에도 남아 있는 finalize의 구성을 분석한다.

### StakeToken (122회 잔여)

| 카테고리 | 예상 횟수 | 설명 |
|----------|:---:|------|
| store.Get/Set (캐싱 후 잔여) | ~30 | Deposit/Pool 관련 store 접근 |
| avl.Tree 조작 | ~20 | Deposit tree 삽입/순회 |
| Deposit accessor | ~15 | 개별 getter (Ticks, Liquidity 등) |
| gns/common/halt cross-realm | ~27 | 외부 realm 호출 |
| position/gnft | ~13 | NFT 민팅, 포지션 조회 |
| 기타 | ~17 | explicit_cross 포함 |

### CollectReward (65회 잔여)

| 카테고리 | 예상 횟수 | 설명 |
|----------|:---:|------|
| staker 내부 store 접근 | ~40 | 보상 계산 로직 내 store 접근 |
| staker 내부 accessor | ~20 | Deposit/Pool 필드 접근 |
| 외부 realm | ~5 | halt, emission, pool |

### 추가 최적화 가능성

| 방안 | 예상 효과 | 변경 비용 | 우선순위 |
|------|:---:|:---:|:---:|
| Batch Accessor (Snapshot 패턴) | ~15~18회 추가 감소 (GAS 절감) | 높음 (API 변경) | P2 |
| Deposit accessor 통합 | ~10회 추가 감소 (GAS 절감) | 중간 | P2 |
| VM no-op finalize skip | wall-clock 시간만 절감 (GAS 무영향) | 낮음 (VM 코어) | P3 |

### VM No-op Finalize Skip 검증 결과

`FinalizeRealmTransaction` 내부에서 marks가 비어있을 때 early return하는 최적화를
구현하여 검증하였다.

**구현**:
```go
// realm.go — FinalizeRealmTransaction 내부
store.LogFinalizeRealm(rlm.Path)  // opslog 유지 (62개 테스트 호환)

if len(rlm.newCreated) == 0 && len(rlm.newDeleted) == 0 &&
   len(rlm.newEscaped) == 0 && len(rlm.updated) == 0 {
    rlm.clearMarks()
    realmDiffs := store.RealmStorageDiffs()
    realmDiffs[rlm.Path] += rlm.sumDiff
    rlm.sumDiff = 0
    return
}
```

**결과**:

| Operation | GAS (without skip) | GAS (with skip) | 차이 |
|-----------|:---:|:---:|:---:|
| SetPoolTier | 40,824,776 | 40,824,776 | **0** |
| StakeToken | 55,383,809 | 55,383,809 | **0** |
| CollectReward | 34,368,196 | 34,368,196 | **0** |
| UnStakeToken | 57,573,456 | 57,573,456 | **0** |

PoC 테스트(`poc_sticky_last_realm`)에서도 전 시나리오 GAS 동일.

**원인**: `FinalizeRealmTransaction`은 시작 시 `bm.PauseOpCode()`로 opcode 계측을
중단한다. 따라서 내부의 모든 처리(빈 슬라이스 순회, 함수 호출 오버헤드)는
**GAS에 포함되지 않는다.** GAS에 반영되는 것은 `store.SetObject()` /
`store.DelObject()`의 fixed cost뿐이며, marks가 비어있으면 이 호출이 발생하지 않으므로
no-op finalize는 **원래부터 GAS 0이다.**

**결론**: VM no-op finalize skip은 노드의 wall-clock 실행 시간만 절감하며,
사용자 GAS 비용에는 영향을 주지 않는다. GAS 절감이 목적이라면
app 레벨 최적화(store 접근 횟수 자체를 줄이는 Task A, B)가 유일한 방법이다.

---

## 9. 결론

### 달성 성과

1. **CollectReward**: finalize 80.4% 감소, GAS 30.6% 절감. DeFi에서 가장 빈번한
   오퍼레이션이므로 실질적 사용자 비용 절감 효과가 크다.

2. **UnStakeToken**: finalize 58.0% 감소, 2차 호출 정규화 (216→142).
   개별 key 정리 비용이 통합으로 해소된 부수 효과.

3. **StakeToken**: finalize 55.1% 감소. GAS는 8.4% 증가했으나
   라이프사이클 전체로는 순이익 (CollectReward 절감으로 상쇄).

4. **라이프사이클 순 storage 비용 동일**: Key Consolidation은 비용 총량을 변경하지 않고
   발생 시점만 재분배. 빈번한 CollectReward의 비용이 0으로 전환.

### 설계 교훈

1. **N+1 문제 해결이 가장 효과적**: store.Get/Set 호출 횟수를 줄이는 캐싱(Task B)이
   전체 감소의 대부분을 기여했다. 스키마 통합(Task A)은 보조적 역할.

2. **B→A 작업 순서가 올바른 선택**: 접근 패턴 개선(안전, 낮은 위험) →
   스키마 변경(구조적, 중간 위험) 순서가 변경 충돌 없이 효과를 누적할 수 있었다.

3. **비용 재분배는 의도적으로 설계해야 한다**: Key Consolidation에 의한
   StakeToken GAS 증가는 예상 가능한 트레이드오프이며, 라이프사이클 관점에서
   빈번한 오퍼레이션에 유리하도록 비용을 이동시킨 것이다.

4. **전체 테스트 집계 vs 오퍼레이션별 분리**: pool realm이 86%라는 수치는
   테스트 전체 집계의 착시이며, staker 오퍼레이션별로 분리하면 pool 기여는 1~4회에
   불과했다. 측정 시 집계 범위를 명확히 분리해야 정확한 최적화 방향을 잡을 수 있다.

5. **Finalize 횟수 ≠ GAS 비용**: FinalizeRealmTransaction 내부는
   `bm.PauseOpCode()`로 GAS 계측에서 제외된다. no-op finalize는 원래부터 GAS 0이므로,
   finalize 횟수를 줄이는 것 자체가 GAS를 줄이는 것은 아니다. GAS 절감은
   finalize에서 실행되는 `store.SetObject()` / `store.DelObject()` 호출 횟수를
   줄일 때만 발생한다. 이것이 app 레벨 최적화(store 접근 패턴 개선)가 VM 레벨
   최적화(no-op skip)보다 GAS 절감에 효과적인 이유이다.

### 한계 및 향후 과제

- **VM no-op finalize skip은 GAS 무영향**: 검증 결과, `FinalizeRealmTransaction`
  내부 처리는 `bm.PauseOpCode()`에 의해 GAS 계측에서 제외된다.
  no-op finalize를 건너뛰어도 사용자 GAS 비용은 변하지 않으며, 노드의
  wall-clock 실행 시간만 절감된다. GAS 절감이 목적이라면 app 레벨 최적화만 유효하다.

- **Deposit accessor 통합 미적용**: 개별 getter(Owner, PoolPath, TickLower 등)가
  각각 finalize를 트리거한다. Batch Accessor(Snapshot 패턴) 도입 시 추가 개선
  가능하나 API 변경 범위가 넓다 (P2 범위).

- **StakeToken의 추가 개선 여지**: Task A에서 변화 없었으므로,
  StakeToken 경로 특유의 store 접근 패턴(Deposit tree 조작, NFT 민팅)을
  별도로 분석해야 한다.

---

## 관련 문서

- [P1 Overview](../plans/P1_overview.md) — 배경, 아키텍처, 멘탈 모델
- [Task A: Key Consolidation](../plans/P1_task_A_key_consolidation.md)
- [Task B: Store Access Caching](../plans/P1_task_B_store_access_caching.md)
- [Finalize 트레이스 분석](../analysis/staker_finalize_trace_analysis.md) — Baseline 상세 분석
- [Phase 1 측정](./phase1_store_access_caching.md)
- [Phase 2 측정](./phase2_key_consolidation.md)
- PoC: `gno.land/pkg/integration/testdata/poc_sticky_last_realm.txtar`
- PoC: `gno.land/pkg/integration/testdata/poc_contract_level_optimizations.txtar`
