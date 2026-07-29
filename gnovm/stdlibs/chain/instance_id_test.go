package chain

import (
	"math"
	"testing"

	gno "github.com/gnolang/gno/gnovm/pkg/gnolang"
	"github.com/gnolang/gno/tm2/pkg/db/memdb"
	"github.com/gnolang/gno/tm2/pkg/store/dbadapter"
	storetypes "github.com/gnolang/gno/tm2/pkg/store/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type realmRecordingStore struct {
	gno.Store
	realm *gno.Realm
}

func (s *realmRecordingStore) SetPackageRealm(rlm *gno.Realm) {
	s.realm = rlm
}

func TestNewRealmInstanceID(t *testing.T) {
	const path = "gno.land/r/test/instance"
	rlm := gno.NewRealm(path)
	rlm.Time = 9
	st := &realmRecordingStore{}
	m := &gno.Machine{Realm: rlm, Store: st}

	assert.Equal(t, path+"@10", X_newRealmInstanceID(m, path))
	assert.Equal(t, path+"@11", X_newRealmInstanceID(m, path))
	assert.Equal(t, uint64(11), rlm.Time)
	assert.Same(t, rlm, st.realm)
}

func TestNewRealmInstanceIDRejectsInvalidRealm(t *testing.T) {
	const path = "gno.land/r/test/instance"
	tests := []struct {
		name    string
		realm   *gno.Realm
		pkgPath string
	}{
		{name: "nil realm", pkgPath: path},
		{name: "spoofed path", realm: gno.NewRealm(path), pkgPath: "gno.land/r/test/other"},
		{name: "non-realm path", realm: gno.NewRealm("gno.land/p/test/instance"), pkgPath: "gno.land/p/test/instance"},
		{name: "ephemeral path", realm: gno.NewRealm("gno.land/e/g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5/run"), pkgPath: "gno.land/e/g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5/run"},
		{name: "mismatched ID", realm: &gno.Realm{Path: path, ID: gno.PkgIDFromPkgPath("gno.land/r/test/other")}, pkgPath: path},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &realmRecordingStore{}
			m := &gno.Machine{Realm: tt.realm, Store: st}

			require.Panics(t, func() { X_newRealmInstanceID(m, tt.pkgPath) })
			assert.Nil(t, st.realm)
			if tt.realm != nil {
				assert.Zero(t, tt.realm.Time)
			}
		})
	}
}

func TestNewRealmInstanceIDOverflow(t *testing.T) {
	const path = "gno.land/r/test/instance"
	rlm := gno.NewRealm(path)
	rlm.Time = math.MaxUint64
	st := &realmRecordingStore{}
	m := &gno.Machine{Realm: rlm, Store: st}

	require.Panics(t, func() { X_newRealmInstanceID(m, path) })
	assert.Equal(t, uint64(math.MaxUint64), rlm.Time)
	assert.Nil(t, st.realm)
}

func TestNewRealmInstanceIDCommitReopenAndRollback(t *testing.T) {
	const path = "gno.land/r/test/instance"
	base := dbadapter.StoreConstructor(memdb.NewMemDB(), storetypes.StoreOptions{})
	st := gno.NewStore(nil, base, base)
	st.SetPackageRealm(gno.NewRealm(path))

	wrapped := base.CacheWrap()
	tx := st.BeginTransaction(wrapped, wrapped, nil, nil)
	rlm := tx.GetPackageRealm(path)
	m := &gno.Machine{Realm: rlm, Store: tx}
	assert.Equal(t, path+"@1", X_newRealmInstanceID(m, path))
	assert.Equal(t, path+"@2", X_newRealmInstanceID(m, path))
	tx.Write()
	wrapped.Write()

	reopened := gno.NewStore(nil, base, base)
	assert.Equal(t, uint64(2), reopened.GetPackageRealm(path).Time)

	rollback := base.CacheWrap()
	tx = reopened.BeginTransaction(rollback, rollback, nil, nil)
	rlm = tx.GetPackageRealm(path)
	m = &gno.Machine{Realm: rlm, Store: tx}
	assert.Equal(t, path+"@3", X_newRealmInstanceID(m, path))

	reopened = gno.NewStore(nil, base, base)
	assert.Equal(t, uint64(2), reopened.GetPackageRealm(path).Time)
}
