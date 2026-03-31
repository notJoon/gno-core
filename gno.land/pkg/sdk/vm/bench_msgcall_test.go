package vm

import (
	"fmt"
	"testing"
	"time"

	"github.com/gnolang/gno/gno.land/pkg/gnoland/ugnot"
	gnolang "github.com/gnolang/gno/gnovm/pkg/gnolang"
	bft "github.com/gnolang/gno/tm2/pkg/bft/types"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/std"
	storetypes "github.com/gnolang/gno/tm2/pkg/store/types"
)

// TestMsgCallExecTimeByAVLSize measures the actual wall-clock execution time
// of MsgCall as the realm's AVL tree grows.
//
// For each AVL size N, it:
//  1. Pre-populates the realm's AVL tree with N items via MsgCall(Set)
//  2. Measures the time for one additional MsgCall(Set) — insert into existing tree
//  3. Measures the time for one MsgCall(Get) — read from existing tree
//
// Both Set and Get measurements use countingGasMeter to track store operation
// counts and bytes.
//
// Run with:
//
//	go test -run TestMsgCallExecTimeByAVLSize -timeout 30m -v ./gno.land/pkg/sdk/vm/
func TestMsgCallExecTimeByAVLSize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long-running test in short mode")
	}

	avlSizes := []int{0, 10, 100, 1_000, 5_000, 10_000, 100_000, 1_000_000}
	const nMeasurements = 20 // average over multiple calls for stability

	// Gas-per-byte rates for deriving byte totals from gas amounts.
	const (
		getObjectPerByte = 16
		setObjectPerByte = 16
	)

	env := setupTestEnv()
	ctx := env.vmk.MakeGnoTransactionStore(env.ctx)

	// Create funded account.
	addr := crypto.AddressFromPreimage([]byte("msgcall-bench"))
	acc := env.acck.NewAccountWithAddress(ctx, addr)
	env.acck.SetAccount(ctx, acc)
	env.bankk.SetCoins(ctx, addr, std.MustParseCoins(ugnot.ValueString(1_000_000_000_000)))

	// Pre-deploy avl dependency.
	err := deployExamplePackage(env, ctx, addr, "gno.land/p/nt/avl/v0")
	if err != nil {
		t.Fatalf("deploy avl: %v", err)
	}

	// Deploy the benchmark realm.
	const pkgPath = "gno.land/r/bench/avlsize"
	files := []*std.MemFile{
		{Name: "gnomod.toml", Body: gnolang.GenGnoModLatest(pkgPath)},
		{Name: "realm.gno", Body: `package avlsize

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
	msg := NewMsgAddPackage(addr, pkgPath, files)
	err = env.vmk.AddPackage(ctx, msg)
	if err != nil {
		t.Fatalf("deploy realm: %v", err)
	}
	env.vmk.CommitGnoTransactionStore(ctx)

	type opStats struct {
		timeNs     int64
		getObjCnt  int64
		setObjCnt  int64
		getObjBytes int64
		setObjBytes int64
	}

	type result struct {
		avlSize int
		set     opStats
		get     opStats
	}

	var results []result
	insertIdx := 0

	for _, targetSize := range avlSizes {
		// Populate AVL tree to target size.
		for insertIdx < targetSize {
			mctx := env.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
			mctx = mctx.WithBlockHeader(&bft.Header{Height: 1, ChainID: "test-chain-id"})
			tctx := env.vmk.MakeGnoTransactionStore(mctx)

			key := fmt.Sprintf("key-%06d", insertIdx)
			val := fmt.Sprintf("value-%06d", insertIdx)
			setMsg := NewMsgCall(addr, nil, pkgPath, "Set", []string{key, val})
			_, err := env.vmk.Call(tctx, setMsg)
			if err != nil {
				t.Fatalf("populate Set(%d): %v", insertIdx, err)
			}
			env.vmk.CommitGnoTransactionStore(tctx)
			insertIdx++
		}

		// --- Measure Set ---
		var setStats opStats
		for i := range nMeasurements {
			meter := newCountingGasMeter()
			mctx := env.ctx.WithGasMeter(meter)
			mctx = mctx.WithBlockHeader(&bft.Header{Height: 1, ChainID: "test-chain-id"})
			tctx := env.vmk.MakeGnoTransactionStore(mctx)

			key := fmt.Sprintf("measure-set-%d-%d", targetSize, i)
			setMsg := NewMsgCall(addr, nil, pkgPath, "Set", []string{key, "v"})

			start := time.Now()
			_, err := env.vmk.Call(tctx, setMsg)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("measure Set(avl=%d): %v", targetSize, err)
			}
			env.vmk.CommitGnoTransactionStore(tctx)

			setStats.timeNs += elapsed.Nanoseconds()
			setStats.getObjCnt += meter.callCounts[gnolang.GasGetObjectDesc]
			setStats.setObjCnt += meter.callCounts[gnolang.GasSetObjectDesc]
			setStats.getObjBytes += meter.gasAmounts[gnolang.GasGetObjectDesc] / getObjectPerByte
			setStats.setObjBytes += meter.gasAmounts[gnolang.GasSetObjectDesc] / setObjectPerByte
		}
		n := int64(nMeasurements)
		setStats.timeNs /= n
		setStats.getObjCnt /= n
		setStats.setObjCnt /= n
		setStats.getObjBytes /= n
		setStats.setObjBytes /= n

		// --- Measure Get ---
		var getStats opStats
		for i := range nMeasurements {
			meter := newCountingGasMeter()
			mctx := env.ctx.WithGasMeter(meter)
			mctx = mctx.WithBlockHeader(&bft.Header{Height: 1, ChainID: "test-chain-id"})
			tctx := env.vmk.MakeGnoTransactionStore(mctx)

			readKey := fmt.Sprintf("key-%06d", i%max(targetSize, 1))
			getMsg := NewMsgCall(addr, nil, pkgPath, "Get", []string{readKey})

			start := time.Now()
			_, err := env.vmk.Call(tctx, getMsg)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("measure Get(avl=%d): %v", targetSize, err)
			}
			env.vmk.CommitGnoTransactionStore(tctx)

			getStats.timeNs += elapsed.Nanoseconds()
			getStats.getObjCnt += meter.callCounts[gnolang.GasGetObjectDesc]
			getStats.setObjCnt += meter.callCounts[gnolang.GasSetObjectDesc]
			getStats.getObjBytes += meter.gasAmounts[gnolang.GasGetObjectDesc] / getObjectPerByte
			getStats.setObjBytes += meter.gasAmounts[gnolang.GasSetObjectDesc] / setObjectPerByte
		}
		getStats.timeNs /= n
		getStats.getObjCnt /= n
		getStats.setObjCnt /= n
		getStats.getObjBytes /= n
		getStats.setObjBytes /= n

		results = append(results, result{
			avlSize: targetSize,
			set:     setStats,
			get:     getStats,
		})
	}

	// Print Set results.
	t.Logf("")
	t.Logf("=== MsgCall(Set) by AVL size ===")
	t.Logf("%-10s | %12s | %10s | %12s | %10s | %12s",
		"AVL Size", "Time (ns)", "GetObj", "GetObj Bytes", "SetObj", "SetObj Bytes")
	t.Logf("%s", "-----------|--------------|------------|--------------|------------|-------------")
	for _, r := range results {
		t.Logf("%-10d | %12d | %10d | %12d | %10d | %12d",
			r.avlSize, r.set.timeNs, r.set.getObjCnt, r.set.getObjBytes, r.set.setObjCnt, r.set.setObjBytes)
	}

	// Print Get results.
	t.Logf("")
	t.Logf("=== MsgCall(Get) by AVL size ===")
	t.Logf("%-10s | %12s | %10s | %12s | %10s | %12s",
		"AVL Size", "Time (ns)", "GetObj", "GetObj Bytes", "SetObj", "SetObj Bytes")
	t.Logf("%s", "-----------|--------------|------------|--------------|------------|-------------")
	for _, r := range results {
		t.Logf("%-10d | %12d | %10d | %12d | %10d | %12d",
			r.avlSize, r.get.timeNs, r.get.getObjCnt, r.get.getObjBytes, r.get.setObjCnt, r.get.setObjBytes)
	}
}
