package grc721

import (
	"math"
	"testing"

	"gno.land/p/nt/testutils/v0"
	"gno.land/p/nt/uassert/v0"
)

func newTestRoyaltyNFT(cur realm) (*RoyaltyNFT, *RoyaltyLedger) {
	var nft *RoyaltyNFT
	var ledger *RoyaltyLedger
	func(cur realm) {
		nft, ledger = NewNFTWithRoyalty(0, cur, dummyNFTName, dummyNFTSymbol)
	}(cross(cur))
	return nft, ledger
}

func TestRoyaltyLifecycleAndCalculation(cur realm, t *testing.T) {
	nft, ledger := newTestRoyaltyNFT(cur)
	owner := testutils.TestAddress("owner")
	payee := testutils.TestAddress("payee")

	uassert.NoError(t, ledger.Mint(owner, "1"))
	receiver, amount, err := nft.RoyaltyInfo("1", 1000)
	uassert.NoError(t, err)
	uassert.Equal(t, address(""), receiver)
	uassert.Equal(t, int64(0), amount)

	uassert.ErrorIs(t, ledger.SetTokenRoyalty(owner, "1", RoyaltyInfo{BPS: -1}), ErrInvalidRoyaltyBPS)
	uassert.ErrorIs(t, ledger.SetTokenRoyalty(owner, "1", RoyaltyInfo{BPS: 10001}), ErrInvalidRoyaltyBPS)
	uassert.ErrorIs(
		t,
		ledger.SetTokenRoyalty(owner, "1", RoyaltyInfo{BPS: 1}),
		ErrInvalidRoyaltyPaymentAddress,
	)

	uassert.NoError(t, ledger.SetTokenRoyalty(owner, "1", RoyaltyInfo{
		PaymentAddress: payee,
		BPS:            1,
	}))
	receiver, amount, err = nft.RoyaltyInfo("1", math.MaxInt64)
	uassert.NoError(t, err)
	uassert.Equal(t, payee, receiver)
	uassert.Equal(t, int64(922337203685477), amount)

	uassert.NoError(t, ledger.SetTokenRoyalty(owner, "1", RoyaltyInfo{
		PaymentAddress: payee,
		BPS:            10000,
	}))
	_, amount, err = nft.RoyaltyInfo("1", math.MaxInt64)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(math.MaxInt64), amount)
	_, _, err = nft.RoyaltyInfo("1", -1)
	uassert.ErrorIs(t, err, ErrInvalidSalePrice)

	uassert.NoError(t, ledger.SetTokenRoyalty(owner, "1", RoyaltyInfo{}))
	receiver, amount, err = nft.RoyaltyInfo("1", 1000)
	uassert.NoError(t, err)
	uassert.Equal(t, address(""), receiver)
	uassert.Equal(t, int64(0), amount)

	uassert.NoError(t, ledger.Burn("1"))
	_, _, err = nft.RoyaltyInfo("1", 1000)
	uassert.ErrorIs(t, err, ErrTokenNotFound)
}
