# P2: Value Type 전환 확대 적용 검토

> **선행 문서**: [`P1_pointer_to_value_types.md`](./P1_pointer_to_value_types.md)를 반드시 먼저 읽을 것.
> P1에 기술된 전환 패턴(방식 B: accessor 패턴), PoC 검증 결과, TickInfo/PositionInfo 실측 데이터,
> 그리고 aliasing 이슈 및 해결 방법이 이 문서의 기반이다.

## 목적

P1에서 Pool realm의 TickInfo(5필드), PositionInfo(5필드), Pool(9필드)을 대상으로 `*u256.Uint`/`*i256.Int` → value type + accessor 전환을 진행했다. 본 문서는 동일 패턴을 적용할 수 있는 **나머지 구조체**를 전수 조사한 결과를 정리한다.

## 전환 기준

최적화 효과가 있으려면 **KV store에 영속되는 구조체**여야 한다:
- realm-level 전역 변수로 저장되거나
- `avl.Tree`, `map` 등 영속 컬렉션에 저장되는 구조체

함수 파라미터, 로컬 변수, 반환값 등 **transient 구조체**는 FinalizeRealmTransaction에서 직렬화되지 않으므로 HeapItemValue 제거 효과가 없다.

## 전환 대상 전수 조사 결과

### 완료 (P1)

| 구조체 | Realm | 포인터 필드 | 상태 |
|--------|-------|:---:|------|
| TickInfo | pool | 5 | **완료** — P1 2~5단계 |
| PositionInfo | pool | 5 | **완료** — P1 6단계 |

### 완료 (P1 7단계)

| 구조체 | Realm | 포인터 필드 | 상태 |
|--------|-------|:---:|------|
| Pool 본체 | pool | 4 | **완료** |
| Slot0 | pool (Pool 내장) | 1 | **완료** |
| Balances (TokenPair) | pool (Pool 내장) | 2 | **완료** |
| ProtocolFees (TokenPair) | pool (Pool 내장) | 2 | **완료** |
| **소계** | | **9** | |

> `*avl.Tree`(×3)와 `*ObservationState`(×1)는 참조 타입으로 유지.

### 추가 후보 — 높은 우선순위

#### Position (position realm) — 7 필드

```
파일: contract/r/gnoswap/position/position.gno
저장: avl.Tree (positionId → *Position)
```

| 필드 | 타입 |
|------|------|
| liquidity | `*u256.Uint` |
| feeGrowthInside0LastX128 | `*u256.Uint` |
| feeGrowthInside1LastX128 | `*u256.Uint` |
| tokensOwed0 | `*u256.Uint` |
| tokensOwed1 | `*u256.Uint` |
| token0Balance | `*u256.Uint` |
| token1Balance | `*u256.Uint` |

**pool realm의 PositionInfo(5필드)와 별개의 구조체**이다. position realm에서 독립적으로 영속 저장되며, Mint/Burn/CollectFee 등 position 관련 모든 오퍼레이션에서 접근된다. 7개 포인터 필드는 현재 조사 대상 중 단일 struct 최대이므로 HeapItemValue 제거 효과가 가장 크다.

**예상 효과**: Mint 시 position realm에서 -7 HeapItemValue per position.

**주의**: P1에서 발견된 aliasing 이슈가 동일하게 적용된다. tokensOwed0/1 필드는 collect 경로에서 getter 결과를 변수에 저장 후 setter로 수정하는 패턴이 있을 수 있으므로 반드시 감사할 것.

#### Observation (pool/oracle) — 2 필드

```
파일: contract/r/gnoswap/pool/oracle.gno
저장: ObservationState.observations map[uint16]*Observation (Pool 내장)
```

| 필드 | 타입 |
|------|------|
| liquidityCumulative | `*u256.Uint` |
| secondsPerLiquidityCumulativeX128 | `*u256.Uint` |

Pool에 내장된 ObservationState의 observations map에 저장된다. Pool 전환(P1 7단계) 시 함께 전환하는 것이 자연스럽다.

### 추가 후보 — 낮은 우선순위

포인터 필드 1~2개로 단일 struct당 HeapItemValue 제거 효과가 작다. 다만 대량으로 생성되는 경우 누적 효과가 있을 수 있다.

| 구조체 | Realm | 포인터 필드 수 | 필드 | 비고 |
|--------|-------|:---:|------|------|
| Deposit | staker | 1 | liquidity | avl.Tree에 저장. stake 수에 비례하여 생성 |
| EmissionRewardState | gov/staker | 1 | rewardDebtX128 | delegation별 생성 |
| ProtocolFeeRewardState | gov/staker | 1 (map) | rewardDebtX128 `map[string]*u256.Uint` | map 내부 포인터 — 전환 방식 검토 필요 |
| ProjectTier | launchpad | 1 | distributeAmountPerSecondX128 | 프로젝트당 tier 수만큼 생성 |
| RewardState | launchpad | 1 | priceDebtX128 | deposit별 reward state |
| EmissionRewardManager | gov/staker | 1 | accumulatedRewardX128PerStake | 싱글턴 |
| RewardManager | launchpad | 2 | distributeAmountPerSecondX128, accumulatedRewardPerDepositX128 | tier별 1개 |

**ProtocolFeeRewardState 특이사항**: `rewardDebtX128`가 `map[string]*u256.Uint` 타입이다. map의 value가 포인터이므로, value type으로 변경하려면 `map[string]u256.Uint`로 전환해야 한다. map value에 대한 accessor 패턴은 struct 필드와 다르므로 별도 검토가 필요하다.

### 전환 불필요 — Transient 구조체

KV store에 영속되지 않는 구조체. 함수 실행 중에만 존재하므로 HeapItemValue가 store에 기록되지 않는다.

| 구조체 | Realm | 포인터 필드 수 | 용도 |
|--------|-------|:---:|------|
| SwapState | pool/v1 | 6 | swap 실행 중 상태 |
| StepComputations | pool/v1 | 5 | swap 루프 단계별 계산 |
| MintParams | position/v1 | 4 | Mint 함수 파라미터 |
| IncreaseLiquidityParams | position/v1 | 4 | 함수 파라미터 |
| ProcessedMintInput | position/v1 | 4 | Mint 입력 전처리 |
| SwapCache | pool/v1 | 2 | swap 캐시 |
| FeeGrowthInside | position/v1 | 2 | fee 계산 임시값 |
| DecreaseLiquidityParams | position/v1 | 2 | 함수 파라미터 |
| SwapResult | pool/v1 | 6 | swap 결과 반환값 |
| ModifyPositionParams | pool/v1 | 1 | 함수 파라미터 |
| SingleSwapParams | router/v1 | 1 | 함수 파라미터 |
| BaseSwapParams | router/v1 | 1 | 함수 파라미터 |
| poolCreateConfig | pool/v1 | 1 | 팩토리 파라미터 |
| bitShift | p/gnoswap/gnsmath | 1 | 수학 연산 유틸 |
| tickCrossEventInfo | staker/v1 | 3 | 이벤트 발행용 임시 struct |

## 전체 요약

| 카테고리 | 구조체 수 | 포인터 필드 합계 | 상태 |
|----------|:---:|:---:|------|
| 완료 (TickInfo + PositionInfo) | 2 | 10 | P1 완료 |
| 완료 (Pool + 하위 struct) | 4 | 9 | P1 7단계 완료 |
| **추가 — Position (position realm)** | **1** | **7** | 미착수, 높은 우선순위 |
| 추가 — Observation | 1 | 2 | P1 7단계와 묶어서 처리 권장 |
| 추가 — 소규모 영속 struct | 7 | 8+ | 낮은 우선순위 |
| 전환 불필요 (transient) | 15 | 43 | 대상 아님 |

## 권장 작업 순서

1. ~~**P1 7단계 완료**: Pool + Slot0 + Balances + ProtocolFees (9 필드)~~ **완료**
2. **Position (position realm)**: 7 필드 — 단일 struct 중 최대 효과
3. **Staker Deposit**: 1 필드 — stake 수 비례 생성으로 누적 효과 기대
4. **Gov/Staker, Launchpad**: 1 필드씩 — 낮은 우선순위, 필요 시 적용

## Storage 측정: Baseline → 수정 → 비교 워크플로우

각 전환 작업은 반드시 아래 순서를 따른다:

### Step 1: Baseline 측정

수정 전 상태에서 관련 txtar 테스트를 실행하여 가스 사용량과 함께 `STORAGE DELTA` 수치를 기록한다.

```bash
export GNO_REALM_STATS_LOG=stderr
go test -v -run TestTestdata/<test_name> -timeout 5m ./gno.land/pkg/integration/
```

### Step 2: 코드 수정

Value type 전환을 적용한다 (accwessor 패턴, aliasing 감사 포함).

### Step 3: 동일 테스트 재실행 및 비교

동일 테스트를 다시 실행하여 STORAGE DELTA 값이 감소했는지 확인한다.
HeapItemValue 제거 효과가 실측치로 나타나지 않는 경우, 전환을 재검토한다.

### 전환 대상별 측정 테스트 매핑

#### Position (position realm) — 7 필드, 높은 우선순위

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `position/storage_poisition_lifecycle.txtar` | `position_storage_poisition_lifecycle` | Mint → Swap → CollectFee → DecreaseLiquidity 전체 lifecycle |
| `position/position_mint_with_gnot.txtar` | `position_position_mint_with_gnot` | GNOT 기반 Mint |
| `position/reposition.txtar` | `position_reposition` | Reposition (position 삭제 + 재생성) |
| `position/stake_position.txtar` | `position_stake_position` | Position stake (position + staker 경로) |

> Position 전환의 핵심 측정 테스트는 `storage_poisition_lifecycle.txtar`이다.
> Mint 단계에서 -7 HeapItemValue 감소가 STORAGE DELTA에 반영되는지 확인할 것.

#### Observation (pool/oracle) — 2 필드

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `pool/create_pool_and_mint.txtar` | `pool_create_pool_and_mint` | CreatePool + Mint (oracle 초기화 포함) |
| `pool/swap_wugnot_gns_tokens.txtar` | `pool_swap_wugnot_gns_tokens` | Swap (observation 업데이트 경로) |

#### Staker Deposit — 1 필드

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `staker/storage_staker_lifecycle.txtar` | `staker_storage_staker_lifecycle` | Stake → CollectReward → UnStake 전체 lifecycle |
| `staker/storage_staker_stake_only.txtar` | `staker_storage_staker_stake_only` | StakeToken 단독 (순수 stake 비용 분리) |
| `staker/storage_staker_stake_with_externals.txtar` | `staker_storage_staker_stake_with_externals` | External incentive 3개 + StakeToken |
| `staker/collect_reward_immediately_after_stake_token.txtar` | `staker_collect_reward_immediately_after_stake_token` | Stake 직후 CollectReward |
| `staker/staker_create_external_incentive.txtar` | `staker_staker_create_external_incentive` | External incentive 생성 |

#### Gov/Staker — EmissionRewardState, ProtocolFeeRewardState (각 1 필드)

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `gov/staker/delegate_and_undelegate.txtar` | `gov_staker_delegate_and_undelegate` | Delegate + Undelegate |
| `gov/staker/delegate_and_redelegate.txtar` | `gov_staker_delegate_and_redelegate` | Delegate + Redelegate |

#### Launchpad — ProjectTier, RewardState, RewardManager (각 1~2 필드)

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `launchpad/create_new_launchpad_project.txtar` | `launchpad_create_new_launchpad_project` | 프로젝트 생성 (ProjectTier 생성) |
| `launchpad/deposit_gns_to_inactivated_project_should_fail.txtar` | `launchpad_deposit_gns_to_inactivated_project_should_fail` | Deposit 시도 (RewardState 경로) |
| `launchpad/collect_protocol_fee_failure.txtar` | `launchpad_collect_protocol_fee_failure` | Protocol fee 수집 경로 |
| `launchpad/collect_left_before_project_ended_should_fail.txtar` | `launchpad_collect_left_before_project_ended_should_fail` | 프로젝트 종료 전 수집 |

### 결과 기록 형식

각 테스트에서 출력되는 `STORAGE DELTA` 값을 아래 형식으로 기록한다:

```
| 테스트 | 단계 | Before (bytes) | After (bytes) | 차이 |
|--------|------|----------------|---------------|------|
| position_lifecycle | Mint          | XXXX | YYYY | -ZZZ |
| position_lifecycle | CollectFee    | XXXX | YYYY | -ZZZ |
| staker_lifecycle   | StakeToken    | XXXX | YYYY | -ZZZ |
| ...                | ...           | ...  | ...  | ...  |
```

## 작업 시 참조 사항

- **전환 패턴**: P1 문서의 "방식 B: Accessor 패턴" 섹션 참조
- **Aliasing 이슈**: P1 문서의 "위험 요소 — Aliasing 위험" 참조. getter 결과를 변수에 저장 후 같은 필드를 setter로 수정하는 패턴이 있는지 반드시 감사할 것
- **PoC 및 실측 데이터**: P1 문서의 "PoC 결과" 및 "2~5단계/6단계" 실측 데이터 참조
- **측정 방법**: `GNO_REALM_STATS_LOG=stderr` 환경변수로 realm-stats 수집. 위 테스트 매핑표의 txtar 테스트 사용
