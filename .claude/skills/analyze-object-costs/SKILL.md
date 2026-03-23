---
name: analyze-object-costs
description: Analyze per-object storage deposit costs in GnoVM at the variable and file level. Use when investigating which specific variables, struct fields, or AVL tree operations drive storage costs in a realm transaction. Trigger this skill when the user asks about per-object cost breakdown, variable-level storage attribution, "which variable costs the most", line-level cost tracing, or wants to drill down beyond realm-level summaries into individual object costs. Also use when the user mentions analyze_object_costs.py, [obj-cost] logs, or --by-var analysis.
argument-hint: [call-pattern e.g. "position.Mint"] or [log-file-path]
---

# Per-Object Storage Cost Analysis

Break down storage deposit costs to the individual object and source variable level. This goes deeper than realm-level stats — it answers "which variable in which file caused how many bytes of storage cost".

## When to use this vs analyze-realm-stats

- **analyze-realm-stats**: High-level view — how many objects created/updated/deleted per realm per transaction. Good for initial triage.
- **analyze-object-costs** (this skill): Drill-down — which package variable (`kvStore`, `Token`, `nft`) in which file (`state.gno:23`, `wugnot.gno:14`) drives the cost, broken down by operation type (create/update/ancestor/delete).

## Key files

| File | Purpose |
|------|---------|
| `docs/realm-stats/analyze_object_costs.py` | Analysis script with `--by-var`, `--list`, `--nth` modes |
| `gnovm/pkg/gnolang/realm_stats.go` | Instrumentation: `LogObjectCost`, `objectRootVar`, `findVarInPkgBlock` |
| `gnovm/pkg/gnolang/realm.go` | `saveObject`, `removeDeletedObjects` — where costs are recorded |
| `docs/realm-stats/analyze_realm_stats.py` | Companion realm-level analysis (coarser granularity) |

## Step 1: Generate a log

The `[obj-cost]` lines require `GNO_REALM_STATS_LOG` to be set. There are two contexts:

### Integration tests (txtar)

```bash
GNO_REALM_STATS_LOG=/tmp/obj_costs.log \
  go test -v -run 'TestTestdata/<test_name>' \
  ./gno.land/pkg/integration/ -count=1 -timeout 300s
```

Available storage lifecycle tests in `gno.land/pkg/integration/testdata/`:
- `position_storage_poisition_lifecycle.txtar` — Mint, Swap, CollectFee, DecreaseLiquidity
- `staker_storage_staker_lifecycle.txtar` — SetPoolTier, StakeToken, CollectReward, UnStakeToken
- `router_storage_swap_lifecycle.txtar` — Swap routing

### Unit tests (file tests)

```bash
GNO_REALM_STATS_LOG=stderr \
  go test -run 'TestFiles/zrealm_avl0' \
  ./gnovm/pkg/gnolang/ -v -count=1
```

If `$ARGUMENTS` is a `.log` file path, skip to Step 2.

## Step 2: Choose an analysis mode

### List all operations in a log

```bash
python3 docs/realm-stats/analyze_object_costs.py <logfile> --list
```

Shows every `Call` and `AddPackage` section with line numbers.

### Analyze a specific function call

```bash
python3 docs/realm-stats/analyze_object_costs.py <logfile> "position.Mint"
```

The pattern matches against the Call target (e.g., `"position.Mint"`, `"pool.CreatePool"`, `"gns.Approve"`).

### Analyze the Nth occurrence

```bash
python3 docs/realm-stats/analyze_object_costs.py <logfile> "position.Mint" --nth 2
```

Useful for comparing 1st Mint (creates new ticks) vs 2nd Mint (reuses ticks).

### Variable-level breakdown

```bash
python3 docs/realm-stats/analyze_object_costs.py <logfile> "position.Mint" --by-var
```

Groups all objects by their root package variable and shows which file:line each variable is declared at. This is the most actionable view for optimization.

## Step 3: Read the output

### Summary table

```
Realm                                      Trigger      Cr  Up  An Del      Net  |Churn|
r/gnoswap/pool                             Mint         44   9   0   8   +17592    23558
r/gnoswap/gnft                             Mint         10   4   0   0    +5178     5178
```

- **Cr/Up/An/Del**: Created, Updated, Ancestor, Deleted object counts
- **Net**: Net bytes added to storage (this determines the storage deposit)
- **|Churn|**: Sum of absolute byte diffs — measures total serialization work including deletes

### --by-var table

```
Variable                                        Create   Update  Delete      Net  |Churn|
r/gnoswap/pool.kvStore@state.gno:23             +20149     +426      +0   +20575    20575
r/gnoland/wugnot.Token@wugnot.gno:14             +5038       +5      +0    +5043     5043
```

Each row is `realm.variable@file:line` — the exact source declaration driving the cost.

### Detailed per-object breakdown (default mode, without --by-var)

```
  [CREATE] 43 objects, +20149 bytes
    StructValue            x13  diff=  +9887 avg_size=  760
      oid=...:166    size=  1150 diff= +1150  root=kvStore@state.gno:23
    ArrayValue             x20  diff=  +6780 avg_size=  339
      oid=...:167    size=   339 diff=  +339  root=kvStore@state.gno:23
```

Each object shows:
- **op**: `create` (new), `update` (modified), `ancestor` (re-serialized parent), `delete` (removed)
- **type**: `StructValue`, `ArrayValue`, `HeapItemValue`, `MapValue`, `Block`, `FuncValue`, `PackageValue`
- **oid**: Object ID (`PkgHash:NewTime`)
- **size**: Final serialized size in bytes
- **diff**: Bytes added or freed by this single object
- **root=**: Package variable this object belongs to, with declaration file:line

## Step 4: Interpret the results

### What the operation types mean for cost

| Op | Meaning | Cost driver |
|----|---------|-------------|
| **create** | New object persisted for the first time | Full serialized size is added |
| **update** | Existing object re-serialized with changes | Only the size *difference* counts (often small) |
| **ancestor** | Parent objects re-serialized because a child changed | Hash tree propagation — no data change but re-serialization cost |
| **delete** | Object removed from storage | Frees bytes (negative diff), reduces deposit |

### What the object types mean

| Type | Typical source | Optimization angle |
|------|---------------|-------------------|
| **StructValue** | AVL tree nodes, realm data structs | Large structs with many pointer fields → consider value types |
| **ArrayValue** | Struct field arrays (phantom arrays) | Each struct with N fields creates an ArrayValue of N slots — phantom array inlining eliminates these |
| **HeapItemValue** | Pointer fields (`*u256.Uint`, etc.), AVL tree left/right | Each pointer = separate object with ~340 byte overhead — use value types where possible |
| **MapValue** | `map[string]any` in KVStore pattern | Maps re-serialize entirely on any change — migrate to `avl.Tree` for incremental updates |
| **Block** | Package-level scope | Re-serialized when any variable in the scope changes — usually small diff |

### Common patterns and what to do

**High ArrayValue count with uniform ~339 byte size**
→ Phantom array pattern. Each struct field array is a separate object. Inlining these at the VM level removes the overhead.

**High HeapItemValue count under a single root variable**
→ Pointer-heavy struct. Each `*Type` field becomes a HeapItemValue. Converting pointer fields to value types (e.g., `*u256.Uint` → `u256.Uint`) eliminates one object per field.

**Large |Churn| relative to Net**
→ AVL tree rebalancing. Insertions cause old nodes to be deleted and new nodes created. Net cost is the new leaf, but churn includes the full old+new subtree.

**`(unknown)` entries in --by-var output**
→ Deleted objects whose ownership chain was already severed. These are the "delete" side of AVL rebalancing. Their cost is already reflected as negative diff — no action needed.

**MapValue with diff=+0 in update**
→ Map re-serialized to same size. The map didn't grow but was re-written because something else in the same realm changed. Consider whether this variable needs to be touched at all.

## How the instrumentation works

Understanding the internals helps interpret edge cases.

### Cost recording points

1. **`saveObject`** (`realm.go`): Called for every created/updated/ancestor object. Calls `store.SetObject(oo)` which amino-serializes the object and returns `diff = newSize - oldSize`. Logs via `LogObjectCost` with op="create"/"update"/"ancestor".

2. **`removeDeletedObjects`** (`realm.go`): Called for every deleted object. `store.DelObject(do)` returns `LastObjectSize` (bytes freed). Logs with op="delete".

### Root variable resolution

The `root=` label is resolved by `objectRootVar` (`realm_stats.go`):

1. **Ownership chain walk**: Follow `GetOwner()` up from the object through HeapItemValues and StructValues until reaching a Block.
2. **Block slot lookup**: Find which named slot in the Block contains the object, using `GetBlockNames()` and OID comparison.
3. **Escaped object fallback**: If an object has no owner (escaped, refcount>1), `findVarInPkgBlock` loads the package block and recursively searches each HeapItemValue slot.
4. **File resolution**: `PackageNode.FileSet.GetDeclForSafe(name)` finds the declaring FileNode, giving the exact `.gno` filename. Line number comes from `NameSource.Origin.GetSpan()`.

### Cost formula

```
Storage deposit = net_bytes × 100 ugnot/byte
1 GNOT = 1,000,000 ugnot = 10,000 bytes net storage delta
```

## Example workflow

A typical analysis session:

```bash
# 1. Generate log
GNO_REALM_STATS_LOG=/tmp/mint.log \
  go test -v -run 'TestTestdata/position_storage_poisition_lifecycle' \
  ./gno.land/pkg/integration/ -count=1 -timeout 300s

# 2. See what operations are in the log
python3 docs/realm-stats/analyze_object_costs.py /tmp/mint.log --list

# 3. Get variable-level breakdown for first Mint
python3 docs/realm-stats/analyze_object_costs.py /tmp/mint.log "position.Mint" --by-var

# 4. Get detailed object breakdown for the biggest cost driver
python3 docs/realm-stats/analyze_object_costs.py /tmp/mint.log "position.Mint"

# 5. Compare 1st vs 2nd Mint (tick creation vs reuse)
python3 docs/realm-stats/analyze_object_costs.py /tmp/mint.log "position.Mint" --nth 1 --by-var
python3 docs/realm-stats/analyze_object_costs.py /tmp/mint.log "position.Mint" --nth 2 --by-var
```
