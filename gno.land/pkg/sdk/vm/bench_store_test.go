package vm

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gnolang/gno/gno.land/pkg/gnoland/ugnot"
	gnolang "github.com/gnolang/gno/gnovm/pkg/gnolang"
	bft "github.com/gnolang/gno/tm2/pkg/bft/types"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/sdk"
	"github.com/gnolang/gno/tm2/pkg/std"
	storetypes "github.com/gnolang/gno/tm2/pkg/store/types"
)

// ---------------------------------------------------------------------------
// countingGasMeter: wraps an infinite gas meter, counting calls per descriptor.
// ---------------------------------------------------------------------------

type countingGasMeter struct {
	mu         sync.Mutex
	callCounts map[string]int64
	gasAmounts map[string]int64
	totalGas   int64
	inner      storetypes.GasMeter
}

func newCountingGasMeter() *countingGasMeter {
	return &countingGasMeter{
		callCounts: make(map[string]int64),
		gasAmounts: make(map[string]int64),
		inner:      storetypes.NewInfiniteGasMeter(),
	}
}

func (c *countingGasMeter) GasConsumed() storetypes.Gas        { return c.inner.GasConsumed() }
func (c *countingGasMeter) GasConsumedToLimit() storetypes.Gas { return c.inner.GasConsumedToLimit() }
func (c *countingGasMeter) Limit() storetypes.Gas              { return c.inner.Limit() }
func (c *countingGasMeter) Remaining() storetypes.Gas          { return c.inner.Remaining() }
func (c *countingGasMeter) IsPastLimit() bool                  { return c.inner.IsPastLimit() }
func (c *countingGasMeter) IsOutOfGas() bool                   { return c.inner.IsOutOfGas() }

func (c *countingGasMeter) ConsumeGas(amount storetypes.Gas, descriptor string) {
	c.mu.Lock()
	c.callCounts[descriptor]++
	c.gasAmounts[descriptor] += amount
	c.totalGas += amount
	c.mu.Unlock()
	c.inner.ConsumeGas(amount, descriptor)
}

func (c *countingGasMeter) snapshot() storeOpReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	return storeOpReport{
		callCounts: copyMap(c.callCounts),
		gasAmounts: copyMap(c.gasAmounts),
		totalGas:   c.totalGas,
	}
}

func (c *countingGasMeter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callCounts = make(map[string]int64)
	c.gasAmounts = make(map[string]int64)
	c.totalGas = 0
	c.inner = storetypes.NewInfiniteGasMeter()
}

// ---------------------------------------------------------------------------
// Report types and printing
// ---------------------------------------------------------------------------

type storeOpReport struct {
	name       string
	callCounts map[string]int64
	gasAmounts map[string]int64
	totalGas   int64
}

// storeOpsDescriptors: gas descriptors from gnolang/store.go.
var storeOpsDescriptors = []string{
	gnolang.GasGetObjectDesc,
	gnolang.GasSetObjectDesc,
	gnolang.GasGetTypeDesc,
	gnolang.GasSetTypeDesc,
	gnolang.GasGetPackageRealmDesc,
	gnolang.GasSetPackageRealmDesc,
	gnolang.GasAddMemPackageDesc,
	gnolang.GasGetMemPackageDesc,
	gnolang.GasDeleteObjectDesc,
}

func printReport(t *testing.T, reports []storeOpReport) {
	t.Helper()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n%-30s", "Descriptor"))
	for _, r := range reports {
		sb.WriteString(fmt.Sprintf(" | %22s", r.name))
	}
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", 31+25*len(reports)))
	sb.WriteString("\n")

	for _, desc := range storeOpsDescriptors {
		sb.WriteString(fmt.Sprintf("%-30s", desc))
		for _, r := range reports {
			count := r.callCounts[desc]
			gas := r.gasAmounts[desc]
			sb.WriteString(fmt.Sprintf(" | %8d (%11d)", count, gas))
		}
		sb.WriteString("\n")
	}

	// Collect any descriptors not in our known list.
	seen := make(map[string]bool)
	for _, d := range storeOpsDescriptors {
		seen[d] = true
	}
	for _, r := range reports {
		for d := range r.callCounts {
			if !seen[d] {
				seen[d] = true
				sb.WriteString(fmt.Sprintf("%-30s", d))
				for _, r2 := range reports {
					count := r2.callCounts[d]
					gas := r2.gasAmounts[d]
					sb.WriteString(fmt.Sprintf(" | %8d (%11d)", count, gas))
				}
				sb.WriteString("\n")
			}
		}
	}

	sb.WriteString(strings.Repeat("-", 31+25*len(reports)))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("%-30s", "TOTAL STORE GAS"))
	for _, r := range reports {
		sb.WriteString(fmt.Sprintf(" | %22d", r.totalGas))
	}
	sb.WriteString("\n")

	t.Log(sb.String())
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func setupBenchAccount(env testEnv, ctx sdk.Context) crypto.Address {
	addr := crypto.AddressFromPreimage([]byte("bench1"))
	acc := env.acck.NewAccountWithAddress(ctx, addr)
	env.acck.SetAccount(ctx, acc)
	env.bankk.SetCoins(ctx, addr, std.MustParseCoins(ugnot.ValueString(100_000_000)))
	return addr
}

// runWithMeter sets the counting gas meter on the context, creates a gno
// transaction store, and calls fn. It returns the captured report.
func runWithMeter(t *testing.T, env testEnv, meter *countingGasMeter, name string, fn func(sdk.Context)) storeOpReport {
	t.Helper()
	meter.reset()

	// Set the counting meter on the context — this flows into BeginTransaction.
	mctx := env.ctx.WithGasMeter(meter)
	mctx = mctx.WithBlockHeader(&bft.Header{Height: 1, ChainID: "test-chain-id"})
	ctx := env.vmk.MakeGnoTransactionStore(mctx)

	fn(ctx)

	env.vmk.CommitGnoTransactionStore(ctx)

	r := meter.snapshot()
	r.name = name
	return r
}

// ---------------------------------------------------------------------------
// Test: store operations count per transaction type
// ---------------------------------------------------------------------------

func TestStoreOpsCountPerTxType(t *testing.T) {
	env := setupTestEnv()
	ctx := env.vmk.MakeGnoTransactionStore(env.ctx)

	addr := setupBenchAccount(env, ctx)

	// Pre-deploy avl dependency so realms that import it can compile.
	err := deployExamplePackage(env, ctx, addr, "gno.land/p/nt/avl/v0")
	if err != nil {
		t.Fatalf("pre-deploy avl failed: %v", err)
	}
	env.vmk.CommitGnoTransactionStore(ctx)

	meter := newCountingGasMeter()
	var reports []storeOpReport

	// --- 1. MsgAddPackage (simple realm) ---
	reports = append(reports, runWithMeter(t, env, meter, "AddPkg(simple)", func(ctx sdk.Context) {
		const pkgPath = "gno.land/r/bench/simple"
		files := []*std.MemFile{
			{Name: "gnomod.toml", Body: gnolang.GenGnoModLatest(pkgPath)},
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
		msg := NewMsgAddPackage(addr, pkgPath, files)
		err := env.vmk.AddPackage(ctx, msg)
		if err != nil {
			t.Fatalf("MsgAddPackage(simple) failed: %v", err)
		}
	}))

	// --- 2. MsgAddPackage (with avl dependency) ---
	reports = append(reports, runWithMeter(t, env, meter, "AddPkg(avl)", func(ctx sdk.Context) {
		const pkgPath = "gno.land/r/bench/withdep"
		files := []*std.MemFile{
			{Name: "gnomod.toml", Body: gnolang.GenGnoModLatest(pkgPath)},
			{Name: "withdep.gno", Body: `package withdep

import "gno.land/p/nt/avl/v0"

var tree avl.Tree

func Set(cur realm, key, val string) {
	tree.Set(key, val)
}

func Get(key string) string {
	v, ok := tree.Get(key)
	if !ok {
		return ""
	}
	return v.(string)
}
`},
		}
		msg := NewMsgAddPackage(addr, pkgPath, files)
		err := env.vmk.AddPackage(ctx, msg)
		if err != nil {
			t.Fatalf("MsgAddPackage(withdep) failed: %v", err)
		}
	}))

	// --- 3. MsgCall (simple: Inc - state mutation) ---
	reports = append(reports, runWithMeter(t, env, meter, "Call(Inc)", func(ctx sdk.Context) {
		msg := NewMsgCall(addr, nil, "gno.land/r/bench/simple", "Inc", nil)
		_, err := env.vmk.Call(ctx, msg)
		if err != nil {
			t.Fatalf("MsgCall(Inc) failed: %v", err)
		}
	}))

	// --- 4. MsgCall (Echo - read only with args) ---
	reports = append(reports, runWithMeter(t, env, meter, "Call(Echo)", func(ctx sdk.Context) {
		msg := NewMsgCall(addr, nil, "gno.land/r/bench/simple", "Echo", []string{"hello"})
		_, err := env.vmk.Call(ctx, msg)
		if err != nil {
			t.Fatalf("MsgCall(Echo) failed: %v", err)
		}
	}))

	// --- 5. MsgCall (avl.Set - first insert) ---
	reports = append(reports, runWithMeter(t, env, meter, "Call(avl.Set)", func(ctx sdk.Context) {
		msg := NewMsgCall(addr, nil, "gno.land/r/bench/withdep", "Set", []string{"key1", "value1"})
		_, err := env.vmk.Call(ctx, msg)
		if err != nil {
			t.Fatalf("MsgCall(avl.Set) failed: %v", err)
		}
	}))

	// --- 6. MsgCall (avl.Set - second insert, tree grows) ---
	reports = append(reports, runWithMeter(t, env, meter, "Call(avl.Set#2)", func(ctx sdk.Context) {
		msg := NewMsgCall(addr, nil, "gno.land/r/bench/withdep", "Set", []string{"key2", "value2"})
		_, err := env.vmk.Call(ctx, msg)
		if err != nil {
			t.Fatalf("MsgCall(avl.Set#2) failed: %v", err)
		}
	}))

	// --- 7. MsgRun (simple script) ---
	reports = append(reports, runWithMeter(t, env, meter, "Run(simple)", func(ctx sdk.Context) {
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
			t.Fatalf("MsgRun(simple) failed: %v", err)
		}
	}))

	// --- 8. MsgRun (calling deployed realm) ---
	reports = append(reports, runWithMeter(t, env, meter, "Run(callRealm)", func(ctx sdk.Context) {
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
			t.Fatalf("MsgRun(callRealm) failed: %v", err)
		}
	}))

	printReport(t, reports)
}

func copyMap(m map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
