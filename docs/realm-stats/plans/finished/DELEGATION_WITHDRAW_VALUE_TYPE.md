# `[]*DelegationWithdraw` → `[]DelegationWithdraw` 전환 계획

## Background

`Delegation` struct의 `withdraws` 필드가 `[]*DelegationWithdraw` (pointer slice)로 선언되어 있다.
Gno에서 pointer slice의 각 요소는 별도 Object가 되어, 부모 struct 쓰기 시 **요소당 ~2,000 gas** 추가 비용이 발생한다.

`DelegationWithdraw`는 순수 값 타입 (7 `int64` + 1 `bool` = ~57 bytes)이므로, `[]DelegationWithdraw` (value slice)로 전환하면 모든 요소가 부모 Object에 inline 직렬화되어 별도 Object 오버헤드가 제거된다.

**예상 절감:** delegation당 N개 withdraw가 있을 때, N개 Object 제거 = N × ~2,000 gas 절감.

---

## 변경 대상 파일

### 1. `gov/staker/delegation.gno` — Delegation struct 및 메서드

**현재:**

```go
// line 25
withdraws []*DelegationWithdraw

// line 43
withdraws: make([]*DelegationWithdraw, 0),

// line 59
func (d *Delegation) Withdraws() []*DelegationWithdraw { return d.withdraws }

// line 70
func (d *Delegation) AddWithdraw(withdraw *DelegationWithdraw) {
    d.withdraws = append(d.withdraws, withdraw)
}

// line 74
func (d *Delegation) SetWithdraws(withdraws []*DelegationWithdraw) {
    d.withdraws = withdraws
}
```

**변경:**

```go
// line 25
withdraws []DelegationWithdraw

// line 43
withdraws: make([]DelegationWithdraw, 0),

// line 59 — 값 슬라이스 반환
func (d *Delegation) Withdraws() []DelegationWithdraw { return d.withdraws }

// line 70 — 값으로 받아서 append
func (d *Delegation) AddWithdraw(withdraw DelegationWithdraw) {
    d.withdraws = append(d.withdraws, withdraw)
}

// line 74
func (d *Delegation) SetWithdraws(withdraws []DelegationWithdraw) {
    d.withdraws = withdraws
}
```

**Clone() 변경 (line 78-102):**

```go
func (d *Delegation) Clone() *Delegation {
    if d == nil {
        return nil
    }

    clonedWithdraws := make([]DelegationWithdraw, len(d.withdraws))
    copy(clonedWithdraws, d.withdraws)

    return &Delegation{
        id:               d.id,
        delegateAmount:   d.delegateAmount,
        unDelegateAmount: d.unDelegateAmount,
        collectedAmount:  d.collectedAmount,
        delegateFrom:     d.delegateFrom,
        delegateTo:       d.delegateTo,
        createdHeight:    d.createdHeight,
        createdAt:        d.createdAt,
        withdraws:        clonedWithdraws,
    }
}
```

> `DelegationWithdraw`는 순수 값 타입이므로 `copy()`로 deep clone이 완료된다.
> 개별 `w.Clone()` 호출이 불필요해진다.

---

### 2. `gov/staker/delegation_withdraw.gno` — 생성자 반환 타입

**현재:**

```go
// line 130
func NewDelegationWithdraw(...) *DelegationWithdraw {
    return &DelegationWithdraw{...}
}

// line 158
func NewDelegationWithdrawWithoutLockup(...) *DelegationWithdraw {
    return &DelegationWithdraw{...}
}

// line 172
func (d *DelegationWithdraw) Clone() *DelegationWithdraw {
```

**변경:**

```go
func NewDelegationWithdraw(...) DelegationWithdraw {
    return DelegationWithdraw{...}
}

func NewDelegationWithdrawWithoutLockup(...) DelegationWithdraw {
    return DelegationWithdraw{...}
}
```

`Clone()` 메서드는 더 이상 필요하지 않다 (값 복사로 충분). 단, 외부에서 호출하는 곳이 없으면 제거, 있으면 호환을 위해 유지하되 구현을 값 반환으로 변경.

**Setter 메서드 receiver 주의:** `SetCollected`, `SetCollectedAt`, `SetCollectedAmount`는 pointer receiver (`*DelegationWithdraw`)를 사용한다. value slice에서 인덱스로 접근 시 `&d.withdraws[i]`로 포인터를 얻어야 수정이 반영된다. 이 점은 아래 v1/delegation.gno의 호출 패턴 변경에서 다룬다.

---

### 3. `gov/staker/v1/delegation_withdraw.gno` — Resolver 및 wrapper

**현재:**

```go
// line 8
type DelegationWithdrawResolver struct {
    withdraw *staker.DelegationWithdraw
}

// line 11
func NewDelegationWithdrawResolver(withdraw *staker.DelegationWithdraw) *DelegationWithdrawResolver {

// line 107-121
func NewDelegationWithdraw(...) *staker.DelegationWithdraw {
    return staker.NewDelegationWithdraw(...)
}

// line 134-146
func NewDelegationWithdrawWithoutLockup(...) *staker.DelegationWithdraw {
    return staker.NewDelegationWithdrawWithoutLockup(...)
}
```

**변경:**

Resolver는 **포인터를 유지**해야 한다. value slice의 요소를 수정하려면 포인터가 필요하기 때문이다.

```go
// 변경 없음 — Resolver는 여전히 *staker.DelegationWithdraw를 받음
type DelegationWithdrawResolver struct {
    withdraw *staker.DelegationWithdraw
}
```

wrapper 함수 반환 타입 변경:

```go
func NewDelegationWithdraw(...) staker.DelegationWithdraw {
    return staker.NewDelegationWithdraw(...)
}

func NewDelegationWithdrawWithoutLockup(...) staker.DelegationWithdraw {
    return staker.NewDelegationWithdrawWithoutLockup(...)
}
```

---

### 4. `gov/staker/v1/delegation.gno` — 핵심 로직 변경

#### 4a. `CollectableAmount` (line 38-43)

**현재:**

```go
for _, withdraw := range r.delegation.Withdraws() {
    total = safeAddInt64(total, NewDelegationWithdrawResolver(withdraw).CollectableAmount(currentTime))
}
```

**변경:**

```go
withdraws := r.delegation.Withdraws()
for i := range withdraws {
    total = safeAddInt64(total, NewDelegationWithdrawResolver(&withdraws[i]).CollectableAmount(currentTime))
}
```

> `range` value copy 대신 `&withdraws[i]`로 원본 요소의 포인터를 전달.

#### 4b. `UnDelegate` (line 46-59)

**현재:**

```go
withdraw := NewDelegationWithdraw(...)  // returns *DelegationWithdraw
r.delegation.AddWithdraw(withdraw)
```

**변경:**

```go
withdraw := NewDelegationWithdraw(...)  // returns DelegationWithdraw
r.delegation.AddWithdraw(withdraw)
```

> `AddWithdraw`가 `DelegationWithdraw` 값을 받도록 변경되므로 호출부는 그대로.

#### 4c. `processCollection` (line 70-113) — **가장 중요한 변경**

**현재:**

```go
withdraws := r.delegation.Withdraws()

for _, withdraw := range withdraws {
    resolver := NewDelegationWithdrawResolver(withdraw)  // pointer into slice element
    // ... resolver.Collect() modifies withdraw in-place
}

// filtering: compact non-collected into front
for _, withdraw := range withdraws {
    if !withdraw.IsCollected() {
        withdraws[currentIndex] = withdraw
        currentIndex++
    }
}

r.delegation.SetWithdraws(withdraws[:currentIndex])
```

**변경:**

```go
withdraws := r.delegation.Withdraws()

for i := range withdraws {
    resolver := NewDelegationWithdrawResolver(&withdraws[i])

    collectableAmount := resolver.CollectableAmount(currentTime)
    if collectableAmount <= 0 {
        continue
    }

    err := resolver.Collect(collectableAmount, currentTime)
    if err != nil {
        return 0, err
    }

    updatedAmount, err := addToCollectedAmount(r.delegation.CollectedAmount(), collectableAmount)
    if err != nil {
        return 0, err
    }

    r.delegation.SetCollectedAmount(updatedAmount)
    collectedAmount = safeAddInt64(collectedAmount, collectableAmount)
}

// filtering 동일
currentIndex := 0
for i := range withdraws {
    if !withdraws[i].IsCollected() {
        withdraws[currentIndex] = withdraws[i]
        currentIndex++
    }
}

r.delegation.SetWithdraws(withdraws[:currentIndex])
```

> **핵심:** `range` loop에서 `_, withdraw` (값 복사) 대신 `i` + `&withdraws[i]` (원본 포인터) 사용.
> 값 복사본의 Setter를 호출하면 원본이 수정되지 않는 aliasing 버그가 발생한다.

---

### 5. `gov/staker/v1/getter.gno` — 반환 타입 변경

**현재 (line 298):**

```go
func (gs *govStakerV1) GetDelegationWithdraws(...) ([]*staker.DelegationWithdraw, error) {
```

**변경:**

```go
func (gs *govStakerV1) GetDelegationWithdraws(...) ([]staker.DelegationWithdraw, error) {
```

빈 반환도 변경:
```go
return []staker.DelegationWithdraw{}, nil
```

`GetCollectableWithdrawAmount` (line 319-331)도 동일 패턴 적용:

```go
for i := range delegation.Withdraws() {
```

→ 이 함수는 read-only이므로 `_, withdraw` 패턴도 안전하지만, 일관성을 위해 `i` + index 접근 권장.

---

### 6. `gov/staker/types.gno` — interface 반환 타입

**현재 (line 66):**

```go
GetDelegationWithdraws(delegationID int64, offset, count int) ([]*DelegationWithdraw, error)
```

**변경:**

```go
GetDelegationWithdraws(delegationID int64, offset, count int) ([]DelegationWithdraw, error)
```

---

### 7. `gov/staker/getters.gno` — proxy 반환 타입

**현재 (line 134):**

```go
func GetDelegationWithdraws(delegationID int64, offset, count int) ([]*DelegationWithdraw, error) {
```

**변경:**

```go
func GetDelegationWithdraws(delegationID int64, offset, count int) ([]DelegationWithdraw, error) {
```

---

### 8. 테스트 파일

| 파일 | 변경 내용 |
| --- | --- |
| `gov/staker/delegation_withdraw_test.gno` | `*DelegationWithdraw` → `DelegationWithdraw` 타입 변경 |
| `gov/staker/v1/delegation_withdraw_test.gno` | Resolver에 `&withdraw`로 전달하도록 변경 |
| `gov/staker/v1/getter_test.gno` | 반환 타입 `[]*` → `[]` 변경 |
| `gov/staker/_mock_test.gno` | mock 반환 타입 변경 (line 269, 275, 277) |

---

## Aliasing 위험 체크리스트

value slice 전환 시 가장 흔한 버그 패턴:

| 패턴 | 위험 | 수정 |
| --- | --- | --- |
| `for _, w := range withdraws { w.SetX() }` | `w`는 복사본 — 원본 미수정 | `for i := range withdraws { withdraws[i].SetX() }` |
| `resolver := NewResolver(withdraw)` (값 전달) | resolver가 복사본을 수정 | `NewResolver(&withdraws[i])` |
| `slice = append(slice, *ptr)` | append 후 기존 포인터 무효화 가능 | value slice에서는 해당 없음 |

**이 코드베이스에서 Setter를 호출하는 경로:**
1. `v1/delegation.gno:processCollection` → `resolver.Collect()` → `withdraw.SetCollectedAmount()`, `SetCollected()`, `SetCollectedAt()`
2. `v1/delegation.gno:UnDelegate` → `AddWithdraw()` (append만, Setter 없음)

**경로 1이 유일한 위험 지점**이며, 위 4c 항목에서 `&withdraws[i]` 패턴으로 해결한다.

---

## Storage 측정

### 측정 테스트

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `gov/staker/delegate_and_undelegate.txtar` | `gov_staker_delegate_and_undelegate` | Delegate + Undelegate (withdraw 생성) |
| `gov/staker/delegate_and_redelegate.txtar` | `gov_staker_delegate_and_redelegate` | Delegate + Redelegate |

### 워크플로우

1. **Baseline 측정:** 수정 전 위 테스트 실행, STORAGE DELTA 기록
2. **코드 수정:** 위 변경사항 적용
3. **재측정:** 동일 테스트 실행, STORAGE DELTA 비교

```bash
export GNO_REALM_STATS_LOG=stderr
go test -v -run TestTestdata/gov_staker_delegate_and_undelegate -timeout 5m ./gno.land/pkg/integration/
go test -v -run TestTestdata/gov_staker_delegate_and_redelegate -timeout 5m ./gno.land/pkg/integration/
```

### 결과 기록

```
| 테스트 | 단계 | Before (bytes) | After (bytes) | 차이 |
|--------|------|----------------|---------------|------|
| delegate_and_undelegate | Undelegate | XXXX | YYYY | -ZZZ |
| delegate_and_redelegate | Redelegate | XXXX | YYYY | -ZZZ |
```
