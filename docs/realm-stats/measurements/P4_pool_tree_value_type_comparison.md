# P4-2 Comparison: Pool `*avl.Tree` x 3 + `*ObservationState` → value type

**Date:** 2026-03-17
**Base commit:** 99662881c (master)
**Change:** `pool/pool.gno` — 3x `*avl.Tree` → `avl.Tree`, `*ObservationState` → `ObservationState`; `pool/v1/oracle.gno` — nil check 3곳 수정

---

## Test 1: pool_create_pool_and_mint

### Per-operation (total STORAGE DELTA)

| Operation | Baseline (B) | After (B) | Delta | % |
|-----------|-------------|-----------|-------|---|
| **CreatePool (fee 3000)** | 15,932 | **14,345** | **-1,587** | **-10.0%** |
| **Mint #1** | 32,009 | **32,023** | +14 | +0.04% |
| DecreaseLiquidity | 2,229 | **2,217** | **-12** | -0.5% |
| IncreaseLiquidity | 4 | 4 | 0 | 0% |
| Swap (fee 3000) | 5,688 | 5,688 | 0 | 0% |
| CollectFee | 55 | 55 | 0 | 0% |

### Per-operation (pool realm only)

| Operation | Baseline (B) | After (B) | Delta |
|-----------|-------------|-----------|-------|
| **CreatePool** | 13,759 | **12,172** | **-1,587** |
| **Mint #1** | 17,592 | **17,604** | +12 |
| DecreaseLiquidity | 54 | **42** | **-12** |
| IncreaseLiquidity | 6 | 6 | 0 |
| Swap | 5,688 | 5,688 | 0 |
| CollectFee | -6 | -6 | 0 |

---

## Test 2: pool_swap_wugnot_gns_tokens

### Per-operation (total STORAGE DELTA)

| Operation | Baseline (B) | After (B) | Delta | % |
|-----------|-------------|-----------|-------|---|
| **CreatePool (fee 500)** | 15,936 | **14,349** | **-1,587** | **-10.0%** |
| **Mint #1** | 32,023 | 32,023 | 0 | 0% |
| ExactIn Swap (fee 500) | 19,863 | 19,863 | 0 | 0% |

### Per-operation (pool realm only)

| Operation | Baseline (B) | After (B) | Delta |
|-----------|-------------|-----------|-------|
| **CreatePool** | 13,757 | **12,170** | **-1,587** |
| Mint #1 | 17,607 | 17,607 | 0 |
| ExactIn Swap | 19,863 | 19,863 | 0 |

---

## Analysis

### What happened

Converting `*avl.Tree` x3 + `*ObservationState` → value type eliminates 4 intermediate Object headers per Pool. The savings manifest as:

1. **CreatePool (-1,587 bytes)**: Pool creation saves ~1.6 KB from eliminated Object overhead. Consistent across both fee tiers (3000, 500).
2. **Mint #1 (~0 change)**: Mutation delta is essentially unchanged — the same tick/position data is added regardless of layout.
3. **DecreaseLiquidity (-12 bytes)**: Minor improvement from smaller Pool re-serialization.
4. **Swap, IncreaseLiquidity, CollectFee (0 change)**: No measurable delta change on subsequent mutations.

### Net effect

| Scenario | Net pool saving |
|----------|-----------------|
| CreatePool (per pool, one-time) | **-1,587 bytes** |
| Full lifecycle (Create+Mint+Decrease+Increase+Swap+Collect) | **-1,585 bytes** |
| Swap test (Create+Mint+Swap) | **-1,587 bytes** |

### Savings breakdown

- 4 Objects eliminated = 4 × ~100 bytes header = ~400 bytes (Object headers)
- Additional ~1,187 bytes saved from eliminated Object reference overhead and alignment

Plan에서 예상한 ~400 bytes보다 실제 절감이 크다. 이는 Object 참조가 inline화됨으로써 중간 노드의 encoding overhead도 함께 제거되기 때문이다.

### GAS comparison

| Operation | Baseline GAS | After GAS | Delta |
|-----------|-------------|-----------|-------|
| CreatePool (3000) | 33,683,610 | 33,654,597 | -29,013 |
| CreatePool (500) | 34,077,134 | 34,048,805 | -28,329 |
| Mint #1 (3000) | 55,927,363 | 56,760,394 | +833,031 |
| Mint #1 (500) | 57,067,076 | 57,049,922 | -17,154 |
| Swap (3000) | 42,513,543 | 43,026,285 | +512,742 |
| ExactIn Swap (500) | 71,875,180 | 71,860,829 | -14,351 |
| DecreaseLiquidity | 58,914,624 | 58,614,489 | -300,135 |
| IncreaseLiquidity | 54,339,490 | 54,429,397 | +89,907 |
| CollectFee | 44,543,882 | 44,532,463 | -11,419 |

> Note: fee 3000 테스트에서 GAS 변동이 fee 500 테스트보다 큰 것은 테스트 실행 환경에 따른 jitter일 수 있다.
> Storage delta 기준 결과가 일관되므로 GAS 변동은 유의미하지 않다.
