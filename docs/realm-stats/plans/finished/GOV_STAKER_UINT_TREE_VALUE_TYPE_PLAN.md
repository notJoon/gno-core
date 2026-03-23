# gov/staker UintTree 내부 포인터 → value type 전환 계획

## Background

`gov/staker/tree.gno`에 정의된 `UintTree`가 staker 모듈과 동일하게 내부 `*avl.Tree` pointer를 갖고 있어 별도 Object 오버헤드가 발생한다. staker 모듈에서 검증된 동일한 전환 (`*avl.Tree` → `avl.Tree`)을 적용한다.

```go
// 현재
type UintTree struct {
    tree *avl.Tree  // pointer → 별도 Object
}

// 변경
type UintTree struct {
    tree avl.Tree   // value → 부모에 inline
}
```

### 영향 범위

| 위치 | 설명 | 제거되는 Object |
|------|------|:---:|
| `totalDelegationHistory` | Store에 `*UintTree`로 저장 | **1 global** |
| `userDelegationHistory` | `avl.Tree: address → *UintTree` — 사용자별 UintTree | **1 per user** |

100명의 delegator가 있는 경우: 1 + 100 = **101개 `*avl.Tree` Object 제거**.

### 추가 사항: key 인코딩 (seqid 미적용)

gov/staker의 `EncodeUint`는 아직 **20자리 zero-padded decimal**을 사용한다:

```go
func EncodeUint(num uint64) string {
    s := strconv.FormatUint(num, 10)
    zerosNeeded := 20 - len(s)
    return strings.Repeat("0", zerosNeeded) + s  // 20 bytes
}
```

staker 모듈은 이미 seqid(7 bytes)로 전환 완료. gov/staker도 동일하게 전환하면 key당 13 bytes 추가 절감 가능하나, 이번 작업 범위에서는 **value type 전환만** 수행한다. seqid 전환은 별도 판단.

---

## 변경 대상

### `gov/staker/tree.gno` — 2줄 변경

```go
// line 22: 필드 타입
tree *avl.Tree → tree avl.Tree

// line 28: 생성자
tree: avl.NewTree() → tree: avl.Tree{}
```

`strconv`, `strings` import는 `EncodeUint`에서 여전히 사용하므로 유지.

### 변경이 필요하지 않는 파일

외부 API (`*UintTree`, 메서드 시그니처)가 변경되지 않으므로 **나머지 모든 파일은 변경 불필요**:

| 파일 | 이유 |
|------|------|
| `gov/staker/store.gno` | `*UintTree` 타입 유지 |
| `gov/staker/types.gno` | interface `*UintTree` 유지 |
| `gov/staker/v1/state.gno` | `*UintTree` 메서드 호출 — 동일 동작 |
| `gov/staker/v1/getter.gno` | `*staker.UintTree` type assert — 타입 변경 없음 |
| `gov/staker/v1/init.gno` | `staker.NewUintTree()` — API 동일 |
| `gov/staker/v1/staker_delegation_snapshot.gno` | `*staker.UintTree` — 동일 |
| 모든 테스트 파일 | API 호환, 변경 불필요 |

---

## Storage 측정

### 측정 테스트

두 테스트 모두 Delegate 시 `updateTotalDelegationHistory` + `updateUserDelegationHistory`를 통해 UintTree를 생성/수정한다.

| 테스트 파일 | 테스트 이름 | UintTree 경로 |
|---|---|---|
| `gov/staker/delegate_and_undelegate.txtar` | `gov_staker_delegate_and_undelegate` | Delegate → `addDelegationRecord` → totalDelegationHistory(UintTree) + userDelegationHistory(UintTree) 생성. Undelegate → 동일 경로 업데이트 |
| `gov/staker/delegate_and_redelegate.txtar` | `gov_staker_delegate_and_redelegate` | Delegate → 동일. Redelegate → 동일 경로 업데이트 |

### 워크플로우

```bash
export GNO_REALM_STATS_LOG=stderr

# Step 1: Baseline
go test -v -run TestTestdata/gov_staker_delegate_and_undelegate -timeout 5m ./gno.land/pkg/integration/
go test -v -run TestTestdata/gov_staker_delegate_and_redelegate -timeout 5m ./gno.land/pkg/integration/

# Step 2: gov/staker/tree.gno 수정 (2줄)

# Step 3: 재측정 및 비교
```

### 결과 기록

#### gov/staker bytes_delta — Delegate 단계 (안정적 비교 기준)

| 단계 | Baseline (20-char zero-padded) | Value type only | Value type + seqid | 총 절감 |
|------|------|------|------|------|
| Delegate (undelegate 테스트) | +16,279 | +15,886 (-393) | +15,860 (-419) | **-419 (-2.6%)** |
| Delegate (redelegate 테스트) | +16,279 | +15,886 (-393) | +15,860 (-419) | **-419 (-2.6%)** |

#### gov/staker bytes_delta — Undelegate/Redelegate 단계

| 단계 | Baseline | Value type + seqid | 차이 |
|------|------|------|------|
| Undelegate | +915 | +915 | 0 (동일) |
| Redelegate | +10,804 | +10,398 | **-406 (-3.8%)** |

#### 관찰

1. **Delegate에서 -419 bytes** — value type(-393) + seqid(-26) 합산 효과.
2. **Redelegate에서 -406 bytes** — delegation history 업데이트 시 key 크기 절감.
3. **Undelegate는 변화 없음** — 해당 단계에서 tree key 조작이 적어 seqid 영향 미미.
4. seqid 전환으로 key당 13 bytes 절감 (20자 → 7자). tree에 entry가 많을수록 누적 효과 증가.
5. 변경 파일 **1개** (`gov/staker/tree.gno`), 모든 테스트 **PASS** — API 완전 호환.
