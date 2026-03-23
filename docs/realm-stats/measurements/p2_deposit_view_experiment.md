# P2 실험 결과: DepositView 패턴 (미적용 — 회귀 확인)

> 측정일: 2026-03-11
> 브랜치: `perf/object-dirty-log`
> 테스트: `staker_storage_staker_lifecycle.txtar`
> 선행 조건: P1 (Task A + Task B) 적용 상태에서 DepositView 추가 적용
> **결론: 회귀 확인. 본 변경은 적용하지 않는다.**

---

## 1. 요약

DepositView 패턴은 tainted `*sr.Deposit` 객체의 스칼라 필드를 로컬 struct에 1회
스냅샷하여 이후 읽기를 0-finalize로 전환하는 최적화이다.

**결과**: CollectReward와 UnStakeToken에서 finalize가 오히려 증가했다.
스냅샷의 초기 비용(~10-11 finalize)이 후속 읽기 절감 효과를 초과했기 때문이다.

---

## 2. Finalize 트리거 수 비교

### 오퍼레이션별 요약

| Operation | P1 (Task A+B) | P2 (DepositView) | 변화 | 변화율 |
|-----------|---------------|-------------------|------|--------|
| **SetPoolTier** | 30 | 32 (avg) | +2 | +6.7% |
| **StakeToken** | 122 | 108 | -14 | **-11.5%** |
| **CollectReward** | 65 | 84 (avg) | +19 | **+29.2%** |
| **UnStakeToken** | 142 | 161 | +19 | **+13.4%** |

### 호출별 상세

| Operation | 호출 | P1 | P2 | 변화 |
|-----------|------|-----|-----|------|
| SetPoolTier | 1차 | 30 | 30 | 0 |
| SetPoolTier | 2차 | 30 | 34 | +4 |
| StakeToken | 1차 | 122 | 108 | -14 |
| StakeToken | 2차 | 122 | 108 | -14 |
| CollectReward | 1차 | 65 | 71 | +6 |
| CollectReward | 2차 | 65 | 71 | +6 |
| CollectReward | 3차 | 65 | 71 | +6 |
| CollectReward | 4차 | 65 | 121 | +56 |
| UnStakeToken | 1차 | 142 | 161 | +19 |
| UnStakeToken | 2차 | 142 | 161 | +19 |

- **CollectReward 4차 호출 이상치**: 121 triggers (staker=100, gns=13, v1=3). 마지막 CollectReward에서 추가 상태 정리 로직이 실행되면서 gns realm 접근과 v1 realm 내부 finalize가 추가 발생한 것으로 추정.

---

## 3. Realm별 Finalize 분포 비교

### CollectReward (P1 avg 65 → P2 avg 84)

| Realm | P1 avg | P2 avg | 변화 |
|-------|--------|--------|------|
| `staker` | 60 | 74.5 | **+14.5** |
| `gns` | 0 | 3.2 | +3.2 |
| `halt` | 3 | 3 | 0 |
| `emission` | 1 | 1 | 0 |
| `pool` | 1 | 1 | 0 |
| `v1` | 0 | 0.8 | +0.8 |

### UnStakeToken (P1 avg 142 → P2 avg 161)

| Realm | P1 avg | P2 avg | 변화 |
|-------|--------|--------|------|
| `staker` | 116 | 135 | **+19** |
| `common` | 10 | 10 | 0 |
| `position` | 6 | 6 | 0 |
| `halt` | 5 | 5 | 0 |
| `pool` | — | 2 | +2 |
| `gnft` | — | 2 | +2 |
| `emission` | — | 1 | +1 |

### StakeToken (P1 avg 122 → P2 avg 108)

| Realm | P1 avg | P2 avg | 변화 |
|-------|--------|--------|------|
| `staker` | 78 | 77 | -1 |
| `common` | 10 | 10 | 0 |
| `position` | 9 | 9 | 0 |
| `halt` | 4 | 4 | 0 |
| `gnft` | 2 | 2 | 0 |
| `referral` | — | 2 | — |
| `pool` | — | 2 | — |
| `emission` | — | 1 | — |

> StakeToken의 -14 개선은 DepositView와 무관하다. StakeToken은 새 deposit을
> 생성하므로 기존 tainted deposit 접근이 없다. 다른 코드 변경(함수 시그니처
> 조정, 호출 체인 정리 등)의 부수 효과로 추정된다.

---

## 4. GAS 사용량 비교

| Operation | P1 GAS | P2 GAS | 변화 | 변화율 |
|-----------|--------|--------|------|--------|
| SetPoolTier (1차) | 40,824,776 | 41,175,049 | +350,273 | +0.9% |
| StakeToken | 61,688,264 | 55,734,082 | -5,954,182 | **-9.7%** |
| CollectReward (1차) | 34,464,844 | 34,803,425 | +338,581 | +1.0% |
| CollectReward (2차) | 34,464,844 | 48,538,959 | +14,074,115 | **+40.8%** |
| UnStakeToken | 57,092,433 | 58,066,987 | +974,554 | +1.7% |

### Storage Delta

| Operation | P1 | P2 | 변화 |
|-----------|-----|-----|------|
| SetPoolTier | +24,427 bytes | +24,427 bytes | 0 |
| StakeToken | +33,099 bytes | +23,447 bytes | -9,652 bytes |
| CollectReward (1차) | 0 bytes | — | — |
| CollectReward (2차) | 0 bytes | +5,758 bytes | +5,758 bytes |
| UnStakeToken | -7,198 bytes | -3,304 bytes | +3,894 bytes |

---

## 5. 회귀 원인 분석

### 5.1 핵심 원인: 스냅샷 초기 비용 > 읽기 절감 효과

`NewDepositView(d *sr.Deposit)` 생성자는 10-11개의 accessor를 한 번에 호출한다:

```
d.Owner()                          → finalize #1
d.TargetPoolPath()                 → finalize #2
d.Liquidity().Clone()              → finalize #3 + #4 (Liquidity + Clone)
d.StakeTime()                      → finalize #5
d.TickLower()                      → finalize #6
d.TickUpper()                      → finalize #7
d.Warmups() + copy()               → finalize #8
d.InternalRewardLastCollectTime()  → finalize #9
d.CollectedInternalReward()        → finalize #10
d.LastExternalIncentiveUpdatedAt() → finalize #11
```

이 11회의 초기 비용은 **매번 DepositView를 생성할 때마다** 발생한다.

### 5.2 P1 캐싱이 이미 반복 접근을 제거한 상태

P1 (Task B: Store Access Caching)에서 이미 동일 오퍼레이션 내 `deposits.get(positionId)`의
반복 호출을 제거했다. 따라서 P1 이후 deposit accessor 호출 패턴은 이미 다음과 같았다:

```
기존 (Baseline):
  deposits.get(positionId)  ← 5-6회 반복
  deposit.Owner()           ← 3-4회 반복
  deposit.TickLower()       ← 4-6회 반복
  ...

P1 후:
  deposits.get(positionId)  ← 1회 (캐싱)
  deposit.Owner()           ← 2-3회 (일부 경로에서만 반복)
  deposit.TickLower()       ← 2-4회 (CalculateRawRewardForPosition 내부)
  ...
```

P1 이후 각 accessor의 **잔여 반복 횟수가 이미 낮은 상태**에서, DepositView의
초기 스냅샷 비용(11 finalize)은 절감(각 필드당 1-3회 반복 제거)을 초과한다.

### 5.3 비용-편익 산술

**CollectReward 기준**:

| 항목 | finalize 수 |
|------|------------|
| DepositView 생성 비용 | +11 |
| Owner() 반복 제거 (2-3회 → 0) | -2~3 |
| TickLower/Upper 반복 제거 (2-4회 → 0) | -2~4 |
| Warmups() 반복 제거 (2-3회 → 0) | -2~3 |
| Liquidity() 반복 제거 (1-2회 → 0) | -1~2 |
| **순 효과** | **약 +1~6** |

실측 증가분 +6 (71 - 65)은 이 산술과 일치한다.

### 5.4 CollectReward 4차 호출 이상치 (121 triggers)

4번째 CollectReward에서만 staker realm이 100회(vs 1-3차 66회)로 급증했다.
추가로 gns 13회, v1 3회가 발생했다.

추정 원인:
- 마지막 CollectReward 이후 UnStakeToken이 예정되어 있어, 내부적으로
  추가 상태 정리 또는 보상 정산 로직이 트리거됨
- gns realm 접근 13회는 GNS 토큰 전송 관련 로직으로, 보상 금액이 0이 아닌
  경우에만 발생하는 경로가 활성화된 것으로 보임
- v1 realm 자체 finalize 3회는 DepositView의 write-through setter가 v1 소유
  캐시를 갱신하면서 발생한 것으로 추정

### 5.5 StakeToken 개선 (-14)의 원인

StakeToken은 새 deposit을 생성하므로 DepositView를 사용하지 않는다.
-14 개선은 P2 작업 중 부수적으로 발생한 코드 변경의 효과로 추정된다:

- `DepositResolver` 구조 변경에 따른 `NewDepositResolver` 경로 최적화
- 함수 시그니처 변경(`*sr.Deposit` → `*DepositView`)에 따른 호출 체인 단축
- 또는 계측 코드의 finalize-trigger 로깅 범위 미세 변경

이 개선은 DepositView 패턴의 직접적 효과가 아니므로, DepositView를 적용하지 않더라도
해당 부수 변경만 별도 추출하여 적용하는 것을 검토할 수 있다.

---

## 6. 교훈 및 향후 참고사항

### 6.1 스냅샷 패턴의 적용 조건

DepositView(스냅샷) 패턴이 효과적이려면 다음 조건을 충족해야 한다:

```
스냅샷 비용(N개 필드 × 1 finalize) < 잔여 반복 접근 횟수의 합
```

**P1 이전**(Baseline) 상태에서는 반복 접근이 28-40회로 충분히 많아 스냅샷이
유효했을 것이다. 그러나 **P1 캐싱 적용 후** 잔여 반복이 ~8-14회로 줄어들면서
손익분기점 아래로 떨어졌다.

이는 DB 최적화에서 "query preload 적용 후 result cache를 추가하면 오히려
cache population 비용이 더 커지는" 현상과 동일하다.

### 6.2 선택적 스냅샷의 가능성

모든 필드를 한 번에 스냅샷하는 대신, **접근 빈도가 높은 필드만 선택적으로
캐싱**하면 비용을 줄일 수 있다:

```go
// 전체 스냅샷 (11 finalize) — 현재 방식
view := NewDepositView(deposit)  // 모든 필드 1회 접근

// 선택적 스냅샷 (3 finalize) — 대안
tickLower := deposit.TickLower()   // 1 finalize
tickUpper := deposit.TickUpper()   // 1 finalize
warmups := deposit.Warmups()       // 1 finalize
// 나머지는 필요할 때 직접 접근
```

`TickLower`/`TickUpper`는 `CalculateRawRewardForPosition` 내에서 2-4회
반복 접근되므로, 이 2개 필드만 캐싱하면 비용 2 finalize, 절감 2-4 finalize로
순이익이 될 수 있다. 다만 절감 규모가 작아(~2회) 구현 복잡도 대비 효과가 낮다.

### 6.3 VM 레벨 최적화와의 관계

DepositView가 필요한 근본 원인은 **읽기 전용 필드 접근에도 finalize가
트리거되는** VM 동작이다. VM이 다음 최적화를 지원하면 DepositView 자체가 불필요하다:

1. **Read-only finalize skip**: dirty mark가 없는 객체의 finalize를 건너뜀
2. **Tainted field read 최적화**: 값 변경 없는 getter 호출 시 finalize를 생략

이 두 최적화 중 하나라도 적용되면 현재 잔여 finalize의 ~70%가 제거된다.
컨트랙트 레벨 스냅샷은 VM 최적화가 없는 환경에서의 우회 전략이지만,
P1 캐싱으로 이미 접근 횟수가 충분히 줄어든 상태에서는 추가 효과가 미미하다.

### 6.4 결론

| 구분 | 내용 |
|------|------|
| **적용 여부** | 미적용 (회귀 확인) |
| **핵심 원인** | 스냅샷 초기 비용(11 finalize) > 잔여 반복 접근 절감(~5-8 finalize) |
| **전제 조건 오류** | P2 계획 시 추정한 반복 접근 28-40회는 Baseline 기준. P1 캐싱 후 잔여 반복은 ~8-14회로 감소해 있었음 |
| **대안** | 선택적 필드 캐싱(TickLower/Upper만) — 효과 미미(~2회), 비권장 |
| **근본 해결** | VM 레벨 read-only finalize skip (P3 범위) |

---

## 관련 문서

- [P2 작업 계획](../plans/P2_deposit_accessor_optimization.md)
- [P1 Phase 2 측정 (Task A+B)](./phase2_key_consolidation.md)
- [P1 Phase 1 측정 (Task B)](./phase1_store_access_caching.md)
- [최종 보고서](./final_report.md)
