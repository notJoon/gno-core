package benchstore

// Raw PebbleDB benchmarks measuring the backing DB cost
// that the GnoVM store sits on.
//
// Run with:
//
//	go test -bench=. ./gnovm/cmd/benchstore/ -benchmem -timeout=30m
//
// Override defaults:
//
//	go test -bench=. ./gnovm/cmd/benchstore/ -cache-mb=1024 -memtable-mb=128 -compactions=4

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/gnolang/gno/tm2/pkg/db/pebbledb"
)

var (
	flagCacheMB     = flag.Int("cache-mb", 0, "PebbleDB block cache size in MB (0 = use default 500MB)")
	flagMemtableMB  = flag.Int("memtable-mb", 0, "PebbleDB memtable size in MB (0 = use default 64MB)")
	flagCompactions = flag.Int("compactions", 0, "PebbleDB max concurrent compactions (0 = use default 3)")
	flagMaxKeys     = flag.Int("max-keys", 0, "Skip DB sizes above this many keys (0 = no limit)")
	flagCacheSweep  = flag.String("cache-sweep", "", "Comma-separated cache sizes in MB for GetCacheSweep (e.g. 500,1024,2048,4096,8192)")
	flagSweepKeys   = flag.Int("sweep-keys", 100_000_000, "Number of keys for GetCacheSweep benchmark")
)

func benchPebbleOpts() *pebble.Options {
	opts := pebbledb.DefaultPebbleOptions()
	if *flagCacheMB > 0 {
		opts.Cache = pebble.NewCache(int64(*flagCacheMB) << 20)
	}
	if *flagMemtableMB > 0 {
		opts.MemTableSize = uint64(*flagMemtableMB) << 20
	}
	if *flagCompactions > 0 {
		n := *flagCompactions
		opts.MaxConcurrentCompactions = func() int { return n }
	}
	return opts
}

func printProgress(label string, done, total int) {
	const width = 30
	filled := width * done / total
	fmt.Fprintf(os.Stderr, "\r  %s [%s%s] %d/%d",
		label,
		string(repeat('#', filled)),
		string(repeat(' ', width-filled)),
		done, total)
	if done == total {
		fmt.Fprint(os.Stderr, "\n")
	}
}

func repeat(ch byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = ch
	}
	return b
}

// noopLogger suppresses PebbleDB WAL replay log spam.
type noopLogger struct{}

func (noopLogger) Infof(format string, args ...interface{})  {}
func (noopLogger) Fatalf(format string, args ...interface{}) { panic(fmt.Sprintf(format, args...)) }

// pebbleEnv holds a populated and warmed PebbleDB.
// Setup once, then run Get/Set sub-benchmarks against it.
type pebbleEnv struct {
	db  *pebbledb.PebbleDB
	dir string
	n   int // number of keys populated
}

func newPebbleEnv(n int, valSize int) (*pebbleEnv, error) {
	dir, err := os.MkdirTemp("", "gno-pebble-bench-*")
	if err != nil {
		return nil, err
	}
	db, err := pebbledb.NewPebbleDBWithOpts("bench", dir, benchPebbleOpts())
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	// Populate with varying values to avoid compression artifacts.
	val := make([]byte, valSize)
	prng := rand.New(rand.NewSource(0))
	batch := db.NewBatch()
	for i := 0; i < n; i++ {
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(i))
		prng.Read(val)
		batch.Set(key, val)
		if (i+1)%10000 == 0 {
			batch.Write()
			batch.Close()
			batch = db.NewBatch()
			printProgress("populate", i+1, n)
		}
	}
	batch.Write()
	batch.Close()
	printProgress("populate", n, n)

	// Warmup: iterate full keyspace to prime block cache and bloom filters.
	it, err := db.Iterator(nil, nil)
	if err != nil {
		db.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	warmCount := 0
	for ; it.Valid(); it.Next() {
		warmCount++
		if warmCount%10000 == 0 {
			printProgress("warmup", warmCount, n)
		}
	}
	it.Close()
	printProgress("warmup", n, n)

	return &pebbleEnv{db: db, dir: dir, n: n}, nil
}

func (env *pebbleEnv) Close() {
	env.db.Close()
	fmt.Fprintf(os.Stderr, "  db size: %s\n", dirSize(env.dir))
	os.RemoveAll(env.dir)
}

func dirSize(path string) string {
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	switch {
	case total >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(total)/(1<<30))
	case total >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(total)/(1<<20))
	default:
		return fmt.Sprintf("%.1f KB", float64(total)/(1<<10))
	}
}

func BenchmarkStorePebbleGet(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000, 10_000_000, 100_000_000, 1_000_000_000} {
		n := n
		if *flagMaxKeys > 0 && n > *flagMaxKeys {
			continue
		}
		var env *pebbleEnv
		b.Run(fmt.Sprintf("keys=%d", n), func(b *testing.B) {
			if env == nil {
				var err error
				env, err = newPebbleEnv(n, 256)
				if err != nil {
					b.Fatal(err)
				}
			}
			rng := rand.New(rand.NewSource(42))
			var sink []byte
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := make([]byte, 8)
				binary.BigEndian.PutUint64(key, uint64(rng.Intn(n)))
				sink, _ = env.db.Get(key)
			}
			runtime.KeepAlive(sink)
		})
		if env != nil {
			env.Close()
		}
	}
}

func BenchmarkStorePebbleSetOverwrite(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000, 10_000_000, 100_000_000, 1_000_000_000} {
		n := n
		if *flagMaxKeys > 0 && n > *flagMaxKeys {
			continue
		}
		var env *pebbleEnv
		b.Run(fmt.Sprintf("keys=%d", n), func(b *testing.B) {
			if env == nil {
				var err error
				env, err = newPebbleEnv(n, 256)
				if err != nil {
					b.Fatal(err)
				}
			}
			rng := rand.New(rand.NewSource(42))
			val := make([]byte, 256)
			rng.Read(val)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := make([]byte, 8)
				binary.BigEndian.PutUint64(key, uint64(rng.Intn(n)))
				env.db.Set(key, val)
			}
		})
		if env != nil {
			env.Close()
		}
	}
}

func BenchmarkStorePebbleSetInsert(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000, 10_000_000, 100_000_000, 1_000_000_000} {
		n := n
		if *flagMaxKeys > 0 && n > *flagMaxKeys {
			continue
		}
		var env *pebbleEnv
		b.Run(fmt.Sprintf("keys=%d", n), func(b *testing.B) {
			if env == nil {
				var err error
				env, err = newPebbleEnv(n, 256)
				if err != nil {
					b.Fatal(err)
				}
			}
			val := make([]byte, 256)
			rand.Read(val)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := make([]byte, 8)
				binary.BigEndian.PutUint64(key, uint64(n+i))
				env.db.Set(key, val)
			}
		})
		if env != nil {
			env.Close()
		}
	}
}

func BenchmarkStorePebbleDeleteAndInsert(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000, 10_000_000, 100_000_000, 1_000_000_000} {
		n := n
		if *flagMaxKeys > 0 && n > *flagMaxKeys {
			continue
		}
		var env *pebbleEnv
		b.Run(fmt.Sprintf("keys=%d", n), func(b *testing.B) {
			if env == nil {
				var err error
				env, err = newPebbleEnv(n, 256)
				if err != nil {
					b.Fatal(err)
				}
			}
			rng := rand.New(rand.NewSource(42))
			val := make([]byte, 256)
			rng.Read(val)
			delKey := make([]byte, 8)
			addKey := make([]byte, 8)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Delete a random existing key.
				binary.BigEndian.PutUint64(delKey, uint64(rng.Intn(n)))
				env.db.Delete(delKey)
				// Insert a new key to keep DB size stable.
				binary.BigEndian.PutUint64(addKey, uint64(n+i))
				env.db.Set(addKey, val)
			}
		})
		if env != nil {
			env.Close()
		}
	}
}

// BenchmarkStorePebbleGetCacheSweep populates a DB once, then benchmarks Get
// at different cache sizes by closing and reopening PebbleDB.
//
// Usage:
//
//	go test ./gnovm/cmd/benchstore/ -bench=GetCacheSweep -timeout=6h \
//	    -sweep-keys=100000000 -cache-sweep=500,1024,2048,4096,8192
func BenchmarkStorePebbleGetCacheSweep(b *testing.B) {
	if *flagCacheSweep == "" {
		b.Skip("use -cache-sweep=500,1024,... to run")
	}
	var cacheSizes []int
	for _, s := range strings.Split(*flagCacheSweep, ",") {
		mb, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			b.Fatalf("bad cache size %q: %v", s, err)
		}
		cacheSizes = append(cacheSizes, mb)
	}

	n := *flagSweepKeys

	// Populate once using default options.
	dir, err := os.MkdirTemp("", "gno-cache-sweep-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	func() {
		db, err := pebbledb.NewPebbleDBWithOpts("bench", dir, benchPebbleOpts())
		if err != nil {
			b.Fatal(err)
		}
		defer db.Close()

		val := make([]byte, 256)
		prng := rand.New(rand.NewSource(0))
		batch := db.NewBatch()
		for i := 0; i < n; i++ {
			key := make([]byte, 8)
			binary.BigEndian.PutUint64(key, uint64(i))
			prng.Read(val)
			batch.Set(key, val)
			if (i+1)%10000 == 0 {
				batch.Write()
				batch.Close()
				batch = db.NewBatch()
				printProgress("populate", i+1, n)
			}
		}
		batch.Write()
		batch.Close()
		printProgress("populate", n, n)
	}()

	fmt.Fprintf(os.Stderr, "  db size: %s\n", dirSize(dir))

	for _, mb := range cacheSizes {
		mb := mb

		// Open DB and warmup outside b.Run so it happens once per cache size.
		opts := pebbledb.DefaultPebbleOptions()
		cache := pebble.NewCache(int64(mb) << 20)
		opts.Cache = cache
		opts.Logger = noopLogger{}

		db, err := pebbledb.NewPebbleDBWithOpts("bench", dir, opts)
		if err != nil {
			b.Fatal(err)
		}

		// Warmup: random reads to fill the cache.
		// Each read loads a 4KB block (~15 KVs), so read enough
		// to fill the cache capacity.
		warmupReads := int(int64(mb) << 20 / 4096)
		if warmupReads > n {
			warmupReads = n
		}
		rng := rand.New(rand.NewSource(99))
		for i := 0; i < warmupReads; i++ {
			key := make([]byte, 8)
			binary.BigEndian.PutUint64(key, uint64(rng.Intn(n)))
			db.Get(key)
			if (i+1)%10000 == 0 {
				printProgress(fmt.Sprintf("warmup %dMB", mb), i+1, warmupReads)
			}
		}
		printProgress(fmt.Sprintf("warmup %dMB", mb), warmupReads, warmupReads)

		b.Run(fmt.Sprintf("cache=%dMB/keys=%d", mb, n), func(b *testing.B) {
			rng := rand.New(rand.NewSource(42))
			var sink []byte
			for i := 0; i < b.N; i++ {
				key := make([]byte, 8)
				binary.BigEndian.PutUint64(key, uint64(rng.Intn(n)))
				sink, _ = db.Get(key)
			}
			runtime.KeepAlive(sink)
		})

		db.Close()
		cache.Unref()
	}
}
