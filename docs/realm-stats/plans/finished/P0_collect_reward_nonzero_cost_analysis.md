# P0: CollectReward reward>0 경로 상세 비용 분석

## 문제 정의

기존 `P0_collect_reward_early_return.md`는 reward=0인 경우의 낭비만 제거한다. 그러나 실제 사용자가 보상을 수령하는 reward>0 경로가 더 빈번하고 비용이 더 크다. 이 경로의 비용 구조를 정량적으로 분석하여, P1 최적화(finalize 횟수 축소, 포인터→value type 전환)의 적용 우선순위와 기대 효과를 확정해야 한다.

### 기존 분석의 한계

현재까지의 realm stats 데이터는 모두 **reward=0 상태**에서 수집되었다:

- `staker_storage_staker_lifecycle.txtar`: emission 미설정 → CollectReward가 항상 reward=0
- `staker_lifecycle_raw.log`: 위 테스트의 로그 → CollectReward 12회 finalize가 모두 zero-byte

reward>0일 때는 다음이 추가로 발생한다:

1. **incentive 잔액 차감** → KVStore Set (externalIncentives)
2. **deposit 수집 기록 갱신** → KVStore Set (deposits)
3. **GNS 토큰 전송** → gns realm cross-call → gns realm finalize
4. **외부 토큰 전송** → common.SafeGRC20Transfer cross-call → 해당 토큰 realm finalize
5. **totalEmissionSent 갱신** → KVStore Set (store)

이 추가 비용이 reward=0 대비 얼마나 증가하는지가 핵심 질문이다.

## 측정 방법: Unit Test

### txtar로 측정할 수 없는 이유

txtar 통합 테스트에서는 블록 시간을 조작할 수 없다. reward>0이 되려면 `StakeToken` 후 시간이 경과해야 emission이 누적되는데, txtar에서는 트랜잭션 간 시간이 거의 동일하므로 reward가 항상 0이다.

따라서 **unit test에서 시간을 mock하여 측정**해야 한다.

### 측정 대상 시나리오

다음 시나리오별로 gas/storage 비용을 비교한다:

| 시나리오 | 설명 | 목적 |
|---------|------|------|
| **A. reward=0 (베이스라인)** | StakeToken 직후 즉시 CollectReward | 기존 데이터 재확인 |
| **B. internal reward만 >0** | 시간 경과 후 CollectReward, external incentive 없음 | GNS 전송 비용 분리 측정 |
| **C. external reward 1개 >0** | external incentive 1개 등록 후 시간 경과 | 외부 토큰 전송 비용 분리 측정 |
| **D. external reward 3개 >0** | external incentive 3개 등록 후 시간 경과 | incentive 수 증가에 따른 비용 스케일링 |
| **E. internal + external 복합** | B + C 결합 | 실제 사용 패턴에 가장 가까운 시나리오 |

### 각 시나리오에서 측정할 메트릭

1. **finalize 횟수**: staker realm / 기타 realm 각각
2. **zero-byte finalize 횟수**: 낭비되는 재직렬화 식별
3. **Object 변경 내역**: created / updated / deleted 별 타입과 수
4. **STORAGE DELTA**: 바이트 단위 순 변경
5. **cross-realm 호출 횟수**: 어떤 realm이 몇 회 관여하는지
6. **GAS USED**: 총 가스 소모량

## 작업 계획

### 1단계: 기존 unit test 구조 파악

staker의 기존 CollectReward unit test를 분석하여 테스트 인프라를 이해한다.

확인 대상 파일:

| 파일 | 확인 사항 |
|------|----------|
| `contract/r/gnoswap/staker/v1/staker_test.gno` | 기존 CollectReward 테스트 패턴 |
| `contract/r/gnoswap/staker/v1/_helper_test.gno` | `mockInstanceCollectReward`, mock 설정 방법 |
| `contract/r/gnoswap/staker/v1/calculate_pool_position_reward_test.gno` | reward 계산 테스트, 시간 mock 방법 |

확인할 사항:

- `testing.SetRealm()`, `testing.NewCodeRealm()` 사용 패턴
- 시간/블록 조작 API 사용법 (아래 참조)
- emission이 실제로 발생하도록 설정하는 전제 조건
- `getMockInstance()` 초기화 과정에서 pool, position, emission 설정 순서

#### 사용 가능한 시간/블록 조작 API

`testing` 패키지(`gnovm/tests/stdlibs/testing/context_testing.gno`)에서 제공하는 함수:

| 함수 | 설명 |
|------|------|
| `testing.SkipHeights(count int64)` | 블록 높이를 `count`만큼, 시간을 `count × 5초`만큼 전진 |
| `testing.SetHeight(height int64)` | 블록 높이를 특정 값으로 직접 설정 |
| `testing.GetContext() Context` | 현재 Height, Time 등 전체 컨텍스트 조회 |
| `testing.SetContext(ctx Context) Context` | Height, Time 포함 전체 컨텍스트 직접 설정 |

`testing.SkipHeights(100)`은 블록 높이 +100, 시간 +500초를 동시에 적용한다. 별도의 시간만 전진하는 `TestSkipTime`은 현재 미구현이다.

**주의**: `std.TestSkipHeights()`는 존재하지 않는다. 반드시 `testing` 패키지의 `testing.SkipHeights()`를 사용해야 한다.

### 2단계: realm stats 계측 코드의 unit test 호환 여부 확인

현재 realm stats는 `GNO_REALM_STATS_LOG` 환경변수로 활성화되며, `FinalizeRealmTransaction` 내부에서 로깅된다. unit test에서도 이 로깅이 활성화되는지 확인한다.

확인 대상:

| 파일 | 확인 사항 |
|------|----------|
| `gnovm/pkg/gnolang/realm.go` | `FinalizeRealmTransaction`의 로깅 조건 |
| `gnovm/pkg/gnolang/store.go` | `GetRealmStatsLogger()` 구현 |
| `gno.land/pkg/integration/testscript_gnoland.go` | 통합 테스트에서 realm stats 활성화 방법 |

가능한 결과:

- **A) unit test에서도 realm stats 로그 수집 가능**: `GNO_REALM_STATS_LOG` 환경변수만 설정하면 됨 → 3단계로 진행
- **B) 통합 테스트 전용**: unit test의 Gno VM 인스턴스에서는 realm stats logger가 설정되지 않음 → 대안 필요

**대안 B-1**: unit test에서 `store.SetRealmStatsLogger()` 를 직접 호출하여 로거 주입
**대안 B-2**: realm stats 없이 GAS USED만 측정 (정밀도 낮음)
**대안 B-3**: Gno VM의 `Machine`을 직접 구성하는 Go 레벨 테스트 작성

### 3단계: reward>0 시나리오용 unit test 작성

기존 `_helper_test.gno`의 mock 인프라를 활용하여 시나리오 A~E의 테스트를 작성한다.

테스트 구조 (의사 코드):

```go
func TestCollectRewardCostAnalysis_InternalOnly(t *testing.T) {
    // 1. Setup: pool 생성, position mint, pool tier 설정
    setupPoolAndPosition(t)

    // 2. StakeToken
    mockInstanceStakeToken(positionId)

    // 3. 시간 경과 시뮬레이션 (emission 누적)
    testing.SkipHeights(100)  // 또는 해당 mock 함수

    // 4. CollectReward — 여기서 realm stats 수집
    poolPath, gnsReward, extRewards, extPenalties := mockInstanceCollectReward(positionId)

    // 5. 결과 검증
    // - gnsReward > "0" 확인
    // - realm stats 로그에서 finalize 패턴 분석
}
```

#### 시나리오별 설정 차이

**시나리오 A (reward=0, 베이스라인)**:
- StakeToken 직후 즉시 CollectReward
- `testing.SkipHeights(0)` 또는 건너뜀

**시나리오 B (internal only)**:
- pool tier 설정 (SetPoolTier) → emission 대상이 됨
- `testing.SkipHeights(100)` → emission 누적
- external incentive 없음

**시나리오 C (external 1개)**:
- `CreateExternalIncentive` 1회 호출
- `testing.SkipHeights(100)`

**시나리오 D (external 3개)**:
- `CreateExternalIncentive` 3회 호출 (서로 다른 토큰)
- `testing.SkipHeights(100)`

**시나리오 E (internal + external)**:
- pool tier 설정 + `CreateExternalIncentive` 1회
- `testing.SkipHeights(100)`

### 4단계: 테스트 실행 및 데이터 수집

```bash
cd contract

# realm stats 로그와 함께 실행
GNO_REALM_STATS_LOG=/tmp/collect_reward_nonzero.log \
  gno test -v ./r/gnoswap/staker/v1/ \
  -run "TestCollectRewardCostAnalysis" \
  -timeout 300s \
  2>&1 | tee /tmp/collect_reward_nonzero_output.txt
```

realm stats가 unit test에서 지원되지 않는 경우, GAS USED 비교만으로 진행:

```bash
gno test -v ./r/gnoswap/staker/v1/ \
  -run "TestCollectRewardCostAnalysis" \
  -timeout 300s
```

### 5단계: 데이터 분석

#### 5-1. realm stats 로그 분석 (가능한 경우)

```bash
python3 docs/realm-stats/analyze_realm_stats.py /tmp/collect_reward_nonzero.log
```

시나리오별로 다음 표를 채운다:

| 시나리오 | staker finalize | 기타 finalize | zero-byte | STORAGE DELTA | GAS USED |
|---------|:---:|:---:|:---:|:---:|:---:|
| A. reward=0 | 12 | 0 | 12 | 0 B | (측정) |
| B. internal only | ? | ? | ? | ? B | (측정) |
| C. external ×1 | ? | ? | ? | ? B | (측정) |
| D. external ×3 | ? | ? | ? | ? B | (측정) |
| E. internal + external | ? | ? | ? | ? B | (측정) |

#### 5-2. finalize 상세 분류

reward>0 시나리오의 finalize를 다음 카테고리로 분류한다:

1. **실질 데이터 기록**: deposit 갱신, incentive 잔액 차감 등 실제 bytes>0
2. **토큰 전송에 의한 cross-realm finalize**: gns.Transfer, SafeGRC20Transfer
3. **KVStore 읽기에 의한 zero-byte 재직렬화**: MapValue dirty marking
4. **avl.Tree 노드 조작**: create/delete 쌍

#### 5-3. 비용 증분 분석

reward=0 (시나리오 A) 대비 각 시나리오의 증분을 계산한다:

```
Δ(B→A) = internal reward 처리 비용 (GNS 전송 + deposit 갱신)
Δ(C→A) = external reward 1개 처리 비용
Δ(D→C) = external reward 추가 1개당 한계 비용
Δ(E→B) = internal + external 결합 시 추가 비용 (cross-realm 중복 여부)
```

### 6단계: P1 최적화 효과 예측

분석 결과를 바탕으로 P1 최적화가 reward>0 경로에 미치는 효과를 추정한다.

#### P1_reduce_finalize_calls 적용 시

- **제거 가능한 finalize**: 카테고리 3 (KVStore 읽기에 의한 zero-byte) 전부
- **제거 불가능한 finalize**: 카테고리 1 (실질 데이터), 카테고리 2 (토큰 전송)
- reward>0에서 카테고리 3이 차지하는 비율 = ?

#### P1_pointer_to_value_types 적용 시

- reward>0에서 deposit/incentive StructValue의 HeapItemValue 수 = ?
- value type 전환으로 제거 가능한 Object 수 = ?

#### 두 P1을 모두 적용한 복합 효과

- 예상 finalize 횟수: reward=0과 동일 수준(0~1회)까지 줄일 수 있는지?
- 예상 GAS USED 감소율: ?%

### 7단계: 결과 문서화

분석 결과를 이 문서에 업데이트하고, 다음을 포함한다:

1. 시나리오별 비용 비교 표 (확정 데이터)
2. finalize 상세 분류 결과
3. P1 적용 시 예상 효과 (정량적 추정)
4. 권장 최적화 순서 재검토

```bash
# 결과 로그 보존
cp /tmp/collect_reward_nonzero.log docs/realm-stats/collect_reward_nonzero.log
```

## CollectReward reward>0 경로의 코드 흐름

참고: `contract/r/gnoswap/staker/v1/staker.gno` line 460-715

```
CollectReward(positionId)
│
├── [사전 검증] halt 체크, 권한 검증
├── [KV Read] deposits.get(positionId)         ← MapValue dirty #1
├── [Cross-realm] en.MintAndDistributeGns()    ← emission finalize
│
├── reward := calcPositionReward(...)          ← 내부 KV Read 다수
│   ├── [KV Read] pools, incentives 조회       ← MapValue dirty #2~N
│   └── calculatePositionReward()
│       ├── calculateInternalReward()
│       └── calculateExternalReward() × incentive 수
│
├── [External Rewards Loop] reward.External 순회
│   ├── [KV Read] externalIncentives.get()     ← MapValue dirty
│   ├── incentive.SetRewardAmount()            ← 실질 변경
│   ├── [KV Write] externalIncentives.set()    ← 실질 변경
│   ├── [Cross-realm] SafeGRC20Transfer()      ← 토큰 realm finalize
│   ├── deposit 수집 기록 갱신                   ← 실질 변경
│   └── (조건부) incentive lazy removal
│
├── [Internal Reward]
│   ├── handleStakingRewardFee()
│   ├── [KV Write] SetTotalEmissionSent()      ← 실질 변경
│   ├── [Cross-realm] gns.Transfer()           ← gns realm finalize
│   └── deposit 수집 기록 갱신                   ← 실질 변경
│
├── [KV Write] deposits.set(positionId, deposit) ← 실질 변경
└── [Events] chain.Emit() × 여러 회
```

## 측정 결과

### 측정 인프라

기존 unit test에서는 `GNO_REALM_STATS_LOG` 환경변수가 지원되지 않았다. `gnovm/pkg/test/imports.go`의 `StoreWithOptions()`에 환경변수 체크를 추가하여 unit test에서도 realm stats 로깅을 활성화했다:

```go
// gnovm/pkg/test/imports.go (StoreWithOptions 내부)
if statsPath := os.Getenv("GNO_REALM_STATS_LOG"); statsPath != "" {
    gnoStore.SetRealmStatsLogger(gno.NewRealmStatsLogger(statsPath))
}
```

실행 방법:
```bash
cd contract/r/gnoswap/staker/v1
GNO_REALM_STATS_LOG=stderr gno test -v . -run "TestName" -timeout 300s
```

### 시나리오 E: internal + external reward (TestCollectRewardGracefulDegradation/normal)

pool tier 1 설정 + external incentive(GNS) 1개 + SkipHeights(blocksToSkip+20) 후 CollectReward.
결과: `afterGns(5000102336) > beforeGns(5000000000)` → reward = 102,336 GNS.

#### Finalize 상세 (12회)

| # | Realm | Created | Updated | Ancestors | Deleted | Bytes | 추정 원인 |
|---|-------|:---:|:---:|:---:|:---:|---:|-----------|
| 1 | gns | 2 | 1 | 0 | 0 | +1,073 | MintAndDistributeGns — mint |
| 2 | gns | 10 | 3 | 0 | 6 | +2,032 | MintAndDistributeGns — distribute |
| 3 | gns | 10 | 3 | 0 | 10 | **+0** | avl rebalance (zero-byte) |
| 4 | **staker/v1** | 13 | 6 | **14** | 0 | +7,177 | calcPositionReward 후 중간 finalize |
| 5 | gns | 8 | 10 | 5 | 4 | +2,078 | external reward GNS Transfer |
| 6 | gns | 10 | 3 | 0 | 10 | **-6** | avl rebalance (near-zero) |
| 7 | gns | 10 | 3 | 0 | 6 | +2,031 | internal reward Transfer |
| 8 | gns | 10 | 3 | 0 | 6 | +2,031 | staking fee Transfer |
| 9 | **emission** | 0 | 8 | 0 | 0 | +40 | emission state update |
| 10 | gns | 12 | 4 | 0 | 12 | **+0** | avl rebalance (zero-byte) |
| 11 | gns | 8 | 2 | 0 | 8 | **+0** | avl rebalance (zero-byte) |
| 12 | **staker/v1** | 18 | 16 | **14** | 6 | +6,082 | 최종 finalize (deposit.set 등) |

**요약:**
- 총 12회 finalize: staker/v1(2) + gns(9) + emission(1)
- 총 STORAGE DELTA: **+22,538 bytes**
- Zero-byte finalize: **3회** (entries 3, 10, 11 — 모두 gns avl rebalance)
- staker/v1 ancestors 합계: **28개** (14+14) dirty propagation 재직렬화
- GAS USED: 34,651,682

### 시나리오: internal reward + warmup penalty (TestCollectReward_ZeroNetRewardWithPenalty)

pool tier 1 + warmup 1일 + unstaking fee 10% + SkipHeights(100). External incentive 없음.
warmup으로 인해 net user reward = 0이지만 penalty > 0이므로 내부 처리는 발생.

#### Finalize 상세 (7회)

| # | Realm | Created | Updated | Ancestors | Deleted | Bytes | 추정 원인 |
|---|-------|:---:|:---:|:---:|:---:|---:|-----------|
| 1 | **staker/v1** | 0 | 3 | **5** | 0 | +12 | emission 후 중간 finalize |
| 2 | gns | 6 | 9 | 5 | 2 | +2,079 | MintAndDistributeGns |
| 3 | gns | 10 | 3 | 0 | 6 | +2,025 | penalty Transfer |
| 4 | gns | 10 | 3 | 0 | 6 | +2,031 | fee Transfer |
| 5 | gns | 6 | 3 | 0 | 6 | +6 | avl rebalance (near-zero) |
| 6 | **emission** | 0 | 7 | 0 | 0 | +35 | emission state update |
| 7 | **staker/v1** | 10 | 10 | **10** | 4 | +2,891 | 최종 finalize |

**요약:**
- 총 7회 finalize: staker/v1(2) + gns(4) + emission(1)
- 총 STORAGE DELTA: **+9,079 bytes**
- Zero/near-zero finalize: 1회 (entry 5, +6 bytes)
- staker/v1 ancestors 합계: **15개** (5+10)

### 시나리오: UnStakeToken (TestUnStakeToken)

pool tier 1 + external incentive(BAR) + SkipHeights(100) 후 UnStakeToken.
UnStakeToken 내부에서 CollectReward가 먼저 실행된 후 unstake 처리.

#### Finalize 상세 (12회)

| # | Realm | Created | Updated | Ancestors | Deleted | Bytes | 추정 원인 |
|---|-------|:---:|:---:|:---:|:---:|---:|-----------|
| 1 | gns | 2 | 1 | 0 | 0 | +1,073 | MintAndDistributeGns — mint |
| 2 | gns | 8 | 3 | 0 | 6 | +965 | MintAndDistributeGns — distribute |
| 3 | **staker/v1** | 13 | 6 | **14** | 0 | +7,176 | calcPositionReward 후 중간 finalize |
| 4 | gns | 8 | 10 | 5 | 4 | +2,078 | reward Transfer |
| 5 | gns | 10 | 3 | 0 | 10 | **-6** | avl rebalance (near-zero) |
| 6 | gns | 10 | 3 | 0 | 6 | +2,031 | internal reward Transfer |
| 7 | gns | 6 | 3 | 0 | 6 | +6 | avl rebalance (near-zero) |
| 8 | **emission** | 0 | 7 | 0 | 0 | +35 | emission state update |
| 9 | **staker/v1** | 12 | 11 | **11** | 4 | +3,950 | CollectReward 최종 |
| 10 | position | 0 | 1 | 2 | 0 | +0 | NFT 소유권 복원 (zero-byte) |
| 11 | position | 3 | 8 | 5 | 3 | -44 | position 상태 갱신 |
| 12 | **staker/v1** | 26 | 13 | **38** | 35 | -4,257 | unstake 정리 (deposit 삭제, tick 갱신) |

**요약:**
- 총 12회 finalize: staker/v1(3) + gns(5) + emission(1) + position(2) + bar(미포함, 필터 외)
- 총 STORAGE DELTA: **+13,007 bytes**
- staker/v1 ancestors 합계: **63개** (14+11+38) — unstake 정리에서 38개 대량 발생
- staker/v1 deleted 합계: **39개** (0+4+35) — deposit/tick 객체 일괄 삭제

### 시나리오 비교 종합

| 시나리오 | 테스트명 | finalize | staker/v1 | gns | emission | position | Bytes | Ancestors |
|---------|---------|:---:|:---:|:---:|:---:|:---:|---:|:---:|
| E. internal + external | GracefulDegradation/normal | **12** | 2 | 9 | 1 | - | +22,538 | 28 |
| internal + warmup penalty | ZeroNetRewardWithPenalty | **7** | 2 | 4 | 1 | - | +9,079 | 15 |
| UnStakeToken (collect+unstake) | UnStakeToken | **12** | 3 | 5 | 1 | 2 | +13,007 | 63 |

### 핵심 발견

#### 1. gns realm이 finalize 횟수의 주요 원인

시나리오 E에서 gns realm이 **9/12 = 75%** 의 finalize를 차지한다. 이 중 3회(25%)는 avl.Tree rebalancing에 의한 **zero-byte finalize**로, 순수한 낭비이다.

gns 토큰 전송 1회당 1~2회의 finalize가 발생하며, CollectReward에서는 최소 3~4회의 독립적인 gns 전송이 일어난다:
1. MintAndDistributeGns (emission → staker 배분)
2. External reward SafeGRC20Transfer (보상 토큰이 GNS인 경우)
3. Internal reward gns.Transfer (사용자에게)
4. Staking fee gns.Transfer (community pool에)

#### 2. dirty propagation(ancestors)이 staker/v1의 핵심 비용

staker/v1의 2회 finalize에서 ancestors가 각각 14개씩 총 **28개** 발생한다. 이는 avl.Tree에서 하나의 노드를 업데이트할 때 root까지의 경로상 모든 조상 노드가 dirty marking되어 재직렬화되는 현상이다.

UnStakeToken에서는 ancestors가 **63개**로 급증한다. deposit 삭제 + tick liquidity 갱신이 연쇄적으로 dirty propagation을 유발하기 때문이다.

#### 3. 가설 검증 결과

| 가설 항목 | 예측 | 실측 | 일치 |
|----------|------|------|------|
| finalize 12→14~18회 증가 | 14~18회 | **12회** | **불일치** — 예상보다 적음 |
| zero-byte finalize 9~12회 | reward 유무 무관하게 동일 | **3회** (gns avl만) | **불일치** — staker zero-byte 없음 |
| 추가분은 토큰 전송 cross-realm | 맞음 | gns 9회 중 6회가 토큰 전송 | **부분 일치** |

**수정된 이해**: reward>0일 때 staker/v1에서는 실제 데이터 변경이 발생하므로 zero-byte finalize가 없다. 대신 ancestors(dirty propagation)가 주요 비용이다. reward=0 경로의 12회 zero-byte finalize와는 비용 구조가 근본적으로 다르다.

#### 4. external incentive 유무에 따른 비용 차이

| 조건 | finalize | Bytes | gns finalize |
|------|:---:|---:|:---:|
| internal + external (E) | 12 | +22,538 | 9 |
| internal only (warmup) | 7 | +9,079 | 4 |
| **차이 (external 추가 비용)** | **+5** | **+13,459** | **+5** |

external incentive 1개 추가로 finalize 5회, 13KB+ 의 스토리지가 증가한다. 주로 external reward의 GNS Transfer에서 발생하는 gns realm finalize가 원인이다.

## P1 최적화 효과 예측

### P1_reduce_finalize_calls 적용 시

reward>0 경로에서 제거 가능한 finalize:

| 카테고리 | 해당 entries | 제거 가능 여부 |
|----------|-------------|--------------|
| gns avl rebalance zero-byte | #3, #10, #11 (3회) | **제거 가능** — 실질 변경 없는 재직렬화 |
| gns near-zero (#6, -6B) | 1회 | **제거 가능** — avl rebalance 부작용 |
| staker/v1 중간 finalize (#4) | 1회 | **제거 어려움** — cross-realm 경계에서 발생 |
| gns 토큰 전송 (#1,2,5,7,8) | 5회 | **제거 불가** — 실질 데이터 변경 필수 |
| emission (#9) | 1회 | **제거 불가** — 실질 데이터 변경 필수 |
| staker/v1 최종 (#12) | 1회 | **제거 불가** — 실질 데이터 변경 필수 |

**예상 효과**: 12회 → 약 8회로 감소 (33% 절감). 그러나 bytes 절감은 미미 (~6B 수준의 zero-byte 제거).

### P1_pointer_to_value_types 적용 시

staker/v1의 ancestors 28개는 avl.Tree 내부의 HeapItemValue → StructValue 체인에서 발생한다. value type 전환으로:

- deposit, incentive 등의 StructValue에 대한 HeapItemValue wrapper 제거
- avl.Tree 노드 깊이만큼의 dirty propagation 경로 단축

**예상 효과**: staker/v1 ancestors 28개 → 절반 이하로 감소 가능. bytes 절감 효과가 finalize 횟수 절감보다 클 것으로 예상.

### 두 P1을 모두 적용한 복합 효과

- finalize 횟수: 12회 → ~8회 (zero-byte 제거)
- staker/v1 bytes: +13,259 → ~8,000 (ancestors 감소)
- 총 bytes: +22,538 → ~15,000 (약 33% 절감 추정)

reward=0 경로처럼 finalize를 0회로 만드는 것은 불가능하다. reward>0에서는 실질 데이터 변경(토큰 전송, deposit 갱신)이 필수이기 때문이다.

### 권장 최적화 순서

1. **P0_collect_reward_early_return** (reward=0): 즉시 적용, 12회→0회 (100% 절감)
2. **P1_pointer_to_value_types**: bytes 절감 효과가 크고, ancestors(dirty propagation) 감소에 직결
3. **P1_reduce_finalize_calls**: finalize 횟수 감소 효과는 있지만, 제거 가능한 것이 zero-byte 3~4회에 한정
4. **gns realm 최적화**: 가장 큰 개선 여지가 있으나, gnoswap staker 범위 밖 (GNS 토큰 구조 자체의 문제)

## 이전 가설 (참고용)

> **원래 가설**: reward>0일 때 finalize 횟수는 12회 → 14~18회로 증가하되, 추가분은 대부분 토큰 전송에 의한 cross-realm finalize이다. KVStore 읽기에 의한 zero-byte finalize(9~12회)는 reward 유무와 무관하게 동일하게 발생한다.

이 가설은 **불일치**로 판정되었다. 실측 결과, reward>0에서는 staker/v1의 zero-byte finalize가 발생하지 않으며, 대신 gns realm의 avl rebalance가 zero-byte finalize의 주 원인이다. 비용 구조가 reward=0과 근본적으로 다르다.

## P0 early return과의 관계

이 분석은 `P0_collect_reward_early_return.md`를 대체하지 않는다. 두 문서의 역할:

| 문서 | 범위 | 조치 |
|------|------|------|
| `P0_collect_reward_early_return.md` | reward=0 경로 | 즉시 적용 가능한 코드 수정 |
| **이 문서** | reward>0 경로 | 비용 분석 → P1 최적화 우선순위 결정 |

early return은 reward=0일 때 12회 finalize를 0으로 줄이는 확실한 개선이다. 이 문서는 early return으로 해결되지 않는 reward>0 경로의 비용을 분석하여, 더 근본적인 P1 최적화의 방향을 제시한다.

## 부록: 측정 환경 및 재현 방법

### 적용된 패치

`gnovm/pkg/test/imports.go`에 realm stats logger 주입 코드 추가:
```go
gnoStore = gno.NewStore(nil, baseStore, baseStore)
gnoStore.SetPackageGetter(getPackage)
// Enable realm stats logging for unit tests when GNO_REALM_STATS_LOG is set.
if statsPath := os.Getenv("GNO_REALM_STATS_LOG"); statsPath != "" {
    gnoStore.SetRealmStatsLogger(gno.NewRealmStatsLogger(statsPath))
}
```

### 재현 명령

```bash
# 패치된 gno 바이너리 빌드
go build -o /tmp/gno-patched ./gnovm/cmd/gno/

# 시나리오 E (internal + external)
cd contract/r/gnoswap/staker/v1
GNO_REALM_STATS_LOG=stderr /tmp/gno-patched test -v . \
  -run "TestCollectRewardGracefulDegradation/normal" -timeout 300s

# 시나리오: warmup penalty
GNO_REALM_STATS_LOG=stderr /tmp/gno-patched test -v . \
  -run "TestCollectReward_ZeroNetRewardWithPenalty" -timeout 300s

# UnStakeToken
GNO_REALM_STATS_LOG=stderr /tmp/gno-patched test -v . \
  -run "TestUnStakeToken$" -timeout 300s
```

### 로그 필터링

```bash
# gnoswap realm 관련 항목만 추출 (패키지 초기화 노이즈 제거)
grep -E "(realm-stats.*gno\.land/r/gnoswap|=== RUN|--- PASS|--- FAIL|\[created\]|\[updated\]|\[deleted\])"
```
