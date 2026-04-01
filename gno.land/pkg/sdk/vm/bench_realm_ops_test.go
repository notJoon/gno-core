package vm

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	gnolang "github.com/gnolang/gno/gnovm/pkg/gnolang"
	bft "github.com/gnolang/gno/tm2/pkg/bft/types"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/sdk"
	"github.com/gnolang/gno/tm2/pkg/std"
	storetypes "github.com/gnolang/gno/tm2/pkg/store/types"

	"github.com/gnolang/gno/gno.land/pkg/gnoland/ugnot"
)

// TestStoreOpsByRealm measures GetObject/SetObject calls per transaction type,
// broken down by target realm (own realm vs stdlibs vs dependencies).
//
// Run with:
//
//	go test -run TestStoreOpsByRealm -v ./gno.land/pkg/sdk/vm/
func TestStoreOpsByRealm(t *testing.T) {
	env := setupTestEnv()
	ctx := env.vmk.MakeGnoTransactionStore(env.ctx)

	// Create funded account.
	addr := crypto.AddressFromPreimage([]byte("realm-ops-bench"))
	acc := env.acck.NewAccountWithAddress(ctx, addr)
	env.acck.SetAccount(ctx, acc)
	env.bankk.SetCoins(ctx, addr, std.MustParseCoins(ugnot.ValueString(10_000_000_000)))

	// Pre-deploy avl.
	err := deployExamplePackage(env, ctx, addr, "gno.land/p/nt/avl/v0")
	if err != nil {
		t.Fatalf("deploy avl: %v", err)
	}

	// Deploy simple realm.
	const simplePkg = "gno.land/r/bench/simple"
	simpleFiles := []*std.MemFile{
		{Name: "gnomod.toml", Body: gnolang.GenGnoModLatest(simplePkg)},
		{Name: "simple.gno", Body: `package simple

import "chain/runtime"

var counter int

func Inc(cur realm) {
	counter++
}

func Get() int {
	return counter
}

func Echo(cur realm, msg string) string {
	caller := runtime.OriginCaller()
	return "echo:" + msg + ":" + caller.String()
}
`},
	}
	simpleMsg := NewMsgAddPackage(addr, simplePkg, simpleFiles)
	err = env.vmk.AddPackage(ctx, simpleMsg)
	if err != nil {
		t.Fatalf("deploy simple: %v", err)
	}

	// Deploy avl realm.
	const avlPkg = "gno.land/r/bench/withdep"
	avlFiles := []*std.MemFile{
		{Name: "gnomod.toml", Body: gnolang.GenGnoModLatest(avlPkg)},
		{Name: "withdep.gno", Body: `package withdep

import "gno.land/p/nt/avl/v0"

var tree avl.Tree

func Set(cur realm, key, val string) {
	tree.Set(key, val)
}

func Get(cur realm, key string) string {
	v, ok := tree.Get(key)
	if !ok {
		return ""
	}
	return v.(string)
}
`},
	}
	avlMsg := NewMsgAddPackage(addr, avlPkg, avlFiles)
	err = env.vmk.AddPackage(ctx, avlMsg)
	if err != nil {
		t.Fatalf("deploy withdep: %v", err)
	}
	env.vmk.CommitGnoTransactionStore(ctx)

	// Additional pkg paths for MsgAddPackage scenarios.
	const addPkgSimple = "gno.land/r/bench/addpkg_simple"
	const addPkgAvl = "gno.land/r/bench/addpkg_avl"

	// Known pkg paths to register for display.
	knownPaths := []string{
		simplePkg,
		avlPkg,
		addPkgSimple,
		addPkgAvl,
		"gno.land/p/nt/avl/v0",
		"chain",
		"chain/runtime",
		"chain/banker",
		".uverse",
	}

	type scenario struct {
		name string
		fn   func(sdk.Context)
	}

	scenarios := []scenario{
		{"MsgAddPackage(simple)", func(ctx sdk.Context) {
			files := []*std.MemFile{
				{Name: "gnomod.toml", Body: gnolang.GenGnoModLatest(addPkgSimple)},
				{Name: "simple.gno", Body: `package addpkg_simple

var counter int

func Inc(cur realm) {
	counter++
}
`},
			}
			msg := NewMsgAddPackage(addr, addPkgSimple, files)
			err := env.vmk.AddPackage(ctx, msg)
			if err != nil {
				t.Fatalf("AddPackage(simple): %v", err)
			}
		}},
		{"MsgAddPackage(avl)", func(ctx sdk.Context) {
			files := []*std.MemFile{
				{Name: "gnomod.toml", Body: gnolang.GenGnoModLatest(addPkgAvl)},
				{Name: "withdep.gno", Body: `package addpkg_avl

import "gno.land/p/nt/avl/v0"

var tree avl.Tree

func Set(cur realm, key, val string) {
	tree.Set(key, val)
}
`},
			}
			msg := NewMsgAddPackage(addr, addPkgAvl, files)
			err := env.vmk.AddPackage(ctx, msg)
			if err != nil {
				t.Fatalf("AddPackage(avl): %v", err)
			}
		}},
		{"MsgCall(Inc)", func(ctx sdk.Context) {
			msg := NewMsgCall(addr, nil, simplePkg, "Inc", nil)
			_, err := env.vmk.Call(ctx, msg)
			if err != nil {
				t.Fatalf("Call(Inc): %v", err)
			}
		}},
		{"MsgCall(Echo)", func(ctx sdk.Context) {
			msg := NewMsgCall(addr, nil, simplePkg, "Echo", []string{"hello"})
			_, err := env.vmk.Call(ctx, msg)
			if err != nil {
				t.Fatalf("Call(Echo): %v", err)
			}
		}},
		{"MsgCall(avl.Set)", func(ctx sdk.Context) {
			msg := NewMsgCall(addr, nil, avlPkg, "Set", []string{"k1", "v1"})
			_, err := env.vmk.Call(ctx, msg)
			if err != nil {
				t.Fatalf("Call(avl.Set): %v", err)
			}
		}},
		{"MsgRun(simple)", func(ctx sdk.Context) {
			files := []*std.MemFile{
				{Name: "gnomod.toml", Body: gnolang.GenGnoModLatest("gno.land/e/" + addr.String() + "/run")},
				{Name: "script.gno", Body: `package main

func main() {
	println("hello from MsgRun")
}
`},
			}
			msg := NewMsgRun(addr, nil, files)
			_, err := env.vmk.Run(ctx, msg)
			if err != nil {
				t.Fatalf("Run(simple): %v", err)
			}
		}},
		{"MsgRun(callRealm)", func(ctx sdk.Context) {
			files := []*std.MemFile{
				{Name: "gnomod.toml", Body: gnolang.GenGnoModLatest("gno.land/e/" + addr.String() + "/run")},
				{Name: "script.gno", Body: `package main

import "gno.land/r/bench/simple"

func main() {
	simple.Inc(cross)
	println(simple.Get())
}
`},
			}
			msg := NewMsgRun(addr, nil, files)
			_, err := env.vmk.Run(ctx, msg)
			if err != nil {
				t.Fatalf("Run(callRealm): %v", err)
			}
		}},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			// Enable counters on the base gno store.
			counters := env.vmk.gnoStore.EnableStoreOpCounters()
			for _, p := range knownPaths {
				counters.RegisterPkgPath(p)
			}

			mctx := env.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
			mctx = mctx.WithBlockHeader(&bft.Header{Height: 1, ChainID: "test-chain-id"})
			tctx := env.vmk.MakeGnoTransactionStore(mctx)

			sc.fn(tctx)
			env.vmk.CommitGnoTransactionStore(tctx)

			printRealmOpsTable(t, sc.name, counters)

			// Reset for next scenario.
			counters.Reset()
		})
	}
}

func printRealmOpsTable(t *testing.T, name string, counters *gnolang.StoreOpCounters) {
	t.Helper()

	type row struct {
		path     string
		stats    *gnolang.PkgOpStats
		category string // "own", "stdlib", "dep", "other"
	}

	var rows []row
	for pid, stats := range counters.Stats() {
		path := counters.PathForPkgID(pid)
		cat := categorize(path)
		rows = append(rows, row{path: path, stats: stats, category: cat})
	}

	sort.Slice(rows, func(i, j int) bool {
		ci, cj := catOrder(rows[i].category), catOrder(rows[j].category)
		if ci != cj {
			return ci < cj
		}
		return rows[i].path < rows[j].path
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n=== %s ===\n", name))
	sb.WriteString(fmt.Sprintf("%-35s | %4s | %8s | %10s | %8s | %10s | %10s\n",
		"pkg path", "cat", "Get(miss)", "Get bytes", "Set", "Set bytes", "Get(hit)"))
	sb.WriteString(strings.Repeat("-", 105))
	sb.WriteString("\n")

	var totalGetMiss, totalGetHit, totalSet int64
	for _, r := range rows {
		sb.WriteString(fmt.Sprintf("%-35s | %4s | %8d | %10d | %8d | %10d | %10d\n",
			r.path, r.category, r.stats.GetCount, r.stats.GetBytes,
			r.stats.SetCount, r.stats.SetBytes, r.stats.GetCacheHit))
		totalGetMiss += r.stats.GetCount
		totalGetHit += r.stats.GetCacheHit
		totalSet += r.stats.SetCount
	}

	sb.WriteString(strings.Repeat("-", 105))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("%-35s | %4s | %8d | %10s | %8d | %10s | %10d\n",
		"TOTAL", "", totalGetMiss, "", totalSet, "", totalGetHit))

	t.Log(sb.String())
}

func categorize(path string) string {
	switch {
	case strings.HasPrefix(path, "gno.land/r/bench/"):
		return "own"
	case strings.HasPrefix(path, "gno.land/p/"):
		return "dep"
	case path == ".uverse" || path == ".dontcare" ||
		path == "chain" || path == "chain/runtime" || path == "chain/banker" ||
		!strings.Contains(path, "/"):
		return "std"
	case strings.HasPrefix(path, "gno.land/e/"):
		return "run" // MsgRun ephemeral package
	default:
		return "?"
	}
}

func catOrder(cat string) int {
	switch cat {
	case "own":
		return 0
	case "dep":
		return 1
	case "std":
		return 2
	case "run":
		return 3
	default:
		return 4
	}
}
