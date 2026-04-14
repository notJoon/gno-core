package doctest

import (
	"github.com/gnolang/gno/gno.land/pkg/sdk/vm"
	bft "github.com/gnolang/gno/tm2/pkg/bft/types"
	"github.com/gnolang/gno/tm2/pkg/db/memdb"
	"github.com/gnolang/gno/tm2/pkg/log"
	"github.com/gnolang/gno/tm2/pkg/sdk"
	authm "github.com/gnolang/gno/tm2/pkg/sdk/auth"
	bankm "github.com/gnolang/gno/tm2/pkg/sdk/bank"
	paramsm "github.com/gnolang/gno/tm2/pkg/sdk/params"
	"github.com/gnolang/gno/tm2/pkg/std"
	"github.com/gnolang/gno/tm2/pkg/store"
	"github.com/gnolang/gno/tm2/pkg/store/dbadapter"
	"github.com/gnolang/gno/tm2/pkg/store/iavl"
)

// setupEnv creates and initializes the execution environment for running
// extracted code blocks. It sets up necessary keepers (account, bank, VM),
// initializes a test chain context, and loads standard libraries.
//
// ref: gno.land/pkg/sdk/vm/common_test.go
func setupEnv(stdlibsDir string) (
	sdk.Context,
	authm.AccountKeeper,
	bankm.BankKeeper,
	*vm.VMKeeper,
	sdk.Context,
) {
	baseKey := store.NewStoreKey("baseKey")
	iavlKey := store.NewStoreKey("iavlKey")

	db := memdb.NewMemDB()

	ms := store.NewCommitMultiStore(db)
	ms.MountStoreWithDB(baseKey, dbadapter.StoreConstructor, db)
	ms.MountStoreWithDB(iavlKey, iavl.StoreConstructor, db)
	ms.LoadLatestVersion()

	ctx := sdk.NewContext(
		sdk.RunTxModeDeliver,
		ms,
		&bft.Header{ChainID: "test-chain-id"},
		log.NewNoopLogger(),
	)
	prmk := paramsm.NewParamsKeeper(iavlKey)
	acck := authm.NewAccountKeeper(iavlKey, prmk.ForModule(authm.ModuleName), std.ProtoBaseAccount)
	bank := bankm.NewBankKeeper(acck, prmk.ForModule(bankm.ModuleName))

	prmk.Register(authm.ModuleName, acck)
	prmk.Register(bankm.ModuleName, bank)

	mcw := ms.MultiCacheWrap()

	vmk := vm.NewVMKeeper(baseKey, iavlKey, acck, bank, prmk)
	prmk.Register(vm.ModuleName, vmk)
	vmk.SetParams(ctx, vm.DefaultParams())
	vmk.Initialize(log.NewNoopLogger(), mcw)

	stdlibCtx := vmk.MakeGnoTransactionStore(ctx.WithMultiStore(mcw))
	vmk.LoadStdlib(stdlibCtx, stdlibsDir)
	vmk.CommitGnoTransactionStore(stdlibCtx)

	mcw.MultiWrite()

	return ctx, acck, bank, vmk, stdlibCtx
}
