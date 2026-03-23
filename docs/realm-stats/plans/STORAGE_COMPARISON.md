# Storage 최적화 비교 측정 계획서

**목적:** main 브랜치 대비 storage deposit 개선 수치를 유저 엔트리 함수 기준으로 측정
**배경:** 미팅 요구사항 — (1) 유저 엔트리 함수 기준 storage 비교, (2) deposit된 gnot 반환 가능성 검토, (3) 데이터 누적 병목 파악

---

## 1. 측정 대상: 유저 엔트리 함수

유저가 직접 호출하는 함수만 대상. Admin/governance 전용 함수는 제외.

### Tier 1 — 가장 빈번한 유저 함수 (필수 측정)

| 함수 | 모듈 | 설명 | 빈도 |
|------|------|------|------|
| `ExactInSwapRoute` | router | 토큰 스왑 (exact input) | 매우 높음 |
| `Mint` | position | LP 포지션 생성 | 높음 |
| `CollectFee` | position | LP 수수료 수령 | 높음 |
| `CollectReward` | staker | 스테이킹 보상 수령 | 높음 |
| `StakeToken` | staker | LP 포지션 스테이킹 | 보통 |
| `UnStakeToken` | staker | 스테이킹 해제 | 보통 |

### Tier 2 — 주요 유저 함수 (가능하면 측정)

| 함수 | 모듈 | 설명 | 빈도 |
|------|------|------|------|
| `IncreaseLiquidity` | position | 기존 포지션에 유동성 추가 | 보통 |
| `DecreaseLiquidity` | position | 유동성 제거 | 보통 |
| `Delegate` | gov/staker | GNS 스테이킹 | 보통 |
| `Undelegate` | gov/staker | GNS 언스테이킹 | 낮음 |
| `Redelegate` | gov/staker | 위임 변경 | 낮음 |

### Tier 3 — 셋업/운영 함수 (참고 수치)

| 함수 | 모듈 | 설명 |
|------|------|------|
| `CreatePool` | pool | 풀 생성 (1회성) |
| `CreateExternalIncentive` | staker | 외부 인센티브 생성 (프로젝트 팀) |

---

## 2. 기존 txtar 테스트 커버리지

### 있음 (그대로 사용 가능)

| txtar 파일 | 커버하는 함수 |
|---|---|
| `position/storage_poisition_lifecycle.txtar` | CreatePool, Mint ×3, Swap, CollectFee, DecreaseLiquidity |
| `staker/storage_staker_lifecycle.txtar` | SetPoolTier, StakeToken, CollectReward ×2, UnStakeToken |
| `staker/storage_staker_stake_only.txtar` | StakeToken (단독) |
| `staker/storage_staker_stake_with_externals.txtar` | CreateExternalIncentive ×3, StakeToken |
| `gov/staker/delegate_and_undelegate.txtar` | Delegate, Undelegate, CollectUndelegatedGns |
| `gov/staker/delegate_and_redelegate.txtar` | Delegate, Redelegate |

### 없음 (추가 필요)

| 필요한 테스트 | 이유 | 우선순위 |
|---|---|---|
| `router/storage_swap_lifecycle.txtar` | ExactInSwapRoute — 가장 빈번한 유저 함수, storage 전용 측정 없음 | **필수** |
| `position/storage_increase_liquidity.txtar` | IncreaseLiquidity — 포지션 모듈 유일한 누락 | 권장 |

---

## 3. 추가 필요 테스트 시나리오

### 3a. `router/storage_swap_lifecycle.txtar` (필수)

**시나리오:** 유저가 router를 통해 스왑 실행

```
# 셋업
1. CreatePool (WUGNOT:GNS, fee=3000)
2. Mint (유동성 제공)
3. Token approve

# 측정 대상
4. ExactInSwapRoute (GNS → WUGNOT, 1st swap)     ← STORAGE DELTA 측정
5. ExactInSwapRoute (WUGNOT → GNS, reverse swap)  ← STORAGE DELTA 측정
6. ExactInSwapRoute (GNS → WUGNOT, 2nd swap)      ← STORAGE DELTA 측정 (2회차 비교)
```

**측정 포인트:**
- 1st swap: pool 상태 초기 변경 비용
- reverse swap: 반대 방향 swap 비용
- 2nd swap: 동일 방향 반복 시 storage delta (steady-state 비용)

> Note: 기존 `router/exact_in_swap_route.txtar`에 이미 동일한 구조가 있으나 storage 측정 헤더와 주석이 없음. 기존 파일을 복사하여 storage 측정 전용으로 정리하는 것이 효율적.

### 3b. `position/storage_increase_liquidity.txtar` (권장)

**시나리오:** 기존 포지션에 유동성 추가

```
# 셋업
1. CreatePool (WUGNOT:GNS, fee=3000)
2. Mint (포지션 생성)

# 측정 대상
3. IncreaseLiquidity (positionId=1)   ← STORAGE DELTA 측정
4. IncreaseLiquidity (positionId=1)   ← STORAGE DELTA 측정 (2회차)
```

---

## 4. 측정 방법

### 4a. 환경 설정

```bash
# storage/gas 통계 활성화
export GNO_REALM_STATS_LOG=stderr
```

### 4b. 비교 실행 절차

```bash
# 1. main 브랜치 baseline 수집
git stash
git checkout main
cd tests/integration

# storage 전용 테스트만 실행
go test -v -run "TestTestdata/position_storage|TestTestdata/staker_storage|TestTestdata/staker_delegate|TestTestdata/staker_collect_reward_immediately" -count=1 2>&1 | tee /tmp/main_storage.txt

# router swap 테스트 (기존 또는 신규)
go test -v -run "TestTestdata/router_exact_in_swap_route" -count=1 2>&1 | tee -a /tmp/main_storage.txt

# 2. 최적화 브랜치 측정
git checkout <optimized-branch>
cd tests/integration

go test -v -run "TestTestdata/position_storage|TestTestdata/staker_storage|TestTestdata/staker_delegate|TestTestdata/staker_collect_reward_immediately" -count=1 2>&1 | tee /tmp/optimized_storage.txt

go test -v -run "TestTestdata/router_exact_in_swap_route" -count=1 2>&1 | tee -a /tmp/optimized_storage.txt
```

### 4c. 결과 추출

각 txtar 테스트의 출력에서 다음 수치를 추출:
- `GAS USED`: 실행 gas 비용
- `STORAGE DELTA`: 영속 storage 바이트 변화
- `STORAGE FEE`: storage deposit 비용 (ugnot)

비교 테이블 형식:

```
| 함수 | main (bytes) | optimized (bytes) | delta | % |
```

---

## 5. 미팅 요구사항 매핑

### 요구사항 1: 유저 엔트리 함수 기준 storage 비교

위 섹션 1-4로 커버됨. 최종 산출물:

```
유저 엔트리 함수별 Storage 비교 (main vs optimized)
─────────────────────────────────────────────────
함수              | main     | optimized | 절감
Mint              | XX,XXX B | XX,XXX B  | -XX.X%
ExactInSwapRoute  | XX,XXX B | XX,XXX B  | -XX.X%
CollectFee        | XX,XXX B | XX,XXX B  | -XX.X%
StakeToken        | XX,XXX B | XX,XXX B  | -XX.X%
CollectReward     | XX,XXX B | XX,XXX B  | -XX.X%
UnStakeToken      | XX,XXX B | XX,XXX B  | -XX.X%
Delegate          | XX,XXX B | XX,XXX B  | -XX.X%
Undelegate        | XX,XXX B | XX,XXX B  | -XX.X%
```

### 요구사항 2: deposit된 gnot 반환 가능성

이것은 코드 측정이 아닌 **gnovm 레벨 조사**가 필요한 항목.

조사 대상:
- Gno VM에서 storage deposit (addpkg/maketx 시 지불하는 gnot)이 반환 가능한 메커니즘이 있는지
- 현재 gno-core의 `StorageFee` / `StorageDeposit` 관련 코드에서 refund 로직 존재 여부
- Cosmos SDK의 `AnteHandler` → storage deposit 관련 handler 확인
- 관련 gno issue/PR 검색 (storage deposit refund)

산출물:
- gnovm storage deposit 반환 메커니즘 유무
- 반환 가능하다면 조건 (state 삭제 시? 또는 영구 lock?)
- 유저 관점에서 Mint 시 지불한 5 gnot이 DecreaseLiquidity/Burn 시 돌아올 수 있는지

### 요구사항 3: 데이터 누적 병목

측정 방법: N회 반복 시나리오로 O(n) 스캔 비용 증가를 관측.

현재 알려진 O(n) 경로:
- `EndExternalIncentive` → 전체 deposit 스캔 (#10a)
- `GetExternalIncentiveByPoolPath` → 전체 incentive 스캔 (#10b)
- `GetPoolsByTier` → 전체 pool-tier 스캔 (#10c)

이 항목들은 storage deposit이 아닌 **gas 비용 증가** 문제이며, STORAGE_AUDIT_REPORT에서 Skip으로 처리됨. 별도 가스 측정 테스트로 병목 규모를 수치화할 수 있으나, 현재 측정 범위에서는 제외.

---

## 6. 작업 순서

| 순서 | 작업 | 산출물 |
|------|------|--------|
| 1 | `router/storage_swap_lifecycle.txtar` 작성 | 테스트 파일 |
| 2 | (선택) `position/storage_increase_liquidity.txtar` 작성 | 테스트 파일 |
| 3 | main 브랜치에서 전체 storage 테스트 실행 → baseline 수집 | `main_storage.txt` |
| 4 | 최적화 브랜치에서 동일 테스트 실행 → 비교 수집 | `optimized_storage.txt` |
| 5 | 유저 함수별 비교 테이블 작성 | 비교 문서 |
| 6 | gnovm storage deposit refund 메커니즘 조사 | 조사 결과 문서 |
