# P4-5: ObservationState `map[uint16]*Observation` → `map[uint16]Observation` value 저장

**Priority:** 5 (탐색적)
**예상 절감:** Pool write 시 cardinality × ~100 bytes (cardinality=1 기준 ~100B, 10 기준 ~1KB)
**변경 범위:** `pool/oracle.gno`, `pool/v1/oracle.gno`
**패턴:** P4-4와 동일 (map value를 pointer에서 value type으로 전환)
**리스크:** Gno의 map 직렬화 동작에 대한 추가 검증 필요

---

## 문제 정의

`ObservationState`의 `observations` map이 `*Observation` 포인터를 값으로 저장한다:

```go
type ObservationState struct {
    observations    map[uint16]*Observation  // ← 포인터 값
    index           uint16
    cardinality     uint16
    cardinalityNext uint16
}
```

`Observation` struct는 이미 모든 필드가 value type:

```go
type Observation struct {
    blockTimestamp                    int64
    tickCumulative                    int64
    liquidityCumulative               u256.Uint  // value type (P1에서 전환 완료)
    secondsPerLiquidityCumulativeX128 u256.Uint  // value type
    initialized                       bool
}
```

map에 `*Observation`으로 저장하면 각 entry가 별도 HeapItemValue Object를 생성한다. `Observation`(value)으로 저장하면 이 Object가 제거된다.

### 영향 규모

| Cardinality | 제거 Object 수 | 예상 절감 |
|:-----------:|:-----------:|----------|
| 1 (기본) | 1 | ~100 bytes |
| 10 | 10 | ~1,000 bytes |
| 100 | 100 | ~10,000 bytes |

> 테스트 환경에서는 cardinality가 낮지만 (1~3), 프로덕션에서는 `increaseObservationCardinalityNext()`로 확장 가능.

---

## 사용 패턴 분석

### 읽기 (map 접근)

```go
// v1/oracle.gno:248 — observationAt()
ob := os.Observations()[index]  // *Observation

// v1/oracle.gno:237 — lastObservation()
ob := os.Observations()[os.Index()]  // *Observation
```

### 쓰기 (map 할당)

```go
// v1/oracle.gno:222 — writeObservation()
os.Observations()[nextIndex] = &Observation{...}  // *Observation 할당

// v1/oracle.gno:172 — grow()
os.Observations()[i] = observation  // *Observation 할당 (기존 entry 확장)
```

### getter/setter

```go
// oracle.gno:19
func (os *ObservationState) Observations() map[uint16]*Observation { return os.observations }

// oracle.gno:34
func (os *ObservationState) SetObservations(observations map[uint16]*Observation) {
    os.observations = observations
}
```

---

## 전환 계획

### Step 1: map 타입 변경

```go
// BEFORE
type ObservationState struct {
    observations map[uint16]*Observation
    // ...
}

// AFTER
type ObservationState struct {
    observations map[uint16]Observation  // value type
    // ...
}
```

### Step 2: getter/setter 변경

```go
// BEFORE
func (os *ObservationState) Observations() map[uint16]*Observation { return os.observations }
func (os *ObservationState) SetObservations(obs map[uint16]*Observation) { os.observations = obs }

// AFTER
func (os *ObservationState) Observations() map[uint16]Observation { return os.observations }
func (os *ObservationState) SetObservations(obs map[uint16]Observation) { os.observations = obs }
```

### Step 3: 쓰기 코드 수정

```go
// BEFORE (v1/oracle.gno:222)
os.Observations()[nextIndex] = &Observation{
    blockTimestamp:                    blockTimestamp,
    tickCumulative:                    tickCumulative,
    liquidityCumulative:               *liquidityCumulative,
    secondsPerLiquidityCumulativeX128: *secondsPerLiquidity,
    initialized:                       true,
}

// AFTER — & 제거
os.Observations()[nextIndex] = Observation{
    blockTimestamp:                    blockTimestamp,
    tickCumulative:                    tickCumulative,
    liquidityCumulative:               *liquidityCumulative,
    secondsPerLiquidityCumulativeX128: *secondsPerLiquidity,
    initialized:                       true,
}
```

### Step 4: 읽기 코드 수정

현재 읽기 코드에서 `*Observation`으로 받은 후 필드에 접근하는 패턴:

```go
// BEFORE
ob := os.Observations()[index]  // *Observation
ob.BlockTimestamp()             // pointer receiver 호출

// AFTER
ob := os.Observations()[index]  // Observation (value)
ob.BlockTimestamp()             // 여전히 동작 (Go는 value에서도 pointer receiver 호출 가능,
                                // 단 addressable한 경우에만)
```

**주의:** map에서 꺼낸 값은 Go에서 **not addressable**이다. 따라서 pointer receiver 메서드를 직접 호출할 수 없다. 해결 방법:

```go
// 방법 1: 로컬 변수에 할당 후 사용 (addressable)
ob := os.Observations()[index]
// ob는 로컬 변수 → addressable → pointer receiver 호출 가능

// 방법 2: getter를 value receiver로 변경 (Observation이 작으므로 성능 영향 미미)
func (o Observation) BlockTimestamp() int64 { return o.blockTimestamp }
```

현재 코드에서 map에서 꺼낸 결과를 **로컬 변수에 할당한 후** 사용하는 패턴이므로 방법 1이 적용됨.

### Step 5: NewObservationState / Clone 수정

```go
// BEFORE
func NewObservationState(currentTime int64) *ObservationState {
    os := &ObservationState{
        observations: make(map[uint16]*Observation),
        // ...
    }
    os.observations[0] = &Observation{...}
    return os
}

// AFTER
func NewObservationState(currentTime int64) *ObservationState {
    os := &ObservationState{
        observations: make(map[uint16]Observation),
        // ...
    }
    os.observations[0] = Observation{...}
    return os
}
```

---

## 리스크 및 검증 필요 사항

### Gno map 직렬화 동작

Gno VM에서 `map[uint16]*Observation`과 `map[uint16]Observation`의 직렬화 차이를 확인해야 한다:

1. **pointer value map:** 각 entry가 별도 Object (HeapItemValue + StructValue)
2. **value map:** 각 entry가 inline StructValue

이 가정이 맞는지 Gno VM의 `getSelfOrChildObjects()` 로직에서 확인 필요. map value가 StructValue인 경우 별도 Object로 분리될 가능성이 있다.

> P4-4 (ExternalIncentive value 저장)에서 avl.Tree의 interface{} value에 대한 결과를 먼저 확인한 후, 동일 원리가 map에도 적용되는지 검증하는 것이 안전하다.

### Observation의 in-place mutation

현재 `*Observation`으로 map에서 꺼낸 후 setter로 직접 수정하는 패턴이 있는지 확인:

```go
// grow() (v1/oracle.gno:172)
os.Observations()[i] = observation  // 전체 교체 (assignment)
```

전체 교체 패턴만 사용하므로, value type으로 전환해도 안전하다.

---

## 예상 영향

| Operation | Cardinality | 현재 (B) | 예상 (B) | 절감 |
|-----------|:-----------:|---------|---------|------|
| CreatePool | 1 | ~17,450* | ~17,350 | ~100 |
| ExactInSwapRoute (1st) | 1 | ~4,100* | ~4,000 | ~100 |
| Mint (tick cross + oracle) | 1~2 | ~31,100* | ~30,900 | ~200 |

> (*) P4-1 + P4-2 적용 후 기준

개별 절감은 작지만, cardinality가 증가할수록 선형적으로 효과가 커진다.

---

## 변경 파일

| 파일 | 변경 내용 |
|------|----------|
| `pool/oracle.gno` | ObservationState 타입, getter/setter, NewObservationState, Clone |
| `pool/v1/oracle.gno` | writeObservation, grow, observationAt, lastObservation 등 |

---

## 실행 조건

P4-4 (ExternalIncentive value 저장)의 결과를 먼저 확인하여 Gno VM의 value-in-container 동작을 검증한 후 착수 권장.

---

## 측정 방법

```bash
go test ./gno.land/pkg/integration/ -run TestTxtar/pool_create_pool_and_mint -v
go test ./gno.land/pkg/integration/ -run TestTxtar/pool_swap_wugnot_gns_tokens -v
```
