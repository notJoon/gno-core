package chain

import (
	"math"
	"strconv"

	gno "github.com/gnolang/gno/gnovm/pkg/gnolang"
)

func X_newRealmInstanceID(m *gno.Machine, pkgPath string) string {
	rlm := m.Realm
	if rlm == nil ||
		rlm.Path != pkgPath ||
		!gno.IsRealmPath(rlm.Path) ||
		rlm.ID != gno.PkgIDFromPkgPath(rlm.Path) ||
		!rlm.ID.IsRealmPkg() {
		m.PanicString("invalid realm instance ID realm")
	}
	if rlm.Time == math.MaxUint64 {
		m.PanicString("realm instance ID exhausted")
	}
	rlm.Time++
	m.Store.SetPackageRealm(rlm)
	return pkgPath + "@" + strconv.FormatUint(rlm.Time, 10)
}
