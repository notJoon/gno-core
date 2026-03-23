# Tick/Incentives Value Type 전환 비교 결과

**Branch:** `perf/object-dirty-log` @ `668f4174a`
**Date:** 2026-03-16

---

## 전환 내용

- **Tick struct**: `*u256.Uint` → `u256.Uint`, `*i256.Int` → `i256.Int`, `*UintTree` → `UintTree`
- **Incentives struct**: `*avl.Tree` → `avl.Tree`, `*UintTree` → `UintTree`
- **Pool struct**: `*Incentives` → `Incentives`

---

## Before/After 비교 (Total STORAGE DELTA)

| Operation | Before (B) | After (B) | Delta (B) | % |
|-----------|----------:|----------:|----------:|------:|
| T1 SetPoolTier | 22,384 | 21,185 | **-1,199** | -5.4% |
| T1 StakeToken | 20,199 | 17,803 | **-2,396** | -11.9% |
| T1 CollectReward #1 (immediate) | 0 | 0 | 0 | — |
| T1 CollectReward #2 (1 block) | 5,706 | 5,706 | 0 | 0% |
| T1 UnStakeToken | -917 | -917 | 0 | 0% |
| T2 StakeToken (isolated) | 20,199 | 17,803 | **-2,396** | -11.9% |
| T3 CreateExtIncentive #1 (BAR) | 11,236 | 11,241 | +5 | ~0% |
| T3 CreateExtIncentive #2 (FOO) | 12,196 | 12,201 | +5 | ~0% |
| T3 CreateExtIncentive #3 (WUGNOT) | 8,071 | 8,071 | 0 | 0% |
| T3 StakeToken (w/ 3 externals) | 36,167 | 28,819 | **-7,348** | -20.3% |
| T4 CreateExtIncentive (1st for pool) | 28,742 | 27,531 | **-1,211** | -4.2% |
| T5 SetPoolTier | 22,384 | 21,185 | **-1,199** | -5.4% |
| T5 StakeToken | 20,199 | 17,803 | **-2,396** | -11.9% |
| T5 CollectReward (immediate) | 0 | 0 | 0 | — |

## Staker realm 단독 비교

| Operation | Before (B) | After (B) | Delta (B) | % |
|-----------|----------:|----------:|----------:|------:|
| T1 StakeToken staker | 19,147 | 16,751 | **-2,396** | -12.5% |
| T3 StakeToken (w/ ext) staker | 35,115 | 27,767 | **-7,348** | -20.9% |
| T4 CreateExtIncentive staker | 24,662 | 23,451 | **-1,211** | -4.9% |
| T1 SetPoolTier staker | 22,384 | 21,185 | **-1,199** | -5.4% |

## GAS USED 비교

| Operation | Before | After | Delta | % |
|-----------|-------:|------:|------:|------:|
| T1 SetPoolTier | 38,268,045 | 38,250,333 | -17,712 | -0.05% |
| T1 StakeToken | 50,781,887 | 50,691,690 | -90,197 | -0.18% |
| T1 CollectReward #1 | 31,290,790 | 31,266,121 | -24,669 | -0.08% |
| T1 CollectReward #2 | 37,272,563 | 37,248,938 | -23,625 | -0.06% |
| T1 UnStakeToken | 51,223,240 | 51,130,547 | -92,693 | -0.18% |
| T3 StakeToken (w/ ext) | 54,878,325 | 54,352,452 | -525,873 | -0.96% |

---

## 분석

### 절감 효과

1. **StakeToken (단독)**: -2,396 B (-11.9%)
   - Tick 2개(lower, upper) 생성 시 각 tick에서 3 pointer objects 제거 = 6 objects
   - 6 objects × ~400 B/object = ~2,400 B (측정값과 일치)

2. **StakeToken (w/ 3 externals)**: -7,348 B (-20.3%)
   - Tick objects 절감: -2,396 B
   - 추가 절감 ~4,952 B: external incentive 처리 중 Incentives/Pool 관련 object 재생성 감소

3. **SetPoolTier**: -1,199 B (-5.4%)
   - Pool 생성 시 Incentives 내부 `*avl.Tree`, `*UintTree` → value embed 효과
   - Pool.incentives 자체 `*Incentives` → `Incentives` value embed

4. **CreateExtIncentive (first for pool)**: -1,211 B (-4.2%)
   - 새 풀에 첫 incentive 생성 시 Incentives 객체 포인터 오버헤드 제거

5. **CollectReward/UnStakeToken**: 0 변화
   - 이미 생성된 객체를 읽기만 하므로 storage write 패턴 불변

### GAS 절감

- Gas는 전체적으로 미미하게 감소 (-0.05% ~ -0.96%)
- T3 StakeToken에서 -525K gas (-0.96%)로 가장 큰 절감 — object 직렬화 비용 감소

### 예상 vs 실제

| 항목 | 예상 | 실제 |
|------|------|------|
| StakeToken 절감 | 600~1,200 B | **2,396 B** (2x 예상) |
| 제거 objects/tick | 3 | 3 (확인됨) |
| Incentives 효과 | per pool | per pool + per stake (확인됨) |
