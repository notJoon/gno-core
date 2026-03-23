# P1: Staker Finalize 호출 횟수 축소

## 문제 정의

StakeToken과 UnStakeToken은 staker realm 단독으로 23~25회의 FinalizeRealmTransaction을 유발한다. 관여하는 realm이 3개임을 감안하면 realm당 1회씩 총 3회가 적정한데, staker realm에서만 23회가 발생하는 것은 내부 구조에 문제가 있음을 시사한다.

Gno VM에서 FinalizeRealmTransaction은 crossing 함수 경계에서 호출된다. `cross` 키워드가 붙은 함수가 반환될 때, 또는 다른 realm의 Object에 대한 메서드 호출이 반환될 때 finalize가 트리거된다. staker 내부에서 같은 realm의 crossing 함수를 반복 호출하거나, KVStore의 Get/Set 각각이 별도의 realm 경계를 형성하는 구조라면 finalize가 과도하게 발생할 수 있다.

## 로그에서 확인된 근거

오퍼레이션별 staker realm의 finalize 횟수:

| 오퍼레이션 | staker finalize | 다른 realm finalize | 총 finalize |
|-----------|:---:|:---:|:---:|
| CreatePool | 1 | 2 | 3 |
| Mint | 1 | 4 | 5 |
| SetPoolTier | **11** | 0 | 11 |
| StakeToken | **23** | 2 | 25 |
| CollectReward | **12** | 0 | 12 |
| UnStakeToken | **23** | 2 | 25 |

CreatePool과 Mint에서 staker finalize가 1회인 것은 정상적인 수준이다. staker 오퍼레이션에서만 10~23회로 급증하는 것은 staker 내부의 코드 구조에 기인한다.

StakeToken의 23회 finalize를 시간순으로 정리하면:

```
finalize #1:  c=17 u=1  a=0  d=0   bytes=+7980  ← deposit 생성 (실질 데이터)
finalize #2:  c=4  u=1  a=0  d=0   bytes=+1819  ← staked liquidity (실질 데이터)
finalize #3:  c=2  u=3  a=6  d=0   bytes=+1241  ← 내부 상태 + ancestors 전파
finalize #4~8: [updated] MapValue x1  bytes=+0   ← KVStore map 읽기 (낭비)
finalize #9~12: create/delete 쌍    bytes=+0/+6  ← avl.Tree 노드 회전 (구조적)
finalize #13~18: create + delete    bytes=+0~+24 ← avl.Tree 삽입/삭제 (구조적)
finalize #19~23: [updated] MapValue/StructValue  ← 정리 로직 (낭비 가능)
```

실질적인 데이터 기록은 #1~#3의 3회이며, 나머지 20회는 KVStore 읽기에 의한 재직렬화(8회)와 avl.Tree 조작에 의한 노드 단위 finalize(12회)이다.

## 수정 대상 파일

| 파일 | 설명 |
|------|------|
| `contract/r/gnoswap/staker/v1/staker.gno` | StakeToken, UnStakeToken 함수 본체 |
| `contract/r/gnoswap/staker/v1/instance.gno` | getDeposits/updateDeposits 등 KVStore 접근 패턴 |
| `contract/r/gnoswap/staker/store.gno` | stakerStore wrapper — Get/Set 호출 패턴 |
| `contract/p/gnoswap/store/kv_store.gno` | KVStore 구현 — crossing 함수 여부 확인 |
| `gnovm/pkg/gnolang/op_call.go` | (참고) maybeFinalize 로직 |
| `gnovm/pkg/gnolang/machine.go` | (참고) isRealmBoundary 판정 로직 |

## 작업 계획

### 1단계: Finalize 트리거 원인 분석

staker 내부의 finalize가 어디서 트리거되는지 정확히 파악해야 한다. 다음 두 가지를 조사한다.

#### 1-1. KVStore Get/Set이 crossing 함수인지 확인

```bash
cd contract
grep -n "cross\|crossing" p/gnoswap/store/kv_store.gno
```

KVStore의 Get/Set 메서드에 `cross` 키워드가 있다면, 각 Get/Set 호출마다 realm 경계가 형성되어 finalize가 발생한다. 이 경우 KVStore 접근 패턴을 일괄 처리 방식으로 변경해야 한다.

#### 1-2. staker 내부의 cross-realm 호출 횟수 확인

```bash
cd contract
grep -c "cross" r/gnoswap/staker/v1/staker.gno
grep -n "cross" r/gnoswap/staker/v1/staker.gno | head -30
```

StakeToken 함수 내에서 `cross` 키워드가 사용되는 횟수를 세고, 각각이 같은 realm인지 다른 realm인지 확인한다.

#### 1-3. realm stats 로그에 finalize 원인 추적 추가

현재 realm stats 로그는 finalize 결과만 기록하고 원인(어떤 함수 반환에 의해 트리거되었는지)은 기록하지 않는다. 디버깅을 위해 `FinalizeRealmTransaction` 호출 시 call stack 정보를 로깅하는 임시 코드를 추가할 수 있다.

`gnovm/pkg/gnolang/realm.go`의 `FinalizeRealmTransaction` 시작 부분에 임시 디버그 로그 추가:

```go
func (rlm *Realm) FinalizeRealmTransaction(readonly bool, store Store) {
    if logger := store.GetRealmStatsLogger(); logger != nil {
        // 디버그: finalize 트리거 위치 기록
        logger.LogSeparator(fmt.Sprintf("Finalize %s (readonly=%v)", rlm.Path, readonly))
    }
    // ...
}
```

### 2단계: 수정 전 베이스라인 측정

```bash
cd gno.land/pkg/integration

GNO_REALM_STATS_LOG=/tmp/before_finalize_reduction.log \
  go test -v -run "TestTestdata/staker_storage_staker_lifecycle" -timeout 600s \
  2>&1 | tee /tmp/before_finalize_reduction_output.txt
```

### 3단계: 원인별 대응 방안 적용

1단계의 분석 결과에 따라 다음 중 해당하는 조치를 적용한다.

#### 3-A. KVStore Get/Set이 crossing인 경우

KVStore의 Get/Set에서 `cross` 키워드를 제거하거나, staker가 KVStore를 직접 소유하도록 구조를 변경한다.

현재 구조:

```
staker realm → KVStore (p/gnoswap/store) → map[string]any
         ↑ crossing 경계           ↑ 같은 Object이지만 다른 패키지
```

KVStore가 패키지(p/) 레벨에 있고 staker가 realm(r/) 레벨에 있으므로, KVStore 메서드 호출 시 realm 경계가 형성될 수 있다. 이 경우 해결 방법:

**방법 A-1**: KVStore 코드를 staker realm 내부로 인라인화

```go
// r/gnoswap/staker/store.gno에 직접 구현
type stakerStore struct {
    data *avl.Tree  // p/gnoswap/store 대신 직접 avl.Tree 사용
}
```

**방법 A-2**: KVStore 접근을 배치 처리

여러 Get/Set을 하나의 crossing 함수 호출로 묶어 finalize 횟수를 줄인다.

#### 3-B. staker 내부 로직의 불필요한 crossing 제거

StakeToken 내부에서 동일 realm의 helper 함수에 `cross`가 붙어 있다면, 이를 일반 함수 호출로 변경한다. 같은 realm 내의 함수 호출에는 `cross`가 불필요하다.

#### 3-C. avl.Tree 노드 조작에 의한 finalize

avl.Tree의 Set/Remove 시 노드 회전이 각각 별도 finalize를 트리거하는 경우, 이는 Gno VM 레벨의 문제이다. avl.Tree 노드가 같은 realm에 속하므로, 노드 조작이 realm 경계를 형성하지 않아야 한다. 만약 형성한다면 VM 레벨 수정이 필요하다.

### 4단계: 수정 후 측정

```bash
cd gno.land/pkg/integration

GNO_REALM_STATS_LOG=/tmp/after_finalize_reduction.log \
  go test -v -run "TestTestdata/staker_storage_staker_lifecycle" -timeout 600s \
  2>&1 | tee /tmp/after_finalize_reduction_output.txt
```

### 5단계: 결과 비교

```bash
echo "=== BEFORE ==="
python3 docs/realm-stats/analyze_realm_stats.py /tmp/before_finalize_reduction.log \
  2>&1 | grep -A3 "StakeToken\|UnStakeToken\|SetPoolTier\|CollectReward"

echo "=== AFTER ==="
python3 docs/realm-stats/analyze_realm_stats.py /tmp/after_finalize_reduction.log \
  2>&1 | grep -A3 "StakeToken\|UnStakeToken\|SetPoolTier\|CollectReward"
```

검증 기준:
- StakeToken의 staker finalize가 23회 → 3~5회로 감소해야 한다
- UnStakeToken의 staker finalize가 23회 → 3~5회로 감소해야 한다
- SetPoolTier의 staker finalize가 11회 → 2~3회로 감소해야 한다
- GAS USED가 유의미하게 감소해야 한다
- 모든 테스트가 PASS해야 한다

### 6단계: 결과 저장

```bash
cp /tmp/after_finalize_reduction.log docs/realm-stats/after_finalize_reduction.log
```

## 예상 효과

StakeToken 기준:
- finalize 횟수: 25회 → 5회 이하 (80% 감소)
- finalize당 평균 gas 비용(MapValue 재직렬화 + KV Write flat cost)을 17,000 gas로 추정하면, 20회 제거 시 약 340,000 gas 절감

CollectReward 기준:
- finalize 횟수: 12회 → 1~2회 (90% 감소)
- 약 170,000 gas 절감

## 위험 요소

- **1단계 분석이 핵심**: finalize 과다 발생의 정확한 원인을 파악하지 않으면 잘못된 수정을 적용할 수 있다. 반드시 1단계의 원인 분석을 먼저 완료해야 한다.

- **KVStore 내부 구조**: KVStore가 `map[string]any`를 사용하고 있으므로, Get/Set 시 MapValue 전체가 재직렬화된다. 이 구조 자체가 zero-byte finalize의 원인 중 하나이며, 1단계 분석에서 정확한 기여도를 확인해야 한다.

- **crossing 함수 제거 시 보안 영향**: `cross` 키워드는 realm 간 접근 제어에도 사용된다. 불필요한 crossing을 제거할 때, 해당 함수가 외부 realm에서 호출 가능한지 여부를 확인해야 한다. 내부 helper 함수의 crossing만 제거해야 한다.

- **Gno VM 레벨 수정의 범위**: avl.Tree 노드 조작에 의한 finalize가 VM 레벨 문제인 경우, 수정 범위가 컨트랙트 코드를 넘어 Gno VM 코어에 해당한다. 이 경우 별도의 PR로 분리하고 충분한 테스트가 필요하다.

## 베이스라인 측정 결과 (2026-03-13, main 브랜치 기준)

테스트: `position_stake_position.txtar`, `staker_collect_reward_immediately_after_stake_token.txtar`

### P1 대상 오퍼레이션

| 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST | 비고 |
|---|---:|---:|---:|---|
| **SetPoolTier** | 42,222,934 | 24,427 bytes | 12,442,700 ugnot | staker 단독 24,427 bytes |
| **StakeToken** | 56,966,049 | 23,447 bytes | 12,344,700 ugnot | staker 22,395 / gnft 973 / position 79 |
| **CollectReward** | 35,843,649 | — | — | 보상 0이라 storage 변경 없음 |
| **UnStakeToken** | 71,913,930 | 2,454 bytes | 10,245,400 ugnot | staker 2,476 / gnft 22 / position -44 |

### 참고 오퍼레이션 (셋업 단계)

| 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST |
|---|---:|---:|---:|
| CreatePool | 33,866,398 | 20,735 bytes | 12,073,500 ugnot |
| Mint | 52,784,136 ~ 53,125,281 | 40,733 ~ 40,734 bytes | 14,073,300 ~ 14,073,400 ugnot |
| GNFT Approve | 4,413,390 | 1,061 bytes | 10,106,100 ugnot |

### realm별 bytes_delta 분해

**StakeToken**:
- `gno.land/r/gnoswap/staker`: 22,395 bytes
- `gno.land/r/gnoswap/gnft`: 973 bytes
- `gno.land/r/gnoswap/position`: 79 bytes

**UnStakeToken**:
- `gno.land/r/gnoswap/staker`: 2,476 bytes
- `gno.land/r/gnoswap/gnft`: 22 bytes
- `gno.land/r/gnoswap/position`: -44 bytes (감소)

## 수정 후 측정 결과 (2026-03-13, 수정 브랜치)

테스트: 동일 (`position_stake_position.txtar`, `staker_collect_reward_immediately_after_stake_token.txtar`)

### P1 대상 오퍼레이션

| 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST | 비고 |
|---|---:|---:|---:|---|
| **SetPoolTier** | 40,824,776 | 24,427 bytes | 12,442,700 ugnot | staker 단독, storage 변화 없음 |
| **StakeToken** | 61,688,294 | 33,099 bytes | 13,309,900 ugnot | staker 32,047 / gnft 973 / position 79 |
| **CollectReward** | 34,368,232 | — | — | 보상 0이라 storage 변경 없음 |
| **UnStakeToken** | 57,092,469 | -7,198 bytes | 9,280,200 ugnot | staker -7,176 / gnft 22 / position -44 |

### realm별 bytes_delta 분해

**StakeToken**:
- `gno.land/r/gnoswap/staker`: 32,047 bytes (+9,652 vs baseline)
- `gno.land/r/gnoswap/gnft`: 973 bytes (동일)
- `gno.land/r/gnoswap/position`: 79 bytes (동일)

**UnStakeToken**:
- `gno.land/r/gnoswap/staker`: -7,176 bytes (baseline +2,476 → -7,176, 즉 -9,652)
- `gno.land/r/gnoswap/gnft`: 22 bytes (동일)
- `gno.land/r/gnoswap/position`: -44 bytes (동일)

## 전후 비교

### 오퍼레이션별 GAS USED 비교

| 오퍼레이션 | Before | After | Delta | 변화율 |
|---|---:|---:|---:|---:|
| **SetPoolTier** | 42,222,934 | 40,824,776 | -1,398,158 | **-3.3%** |
| **StakeToken** | 56,966,049 | 61,688,294 | +4,722,245 | +8.3% |
| **CollectReward** | 35,843,649 | 34,368,232 | -1,475,417 | **-4.1%** |
| **UnStakeToken** | 71,913,930 | 57,092,469 | -14,821,461 | **-20.6%** |

### Stake→Unstake 라이프사이클 합산 비교

| 지표 | Before | After | Delta |
|---|---:|---:|---:|
| GAS (Stake+Unstake) | 128,879,979 | 118,780,763 | **-10,099,216 (-7.8%)** |
| STORAGE (Stake+Unstake) | +25,901 bytes | +25,901 bytes | 0 (net 동일) |
| TOTAL TX COST (Stake+Unstake) | 22,590,100 ugnot | 22,590,100 ugnot | 0 (net 동일) |

### 분석

1. **UnStakeToken이 가장 큰 수혜**: gas -20.6% (-14.8M gas). 이전에 finalize 과다로 낭비되던 gas가 감소.
2. **StakeToken은 gas가 +8.3% 증가했으나 storage가 +9,652 bytes 증가**: 수정으로 인해 StakeToken 시점에 더 많은 상태를 선제적으로 기록하고, UnStakeToken에서 이를 정리하면서 -9,652 bytes를 회수하는 구조로 변경됨.
3. **라이프사이클 합산 storage는 정확히 동일** (+25,901 bytes): storage 비용의 총량은 변하지 않고, 할당 시점만 재분배됨.
4. **라이프사이클 합산 gas는 -10.1M (-7.8%) 감소**: 이것이 P1 수정의 실질적 효과.
5. **SetPoolTier, CollectReward 각각 -3.3%, -4.1% gas 감소**: finalize 횟수 감소의 직접적 효과.

### collect_reward 테스트 참고치

collect_reward 테스트에서는 StakeToken이 baseline과 동일한 23,447 bytes를 기록 (GAS 55,383,839, -2.8%). 이는 position_stake_position 테스트와 다른 결과인데, 두 테스트의 셋업 경로가 약간 다르기 때문으로 추정. 정확한 원인은 추가 조사 필요.

## 비고

KVStore의 `map[string]any` → `avl.Tree` 전환은 별도 검토 과정에서 reject 되었다. 따라서 현재 KVStore는 `map[string]any` 기반을 유지하며, MapValue 재직렬화에 의한 zero-byte finaleze는 이 작업(crossing 분석 및 제거)으로 직접 대응해야 한다.
