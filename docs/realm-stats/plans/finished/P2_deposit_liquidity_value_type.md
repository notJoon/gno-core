# P2: Deposit liquidity 포인터 → 값 타입 변환

## 개요

`Deposit` 구조체의 `liquidity *u256.Uint` 포인터 필드를 `liquidity u256.Uint` 값 타입으로 변환하여 HeapItemValue Object 1개를 제거한다.

### 배경

`Deposit.liquidity`는 생성자(`NewDeposit`)에서 한 번 설정된 후 `SetLiquidity()`로 변경되는 호출이 외부에 존재하지 않는다. 읽기 전용에 가까운 필드이므로 값 타입 변환의 부작용이 최소화된다.

### 대상 필드

| 필드 | 현재 타입 | 변경 후 | 파일 위치 |
|---|---|---|---|
| `Deposit.liquidity` | `*u256.Uint` | `u256.Uint` | `staker/deposit.gno:12` |

---

## 필요조건

1. `u256.Uint`가 값 타입으로 복사 가능해야 함
2. `Liquidity()` getter의 반환 타입을 포인터로 유지하여 API 호환성 보장
3. `NewDeposit()` 인자가 `*u256.Uint` 포인터를 받으므로 역참조 필요

---

## 수정 사항

### 1. 구조체 정의 변경

**파일**: `examples/gno.land/r/gnoswap/staker/deposit.gno` (라인 10~24)

```go
// Before
type Deposit struct {
	warmups                        []Warmup
	liquidity                      *u256.Uint  // 포인터
	targetPoolPath                 string
	// ...
}

// After
type Deposit struct {
	warmups                        []Warmup
	liquidity                      u256.Uint   // 값 타입
	targetPoolPath                 string
	// ...
}
```

### 2. Getter 변경 — Accessor 패턴

**파일**: `examples/gno.land/r/gnoswap/staker/deposit.gno` (라인 42~44)

```go
// Before
func (d *Deposit) Liquidity() *u256.Uint {
	return d.liquidity
}

// After
func (d *Deposit) Liquidity() *u256.Uint {
	return &d.liquidity
}
```

### 3. Setter 변경 — 역참조 대입

**파일**: `examples/gno.land/r/gnoswap/staker/deposit.gno` (라인 46~48)

```go
// Before
func (d *Deposit) SetLiquidity(liquidity *u256.Uint) {
	d.liquidity = liquidity
}

// After
func (d *Deposit) SetLiquidity(liquidity *u256.Uint) {
	d.liquidity = *liquidity
}
```

### 4. 생성자 변경

**파일**: `examples/gno.land/r/gnoswap/staker/deposit.gno` (라인 256~279)

```go
// Before
func NewDeposit(
	owner address,
	targetPoolPath string,
	liquidity *u256.Uint,
	currentTime int64,
	tickLower, tickUpper int32,
	warmups []Warmup,
) *Deposit {
	return &Deposit{
		// ...
		liquidity:                      liquidity,
		// ...
	}
}

// After
func NewDeposit(
	owner address,
	targetPoolPath string,
	liquidity *u256.Uint,
	currentTime int64,
	tickLower, tickUpper int32,
	warmups []Warmup,
) *Deposit {
	return &Deposit{
		// ...
		liquidity:                      *liquidity,  // 역참조
		// ...
	}
}
```

### 5. Clone 변경

**파일**: `examples/gno.land/r/gnoswap/staker/deposit.gno` (라인 234~254)

```go
// Before
func (d *Deposit) Clone() *Deposit {
	// ...
	return &Deposit{
		// ...
		liquidity:                      d.liquidity.Clone(),
		// ...
	}
}

// After
func (d *Deposit) Clone() *Deposit {
	// ...
	return &Deposit{
		// ...
		liquidity:                      *d.liquidity.Clone(),  // Clone()이 포인터 반환하므로 역참조
		// ...
	}
}
```

> **주의**: `d.liquidity`가 값 타입이 되면 `d.liquidity.Clone()` 호출 시 Go가 자동으로 주소를 취함. Gno VM에서도 동일하게 동작하는지 확인 필요.

---

## 호출자 분석 — v1 패키지

모든 호출자가 `Liquidity()` getter를 통해 접근하므로 **v1 패키지 변경 불필요**.

| 호출 위치 | 패턴 | 변경 필요 |
|---|---|---|
| `v1/staker.gno:864` | `deposit.Liquidity()` → 포인터 반환 | ❌ |
| `v1/reward_calculation_pool.gno:411` | `deposit.Liquidity()` → 포인터 반환 | ❌ |
| `v1/reward_calculation_pool.gno:426` | `deposit.Liquidity()` → 포인터 반환 | ❌ |
| `v1/getter.gno:268` | `deposit.Liquidity()` → 포인터 반환 | ❌ |

### NewDeposit 호출

| 호출 위치 | 인자 타입 | 변경 필요 |
|---|---|---|
| `v1/staker.gno` (StakeToken) | `*u256.Uint` 포인터 전달 | ❌ (함수 시그니처 유지) |

---

## 수정 파일 목록

| 파일 | 변경 내용 |
|---|---|
| `examples/gno.land/r/gnoswap/staker/deposit.gno` | Deposit 구조체, Liquidity(), SetLiquidity(), NewDeposit(), Clone() |

---

## 주의사항

1. **nil 포인터 체크 제거**: `d.liquidity`가 값 타입이 되면 nil이 될 수 없음. 기존 코드에서 `d.liquidity`의 nil 체크가 있다면 제거 필요. 현재 코드 분석 결과 `liquidity` 필드에 대한 직접적인 nil 체크는 없으므로 안전.

2. **함수 인자 시그니처 유지**: `NewDeposit()`의 `liquidity *u256.Uint` 인자는 포인터 그대로 유지. 함수 내부에서만 역참조하여 값 타입으로 복사.

3. **v1/reward_calculation_types.gno의 UintTree**: v1에도 `UintTree`와 동일한 `EncodeUint`/`DecodeUint`가 있으나 `Deposit.liquidity`와는 무관.

---

## 잠재적 사이드 이펙트

1. **메모리 레이아웃 변경**: `u256.Uint`는 `[4]uint64` = 32 bytes. 이 데이터가 `Deposit` 구조체에 인라인되므로 `Deposit` 자체의 크기가 포인터(8 bytes) → 값(32 bytes)으로 24 bytes 증가. 그러나 별도 HeapItemValue Object의 메타데이터 오버헤드(~50-100 bytes)가 사라지므로 **순 절감**.

2. **포인터 앨리어싱**: `Liquidity()`가 `&d.liquidity`를 반환하므로, 반환된 포인터를 통한 수정이 원본 Deposit에 반영됨. 기존 동작과 동일.

3. **SetLiquidity 미사용**: 현재 `SetLiquidity()`는 외부에서 호출되지 않음 (생성자에서만 설정). 그러나 향후 호환성을 위해 setter는 유지.

---

## 예상 절감 효과

- Deposit당 1개 HeapItemValue Object 제거
- Object당 약 50-100 bytes 메타데이터 절감
- `StakeToken` 시 새 Deposit 생성마다 절감 발생

---

## 테스트 전략

1. 기존 txtar 테스트로 기능 회귀 확인:
   - `position_stake_position`
   - `staker_collect_reward_immediately_after_stake_token`
   - `staker_staker_create_external_incentive`

2. STORAGE DELTA 측정으로 Deposit 생성 시 절감 효과 확인

3. `CollectReward`, `UnStakeToken` 경로에서 `Liquidity()` 반환값 정확성 확인
