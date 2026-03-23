# P0: CollectReward reward=0 조기 반환

## 문제 정의

CollectReward는 reward가 0인 경우에도 내부 상태를 전부 조회하여 12회의 FinalizeRealmTransaction을 유발한다. 3~4번째 연속 호출에서는 12회 전부가 zero-byte finalize이므로, 데이터 변경 없이 gas만 소모하는 완전한 낭비이다.

현재 코드에서 reward 금액은 `calcPositionReward` 호출(staker.gno line 477) 이후에야 확정된다. 그러나 reward가 0으로 확정된 후에도 external incentive 순회, deposit 상태 갱신, GNS 토큰 전송 로직을 모두 거치며, 이 과정에서 KVStore의 MapValue가 반복적으로 dirty 표시된다.

## 로그에서 확인된 근거

CollectReward 4회 호출의 finalize 패턴:

```
Call gno.land/r/gnoswap/staker.CollectReward #1: zero-byte 9/12 (75%)
Call gno.land/r/gnoswap/staker.CollectReward #2: zero-byte 9/12 (75%)
Call gno.land/r/gnoswap/staker.CollectReward #3: zero-byte 12/12 (100%)
Call gno.land/r/gnoswap/staker.CollectReward #4: zero-byte 12/12 (100%)
```

3~4번째 호출의 12회 finalize 상세:

```
[updated] MapValue x1  bytes=+0    ← KVStore map 읽기에 의한 dirty
[updated] MapValue x1  bytes=+0    ← 반복
[updated] MapValue x1  bytes=+0    ← 반복
...
[updated] StructValue x1  bytes=+0  ← deposit 조회
[updated] StructValue x1  bytes=+0  ← 타임스탬프 확인
```

모든 finalize의 총 STORAGE DELTA = 0 bytes. 그러나 12회의 finalize 각각에서 MapValue 재직렬화 gas가 발생한다.

## 수정 대상 파일

| 파일 | 위치 | 설명 |
|------|------|------|
| `contract/r/gnoswap/staker/v1/staker.gno` | line 460-715 | CollectReward 함수 본체 |
| `contract/r/gnoswap/staker/v1/calculate_pool_position_reward.gno` | line 19 | calcPositionReward 함수 |

## 작업 계획

### 1단계: 수정 전 베이스라인 측정

```bash
cd gno.land/pkg/integration

GNO_REALM_STATS_LOG=/tmp/before_collectreward.log \
  go test -v -run "TestTestdata/staker_storage_staker_lifecycle" -timeout 600s \
  2>&1 | tee /tmp/before_collectreward_output.txt
```

CollectReward 관련 STORAGE DELTA 기록:

```bash
grep -A2 "CollectReward" /tmp/before_collectreward_output.txt | grep "STORAGE DELTA"
```

realm stats에서 CollectReward 구간의 finalize 횟수 확인:

```bash
python3 docs/realm-stats/analyze_realm_stats.py /tmp/before_collectreward.log
```

### 2단계: Reward 구조체의 전체 zero 판별 함수 추가

`contract/r/gnoswap/staker/v1/calculate_pool_position_reward.gno`에 Reward의 zero 판별 메서드를 추가한다.

```go
// IsZero returns true if all reward and penalty amounts are zero.
func (r Reward) IsZero() bool {
    if r.Internal != 0 || r.InternalPenalty != 0 {
        return false
    }
    for _, v := range r.External {
        if v != 0 {
            return false
        }
    }
    for _, v := range r.ExternalPenalty {
        if v != 0 {
            return false
        }
    }
    return true
}
```

### 3단계: CollectReward에 조기 반환 로직 삽입

`contract/r/gnoswap/staker/v1/staker.gno`의 CollectReward 함수에서, `calcPositionReward` 호출 직후(line 477 이후)에 조기 반환을 추가한다.

수정 전 (line 477 부근):

```go
reward := s.calcPositionReward(blockHeight, currentTime, positionId)

// ... 이후 500줄 이상의 처리 로직 ...
```

수정 후:

```go
reward := s.calcPositionReward(blockHeight, currentTime, positionId)

// reward가 완전히 0이면 상태 변경 없이 즉시 반환
if reward.IsZero() {
    return "0", "0", make(map[string]int64), make(map[string]int64)
}

// ... 기존 처리 로직 ...
```

### 4단계: 조기 반환 시 부작용 검증

조기 반환 시 건너뛰게 되는 로직들의 부작용을 확인한다:

1. **deposit.lastCollectTime 갱신 (line 658-661)**: reward=0이어도 마지막 수집 시간을 기록해야 하는지 확인한다. reward=0이면 수집한 것이 없으므로 갱신할 필요가 없다.

2. **en.MintAndDistributeGns (line 470)**: 이 호출은 reward 계산 **이전**에 실행된다. 조기 반환을 calcPositionReward 이후에 삽입하므로, emission 분배는 정상 수행된다.

3. **이벤트 발행 (line 678-714)**: reward=0일 때 이벤트를 발행할 필요가 있는지 확인한다. 프론트엔드에서 0-reward 이벤트를 기대하는 경우, 이벤트만 발행하고 상태 변경은 건너뛰는 방식을 고려한다.

4. **accumulators 업데이트 (line 682-691)**: pool accumulator 조회는 이벤트 데이터용이므로 reward=0이면 불필요하다.

**주의**: `en.MintAndDistributeGns(cross)` 호출이 calcPositionReward 이전에 있으므로, 이 호출 자체의 finalize는 조기 반환으로 제거할 수 없다. 그러나 이 호출 이후의 모든 KVStore 접근에 의한 zero-byte finalize는 제거된다.

만약 MintAndDistributeGns도 reward=0일 때 건너뛰려면, reward 계산을 MintAndDistributeGns보다 먼저 수행하도록 순서를 변경해야 한다. 이는 emission 분배가 reward 계산 결과에 영향을 주는지 확인 후 결정한다.

### 5단계: 컴파일 확인

```bash
cd contract
gno build ./r/gnoswap/staker/
```

### 6단계: 수정 후 측정

```bash
cd gno.land/pkg/integration

GNO_REALM_STATS_LOG=/tmp/after_collectreward.log \
  go test -v -run "TestTestdata/staker_storage_staker_lifecycle" -timeout 600s \
  2>&1 | tee /tmp/after_collectreward_output.txt
```

### 7단계: 결과 비교

```bash
echo "=== BEFORE ==="
grep "STORAGE DELTA" /tmp/before_collectreward_output.txt
echo "=== AFTER ==="
grep "STORAGE DELTA" /tmp/after_collectreward_output.txt

echo "=== REALM STATS COMPARISON ==="
python3 docs/realm-stats/analyze_realm_stats.py /tmp/after_collectreward.log
```

검증 기준:
- CollectReward (reward=0)의 finalize 횟수가 12회 → 0~1회로 감소해야 한다
- CollectReward의 STORAGE DELTA가 0 bytes로 유지되어야 한다 (기존에도 0이었음)
- CollectReward의 GAS USED가 유의미하게 감소해야 한다
- reward > 0인 CollectReward의 동작이 변경되지 않아야 한다
- staker 컨트랙트와 관련된 모든 테스트가 PASS해야 한다

### 8단계: 결과 저장

```bash
cp /tmp/after_collectreward.log docs/realm-stats/after_collectreward.log
```

## 예상 효과

reward=0인 CollectReward 호출에서:
- finalize 횟수: 12회 → 0~1회 (MintAndDistributeGns의 finalize만 남을 수 있음)
- 절감 gas: 약 153,000~204,000 gas (MapValue 재직렬화 비용)

이 최적화는 코드 변경량이 매우 적고(IsZero 메서드 + if 조건문 1개) 위험도가 낮아, 즉시 적용 가능하다.

## 위험 요소

- `en.MintAndDistributeGns(cross)` 호출이 calcPositionReward보다 먼저 실행되므로, emission 분배의 finalize는 조기 반환으로 제거되지 않는다. 이 부분은 별도 최적화가 필요하다.
- reward=0인 경우에도 이벤트를 기대하는 외부 시스템이 있을 수 있다. 프론트엔드 인덱서의 요구사항을 확인해야 한다.
- lastCollectTime을 갱신하지 않으면, 다음 CollectReward 호출 시 동일한 기간에 대해 reward를 재계산하게 된다. 이는 reward=0이므로 결과에 영향을 주지 않지만, 불필요한 연산이 반복될 수 있다.
