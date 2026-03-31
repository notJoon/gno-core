package cache_batch_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	dbm "github.com/gnolang/gno/tm2/pkg/db"
	"github.com/gnolang/gno/tm2/pkg/db/lmdbdb"
	_ "github.com/gnolang/gno/tm2/pkg/db/pebbledb"
	"github.com/gnolang/gno/tm2/pkg/store/cache"
	"github.com/gnolang/gno/tm2/pkg/store/dbadapter"
)

func TestCacheBatchWritePebbleDB(t *testing.T) {
	t.Parallel()
	db, err := dbm.NewDB("test_pebble", dbm.PebbleDBBackend, t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	testCacheBatchWrite(t, db)
}

func TestCacheBatchWriteLMDB(t *testing.T) {
	t.Parallel()
	db, err := lmdbdb.NewLMDB("test_lmdb", t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	testCacheBatchWrite(t, db)
}

func testCacheBatchWrite(t *testing.T, db dbm.DB) {
	t.Helper()
	parent := dbadapter.Store{DB: db}
	cs := cache.New(parent)

	const n = 500
	for i := 0; i < n; i++ {
		k := fmt.Sprintf("key%04d", i)
		v := fmt.Sprintf("val%04d", i)
		cs.Set([]byte(k), []byte(v))
	}
	for i := 0; i < 50; i++ {
		k := fmt.Sprintf("key%04d", i)
		cs.Delete([]byte(k))
	}

	// Not visible in parent yet.
	val := parent.Get([]byte("key0100"))
	require.Nil(t, val)

	cs.Write()

	for i := 50; i < n; i++ {
		k := fmt.Sprintf("key%04d", i)
		v := fmt.Sprintf("val%04d", i)
		got := parent.Get([]byte(k))
		require.Equal(t, []byte(v), got, "key %s", k)
	}
	for i := 0; i < 50; i++ {
		k := fmt.Sprintf("key%04d", i)
		got := parent.Get([]byte(k))
		require.Nil(t, got, "key %s should be deleted", k)
	}
}

func TestCacheBatchWriteOverwritePebbleDB(t *testing.T) {
	t.Parallel()
	db, err := dbm.NewDB("test_pebble_ow", dbm.PebbleDBBackend, t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	testCacheBatchWriteOverwrite(t, db)
}

func TestCacheBatchWriteOverwriteLMDB(t *testing.T) {
	t.Parallel()
	db, err := lmdbdb.NewLMDB("test_lmdb_ow", t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	testCacheBatchWriteOverwrite(t, db)
}

func testCacheBatchWriteOverwrite(t *testing.T, db dbm.DB) {
	t.Helper()
	parent := dbadapter.Store{DB: db}

	require.NoError(t, db.Set([]byte("existing"), []byte("old")))

	cs := cache.New(parent)
	cs.Set([]byte("existing"), []byte("new"))
	cs.Set([]byte("fresh"), []byte("val"))
	cs.Write()

	got := parent.Get([]byte("existing"))
	require.Equal(t, []byte("new"), got)
	got = parent.Get([]byte("fresh"))
	require.Equal(t, []byte("val"), got)
}

func TestCacheBatchWriteEmptyPebbleDB(t *testing.T) {
	t.Parallel()
	db, err := dbm.NewDB("test_pebble_empty", dbm.PebbleDBBackend, t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	parent := dbadapter.Store{DB: db}
	cs := cache.New(parent)
	cs.Write()
}

func TestCacheBatchWriteEmptyLMDB(t *testing.T) {
	t.Parallel()
	db, err := lmdbdb.NewLMDB("test_lmdb_empty", t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	parent := dbadapter.Store{DB: db}
	cs := cache.New(parent)
	cs.Write()
}

func TestCacheBatchWriteSetThenDeletePebbleDB(t *testing.T) {
	t.Parallel()
	db, err := dbm.NewDB("test_pebble_sd", dbm.PebbleDBBackend, t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	testCacheBatchWriteSetThenDelete(t, db)
}

func TestCacheBatchWriteSetThenDeleteLMDB(t *testing.T) {
	t.Parallel()
	db, err := lmdbdb.NewLMDB("test_lmdb_sd", t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	testCacheBatchWriteSetThenDelete(t, db)
}

func testCacheBatchWriteSetThenDelete(t *testing.T, db dbm.DB) {
	t.Helper()
	parent := dbadapter.Store{DB: db}
	cs := cache.New(parent)

	cs.Set([]byte("k"), []byte("v"))
	cs.Delete([]byte("k"))
	cs.Write()

	got := parent.Get([]byte("k"))
	require.Nil(t, got)
}

func TestCacheBatchUsesDBBatch(t *testing.T) {
	t.Parallel()
	db, err := lmdbdb.NewLMDB("test_batch_path", t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	parent := dbadapter.Store{DB: db}
	var iface interface{} = parent
	_, ok := iface.(interface{ GetDB() dbm.DB })
	require.True(t, ok, "dbadapter.Store should implement GetDB()")

	cs := cache.New(parent)
	cs.Set([]byte("k"), []byte("v"))
	cs.Write()

	got := parent.Get([]byte("k"))
	require.Equal(t, []byte("v"), got)
}
