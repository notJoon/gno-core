package grc721

import (
	"testing"

	"gno.land/p/nt/testutils/v0"
	"gno.land/p/nt/uassert/v0"
)

const (
	newNFTName   = "NewNFT"
	newNFTSymbol = "NEW"
)

func TestNewNFT(t *testing.T) {
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	uassert.True(t, nft != nil, "nft should not be nil")
	uassert.True(t, ledger != nil, "ledger should not be nil")
	uassert.True(t, ledger.nft == nft, "ledger should reference back to nft")
}

func TestNFT_Metadata(t *testing.T) {
	nft, _ := NewNFT(newNFTName, newNFTSymbol)
	uassert.Equal(t, newNFTName, nft.Name())
	uassert.Equal(t, newNFTSymbol, nft.Symbol())
}

func TestNFT_TokenCount_Empty(t *testing.T) {
	nft, _ := NewNFT(newNFTName, newNFTSymbol)
	uassert.Equal(t, int64(0), nft.TokenCount())
	uassert.Equal(t, int64(0), nft.TotalSupply())
}

func TestNFT_TokenCount_AfterMint(t *testing.T) {
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	alice := testutils.TestAddress("alice")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))
	uassert.NoError(t, ledger.Mint(alice, TokenID("2")))

	uassert.Equal(t, int64(2), nft.TokenCount())
	uassert.Equal(t, int64(2), nft.TotalSupply())
}

func TestNFT_BalanceOf(t *testing.T) {
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	alice := testutils.TestAddress("alice")
	bob := testutils.TestAddress("bob")

	bal, err := nft.BalanceOf(alice)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(0), bal)

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))
	uassert.NoError(t, ledger.Mint(alice, TokenID("2")))
	uassert.NoError(t, ledger.Mint(bob, TokenID("3")))

	balAlice, err := nft.BalanceOf(alice)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(2), balAlice)

	balBob, err := nft.BalanceOf(bob)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(1), balBob)
}

func TestNFT_BalanceOf_InvalidAddress(t *testing.T) {
	nft, _ := NewNFT(newNFTName, newNFTSymbol)
	_, err := nft.BalanceOf(address(""))
	uassert.ErrorIs(t, err, ErrInvalidAddress)
}

func TestNFT_OwnerOf(t *testing.T) {
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	alice := testutils.TestAddress("alice")

	_, err := nft.OwnerOf(TokenID("missing"))
	uassert.ErrorIs(t, err, ErrInvalidTokenId)

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))
	owner, err := nft.OwnerOf(TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, alice.String(), owner.String())
}

func TestNFT_TokenURI(t *testing.T) {
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	alice := testutils.TestAddress("alice")
	uri := "ipfs://example/1"

	_, err := nft.TokenURI(TokenID("1"))
	uassert.ErrorIs(t, err, ErrInvalidTokenId)

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))
	uassert.NoError(t, ledger.SetTokenURI(TokenID("1"), TokenURI(uri)))

	got, err := nft.TokenURI(TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, uri, got)
}

func TestNFT_GetApproved(t *testing.T) {
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	owner := testutils.TestAddress("owner")
	approved := testutils.TestAddress("approved")

	_, err := nft.GetApproved(TokenID("1"))
	uassert.ErrorIs(t, err, ErrTokenIdNotHasApproved)

	uassert.NoError(t, ledger.Mint(owner, TokenID("1")))
	uassert.NoError(t, ledger.Approve(approved, TokenID("1")))

	got, err := nft.GetApproved(TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, approved.String(), got.String())
}

func TestNFT_IsApprovedForAll(t *testing.T) {
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	owner := testutils.TestAddress("owner")
	op := testutils.TestAddress("operator")

	uassert.False(t, nft.IsApprovedForAll(owner, op))

	uassert.NoError(t, ledger.SetApprovalForAll(owner, op, true))
	uassert.True(t, nft.IsApprovedForAll(owner, op))

	uassert.NoError(t, ledger.SetApprovalForAll(owner, op, false))
	uassert.False(t, nft.IsApprovedForAll(owner, op))
}

func TestNFT_ID(t *testing.T) {
	nft, _ := NewNFT(newNFTName, newNFTSymbol)
	id := nft.ID()
	// origRealm is captured from the test runtime; we only assert the
	// id ends with "."+symbol so the format is non-trivial.
	suffix := "." + newNFTSymbol
	if len(id) < len(suffix) || id[len(id)-len(suffix):] != suffix {
		t.Errorf("ID() = %q; want suffix %q", id, suffix)
	}
}

// PrivateLedger admin-grade tests — no caller resolution, no authorization.

func TestPrivateLedger_Mint_Duplicate(t *testing.T) {
	_, ledger := NewNFT(newNFTName, newNFTSymbol)
	alice := testutils.TestAddress("alice")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))
	err := ledger.Mint(alice, TokenID("1"))
	uassert.ErrorIs(t, err, ErrTokenIdAlreadyExists)
}

func TestPrivateLedger_Mint_InvalidAddress(t *testing.T) {
	_, ledger := NewNFT(newNFTName, newNFTSymbol)
	err := ledger.Mint(address(""), TokenID("1"))
	uassert.ErrorIs(t, err, ErrInvalidAddress)
}

func TestPrivateLedger_Burn(t *testing.T) {
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	alice := testutils.TestAddress("alice")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))
	uassert.NoError(t, ledger.Burn(TokenID("1")))

	_, err := nft.OwnerOf(TokenID("1"))
	uassert.ErrorIs(t, err, ErrInvalidTokenId)
}

func TestPrivateLedger_Burn_Missing(t *testing.T) {
	_, ledger := NewNFT(newNFTName, newNFTSymbol)
	err := ledger.Burn(TokenID("ghost"))
	uassert.ErrorIs(t, err, ErrInvalidTokenId)
}

func TestPrivateLedger_Transfer_Admin(t *testing.T) {
	// Transfer on the ledger requires no caller check; possessing the
	// pointer is the authorization.
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	from := testutils.TestAddress("from")
	to := testutils.TestAddress("to")

	uassert.NoError(t, ledger.Mint(from, TokenID("1")))
	uassert.NoError(t, ledger.Transfer(from, to, TokenID("1")))

	bFrom, err := nft.BalanceOf(from)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(0), bFrom)

	bTo, err := nft.BalanceOf(to)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(1), bTo)

	owner, err := nft.OwnerOf(TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, to.String(), owner.String())
}

func TestPrivateLedger_Transfer_WrongFrom(t *testing.T) {
	_, ledger := NewNFT(newNFTName, newNFTSymbol)
	owner := testutils.TestAddress("owner")
	other := testutils.TestAddress("other")

	uassert.NoError(t, ledger.Mint(owner, TokenID("1")))
	err := ledger.Transfer(other, owner, TokenID("1"))
	uassert.ErrorIs(t, err, ErrTransferFromIncorrectOwner)
}

func TestPrivateLedger_Transfer_SelfRejected(t *testing.T) {
	_, ledger := NewNFT(newNFTName, newNFTSymbol)
	alice := testutils.TestAddress("alice")

	uassert.NoError(t, ledger.Mint(alice, TokenID("1")))
	err := ledger.Transfer(alice, alice, TokenID("1"))
	uassert.ErrorIs(t, err, ErrCannotTransferToSelf)
}

func TestPrivateLedger_Approve_Admin(t *testing.T) {
	// Approve on the ledger does not check caller is owner.
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	owner := testutils.TestAddress("owner")
	approved := testutils.TestAddress("approved")

	uassert.NoError(t, ledger.Mint(owner, TokenID("1")))
	uassert.NoError(t, ledger.Approve(approved, TokenID("1")))

	got, err := nft.GetApproved(TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, approved.String(), got.String())
}

func TestPrivateLedger_Approve_ToOwnerRejected(t *testing.T) {
	_, ledger := NewNFT(newNFTName, newNFTSymbol)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, ledger.Mint(owner, TokenID("1")))
	err := ledger.Approve(owner, TokenID("1"))
	uassert.ErrorIs(t, err, ErrApprovalToCurrentOwner)
}

func TestPrivateLedger_SetTokenURI_Admin(t *testing.T) {
	// SetTokenURI on the ledger does not require caller == owner.
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	owner := testutils.TestAddress("owner")
	uri := "ipfs://example/1"

	uassert.NoError(t, ledger.Mint(owner, TokenID("1")))
	uassert.NoError(t, ledger.SetTokenURI(TokenID("1"), TokenURI(uri)))

	got, err := nft.TokenURI(TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, uri, got)
}

func TestPrivateLedger_SetTokenURI_InvalidTokenId(t *testing.T) {
	_, ledger := NewNFT(newNFTName, newNFTSymbol)
	err := ledger.SetTokenURI(TokenID("ghost"), TokenURI("ipfs://x"))
	uassert.ErrorIs(t, err, ErrInvalidTokenId)
}

func TestPrivateLedger_SetApprovalForAll(t *testing.T) {
	nft, ledger := NewNFT(newNFTName, newNFTSymbol)
	owner := testutils.TestAddress("owner")
	op := testutils.TestAddress("operator")

	uassert.NoError(t, ledger.SetApprovalForAll(owner, op, true))
	uassert.True(t, nft.IsApprovedForAll(owner, op))

	uassert.NoError(t, ledger.SetApprovalForAll(owner, op, false))
	uassert.False(t, nft.IsApprovedForAll(owner, op))
}

func TestPrivateLedger_SetApprovalForAll_SelfRejected(t *testing.T) {
	_, ledger := NewNFT(newNFTName, newNFTSymbol)
	owner := testutils.TestAddress("owner")

	err := ledger.SetApprovalForAll(owner, owner, true)
	uassert.ErrorIs(t, err, ErrApprovalToCurrentOwner)
}
