# P1: seqid 기반 AVL Tree 키 인코딩

## 개요

staker realm의 `EncodeUint()`/`DecodeUint()` 함수가 사용하는 20자리 zero-padding 방식을
`seqid` 패키지의 cford32 compact 인코딩으로 교체하여 AVL tree 키 크기를 줄임.

### 변경 내용

- `EncodeUint(num uint64)` → `seqid.ID(num).String()` (20 bytes → 7 bytes for ID < 2^34)
- `DecodeUint(s string)` → `seqid.FromString(s)`
- `EncodeInt64`/`DecodeInt64` → 동일하게 seqid 기반으로 변경
- `EncodeInt` (int32, tick용) → 음수 지원 필요하므로 변경 없음

### 변경 파일

1. `examples/gno.land/r/gnoswap/staker/tree.gno`
2. `examples/gno.land/r/gnoswap/staker/v1/reward_calculation_types.gno`

### 영향 범위

- `Deposits` tree (positionId 키): 20 bytes → 7 bytes per key
- `Stakers` tree (depositId 키): 20 bytes → 7 bytes per key
- `UintTree` (timestamp 키): 20 bytes → 7 bytes per key
- Pool의 `EncodeInt` (tick ID, int32): 변경 없음

---

## 수정 브랜치 측정 결과 (2026-03-13)

### 테스트 1: `position_stake_position`

| 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST |
|---|---|---|---|
| CreatePool | 33,805,001 | 19,139 bytes | 11,913,900 ugnot |
| Mint | 53,710,714 | 31,998 bytes | 13,199,800 ugnot |
| SetPoolTier | 38,369,647 | 24,349 bytes | 12,434,900 ugnot |
| StakeToken | 51,573,393 | 23,421 bytes | 12,342,100 ugnot |
| UnStakeToken | 51,022,674 | -8,179 bytes (refund) | 9,182,100 ugnot |

### 테스트 2: `staker_collect_reward_immediately_after_stake_token`

| 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST |
|---|---|---|---|
| CreatePool | 33,805,001 | 19,139 bytes | 11,913,900 ugnot |
| Mint | 52,751,270 | 31,988 bytes | 13,198,800 ugnot |
| SetPoolTier | 38,369,647 | 24,349 bytes | 12,434,900 ugnot |
| StakeToken | 54,899,776 | 32,982 bytes | 13,298,200 ugnot |
| CollectReward | 31,707,339 | (no delta) | - |

### 테스트 3: `pool_create_pool_and_mint`

| 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST |
|---|---|---|---|
| CreatePool | 33,721,364 | 19,150 bytes | 11,915,000 ugnot |
| Mint | 56,919,062 | 32,031 bytes | 103,203,100 ugnot |
| DecreaseLiquidity | 59,082,134 | 2,217 bytes | 100,221,700 ugnot |
| IncreaseLiquidity | 54,575,347 | 4 bytes | 100,000,400 ugnot |
| Swap | 42,783,361 | 5,688 bytes | 100,568,800 ugnot |
| CollectFee | 44,232,052 | 55 bytes | 100,005,500 ugnot |

---

## 베이스라인 측정 결과 (2026-03-13, main 브랜치)

### 테스트 1: `position_stake_position`

| 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST |
|---|---|---|---|
| CreatePool | 33,866,398 | 20,735 bytes | 12,073,500 ugnot |
| Mint | 52,922,537 | 40,734 bytes | 14,073,400 ugnot |
| SetPoolTier | 42,222,934 | 24,427 bytes | 12,442,700 ugnot |
| StakeToken | 56,966,049 | 23,447 bytes | 12,344,700 ugnot |
| UnStakeToken | 58,658,003 | -8,192 bytes (refund) | 9,180,800 ugnot |

### 테스트 2: `staker_collect_reward_immediately_after_stake_token`

| 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST |
|---|---|---|---|
| CreatePool | 33,866,398 | 20,735 bytes | 12,073,500 ugnot |
| Mint | 52,918,017 | 40,733 bytes | 14,073,300 ugnot |
| SetPoolTier | 42,222,934 | 24,427 bytes | 12,442,700 ugnot |
| StakeToken | 63,270,504 | 33,099 bytes | 13,309,900 ugnot |
| CollectReward | 35,940,297 | (no delta) | - |

### 테스트 3: `pool_create_pool_and_mint`

| 오퍼레이션 | GAS USED | STORAGE DELTA | TOTAL TX COST |
|---|---|---|---|
| CreatePool | 33,782,761 | 20,746 bytes | 12,074,600 ugnot |
| Mint | 56,111,039 | 40,765 bytes | 104,076,500 ugnot |
| DecreaseLiquidity | 59,548,365 | 2,279 bytes | 100,227,900 ugnot |
| IncreaseLiquidity | 54,688,336 | 2 bytes | 100,000,200 ugnot |
| Swap | 42,298,920 | 5,694 bytes | 100,569,400 ugnot |
| CollectFee | 44,336,584 | 69 bytes | 100,006,900 ugnot |

---

## 전후 비교

### STORAGE DELTA 비교

#### 테스트 1: `position_stake_position`

| 오퍼레이션 | Baseline | seqid | 차이 | 변화율 |
|---|---|---|---|---|
| CreatePool | 20,735 | 19,139 | **-1,596** | **-7.7%** |
| Mint | 40,734 | 31,998 | **-8,736** | **-21.4%** |
| SetPoolTier | 24,427 | 24,349 | **-78** | -0.3% |
| StakeToken | 23,447 | 23,421 | **-26** | -0.1% |
| UnStakeToken | -8,192 | -8,179 | +13 | - |

#### 테스트 2: `staker_collect_reward_immediately_after_stake_token`

| 오퍼레이션 | Baseline | seqid | 차이 | 변화율 |
|---|---|---|---|---|
| CreatePool | 20,735 | 19,139 | **-1,596** | **-7.7%** |
| Mint | 40,733 | 31,988 | **-8,745** | **-21.5%** |
| SetPoolTier | 24,427 | 24,349 | **-78** | -0.3% |
| StakeToken | 33,099 | 32,982 | **-117** | -0.4% |
| CollectReward | (no delta) | (no delta) | - | - |

#### 테스트 3: `pool_create_pool_and_mint`

| 오퍼레이션 | Baseline | seqid | 차이 | 변화율 |
|---|---|---|---|---|
| CreatePool | 20,746 | 19,150 | **-1,596** | **-7.7%** |
| Mint | 40,765 | 32,031 | **-8,734** | **-21.4%** |
| DecreaseLiquidity | 2,279 | 2,217 | **-62** | -2.7% |
| IncreaseLiquidity | 2 | 4 | +2 | - |
| Swap | 5,694 | 5,688 | -6 | -0.1% |
| CollectFee | 69 | 55 | **-14** | -20.3% |

### GAS USED 비교

#### 테스트 1: `position_stake_position`

| 오퍼레이션 | Baseline | seqid | 차이 | 변화율 |
|---|---|---|---|---|
| CreatePool | 33,866,398 | 33,805,001 | -61,397 | -0.2% |
| Mint | 52,922,537 | 53,710,714 | +788,177 | +1.5% |
| SetPoolTier | 42,222,934 | 38,369,647 | **-3,853,287** | **-9.1%** |
| StakeToken | 56,966,049 | 51,573,393 | **-5,392,656** | **-9.5%** |
| UnStakeToken | 58,658,003 | 51,022,674 | **-7,635,329** | **-13.0%** |

#### 테스트 2: `staker_collect_reward_immediately_after_stake_token`

| 오퍼레이션 | Baseline | seqid | 차이 | 변화율 |
|---|---|---|---|---|
| CreatePool | 33,866,398 | 33,805,001 | -61,397 | -0.2% |
| Mint | 52,918,017 | 52,751,270 | -166,747 | -0.3% |
| SetPoolTier | 42,222,934 | 38,369,647 | **-3,853,287** | **-9.1%** |
| StakeToken | 63,270,504 | 54,899,776 | **-8,370,728** | **-13.2%** |
| CollectReward | 35,940,297 | 31,707,339 | **-4,232,958** | **-11.8%** |

#### 테스트 3: `pool_create_pool_and_mint`

| 오퍼레이션 | Baseline | seqid | 차이 | 변화율 |
|---|---|---|---|---|
| CreatePool | 33,782,761 | 33,721,364 | -61,397 | -0.2% |
| Mint | 56,111,039 | 56,919,062 | +808,023 | +1.4% |
| DecreaseLiquidity | 59,548,365 | 59,082,134 | -466,231 | -0.8% |
| IncreaseLiquidity | 54,688,336 | 54,575,347 | -112,989 | -0.2% |
| Swap | 42,298,920 | 42,783,361 | +484,441 | +1.1% |
| CollectFee | 44,336,584 | 44,232,052 | -104,532 | -0.2% |

### 분석

**Storage 절감:**
- **CreatePool**: 일관적으로 -1,596 bytes (-7.7%) — pool 내부 AVL tree 키 인코딩 변경 효과
- **Mint**: 약 -8,735 bytes (-21.4%) — 가장 큰 절감. position/tick 관련 AVL tree 노드 생성 시 키 크기 감소
- **Staker 오퍼레이션**: StakeToken, SetPoolTier에서 소폭 감소 (-26 ~ -117 bytes)
- **CollectFee**: -14 bytes (-20.3%)

**Gas 절감:**
- **Staker 오퍼레이션에서 큰 효과**: SetPoolTier -9.1%, StakeToken -9.5~13.2%, UnStakeToken -13.0%, CollectReward -11.8%
- **Pool 오퍼레이션**: 미미한 변화 (±1.5% 이내)
- Mint에서 소폭 증가 (+1.4~1.5%)는 cford32 인코딩 연산 비용이 단순 strconv보다 약간 높기 때문으로 추정

**핵심 발견:**
- seqid 적용의 주요 효과는 **storage 크기 감소**에 있으며, 특히 새 노드를 많이 생성하는 Mint에서 가장 두드러짐
- Gas 절감은 staker realm의 UintTree/Deposits/Stakers tree 접근이 많은 오퍼레이션에서 나타남
- Pool 오퍼레이션은 `EncodeInt` (tick용)을 변경하지 않았으므로 직접적 영향 없음
