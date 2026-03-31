package benchmarks

import (
	"encoding/binary"
	"fmt"
	mrand "math/rand"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gnolang/gno/tm2/pkg/db/pebbledb"
	"github.com/gnolang/gno/tm2/pkg/iavl"
)

// BenchmarkIAVLGetByDBSize measures IAVL Get latency as DB item count grows.
// This directly addresses the storage gas parameterization problem:
// the per-call cost of GetObject depends on DB size + RAM availability.
//
// Run with:
//
//	go test -bench BenchmarkIAVLGetByDBSize -benchtime 5s -timeout 30m -v ./tm2/pkg/iavl/benchmarks/
func BenchmarkIAVLGetByDBSize(b *testing.B) {
	sizes := []int{
		1_000,
		10_000,
		100_000,
		1_000_000,
	}

	const (
		keyLen    = 32
		dataLen   = 100 // approximate average serialized gno object size
		cacheSize = 10000
	)

	for _, size := range sizes {
		b.Run(fmt.Sprintf("items=%d", size), func(b *testing.B) {
			dirName := b.TempDir()

			db, err := pebbledb.NewPebbleDB("bench", dirName)
			require.NoError(b, err)
			defer db.Close()

			tree := iavl.NewMutableTree(db, cacheSize, true, iavl.NewNopLogger())

			keys := make([][]byte, size)
			for i := range size {
				key := make([]byte, keyLen)
				binary.BigEndian.PutUint64(key, uint64(i))
				copy(key[8:], randBytes(keyLen-8))
				_, err := tree.Set(key, randBytes(dataLen))
				require.NoError(b, err)
				keys[i] = key

				if (i+1)%50_000 == 0 {
					tree.Hash()
					_, _, err := tree.SaveVersion()
					require.NoError(b, err)
				}
			}
			tree.Hash()
			_, _, err = tree.SaveVersion()
			require.NoError(b, err)

			runtime.GC()

			b.Run("warm-random-get", func(b *testing.B) {
				b.ReportAllocs()
				l := int32(len(keys))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					k := keys[mrand.Int31n(l)]
					_, err := tree.Get(k)
					require.NoError(b, err)
				}
			})

			b.Run("warm-random-set", func(b *testing.B) {
				b.ReportAllocs()
				l := int32(len(keys))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					k := keys[mrand.Int31n(l)]
					_, err := tree.Set(k, randBytes(dataLen))
					require.NoError(b, err)
				}
			})
		})
	}
}

// TestIAVLGetLatencyByDBSize runs a non-benchmark test that prints a latency
// table for different DB sizes, including cold-read simulation.
//
// Cold simulation: closes DB, runs `purge` (macOS disk cache flush), and reopens.
// On non-macOS systems, cold test is skipped.
//
// Run with:
//
//	go test -run TestIAVLGetLatencyByDBSize -timeout 30m -v ./tm2/pkg/iavl/benchmarks/
func TestIAVLGetLatencyByDBSize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long-running latency test in short mode")
	}

	sizes := []int{
		1_000,
		10_000,
		100_000,
		1_000_000,
	}

	const (
		keyLen    = 32
		dataLen   = 100
		cacheSize = 10000
		nQueries  = 5000
	)

	type result struct {
		size      int
		warmGetNs int64
		coldGetNs int64 // -1 if cold test was skipped
		warmSetNs int64
	}

	// Check if purge is available (macOS only).
	canPurge := exec.Command("which", "purge").Run() == nil

	var results []result

	for _, size := range sizes {
		t.Logf("Populating IAVL tree with %d items...", size)

		dirName := t.TempDir()
		db, err := pebbledb.NewPebbleDB("bench", dirName)
		require.NoError(t, err)

		tree := iavl.NewMutableTree(db, cacheSize, true, iavl.NewNopLogger())

		keys := make([][]byte, size)
		for i := range size {
			key := make([]byte, keyLen)
			binary.BigEndian.PutUint64(key, uint64(i))
			copy(key[8:], randBytes(keyLen-8))
			_, err := tree.Set(key, randBytes(dataLen))
			require.NoError(t, err)
			keys[i] = key

			if (i+1)%50_000 == 0 {
				tree.Hash()
				_, _, err := tree.SaveVersion()
				require.NoError(t, err)
			}
		}
		tree.Hash()
		_, _, err = tree.SaveVersion()
		require.NoError(t, err)
		runtime.GC()

		// --- Warm Get (random access) ---
		l := int32(len(keys))
		start := time.Now()
		for range nQueries {
			k := keys[mrand.Int31n(l)]
			_, err := tree.Get(k)
			require.NoError(t, err)
		}
		warmGetNs := time.Since(start).Nanoseconds() / int64(nQueries)

		// --- Warm Set (random access) ---
		start = time.Now()
		for range nQueries {
			k := keys[mrand.Int31n(l)]
			_, err := tree.Set(k, randBytes(dataLen))
			require.NoError(t, err)
		}
		warmSetNs := time.Since(start).Nanoseconds() / int64(nQueries)

		// --- Cold Get: close DB, purge OS cache, reopen ---
		var coldGetNs int64 = -1
		db.Close()
		runtime.GC()

		if canPurge {
			// Flush macOS disk cache. Requires sudo on some systems.
			// If purge fails (no sudo), fall back to just DB reopen.
			_ = exec.Command("purge").Run()
			time.Sleep(500 * time.Millisecond) // let purge settle
		}

		db2, err := pebbledb.NewPebbleDB("bench", dirName)
		require.NoError(t, err)

		tree2 := iavl.NewMutableTree(db2, cacheSize, true, iavl.NewNopLogger())
		_, err = tree2.Load()
		require.NoError(t, err)

		// Cold get: measure first nColdQueries queries separately.
		// These are truly cold — IAVL node cache is empty.
		const nColdQueries = 100
		start = time.Now()
		for range nColdQueries {
			k := keys[mrand.Int31n(l)]
			_, err := tree2.Get(k)
			require.NoError(t, err)
		}
		coldGetNs = time.Since(start).Nanoseconds() / int64(nColdQueries)

		db2.Close()

		results = append(results, result{
			size:      size,
			warmGetNs: warmGetNs,
			coldGetNs: coldGetNs,
			warmSetNs: warmSetNs,
		})
	}

	// Print table
	t.Logf("")
	t.Logf("%-15s | %12s | %12s | %12s | %10s",
		"DB Items", "Warm Get", "Cold Get", "Warm Set", "Cold/Warm")
	t.Logf("%s", "----------------|--------------|--------------|--------------|------------")
	for _, r := range results {
		coldStr := "N/A"
		ratioStr := "N/A"
		if r.coldGetNs >= 0 {
			coldStr = fmt.Sprintf("%10d ns", r.coldGetNs)
			ratio := float64(r.coldGetNs) / float64(r.warmGetNs)
			ratioStr = fmt.Sprintf("%9.1fx", ratio)
		}
		t.Logf("%-15s | %10d ns | %12s | %10d ns | %10s",
			formatCount(r.size), r.warmGetNs, coldStr, r.warmSetNs, ratioStr)
	}

	if !canPurge {
		t.Log("\nNote: 'purge' command not available. Cold reads used DB reopen only (OS page cache still warm).")
	}
}

func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%dM", n/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
