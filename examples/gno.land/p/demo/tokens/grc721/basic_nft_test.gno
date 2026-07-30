package grc721

import (
	"strings"
	"testing"

	"gno.land/p/nt/testutils/v0"
	"gno.land/p/nt/uassert/v0"
)

const (
	dummyNFTName   = "DummyNFT"
	dummyNFTSymbol = "DNFT"
)

func newTestNFT(cur realm) (*NFT, *PrivateLedger) {
	var nft *NFT
	var ledger *PrivateLedger
	func(cur realm) {
		nft, ledger = NewBasicNFT(dummyNFTName, dummyNFTSymbol, cur)
	}(cross(cur))
	return nft, ledger
}

func TestNewBasicNFTValidation(cur realm, t *testing.T) {
	uassert.PanicsContains(t, cur, "method selector on nil interface", func() {
		NewBasicNFT("Name", "OK", nil)
	})
	uassert.AbortsContains(t, cur, "rlm does not match the current crossing frame", func(cur realm) {
		NewBasicNFT("Name", "OK", cur.Previous())
	})
	uassert.AbortsContains(t, cur, "invalid token name", func(cur realm) {
		NewBasicNFT("", "OK", cur)
	})
	uassert.AbortsContains(t, cur, "invalid token symbol", func(cur realm) {
		NewBasicNFT("Name", "BAD.SYMBOL", cur)
	})
	uassert.NotAborts(t, cur, func(cur realm) {
		NewBasicNFT(strings.Repeat("a", MaxNameLen), strings.Repeat("A", MaxSymbolLen), cur)
	})
}

func TestTokenIDValidation(cur realm, t *testing.T) {
	_, ledger := newTestNFT(cur)
	owner := testutils.TestAddress("owner")

	for _, tid := range []TokenID{
		"",
		TokenID(strings.Repeat("a", MaxTokenIDLen+1)),
		"with space",
		"with.dot",
		"with/slash",
		"with:colon",
		"control\x01",
		"한글",
	} {
		uassert.ErrorIs(t, ledger.Mint(owner, tid), ErrInvalidTokenID)
	}

	uassert.NoError(t, ledger.Mint(owner, TokenID(strings.Repeat("a", MaxTokenIDLen))))
	for _, tid := range []TokenID{"A", "a", "0", "-", "_", "Aa0-_"} {
		uassert.NoError(t, ledger.Mint(owner, tid))
	}
}

func TestMintBurnLifecycle(cur realm, t *testing.T) {
	nft, ledger := newTestNFT(cur)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, ledger.Mint(owner, "1"))
	uassert.ErrorIs(t, ledger.Mint(owner, "1"), ErrTokenIDAlreadyUsed)
	uassert.Equal(t, int64(1), nft.TokenCount())
	balance, err := nft.BalanceOf(owner)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(1), balance)

	uassert.NoError(t, ledger.Burn("1"))
	uassert.Equal(t, int64(0), nft.TokenCount())
	balance, err = nft.BalanceOf(owner)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(0), balance)
	_, err = nft.OwnerOf("1")
	uassert.ErrorIs(t, err, ErrTokenNotFound)
	uassert.ErrorIs(t, ledger.Mint(owner, "1"), ErrTokenIDAlreadyUsed)
}

func TestApprovalAndTransfer(cur realm, t *testing.T) {
	nft, ledger := newTestNFT(cur)
	owner := testutils.TestAddress("owner")
	approved := testutils.TestAddress("approved")
	operator := testutils.TestAddress("operator")
	receiver := testutils.TestAddress("receiver")

	uassert.NoError(t, ledger.Mint(owner, "1"))
	uassert.NoError(t, ledger.Mint(owner, "2"))
	uassert.NoError(t, ledger.Mint(owner, "3"))
	uassert.ErrorIs(t, ledger.Approve(owner, owner, "1"), ErrApprovalToCurrentOwner)

	addr, err := nft.GetApproved("1")
	uassert.NoError(t, err)
	uassert.Equal(t, address(""), addr)

	uassert.NoError(t, ledger.Approve(owner, approved, "1"))
	uassert.NoError(t, ledger.TransferFrom(approved, owner, receiver, "1"))
	addr, err = nft.GetApproved("1")
	uassert.NoError(t, err)
	uassert.Equal(t, address(""), addr)

	uassert.NoError(t, ledger.SetApprovalForAll(owner, operator, true))
	uassert.NoError(t, ledger.TransferFrom(operator, owner, receiver, "2"))
	uassert.True(t, nft.IsApprovedForAll(owner, operator))
	uassert.NoError(t, ledger.TransferFrom(owner, owner, receiver, "3"))
	uassert.Equal(t, int64(3), nft.TokenCount())

	uassert.NoError(t, ledger.Approve(receiver, approved, "2"))
	uassert.NoError(t, ledger.Approve(receiver, "", "2"))
	addr, err = nft.GetApproved("2")
	uassert.NoError(t, err)
	uassert.Equal(t, address(""), addr)

	uassert.NoError(t, ledger.Approve(receiver, approved, "3"))
	uassert.NoError(t, ledger.SetApprovalForAll(receiver, operator, true))
	uassert.NoError(t, ledger.Burn("3"))
	_, err = nft.GetApproved("3")
	uassert.ErrorIs(t, err, ErrTokenNotFound)
	uassert.True(t, nft.IsApprovedForAll(receiver, operator))
	ownerBalance, err := nft.BalanceOf(owner)
	uassert.NoError(t, err)
	receiverBalance, err := nft.BalanceOf(receiver)
	uassert.NoError(t, err)
	uassert.Equal(t, nft.TokenCount(), ownerBalance+receiverBalance)

	uassert.ErrorIs(t, ledger.SetApprovalForAll(owner, owner, true), ErrSelfApproval)
	uassert.ErrorIs(t, ledger.TransferFrom(receiver, receiver, receiver, "1"), ErrCannotTransferToSelf)
}

func TestFailedTransferKeepsState(cur realm, t *testing.T) {
	nft, ledger := newTestNFT(cur)
	owner := testutils.TestAddress("owner")
	other := testutils.TestAddress("other")
	receiver := testutils.TestAddress("receiver")

	uassert.NoError(t, ledger.Mint(owner, "1"))
	uassert.NoError(t, ledger.Approve(owner, other, "1"))
	uassert.ErrorIs(
		t,
		ledger.TransferFrom(owner, other, receiver, "1"),
		ErrTransferFromIncorrectOwner,
	)
	uassert.ErrorIs(
		t,
		ledger.TransferFrom(receiver, owner, receiver, "1"),
		ErrCallerIsNotOwnerOrApproved,
	)

	actualOwner, err := nft.OwnerOf("1")
	uassert.NoError(t, err)
	uassert.Equal(t, owner, actualOwner)
	uassert.Equal(t, int64(1), nft.TokenCount())
	ownerBalance, err := nft.BalanceOf(owner)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(1), ownerBalance)
	receiverBalance, err := nft.BalanceOf(receiver)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(0), receiverBalance)
	approved, err := nft.GetApproved("1")
	uassert.NoError(t, err)
	uassert.Equal(t, other, approved)

	uassert.ErrorIs(t, ledger.Mint("", "unused"), ErrInvalidAddress)
	uassert.NoError(t, ledger.Mint(owner, "unused"))
}

func TestTokenURI(cur realm, t *testing.T) {
	nft, ledger := newTestNFT(cur)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, ledger.Mint(owner, "1"))
	uri, err := nft.TokenURI("1")
	uassert.NoError(t, err)
	uassert.Equal(t, "", uri)

	uassert.NoError(t, ledger.SetTokenURI("1", "ipfs://one"))
	uri, err = nft.TokenURI("1")
	uassert.NoError(t, err)
	uassert.Equal(t, "ipfs://one", uri)

	uassert.NoError(t, ledger.Burn("1"))
	_, err = nft.TokenURI("1")
	uassert.ErrorIs(t, err, ErrTokenNotFound)
}
