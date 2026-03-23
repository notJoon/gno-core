# P3 작업 계획: Halving 경계 캐싱 최적화 [DROP]

> 작성일: 2026-03-12
> 상태: **DROP** — P2 선별 적용과 함께 drop. CollectReward 이상치의 근본 원인(P2가
> `cacheReward` 타이밍을 변경하여 regression 유발)이 해소되지 않은 상태에서
> P3 단독 적용은 무의미.
> 선행 조건: P1 (Store Access Caching + Key Consolidation) 적용 상태
> 대상: `r/gnoswap/staker/v1/reward_calculation_pool_tier.gno`, `r/gnoswap/gns/getter.gno`

---

## 요약

P2 선별 적용 후 발생한 CollectReward 2차 호출 이상치(125 finalize)의 원인을 분석하여,
`PoolTier.cacheReward`에서 `getHalvingBlocksInRange`가 매번 12년 전체를 순회하는
비용(+13 finalize)을 `nextHalvingTimestamp` 캐싱으로 제거하는 최적화를 설계.

## 측정 결과

- CollectReward 이상치: 125 → 112 (-13 finalize, -1.7M GAS)
- SetPoolTier: 32 → 47 (+15 finalize) — `findNextHalvingTimestamp` 초기화 비용
- Storage delta: 변화 없음

## Drop 사유

P3는 P2 위에서만 의미가 있으나, P2 자체가 CollectReward의 `cacheReward` 타이밍을
변경하여 이상치(+47 finalize)를 도입함. P1 상태에서는 블록이 진행되어도 모든
CollectReward가 65로 일관되므로, P2+P3를 적용하면 CollectReward가 65 → 112로
악화될 위험이 있음. 이 위험을 해소하려면 P1 상태에서의 블록 진행 시 cacheReward
동작을 추가 검증해야 하나, 컨트랙트 레벨 최적화 작업을 종료하기로 결정.

상세 설계(변경 사항, 테스트 계획 등)는 이전 버전의 이 문서에 기록되어 있었으나,
drop 결정에 따라 요약만 보존.

## 관련 문서

- [P2 선별 적용 + P3 측정](../measurements/p2_selective_changes.md)
- [P2 DepositView 실험 (미적용)](../measurements/p2_deposit_view_experiment.md)
- [P1 Overview](./P1_overview.md)
- [메인 보고서](../../storage-deposit-optimization-report.md)
