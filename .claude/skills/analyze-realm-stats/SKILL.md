---
name: analyze-realm-stats
description: Run txtar storage tests with realm stats logging and analyze the results. Use when measuring storage costs of GnoSwap operations, comparing before/after optimization deltas, or investigating which objects and realms contribute most to storage overhead.
argument-hint: [txtar-test-name-or-log-file]
---

# Realm Stats Storage Analysis

Measure and analyze per-realm, per-object storage costs from txtar integration tests. This skill runs a test with `GNO_REALM_STATS_LOG` enabled, then parses the output to identify optimization targets.

## Quick reference

- Baseline data: `docs/GNOSWAP_STORAGE_BASELINE.md`
- Raw logs: `docs/realm-stats/`
- Analysis script: `docs/realm-stats/analyze_realm_stats.py`
- Realm stats implementation: `gnovm/pkg/gnolang/realm_stats.go`

## Step 1: Identify the txtar test

Available storage lifecycle tests live in `gno.land/pkg/integration/testdata/`. Find the relevant test:

```bash
ls gno.land/pkg/integration/testdata/*storage*.txtar
```

Current test files:
- `position_storage_poisition_lifecycle.txtar` — Pool creation, Mint, Swap, CollectFee, DecreaseLiquidity
- `staker_storage_staker_lifecycle.txtar` — SetPoolTier, StakeToken, CollectReward, UnStakeToken

If `$ARGUMENTS` is a `.log` file path, skip to Step 3 (analysis only).

## Step 2: Run the test with realm stats logging

Set `GNO_REALM_STATS_LOG` to capture per-realm object statistics:

```bash
cd gno.land/pkg/integration

# Choose a descriptive output filename
GNO_REALM_STATS_LOG=/tmp/realm_stats_<test_name>.log \
  go test -v -run "TestTestdata/<txtar_file_name_without_extension>" -timeout 600s
```

After the test completes, also capture the STORAGE DELTA values from stdout — these are the per-transaction byte totals that appear in the test output.

**Important**: Copy the log to the persistent directory for future reference:

```bash
cp /tmp/realm_stats_<test_name>.log docs/realm-stats/<descriptive_name>.log
```

## Step 3: Analyze the log

### Automated analysis

Run the bundled analysis script:

```bash
python3 docs/realm-stats/analyze_realm_stats.py <logfile>
```

This produces four sections:
1. **Per-operation detail** — Every `Call` with per-realm object breakdown
2. **Summary table** — Created/Updated/Ancestors/Deleted/Bytes per operation
3. **Zero-byte finalize analysis** — Wasted re-serialization calls (key optimization signal)
4. **AddPackage summary** — Deployment costs

### Manual deep-dive

For targeted investigation, use grep patterns on the raw log:

```bash
# All entries for a specific realm
grep "path=gno.land/r/gnoswap/staker" <logfile>

# Find operations with high ancestor propagation
grep "ancestors=[1-9]" <logfile>

# Find zero-byte finalize calls (wasted work)
grep "bytes=+0$" <logfile>

# Count MapValue updates per Call (KVStore re-serialization indicator)
grep -A5 "^--- Call" <logfile> | grep "MapValue"
```

## Step 4: Interpret the results

### Key metrics to evaluate

| Metric | What it means | Optimization signal |
|--------|--------------|---------------------|
| **finalize_calls per operation** | Number of `FinalizeRealmTransaction` invocations | >5 per Call = likely excessive cross-realm or internal re-serialization |
| **zero-byte finalize ratio** | Finalize calls that produce bytes=+0 | High ratio (>50%) = MapValue / KVStore being re-serialized without actual data change |
| **ancestors count** | Objects re-serialized due to dirty propagation | High count = deep ownership chains, consider flattening struct hierarchy |
| **created HeapItemValue** | Pointer fields (`*u256.Uint`, etc.) materialized as separate objects | Each one = 2,000 gas flat cost; consider value types |
| **MapValue updates** | KVStore `map[string]any` being re-written | Primary cost driver; target for `map` → `avl.Tree` migration |

### Red flags by operation type

- **Mint**: >10,000 bytes per Mint after tick reuse = pool struct overhead
- **SetPoolTier/StakeToken**: >10 finalize calls = staker KVStore map churn
- **CollectReward with bytes=0**: Should short-circuit when reward=0
- **Any operation**: >50% zero-byte finalize ratio = structural re-serialization waste

### Cost estimation

```
Storage deposit = STORAGE_DELTA_bytes × 100 ugnot/byte
1 GNOT = 1,000,000 ugnot = 10,000 bytes storage delta
```

Target: all user-facing operations < 10,000 bytes (< 1 GNOT deposit).

## Step 5: Compare with baseline

Cross-reference results against `docs/GNOSWAP_STORAGE_BASELINE.md`:

| Operation | Baseline | Current | Reduction |
|-----------|----------|---------|-----------|
| CreatePool | 20,746 bytes | ? | ? |
| Mint (1st) | 40,763 bytes | ? | ? |
| SetPoolTier | 24,427 bytes | ? | ? |
| StakeToken | 23,447 bytes | ? | ? |
| CollectReward (1st) | 5,758 bytes | ? | ? |

Fill in current values from the test output's STORAGE DELTA lines and calculate reduction percentages.

## Environment setup notes

- The txtar tests require GnoSwap contract unit tests to be removed before running (they conflict with the integration test framework).
- `GNO_REALM_STATS_LOG` accepts: `stdout`, `stderr`, or a file path.
- Tests have a ~5 minute timeout; use `-timeout 600s`.
- Each `Call` separator appears duplicated (known minor issue) — the analysis script handles this correctly.
