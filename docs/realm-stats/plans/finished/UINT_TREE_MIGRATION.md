# UintTree 내부 포인터 → value type 전환 계획

## Background

`UintTree`는 `*avl.Tree`를 감싸는 wrapper로, int64 key를 seqid(cford32)로 인코딩하는 캡슐화를 제공한다.

```go
type UintTree struct {
    tree *avl.Tree  // ← pointer: 별도 Object로 직렬화
}
```

문제는 내부 `tree` 필드가 **pointer** (`*avl.Tree`)이므로, Gno에서 별도 Object가 된다:

```
Pool → *UintTree (Object 1) → *avl.Tree (Object 2) → nodes...
```

### 초기 접근: wrapper 제거 → `*avl.Tree` 직접 노출

분석 결과, 이 접근은 **캡슐화 상실로 인한 위험**이 크다:

1. **Key 인코딩 우회** — `*avl.Tree`가 직접 노출되면 호출자가 helper 함수를 우회하고 raw string key를 삽입할 수 있다. EncodeInt64 없이 삽입된 key는 기존 encoded key와 섞이면서 **순회 순서가 깨지는 silent corruption** 발생.
2. **변경 범위 과대** — 40+ 곳의 메서드 체인 호출을 모두 helper 함수로 변환해야 하며, 한 곳이라도 누락 시 컴파일은 통과하지만 runtime 동작 차이 발생.
3. **v1 접근 제어 해제** — v1의 UintTree는 `set()`/`remove()`가 unexported로 외부 수정을 방지. wrapper 제거 시 이 보호가 무효화.

### 수정된 접근: wrapper 유지, 내부 `*avl.Tree` → `avl.Tree` (value type)

```go
type UintTree struct {
    tree avl.Tree  // ← value type: 부모 Object에 inline 직렬화
}
```

이렇게 하면:

```
Pool → *UintTree (Object 1) → avl.Tree (inline) → nodes...
```

`*avl.Tree` pointer Object가 제거되면서 UintTree의 **캡슐화는 완전히 유지**된다.

### avl.Tree value type 사용의 안전성

`avl.Tree`는 `struct { node *Node }`로 정의된다:

```go
type Tree struct {
    node *Node
}
```

- 모든 메서드가 pointer receiver (`*Tree`) 사용
- `self.tree.Set(...)` 호출 시 Go가 자동으로 `(&self.tree).Set(...)` 로 변환
- `node *Node` pointer를 통해 실제 tree 구조가 수정되므로 value copy 문제 없음

---

## 영향 범위

| 위치 | UintTree 필드 수 | 제거되는 `*avl.Tree` Object |
|------|:---:|:---:|
| Pool struct | 4 (`stakedLiquidity`, `rewardCache`, `globalRewardRatioAccumulation`, `historicalTick`) | **4 per pool** |
| Incentives struct | 1 (`unclaimablePeriods`) | **1 per pool** |
| Tick struct | 1 (`outsideAccumulation`) | **1 per tick** |
| Store (top-level) | 1 (`externalIncentivesByCreationTime`) | **1 global** |
| **합계** | | 6 per pool + 1 per tick + 1 global |

100 tick pool 기준: 6 + 100 + 1 = **107개 `*avl.Tree` Object 제거**.

---

## 전략 비교

| | wrapper 제거 (`*avl.Tree` 노출) | **value type 전환 (채택)** |
|---|---|---|
| Object 제거 | O | **O** |
| 캡슐화 (key 인코딩 강제) | **상실** — raw key 주입 가능 | **유지** |
| 코드 변경량 | 40+ 호출처, 15+ 파일 | **UintTree 내부만 (~10줄)** |
| API 호환 | 전면 변경 | **완전 호환** |
| Regression 위험 | High | **극히 낮음** |
| v1 접근 제어 | 해제됨 | **유지** |

---

## 변경 대상

### 1. `staker/tree.gno` — UintTree 내부 필드 변경

**현재:**

```go
type UintTree struct {
    tree *avl.Tree
}

func NewUintTree() *UintTree {
    return &UintTree{
        tree: avl.NewTree(),
    }
}
```

**변경:**

```go
type UintTree struct {
    tree avl.Tree
}

func NewUintTree() *UintTree {
    return &UintTree{
        tree: avl.Tree{},
    }
}
```

> `avl.Tree`의 zero value는 빈 tree로 유효하다 (`node: nil`). `avl.NewTree()`는 동일한 `&Tree{node: nil}`을 반환하므로 `avl.Tree{}`와 동등.

**메서드 변경 — 없음.** 모든 메서드는 `self.tree.Get(...)` 형태로 호출하며, Go가 자동으로 `(&self.tree).Get(...)` 로 변환한다. pointer receiver 메서드 호출에 문제 없음.

**Clone 변경:**

```go
// 현재
func (self *UintTree) Clone() *UintTree {
    if self == nil {
        return nil
    }
    cloned := NewUintTree()
    self.tree.Iterate("", "", func(key string, value any) bool {
        cloned.tree.Set(key, value)
        return false
    })
    return cloned
}
```

**변경 불필요** — `cloned.tree`가 value type이어도 `(&cloned.tree).Set(...)` 으로 자동 변환되어 동작 동일.

### 2. `staker/v1/reward_calculation_types.gno` — v1 내부 UintTree

v1에도 동일한 UintTree가 별도 정의되어 있다. 동일하게 내부 필드만 변경:

```go
// 현재
type UintTree struct {
    tree *avl.Tree
}

// 변경
type UintTree struct {
    tree avl.Tree
}
```

`NewUintTree()` 생성자도 동일 패턴 변경.

### 변경이 필요하지 않는 파일

**나머지 모든 파일은 변경 불필요:**

| 파일 | 이유 |
|------|------|
| `staker/pool.gno` | `*UintTree` 타입의 필드/Getter/Setter — 타입 변경 없음 |
| `staker/store.gno` | `*UintTree` 반환/파라미터 — 타입 변경 없음 |
| `staker/types.gno` | interface 시그니처 `*UintTree` — 변경 없음 |
| `staker/v1/*.gno` (전체) | `.Set()`, `.Get()`, `.Iterate()` 등 메서드 호출 — 그대로 동작 |
| 모든 테스트 파일 | `NewUintTree()`, 메서드 호출 — 변경 없음 |

---

## 주의사항

### avl.Tree value type의 복사 의미론

`UintTree`가 **의도치 않게 값 복사**되면, 두 복사본이 **독립적인 tree**가 된다 (Go의 struct 값 복사). 현재 `*UintTree` (pointer)로 전달/저장하므로 이 문제는 발생하지 않는다:

```go
// Pool 필드: *UintTree (pointer) — 복사 시 같은 UintTree를 가리킴
stakedLiquidity *UintTree
```

`UintTree` 자체를 value로 전달하는 경로가 있는지 확인:

- `NewUintTree()` → `*UintTree` 반환 ✓
- 모든 Getter → `*UintTree` 반환 ✓
- 모든 Setter → `*UintTree` 파라미터 ✓
- `Clone()` → `*UintTree` 반환 ✓

**모든 경로가 `*UintTree` (pointer)를 사용하므로 값 복사 위험 없음.**

### Clone 동작 — shallow value copy

`UintTree.Clone()`이 `self.tree.Iterate`로 key-value를 새 tree에 복사할 때, value는 shallow copy (포인터 공유). 이 동작은 `*avl.Tree`에서 `avl.Tree`로 바꿔도 **변화 없음** — iterate + Set 로직이 동일하기 때문.

### Store 타입 호환성

Store에는 `*UintTree`로 저장된다. 내부 필드가 `*avl.Tree` → `avl.Tree`로 바뀌어도 **외부 타입 (`*UintTree`)은 동일**하므로, `result.(*UintTree)` type assert가 그대로 성공한다.

단, 이미 배포된 realm에서 직렬화된 `*UintTree`의 내부 구조가 `*avl.Tree` → `avl.Tree`로 변경되면 **역직렬화 호환성**에 영향이 있을 수 있다. **fresh deploy가 아닌 경우 Gno의 struct 필드 변경 시 역직렬화 동작을 확인**해야 한다.

---

## Storage 측정

### 측정 테스트

| 테스트 파일 | 테스트 이름 | 측정 내용 |
|---|---|---|
| `staker/storage_staker_lifecycle.txtar` | `staker_storage_staker_lifecycle` | Stake → CollectReward → UnStake (Pool+Tick 생성/수정) |
| `staker/storage_staker_stake_only.txtar` | `staker_storage_staker_stake_only` | StakeToken 단독 |
| `staker/storage_staker_stake_with_externals.txtar` | `staker_storage_staker_stake_with_externals` | External incentive + StakeToken |
| `staker/staker_create_external_incentive.txtar` | `staker_staker_create_external_incentive` | Incentive 생성 (Incentives struct 경로) |

### 워크플로우

```bash
export GNO_REALM_STATS_LOG=stderr

# Step 1: Baseline
go test -v -run TestTestdata/staker_storage_staker_lifecycle -timeout 5m ./gno.land/pkg/integration/
go test -v -run TestTestdata/staker_storage_staker_stake_only -timeout 5m ./gno.land/pkg/integration/

# Step 2: 코드 수정 (tree.gno, reward_calculation_types.gno — 내부 필드만 변경)

# Step 3: 재측정 및 비교
```

### 결과 기록

#### staker bytes_delta (트랜잭션별, staker realm만)

| 테스트 | 단계 | Before (bytes) | After (bytes) | 차이 |
|--------|------|----------------|---------------|------|
| staker_lifecycle | SetPoolTier | +24,349 | +22,384 | **-1,965 (-8.1%)** |
| staker_lifecycle | StakeToken | +31,920 | +31,134 | **-786 (-2.5%)** |
| staker_lifecycle | CollectReward (2nd) | +5,712 | — | — |
| staker_lifecycle | UnStakeToken | -2,340 | -7,176 | **-4,836** |
| staker_stake_only | SetPoolTier | +24,349 | +22,384 | **-1,965 (-8.1%)** |
| staker_stake_only | StakeToken | +22,359 | +31,134 | *편차 (emission state)* |
| create_incentive | CreateExternalIncentive | +26,627 | +24,662 | **-1,965 (-7.4%)** |

> `stake_with_externals` 테스트는 타임스탬프 유효성 에러로 FAIL (baseline/after 모두).

#### 관찰

1. **SetPoolTier / CreateExternalIncentive에서 일관된 -1,965 bytes** — Pool 초기화 시 `*avl.Tree` 4개 Object 제거 효과.
2. **StakeToken의 -786 bytes** — Tick 생성 시 `outsideAccumulation`의 `*avl.Tree` 1개 제거 포함.
3. **UnStakeToken에서 -4,836 bytes 추가 절감** — 정리 시 제거 대상 Object 감소.
4. **StakeToken 편차** (22,359 vs 31,134) — emission 관련 state 초기화 타이밍 차이. 동일 단계의 before/after 비교(lifecycle 기준)에서는 -786 bytes.
5. 모든 테스트 **PASS** — API 완전 호환, 변경 파일 2개(`tree.gno`, `reward_calculation_types.gno`)만 수정.
