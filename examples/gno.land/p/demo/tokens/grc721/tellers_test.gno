package grc721

import (
	"testing"

	"gno.land/p/nt/testutils/v0"
	"gno.land/p/nt/uassert/v0"
)

const (
	tellerNFTName   = "TellerNFT"
	tellerNFTSymbol = "TLR"
)

func TestCallerTeller_TransferFrom(t *testing.T) {
	nft, ledger := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")
	bob := testutils.TestAddress("bob")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(alice))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		uassert.NoError(t, teller.TransferFrom(alice, bob, TokenID("1")))
	}()

	owner, err := nft.OwnerOf(TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, bob.String(), owner.String())
}

func TestCallerTeller_TransferSugar(t *testing.T) {
	nft, ledger := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")
	bob := testutils.TestAddress("bob")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(alice))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		uassert.NoError(t, teller.Transfer(bob, TokenID("1")))
	}()

	owner, _ := nft.OwnerOf(TokenID("1"))
	uassert.Equal(t, bob.String(), owner.String())
}

func TestCallerTeller_Approve(t *testing.T) {
	nft, ledger := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")
	bob := testutils.TestAddress("bob")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(alice))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		uassert.NoError(t, teller.Approve(bob, TokenID("1")))
	}()

	got, err := nft.GetApproved(TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, bob.String(), got.String())
}

func TestCallerTeller_Approve_NotOwner(t *testing.T) {
	nft, ledger := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")
	eve := testutils.TestAddress("eve")
	bob := testutils.TestAddress("bob")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(eve))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		err := teller.Approve(bob, TokenID("1"))
		uassert.ErrorIs(t, err, ErrCallerIsNotOwnerOrApproved)
	}()
}

func TestCallerTeller_TransferFrom_NotApproved(t *testing.T) {
	nft, ledger := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")
	bob := testutils.TestAddress("bob")
	eve := testutils.TestAddress("eve")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(eve))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		err := teller.TransferFrom(alice, bob, TokenID("1"))
		uassert.ErrorIs(t, err, ErrCallerIsNotOwnerOrApproved)
	}()
}

func TestCallerTeller_TransferFrom_ViaApprovalForAll(t *testing.T) {
	nft, ledger := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")
	op := testutils.TestAddress("operator")
	bob := testutils.TestAddress("bob")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))
	uassert.NoError(t, ledger.SetApprovalForAll(alice, op, true))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(op))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		uassert.NoError(t, teller.TransferFrom(alice, bob, TokenID("1")))
	}()

	owner, _ := nft.OwnerOf(TokenID("1"))
	uassert.Equal(t, bob.String(), owner.String())
}

func TestCallerTeller_SetApprovalForAll(t *testing.T) {
	nft, _ := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")
	op := testutils.TestAddress("operator")

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(alice))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		uassert.NoError(t, teller.SetApprovalForAll(op, true))
	}()

	uassert.True(t, nft.IsApprovedForAll(alice, op))
}

func TestCallerTeller_SetTokenURI(t *testing.T) {
	nft, ledger := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")
	uri := "ipfs://example/1"

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(alice))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		ok, err := teller.SetTokenURI(TokenID("1"), TokenURI(uri))
		uassert.NoError(t, err)
		uassert.True(t, ok)
	}()

	got, err := nft.TokenURI(TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, uri, got)
}

func TestCallerTeller_SetTokenURI_NotOwner(t *testing.T) {
	nft, ledger := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")
	eve := testutils.TestAddress("eve")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(eve))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		_, err := teller.SetTokenURI(TokenID("1"), TokenURI("ipfs://x"))
		uassert.ErrorIs(t, err, ErrCallerIsNotOwner)
	}()
}

func TestSafeTransferFrom_AliasesTransferFrom(t *testing.T) {
	nft, ledger := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")
	bob := testutils.TestAddress("bob")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(alice))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		uassert.NoError(t, teller.SafeTransferFrom(alice, bob, TokenID("1")))
	}()

	owner, _ := nft.OwnerOf(TokenID("1"))
	uassert.Equal(t, bob.String(), owner.String())
}

func TestReadonlyTeller_WritesReturnError(t *testing.T) {
	nft, _ := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")

	teller := nft.ReadonlyTeller()

	uassert.ErrorIs(t, teller.Transfer(alice, TokenID("1")), ErrReadonly)
	uassert.ErrorIs(t, teller.TransferFrom(alice, alice, TokenID("1")), ErrReadonly)
	uassert.ErrorIs(t, teller.SafeTransferFrom(alice, alice, TokenID("1")), ErrReadonly)
	uassert.ErrorIs(t, teller.Approve(alice, TokenID("1")), ErrReadonly)
	uassert.ErrorIs(t, teller.SetApprovalForAll(alice, true), ErrReadonly)

	_, err := teller.SetTokenURI(TokenID("1"), TokenURI("ipfs://x"))
	uassert.ErrorIs(t, err, ErrReadonly)
}

func TestReadonlyTeller_ReadsPassThrough(t *testing.T) {
	nft, ledger := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))

	teller := nft.ReadonlyTeller()

	uassert.Equal(t, tellerNFTName, teller.Name())
	uassert.Equal(t, tellerNFTSymbol, teller.Symbol())

	bal, err := teller.BalanceOf(alice)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(1), bal)

	owner, err := teller.OwnerOf(TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, alice.String(), owner.String())
}

func TestImpersonateTeller_ActsAsAddress(t *testing.T) {
	nft, ledger := NewNFT(tellerNFTName, tellerNFTSymbol)
	alice := testutils.TestAddress("alice")
	bob := testutils.TestAddress("bob")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))

	imp := ledger.ImpersonateTeller(alice)

	// No realm setup needed: accountFn returns alice unconditionally.
	uassert.NoError(t, imp.Transfer(bob, TokenID("1")))

	owner, _ := nft.OwnerOf(TokenID("1"))
	uassert.Equal(t, bob.String(), owner.String())
}

func TestRealmTeller_Smoke(t *testing.T) {
	nft, _ := NewNFT(tellerNFTName, tellerNFTSymbol)

	testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
	teller := nft.RealmTeller()
	uassert.True(t, teller != nil, "teller should not be nil")
}

func TestRealmSubTeller_Smoke(t *testing.T) {
	nft, _ := NewNFT(tellerNFTName, tellerNFTSymbol)

	testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
	teller := nft.RealmSubTeller("vault")
	uassert.True(t, teller != nil, "teller should not be nil")
}

func TestAccountSlugAddr_EmptySlug(t *testing.T) {
	addr := testutils.TestAddress("base")
	uassert.Equal(t, addr.String(), accountSlugAddr(addr, "").String())
}

func TestAccountSlugAddr_NonEmptySlug(t *testing.T) {
	addr := testutils.TestAddress("base")
	derived := accountSlugAddr(addr, "vault")
	uassert.True(t, derived.String() != addr.String(), "derived address should differ from base")
}
