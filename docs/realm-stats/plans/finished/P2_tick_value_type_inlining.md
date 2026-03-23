# P2: Staker Tick 포인터 → 값 타입 변환

## 개요

Staker의 `Tick` 구조체가 가진 `*u256.Uint`와 `*i256.Int` 포인터 필드를 값 타입(`u256.Uint`, `i256.Int`)으로 변환하여 Gno VM의 HeapItemValue Object 생성을 제거한다.

### 배경

Gno VM은 구조체의 포인터 필드(`*T`)에 대해 별도의 HeapItemValue Object를 생성하여 독립적으로 직렬화/저장한다. 값 타입(`T`)으로 변경하면 데이터가 부모 구조체의 직렬화에 인라인되어 Object 수가 줄어든다.

### 대상 필드

| 필드 | 현재 타입 | 변경 후 | 파일 위치 |
|---|---|---|---|
| `Tick.stakedLiquidityGross` | `*u256.Uint` | `u256.Uint` | `staker/pool.gno:618` |
| `Tick.stakedLiquidityDelta` | `*i256.Int` | `i256.Int` | `staker/pool.gno:621` |

> `Tick.outsideAccumulation`은 `*UintTree`로 복합 타입이므로 변환 대상에서 제외.

---

## 필요조건

1. `u256.Uint`와 `i256.Int`가 값 타입으로 복사 가능해야 함 (Go의 struct assignment)
2. `u256.Uint.Clone()`, `i256.Int.Clone()` 메서드가 값 수신자에서도 동작해야 함
3. `common.LiquidityMathAddDelta()` 함수의 시그니처 확인 — 포인터를 반환하므로 역참조 필요

---

## 수정 사항

### 1. 구조체 정의 변경

**파일**: `examples/gno.land/r/gnoswap/staker/pool.gno` (라인 614~626)

```go
// Before
type Tick struct {
	id                   int32
	stakedLiquidityGross *u256.Uint
	stakedLiquidityDelta *i256.Int
	outsideAccumulation  *UintTree
}

// After
type Tick struct {
	id                   int32
	stakedLiquidityGross u256.Uint
	stakedLiquidityDelta i256.Int
	outsideAccumulation  *UintTree
}
```

### 2. Getter 변경 — 포인터 반환 유지 (Accessor 패턴)

기존 API 호환성을 위해 getter는 주소 연산자(`&`)로 포인터를 반환한다.

**파일**: `examples/gno.land/r/gnoswap/staker/pool.gno` (라인 641~658)

```go
// Before
func (t *Tick) StakedLiquidityGross() *u256.Uint {
	return t.stakedLiquidityGross
}

func (t *Tick) StakedLiquidityDelta() *i256.Int {
	return t.stakedLiquidityDelta
}

// After
func (t *Tick) StakedLiquidityGross() *u256.Uint {
	return &t.stakedLiquidityGross
}

func (t *Tick) StakedLiquidityDelta() *i256.Int {
	return &t.stakedLiquidityDelta
}
```

### 3. Setter 변경 — 역참조 대입

**파일**: `examples/gno.land/r/gnoswap/staker/pool.gno` (라인 646~658)

```go
// Before
func (t *Tick) SetStakedLiquidityGross(stakedLiquidityGross *u256.Uint) {
	t.stakedLiquidityGross = stakedLiquidityGross
}

func (t *Tick) SetStakedLiquidityDelta(stakedLiquidityDelta *i256.Int) {
	t.stakedLiquidityDelta = stakedLiquidityDelta
}

// After
func (t *Tick) SetStakedLiquidityGross(stakedLiquidityGross *u256.Uint) {
	t.stakedLiquidityGross = *stakedLiquidityGross
}

func (t *Tick) SetStakedLiquidityDelta(stakedLiquidityDelta *i256.Int) {
	t.stakedLiquidityDelta = *stakedLiquidityDelta
}
```

### 4. 생성자 변경

**파일**: `examples/gno.land/r/gnoswap/staker/pool.gno`

#### `NewTick()` (라인 684~691)

```go
// Before
func NewTick(tickId int32) *Tick {
	return &Tick{
		id:                   tickId,
		stakedLiquidityGross: u256.Zero(),
		stakedLiquidityDelta: i256.Zero(),
		outsideAccumulation:  NewUintTree(),
	}
}

// After
func NewTick(tickId int32) *Tick {
	return &Tick{
		id:                   tickId,
		stakedLiquidityGross: *u256.Zero(),
		stakedLiquidityDelta: *i256.Zero(),
		outsideAccumulation:  NewUintTree(),
	}
}
```

#### `Ticks.Get()` 인라인 생성 (라인 535~553)

```go
// Before (라인 539~542)
tick := &Tick{
	id:                   tickId,
	stakedLiquidityGross: u256.Zero(),
	stakedLiquidityDelta: i256.Zero(),
	outsideAccumulation:  NewUintTree(),
}

// After
tick := &Tick{
	id:                   tickId,
	stakedLiquidityGross: *u256.Zero(),
	stakedLiquidityDelta: *i256.Zero(),
	outsideAccumulation:  NewUintTree(),
}
```

### 5. Clone 변경

**파일**: `examples/gno.land/r/gnoswap/staker/pool.gno` (라인 671~682)

```go
// Before
func (t *Tick) Clone() *Tick {
	if t == nil {
		return nil
	}
	return &Tick{
		id:                   t.id,
		stakedLiquidityGross: t.stakedLiquidityGross.Clone(),
		stakedLiquidityDelta: t.stakedLiquidityDelta.Clone(),
		outsideAccumulation:  t.outsideAccumulation.Clone(),
	}
}

// After
func (t *Tick) Clone() *Tick {
	if t == nil {
		return nil
	}
	return &Tick{
		id:                   t.id,
		stakedLiquidityGross: *t.stakedLiquidityGross.Clone(),
		stakedLiquidityDelta: *t.stakedLiquidityDelta.Clone(),
		outsideAccumulation:  t.outsideAccumulation.Clone(),
	}
}
```

> **주의**: `Clone()` 반환 타입이 포인터(`*u256.Uint`)이므로 역참조(`*`)가 필요.
> `u256.Uint.Clone()`과 `i256.Int.Clone()`이 값 수신자에서 호출 가능한지 사전에 확인 필요.

### 6. `Ticks.SetTick()` 내 필드 접근

**파일**: `examples/gno.land/r/gnoswap/staker/pool.gno` (라인 560~567)

```go
func (t *Ticks) SetTick(tickId int32, tick *Tick) {
	if tick.stakedLiquidityGross.IsZero() {  // 값 타입이므로 직접 호출 가능 (변경 불필요)
		t.tree.Remove(EncodeInt(tickId))
		return
	}
	t.tree.Set(EncodeInt(tickId), tick)
}
```

`tick.stakedLiquidityGross`이 값 타입이 되면 `.IsZero()`가 값 수신자로 호출됨.
`u256.Uint.IsZero()`가 값 수신자에서 동작하는지 확인 필요. 만약 포인터 수신자만 지원하면:

```go
if tick.StakedLiquidityGross().IsZero() {  // getter로 포인터 얻어서 호출
```

### 7. v1 패키지 호출자 — 변경 불필요

**파일**: `examples/gno.land/r/gnoswap/staker/v1/reward_calculation_tick.gno`

호출 패턴이 모두 getter/setter를 통하므로 변경 불필요:

```go
// modifyDepositLower (라인 41)
self.SetStakedLiquidityGross(common.LiquidityMathAddDelta(self.StakedLiquidityGross(), liquidity))
// → StakedLiquidityGross()는 &t.stakedLiquidityGross 반환 (포인터)
// → SetStakedLiquidityGross()는 *stakedLiquidityGross로 역참조 대입
// → common.LiquidityMathAddDelta()는 *u256.Uint 반환 → setter가 받아서 처리
// ✅ 변경 불필요

// modifyDepositUpper (라인 50~54)
// 동일한 패턴 → ✅ 변경 불필요

// tickCrossHook (라인 136)
if tick.StakedLiquidityDelta().Sign() == 0 {  // getter가 포인터 반환 → ✅ 변경 불필요

// processBatchedTickCrosses (라인 244~245)
tick.StakedLiquidityGross(),   // getter가 포인터 반환 → ✅ 변경 불필요
tick.StakedLiquidityDelta(),   // getter가 포인터 반환 → ✅ 변경 불필요
```

---

## 수정 파일 목록

| 파일 | 변경 내용 |
|---|---|
| `examples/gno.land/r/gnoswap/staker/pool.gno` | Tick 구조체, getter, setter, 생성자, Clone, SetTick |

v1 패키지 파일은 getter/setter를 통해 접근하므로 **변경 불필요**.

---

## 주의사항

1. **`u256.Uint.Clone()` 수신자 타입 확인**: `Clone()`이 포인터 수신자(`func (z *Uint) Clone() *Uint`)인 경우, 값 타입 필드에서도 `t.stakedLiquidityGross.Clone()` 호출 시 Go가 자동으로 주소를 취하므로 동작함. 그러나 **Gno VM에서도 동일하게 동작하는지 확인 필요**.

2. **`IsZero()` 수신자 타입 확인**: `SetTick()`에서 `tick.stakedLiquidityGross.IsZero()` 직접 호출. 포인터 수신자만 지원하면 `tick.StakedLiquidityGross().IsZero()`로 변경.

3. **제로값 초기화**: `u256.Uint`의 제로값(`u256.Uint{}`)이 `u256.Zero()`와 동일한지 확인. 구조체가 기본 제로값으로 초기화될 때 의도하지 않은 동작 방지.

4. **Gno VM Object 직렬화**: 값 타입 전환 후 기존에 저장된 Object와의 호환성 문제 없음 (새로운 배포이므로 마이그레이션 불필요).

---

## 잠재적 사이드 이펙트

1. **메모리 복사 증가**: 값 타입은 대입 시 전체 구조체가 복사됨. `u256.Uint`는 내부적으로 `[4]uint64` (32 bytes)이므로 복사 비용은 미미하나, 빈번한 대입이 있는 hot path에서는 약간의 CPU 오버헤드 발생 가능.

2. **포인터 앨리어싱 제거**: 기존에는 `StakedLiquidityGross()`가 내부 필드의 포인터를 직접 반환하여 외부에서 수정하면 원본도 변경되었음. 값 타입 + accessor 패턴에서도 `&t.stakedLiquidityGross`로 내부 필드 주소를 반환하므로 **동일한 앨리어싱 동작 유지**.

3. **예상 절감 효과**: Tick당 2개의 HeapItemValue Object 제거. 각 Object는 최소 ~50-100 bytes의 메타데이터 + 직렬화 오버헤드를 가지므로, Tick이 많은 풀에서 유의미한 절감.

---

## 테스트 전략

1. 기존 txtar 테스트 실행으로 기능 회귀 확인:
   - `position_stake_position`
   - `staker_collect_reward_immediately_after_stake_token`
   - `pool_create_pool_and_mint`
   - `pool_swap_wugnot_gns_tokens`

2. STORAGE DELTA / GAS USED 측정으로 최적화 효과 검증

3. 특히 `tick cross` 경로에서의 정확성 확인 (swap 시 StakedLiquidityDelta 부호 반전)
