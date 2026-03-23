# P4-1: Pool Sub-struct 포인터 필드 → value type 전환

**Priority:** 1 (최고)
**예상 절감:** 모든 Pool write에서 ~500 bytes (Swap -8~10%, CollectFee -20%+)
**변경 범위:** `pool/pool.gno` 1파일, getter/setter/constructor/clone 수정
**패턴:** P1에서 검증된 accessor `return &t.field` 패턴과 동일

---

## 문제 정의

P1에서 Pool body 필드(`feeGrowthGlobal0X128`, `feeGrowthGlobal1X128`, `liquidity`, `maxLiquidityPerTick`)는 value type으로 전환 완료했다. 그러나 Pool의 sub-struct인 `Slot0`, `TokenPair` (Balances, ProtocolFees에 embedded)에는 아직 5개의 `*u256.Uint` 포인터가 남아있다.

### 현재 상태

```go
type Slot0 struct {
    sqrtPriceX96 *u256.Uint  // ← 포인터 (HeapItemValue 1개)
    tick         int32
    feeProtocol  uint8
    unlocked     bool
}

type TokenPair struct {
    token0, token1 *u256.Uint  // ← 포인터 (HeapItemValue 2개)
}

type Balances struct{ TokenPair }       // Pool.balances     → 2 Objects
type ProtocolFees struct{ TokenPair }   // Pool.protocolFees → 2 Objects
```

### Object 비용

| 필드 | 위치 | Object 수 | 비고 |
|------|------|:---:|------|
| `Slot0.sqrtPriceX96` | Pool.slot0 | **1** | 매 swap마다 변경 |
| `Balances.token0` | Pool.balances | **1** | 매 swap/mint/burn마다 변경 |
| `Balances.token1` | Pool.balances | **1** | 매 swap/mint/burn마다 변경 |
| `ProtocolFees.token0` | Pool.protocolFees | **1** | 매 swap마다 변경 |
| `ProtocolFees.token1` | Pool.protocolFees | **1** | 매 swap마다 변경 |
| **합계** | | **5** | 매 Pool write마다 5개 HeapItemValue |

P1 분석 기준으로 HeapItemValue 1개 = ~100 bytes 직렬화 오버헤드. 5개 제거 시 **~500 bytes/operation** 절감.

---

## 사용 패턴 분석

### Slot0.sqrtPriceX96

**읽기 (READ):**
- `swap.gno:432-452` — `validatePriceLimits()`에서 limit 비교
- `position.gno:253` — position 계산용 현재 가격 조회
- `getter.gno:176` — `GetSlot0SqrtPriceX96()` API 반환
- `type.gno:108,174` — SwapState 초기화, pool 연산

**쓰기 (MUTATE):**
- `swap.gno:411` — `slot0.SetSqrtPriceX96(result.NewSqrtPrice)` (swap 완료 후)

**패턴:** Slot0는 `pool.Slot0()`로 복사본을 가져와 수정 후 `pool.SetSlot0(slot0)`로 전체 교체하는 방식. sqrtPriceX96에 대한 in-place mutation은 없다.

```go
// swap.gno:410-413 — 현재 패턴
slot0 := pool.Slot0()
slot0.SetSqrtPriceX96(result.NewSqrtPrice)
slot0.SetTick(result.NewTick)
pool.SetSlot0(slot0)
```

### TokenPair (Balances, ProtocolFees)

**in-place mutation (주의 필요):**
```go
// swap.gno:394 — protocolFees의 Token0()에 직접 Add()
result.NewProtocolFees.Token0().Add(result.NewProtocolFees.Token0(), state.protocolFee)
```

이 패턴은 `Token0()`이 `*u256.Uint` 포인터를 반환하고, 그 포인터를 통해 in-place Add()를 수행한다. value type 전환 후에도 accessor가 `return &t.token0`을 반환하면 **동일하게 동작**한다.

**setter 사용:**
```go
// transfer.gno:62-64 — 새 값을 SetToken0으로 교체
poolBalances.SetToken0(newBalance)
poolBalances.SetToken1(newBalance)
```

**Clone() 사용:**
```go
// transfer.gno:122 — before-state 스냅샷
before := poolBalances.Token0().Clone()
```

---

## 전환 계획

### Step 1: Slot0 — sqrtPriceX96

```go
// BEFORE
type Slot0 struct {
    sqrtPriceX96 *u256.Uint
    tick         int32
    feeProtocol  uint8
    unlocked     bool
}
func (s *Slot0) SqrtPriceX96() *u256.Uint           { return s.sqrtPriceX96 }
func (s *Slot0) SetSqrtPriceX96(v *u256.Uint)       { s.sqrtPriceX96 = v }

// AFTER
type Slot0 struct {
    sqrtPriceX96 u256.Uint  // value type
    tick         int32
    feeProtocol  uint8
    unlocked     bool
}
func (s *Slot0) SqrtPriceX96() *u256.Uint           { return &s.sqrtPriceX96 }
func (s *Slot0) SetSqrtPriceX96(v *u256.Uint) {
    if v == nil {
        s.sqrtPriceX96 = u256.Uint{}
        return
    }
    s.sqrtPriceX96 = *v
}
```

**constructor 수정:**
```go
// BEFORE (pool.gno:312-324)
func newSlot0(sqrtPriceX96 *u256.Uint, tick int32, feeProtocol uint8, unlocked bool) Slot0 {
    return Slot0{
        sqrtPriceX96: sqrtPriceX96.Clone(),  // Clone() 반환 *u256.Uint
        // ...
    }
}

// AFTER
func newSlot0(sqrtPriceX96 *u256.Uint, tick int32, feeProtocol uint8, unlocked bool) Slot0 {
    return Slot0{
        sqrtPriceX96: *sqrtPriceX96,  // dereference로 값 복사
        // ...
    }
}
```

**Clone() 수정:**
```go
// BEFORE (pool.gno:207-211)
slot0: Slot0{
    sqrtPriceX96: p.slot0.sqrtPriceX96.Clone(),  // *u256.Uint
    // ...
}

// AFTER
slot0: Slot0{
    sqrtPriceX96: p.slot0.sqrtPriceX96,  // value copy (u256.Uint는 [4]uint64 배열)
    // ...
}
```

### Step 2: TokenPair — token0, token1

```go
// BEFORE
type TokenPair struct {
    token0, token1 *u256.Uint
}
func (t *TokenPair) Token0() *u256.Uint     { return t.token0 }
func (t *TokenPair) Token1() *u256.Uint     { return t.token1 }
func (t *TokenPair) SetToken0(v *u256.Uint) { t.token0 = v }
func (t *TokenPair) SetToken1(v *u256.Uint) { t.token1 = v }

// AFTER
type TokenPair struct {
    token0, token1 u256.Uint  // value type
}
func (t *TokenPair) Token0() *u256.Uint     { return &t.token0 }
func (t *TokenPair) Token1() *u256.Uint     { return &t.token1 }
func (t *TokenPair) SetToken0(v *u256.Uint) {
    if v == nil { t.token0 = u256.Uint{}; return }
    t.token0 = *v
}
func (t *TokenPair) SetToken1(v *u256.Uint) {
    if v == nil { t.token1 = u256.Uint{}; return }
    t.token1 = *v
}
```

**constructor 수정:**
```go
// BEFORE
func NewTokenPair(token0, token1 *u256.Uint) TokenPair {
    return TokenPair{token0: token0, token1: token1}
}

// AFTER
func NewTokenPair(token0, token1 *u256.Uint) TokenPair {
    var t0, t1 u256.Uint
    if token0 != nil { t0 = *token0 }
    if token1 != nil { t1 = *token1 }
    return TokenPair{token0: t0, token1: t1}
}
```

**Clone() 수정:**
```go
// BEFORE
func (t *TokenPair) Clone() TokenPair {
    return NewTokenPair(t.token0.Clone(), t.token1.Clone())
}

// AFTER
func (t *TokenPair) Clone() TokenPair {
    return TokenPair{token0: t.token0, token1: t.token1}  // value copy
}
```

**Pool-level setter 수정:**
```go
// BEFORE (pool.gno:122-128)
func (p *Pool) SetProtocolFeesToken0(token0 *u256.Uint) { p.protocolFees.token0 = token0 }
func (p *Pool) SetProtocolFeesToken1(token1 *u256.Uint) { p.protocolFees.token1 = token1 }

// AFTER
func (p *Pool) SetProtocolFeesToken0(token0 *u256.Uint) {
    if token0 == nil { p.protocolFees.token0 = u256.Uint{}; return }
    p.protocolFees.token0 = *token0
}
func (p *Pool) SetProtocolFeesToken1(token1 *u256.Uint) {
    if token1 == nil { p.protocolFees.token1 = u256.Uint{}; return }
    p.protocolFees.token1 = *token1
}
```

---

## Aliasing 위험 분석

### swap.gno:394의 in-place mutation

```go
result.NewProtocolFees.Token0().Add(result.NewProtocolFees.Token0(), state.protocolFee)
```

전환 후: `Token0()`은 `&t.token0`을 반환. `Add()`의 첫 번째 인수(receiver)와 두 번째 인수(a)가 동일 포인터. `u256.Uint.Add()` 구현이 `z[i] = a[i] + b[i]` 형태이므로 **self-add는 안전**하다 (`a == z` 허용).

### transfer.gno:122의 Clone 후 비교

```go
before := poolBalances.Token0().Clone()          // snapshot
poolBalances.SetToken0(u256.Zero().Add(poolBalances.Token0(), amount))
// before와 Token0() 비교
```

전환 후: `Token0()`은 `&t.token0`을 반환. `before`는 `.Clone()`으로 별도 복사본. `SetToken0()`으로 token0 필드가 덮어쓰기되므로 before와의 비교는 안전.

### Pool.Clone()에서의 value copy

`u256.Uint`는 `[4]uint64` 배열이므로 Go의 value semantics에 의해 자동으로 deep copy된다. Clone()에서 별도의 `.Clone()` 호출이 불필요.

**결론: aliasing 위험 없음.**

---

## 예상 영향

### Storage Delta

| Operation | 현재 (B) | 예상 (B) | 절감 | % |
|-----------|---------|---------|------|---|
| ExactInSwapRoute (1st) | 5,021 | ~4,500 | ~521 | **-10%** |
| ExactInSwapRoute (reverse) | 2,845 | ~2,350 | ~495 | **-17%** |
| ExactInSwapRoute (steady) | 33 | ~33 | 0 | 0% |
| Mint #1 (wide) | 32,019 | ~31,500 | ~519 | -2% |
| CollectFee #1 | 2,216 | ~1,720 | ~496 | **-22%** |
| CreatePool | 18,352 | ~17,850 | ~502 | -3% |

> steady swap은 동일 tick 내 swap이므로 Pool 자체의 dirty 패턴이 달라 절감이 작을 수 있다.

### Gas 영향

- P1 결과 참조: value type 전환은 gas에 **noise level (±1.5%)** 영향
- 읽기 시 부모 struct 역직렬화 비용 소폭 증가 (~6.7%)
- 쓰기 시 Object 수 감소로 상쇄

---

## 변경 파일

| 파일 | 변경 내용 |
|------|----------|
| `pool/pool.gno` | Slot0, TokenPair 필드 타입 + getter/setter/constructor/Clone |

v1/ 디렉토리의 사용 코드는 모두 getter/setter를 통해 접근하므로 **변경 불필요**.

---

## 측정 방법

```bash
# pool_create_pool_and_mint.txtar (Swap, Mint, CollectFee 포함)
go test ./gno.land/pkg/integration/ -run TestTxtar/pool_create_pool_and_mint -v

# swap lifecycle
go test ./gno.land/pkg/integration/ -run TestTxtar/pool_swap_wugnot_gns_tokens -v
```

Before/After 비교 시 `STORAGE_DELTA` 라인에서 Pool write 관련 바이트 변화를 확인.
