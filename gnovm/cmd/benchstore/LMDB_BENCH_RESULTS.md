# LMDB Benchmark Results

Hardware: Intel Xeon Platinum 8358 @ 2.60GHz, 2 cores, Linux amd64
Keys: 8 bytes, Values: 256 bytes (random, incompressible)
Flags: NoMetaSync | WriteMap | NoReadahead

## Read Latency (Get, random access)

| Keys | DB size | ns/op | B+ depth | Notes |
|------|---------|-------|----------|-------|
| 1K | 324 KB | 766 | 2 | All cached |
| 10K | 3.0 MB | 902 | 2 | All cached |
| 100K | 30.2 MB | 1,170 | 3 | All cached |
| 1M | 301.9 MB | 1,574 | 3 | All cached |
| 10M | 2.9 GB | 1,951 | 3 | All cached |
| 100M | 29.5 GB | 67,676 | 4 | Leaf pages cold, ~1 page fault/read |
| 500M | 147.4 GB | 174,000 | 4 | Branch + leaf pages cold |
| 750M | 221.1 GB | 195,638 | 4 | Mostly disk-bound |

The cliff between 10M and 100M is caused by:
1. B+ tree depth increases from 3 to 4 (branch fanout ~226 with 8-byte keys)
2. DB size exceeds RAM — leaf pages cause major page faults (~50µs each on NVMe)
3. TLB pressure — 29.5 GB mmap = ~7.2M pages vs ~1500 TLB entries

At 500M-750M, the latency flattens — every read is one SSD random read (~200µs),
regardless of how many more keys are added.

## Write Latency (SetOverwrite, batch=1000, per-key)

| Keys | DB size | ns/key | batch total (ms) | Notes |
|------|---------|--------|-------------------|-------|
| 1K | 960 KB | 1,679 | 1.7 | fsync-dominated |
| 10K | 7.6 MB | 6,145 | 6.1 | fsync-dominated |
| 100K | 37.9 MB | 12,234 | 12.2 | fsync-dominated |
| 1M | 312.3 MB | 18,784 | 18.8 | Mixed |
| 10M | 3.0 GB | 29,210 | 29.2 | Mixed |
| 100M | 29.5 GB | 115,843 | 115.8 | Read-dominated |
| 500M | 147.4 GB | 216,161 | 216.2 | Read-dominated |
| 750M | 221.1 GB | 207,129 | 207.1 | Read-dominated |

Each key in the batch requires reading the B+ tree leaf page (same cost as Get),
modifying it via copy-on-write, then one fsync at the end for all dirty pages.

At small DB sizes, the fsync cost dominates (all pages cached, only fsync takes time).
At large DB sizes, reading cold leaf pages dominates (fsync is amortized to ~40µs/key).

Model: `write_ns/key ≈ read_ns + fsync_total / batch_size`

## Write Latency (SetInsert, batch=1000, per-key)

| Keys | DB size | ns/key | batch total (ms) | Notes |
|------|---------|--------|-------------------|-------|
| 1K | 229.1 MB | 1,933 | 1.9 | Sequential append |
| 10K | 235.2 MB | 1,994 | 2.0 | Sequential append |
| 100K | 267.5 MB | 1,931 | 1.9 | Sequential append |
| 1M | 526.4 MB | 1,907 | 1.9 | Sequential append |
| 10M | 3.2 GB | 2,173 | 2.2 | Sequential append |
| 100M | 29.7 GB | 2,179 | 2.2 | Sequential append |

Insert is ~2µs/key regardless of DB size because sequential keys always append
to the rightmost B+ tree leaf, which stays hot in page cache. Not representative
of production workloads where keys are hash-based (random insertion points).

## Performance Model

B+ tree parameters (4KB pages, 8-byte keys, 256-byte values):
- Branch fanout: ~226 entries/page
- Leaf capacity: ~14 entries/page
- Depth 2: up to ~3K keys
- Depth 3: up to ~11.5M keys
- Depth 4: up to ~2.6B keys

Read model:
```
latency = Σ(per level) [ p_cached × ~500ns + p_miss × ~50µs ]
```

| Keys | Depth | Levels cached | Levels cold | Predicted | Actual |
|------|-------|---------------|-------------|-----------|--------|
| 10M | 3 | 3 | 0 | ~1.5µs | 1.95µs |
| 100M | 4 | 3 | 1 | ~52µs | 67.7µs |
| 500M | 4 | 2 | 2 | ~101µs | 174µs |
| 750M | 4 | 1-2 | 2-3 | ~125µs | 196µs |

Write model (batch=1000):
```
ns/key ≈ read_cost + fsync_total / 1000
```

| Keys | Read cost/key | fsync/1000 | Predicted | Actual |
|------|---------------|------------|-----------|--------|
| 1K | ~0 (cached) | ~1.7µs | ~1.7µs | 1.7µs |
| 10M | ~2µs | ~25µs | ~27µs | 29.2µs |
| 100M | ~68µs | ~40µs | ~108µs | 115.8µs |
| 750M | ~196µs | ~40µs | ~236µs | 207µs |

## vs PebbleDB

| Keys | LMDB Get | PebbleDB Get | LMDB speedup |
|------|----------|--------------|--------------|
| 1K | 766 ns | 641 ns | 0.8x |
| 10K | 902 ns | 788 ns | 0.9x |
| 100K | 1,170 ns | 1,360 ns | 1.2x |
| 1M | 1,574 ns | 3,365 ns | 2.1x |
| 10M | 1,951 ns | 17,204 ns | 8.8x |
| 100M | 67,676 ns | 254,936 ns | 3.8x |
| 1B | — | 457,171 ns | — |

LMDB wins decisively above 100K keys. PebbleDB is slightly faster at small
sizes (all in block cache) due to avoiding CGo overhead.

PebbleDB writes are ~3-4µs regardless of DB size (memtable append, no fsync),
but compaction creates deferred I/O load and can stall writes when L0 backs up.
LMDB writes are slower but deterministic — no background processes, no stalls.
