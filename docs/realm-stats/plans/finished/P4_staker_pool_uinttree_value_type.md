# P4-3: Staker Pool `*UintTree` × 4 + `Ticks.tree *avl.Tree` → value type 전환

**Priority:** 3
**예상 절감:** StakeToken -3~5% (~500-800 bytes), SetPoolTier -2~3%
**변경 범위:** `staker/pool.gno` 1파일
**패턴:** `UINT_TREE_MIGRATION.md`에서 검증된 동일 패턴
**의존:** P4-1, P4-2와 독립 (staker realm 범위)

---

## 문제 정의

Staker의 Pool struct에 4개의 `*UintTree`와 1개의 `*avl.Tree` 포인터가 남아있다:

```go
type Pool struct {
    poolPath string

    stakedLiquidity               *UintTree  // ← 포인터 (1 Object)
    lastUnclaimableTime           int64
    unclaimableAcc                int64
    rewardCache                   *UintTree  // ← 포인터 (1 Object)
    incentives                    Incentives
    ticks                         Ticks      // Ticks.tree → *avl.Tree (1 Object)
    globalRewardRatioAccumulation *UintTree  // ← 포인터 (1 Object)
    historicalTick                *UintTree  // ← 포인터 (1 Object)
}

type Ticks struct {
    tree *avl.Tree  // ← 포인터 (1 Object)
}
```

합계: Pool 당 **5개 Object** (UintTree ×4 + Ticks.tree ×1).

> 참고: UintTree 내부의 `*avl.Tree` → `avl.Tree` 전환은 이미 완료됨 (`UINT_TREE_MIGRATION.md`).
> 현재 남은 것은 Pool이 UintTree를 **포인터로 소유**하는 문제.

### 현재 Object 체인

```
Pool (Object)
  ├─ *UintTree (Object) → avl.Tree (inline) → nodes...    ×4
  └─ Ticks (inline)
       └─ *avl.Tree (Object) → nodes...                   ×1
```

전환 후:
```
Pool (Object)
  ├─ UintTree (inline) → avl.Tree (inline) → nodes...     ×4
  └─ Ticks (inline)
       └─ avl.Tree (inline) → nodes...                    ×1
```

5개 Object 제거.

---

## 사용 패턴 분석

### UintTree × 4 — getter 패턴

모든 필드가 동일한 패턴을 따른다:

```go
// 현재 getter (pointer 반환)
func (p *Pool) StakedLiquidity() *UintTree               { return p.stakedLiquidity }
func (p *Pool) RewardCache() *UintTree                    { return p.rewardCache }
func (p *Pool) GlobalRewardRatioAccumulation() *UintTree  { return p.globalRewardRatioAccumulation }
func (p *Pool) HistoricalTick() *UintTree                 { return p.historicalTick }
```

**사용 패턴 (v1/reward_calculation_pool.gno):**
```go
// 전형적 사용: getter → 메서드 체인
self.StakedLiquidity().ReverseIterate(0, currentTime, func(key int64, value any) bool { ... })
self.RewardCache().Set(currentTime, reward)
self.GlobalRewardRatioAccumulation().Set(currentTime, newAcc)
self.HistoricalTick().Set(currentTime, nextTick)
```

**nil 체크:** 사용 코드에서 **없음**. NewPool()에서 항상 초기화.

**Clone() 패턴:**
```go
func (p *Pool) Clone() *Pool {
    return &Pool{
        stakedLiquidity:               nil,  // ← 명시적 nil
        rewardCache:                   nil,
        globalRewardRatioAccumulation: nil,
        historicalTick:                nil,
        // ...
    }
}
```

Clone()은 UintTree 데이터를 복사하지 않고 nil로 설정. 이는 `getter.gno`의 외부 API에서 mutation 방지 목적.

### Ticks.tree — 직접 접근 패턴

```go
// 현재
type Ticks struct {
    tree *avl.Tree
}
func (t *Ticks) Tree() *avl.Tree     { return t.tree }
func (t *Ticks) SetTree(tree *avl.Tree) { t.tree = tree }

// 사용: pool.gno 내부에서 직접 접근
func (t *Ticks) Get(tickId int32) *Tick {
    v, ok := t.tree.Get(EncodeInt(tickId))  // *avl.Tree 메서드 호출
    // ...
}
```

**nil 체크:** 없음. `NewTicks()`에서 `avl.NewTree()` 로 초기화.

---

## 전환 계획

### Step 1: *UintTree × 4 → UintTree

```go
// BEFORE
type Pool struct {
    stakedLiquidity               *UintTree
    rewardCache                   *UintTree
    globalRewardRatioAccumulation *UintTree
    historicalTick                *UintTree
}
func (p *Pool) StakedLiquidity() *UintTree { return p.stakedLiquidity }
// ... (4개 getter 동일 패턴)

// AFTER
type Pool struct {
    stakedLiquidity               UintTree   // value type
    rewardCache                   UintTree   // value type
    globalRewardRatioAccumulation UintTree   // value type
    historicalTick                UintTree   // value type
}
func (p *Pool) StakedLiquidity() *UintTree { return &p.stakedLiquidity }
func (p *Pool) RewardCache() *UintTree     { return &p.rewardCache }
func (p *Pool) GlobalRewardRatioAccumulation() *UintTree { return &p.globalRewardRatioAccumulation }
func (p *Pool) HistoricalTick() *UintTree  { return &p.historicalTick }
```

**setter 수정 (존재하는 경우):**
```go
// BEFORE
func (p *Pool) SetStakedLiquidity(t *UintTree) { p.stakedLiquidity = t }

// AFTER
func (p *Pool) SetStakedLiquidity(t *UintTree) {
    if t == nil { p.stakedLiquidity = UintTree{}; return }
    p.stakedLiquidity = *t
}
```

**NewPool() 수정:**
```go
// BEFORE (pool.gno:195-201)
pool := &Pool{
    stakedLiquidity:               NewUintTree(),   // *UintTree
    rewardCache:                   NewUintTree(),   // *UintTree
    globalRewardRatioAccumulation: NewUintTree(),   // *UintTree
    historicalTick:                NewUintTree(),   // *UintTree
}

// AFTER
pool := &Pool{
    stakedLiquidity:               *NewUintTree(),  // UintTree (dereference)
    rewardCache:                   *NewUintTree(),
    globalRewardRatioAccumulation: *NewUintTree(),
    historicalTick:                *NewUintTree(),
}
// 또는:
pool := &Pool{
    stakedLiquidity:               UintTree{},
    rewardCache:                   UintTree{},
    globalRewardRatioAccumulation: UintTree{},
    historicalTick:                UintTree{},
}
```

**Clone() 수정:**
```go
// BEFORE
func (p *Pool) Clone() *Pool {
    return &Pool{
        stakedLiquidity:               nil,
        rewardCache:                   nil,
        globalRewardRatioAccumulation: nil,
        historicalTick:                nil,
    }
}

// AFTER — nil 대신 zero-value UintTree (빈 tree)
func (p *Pool) Clone() *Pool {
    return &Pool{
        stakedLiquidity:               UintTree{},
        rewardCache:                   UintTree{},
        globalRewardRatioAccumulation: UintTree{},
        historicalTick:                UintTree{},
    }
}
```

`UintTree{}`의 내부 `tree` 필드는 zero-value `avl.Tree{node: nil, size: 0}` — 빈 tree로서 Get(), Set(), Iterate() 등 모든 메서드가 정상 동작한다.

### Step 2: Ticks.tree *avl.Tree → avl.Tree

```go
// BEFORE
type Ticks struct {
    tree *avl.Tree
}
func (t *Ticks) Tree() *avl.Tree        { return t.tree }
func (t *Ticks) SetTree(tree *avl.Tree) { t.tree = tree }

// AFTER
type Ticks struct {
    tree avl.Tree  // value type
}
func (t *Ticks) Tree() *avl.Tree        { return &t.tree }
func (t *Ticks) SetTree(tree *avl.Tree) { t.tree = *tree }
```

**내부 직접 접근 수정:**

`Ticks.Get()`, `SetTick()`, `IterateTicks()` 내부에서 `t.tree.Get(...)` 등으로 직접 접근한다. `avl.Tree`의 모든 메서드가 pointer receiver이므로, value type 필드에서도 `(&t.tree).Get(...)` 자동 변환으로 **변경 불필요**.

**NewTicks() 수정:**
```go
// BEFORE
func NewTicks() Ticks {
    return Ticks{tree: avl.NewTree()}  // *avl.Tree
}

// AFTER
func NewTicks() Ticks {
    return Ticks{tree: avl.Tree{}}     // avl.Tree zero value
}
// 또는:
func NewTicks() Ticks {
    return Ticks{tree: *avl.NewTree()} // dereference
}
```

**Clone() 수정:**
```go
// BEFORE (pool.gno:582-593)
func (t Ticks) Clone() Ticks {
    cloned := avl.NewTree()   // *avl.Tree
    t.tree.Iterate("", "", func(key string, value any) bool {
        tick, ok := value.(*Tick)
        if !ok { panic("...") }
        cloned.Set(key, tick.Clone())
        return false
    })
    return Ticks{tree: cloned}
}

// AFTER
func (t Ticks) Clone() Ticks {
    cloned := avl.Tree{}
    t.tree.Iterate("", "", func(key string, value any) bool {
        tick, ok := value.(*Tick)
        if !ok { panic("...") }
        cloned.Set(key, tick.Clone())
        return false
    })
    return Ticks{tree: cloned}
}
```

---

## Aliasing 위험 분석

### UintTree value copy

`UintTree{tree: avl.Tree{node: *Node}}` — value copy 시 node 포인터 공유. 그러나:
- Pool 소유권이 단일 (store에서 관리)
- Clone()은 빈 UintTree를 반환 (node 공유 없음)
- getter가 `&p.stakedLiquidity`를 반환하므로 항상 원본 Pool의 필드를 직접 조작

**안전.**

### Ticks.tree value copy

동일 논리. Clone()에서 새 tree를 생성하여 iterate + deep copy하므로 안전.

---

## 예상 영향

| Operation | 현재 (B) | 예상 (B) | 절감 | % |
|-----------|---------|---------|------|---|
| StakeToken (stake only) | 17,803 | ~17,300 | ~503 | **-3%** |
| SetPoolTier | 21,179 | ~20,680 | ~499 | **-2%** |
| StakeToken (3 ext) | 31,295 | ~30,800 | ~495 | -2% |
| CollectReward | 5,712 | ~5,200 | ~512 | -9% |

> UintTree 당 ~100 bytes Object 제거. Pool 1개에 UintTree 4개 + Ticks.tree 1개 = 5 Objects ≈ 500 bytes.

---

## 변경 파일

| 파일 | 변경 내용 |
|------|----------|
| `staker/pool.gno` | Pool 필드 4개 + Ticks.tree 타입 변경 + getter/setter + NewPool() + Clone() + NewTicks() |

v1/ 코드는 getter를 통한 `*UintTree` 포인터 접근을 그대로 유지하므로 **변경 불필요**.

---

## 측정 방법

```bash
# staker lifecycle
go test ./gno.land/pkg/integration/ -run TestTxtar/staker_storage_staker_lifecycle -v

# staker stake only
go test ./gno.land/pkg/integration/ -run TestTxtar/staker_storage_staker_stake_only -v

# staker with externals
go test ./gno.land/pkg/integration/ -run TestTxtar/staker_storage_staker_stake_with_externals -v
```
