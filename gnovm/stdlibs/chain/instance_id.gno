package chain

// NewRealmInstanceID returns an opaque ID owned by the current primary
// persistent realm. The leading parameter keeps rlm out of first position so
// this wrapper remains non-crossing.
func NewRealmInstanceID(_ int, rlm realm) string {
	if !rlm.IsCurrent() ||
		rlm.Subpath() != "" ||
		rlm.IsEphemeral() {
		panic("invalid realm instance ID realm")
	}

	return newRealmInstanceID(rlm.PkgPath())
}

func newRealmInstanceID(pkgPath string) string
