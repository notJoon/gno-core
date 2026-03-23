# decreaseLiquidity() Position 이중 저장 제거 계획

## Background

`position/v1/burn.gno`의 `decreaseLiquidity()`에서 동일 position이 **2회 저장**된다:

```
line 88:  p.mustUpdatePosition(params.positionId, *position)  // 1차 저장
line 147: p.mustUpdatePosition(params.positionId, *position)  // 2차 저장
```

1차 저장 시점의 position은 2차 저장에서 완전히 덮어쓰이므로, **1차 저장은 낭비**다. Position은 12 필드 struct이므로 직렬화 + hash + KV write 비용이 그대로 버려진다.

### 전체 저장 흐름

`decreaseLiquidity`는 내부에서 `collectFee()`를 먼저 호출한다:

```
collectFee 내부 저장 (position.gno:403)  ← 필요 (fee 수집 결과 반영)
  ↓
decreaseLiquidity에서 mustGetPosition으로 다시 로드 (burn.gno:22)
  ↓
burn 결과 반영 후 1차 저장 (burn.gno:88)   ← 낭비
  ↓
collect → slippage check → 토큰 전송 → tokensOwed 차감 → burned 플래그
  ↓
2차 저장 (burn.gno:147)                    ← 필요 (최종 상태)
```

**총 3회 저장 중 1회(1차 저장)가 불필요.**

---

## 1차 저장이 존재하는 이유 분석

1차 저장(line 88)과 2차 저장(line 147) 사이에 **slippage 체크**(line 106)가 있다:

```go
if isSlippageExceeded(...) {
    return ..., makeErrorWithDetails(errSlippage, ...)
}
```

슬리피지 실패 시 error를 반환하며 함수가 종료된다. 만약 1차 저장이 없으면, burn 결과(liquidity 감소, tokensOwed 증가, feeGrowth 업데이트)가 store에 반영되지 않은 상태로 남을 수 있다.

**그러나 이 우려는 불필요하다:**

호출자 (`position.gno:286-289`):

```go
positionId, liquidity, fee0, fee1, amount0, amount1, poolPath, err := p.decreaseLiquidity(params)
if err != nil {
    panic(err)  // ← 에러 시 panic → 트랜잭션 전체 롤백
}
```

Gno 트랜잭션은 **완전 원자적**이다. slippage error → panic → **모든 상태 변경 롤백** (collectFee의 저장, pool의 Burn/Collect 포함). 따라서 1차 저장은 정상 경로에서만 의미가 있는데, 정상 경로에서는 2차 저장이 덮어쓰므로 **어떤 경우에도 불필요**하다.

---

## 변경 내용

### `position/v1/burn.gno` — line 88 제거

**현재:**

```go
// line 80-88
position.SetTokensOwed0(tokensOwed0)
position.SetTokensOwed1(tokensOwed1)
position.SetFeeGrowthInside0LastX128(feeGrowthInside0LastX128)
position.SetFeeGrowthInside1LastX128(feeGrowthInside1LastX128)
position.SetLiquidity(newLiquidity)
position.SetToken0Balance(newToken0Balance)
position.SetToken1Balance(newToken1Balance)

p.mustUpdatePosition(params.positionId, *position)  // ← 제거 대상
```

**변경:**

```go
// line 80-87: 동일 (in-memory 수정)
position.SetTokensOwed0(tokensOwed0)
position.SetTokensOwed1(tokensOwed1)
position.SetFeeGrowthInside0LastX128(feeGrowthInside0LastX128)
position.SetFeeGrowthInside1LastX128(feeGrowthInside1LastX128)
position.SetLiquidity(newLiquidity)
position.SetToken0Balance(newToken0Balance)
position.SetToken1Balance(newToken1Balance)

// 1차 저장 제거 — 2차 저장(line 147)에서 최종 상태 저장
```

**나머지 코드(line 90-149)는 변경 없음.** 2차 저장(line 147)이 최종 상태를 저장한다.

### 변경이 필요하지 않는 것

| 항목 | 이유 |
|------|------|
| `collectFee()` 내부 저장 (position.gno:403) | 필요 — fee 수집 결과를 반영. decreaseLiquidity에서 mustGetPosition으로 다시 로드하므로 이 저장이 store에 있어야 함 |
| 2차 저장 (burn.gno:147) | 필요 — collect 결과 반영 + burned 플래그 |
| slippage error 경로 | panic으로 롤백되므로 position 상태 무관 |

---

## 주의사항

### position 변수의 참조 유효성

`mustGetPosition` (line 22)는 store에서 position을 가져와 **pointer**를 반환한다. 1차 저장 제거 후에도 이 pointer를 통한 in-memory 수정(line 80-87, 131-144)은 **동일 position 객체**를 가리키므로 유효하다.

2차 저장(line 147)에서 `*position` (역참조)으로 값을 store에 쓸 때, line 80-87과 131-144의 모든 수정이 반영된다.

### pl.Collect와 position 상태의 의존성

line 90-100의 `pl.Collect()`은 **pool realm의 상태**를 변경하며, position의 store 상태에 의존하지 않는다. Collect의 파라미터(`burn0`, `burn1`)는 line 51의 `pl.Burn()` 반환값에서 오므로, position의 store 저장 여부와 무관하다.

### collectFee 저장 → mustGetPosition 로드 의존성

`collectFee()` (position.gno:403)에서 position을 저장한 후, `decreaseLiquidity()`의 line 22에서 `mustGetPosition()`으로 **다시 로드**한다. 이 로드가 collectFee의 저장 결과를 반영하므로, collectFee의 저장은 제거하면 안 된다.

---

## Storage 측정

### 측정 테스트

| 테스트 파일 | 테스트 이름 | 측정 내용 |
|---|---|---|
| `position/storage_poisition_lifecycle.txtar` | `position_storage_poisition_lifecycle` | Mint → Swap → CollectFee → **DecreaseLiquidity** 전체 lifecycle |

> DecreaseLiquidity 단계의 STORAGE DELTA 감소를 확인.

### 워크플로우

```bash
export GNO_REALM_STATS_LOG=stderr

# Step 1: Baseline
go test -v -run TestTestdata/position_storage_poisition_lifecycle -timeout 5m ./gno.land/pkg/integration/

# Step 2: burn.gno line 88 제거

# Step 3: 재측정 및 비교
```

### 결과 기록 (2026-03-16)

**변경: `burn.gno` line 88 `p.mustUpdatePosition()` 1차 저장 제거**

#### Storage Delta

| 테스트 | 단계 | Before (bytes) | After (bytes) | 차이 |
|--------|------|----------------|---------------|------|
| position_lifecycle | DecreaseLiquidity | 14 | 14 | 0 |

> Storage delta 동일 — dirty tracking이 동일 키 중복 write를 dedup하므로 최종 bytes 변화 없음.

#### Gas Used

| 테스트 | 단계 | Before | After | 차이 |
|--------|------|--------|-------|------|
| position_lifecycle | DecreaseLiquidity | 55,369,375 | 55,234,188 | **-135,187** |

> 불필요한 직렬화 + hash 연산 제거로 gas ~135K 감소.

#### Per-realm breakdown (DecreaseLiquidity)

| Realm | Before (bytes) | After (bytes) |
|-------|----------------|---------------|
| `gno.land/r/gnoland/wugnot` | +16 | +16 |
| `gno.land/r/gnoswap/position` | -2 | -2 |
goe