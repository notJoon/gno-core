# Deposit Value Type 전환 계획서

**대상:** Triple AVL 통합 후속 최적화
**파일:** `contract/r/gnoswap/staker/deposit.gno`, `contract/r/gnoswap/staker/getter_utils.gno`
**목표:** Deposit 내 포인터 필드를 value type으로 전환하여 Object 수 추가 감소

---

## 1. 현재 구조 (Triple AVL 통합 후)

```go
type Deposit struct {
    warmups                        []Warmup
    liquidity                      *u256.Uint       // ← 포인터 (1 Object)
    targetPoolPath                 string
    owner                          address
    stakeTime                      int64
    internalRewardLastCollectTime  int64
    collectedInternalReward        int64
    externalIncentives             *avl.Tree        // ← 포인터 (1 Object)
    lastExternalIncentiveUpdatedAt int64
    tickLower                      int32
    tickUpper                      int32
}
```

tree 내부 값:
```go
externalIncentives: incentiveID → *ExternalIncentiveState  // ← 포인터 (incentiveID당 1 Object)
```

### 잔여 포인터 Object

| 필드 | Object 수 | 비고 |
|------|-----------|------|
| `*avl.Tree` | 1 per Deposit | Tree 포인터 자체 |
| `*ExternalIncentiveState` | N per Deposit | incentiveID당 1개 |
| `*u256.Uint` | 1 per Deposit | liquidity |
| **합계** | **2 + N** per Deposit | |

---

## 2. 수정 사항

### 2a. `*ExternalIncentiveState` → `ExternalIncentiveState` (value type)

**효과:** incentiveID당 1 Object 제거. StakeToken (3 externals) storage +8.2% 악화의 원인 해소.

**변경 원리:**

포인터로 저장하면 avl.Tree 노드에 `*ExternalIncentiveState`가 HeapItemValue Object로 등록된다. value type으로 저장하면 노드의 `Value any` 필드에 struct가 직접 들어가므로 별도 Object가 생성되지 않는다.

**mutation 패턴 변경:**

```go
// Before (포인터): Get한 포인터를 통해 직접 mutation
state := d.getOrCreateIncentiveState(id)
state.CollectedReward = reward  // tree 내부 값이 직접 변경됨

// After (value): Get → 수정 → Set 패턴
state := d.getOrCreateState(id)
state.CollectedReward = reward
d.externalIncentives.Set(id, state)  // 반드시 다시 Set 해야 반영
```

**변경 대상:**

```go
// getOrCreateIncentiveState: *ExternalIncentiveState → ExternalIncentiveState
func (d *Deposit) getOrCreateIncentiveState(incentiveID string) ExternalIncentiveState {
    v, ok := d.externalIncentives.Get(incentiveID)
    if !ok {
        return ExternalIncentiveState{}
    }
    return v.(ExternalIncentiveState)
}

// getIncentiveState: 반환 타입 변경 + exists 플래그
func (d *Deposit) getIncentiveState(incentiveID string) (ExternalIncentiveState, bool) {
    v, ok := d.externalIncentives.Get(incentiveID)
    if !ok {
        return ExternalIncentiveState{}, false
    }
    return v.(ExternalIncentiveState), true
}
```

**accessor 메서드 변경:**

```go
// Before
func (d *Deposit) SetCollectedExternalReward(incentiveID string, reward int64) {
    d.getOrCreateIncentiveState(incentiveID).CollectedReward = reward
}

// After
func (d *Deposit) SetCollectedExternalReward(incentiveID string, reward int64) {
    state := d.getOrCreateIncentiveState(incentiveID)
    state.CollectedReward = reward
    d.externalIncentives.Set(incentiveID, state)
}
```

```go
// Before
func (d *Deposit) SetExternalRewardLastCollectTime(incentiveID string, currentTime int64) {
    d.getOrCreateIncentiveState(incentiveID).LastCollectTime = currentTime
}

// After
func (d *Deposit) SetExternalRewardLastCollectTime(incentiveID string, currentTime int64) {
    state := d.getOrCreateIncentiveState(incentiveID)
    state.LastCollectTime = currentTime
    d.externalIncentives.Set(incentiveID, state)
}
```

```go
// GetCollectedExternalReward: getIncentiveState 반환 타입 변경 반영
func (d *Deposit) GetCollectedExternalReward(incentiveID string) (int64, bool) {
    state, ok := d.getIncentiveState(incentiveID)
    if !ok {
        return 0, false
    }
    return state.CollectedReward, true
}

// GetExternalRewardLastCollectTime: 동일 패턴
func (d *Deposit) GetExternalRewardLastCollectTime(incentiveID string) (int64, bool) {
    state, ok := d.getIncentiveState(incentiveID)
    if !ok {
        return 0, false
    }
    return state.LastCollectTime, true
}
```

```go
// AddExternalIncentiveId: 포인터 없이 value 저장
func (d *Deposit) AddExternalIncentiveId(incentiveId string) {
    if d.externalIncentives.Has(incentiveId) {
        return
    }
    d.externalIncentives.Set(incentiveId, ExternalIncentiveState{})
}
```

**외부 시그니처:** 모든 공개 메서드의 시그니처 변경 없음. 내부 구현만 변경.

### 2b. `*avl.Tree` → `avl.Tree` (value type)

**효과:** Deposit당 1 Object 제거.

**변경 원리:**

`avl.Tree`는 `struct { node *Node }` (8 bytes). 포인터를 제거하면 Deposit struct에 인라인으로 포함되어 별도 HeapItemValue Object가 불필요해진다.

`avl.Tree`의 모든 메서드는 포인터 receiver (`*Tree`)이므로, value type 필드에서도 `d.externalIncentives.Set(...)` 호출이 가능하다 (Gno가 addressable field에 대해 자동으로 포인터를 취함). `d`가 `*Deposit`이므로 `d.externalIncentives`는 addressable.

avl 패키지 문서에도 "The zero struct can be used as an empty tree" 라고 명시되어 있다.

**변경:**

```go
// Before
type Deposit struct {
    // ...
    externalIncentives *avl.Tree
    // ...
}

// After
type Deposit struct {
    // ...
    externalIncentives avl.Tree
    // ...
}
```

**nil 체크 제거:**

value type이므로 nil이 될 수 없다. 기존 nil 체크를 제거한다:

```go
// Before
func (d *Deposit) HasExternalIncentiveId(incentiveId string) bool {
    if d.externalIncentives == nil {
        return false
    }
    return d.externalIncentives.Has(incentiveId)
}

// After
func (d *Deposit) HasExternalIncentiveId(incentiveId string) bool {
    return d.externalIncentives.Has(incentiveId)
}
```

동일하게 `getOrCreateIncentiveState`, `getIncentiveState`, `RemoveExternalIncentiveId`, `GetExternalIncentiveIdList`, `IterateExternalIncentiveIds`에서 nil 체크 제거.

**NewDeposit:**

```go
// Before
func NewDeposit(...) *Deposit {
    return &Deposit{
        // ...
        externalIncentives: avl.NewTree(),  // 힙 할당
        // ...
    }
}

// After
func NewDeposit(...) *Deposit {
    return &Deposit{
        // ...
        // externalIncentives: zero value avl.Tree{} — 명시적 초기화 불필요
        // ...
    }
}
```

**Clone:**

```go
// Before (getter_utils.gno)
func cloneExternalIncentives(tree *avl.Tree) *avl.Tree {
    if tree == nil {
        return nil
    }
    cloned := avl.NewTree()
    tree.Iterate(...)
    return cloned
}

// After
func cloneExternalIncentives(tree *avl.Tree) avl.Tree {
    var cloned avl.Tree
    tree.Iterate("", "", func(key string, value any) bool {
        state := value.(ExternalIncentiveState)
        cloned.Set(key, ExternalIncentiveState{
            CollectedReward: state.CollectedReward,
            LastCollectTime: state.LastCollectTime,
        })
        return false
    })
    return cloned
}
```

Clone 호출부:

```go
// Before
externalIncentives: cloneExternalIncentives(d.externalIncentives),

// After
externalIncentives: cloneExternalIncentives(&d.externalIncentives),
```

### 2c. `*u256.Uint` → `u256.Uint` (value type) — liquidity

**효과:** Deposit당 1 Object 제거.

`u256.Uint`는 `[4]uint64` (32 bytes). STORAGE_AUDIT_REPORT에서 이미 P2 잔여 항목으로 식별된 필드.

**변경:**

```go
// Before
type Deposit struct {
    liquidity *u256.Uint
}
func (d *Deposit) Liquidity() *u256.Uint { return d.liquidity }
func (d *Deposit) SetLiquidity(liquidity *u256.Uint) { d.liquidity = liquidity }

// After
type Deposit struct {
    liquidity u256.Uint
}
func (d *Deposit) Liquidity() *u256.Uint { return &d.liquidity }
func (d *Deposit) SetLiquidity(liquidity *u256.Uint) { d.liquidity = *liquidity }
```

**Clone:**

```go
// Before
liquidity: d.liquidity.Clone(),

// After
liquidity: d.liquidity,  // value type이므로 복사됨
```

**NewDeposit:**

```go
// Before: liquidity *u256.Uint 파라미터 유지, 역참조로 저장
func NewDeposit(
    // ...
    liquidity *u256.Uint,
    // ...
) *Deposit {
    return &Deposit{
        liquidity: *liquidity,
        // ...
    }
}
```

**주의:** `Liquidity()` getter가 `*u256.Uint`를 반환하므로, 호출부에서 반환된 포인터를 통해 mutation하면 Deposit 내부 값이 직접 변경된다. 현재 코드에서 이 패턴이 사용되는지 확인 필요. 만약 읽기 전용으로만 사용된다면 안전. mutation하는 곳이 있으면 `SetLiquidity`를 사용하도록 변경.

---

## 3. 변경 파일 목록

| 파일 | 변경 내용 |
|------|-----------|
| `staker/deposit.gno` | struct 필드 타입 변경, accessor 내부 구현 변경, nil 체크 제거, NewDeposit/Clone 수정 |
| `staker/getter_utils.gno` | `cloneExternalIncentives` 시그니처/구현 변경 |

테스트 파일은 공개 시그니처가 변경되지 않으므로 수정 불필요할 가능성이 높다.
단, `NewDeposit` 호출부에서 `liquidity` 인자 타입이 유지되므로 호환됨.

---

## 4. 작업 순서

### Task 1: `*ExternalIncentiveState` → `ExternalIncentiveState`

1. `getOrCreateIncentiveState` 반환 타입을 value로 변경, 모든 setter에 `Set()` 호출 추가
2. `getIncentiveState` 반환 타입을 `(ExternalIncentiveState, bool)`로 변경
3. `AddExternalIncentiveId`에서 value type 저장
4. `cloneExternalIncentives`에서 value type 복사
5. 테스트 통과 확인

### Task 2: `*avl.Tree` → `avl.Tree`

1. `externalIncentives` 필드 타입 변경
2. 모든 nil 체크 제거
3. `NewDeposit`에서 `avl.NewTree()` 제거 (zero value 사용)
4. `Clone`에서 `cloneExternalIncentives` 호출 시 `&d.externalIncentives` 전달
5. 테스트 통과 확인

### Task 3: `*u256.Uint` → `u256.Uint` (liquidity)

1. `Liquidity()` 호출부에서 mutation 여부 확인 (`grep "Liquidity().*Set\|Liquidity().*Add"` 등)
2. 필드 타입 변경, getter/setter 수정
3. `NewDeposit`, `Clone` 수정
4. 테스트 통과 확인

### Task 4: 재측정

txtar 테스트로 storage/gas 비교.

---

## 5. 예상 효과

| 항목 | 제거되는 Object | 적용 범위 |
|------|----------------|-----------|
| `*ExternalIncentiveState` → value | N per Deposit | incentiveID당 1개 |
| `*avl.Tree` → value | 1 per Deposit | Deposit당 1개 |
| `*u256.Uint` → value | 1 per Deposit | Deposit당 1개 |
| **합계** | **2 + N per Deposit** | |

3개 external incentive가 있는 Deposit 기준: **5 Object 제거**.

StakeToken (3 externals) storage +8.2% 악화가 해소되고, 추가 절감이 예상됨.

---

## 6. 주의사항

### 6a. Value type ExternalIncentiveState의 Set-back 패턴

모든 mutation 경로에서 `d.externalIncentives.Set(id, state)` 호출이 누락되면 변경이 반영되지 않는 silent bug가 발생한다. setter 메서드 내부에서만 mutation이 일어나므로 범위가 한정적이지만, 향후 새 setter 추가 시 이 패턴을 준수해야 한다.

### 6b. avl.Tree value type의 addressability

`d.externalIncentives.Set(...)` 호출은 `d`가 `*Deposit` (포인터)일 때만 동작한다. `Deposit` value에 대해 호출하면 컴파일 에러가 발생한다. 현재 모든 Deposit 사용처가 `*Deposit`이므로 안전하나, value로 복사된 Deposit에서 tree를 수정하려 하면 문제가 될 수 있다.

### 6c. Liquidity pointer escape

`Liquidity()` getter가 `&d.liquidity`를 반환하므로, 호출부에서 이 포인터를 보관하고 나중에 사용하면 Deposit 내부 값에 대한 aliasing이 발생한다. 현재 코드에서 이 패턴이 안전한지 Task 3에서 반드시 확인.
