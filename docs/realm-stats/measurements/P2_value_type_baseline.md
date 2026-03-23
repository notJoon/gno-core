# P2: Value Type 확대 적용 — Baseline 측정

Date: 2026-03-16
Branch: `perf/object-dirty-log`
Commit: `668f4174a`

## 환경

- `GNO_REALM_STATS_LOG=stderr`
- 모든 테스트: `-timeout 5m`

---

## 전환 대상 현재 상태

| 구조체 | Realm | 포인터 필드 수 | 현재 상태 |
|--------|-------|:-:|------|
| **Position** | position | 0 (7 → 0) | **이미 value type 전환 완료** |
| **Observation** | pool (oracle) | 2 | `*u256.Uint` — 전환 필요 |
| **Deposit** | staker | 1 | `*u256.Uint` — 전환 필요 |
| EmissionRewardState | gov/staker | 1 | `*u256.Uint` — 낮은 우선순위 |
| ProtocolFeeRewardState | gov/staker | 1 (map) | `map[string]*u256.Uint` — 별도 검토 필요 |
| ProjectTier | launchpad | 1 | `*u256.Uint` — 낮은 우선순위 |
| RewardState | launchpad | 1 | `*u256.Uint` — 낮은 우선순위 |
| EmissionRewardManager | gov/staker | 1 | `*u256.Uint` — 싱글턴 |
| RewardManager | launchpad | 2 | `*u256.Uint` — 낮은 우선순위 |

> Position은 이미 `u256.Uint` value type + accessor 패턴으로 전환 완료. P2의 작업 대상에서 제외.

---

## Baseline 측정 결과

### 1. Observation 관련 테스트

#### pool_create_pool_and_mint

| Step | Operation | GAS USED | STORAGE DELTA (bytes) | TOTAL TX COST |
|:---:|---|---:|---:|---:|
| 1 | Deploy pool | 11,453,478 | 5,768 | 676,800 |
| 2 | Deploy wugnot | 5,277,168 | 1,081 | 100,108,100 |
| 3 | CreatePool | 33,723,956 | 19,150 | 11,915,000 |
| 4 | Approve bar | 6,625,821 | 1,037 | 100,103,700 |
| 5 | Approve foo | 4,073,981 | 1,074 | 100,107,400 |
| 6 | Approve (staker) | 4,119,925 | 2,118 | 100,211,800 |
| 7 | **Mint** | 56,845,524 | **32,023** | 103,202,300 |
| 8 | DecreaseLiquidity | 58,789,055 | 2,217 | — |

#### pool_swap_wugnot_gns_tokens

| Step | Operation | GAS USED | STORAGE DELTA (bytes) | TOTAL TX COST |
|:---:|---|---:|---:|---:|
| 1 | Deploy pool | 11,453,478 | 5,768 | 676,800 |
| 2 | Deploy gov/staker | 6,468,873 | 2,043 | 100,204,300 |
| 3 | Deploy gov/staker | 6,589,340 | 2,049 | 100,204,900 |
| 4 | Misc | 6,613,525 | 1 | 100,000,100 |
| 5 | Deploy | 5,279,316 | 1,082 | 100,108,200 |
| 6 | CreatePool | 34,117,480 | 19,154 | 11,915,400 |
| 7 | Approve bar | 6,625,821 | 1,037 | 100,103,700 |
| 8 | Approve foo | 4,074,017 | 1,074 | 100,107,400 |
| 9 | Approve | 4,119,961 | 2,118 | 100,211,800 |
| 10 | **Mint** | 56,273,696 | **32,010** | 103,201,000 |
| 11 | Position setup | 5,324,060 | 2,133 | 100,213,300 |
| 12 | **Swap** | 71,893,903 | **19,875** | 101,987,500 |

### 2. Deposit (staker) 관련 테스트

#### staker_storage_staker_stake_only

| Step | Operation | GAS USED | STORAGE DELTA (bytes) | TOTAL TX COST |
|:---:|---|---:|---:|---:|
| 1-4 | Deploy staker (x4) | — | 2,029 / 2,034 / 2,029 / 2,034 | — |
| 5-8 | Approve (x4) | — | 1,074 / 1,074 / 2,118 / 2,118 | — |
| 9 | Deploy wugnot | 5,277,168 | 1,081 | 100,108,100 |
| 10 | CreatePool | 33,807,593 | 19,139 | 11,913,900 |
| 11 | Mint | 52,812,531 | 31,978 | 13,197,800 |
| 12 | SetPoolTier | 38,376,802 | 24,349 | 12,434,900 |
| 13 | gnft Approve | 5,323,504 | 2,133 | 100,213,300 |
| 14 | Misc | 4,413,390 | 1,061 | — |
| 15 | **StakeToken** | 50,922,315 | **23,411** | 12,341,100 |

#### staker_storage_staker_lifecycle

| Step | Operation | GAS USED | STORAGE DELTA (bytes) | TOTAL TX COST |
|:---:|---|---:|---:|---:|
| 1-2 | Deploy staker (x2) | — | 2,029 / 2,029 | — |
| 3-6 | Approve (x4) | — | 1,074 / 1,074 / 2,118 / 2,118 | — |
| 7 | Deploy wugnot | 5,277,168 | 1,081 | 100,108,100 |
| 8 | CreatePool | 33,741,888 | 19,139 | 11,913,900 |
| 9 | Mint | 52,582,780 | 31,978 | 13,197,800 |
| 10 | SetPoolTier | 38,376,802 | 24,349 | 12,434,900 |
| 11 | gnft Approve | 4,413,390 | 1,061 | 10,106,100 |
| 12 | **StakeToken** | 50,922,285 | **23,411** | 12,341,100 |
| 13 | CollectReward (1st) | 31,398,815 | — | — |
| 14 | CollectReward (2nd) | 31,398,815 | — | — |
| 15 | **UnStakeToken** | 56,120,905 | **2,363** | 10,236,300 |

#### staker_storage_staker_stake_with_externals

| Step | Operation | GAS USED | STORAGE DELTA (bytes) | TOTAL TX COST |
|:---:|---|---:|---:|---:|
| 1-4 | Deploy staker (x4) | — | 2,029 / 2,034 / 2,029 / 2,034 | — |
| 5-8 | Approve (x4) | — | 1,074 / 1,074 / 2,118 / 2,118 | — |
| 9 | Deploy wugnot | 5,277,168 | 1,081 | 100,108,100 |
| 10 | CreatePool | 33,807,593 | 19,139 | 11,913,900 |
| 11 | Mint | 53,008,961 | 31,989 | 13,198,900 |
| 12 | **StakeToken** | 38,376,802 | 24,349 | 12,434,900 |
| 13 | Position setup | 5,323,504 | 2,133 | 100,213,300 |
| 14 | External incentive setup | 6,626,863 | 1,037 | 100,103,700 |
| 15 | Approve | 4,074,053 | 1,074 | 100,107,400 |

### 3. Gov/Staker 관련 테스트

#### gov_staker_delegate_and_undelegate

| Step | Operation | GAS USED | STORAGE DELTA (bytes) | TOTAL TX COST |
|:---:|---|---:|---:|---:|
| 1 | Deploy gov/staker | 6,468,106 | 2,043 | 100,204,300 |
| 2 | Deploy GNS | 5,267,222 | 1,081 | 100,108,100 |
| 3 | **Delegate** (Approve+Delegate) | 28,791,633 | **18,312** | 101,831,200 |
| 4 | **Undelegate** | 25,735,880 | **1,331** | 100,133,100 |
| 5 | CollectUndelegatedGns | 17,129,302 | — | — |

#### gov_staker_delegate_and_redelegate

| Step | Operation | GAS USED | STORAGE DELTA (bytes) | TOTAL TX COST |
|:---:|---|---:|---:|---:|
| 1 | Deploy gov/staker | 6,467,274 | 2,043 | 100,204,300 |
| 2 | Deploy GNS | 5,267,222 | 1,081 | 100,108,100 |
| 3 | **Delegate** (Approve+Delegate) | 28,781,543 | **18,312** | 101,831,200 |
| 4 | **Redelegate** | 25,358,532 | **10,826** | 101,082,600 |

### 4. Launchpad 관련 테스트

#### launchpad_create_new_launchpad_project

| Step | Operation | GAS USED | STORAGE DELTA (bytes) | TOTAL TX COST |
|:---:|---|---:|---:|---:|
| 1 | Deploy launchpad | 5,274,263 | 2,029 | 100,202,900 |
| 2 | Deploy launchpad | 5,385,067 | 2,034 | 100,203,400 |
| 3 | Approve | 4,077,480 | 1,074 | 100,107,400 |
| 4 | **CreateProject** | 26,923,424 | **28,110** | 102,811,000 |

### 5. Position 참조 (이미 전환 완료)

#### position_storage_poisition_lifecycle

| Step | Operation | GAS USED | STORAGE DELTA (bytes) | TOTAL TX COST |
|:---:|---|---:|---:|---:|
| 1 | Deploy pool | 11,448,472 | 5,767 | 676,700 |
| 2 | Approve bar | 6,625,821 | 1,037 | 100,103,700 |
| 3 | Deploy wugnot | 5,277,168 | 1,081 | 100,108,100 |
| 4 | Approve foo | 4,073,981 | 1,074 | 100,107,400 |
| 5 | Approve (staker) | 4,119,925 | 2,118 | 100,211,800 |
| 6 | CreatePool | 33,723,956 | 19,150 | 11,915,000 |
| 7 | **Mint** | 55,988,856 | **32,008** | 103,200,800 |
| 8 | Misc | 5,306,529 | 1 | 100,000,100 |
| 9 | **Swap (via position)** | 55,603,791 | **30,716** | 103,071,600 |
| 10 | **CollectFee** | 53,565,313 | **10,289** | 101,028,900 |
| 11 | Position setup | 5,324,060 | 2,133 | 100,213,300 |
| 12 | DecreaseLiquidity | 44,120,861 | 6 | 100,000,600 |
| 13 | CollectFee (2nd) | 44,917,847 | 2,216 | 100,221,600 |
| 14 | CollectFee (3rd) | 44,819,327 | 50 | 100,005,000 |
| 15 | Final | 55,381,729 | 14 | 100,001,400 |

---

## 핵심 Baseline 요약 (전환 대상 오퍼레이션)

| 전환 대상 | 테스트 | 오퍼레이션 | STORAGE DELTA (bytes) |
|-----------|--------|-----------|----------------------:|
| **Observation (2 필드)** | pool_create_pool_and_mint | CreatePool | 19,150 |
| | pool_create_pool_and_mint | Mint | 32,023 |
| | pool_swap_wugnot_gns | Mint | 32,010 |
| | pool_swap_wugnot_gns | Swap | 19,875 |
| **Deposit (1 필드)** | staker_stake_only | StakeToken | 23,411 |
| | staker_lifecycle | StakeToken | 23,411 |
| | staker_lifecycle | UnStakeToken | 2,363 |
| **EmissionRewardState (1 필드)** | gov_delegate_undelegate | Delegate | 18,312 |
| | gov_delegate_undelegate | Undelegate | 1,331 |
| | gov_delegate_redelegate | Delegate | 18,312 |
| | gov_delegate_redelegate | Redelegate | 10,826 |
| **ProjectTier (1 필드)** | launchpad_create_project | CreateProject | 28,110 |

---

## 예상 효과 분석

### Observation (pool/oracle) — 2 `*u256.Uint` 필드

- `liquidityCumulative`, `secondsPerLiquidityCumulativeX128`
- `map[uint16]*Observation`에 저장 → 각 Observation이 이미 별도 Object
- 전환 시 Observation당 2개 HeapItemValue 제거
- CreatePool 시 1개 Observation 초기화 → -2 HeapItemValue
- Swap/Mint 시 Observation 업데이트 → dirty propagation 감소 기대
- **특이사항**: Observation 자체가 `*Observation`(포인터)로 map에 저장됨
  - value type 전환은 Observation 내부 필드에만 적용
  - Observation을 map에서 꺼내 사용하는 패턴 감사 필요

### Deposit (staker) — 1 `*u256.Uint` 필드

- `liquidity` 필드만 포인터
- stake 수에 비례하여 Deposit 생성 → 누적 효과
- StakeToken 시 -1 HeapItemValue per Deposit
- **주의**: `NewDeposit()`에서 `liquidity` 파라미터가 `*u256.Uint`
  - 값 복사 필요: `liquidity: *liquidity`
  - `Clone()` 메서드도 수정 필요: `liquidity: *d.liquidity.Clone()`

### Gov/Staker EmissionRewardState — 1 `*u256.Uint` 필드

- `rewardDebtX128` 필드
- delegation별 생성
- 효과 작음 (1 필드)

### Launchpad ProjectTier — 1 `*u256.Uint` 필드

- `distributeAmountPerSecondX128` 필드
- 프로젝트당 tier 수만큼 생성
- 효과 작음 (1 필드)

---

## 전환 결과

### Observation (pool/oracle) — 2 `*u256.Uint` → `u256.Uint` — **적용 완료**

변경 파일: `pool/oracle.gno`
- `liquidityCumulative`, `secondsPerLiquidityCumulativeX128` → value type
- Getter: `&o.field` 반환 (accessor 패턴)
- Setter: `o.field = *value` (값 복사)
- Constructor: `*liquidityCumulative` (역참조)
- Clone: value copy (u256.Uint는 [4]uint64 배열 — 값 복사 안전)

#### STORAGE DELTA 비교

| 테스트 | 오퍼레이션 | Before | After | Delta |
|--------|-----------|-------:|------:|------:|
| pool_create_pool_and_mint | **CreatePool** | 19,150 | 18,352 | **-798 (-4.2%)** |
| pool_create_pool_and_mint | Mint | 32,023 | 32,023 | 0 |
| pool_create_pool_and_mint | Swap | 5,688 | 5,688 | 0 |
| pool_swap_wugnot_gns | **CreatePool** | 19,154 | 18,356 | **-798 (-4.2%)** |
| pool_swap_wugnot_gns | Swap | 19,875 | 19,863 | -12 |
| position_lifecycle | **CreatePool** | 19,150 | 18,352 | **-798 (-4.2%)** |
| position_lifecycle | Mint | 32,008 | 32,008 | 0 |
| position_lifecycle | Swap | 30,716 | 30,716 | 0 |

#### 분석

- **CreatePool: -798 bytes (-4.2%)** — 3개 테스트에서 일관
  - NewObservationState 시 Observation 1개 생성 → 2개 HeapItemValue (포인터 래퍼) 제거
  - HeapItemValue당 ~399 bytes (포인터 래퍼 + ArrayValue 직렬화 비용)
- **Mint/Swap: 변동 없음** — circular buffer에서 기존 Observation 교체
  - 새 Observation이 value type으로 생성되지만, 교체되는 기존 Observation도 이미 value type
  - 순 효과: 0 (첫 CreatePool에서만 절감 발생)
- aliasing 이슈 없음 — 모든 getter 반환값은 읽기 전용으로만 사용

### Deposit (staker) — 1 `*u256.Uint` — **전환 불필요 (storage-neutral)**

변경 파일: `staker/deposit.gno` — 적용 후 revert

#### STORAGE DELTA 비교

| 테스트 | 오퍼레이션 | Before | After (value type) | Delta |
|--------|-----------|-------:|-------------------:|------:|
| staker_lifecycle | StakeToken | 23,411 | 32,573 | **+9,162** |
| staker_lifecycle | UnStakeToken | 2,363 | -6,799 | **-9,162** |
| staker_lifecycle | **합계** | **25,774** | **25,774** | **0** |

#### 분석 — 전환 불필요 사유

Deposit의 lifecycle은 **create(StakeToken) → delete(UnStakeToken)** 패턴이다.
Value type 전환 시 HeapItemValue 1개가 제거되지만:

- **StakeToken**: Deposit StructValue가 커져 초기 저장 비용 증가 (+9,162 B)
- **UnStakeToken**: 더 큰 StructValue 제거로 삭제 시 환불 증가 (-9,162 B)
- **순 효과: 0** — 비용이 생성 시점으로 전이될 뿐 총량 불변

P1에서 TickInfo/PositionInfo 전환이 효과적이었던 이유:
- **영구 저장**: Mint 시 생성 후 삭제 없이 영속
- **반복 수정**: Swap마다 tick crossing 시 재직렬화 → Object 수 감소 효과 누적
- Deposit은 liquidity 필드가 생성 후 수정되지 않아 이 패턴에 해당하지 않음

**결론**: create-delete lifecycle 구조체는 value type 전환이 storage-neutral.
동일 사유로 나머지 1-필드 후보(EmissionRewardState, ProjectTier, RewardState 등)도
create-delete 패턴이면 전환 불필요.

---

## 권장 작업 순서

1. ~~**Position (position realm)**: 7 필드~~ → **이미 완료**
2. ~~**Observation (pool/oracle)**: 2 필드~~ → **적용 완료** (CreatePool -798 B)
3. ~~**Deposit (staker)**: 1 필드~~ → **전환 불필요** (storage-neutral)
4. **나머지 (gov/staker, launchpad)**: create-delete 패턴이면 전환 불필요

## Key Insight

Value type 전환의 효과는 구조체의 **lifecycle 패턴**에 의존한다:

| 패턴 | 예시 | 효과 |
|------|------|------|
| **영구 저장 + 반복 수정** | TickInfo, PositionInfo, Pool | **효과 큼** — Object 수 감소 → 수정 시 재직렬화 비용 절감 |
| **영구 저장 + 초기 생성만** | Observation | **효과 있음** — 초기 생성 시 HeapItemValue 제거 비용 절감 |
| **create-delete (수정 없음)** | Deposit | **효과 없음** — 생성/삭제 비용이 상쇄 |
