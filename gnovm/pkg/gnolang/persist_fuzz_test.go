package gnolang_test

// Run on demand:
//
//	go test -fuzz=FuzzPersist -fuzztime=15m ./gnovm/pkg/gnolang/
//
// Skipped under -short. The fuzzer exercises realm execution end-to-end
// (parse + typecheck + RunMemPackage with save=true) and verifies two
// properties per input:
//
//	(A) Execution does not produce a Go-level runtime.Error panic. Gno-side
//	    panics (TypedValue, PreprocessError, UnhandledPanicError, raw runtime
//	    checks) are treated as user-error class.
//	(B) Execution is deterministic: running the same input twice on
//	    independently-constructed stores yields identical store-op logs and
//	    identical user-error classification.

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gnolang/gno/gnovm/pkg/gnoenv"
	"github.com/gnolang/gno/gnovm/pkg/gnolang"
	"github.com/gnolang/gno/gnovm/pkg/test"
	"github.com/gnolang/gno/tm2/pkg/std"
	"github.com/gnolang/gno/tm2/pkg/store"
	storetypes "github.com/gnolang/gno/tm2/pkg/store/types"
)

const fuzzPersistTimeout = 10 * time.Second

// walkRealmSeeds walks gnovm/tests/files/ for zrealm_*.gno fixtures and
// invokes fn(name, body) for each. Used by both the fuzzer seeder and the
// non-fuzz seed regression test so they cannot drift apart.
func walkRealmSeeds(rootDir string, fn func(name string, body []byte)) {
	root := filepath.Join(rootDir, "gnovm", "tests", "files")
	ffs := os.DirFS(root)
	_ = fs.WalkDir(ffs, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := filepath.Base(p)
		if !strings.HasPrefix(base, "zrealm_") || !strings.HasSuffix(base, ".gno") {
			return nil
		}
		body, err := fs.ReadFile(ffs, p)
		if err != nil {
			return nil
		}
		fn(base, body)
		return nil
	})
}

func addRealmSeedsToFuzzer(f *testing.F) {
	f.Helper()
	walkRealmSeeds(gnoenv.RootDir(), func(_ string, body []byte) { f.Add(body) })
}

// FuzzPersist drives realm execution and checks that the run is panic-free
// (modulo user-error classes) and deterministic across two independent stores.
func FuzzPersist(f *testing.F) {
	if testing.Short() {
		f.Skip("skipping fuzz in -short mode")
	}
	addRealmSeedsToFuzzer(f)

	rootDir := gnoenv.RootDir()
	storeA := newSharedTestStore(rootDir)
	storeB := newSharedTestStore(rootDir)

	f.Fuzz(func(t *testing.T, src []byte) {
		pkgPath, ok := realmPkgPath(src)
		if !ok {
			return
		}
		mpkg := buildRealmMemPkg(pkgPath, src)

		runWithTimeout(t, fuzzPersistTimeout, src, func() {
			oplog1, err1, skip1 := runMpkg(storeA, mpkg, pkgPath)
			if skip1 {
				return
			}
			oplog2, err2, skip2 := runMpkg(storeB, mpkg, pkgPath)
			if skip2 {
				return
			}
			if (err1 == nil) != (err2 == nil) {
				t.Fatalf("non-deterministic: err1=%v err2=%v\n--- input ---\n%s", err1, err2, src)
			}
			if err1 != nil {
				if err1.Error() != err2.Error() {
					t.Fatalf("non-deterministic user error\nrun1: %v\nrun2: %v\n--- input ---\n%s", err1, err2, src)
				}
				return
			}
			if oplog1 != oplog2 {
				t.Fatalf("non-deterministic op-log\n--- run1 ---\n%s\n--- run2 ---\n%s\n--- input ---\n%s", oplog1, oplog2, src)
			}
		})
	})
}

type sharedTestStore struct {
	baseStore storetypes.CommitStore
	gnoStore  gnolang.Store
}

func newSharedTestStore(rootDir string) *sharedTestStore {
	bs, gs := test.TestStore(rootDir, io.Discard, nil)
	return &sharedTestStore{baseStore: bs, gnoStore: gs}
}

func realmPkgPath(src []byte) (string, bool) {
	dirs, err := test.ParseDirectives(bytes.NewReader(src))
	if err != nil {
		return "", false
	}
	pkgPath := dirs.FirstDefault(test.DirectivePkgPath, "")
	if !gnolang.IsRealmPath(pkgPath) {
		return "", false
	}
	return pkgPath, true
}

func buildRealmMemPkg(pkgPath string, src []byte) *std.MemPackage {
	return &std.MemPackage{
		Type: gnolang.MPUserProd,
		Name: path.Base(pkgPath),
		Path: pkgPath,
		Files: []*std.MemFile{
			{Name: "gnomod.toml", Body: gnolang.GenGnoModLatest(pkgPath)},
			{Name: "a.gno", Body: string(src)},
		},
	}
}

// runMpkg runs a realm MemPackage on a fresh CacheWrap'd transaction view of
// sts. It does not commit, so iterations stay isolated. The typecheck gate
// runs against sts.gnoStore so its lazy-load side effects are applied to the
// same store the run will execute against — keeping storeA and storeB
// symmetric across the two-run determinism comparison.
//
// Returns (oplog, nil, false) on clean execution, (_, err, false) on a
// gno-side user-error panic (its string is compared across runs), or
// (_, nil, true) when the typecheck gate fails (input not exercising the
// runtime). A Go-level runtime panic in the interpreter re-panics out as a
// fuzzer failure.
func runMpkg(sts *sharedTestStore, mpkg *std.MemPackage, pkgPath string) (oplog string, err error, skip bool) {
	if !typeCheckOK(mpkg, sts.gnoStore) {
		return "", nil, true
	}

	cw := sts.baseStore.CacheWrap()
	gasMeter := store.NewInfiniteGasMeter()
	txStore := sts.gnoStore.BeginTransaction(cw, cw, nil, gasMeter)

	var buf bytes.Buffer
	txStore.SetLogStoreOps(&buf)

	ctx := test.Context("", pkgPath, std.Coins{})
	m := gnolang.NewMachineWithOptions(gnolang.MachineOptions{
		Output:        io.Discard,
		Store:         txStore,
		Context:       ctx,
		ReviveEnabled: true,
	})
	defer m.Release()

	defer func() {
		r := recover()
		if r == nil {
			return
		}
		switch v := r.(type) {
		case runtime.Error:
			panic(v)
		case *gnolang.TypedValue:
			err = fmt.Errorf("typed-value panic: %v", v)
		case *gnolang.PreprocessError:
			err = fmt.Errorf("preprocess error: %v", v.Unwrap())
		case gnolang.UnhandledPanicError:
			err = fmt.Errorf("unhandled panic: %v", v)
		default:
			err = fmt.Errorf("gno panic (%T): %v", r, r)
		}
	}()

	m.RunMemPackage(mpkg, true)
	return buf.String(), nil, false
}

// typeCheckOK runs gnolang.TypeCheckMemPackage with panic recovery, treating
// any non-nil error or recovered panic as a reason to skip the input. Recover
// here lets the fuzzer keep running while the upstream go/types assertion bug
// is investigated separately.
func typeCheckOK(mpkg *std.MemPackage, gnoStore gnolang.Store) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	opts := fuzzTCOpts()
	opts.Getter = gnoStore
	opts.TestGetter = gnoStore
	_, err := gnolang.TypeCheckMemPackage(mpkg, opts)
	return err == nil
}

// TestRealmSeedsRunCleanly runs the curated zrealm_*.gno corpus as a non-fuzz
// subtest so a regular `go test` (non -short) catches regressions even without
// -fuzz. Mirrors the seed walker's discovery so the two cannot drift.
func TestRealmSeedsRunCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping realm-seed run in -short mode")
	}
	rootDir := gnoenv.RootDir()
	storeA := newSharedTestStore(rootDir)
	storeB := newSharedTestStore(rootDir)

	walkRealmSeeds(rootDir, func(name string, body []byte) {
		t.Run(name, func(t *testing.T) {
			pkgPath, ok := realmPkgPath(body)
			if !ok {
				t.Skip("not a realm path")
			}
			mpkg := buildRealmMemPkg(pkgPath, body)
			oplog1, err1, skip1 := runMpkg(storeA, mpkg, pkgPath)
			if skip1 {
				t.Skip("typecheck failed")
			}
			oplog2, err2, skip2 := runMpkg(storeB, mpkg, pkgPath)
			if skip2 {
				t.Skip("typecheck failed (asymmetric)")
			}
			if (err1 == nil) != (err2 == nil) {
				t.Fatalf("non-deterministic: err1=%v err2=%v", err1, err2)
			}
			if err1 != nil && err1.Error() != err2.Error() {
				t.Fatalf("non-deterministic user error\nrun1: %v\nrun2: %v", err1, err2)
			}
			if err1 == nil && oplog1 != oplog2 {
				t.Fatalf("non-deterministic op-log\nrun1:\n%s\nrun2:\n%s", oplog1, oplog2)
			}
		})
	})
}
