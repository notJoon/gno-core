package gnolang_test

// Run on demand:
//
//	go test -fuzz=FuzzParse       -fuzztime=2m ./gnovm/pkg/gnolang/
//	go test -fuzz=FuzzTypeCheck   -fuzztime=2m ./gnovm/pkg/gnolang/
//	go test -fuzz=FuzzPreprocess  -fuzztime=2m ./gnovm/pkg/gnolang/
//
// All three fuzzers honor -short and skip in normal CI passes. Failure inputs
// are written under testdata/fuzz/<FuzzName>/<hash>; commit minimized
// reproducers as permanent regression seeds when found.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gnolang/gno/gnovm/pkg/gnoenv"
	"github.com/gnolang/gno/gnovm/pkg/gnolang"
	"github.com/gnolang/gno/tm2/pkg/std"
)

const (
	fuzzStageTimeout = 5 * time.Second
	fuzzPkgName      = "main"
)

// addGnoSeedsToFuzzer walks both seed corpora and adds every .gno source that
// currently parses successfully. Files that fail an initial parse are dropped
// so the corpus stays focused on plausible Gno source rather than realm-dump
// output (zrealm_*) or filetest fixtures whose body is not a Go source unit.
func addGnoSeedsToFuzzer(f *testing.F) {
	f.Helper()

	roots := []string{
		filepath.Join(gnoenv.RootDir(), "examples"),
		filepath.Join(gnoenv.RootDir(), "gnovm", "tests", "files"),
	}

	probe := gnolang.NewMachine(fuzzPkgName, nil)
	defer probe.Release()

	for _, root := range roots {
		ffs := os.DirFS(root)
		_ = fs.WalkDir(ffs, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".gno") {
				return nil
			}
			if strings.Contains(path, "/extern/") || strings.HasPrefix(path, "extern/") {
				return nil
			}
			body, err := fs.ReadFile(ffs, path)
			if err != nil {
				return nil
			}
			if _, err := probe.ParseFile(filepath.Base(path), string(body)); err != nil {
				return nil
			}
			f.Add(body)
			return nil
		})
	}
}

// runWithTimeout executes fn in a goroutine and fails the test if it does not
// return within dur. On timeout the fn() goroutine is leaked until it returns
// naturally; acceptable because the fuzzer process exits shortly after a
// failure is reported.
func runWithTimeout(t *testing.T, dur time.Duration, src []byte, fn func()) {
	t.Helper()
	timer := time.NewTimer(dur)
	defer timer.Stop()
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		fn()
	}()
	select {
	case r := <-done:
		if r != nil {
			panic(r)
		}
	case <-timer.C:
		t.Fatalf("timeout after %s\n--- input ---\n%s", dur, src)
	}
}

// wrapMain appends a no-op main if the source has no func main. Only needed
// for stages that demand a runnable entry point; harmless for the parser.
func wrapMain(src string) string {
	if strings.Contains(src, "func main()") {
		return src
	}
	return src + "\n\nfunc main() {}\n"
}

func newFuzzMemPkg(body string) *std.MemPackage {
	return &std.MemPackage{
		Name:  fuzzPkgName,
		Path:  fuzzPkgName,
		Type:  gnolang.MPUserAll,
		Files: []*std.MemFile{{Name: "a.gno", Body: body}},
	}
}

func fuzzTCOpts() gnolang.TypeCheckOptions {
	return gnolang.TypeCheckOptions{
		Getter:     emptyMemPackageGetter{},
		TestGetter: emptyMemPackageGetter{},
		Mode:       gnolang.TCLatestRelaxed,
	}
}

// FuzzParse exercises (*Machine).ParseFile, which already recovers all panics
// into an error. Anything escaping ParseFile (stack overflow, OOM, runtime
// fatal) is a real bug. ParseFile is bounded by the Go parser so no timeout
// wrapper is needed.
func FuzzParse(f *testing.F) {
	if testing.Short() {
		f.Skip("skipping fuzz in -short mode")
	}
	addGnoSeedsToFuzzer(f)

	f.Fuzz(func(t *testing.T, src []byte) {
		m := gnolang.NewMachine(fuzzPkgName, nil)
		defer m.Release()
		_, _ = m.ParseFile("a.gno", string(src))
	})
}

// FuzzTypeCheck drives gnolang.TypeCheckMemPackage with a no-op getter. Inputs
// containing imports will fail typecheck and short-circuit, which is acceptable
// for v1: this fuzzer focuses oracle attention on self-contained source.
func FuzzTypeCheck(f *testing.F) {
	if testing.Short() {
		f.Skip("skipping fuzz in -short mode")
	}
	addGnoSeedsToFuzzer(f)

	f.Fuzz(func(t *testing.T, src []byte) {
		mpkg := newFuzzMemPkg(wrapMain(string(src)))
		runWithTimeout(t, fuzzStageTimeout, src, func() {
			_, _ = gnolang.TypeCheckMemPackage(mpkg, fuzzTCOpts())
		})
	})
}

// FuzzPreprocess targets the most regression-prone stage. The oracle treats
// recovered *PreprocessError values as intended user-error reports and any
// other panic value as a real bug. Inputs are gated on typecheck success to
// keep the signal focused on preprocessor-internal failures.
func FuzzPreprocess(f *testing.F) {
	if testing.Short() {
		f.Skip("skipping fuzz in -short mode")
	}
	addGnoSeedsToFuzzer(f)

	f.Fuzz(func(t *testing.T, src []byte) {
		body := wrapMain(string(src))
		mpkg := newFuzzMemPkg(body)
		if _, err := gnolang.TypeCheckMemPackage(mpkg, fuzzTCOpts()); err != nil {
			return
		}

		m := gnolang.NewMachine(fuzzPkgName, nil)
		defer m.Release()
		fn, err := m.ParseFile("a.gno", body)
		if err != nil || fn == nil {
			return
		}
		fset := &gnolang.FileSet{Files: []*gnolang.FileNode{fn}}

		runWithTimeout(t, fuzzStageTimeout, src, func() {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				if _, ok := r.(*gnolang.PreprocessError); ok {
					return
				}
				t.Fatalf("panic in Preprocess: %T %v\n--- input ---\n%s", r, r, src)
			}()
			_, _ = m.PreprocessFiles(fuzzPkgName, fuzzPkgName, fset, false, false, "")
		})
	})
}

// emptyMemPackageGetter satisfies gnolang.MemPackageGetter for fuzz inputs
// that do not need stdlibs. Imports in fuzz inputs will not resolve, which is
// intentional for v1.
type emptyMemPackageGetter struct{}

func (emptyMemPackageGetter) GetMemPackage(pkgPath string) *std.MemPackage {
	return nil
}
