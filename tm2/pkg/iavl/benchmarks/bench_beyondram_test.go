package benchmarks

import (
	"encoding/binary"
	mrand "math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gnolang/gno/tm2/pkg/db/pebbledb"
	"github.com/gnolang/gno/tm2/pkg/iavl"
)

// TestIAVLGetLatencyBeyondRAM fills the underlying tm2 DB with data that
// exceeds available RAM, then measures IAVL Get/Set latency.
//
// This is the crux of the storage gas problem: when the DB doesn't fit in RAM,
// every IAVL tree traversal hits disk I/O, dramatically increasing per-call cost.
//
// The test uses large values (1KB each) to fill memory faster.
// With 16GB RAM, ~20M entries × 1KB = ~20GB should exceed page cache.
//
// WARNING: This test creates tens of GB of data on disk and takes a long time.
// Run with:
//
//	go test -run TestIAVLGetLatencyBeyondRAM -timeout 2h -v ./tm2/pkg/iavl/benchmarks/
func TestIAVLGetLatencyBeyondRAM(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping RAM-exceeding test in short mode")
	}

	const (
		keyLen    = 32
		dataLen   = 1024 // 1KB values to fill RAM faster
		cacheSize = 10000
		batchSize = 50_000
		nQueries  = 1000
	)

	// Milestones at which we pause to measure latency.
	// Each milestone represents total items in the DB at that point.
	milestones := []int{
		100_000,     // ~100MB
		500_000,     // ~500MB
		1_000_000,   // ~1GB
		5_000_000,   // ~5GB
		10_000_000,  // ~10GB — approaching RAM limit
		20_000_000,  // ~20GB — should exceed 16GB RAM
	}

	dirName := t.TempDir()
	t.Logf("DB directory: %s", dirName)

	db, err := pebbledb.NewPebbleDB("bench", dirName)
	require.NoError(t, err)
	defer db.Close()

	tree := iavl.NewMutableTree(db, cacheSize, true, iavl.NewNopLogger())

	type result struct {
		items     int
		getAvgNs  int64
		setAvgNs  int64
		memMB     float64
	}

	var results []result
	var allKeys [][]byte // keep a sample of keys for querying
	inserted := 0

	for _, milestone := range milestones {
		toInsert := milestone - inserted
		if toInsert <= 0 {
			continue
		}

		t.Logf("Inserting %d items (total will be %d)...", toInsert, milestone)
		insertStart := time.Now()

		for i := 0; i < toInsert; i++ {
			key := make([]byte, keyLen)
			binary.BigEndian.PutUint64(key, uint64(inserted+i))
			copy(key[8:], randBytes(keyLen-8))
			_, err := tree.Set(key, randBytes(dataLen))
			require.NoError(t, err)

			// Keep every 1000th key for querying.
			if (inserted+i)%1000 == 0 {
				allKeys = append(allKeys, key)
			}

			if (inserted+i+1)%batchSize == 0 {
				tree.Hash()
				_, _, err := tree.SaveVersion()
				require.NoError(t, err)
			}
		}
		tree.Hash()
		_, _, err := tree.SaveVersion()
		require.NoError(t, err)

		inserted = milestone
		insertDur := time.Since(insertStart)
		t.Logf("  Inserted in %v", insertDur)

		runtime.GC()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		memMB := float64(mem.Alloc) / 1024 / 1024

		// Measure random Get latency.
		l := int32(len(allKeys))
		start := time.Now()
		for range nQueries {
			k := allKeys[mrand.Int31n(l)]
			_, err := tree.Get(k)
			require.NoError(t, err)
		}
		getAvgNs := time.Since(start).Nanoseconds() / int64(nQueries)

		// Measure random Set latency (update existing keys).
		start = time.Now()
		for range nQueries {
			k := allKeys[mrand.Int31n(l)]
			_, err := tree.Set(k, randBytes(dataLen))
			require.NoError(t, err)
		}
		setAvgNs := time.Since(start).Nanoseconds() / int64(nQueries)

		results = append(results, result{
			items:    milestone,
			getAvgNs: getAvgNs,
			setAvgNs: setAvgNs,
			memMB:    memMB,
		})

		t.Logf("  Items: %s, Get: %d ns, Set: %d ns, Go Heap: %.0f MB",
			formatCount(milestone), getAvgNs, setAvgNs, memMB)
	}

	// Print final table.
	t.Logf("")
	t.Logf("%-12s | %12s | %12s | %10s | %12s | %12s",
		"DB Items", "Est. Size", "Warm Get", "Warm Set", "Go Heap", "Get vs 100K")
	t.Logf("%s", "-------------|--------------|--------------|------------|--------------|-------------")

	baseGetNs := results[0].getAvgNs
	for _, r := range results {
		estGB := float64(r.items) * float64(dataLen+keyLen) / 1024 / 1024 / 1024
		ratio := float64(r.getAvgNs) / float64(baseGetNs)
		t.Logf("%-12s | %9.1f GB | %10d ns | %8d ns | %8.0f MB | %10.1fx",
			formatCount(r.items), estGB, r.getAvgNs, r.setAvgNs, r.memMB, ratio)
	}
}
