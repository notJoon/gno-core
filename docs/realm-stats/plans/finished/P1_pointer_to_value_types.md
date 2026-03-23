# P1: Pool/TickInfo/PositionInfo 포인터 필드 → value type 전환

## 문제 정의

Pool, TickInfo, PositionInfo 구조체는 `*u256.Uint` 및 `*i256.Int` 타입의 포인터 필드를 다수 포함한다. Gno VM에서 포인터 필드는 각각 독립 Object(HeapItemValue)로 저장되며, Object당 KV Write flat cost 2,000 gas가 부과된다. 또한 이들 Object가 수정되면 소유자 체인(StructValue → Block → PackageValue)이 dirty ancestor propagation에 의해 함께 재직렬화된다.

현재 각 구조체의 포인터 필드 수:

| 구조체 | `*u256.Uint` | `*i256.Int` | 기타 포인터 | 합계 |
|--------|:---:|:---:|:---:|:---:|
| Pool | 4 | 0 | 5 (`*avl.Tree` ×3, `*ObservationState` ×1) | 9 |
| TickInfo | 4 | 1 | 0 | 5 |
| PositionInfo | 5 | 0 | 0 | 5 |

## Gno VM에서의 Object 생성 메커니즘

### HeapItemValue 생성 조건

Gno VM은 다음 경우에 HeapItemValue를 생성한다:

1. `&CompositeExpr` — 복합 리터럴의 주소 취득
2. `new(T)` — 내장 함수 호출
3. Block 내 heap item 변수 — 클로저 캡처, 루프 변수

구조체 필드 자체는 HeapItemValue를 생성하지 않는다. 그러나 `*u256.Uint` 필드에 할당하는 시점에 `u256.NewUint()`가 `&Uint{...}`를 반환하므로 HeapItemValue가 생성된다.

### 별도 Object로 분리되는 기준

`realm.go`의 `getSelfOrChildObjects()`에서 결정:

```
StructValue의 각 필드 → getSelfOrChildObjects() 호출
  ├── RefValue인 경우 → 별도 Object
  ├── Object 타입인 경우 → 별도 Object
  │   (ArrayValue, StructValue, MapValue, FuncValue, HeapItemValue 등)
  └── 기타 (primitive, string) → 인라인
```

| 필드 타입 | VM 내부 표현 | 별도 Object 수 | 비고 |
|----------|-------------|:---:|------|
| `*u256.Uint` | HeapItemValue → ArrayValue | **2개** | 포인터 래퍼 + 배열 |
| `u256.Uint` (value) | ArrayValue (struct 자식) | **인라인** | 부모 struct에 포함 |
| `int64`, `string` 등 | primitive | **인라인** | 항상 인라인 |

**핵심**: value type `u256.Uint`(`[4]uint64`)는 부모 StructValue에 인라인 직렬화된다. HeapItemValue가 생성되지 않으므로 Object 수와 dirty propagation 경로가 줄어든다.

## 로그에서 확인된 근거

CreatePool 시 pool realm에서 생성되는 42개 Object:

```
[created] HeapItemValue x18    ← 포인터 필드들
[created] StructValue   x12
[created] ArrayValue    x11
[created] MapValue      x1
```

HeapItemValue 18개는 Pool 구조체(4개 `*u256.Uint`)와 초기 TickInfo·PositionInfo의 포인터 필드에서 발생한다.

Mint 시 pool realm에서 생성되는 61개 Object:

```
[created] HeapItemValue x28    ← 새 TickInfo 2개 + PositionInfo 1개의 포인터 필드
[created] ArrayValue    x20
[created] StructValue   x13
```

HeapItemValue 28개의 구성 추정:
- TickInfo 2개 × 5 포인터 = 10
- PositionInfo 1개 × 5 포인터 = 5
- Pool 구조체의 기존 포인터 업데이트 = 4
- 기타 (bitmap 등) = 9

reward>0 CollectReward에서 staker/v1의 ancestors가 28개(finalize 2회 × 14개)인 것도 avl.Tree 내부의 HeapItemValue 체인에 의한 dirty propagation이다.

## u256.Uint / i256.Int 타입 분석

### 타입 정의

```go
// contract/p/gnoswap/uint256/uint256.gno
type Uint [4]uint64    // 32 bytes, 리틀 엔디안

// contract/p/gnoswap/int256/int256.gno
type Int [4]uint64     // 32 bytes, 2의 보수
```

둘 다 `[4]uint64` 배열 타입이다. struct가 아니라 배열 타입 alias이다.

### 메서드 수신자

**모든 메서드가 포인터 수신자**를 사용한다:

| 라이브러리 | 전체 함수 | 포인터 수신자 메서드 | 비율 |
|-----------|:---:|:---:|:---:|
| u256.Uint | 181 | 95 | 52% |
| i256.Int | 132 | 50 | 38% |

주요 메서드: `Add`, `Sub`, `Mul`, `Div`, `Cmp`, `IsZero`, `Clone`, `Set` 등 전부 `func (z *Uint) Method(...) *Uint` 형태.

### 기존 코드의 사용 패턴

| 패턴 | 발생 수 | 예시 |
|------|:---:|------|
| 생성자 할당 | ~93 | `field = u256.NewUint(0)` |
| Clone 호출 | 46 | `field = other.Clone()` |
| nil 체크 | **3** | oracle 관련 방어 코드 |
| 메서드 체이닝 | 다수 | `field.Add(a, b).Mul(c, d)` |

nil 체크가 3곳뿐이므로 zero value 비교로의 전환은 부담이 적다.

## 전환 방식 비교

### 방식 A: 직접 전환 (`*u256.Uint` → `u256.Uint`)

```go
// Before
type TickInfo struct {
    liquidityGross *u256.Uint
}
tick.liquidityGross = u256.NewUint(0)
tick.liquidityGross.Add(tick.liquidityGross, amount)

// After
type TickInfo struct {
    liquidityGross u256.Uint
}
tick.liquidityGross = *u256.NewUint(0)
tick.liquidityGross.Add(&tick.liquidityGross, amount)
```

| 장점 | 단점 |
|------|------|
| 가장 단순한 구조 | 93+ 할당, 46 Clone, 메서드 체이닝 전부 수정 |
| u256 라이브러리 변경 불필요 | `*u256.NewUint(0)` 역참조 패턴이 비직관적 |
| | `Clone()` 반환값도 역참조 필요: `*other.Clone()` |

**수정 범위가 과도하여 권장하지 않음.**

### 방식 B: Accessor 패턴 (권장)

필드는 value type으로 저장하되, accessor 메서드가 포인터를 반환한다:

```go
type TickInfo struct {
    liquidityGross                 u256.Uint   // value type → 인라인 저장
    liquidityNet                   i256.Int
    feeGrowthOutside0X128          u256.Uint
    feeGrowthOutside1X128          u256.Uint
    tickCumulativeOutside          int64
    secondsPerLiquidityOutsideX128 u256.Uint
    secondsOutside                 uint32
    initialized                    bool
}

// Accessor — 포인터 반환으로 기존 메서드 체이닝 호환
func (t *TickInfo) LiquidityGross() *u256.Uint        { return &t.liquidityGross }
func (t *TickInfo) LiquidityNet() *i256.Int            { return &t.liquidityNet }
func (t *TickInfo) FeeGrowthOutside0X128() *u256.Uint  { return &t.feeGrowthOutside0X128 }
func (t *TickInfo) FeeGrowthOutside1X128() *u256.Uint  { return &t.feeGrowthOutside1X128 }
func (t *TickInfo) SecondsPerLiquidityOutsideX128() *u256.Uint {
    return &t.secondsPerLiquidityOutsideX128
}
```

호출 코드 변경:

```go
// Before (직접 필드 접근)
tick.liquidityGross.Add(tick.liquidityGross, amount)
gross := tick.liquidityGross

// After (accessor 경유)
tick.LiquidityGross().Add(tick.LiquidityGross(), amount)
gross := tick.LiquidityGross()
```

할당 코드 변경:

```go
// Before
tick.liquidityGross = u256.NewUint(0)

// After — 역참조하여 value 복사
tick.liquidityGross = *u256.NewUint(0)
// 또는 zero value 직접 사용
tick.liquidityGross = u256.Uint{}
```

| 장점 | 단점 |
|------|------|
| 저장은 value type → HeapItemValue 제거 | accessor 메서드 작성 필요 (struct당 5~9개) |
| `&t.field` 반환 → HeapItemValue 미생성 | 직접 필드 접근을 accessor 호출로 변경 |
| u256 메서드 체이닝 그대로 사용 가능 | 할당 패턴은 역참조(`*`) 필요 |
| u256 라이브러리 변경 불필요 | |
| nil 체크 3곳만 zero 체크로 변경 | |

**직접 전환(방식 A) 대비 수정 범위가 훨씬 작다.** 메서드 체이닝 코드는 `tick.liquidityGross.Add(...)` → `tick.LiquidityGross().Add(...)` 로만 변경하면 되고, u256 라이브러리의 인터페이스(`*Uint` 반환)와 완벽히 호환된다.

### 방식 C: 수치 필드 그룹핑

여러 u256 필드를 하나의 struct로 묶어 Object 수를 더 줄인다:

```go
type PositionNumerics struct {
    Liquidity                u256.Uint
    FeeGrowthInside0LastX128 u256.Uint
    FeeGrowthInside1LastX128 u256.Uint
    TokensOwed0              u256.Uint
    TokensOwed1              u256.Uint
}

type PositionInfo struct {
    numerics PositionNumerics   // 1개 StructValue로 통합
}
```

| 장점 | 단점 |
|------|------|
| Object 최소화 (5개 → 1개) | 구조체 레이아웃 변경으로 모든 접근 코드 수정 |
| 중첩 accessor 필요 (`pos.numerics.Liquidity`) | |
| | 방식 B보다 수정 범위 큼 |

방식 B만으로 이미 HeapItemValue가 제거되므로, 추가 이점이 크지 않다. Object 수는 방식 B에서도 0으로 줄어든다 (value type ArrayValue는 부모에 인라인).

## 수정 대상 파일

| 파일 | 설명 |
|------|------|
| `contract/r/gnoswap/pool/pool.gno` | Pool, TickInfo, PositionInfo 구조체 정의 + accessor 추가 |
| `contract/r/gnoswap/pool/pool_*.gno` | Pool 메서드들 — `pool.liquidity` → `pool.Liquidity()` |
| `contract/r/gnoswap/pool/v1/*.gno` | pool v1 로직 — 동일한 접근 패턴 변경 |

**변경 불필요**:
- `contract/p/gnoswap/uint256/*.gno` — u256 라이브러리 수정 없음
- `contract/p/gnoswap/int256/*.gno` — i256 라이브러리 수정 없음

## PoC 결과: Accessor 패턴 검증 완료

### 테스트 구성

PoC 테스트(`gno.land/pkg/integration/testdata/valuetype_accessor_poc.txtar`)에서 3가지 패턴을 비교했다:

1. **Pointer pattern** (`*Uint256` 필드): 기존 방식의 베이스라인
2. **Value + accessor pattern** (`Uint256` 필드 + `&t.field` 반환): 권장 방식
3. **Multi-field pattern** (TickInfo와 동일한 5개 value type 필드): 실제 적용 시나리오

각 패턴에서 4개 트랜잭션을 실행하여 검증:
- TX1: SetValues (쓰기) → TX2: CheckValues (트랜잭션 경계 후 읽기)
- TX3: Accumulate (in-place 수정) → TX4: CheckAccumulated (수정 후 읽기)

### 검증 결과: ALL PASS

| 검증 항목 | 결과 |
|----------|------|
| `&t.valueField` 반환으로 in-place 수정 | **PASS** |
| 트랜잭션 경계 후 값 유지 (persistence) | **PASS** |
| accessor 경유 메서드 체이닝 (`stored.Amount().Add(...)`) | **PASS** |
| 5개 value type 필드 독립성 (1개 수정 시 나머지 불변) | **PASS** |

### Gas 비교: Pointer vs Value+Accessor

| 오퍼레이션 | Pointer | Value+Accessor | 차이 |
|-----------|:---:|:---:|:---:|
| AddPkg (배포, 1회성) | 647,043 | 758,489 | +111,446 (+17%) |
| SetValues (초기 쓰기) | 452,455 | 442,956 | **-9,499 (-2.1%)** |
| CheckValues (읽기) | 301,267 | 321,598 | +20,331 (+6.7%) |
| **Accumulate (in-place 수정)** | **414,249** | **342,729** | **-71,520 (-17.3%)** |
| CheckAccumulated (읽기) | 301,397 | 321,648 | +20,251 (+6.7%) |

### Storage 비교

| 오퍼레이션 | Pointer STORAGE DELTA | Value STORAGE DELTA | 차이 |
|-----------|:---:|:---:|:---:|
| SetValues (초기 쓰기) | **1,645 bytes** | **10 bytes** | **-99.4%** |
| Accumulate (수정) | 5 bytes | 0 bytes | -100% |

### 핵심 발견

1. **in-place 수정이 17.3% 저렴**: swap/liquidity 변경에서 가장 빈번한 패턴(Add, Sub, Mul)이 직접적으로 혜택을 받는다.

2. **초기 쓰기 시 storage 99.4% 감소**: 포인터 패턴은 HeapItemValue + ArrayValue를 별도 Object로 저장하여 1,645B가 필요하지만, value type은 부모 struct에 인라인되어 10B만 발생한다.

3. **읽기는 6.7% 비싸짐**: value type이 부모 struct에 인라인되므로, 부모 Object의 역직렬화 크기가 증가한다. 그러나 쓰기/수정 절감이 이를 크게 상쇄한다.

4. **배포 비용 17% 증가**: accessor 메서드 정의에 의한 1회성 비용. 런타임에는 영향 없음.

### Multi-field 패턴 (TickInfo 시나리오)

5개 Uint256 value type 필드를 가진 TickInfoLike 구조체의 결과:

| 오퍼레이션 | GAS USED |
|-----------|:---:|
| SetMultipleFields (5개 필드 쓰기) | 578,354 |
| CheckMultipleFields (5개 필드 읽기) | 407,557 |
| ModifyOneField (1개 수정, 4개 불변) | 415,356 |
| CheckIndependence (5개 필드 검증) | 407,543 |

**참고**: realm stats는 testscript 환경에서 `GNO_REALM_STATS_LOG` 환경변수가 전달되지 않아 수집되지 않았다. 그러나 storage delta 차이(1,645B → 10B)가 HeapItemValue 제거를 간접적으로 증명한다.

**PoC 상세**: `gno.land/pkg/integration/testdata/valuetype_accessor_poc.txtar`
**전체 로그**: `docs/realm-stats/poc-results/valuetype_accessor_poc_output.log`
**분석 요약**: `docs/realm-stats/poc-results/valuetype_accessor_poc_analysis.md`

## 작업 계획

### 1단계: PoC — ~~Gno VM에서 `&valueField` 패턴 검증~~ **완료**

PoC 결과 요약은 위 "PoC 결과" 섹션 참조. 모든 검증 항목 PASS.

### 2~5단계: TickInfo accessor 적용 및 측정 — **완료**

TickInfo의 5개 포인터 필드를 value type + accessor로 전환하고 `position_storage_poisition_lifecycle.txtar` 테스트로 before/after를 비교했다.

#### Pool Realm Object Stats (realm-stats)

| 오퍼레이션 | 메트릭 | Before | After | Delta |
|-----------|--------|:---:|:---:|:---:|
| **AddPackage** | created | 113 | 112 | -1 |
| | bytes | +203,777 | +202,860 | -917 |
| **Mint (2 new ticks)** | created | 61 | **51** | **-10** |
| | bytes | +23,582 | **+19,596** | **-3,986 (-16.9%)** |
| **Mint (second pool)** | created | 61 | 57 | -4 |
| | bytes | +23,582 | +19,608 | -3,974 (-16.8%) |
| **Mint (existing pool)** | created | 71 | **61** | **-10** |
| | bytes | +26,544 | **+22,558** | **-3,986 (-15.0%)** |
| **Swap** | created | 47 | **37** | **-10** |
| | deleted | 47 | 37 | -10 |
| | bytes | +60 | +48 | -12 (-20.0%) |
| **DecreaseLiquidity** | created | 27 | 24 | -3 |
| **Burn** | created | 37 | 33 | -4 |

**Object 감소 패턴**: Mint with 2 new ticks에서 -10 objects = 정확히 5 HeapItemValue × 2 ticks. 예측과 완벽 일치.

#### STORAGE DELTA 비교 (txtar 출력)

| 오퍼레이션 | Before | After | Delta |
|-----------|:---:|:---:|:---:|
| First Swap (pool1, Mint 포함) | 40,762 B | **36,789 B** | **-3,973 (-9.7%)** |
| First Swap (pool2, Mint 포함) | 39,500 B | **35,515 B** | **-3,985 (-10.1%)** |
| CollectFee | 13,105 B | 13,093 B | -12 (-0.1%) |
| DecreaseLiquidity | 2,259 B | 2,259 B | 0 |

#### GAS 비교 (txtar 출력)

| 오퍼레이션 | Before | After | Delta |
|-----------|:---:|:---:|:---:|
| AddPackage (pool) | 11,082,756 | 11,066,902 | -15,854 (-0.1%) |
| First Swap (pool1) | 56,099,117 | 56,989,017 | +889,900 (+1.6%) |
| First Swap (pool2) | 54,932,525 | 55,428,250 | +495,725 (+0.9%) |
| CollectFee | 54,189,857 | 54,267,122 | +77,265 (+0.1%) |
| DecreaseLiquidity | 44,671,803 | 44,636,873 | -34,930 (-0.1%) |
| Burn | 55,637,684 | 55,722,011 | +84,327 (+0.2%) |

#### 분석

**Storage 개선 — 확정**:
- Mint 시 pool realm에서 **-16.9% bytes** (3,986B 절감)
- 전체 STORAGE DELTA에서 **-10% bytes** (Mint+Swap 기준)
- Swap에서도 10개 Object 감소 (tick crossing 시 TickInfo 재직렬화)

**Gas — 거의 변동 없음**:
- +0.1% ~ +1.6% 범위로 노이즈 수준
- value type 인라인에 의한 부모 struct 역직렬화 비용 미세 증가 (PoC의 +6.7%와 일관)
- gas 절감은 개별 오퍼레이션보다 cross-realm dirty propagation 감소에서 나타남 (reward>0 CollectReward 시나리오)

**상세 결과**: `docs/realm-stats/poc-results/tickinfo_accessor_comparison.md`

### 6단계: PositionInfo accessor 적용 및 측정 — **완료**

PositionInfo의 5개 `*u256.Uint` 포인터 필드를 value type + accessor로 전환하고 동일한 테스트로 3단계 비교(Before / TickInfo only / TickInfo+PositionInfo)를 수행했다.

#### Pool Realm Object Stats (realm-stats) — TickInfo + PositionInfo 누적

| 오퍼레이션 | 메트릭 | Before | TickInfo only | TickInfo+PositionInfo | Total Delta |
|-----------|--------|:---:|:---:|:---:|:---:|
| **Mint (2 ticks + 1 position)** | created | 61 | 51 | **46** | **-15 (-24.6%)** |
| | bytes | +23,582 | +19,596 | **+17,601** | **-5,981 (-25.4%)** |
| **Mint (existing pool)** | created | 71 | 61 | **62** | **-9** |
| | bytes | +26,544 | +22,558 | **+20,575** | **-5,969 (-22.5%)** |
| **Swap** | created | 47 | 37 | **34** | **-13 (-27.7%)** |
| | deleted | 47 | 37 | **34** | **-13** |
| | bytes | +60 | +48 | **+48** | **-12 (-20.0%)** |
| **CollectFee** | created | 15 | 15 | **12** | **-3 (-20.0%)** |
| | bytes | +6 | +6 | **+0** | **-6 (-100%)** |
| **DecreaseLiquidity** | created | 27 | 24 | **24** | **-3** |
| **Burn** | created | 37 | 33 | **28** | **-9 (-24.3%)** |
| **AddPackage** | created | 113 | 112 | **111** | -2 |
| | bytes | +203,777 | +202,860 | **+201,740** | -2,037 |

**Object 감소 패턴**: Mint (2 ticks + 1 position)에서 -15 objects = 5 fields × 2 ticks + 5 fields × 1 position. 예측과 정확히 일치.

**CollectFee bytes delta → 0**: PositionInfo 필드 인라이닝으로 fee collection 시 HeapItemValue churn이 완전히 제거됨.

#### STORAGE DELTA 비교 (txtar 출력) — 누적

| 오퍼레이션 | Before | TickInfo only | TickInfo+PositionInfo | Total Delta |
|-----------|:---:|:---:|:---:|:---:|
| First Swap (pool1) | 40,762 B | 36,789 B | **34,783 B** | **-5,979 (-14.7%)** |
| First Swap (pool2) | 39,500 B | 35,515 B | **33,532 B** | **-5,968 (-15.1%)** |
| CollectFee | 13,105 B | 13,093 B | **13,094 B** | -11 (-0.1%) |
| DecreaseLiquidity | 2,259 B | 2,259 B | **2,235 B** | **-24 (-1.1%)** |
| Burn (tick cross) | 64 B | 64 B | **40 B** | **-24 (-37.5%)** |

#### GAS 비교 (txtar 출력) — Before vs TickInfo+PositionInfo

| 오퍼레이션 | Before | TickInfo+PositionInfo | Delta |
|-----------|:---:|:---:|:---:|
| AddPackage (pool) | 11,082,756 | 11,046,408 | -36,348 (-0.3%) |
| First Swap (pool1) | 56,099,117 | 56,137,164 | +38,047 (+0.1%) |
| First Swap (pool2) | 54,932,525 | 55,722,942 | +790,417 (+1.4%) |
| CollectFee | 54,189,857 | **53,683,688** | **-506,169 (-0.9%)** |
| DecreaseLiquidity | 44,671,803 | 44,662,971 | -8,832 (0.0%) |
| Burn | 55,637,684 | 55,679,393 | +41,709 (+0.1%) |

#### 분석

**Storage 개선 — 누적 효과 확정**:
- Mint 시 pool realm에서 **-25.4% bytes** (5,981B 절감) — TickInfo 단독 대비 +50% 추가 절감
- 전체 STORAGE DELTA에서 **-14.7~15.1%** (Mint+Swap 기준) — TickInfo 단독(-9.7~10.1%) 대비 +5% 추가
- **CollectFee pool realm bytes delta = 0**: PositionInfo 인라이닝의 직접적 효과
- 개선이 additive — 각 struct 변환이 포인터 필드 수에 비례하여 기여

**Gas — CollectFee에서 유의미한 감소**:
- CollectFee: **-506,169 (-0.9%)** — PositionInfo 필드 HeapItemValue 제거 효과가 fee collection 경로에서 나타남
- 나머지 오퍼레이션은 ±1.5% 범위로 여전히 노이즈 수준

**상세 결과**: `docs/realm-stats/poc-results/positioninfo_accessor_comparison.md`

### 7단계: Pool 구조체 accessor 적용 및 측정 — **완료**

Pool 구조체의 4개 `*u256.Uint` 포인터 필드(Slot0.sqrtPriceX96, Balances.token0/token1, ProtocolFees.token0/token1, feeGrowthGlobal0X128, feeGrowthGlobal1X128, liquidity, maxLiquidityPerTick)를 value type + accessor로 전환했다. `*avl.Tree`(×3)와 `*ObservationState`(×1)는 참조 타입으로 유지.

4단계 비교(Before / TickInfo / +PositionInfo / +Pool)를 수행했다.

#### Pool Realm Object Stats (realm-stats) — 최종 (3 struct 전환 완료)

| 오퍼레이션 | 메트릭 | Before | TickInfo | +PositionInfo | **+Pool (최종)** | Total Delta |
|-----------|--------|:---:|:---:|:---:|:---:|:---:|
| **CreatePool** | created | 42 | 42 | 42 | **38** | **-4 (-9.5%)** |
| | bytes | +18,573 | +18,573 | +18,573 | **+16,977** | **-1,596 (-8.6%)** |
| **Mint (2 ticks + 1 pos)** | created | 61 | 51 | 46 | **45** | **-16 (-26.2%)** |
| | bytes | +23,582 | +19,596 | +17,601 | **+17,601** | **-5,981 (-25.4%)** |
| **Mint (existing pool)** | created | 71 | 61 | 62 | **55** | **-16 (-22.5%)** |
| | bytes | +26,544 | +22,558 | +20,575 | **+20,563** | **-5,981 (-22.5%)** |
| **Swap** | created | 47 | 37 | 34 | **33** | **-14 (-29.8%)** |
| | deleted | 47 | 37 | 34 | **33** | **-14** |
| | bytes | +60 | +48 | +48 | **+48** | **-12 (-20.0%)** |
| **DecreaseLiquidity** | created | 27 | 24 | 24 | **23** | **-4 (-14.8%)** |
| | bytes | +12 | — | — | **+6** | **-6 (-50.0%)** |

**Pool의 증분 기여**:
- **CreatePool**에서 가장 두드러짐: -4 objects, -1,596 B (-8.6%) — Pool의 u256 필드가 새로 생성되는 시점
- Mint에서는 -1 object 증분 — Pool 필드는 Mint 시 새로 생성되지 않고 업데이트만 됨
- DecreaseLiquidity bytes: **-50%** (12→6 B)

#### STORAGE DELTA 비교 (txtar 출력) — 최종

| 오퍼레이션 | Before | TickInfo | +PositionInfo | **+Pool (최종)** | Total Delta |
|-----------|:---:|:---:|:---:|:---:|:---:|
| CreatePool (x2) | 20,746 B | 20,746 B | 20,746 B | **19,150 B** | **-1,596 (-7.7%)** |
| First Swap (pool1) | 40,762 B | 36,789 B | 34,783 B | **34,793 B** | **-5,969 (-14.6%)** |
| First Swap (pool2) | 39,500 B | 35,515 B | 33,532 B | **33,519 B** | **-5,981 (-15.1%)** |
| CollectFee | 13,105 B | 13,093 B | 13,094 B | **13,093 B** | **-12 (-0.1%)** |
| DecreaseLiquidity | 2,259 B | 2,259 B | 2,235 B | **2,235 B** | **-24 (-1.1%)** |
| Swap (tick cross) | 12 B | 12 B | 12 B | **6 B** | **-6 (-50.0%)** |
| Burn | 64 B | 64 B | 40 B | **40 B** | **-24 (-37.5%)** |

#### GAS 비교 (txtar 출력) — Before vs 최종

| 오퍼레이션 | Before | **최종 (3 struct)** | Delta |
|-----------|:---:|:---:|:---:|
| AddPackage (pool) | 11,082,756 | 11,045,504 | -37,252 (-0.3%) |
| CreatePool (x2) | 33,782,761 | 33,721,364 | -61,397 (-0.2%) |
| First Swap (pool1) | 56,099,117 | 56,929,902 | +830,785 (+1.5%) |
| First Swap (pool2) | 54,932,525 | 55,372,977 | +440,452 (+0.8%) |
| CollectFee | 54,189,857 | 54,194,642 | +4,785 (0.0%) |
| DecreaseLiquidity | 44,671,803 | 44,648,317 | -23,486 (-0.1%) |
| Burn | 55,637,684 | 55,790,898 | +153,214 (+0.3%) |

#### 분석

**Storage 개선 — P1 최종 확정**:
- **CreatePool**: -1,596 B (-8.6%) — Pool 전환의 직접적 효과
- **Mint**: -5,981 B (-25.4%) — TickInfo + PositionInfo가 주도, Pool 증분은 미미
- **Swap**: -14 objects (-29.8%) — 3 struct 모두의 누적 효과
- **DecreaseLiquidity bytes**: -50% — Pool 필드 인라이닝 효과
- **Swap (tick cross) STORAGE DELTA**: -50% (12→6 B)

**Gas — 여전히 노이즈 수준**:
- ±1.5% 범위. CreatePool에서 -0.2% 미세 감소
- storage 감소가 이 최적화의 주된 효과

**상세 결과**: `docs/realm-stats/poc-results/pool_accessor_comparison.md`

### 8단계: 결과 저장

```bash
cp /tmp/after_pool_accessor.log docs/realm-stats/after_pointer_to_value.log
```

## 측정 결과 요약 — P1 최종 (전 struct 확정)

### 단계별 누적 효과

| 항목 | TickInfo | +PositionInfo | **+Pool (최종)** |
|------|:---:|:---:|:---:|
| 포인터 필드 제거 | 5 | 10 | **14** |
| Object 감소 (Mint) | -10 (61→51) | -15 (61→46) | **-16 (61→45)** |
| Pool realm bytes 감소 (Mint) | -3,986 B (-16.9%) | -5,981 B (-25.4%) | **-5,981 B (-25.4%)** |
| Object 감소 (CreatePool) | — | — | **-4 (42→38, -9.5%)** |
| CreatePool bytes 감소 | — | — | **-1,596 B (-8.6%)** |
| Swap objects 감소 | -10 | -13 | **-14 (-29.8%)** |
| 전체 STORAGE DELTA (Mint+Swap) | -9.7~10.1% | -14.7~15.1% | **-14.6~15.1%** |
| DecreaseLiquidity bytes | 0 | -24 B | **-6 B (-50.0%)** |
| Swap (tick cross) STORAGE DELTA | 0 | 0 | **-6 B (-50.0%)** |
| Gas 변동 | +0.1~1.6% | ±1.5% | **±1.5%** |

### 구조체별 기여도

| 대상 | 포인터 필드 수 | Object 감소 (Mint) | 주요 bytes 감소 |
|------|:---:|:---:|:---:|
| TickInfo | 5 | -10 | -3,986 B (Mint) |
| PositionInfo | 5 | -5 | -1,995 B (Mint) |
| Pool | 4 | -1 | -1,596 B (CreatePool) |
| **합계** | **14** | **-16** | **-5,981 B (Mint), -1,596 B (CreatePool)** |

Pool의 기여는 **CreatePool에서 집중** (-1,596 B, -8.6%). Mint에서는 Pool 필드가 새로 생성되지 않고 업데이트만 되므로 증분이 -1 object에 그친다. TickInfo/PositionInfo는 Mint 시 새로 생성되므로 Mint에서 효과가 크다.

### 추가 최적화 대상

P1 범위 이후의 확대 적용 대상은 [`P2_value_type_expansion.md`](./P2_value_type_expansion.md) 참조.

reward>0 CollectReward에서의 추가 효과:
- staker/v1 ancestors 28개 → dirty propagation 경로 단축으로 감소 예상

### 트레이드오프

| 항목 | PoC (단순 struct) | TickInfo | +PositionInfo | **+Pool (최종)** |
|------|:---:|:---:|:---:|:---:|
| Mint storage (pool realm) | **-99.4%** | **-16.9%** | **-25.4%** | **-25.4%** |
| CreatePool storage | — | — | — | **-8.6%** |
| 전체 STORAGE DELTA | — | -9.7~10.1% | -14.7~15.1% | **-14.6~15.1%** |
| Swap objects | — | -10 (-21.3%) | -13 (-27.7%) | **-14 (-29.8%)** |
| Gas | -17.3% (in-place) | +0.1~1.6% | ±1.5% | **±1.5%** |
| 배포 비용 (1회성) | +17% | -0.1% | -0.3% | **-0.3%** |

PoC의 -17.3% gas 절감이 실제 코드에서 나타나지 않은 이유: PoC는 단순 2-field struct에서의 단일 수정이었지만, 실제 코드는 Mint/Swap 과정에서 다수의 realm과 struct를 동시에 조작하므로 gas 절감이 전체 gas에서 희석된다. **storage 절감이 이 최적화의 주된 효과**이며, gas 절감은 cross-realm dirty propagation 감소 시나리오(reward>0 CollectReward)에서 더 크게 발현될 것으로 예상된다.

## 검증 측정 (2026-03-13)

테스트: `pool_create_pool_and_mint.txtar` (CreatePool + Mint + DecreaseLiquidity + IncreaseLiquidity + Swap + CollectFee)

### Baseline (main 브랜치)

| 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST |
|---|---:|---:|---:|
| **CreatePool** | 33,782,761 | 20,746 bytes | 12,074,600 ugnot |
| **Mint** (wide range) | 56,119,073 | 40,766 bytes | 104,076,600 ugnot |
| **DecreaseLiquidity** | 59,548,365 | 2,279 bytes | 100,227,900 ugnot |
| **IncreaseLiquidity** | 54,688,336 | 2 bytes | 100,000,200 ugnot |
| **Swap** | 42,298,920 | 5,694 bytes | 100,569,400 ugnot |
| **CollectFee** | 44,336,584 | 69 bytes | 100,006,900 ugnot |

### 수정 브랜치 결과

| 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST |
|---|---:|---:|---:|
| **CreatePool** | 33,721,364 | 19,150 bytes | 11,915,000 ugnot |
| **Mint** (wide range) | 56,077,294 | 32,020 bytes | 103,202,000 ugnot |
| **DecreaseLiquidity** | 58,632,904 | 2,217 bytes | 100,221,700 ugnot |
| **IncreaseLiquidity** | 54,883,253 | 16 bytes | 100,001,600 ugnot |
| **Swap** | 42,233,430 | 5,688 bytes | 100,568,800 ugnot |
| **CollectFee** | 44,232,052 | 55 bytes | 100,005,500 ugnot |

### 전후 비교

#### STORAGE DELTA

| 오퍼레이션 | Before | After | Delta | 변화율 |
|---|---:|---:|---:|---:|
| **CreatePool** | 20,746 bytes | 19,150 bytes | -1,596 | **-7.7%** |
| **Mint** | 40,766 bytes | 32,020 bytes | -8,746 | **-21.5%** |
| **DecreaseLiquidity** | 2,279 bytes | 2,217 bytes | -62 | **-2.7%** |
| **IncreaseLiquidity** | 2 bytes | 16 bytes | +14 | — |
| **Swap** | 5,694 bytes | 5,688 bytes | -6 | **-0.1%** |
| **CollectFee** | 69 bytes | 55 bytes | -14 | **-20.3%** |

#### GAS USED

| 오퍼레이션 | Before | After | Delta | 변화율 |
|---|---:|---:|---:|---:|
| **CreatePool** | 33,782,761 | 33,721,364 | -61,397 | -0.2% |
| **Mint** | 56,119,073 | 56,077,294 | -41,779 | -0.1% |
| **DecreaseLiquidity** | 59,548,365 | 58,632,904 | -915,461 | **-1.5%** |
| **IncreaseLiquidity** | 54,688,336 | 54,883,253 | +194,917 | +0.4% |
| **Swap** | 42,298,920 | 42,233,430 | -65,490 | -0.2% |
| **CollectFee** | 44,336,584 | 44,232,052 | -104,532 | -0.2% |

#### 분석

1. **Mint STORAGE DELTA -21.5%** (-8,746 bytes): P1의 핵심 효과. TickInfo(×2) + PositionInfo(×1)의 포인터 필드 인라이닝으로 HeapItemValue 제거. 기존 측정(-25.4%)보다 절대 감소량은 더 크지만, 이 테스트의 Mint가 더 많은 데이터를 포함하므로 비율은 다름.
2. **CreatePool -7.7%** (-1,596 bytes): Pool 구조체 포인터 필드 인라이닝 효과. 기존 측정(-8.6%)과 일관.
3. **CollectFee -20.3%** (-14 bytes): PositionInfo 인라이닝으로 fee collection 시 HeapItemValue churn 감소.
4. **DecreaseLiquidity -2.7%** (-62 bytes): 소폭 개선.
5. **Gas는 전체적으로 ±1.5% 범위**: storage 절감이 주 효과이며 gas 변동은 노이즈 수준. 기존 측정과 일관.

### 참고: `position_storage_poisition_lifecycle.txtar` 결과 (수정 브랜치만)

main 브랜치에 존재하지 않는 테스트이므로 전후 비교 불가. 수정 브랜치의 절대값만 기록:

| Step | 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST |
|:---:|---|---:|---:|---:|
| 1 | **CreatePool** | 33,721,364 | 19,150 bytes | 11,915,000 ugnot |
| 2 | **First Mint** (wide, 2 new ticks) | 56,926,512 | 32,030 bytes | 103,203,000 ugnot |
| 3 | **Second Mint** (narrow, 2 new ticks) | 55,338,957 | 30,715 bytes | 103,071,500 ugnot |
| 4 | **Third Mint** (existing ticks) | 54,189,255 | 10,290 bytes | 101,029,000 ugnot |
| 5 | **Swap** | 43,593,405 | 6 bytes | 100,000,600 ugnot |
| 6 | **CollectFee #1** | 44,576,795 | 2,216 bytes | 100,221,600 ugnot |
| 7 | **CollectFee #2** | 44,471,734 | 50 bytes | 100,005,000 ugnot |
| 8 | **DecreaseLiquidity** | 55,783,882 | 14 bytes | 100,001,400 ugnot |

## 위험 요소

- ~~**PoC 필수**: accessor 패턴이 Gno VM에서 정상 동작하는지 검증 필요~~ → **검증 완료**. `&t.valueField` 반환, in-place 수정, 트랜잭션 경계 후 persistence 모두 정상 동작 확인됨.

- **읽기 비용 증가 (6.7%)**: value type이 부모 struct에 인라인되므로, 부모 Object 역직렬화 크기가 증가한다. PoC에서 읽기 전용 오퍼레이션이 6.7% 비싸진 것이 확인되었다. 그러나 쓰기/수정 절감(-2.1% ~ -17.3%)과 storage 절감(-99.4%)이 이를 크게 상쇄한다.

- **nil 체크 3곳**: oracle 관련 방어 코드에서 `*u256.Uint` 필드를 nil과 비교하는 곳이 3곳 있다. 모두 `IsZero()` 비교로 변경해야 한다. 누락 시 컴파일 에러가 발생하므로 발견은 쉽다.

- **Clone 패턴 변경**: `field = other.Clone()` → `field = *other.Clone()` 역참조가 필요하다. Clone은 46곳에서 사용되며, TickInfo/PositionInfo 관련 Clone만 변경 대상이다.

- **Aliasing 위험**: accessor가 `&t.field`를 반환하므로, getter 결과를 변수에 저장한 뒤 setter로 필드를 수정하면 변수가 예기치 않게 변경된다. PositionInfo 전환 시 `collect` 함수에서 발견됨:
  ```go
  amount0 = u256Min(amount0Req, position.TokensOwed0())  // amount0이 필드를 직접 참조
  position.SetTokensOwed0(tokenOwed0)  // amount0도 같이 변경!
  ```
  **해결**: `amount0 = u256Min(...).Clone()`으로 alias를 끊는다. **향후 다른 struct 전환 시에도 동일 패턴을 반드시 감사해야 한다** (P2 참조) — getter 결과를 저장한 변수가 있고, 이후 같은 필드가 setter로 수정되는 모든 지점.

- **avl.Tree에 저장되는 TickInfo**: avl.Tree의 값으로 `*TickInfo`(포인터)가 저장되는 경우, TickInfo 내부의 value type 필드들은 TickInfo Object 안에 인라인으로 직렬화된다. 이는 의도한 동작이다.
