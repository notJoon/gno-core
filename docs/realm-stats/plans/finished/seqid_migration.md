# Seqid Migration Plan: Numeric Key Encoding Optimization

## Background

The codebase uses `strconv.FormatInt/FormatUint` or zero-padded decimal strings to encode numeric values as `avl.Tree` keys. These encodings consume 10~20 bytes per key. By switching to `gno.land/p/nt/seqid/v0` (cford32 encoding), keys can be reduced to **7 bytes** (for values < 2^34) while preserving lexicographic ordering.

### seqid Encoding Reference

```go
import "gno.land/p/nt/seqid/v0"

// Encode: uint64 → 7-byte ordered string (for values < 2^34)
seqid.ID(num).String()

// Decode: string → uint64
id, err := seqid.FromString(s)
```

- Lexicographic order matches numeric order: `x < y` ⟹ `x.String() < y.String()`
- 7 bytes for values in `[0, 2^34)`, 13 bytes for `[2^34, 2^64)`
- All values in this codebase fall within the 7-byte range

### Handling Negative Values (int32 ticks)

Tick values range from `-887272` to `+887272`. Apply a constant offset to map into positive space:

```go
const ENCODED_TICK_OFFSET int32 = 887272 // == MAX_TICK

// Encode: add offset → uint64 → seqid
seqid.ID(uint64(tick + ENCODED_TICK_OFFSET)).String()

// Decode: seqid → uint64 → subtract offset
int32(uint64(id)) - ENCODED_TICK_OFFSET
```

Max adjusted value = `1,774,544` — well within 7-byte range.

---

## Task 1: `pool/utils.gno` — EncodeTickKey / DecodeTickKey

**Priority: HIGH** — Most frequently used key, both CRUD and range iteration with decode.

### Current Implementation

File: `contract/r/gnoswap/pool/utils.gno:40-63`

```go
func EncodeTickKey(tick int32) string {
    adjustTick := tick + ENCODED_TICK_OFFSET
    s := strconv.FormatInt(int64(adjustTick), 10)
    zerosNeeded := 10 - len(s)
    return strings.Repeat("0", zerosNeeded) + s  // 10 bytes
}

func DecodeTickKey(s string) int32 {
    adjustTick, _ := strconv.ParseInt(s, 10, 32)
    return int32(adjustTick) - ENCODED_TICK_OFFSET
}
```

### Target Implementation

```go
import "gno.land/p/nt/seqid/v0"

func EncodeTickKey(tick int32) string {
    adjustTick := tick + ENCODED_TICK_OFFSET
    if adjustTick < 0 {
        panic(ufmt.Sprintf("tick(%d) + ENCODED_TICK_OFFSET(%d) < 0", tick, ENCODED_TICK_OFFSET))
    }
    return seqid.ID(uint64(adjustTick)).String() // 7 bytes
}

func DecodeTickKey(s string) int32 {
    id, err := seqid.FromString(s)
    if err != nil {
        panic(ufmt.Sprintf("invalid tick key: %s", s))
    }
    return int32(uint64(id)) - ENCODED_TICK_OFFSET
}
```

### Callers to Verify (no signature change, but test outputs will differ)

| File | Line | Usage |
|---|---|---|
| `contract/r/gnoswap/pool/pool.gno` | 155, 160, 176, 181, 186-190 | HasTick, GetTick, SetTick, DeleteTick, IterateTicks |
| `contract/r/gnoswap/pool/v1/getter.gno` | 316 | GetInitializedTicksInRange |
| `contract/r/gnoswap/pool/v1/utils.gno` | 215 | ticksToString |
| `contract/r/gnoswap/pool/v1/getter_test.gno` | 57 | Mock tick setup |
| `contract/r/gnoswap/pool/v1/tick_test.gno` | 497, 545 | Mock tick setup |

### Tests to Update

- `contract/r/gnoswap/pool/utils_test.gno` — All `TestEncodeTickKey*` / `TestDecodeTickKey*` expected values must change
- `contract/r/gnoswap/pool/pool_test.gno:96` — `TestPool_IterateTicks`
- Any test that calls `EncodeTickKey` directly with hardcoded expected strings

### Validation

- Round-trip: `DecodeTickKey(EncodeTickKey(t)) == t` for all `t ∈ [-887272, 887272]`
- Ordering: `EncodeTickKey(a) < EncodeTickKey(b)` iff `a < b`
- `IterateTicks` returns ticks in ascending numeric order

---

## Task 2: `staker/tree.gno` — EncodeInt (int32 tick keys)

**Priority: HIGH** — Contains a decode inconsistency bug that should be fixed alongside migration.

### Current Implementation

File: `contract/r/gnoswap/staker/tree.gno:119-144`

```go
func EncodeInt(num int32) string {
    // Zero-pads to 10 digits, prepends '-' for negatives
    // e.g., -100 → "-0000000100", 100 → "0000000100"
}
```

### Bug: Ordering Broken for Negative Values

`EncodeInt` produces keys where **negative values sort incorrectly**:
- `"-0000000001"` > `"-0000000100"` (lexicographically), but `-1` > `-100` numerically — happens to be correct
- BUT: `"-0000000100"` < `"0000000100"` because `'-'` < `'0'`, so negatives sort before positives — this is also correct

**However**, the decoder in `staker/pool.gno:578` uses `strconv.Atoi(key)` which does NOT match `EncodeInt`:

```go
// staker/pool.gno:570-584
func (t *Ticks) IterateTicks(fn func(tickId int32, tick *Tick) bool) {
    t.tree.Iterate("", "", func(key string, value interface{}) bool {
        tickId, err := strconv.Atoi(key)  // ← decodes zero-padded OK, but fragile
        ...
    })
}
```

### Target Implementation

Replace `EncodeInt` with an offset-based seqid encoding (same pattern as Task 1):

```go
const encodedTickOffset int32 = 887272

func EncodeInt(num int32) string {
    adjusted := num + encodedTickOffset
    if adjusted < 0 {
        panic(ufmt.Sprintf("negative adjusted tick: %d", adjusted))
    }
    return seqid.ID(uint64(adjusted)).String()
}

func DecodeInt(s string) int32 {
    id, err := seqid.FromString(s)
    if err != nil {
        panic("invalid encoded int: " + s)
    }
    return int32(uint64(id)) - encodedTickOffset
}
```

Then update `staker/pool.gno:578` to use `DecodeInt(key)` instead of `strconv.Atoi(key)`.

### Callers

| File | Line | Usage |
|---|---|---|
| `staker/pool.gno` | 536, 544, 556, 562, 566 | Ticks Get/Set/Has/SetTick |
| `staker/pool.gno` | 578 | IterateTicks — **must replace `strconv.Atoi` with `DecodeInt`** |

### Note

`EncodeUint` / `EncodeInt64` / `DecodeUint` / `DecodeInt64` in the same file already use seqid — no changes needed for those.

---

## Task 3: `position/store.gno` — uint64ToString

**Priority: MEDIUM** — High frequency CRUD, no range iteration dependency.

### Current Implementation

File: `contract/r/gnoswap/position/store.gno:125-127`

```go
func uint64ToString(id uint64) string {
    return strconv.FormatUint(id, 10) // 1~20 bytes variable
}
```

### Issue

Position IDs are sequential uint64 values. Decimal encoding has **variable length**, meaning:
- ID `9` → `"9"` (1 byte)
- ID `10` → `"10"` (2 bytes)
- `"9"` > `"10"` lexicographically — **ordering broken**

Currently only `IterateByOffset` is used (not range `Iterate`), so this doesn't cause bugs, but it prevents future range queries.

### Target Implementation

```go
import "gno.land/p/nt/seqid/v0"

func uint64ToString(id uint64) string {
    return seqid.ID(id).String() // 7 bytes, ordered
}

func uint64FromString(s string) uint64 {
    id, err := seqid.FromString(s)
    if err != nil {
        panic("invalid position key: " + s)
    }
    return uint64(id)
}
```

### Callers

| File | Line | Usage |
|---|---|---|
| `position/store.gno` | 74, 80, 100, 112 | HasPosition, GetPosition, SetPosition, RemovePosition |
| `position/v1/getter.gno` | 37 | IterateByOffset (key not decoded — only offset-based) |

### Tests to Update

- Tests in `position/v1/_mock_test.gno` (lines 44, 48, 56, 61) also have their own `strconv.FormatUint` calls — update to match

---

## Task 4: `staker/store.gno` — uint64ToString

**Priority: MEDIUM** — Same pattern as Task 3.

### Current Implementation

File: `contract/r/gnoswap/staker/store.gno:397-399`

```go
func uint64ToString(id uint64) string {
    return strconv.FormatUint(id, 10)
}
```

### Target

Same as Task 3. Replace with seqid encoding.

### Callers

Search for `uint64ToString` usage within `contract/r/gnoswap/staker/` to find all call sites.

---

## Task 5: `gov/governance/store.gno` — formatInt64Key

**Priority: LOW** — Lookup-only, no iteration.

### Current Implementation

File: `contract/r/gnoswap/gov/governance/store.gno:333-336`

```go
func formatInt64Key(id int64) string {
    return strconv.FormatInt(id, 10)
}
```

Used for proposal IDs and config version IDs. These are sequential positive integers starting from 1.

### Target

```go
func formatInt64Key(id int64) string {
    return seqid.ID(uint64(id)).String()
}
```

### Callers

Lines 105, 124, 154, 172, 202, 221 in `gov/governance/store.gno`.

---

## Task 6: `gov/staker/store.gno` — int64ToString

**Priority: LOW** — Lookup-only, no iteration.

### Current Implementation

File: `contract/r/gnoswap/gov/staker/store.gno:353-355`

```go
func int64ToString(n int64) string {
    return strconv.FormatInt(n, 10)
}
```

Used for delegation ID lookups.

### Target

```go
func int64ToString(n int64) string {
    return seqid.ID(uint64(n)).String()
}
```

### Callers

Lines 148, 155, 171, 178 in `gov/staker/store.gno`.

---

## Task 7: `launchpad/store.gno` — NextDepositID

**Priority: LOW** — Single call site, low volume.

### Current Implementation

File: `contract/r/gnoswap/launchpad/store.gno:102-106`

```go
func (s *launchpadStore) NextDepositID() string {
    counter := s.GetDepositCounter()
    return strconv.FormatInt(counter.Next(), 10)
}
```

### Target

```go
func (s *launchpadStore) NextDepositID() string {
    counter := s.GetDepositCounter()
    return seqid.ID(uint64(counter.Next())).String()
}
```

---

## Task 8 (Optional): `pool/v1/tick_bitmap.gno` — wordPos key

**Priority: LOW** — Only Get/Set, no iteration. But fixing for consistency.

### Current Implementation

File: `contract/r/gnoswap/pool/v1/tick_bitmap.gno:85,108`

```go
wordPosStr := strconv.Itoa(int(wordPos))  // wordPos is int16, range: -3466 to 3466
```

### Target

Apply offset (3466) to make non-negative, then seqid:

```go
const wordPosOffset int16 = 3466

func encodeWordPos(wordPos int16) string {
    return seqid.ID(uint64(wordPos + wordPosOffset)).String()
}
```

### Callers

- `tick_bitmap.gno:85` (getTickBitmap)
- `tick_bitmap.gno:108` (setTickBitmap)
- `pool/v1/getter.gno:343`

---

## Storage Measurement: Baseline → Compare Workflow

각 Task를 적용하기 전에 반드시 baseline STORAGE DELTA 수치를 먼저 기록하고, 수정 후 동일 테스트를 재실행하여 비교해야 한다.

### Step 1: Baseline 측정

아래 테스트들을 실행하여 `STORAGE DELTA` 수치를 기록한다.

```bash
# 환경변수 설정 (storage delta 출력 활성화)
export GNO_REALM_STATS_LOG=stderr

# 통합 테스트 실행 (각각 별도 실행)
go test -v -run TestTestdata/<test_name> -timeout 5m ./gno.land/pkg/integration/
```

### Step 2: 수정 적용

Task 1~8의 코드 변경을 적용한다.

### Step 3: 동일 테스트 재실행 및 비교

동일 테스트를 다시 실행하여 STORAGE DELTA 값이 감소했는지 확인한다.

### 측정 대상 테스트 목록

아래 테스트들은 모두 `STORAGE DELTA`를 출력하며, 수정 대상 모듈(pool, position, staker, gov, launchpad)의 key 인코딩 경로를 포함한다.

#### Pool (Task 1, 8) — tick key, tickBitmap wordPos key

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `pool/create_pool_and_mint.txtar` | `pool_create_pool_and_mint` | CreatePool + Mint (tick entry 생성 포함) |
| `pool/swap_wugnot_gns_tokens.txtar` | `pool_swap_wugnot_gns_tokens` | Swap (tick bitmap 조회 경로) |

#### Position (Task 1, 3) — tick key (via pool), position ID key

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `position/storage_poisition_lifecycle.txtar` | `position_storage_poisition_lifecycle` | Mint → Swap → CollectFee → DecreaseLiquidity 전체 lifecycle |
| `position/position_mint_with_gnot.txtar` | `position_position_mint_with_gnot` | GNOT 기반 Mint |
| `position/reposition.txtar` | `position_reposition` | Reposition (position 삭제 + 재생성) |
| `position/stake_position.txtar` | `position_stake_position` | Position stake (position + staker 경로) |

#### Staker (Task 2, 4) — staker tick key, deposit ID key

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `staker/storage_staker_lifecycle.txtar` | `staker_storage_staker_lifecycle` | Stake → CollectReward → UnStake 전체 lifecycle |
| `staker/storage_staker_stake_only.txtar` | `staker_storage_staker_stake_only` | StakeToken 단독 (순수 stake 비용 분리) |
| `staker/storage_staker_stake_with_externals.txtar` | `staker_storage_staker_stake_with_externals` | External incentive 3개 + StakeToken |
| `staker/staker_create_external_incentive.txtar` | `staker_staker_create_external_incentive` | External incentive 생성 |
| `staker/collect_reward_immediately_after_stake_token.txtar` | `staker_collect_reward_immediately_after_stake_token` | Stake 직후 CollectReward |

#### Router (Task 1 간접) — swap 경로에서 tick key 사용

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `router/exact_in_single_swap_route.txtar` | `router_exact_in_single_swap_route` | 단일 풀 swap |
| `router/exact_in_swap_route.txtar` | `router_exact_in_swap_route` | 멀티 풀 swap |
| `router/exact_out_swap_route.txtar` | `router_exact_out_swap_route` | Exact output swap |

#### Gov (Task 5, 6) — proposal ID key, delegation ID key

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `gov/governance/create_text_proposal.txtar` | `gov_governance_create_text_proposal` | Proposal 생성 |
| `gov/governance/create_parameter_change_proposal.txtar` | `gov_governance_create_parameter_change_proposal` | Parameter change proposal |
| `gov/staker/delegate_and_undelegate.txtar` | `gov_staker_delegate_and_undelegate` | Delegate + Undelegate |
| `gov/staker/delegate_and_redelegate.txtar` | `gov_staker_delegate_and_redelegate` | Delegate + Redelegate |

#### Launchpad (Task 7) — deposit ID key

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `launchpad/create_new_launchpad_project.txtar` | `launchpad_create_new_launchpad_project` | 프로젝트 생성 |
| `launchpad/deposit_gns_to_inactivated_project_should_fail.txtar` | `launchpad_deposit_gns_to_inactivated_project_should_fail` | Deposit 시도 |

#### Base (참조용) — 기본 storage/strconv 비용

| 테스트 파일 | 테스트 이름 | 측정 내용 |
| --- | --- | --- |
| `base/storage_gas_measurement.txtar` | `base_storage_gas_measurement` | 기본 storage write/read 비용 기준선 |
| `base/strconv_gas_measurement.txtar` | `base_strconv_gas_measurement` | strconv 함수별 gas 비용 기준선 |
| `base/data_structure_gas_measurement.txtar` | `base_data_structure_gas_measurement` | avl.Tree 등 자료구조 gas 비용 |

### 결과 기록 형식

각 테스트에서 출력되는 `STORAGE DELTA` 값을 아래 형식으로 기록한다:

```
| 테스트 | 단계 | Before (bytes) | After (bytes) | 차이 |
|--------|------|----------------|---------------|------|
| position_lifecycle | Mint | XXXX | YYYY | -ZZZ |
| position_lifecycle | Swap | XXXX | YYYY | -ZZZ |
| ...    | ...  | ...            | ...           | ...  |
```

---

## Execution Notes

### Import to Add

All modified files need:
```go
"gno.land/p/nt/seqid/v0"
```

And may be able to remove:
```go
"strconv"  // only if no other strconv usage remains in the file
```

### Migration Safety

These changes affect **in-memory avl.Tree key format only**. Since Gno realm state is persistent across transactions, if any realm is already deployed with data using old encoding:

> **Deployed realms with existing data will break** — old keys (decimal) won't match new keys (cford32). This migration is safe only if:
> 1. The realm is being deployed fresh, OR
> 2. A state migration is performed alongside the code upgrade

### Testing Strategy

For each task:
1. Verify round-trip: `Decode(Encode(x)) == x` for full value range
2. Verify ordering: `Encode(a) < Encode(b)` iff `a < b`
3. Run all existing tests — update hardcoded expected key strings
4. For Tasks 1 & 2: explicitly test `Iterate` / `ReverseIterate` boundary behavior

### Byte Savings Summary

| Location | Current | After | Savings per Key | Status |
|---|---|---|---|---|
| pool EncodeTickKey | 10 B | 7 B | 3 B (30%) | **Applied** |
| staker EncodeInt | 10~11 B | 7 B | 3~4 B (30%) + bug fix | **Applied** |
| position uint64ToString | 1~20 B | 7 B | **negative** for small IDs | Reverted |
| staker uint64ToString | 1~20 B | 7 B | **negative** for small IDs | Skipped |
| gov formatInt64Key | 1~19 B | 7 B | **negative** for small IDs | Skipped |
| gov int64ToString | 1~19 B | 7 B | **negative** for small IDs | Skipped |
| launchpad NextDepositID | 1~19 B | 7 B | **negative** for small IDs | Skipped |
| tick_bitmap wordPos | 1~6 B | 7 B | **negative** (+15~84 B/op) | Reverted |

### Lessons Learned

seqid (cford32) encoding produces a **fixed 7-byte** key. This is only beneficial when the
current encoding is **consistently longer than 7 bytes** (e.g., 10-byte zero-padded tick keys).
For variable-length decimal keys where actual values are small (position ID=1, wordPos=-2),
decimal encoding (1-5 bytes) is shorter than seqid (7 bytes), resulting in **net storage increase**.

Measured results: see `docs/realm-stats/measurements/seqid_migration_baseline.md`.
