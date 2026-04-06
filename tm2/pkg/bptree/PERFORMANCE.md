# Disk I/O Performance: B+32 vs IAVL

B+32 = B+ tree with branching factor 32 (up to 32 children per inner node).

Expected disk reads and writes per single GET/SET/DELETE operation.
All numbers are **logical KV operations** (batch.Set / db.Get calls against
the underlying LevelDB/PebbleDB), not physical disk seeks. The LSM layer
adds its own read/write amplification on top of these numbers.

All numbers assume the default 10,000-entry LRU node cache.

**Per-block overhead (amortized away in per-op tables):**
- `LoadVersion`: 1 root ref read (once per block start)
- `SaveVersion`: 1 root ref write (once per block commit)
- These are O(1) per block, negligible at ~1000 mutations/block.

**Important: cache memory is not equal.** The 10K-node cache uses ~43MB
for B+32 (nodes are ~4.3KB in memory) vs ~1.3MB for IAVL (nodes are ~130B).
See the "Equal Memory Comparison" section for a fair same-budget analysis.

## B+32 Tree

### No cache

Every node on the root-to-leaf path is a disk read.

| Items | Height | GET reads | GET writes | SET reads | SET writes | DEL reads | DEL writes |
|-------|--------|-----------|------------|-----------|------------|-----------|------------|
| 25M   |      6 |         7 |          0 |         6 |          7 |         6 |          6 |
| 50M   |      6 |         7 |          0 |         6 |          7 |         6 |          6 |
| 100M  |      6 |         7 |          0 |         6 |          7 |         6 |          6 |
| 200M  |      7 |         8 |          0 |         7 |          8 |         7 |          7 |
| 400M  |      7 |         8 |          0 |         7 |          8 |         7 |          7 |
| 800M  |      7 |         8 |          0 |         7 |          8 |         7 |          7 |
| 1.6G  |      7 |         8 |          0 |         7 |          8 |         7 |          7 |
| 3.2G  |      8 |         9 |          0 |         8 |          9 |         8 |          8 |
| 6.4G  |      8 |         9 |          0 |         8 |          9 |         8 |          8 |

**GET reads** = height (all inner nodes + leaf) + 1 out-of-line value read.
**SET reads** = height (all inner nodes + leaf). No value read needed.
**SET writes** = height COW'd nodes + 1 out-of-line value write.
**DEL reads** = same as SET reads.
**DEL writes** = height COW'd nodes (no value write).

### With 10K-node cache (~43MB)

Cache holds all inner nodes for trees up to ~200M items.
At larger sizes, the deepest inner level partially spills.

| Items | Height | GET reads | GET writes | SET reads | SET writes | DEL reads | DEL writes |
|-------|--------|-----------|------------|-----------|------------|-----------|------------|
| 25M   |      6 |       2.9 |          0 |       1.9 |          7 |       1.9 |          6 |
| 50M   |      6 |       3.0 |          0 |       2.0 |          7 |       2.0 |          6 |
| 100M  |      6 |       3.0 |          0 |       2.0 |          7 |       2.0 |          6 |
| 200M  |      7 |       3.5 |          0 |       2.5 |          8 |       2.5 |          7 |
| 400M  |      7 |       3.8 |          0 |       2.8 |          8 |       2.8 |          7 |
| 800M  |      7 |       3.9 |          0 |       2.9 |          8 |       2.9 |          7 |
| 1.6G  |      7 |       4.0 |          0 |       3.0 |          8 |       3.0 |          7 |
| 3.2G  |      8 |       4.3 |          0 |       3.3 |          9 |       3.3 |          8 |
| 6.4G  |      8 |       4.7 |          0 |       3.7 |          9 |       3.7 |          8 |

**GET reads** = uncached inner nodes on path + 1 leaf + 1 out-of-line value read.
**SET reads** = uncached inner nodes on path + 1 leaf (no value read needed).
**SET writes** = height COW'd nodes + 1 value write.
**DEL writes** = height COW'd nodes.
Writes are identical to cold cache — every COW'd node must be persisted.

Notes:
- SET/DEL writes are independent of cache — every COW'd node must be persisted.
- Occasional splits add ~1/B ≈ 0.03 extra writes per SET (amortized).
- Occasional merges add ~1/B ≈ 0.03 extra writes per DEL (amortized).
- The out-of-line value write (SET only) is always 1, regardless of tree size.

## IAVL Tree

### No cache

Every node on the root-to-leaf path is a disk read. Value is inline, no extra read.

| Items | Height | GET reads | GET (fast) | GET writes | SET reads | SET writes | SET writes (fast) | DEL reads | DEL writes | DEL writes (fast) |
|-------|--------|-----------|------------|------------|-----------|------------|-------------------|-----------|------------|-------------------|
| 25M   |     26 |        26 |          1 |          0 |        26 |         26 |                27 |        26 |         26 |                27 |
| 50M   |     27 |        27 |          1 |          0 |        27 |         27 |                28 |        27 |         27 |                28 |
| 100M  |     28 |        28 |          1 |          0 |        28 |         28 |                29 |        28 |         28 |                29 |
| 200M  |     29 |        29 |          1 |          0 |        29 |         29 |                30 |        29 |         29 |                30 |
| 400M  |     30 |        30 |          1 |          0 |        30 |         30 |                31 |        30 |         30 |                31 |
| 800M  |     31 |        31 |          1 |          0 |        31 |         31 |                32 |        31 |         31 |                32 |
| 1.6G  |     32 |        32 |          1 |          0 |        32 |         32 |                33 |        32 |         32 |                33 |
| 3.2G  |     33 |        33 |          1 |          0 |        33 |         33 |                34 |        33 |         33 |                34 |
| 6.4G  |     34 |        34 |          1 |          0 |        34 |         34 |                35 |        34 |         34 |                35 |

**GET reads** = full height (value is inline in leaf node, no extra read).
**GET (fast)** = 1 fast node index lookup (bypasses tree entirely, cache irrelevant).
**SET/DEL reads** = full height (must traverse tree for COW path).
**SET/DEL writes** = height COW'd nodes (value is inline, no separate write).
**SET/DEL writes (fast)** = +1 additional fast node index write (duplicates value).

### With 10K-node cache (~1.3MB)

Cache covers top ~13 levels (2^13 = 8,192 nodes).
Fast node cache: 100,000 entries (separate index for latest-version GETs).

| Items | Height | GET reads | GET (fast) | GET writes | SET reads | SET writes | SET writes (fast) | DEL reads | DEL writes | DEL writes (fast) |
|-------|--------|-----------|------------|------------|-----------|------------|-------------------|-----------|------------|-------------------|
| 25M   |     26 |        13 |          1 |          0 |        13 |         26 |                27 |        13 |         26 |                27 |
| 50M   |     27 |        14 |          1 |          0 |        14 |         27 |                28 |        14 |         27 |                28 |
| 100M  |     28 |        15 |          1 |          0 |        15 |         28 |                29 |        15 |         28 |                29 |
| 200M  |     29 |        16 |          1 |          0 |        16 |         29 |                30 |        16 |         29 |                30 |
| 400M  |     30 |        17 |          1 |          0 |        17 |         30 |                31 |        17 |         30 |                31 |
| 800M  |     31 |        18 |          1 |          0 |        18 |         31 |                32 |        18 |         31 |                32 |
| 1.6G  |     32 |        19 |          1 |          0 |        19 |         32 |                33 |        19 |         32 |                33 |
| 3.2G  |     33 |        20 |          1 |          0 |        20 |         33 |                34 |        20 |         33 |                34 |
| 6.4G  |     34 |        21 |          1 |          0 |        21 |         34 |                35 |        21 |         34 |                35 |

**GET reads** = height - 13 cached levels (value is inline in leaf node).
**GET reads (fast)** = 1 fast node index lookup (bypasses tree entirely).
**GET writes** = 0 (read-only).
**SET/DEL reads** = height - 13 cached levels (must traverse for COW; fast node doesn't help).
**SET/DEL writes** = height COW'd nodes (value is inline, no separate write).
**SET/DEL writes (fast)** = +1 additional fast node index write (duplicates value).

The cache saves ~13 reads per operation. Writes are unchanged.

Notes:
- Fast node index reduces GET reads to 1 but adds 1 write per SET/DEL.
- Fast node index doubles value storage (value stored both in tree leaf and fast index).
- SET/DEL reads are the same with or without fast nodes — COW requires the full tree path.
- The 10K-node cache is only ~1.3MB for IAVL (nodes are small).

## Side-by-Side Comparison (both cached, default 10K nodes)

| Items | B+32 GET | IAVL GET | IAVL+fast GET | B+32 SET | IAVL SET | IAVL+fast SET |
|-------|----------|----------|---------------|----------|----------|---------------|
| 25M   | 3R / 0W  | 13R / 0W | 1R / 0W       | 2R / 7W  | 13R / 26W | 13R / 27W    |
| 50M   | 3R / 0W  | 14R / 0W | 1R / 0W       | 2R / 7W  | 14R / 27W | 14R / 28W    |
| 100M  | 3R / 0W  | 15R / 0W | 1R / 0W       | 2R / 7W  | 15R / 28W | 15R / 29W    |
| 200M  | 4R / 0W  | 16R / 0W | 1R / 0W       | 3R / 8W  | 16R / 29W | 16R / 30W    |
| 400M  | 4R / 0W  | 17R / 0W | 1R / 0W       | 3R / 8W  | 17R / 30W | 17R / 31W    |
| 800M  | 4R / 0W  | 18R / 0W | 1R / 0W       | 3R / 8W  | 18R / 31W | 18R / 32W    |
| 1.6G  | 4R / 0W  | 19R / 0W | 1R / 0W       | 3R / 8W  | 19R / 32W | 19R / 33W    |
| 3.2G  | 4R / 0W  | 20R / 0W | 1R / 0W       | 3R / 9W  | 20R / 33W | 20R / 34W    |
| 6.4G  | 5R / 0W  | 21R / 0W | 1R / 0W       | 4R / 9W  | 21R / 34W | 21R / 35W    |

## Equal Memory Comparison

The default 10K-node cache uses very different memory:
- B+32: 10K × ~4.3KB = **~43MB**
- IAVL: 10K × ~130B = **~1.3MB**

At equal **43MB** memory budget, IAVL can cache ~330K nodes = top ~18 levels
(2^18 = 262K; partial level 19). This narrows the IAVL read gap:

| Items | B+32 GET (43MB) | IAVL GET (43MB) | IAVL+fast GET | B+32 SET reads | IAVL SET reads (43MB) |
|-------|-----------------|-----------------|---------------|----------------|----------------------|
| 25M   | 3R              | 8R              | 1R            | 2R             | 8R                   |
| 50M   | 3R              | 9R              | 1R            | 2R             | 9R                   |
| 100M  | 3R              | 10R             | 1R            | 2R             | 10R                  |
| 200M  | 4R              | 11R             | 1R            | 3R             | 11R                  |
| 400M  | 4R              | 12R             | 1R            | 3R             | 12R                  |
| 800M  | 4R              | 13R             | 1R            | 3R             | 13R                  |
| 1.6G  | 4R              | 14R             | 1R            | 3R             | 14R                  |
| 3.2G  | 4R              | 15R             | 1R            | 3R             | 15R                  |
| 6.4G  | 5R              | 16R             | 1R            | 4R             | 16R                  |

At equal memory, B+32 GET is **~3-4x** fewer reads (not 4-5x).
**SET writes are unchanged** — writes depend on height, not cache.

## Bytes Written per SET

B+32 writes fewer operations but larger nodes. IAVL writes more
operations but smaller nodes. Both matter for LSM-tree backends.

| Items | B+32 ops | B+32 bytes | IAVL ops | IAVL bytes | IAVL+fast bytes |
|-------|----------|------------|----------|------------|-----------------|
| 25M   |        7 |     ~9.7KB |       26 |     ~2.6KB |          ~2.7KB |
| 50M   |        7 |     ~9.7KB |       27 |     ~2.7KB |          ~2.8KB |
| 100M  |        7 |     ~9.7KB |       28 |     ~2.8KB |          ~2.9KB |
| 200M  |        8 |    ~11.3KB |       29 |     ~2.9KB |          ~3.0KB |
| 400M  |        8 |    ~11.3KB |       30 |     ~3.0KB |          ~3.1KB |
| 800M  |        8 |    ~11.3KB |       31 |     ~3.1KB |          ~3.2KB |
| 1.6G  |        8 |    ~11.3KB |       32 |     ~3.2KB |          ~3.3KB |
| 3.2G  |        9 |    ~12.9KB |       33 |     ~3.3KB |          ~3.4KB |
| 6.4G  |        9 |    ~12.9KB |       34 |     ~3.4KB |          ~3.5KB |

B+32 bytes: height × ~1,600B avg node + ~132B value.
IAVL bytes: height × ~100B avg node (value is inline).

B+32 writes **~3.5x more bytes** per SET despite ~4x fewer operations.
For LSM-tree backends, write amplification is proportional to bytes
written, not operation count. Both factors matter; neither alone tells
the full story.

## Batched Performance

The tables above show unbatched per-op costs. In production, a block
batches ~1000 mutations into one SaveVersion. With batching, upper
COW path nodes are shared across mutations that traverse the same
inner nodes. The sharing is computed via:

`distinct(N, M) = N × (1 - e^(-M/N))`

where N = nodes at a level, M = mutations. When M << N, no sharing.
When M >> N, full sharing (the node is COW'd once for the block).

### B+32 batched writes per mutation

| Items |  500 muts | 1000 muts | 2000 muts | Unbatched |
|-------|-----------|-----------|-----------|-----------|
| 100M  |       4.6 |       4.4 |       4.1 |         7 |
| 1G    |       5.3 |       5.1 |       4.9 |         8 |
| 10G   |       6.1 |       5.9 |       5.6 |         9 |

Reduction: **34-41%** from batching, depending on mutation count.

### IAVL batched writes per mutation

| Items |  500 muts | 1000 muts | 2000 muts | Unbatched |
|-------|-----------|-----------|-----------|-----------|
| 100M  |      19.1 |      18.1 |      17.1 |        28 |
| 1G    |      22.1 |      21.1 |      20.1 |        31 |
| 10G   |      26.1 |      25.1 |      24.1 |        35 |

Reduction: **25-39%** from batching.

### Batched ratio (IAVL / B+32)

| Items |  500 muts | 1000 muts | 2000 muts |
|-------|-----------|-----------|-----------|
| 100M  |      4.2x |      4.2x |      4.2x |
| 1G    |      4.2x |      4.2x |      4.1x |
| 10G   |      4.3x |      4.3x |      4.3x |

The **~4x ratio is stable** across batch sizes and tree sizes.
Batching helps both trees roughly equally (~35% reduction each).
The claim that "batching disproportionately favors B+32" was incorrect.

### Bytes per block (1000 mutations, 100M items)

- B+32: ~4,353 ops × ~1,400B avg ≈ **6.1MB** per block
- IAVL: ~18,143 ops × ~100B avg ≈ **1.8MB** per block

IAVL writes ~3x fewer bytes per block despite ~4x more operations.

## Key Takeaways

**GET performance:**
- B+32 with 10K cache (43MB): 3-5 reads. No fast node index needed.
- IAVL without fast nodes (1.3MB cache): 13-21 reads (~4-5x more).
- IAVL at equal 43MB cache: 8-16 reads (~3-4x more).
- IAVL with fast nodes: 1 read (fastest, but requires maintaining a separate index).

**SET/DEL performance:**
- B+32: 2-4 reads + 7-9 writes = **9-13 total ops** (~10-13KB).
- IAVL: 13-21 reads + 26-34 writes = **39-55 total ops** (~3-4KB).
- IAVL+fast: same reads + 1 extra write = **40-56 total ops** (~3-4KB).
- B+32 is **~4x fewer operations** but **~3.5x more bytes** per SET.

**The tradeoff:** B+32's structural advantage is fewer, larger writes.
This means fewer round-trips to the DB and fewer WAL entries, but more
bytes flowing through LSM compaction. Which factor dominates depends on
the workload and storage backend. For read-heavy workloads (queries,
proof generation), B+32 wins clearly. For write-heavy workloads, the
byte volume partially offsets the operation count advantage.

**Writes are determined by tree height.** Cache helps reads but not writes.
IAVL's height of 26-34 means 26-34 writes per SET regardless of cache.
B+32's height of 6-8 means 7-9 writes. This structural difference
cannot be closed by tuning cache size.

**Batching helps both trees equally.** With 1000 mutations per block, COW
path sharing reduces B+32 per-mutation writes from 7 to ~4.4 (38%) and
IAVL from 28 to ~18 (35%). The ~4x operation ratio is stable across
batch sizes and tree sizes.

## Assumptions

- Key size: ~32 bytes average
- Value size: ~100 bytes average (stored out-of-line for B+32, inline for IAVL)
- B+32 fill factor: 69% (ln 2), effective branching factor = 22
- B+32 in-memory node size: ~4.3KB (includes miniTree cache array)
- B+32 on-disk node size: ~1,600B average (inner ~1,746B, leaf ~1,433B)
- IAVL in-memory node size: ~130B (Go struct, excluding GC overhead)
- IAVL on-disk node size: ~100B average (inner ~97B, leaf ~138B)
- IAVL height: 1.04 × log₂(n) (expected for random insertions)
- Default cache: 10,000 nodes for both trees
- Per-block overhead (root ref read/write) amortized away in per-op tables
- All I/O counts are logical KV operations (batch.Set / db.Get), not
  physical disk seeks. The LSM layer adds its own amplification.
