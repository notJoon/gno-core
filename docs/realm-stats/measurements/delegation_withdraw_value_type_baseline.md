# `[]*DelegationWithdraw` → `[]DelegationWithdraw` 측정 결과

**Date:** 2026-03-16
**Branch:** `perf/object-dirty-log`
**Base Commit:** `668f4174a` (feat(gnolang): instruments realm boundary)

---

## Summary

`Delegation.withdraws` 필드가 `[]*DelegationWithdraw` (pointer slice)로 선언되어 있어,
Undelegate 시 `DelegationWithdraw` 객체가 별도 Object로 생성되었다.

`[]DelegationWithdraw` (value slice)로 전환하여 모든 요소를 부모 Object에 inline 직렬화하도록 변경.

### 변경 파일

| 파일 | 변경 내용 |
| ---- | --------- |
| `gov/staker/delegation.gno` | `[]*DelegationWithdraw` → `[]DelegationWithdraw` (필드, 생성자, getter, setter, Clone) |
| `gov/staker/delegation_withdraw.gno` | 생성자 반환 `*DelegationWithdraw` → `DelegationWithdraw`, `Clone()` 제거 |
| `gov/staker/types.gno` | interface `GetDelegationWithdraws` 반환 타입 변경 |
| `gov/staker/getters.gno` | proxy `GetDelegationWithdraws` 반환 타입 변경 |
| `gov/staker/v1/delegation.gno` | `range` loop을 `&withdraws[i]` 패턴으로 변경 (aliasing 안전) |
| `gov/staker/v1/delegation_withdraw.gno` | wrapper 함수 반환 타입 변경 |
| `gov/staker/v1/getter.gno` | `GetDelegationWithdraws` 반환 타입 변경 |

---

## Before / After 비교

### Test: `gov_staker_delegate_and_undelegate`

#### Storage Delta

| 단계 | Before | After | 절감 | 절감률 |
| ---- | ------ | ----- | ---- | ------ |
| Delegate | 18,312 bytes | 18,290 bytes | -22 bytes | -0.1% |
| **Undelegate** | **5,166 bytes** | **915 bytes** | **-4,251 bytes** | **-82.3%** |
| CollectUndelegatedGns | 0 bytes | 0 bytes | — | — |

#### Gas Used

| 단계 | Before | After | 절감 |
| ---- | ------ | ----- | ---- |
| Delegate | 28,782,375 | 28,757,917 | -24,458 |
| **Undelegate** | **25,790,716** | **25,707,345** | **-83,371** |
| CollectUndelegatedGns | 17,129,302 | 17,099,016 | -30,286 |

### Test: `gov_staker_delegate_and_redelegate`

#### Storage Delta

| 단계 | Before | After | 절감 | 절감률 |
| ---- | ------ | ----- | ---- | ------ |
| Delegate | 18,312 bytes | 18,290 bytes | -22 bytes | -0.1% |
| Redelegate | 10,826 bytes | 10,804 bytes | -22 bytes | -0.2% |

#### Gas Used

| 단계 | Before | After | 절감 |
| ---- | ------ | ----- | ---- |
| Delegate | 28,791,633 | 28,757,917 | -33,716 |
| Redelegate | 25,356,868 | 25,334,074 | -22,794 |

---

## 분석

### Undelegate: -82.3% storage 절감

Undelegate 호출 시 `DelegationWithdraw`가 `AddWithdraw()`를 통해 slice에 추가된다.

- **Before:** `*DelegationWithdraw` 포인터가 별도 Object로 직렬화 → Object 헤더 + 포인터 참조 오버헤드 발생 (5,166 bytes)
- **After:** `DelegationWithdraw` 값이 부모 Delegation Object에 inline 직렬화 → 별도 Object 불필요 (915 bytes)
- **절감:** 4,251 bytes = 별도 Object 1개의 전체 오버헤드

### Redelegate: -22 bytes (미미)

Redelegate는 내부적으로 `UnDelegateWithoutLockup()`을 사용하며, 이 경로는 `AddWithdraw()`를 호출하지 않는다.
따라서 `DelegationWithdraw` Object 생성이 없어 pointer→value 전환의 직접적 효과가 없다.
-22 bytes는 Delegation struct 자체의 `make([]*DW, 0)` → `make([]DW, 0)` 전환에 의한 미미한 차이.

### Delegate: -22 bytes (미미)

신규 Delegation 생성 시 빈 withdraws slice (`make([]DelegationWithdraw, 0)`)가 초기화되는데,
pointer slice → value slice 전환으로 인한 타입 메타데이터 차이.

---

## 결론

| 항목 | 효과 |
| ---- | ---- |
| **Undelegate storage** | **-4,251 bytes (-82.3%)** — 가장 큰 효과 |
| Undelegate gas | -83,371 gas |
| Delegate storage | -22 bytes (미미) |
| Redelegate storage | -22 bytes (미미, `AddWithdraw` 미사용 경로) |

`DelegationWithdraw`가 순수 값 타입 (7개 int64 + 1개 bool = ~57 bytes)이므로
value slice 전환이 안전하고 효과적이다.
N개의 withdraw가 누적될수록 절감 효과는 N배로 확대된다 (withdraw당 ~4,200 bytes 절감).
