---
name: gno-storage-patterns
description: Find storage inefficiency patterns in Gno realm code. Use when auditing a realm for gas optimization, searching for anti-patterns, or when the user asks to find storage issues in their code.
argument-hint: "[path-or-pattern]"
---

# Find Storage Inefficiency Patterns in Gno Code

Scan Gno realm source files for storage anti-patterns that cause high gas costs and storage deposits. Run the searches below against the target path provided in `$ARGUMENTS` (default: current working directory).

For each pattern found, explain **why** it is inefficient and suggest a concrete fix. Reference the `gno-storage-optimization` skill for detailed optimization rules.

## Search procedure

Run the searches in the order below. Each search targets a specific anti-pattern, from highest impact to lowest. Use Grep and Glob tools. Skip searches that are not applicable to the target files.

After all searches, produce a summary report grouped by severity.

### 1. Maps used as persistent storage (Critical)

`map` types do not persist across transactions in Gno. Package-level map declarations in realm code are almost always bugs.

```
Pattern:  var\s+\w+\s*=?\s*map\[
Scope:    *.gno files in r/ (realm) directories
```

If found, recommend replacing with `avl.Tree`.

### 2. Pointer slices in persistent state (High)

Each element in `[]*Struct` becomes a separate Object, incurring 2,000 gas flat cost per element on write. Value-type slices (`[]Struct`) store all elements in a single Object.

```
Pattern:  \[\]\*[A-Z]\w+
Scope:    *.gno files — focus on package-level vars and struct fields
```

When found, check:
- Is this slice stored in persistent state (package-level var or field of a persisted struct)?
- Could the elements reasonably be value types instead?
- Is the collection small enough (< ~100 items) that a value slice is appropriate?

If it is a large collection stored in an `avl.Tree` value (e.g., `avl.Tree` value is `[]*Game`), suggest converting to `[]Game` or storing each element separately in the tree.

### 3. Multiple related avl.Trees as separate variables (High)

Separate package-level `avl.Tree` variables that index the same data must be updated together. If they are not grouped in a struct, a panic between updates causes inconsistent state.

```
Pattern:  avl\.Tree
Scope:    count occurrences per .gno file
Flag:     files with 2+ avl.Tree package-level vars
```

When found, check:
- Do these trees reference the same underlying data (e.g., one by ID, another by user)?
- Are they always updated in the same function?
- If yes to both, suggest grouping them in a struct to ensure atomicity.

### 4. Large structs with mixed-frequency fields (Medium)

When independently-changing data lives in the same struct, modifying any field re-serializes the entire struct (dirty ancestor propagation).

```
Pattern:  type\s+\w+\s+struct\s*\{
Scope:    *.gno files
Flag:     structs with 8+ fields
```

When found, analyze:
- Which fields change frequently (counters, timestamps, status)?
- Which fields are set once and rarely change (config, owner, creation time)?
- Are there fields that always change together vs independently?

Suggest splitting only if fields are **truly independent** (no consistency constraints between them).

### 5. Redundant counters and aggregates (Medium)

Stored counters that track collection size may be redundant if the collection supports O(log n) `.Size()` (like `avl.Tree`). But counters for hot-path reads (like `totalSupply`) are valid.

```
Pattern:  (counter|count|total|num\w*)\s*(\+\+|\+=|-=|--)
Scope:    *.gno files
```

When found, check:
- Is the counter read on a hot path (e.g., `Render`, `RenderHome`)?
- Can the value be computed from an existing data structure's `.Size()`?
- Would computing it require O(n) iteration?

Only flag as redundant if: (a) it can be derived from `.Size()` or similar O(log n) call, AND (b) it is not read on a hot path.

### 6. Package-level slice vars that may need avl.Tree (Medium)

Slices at package level persist but load entirely into memory on any access. For collections that grow over time, `avl.Tree` is more efficient (O(log n) access).

```
Pattern:  var\s+\w+\s+\[\]\w+
Scope:    *.gno files in r/ (realm) directories
```

When found, check:
- Is this a growing, unbounded collection?
- Is it accessed by key lookup (would benefit from tree indexing)?
- Does it have more than ~100 items in practice?

### 7. Verbose avl.Tree keys (Low)

Long string keys increase per-byte storage costs. Common issue: using full paths, addresses, or concatenated strings as keys when compact IDs would suffice.

```
Pattern:  \.Set\(\s*["a-zA-Z].*\+
Scope:    *.gno files
```

Also check if `seqid` is imported — if not, and numeric IDs are used, suggest `seqid.Binary()` for compact keys.

```
Pattern:  padZero|strconv\.Itoa.*\.Set\(
Scope:    *.gno files
```

### 8. Direct type assertions on avl.Tree.Get (Low — code quality)

Repeated inline type assertions are error-prone. Wrapping them in helper functions improves safety and readability.

```
Pattern:  \.Get\(.*\.\(
Scope:    *.gno files
Flag:     files with 3+ occurrences (indicates missing helper)
```

Suggest creating a typed helper:

```go
func getItem(key string) *Item {
    v, exists := items.Get(key)
    if !exists {
        return nil
    }
    return v.(*Item)
}
```

## Output format

After running all searches, produce a report in this format:

```markdown
## Storage Pattern Audit: [target path]

### Critical
- [findings or "None found"]

### High
- [findings]

### Medium
- [findings]

### Low
- [findings]

### Summary
- Total issues found: N
- Estimated impact: [brief assessment]
- Top recommendation: [single most impactful change]
```

For each finding, include:
1. **File and line**: exact location
2. **Pattern**: which anti-pattern was matched
3. **Current code**: the problematic snippet
4. **Suggested fix**: concrete code change
5. **Estimated savings**: qualitative (e.g., "eliminates N Objects, saving ~N*2000 gas per write")
