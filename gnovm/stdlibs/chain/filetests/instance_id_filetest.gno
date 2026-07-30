// PKGPATH: gno.land/r/test/instance

package instance

import (
	"chain"
	"testing"
)

func callNewRealmInstanceID(_ int, rlm realm) (got any) {
	defer func() {
		if err := recover(); err != nil {
			got = err
		}
	}()
	got = chain.NewRealmInstanceID(0, rlm)
	return
}

func checkStale(cur realm, stale realm) {
	println("stale:", callNewRealmInstanceID(0, stale))
}

func main(cur realm) {
	println("valid:", callNewRealmInstanceID(0, cur))
	println("subrealm:", callNewRealmInstanceID(0, cur.Sub("child")))

	spoofed := testing.MakeRealm(cur.Address(), cur.PkgPath(), cur.Previous())
	println("spoofed:", callNewRealmInstanceID(0, spoofed))

	checkStale(cross(cur), cur)
}

// Output:
// valid: gno.land/r/test/instance@7
// subrealm: invalid realm instance ID realm
// spoofed: invalid realm instance ID realm
// stale: invalid realm instance ID realm
