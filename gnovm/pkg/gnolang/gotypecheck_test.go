package gnolang

import (
	"testing"

	"github.com/gnolang/gno/gnovm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/multierr"
)

type mockPackageGetter []*gnovm.MemPackage

func (mi mockPackageGetter) GetMemPackage(path string) *gnovm.MemPackage {
	for _, pkg := range mi {
		if pkg.Path == path {
			return pkg
		}
	}
	return nil
}

type mockPackageGetterCounts struct {
	mockPackageGetter
	counts map[string]int
}

func (mpg mockPackageGetterCounts) GetMemPackage(path string) *gnovm.MemPackage {
	mpg.counts[path]++
	return mpg.mockPackageGetter.GetMemPackage(path)
}

func TestTypeCheckMemPackage(t *testing.T) {
	t.Parallel()

	// if len(ss) > 0, then multierr.Errors must decompose it in errors, and
	// each error in order must contain the associated string.
	errContains := func(s0 string, ss ...string) func(*testing.T, error) {
		return func(t *testing.T, err error) {
			t.Helper()
			errs := multierr.Errors(err)
			if len(errs) == 0 {
				t.Errorf("expected an error, got nil")
				return
			}
			want := len(ss) + 1
			if len(errs) != want {
				t.Errorf("expected %d errors, got %d", want, len(errs))
				return
			}
			assert.ErrorContains(t, errs[0], s0)
			for idx, err := range errs[1:] {
				assert.ErrorContains(t, err, ss[idx])
			}
		}
	}

	type testCase struct {
		name   string
		pkg    *gnovm.MemPackage
		getter MemPackageGetter
		check  func(*testing.T, error)
	}
	tt := []testCase{
		{
			"Simple",
			&gnovm.MemPackage{
				Name: "hello",
				Path: "gno.land/p/demo/hello",
				Files: []*gnovm.MemFile{
					{
						Name: "hello.gno",
						Body: `
							package hello
							type S struct{}
							func A() S { return S{} }
							func B() S { return A() }`,
					},
				},
			},
			nil,
			nil,
		},
		{
			"WrongReturn",
			&gnovm.MemPackage{
				Name: "hello",
				Path: "gno.land/p/demo/hello",
				Files: []*gnovm.MemFile{
					{
						Name: "hello.gno",
						Body: `
							package hello
							type S struct{}
							func A() S { return S{} }
							func B() S { return 11 }`,
					},
				},
			},
			nil,
			errContains("cannot use 11"),
		},
		{
			"ParseError",
			&gnovm.MemPackage{
				Name: "hello",
				Path: "gno.land/p/demo/hello",
				Files: []*gnovm.MemFile{
					{
						Name: "hello.gno",
						Body: `
							package hello!
							func B() int { return 11 }`,
					},
				},
			},
			nil,
			errContains("found '!'"),
		},
		{
			"MultiError",
			&gnovm.MemPackage{
				Name: "main",
				Path: "gno.land/p/demo/main",
				Files: []*gnovm.MemFile{
					{
						Name: "hello.gno",
						Body: `
							package main
							func main() {
								_, _ = 11
								return 88, 88
							}`,
					},
				},
			},
			nil,
			errContains("assignment mismatch", "too many return values"),
		},
		{
			"TestsIgnored",
			&gnovm.MemPackage{
				Name: "hello",
				Path: "gno.land/p/demo/hello",
				Files: []*gnovm.MemFile{
					{
						Name: "hello.gno",
						Body: `
							package hello
							func B() int { return 11 }`,
					},
					{
						Name: "hello_test.gno",
						Body: `This is not valid Gno code, but it doesn't matter because test
				files are not checked.`,
					},
				},
			},
			nil,
			nil,
		},
		{
			"ImportFailed",
			&gnovm.MemPackage{
				Name: "hello",
				Path: "gno.land/p/demo/hello",
				Files: []*gnovm.MemFile{
					{
						Name: "hello.gno",
						Body: `
							package hello
							import "std"
							func Hello() std.Address { return "hello" }`,
					},
				},
			},
			mockPackageGetter{},
			errContains("import not found: std"),
		},
		{
			"ImportSucceeded",
			&gnovm.MemPackage{
				Name: "hello",
				Path: "gno.land/p/demo/hello",
				Files: []*gnovm.MemFile{
					{
						Name: "hello.gno",
						Body: `
							package hello
							import "std"
							func Hello() std.Address { return "hello" }`,
					},
				},
			},
			mockPackageGetter{
				&gnovm.MemPackage{
					Name: "std",
					Path: "std",
					Files: []*gnovm.MemFile{
						{
							Name: "gnovm.gno",
							Body: `
								package std
								type Address string`,
						},
					},
				},
			},
			nil,
		},
		{
			"ImportBadIdent",
			&gnovm.MemPackage{
				Name: "hello",
				Path: "gno.land/p/demo/hello",
				Files: []*gnovm.MemFile{
					{
						Name: "hello.gno",
						Body: `
							package hello
							import "std"
							func Hello() std.Address { return "hello" }`,
					},
				},
			},
			mockPackageGetter{
				&gnovm.MemPackage{
					Name: "a_completely_different_identifier",
					Path: "std",
					Files: []*gnovm.MemFile{
						{
							Name: "gnovm.gno",
							Body: `
								package a_completely_different_identifier
								type Address string`,
						},
					},
				},
			},
			errContains("undefined: std", "a_completely_different_identifier and not used"),
		},
		{
			// Both inits should be considered, without an "imported and not
			// used" error.
			"ImportTwoInits",
			&gnovm.MemPackage{
				Name: "gns",
				Path: "gno.land/r/demo/gns",
				Files: []*gnovm.MemFile{
					{
						Name: "gns.gno",
						Body: `
							package gns
							import (
								"std"
								"math/overflow"
							)

							var sink any

							func init() {
								sink = std.Address("admin")
							}

							func init() {
								sink = overflow.Add(1, 2)
							}
							`,
					},
				},
			},
			mockPackageGetter{
				&gnovm.MemPackage{
					Name: "std",
					Path: "std",
					Files: []*gnovm.MemFile{
						{
							Name: "gnovm.gno",
							Body: `
								package std
								type Address string`,
						},
					},
				},
				&gnovm.MemPackage{
					Name: "overflow",
					Path: "math/overflow",
					Files: []*gnovm.MemFile{
						{
							Name: "overflow.gno",
							Body: `
								package overflow
								func Add(a, b int) int {
									return a + b
								}`,
						},
					},
				},
			},
			nil,
		},
	}

	cacheMpg := mockPackageGetterCounts{
		mockPackageGetter{
			&gnovm.MemPackage{
				Name: "bye",
				Path: "bye",
				Files: []*gnovm.MemFile{
					{
						Name: "bye.gno",
						Body: `
							package bye
							import "std"
							func Bye() std.Address { return "bye" }`,
					},
				},
			},
			&gnovm.MemPackage{
				Name: "std",
				Path: "std",
				Files: []*gnovm.MemFile{
					{
						Name: "gnovm.gno",
						Body: `
							package std
							type Address string`,
					},
				},
			},
		},
		make(map[string]int),
	}

	tt = append(tt, testCase{
		"ImportWithCache",
		// This test will make use of the importer's internal cache for package `std`.
		&gnovm.MemPackage{
			Name: "hello",
			Path: "gno.land/p/demo/hello",
			Files: []*gnovm.MemFile{
				{
					Name: "hello.gno",
					Body: `
						package hello
						import (
							"std"
							"bye"
						)
						func Hello() std.Address { return bye.Bye() }`,
				},
			},
		},
		cacheMpg,
		func(t *testing.T, err error) {
			t.Helper()
			require.NoError(t, err)
			assert.Equal(t, map[string]int{"std": 1, "bye": 1}, cacheMpg.counts)
		},
	})

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			format := false
			err := TypeCheckMemPackage(tc.pkg, tc.getter, format)
			if tc.check == nil {
				assert.NoError(t, err)
			} else {
				tc.check(t, err)
			}
		})
	}
}

func TestTypeCheckMemPackage_format(t *testing.T) {
	t.Parallel()

	input := `
	package hello
		func Hello(name string) string   {return "hello"  + name
}



`

	pkg := &gnovm.MemPackage{
		Name: "hello",
		Path: "gno.land/p/demo/hello",
		Files: []*gnovm.MemFile{
			{
				Name: "hello.gno",
				Body: input,
			},
		},
	}

	mpkgGetter := mockPackageGetter{}
	format := false
	err := TypeCheckMemPackage(pkg, mpkgGetter, format)
	assert.NoError(t, err)
	assert.Equal(t, input, pkg.Files[0].Body) // unchanged

	expected := `package hello

func Hello(name string) string {
	return "hello" + name
}
`

	format = true
	err = TypeCheckMemPackage(pkg, mpkgGetter, format)
	assert.NoError(t, err)
	assert.NotEqual(t, input, pkg.Files[0].Body)
	assert.Equal(t, expected, pkg.Files[0].Body)
}
