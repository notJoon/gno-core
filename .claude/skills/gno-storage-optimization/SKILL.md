---
name: gno-storage-optimization
description: Review and optimize storage usage in Gno realm contracts. Use when writing or reviewing realm code to reduce gas costs and storage deposits, when the user asks about storage efficiency, or when debugging high gas consumption in realm transactions.
---

# Gno Realm Storage Optimization

You are reviewing or writing Gno realm code with the goal of minimizing storage gas costs and storage deposits. Apply the rules below to identify inefficiencies and suggest concrete fixes.

For the full architecture analysis and gas cost breakdown, see [storage-analysis.md](storage-analysis.md).

## How Gno storage works (essential context)

- Every persistent Object is stored as an independent key-value pair via amino serialization.
- Storing an Object costs **VM gas** (16 gas/byte) **plus** **KV gas** (2,000 flat + 30 gas/byte) **plus** **storage deposit** (100 ugnot/byte, locked until deletion).
- When a child Object is modified, the entire **owner chain up to the root PackageValue** is re-serialized (dirty ancestor propagation).
- `map` types do **not** persist across transactions. Only `avl.Tree` and package-level variables persist.

## Rules

### 1. Prefer value-type slices over pointer slices for small collections

Pointer slices (`[]*Struct`) create one independent Object per element. Value-type slices (`[]Struct`) serialize all elements inline in a single Object, eliminating per-element KV Write flat cost (2,000 gas each).

```go
// BAD: N+1 Objects (each *Vote is a separate Object)
var votes []*Vote

// GOOD: 1 Object (all votes inline)
type VoteList []Vote

func (vlist *VoteList) SetVote(voter std.Address, value string) {
    for i, vote := range *vlist {
        if vote.Voter == voter {
            (*vlist)[i] = Vote{Voter: voter, Value: value}
            return
        }
    }
    *vlist = append(*vlist, Vote{Voter: voter, Value: value})
}
```

**Tradeoff**: Modifying one element re-serializes the entire slice. Use this for collections under ~100 items. For larger collections, use `avl.Tree` (rule 5).

### 2. Use `avl.Tree` for persistent collections over ~100 items

`avl.Tree` stores each node as a separate Object. Accessing or modifying a value only loads/re-serializes the search path (O(log n) nodes). `map` types do not persist across transactions — never use them for persistent storage.

| Collection size | Recommended approach | Reason |
|-----------------|---------------------|--------|
| ~10 or fewer | `[]Struct` (value-type slice) | AVL node overhead exceeds benefit |
| 10-100 | Depends on mutation frequency | High mutation -> AVL; low mutation -> slice |
| 100+ | `avl.Tree` | Only O(log n) nodes loaded/re-serialized per access |

Use `seqid` for compact, sortable keys:

```go
import "gno.land/p/nt/seqid/v0"

var (
    idCounter seqid.ID
    items     avl.Tree // seqid key -> *Item
)

func AddItem(data string) {
    id := idCounter.Next() // overflow-safe
    items.Set(id.Binary(), &Item{Data: data}) // 8-byte fixed key
}
```

Key format comparison:

| Format | Size |
|--------|------|
| `seqid.Binary()` | **8 bytes** (most compact, maintains sort order) |
| `seqid.String()` | 7-13 bytes |
| `padZero(id, 10)` | 10 bytes |
| Full path string | 25+ bytes |

Wrap type assertions in helper functions for safety:

```go
func getItem(key string) *Item {
    v, exists := items.Get(key)
    if !exists {
        return nil
    }
    return v.(*Item)
}
```

### 3. Store aggregates only when read-hot and expensive to compute

Do not blindly store or blindly compute aggregates. Decide based on read frequency and computation cost.

**Store** when the value is read frequently and computing it requires O(n) traversal:

```go
// GOOD: totalSupply is read on every RenderHome call
type PrivateLedger struct {
    totalSupply int64 // O(1) read, updated on mint/burn
    balances    avl.Tree
}
```

**Compute** when the value is read infrequently or computation is O(log n) or cheaper:

```go
// GOOD: KnownAccounts is rarely queried
func (tok Token) KnownAccounts() int {
    return tok.ledger.balances.Size() // O(log n) via AVL
}
```

Decision matrix:

| Condition | Store | Compute |
|-----------|-------|---------|
| Read frequently (RenderHome, hot path) | Yes | - |
| Computing requires O(n) or worse | Yes | - |
| Read infrequently | - | Yes |
| Computing is O(1) or O(log n) | - | Yes |
| Always changes together with writes | Yes | - |

### 4. Separate independently-changing state, but never split related data

Splitting a large struct into separate package-level variables reduces dirty ancestor propagation — but only when the variables truly change independently. Splitting related data creates **consistency bugs**.

**Safe to split** — truly independent data:

```go
// GOOD: config changes rarely (admin only), counters change every tx
var config   Config      // admin settings
var counters [100]uint64 // user counters
// No consistency constraint between them
```

**Dangerous to split** — data with consistency constraints:

```go
// BAD: gameStore and user2Games must always be updated together
var (
    gameStore  avl.Tree // gameID -> *Game
    user2Games avl.Tree // address -> []*Game
)
// If addToUser2Games panics after gameStore.Set succeeds,
// the game exists but the user can't find it.
```

**Safe alternative** — bundle related state in a struct:

```go
// GOOD: all related indexes under one owner
type GameRegistry struct {
    games     avl.Tree
    idCounter seqid.ID
    byUser    avl.Tree
}
var registry GameRegistry
```

Quick test: "Can I update variable A without updating variable B and still have a valid state?" If no, they must stay together.

### 5. Minimize serialized data size

Every byte costs gas (46 gas/byte for writes: 16 VM + 30 KV) and storage deposit (100 ugnot/byte).

- Use bit-packing for boolean flags:

```go
// GOOD: 1 byte for multiple flags
type Flags byte

// BAD: each bool is a separate amino field
type Flags struct {
    A, B, C, D bool
}
```

- Use compact numeric types (`uint16` instead of `int`) where the value range allows it.
- Keep string keys and values short. Amino encodes strings verbatim.
- Avoid storing data that can be derived from existing state.

### 6. Consider the full gas cost when designing data structures

The total cost for writing a single Object:

```
VM level:    16 * len(amino_bytes)
KV level:    2,000 + 30 * (len(amino_bytes) + 20)  // +20 for hash prefix
Deposit:     100 * (len(amino_bytes) + 20) ugnot
```

Example — storing a 100-byte object:

```
VM gas:      16 * 100 = 1,600
KV gas:      2,000 + 30 * 120 = 5,600
Total gas:   ~7,200
Deposit:     12,000 ugnot (locked)
```

Reads are much cheaper (1,000 flat + 3/byte KV + 16/byte VM), so design for fewer writes.

## Measurement

When reviewing code, suggest these measurement approaches:

1. **Gas simulation**: `gnokey maketx call ... -simulate only` to compare gas before/after changes.
2. **Storage query**: `gnokey query vm/qstorage -data "gno.land/r/your/realm"` to check actual bytes used.
3. **Local testing**: Use `gnodev` for rapid iteration before deploying.

## Review checklist

When reviewing realm code, check each of these:

- [ ] Are pointer slices (`[]*Struct`) used where value slices (`[]Struct`) would work?
- [ ] Are large collections (100+ items) using `avl.Tree` instead of slices or maps?
- [ ] Is `map` used for persistent storage? (it won't persist — must use `avl.Tree`)
- [ ] Are hot-path aggregates stored for O(1) access?
- [ ] Are rarely-read values computed instead of stored?
- [ ] Is related data that must be updated atomically kept in the same struct?
- [ ] Are independently-changing variables unnecessarily bundled in one struct?
- [ ] Are string keys minimized (using `seqid.Binary()` or similar)?
- [ ] Are boolean flags bit-packed where possible?
- [ ] Can any stored data be derived from existing state instead?
