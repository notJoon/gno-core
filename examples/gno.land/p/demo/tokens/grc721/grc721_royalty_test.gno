package grc721

import (
	"testing"

	"gno.land/p/nt/testutils/v0"
	"gno.land/p/nt/uassert/v0"
)

func TestSetTokenRoyalty(t *testing.T) {
	dummy := NewNFTWithRoyalty(dummyNFTName, dummyNFTSymbol)
	uassert.True(t, dummy != nil, "should not be nil")

	addr1 := testutils.TestAddress("alice")
	addr2 := testutils.TestAddress("bob")

	paymentAddress := testutils.TestAddress("john")
	bps := int64(1000) // 10% in basis points

	salePrice := int64(1000)
	expectRoyaltyAmount := int64(100)

	testing.SetOriginCaller(addr1) // addr1

	dummy.mint(addr1, TokenID("1"))

	derr := dummy.SetTokenRoyalty(TokenID("1"), RoyaltyInfo{
		PaymentAddress: paymentAddress,
		Bps:            bps,
	})
	uassert.NoError(t, derr, "Should not result in error")

	// Test case: Invalid token ID
	_ = dummy.SetTokenRoyalty(TokenID("3"), RoyaltyInfo{
		PaymentAddress: paymentAddress,
		Bps:            bps,
	})
	uassert.ErrorIs(t, derr, ErrInvalidTokenId)

	testing.SetOriginCaller(addr2) // addr2

	cerr := dummy.SetTokenRoyalty(TokenID("1"), RoyaltyInfo{
		PaymentAddress: paymentAddress,
		Bps:            bps,
	})
	uassert.ErrorIs(t, cerr, ErrCallerIsNotOwner)

	// Test case: Invalid payment address
	aerr := dummy.SetTokenRoyalty(TokenID("4"), RoyaltyInfo{
		PaymentAddress: address("###"), // invalid address
		Bps:            bps,
	})
	uassert.ErrorIs(t, aerr, ErrInvalidRoyaltyPaymentAddress)

	// Test case: bps above maxRoyaltyBps
	perr := dummy.SetTokenRoyalty(TokenID("5"), RoyaltyInfo{
		PaymentAddress: paymentAddress,
		Bps:            int64(10001), // over maxRoyaltyBps (10000)
	})
	uassert.ErrorIs(t, perr, ErrInvalidRoyaltyBps)

	// Test case: Retrieving Royalty Info
	testing.SetOriginCaller(addr1) // addr1

	dummyPaymentAddress, dummyRoyaltyAmount, rerr := dummy.RoyaltyInfo(TokenID("1"), salePrice)
	uassert.NoError(t, rerr, "RoyaltyInfo error")
	uassert.Equal(t, paymentAddress, dummyPaymentAddress)
	uassert.Equal(t, expectRoyaltyAmount, dummyRoyaltyAmount)
}

func TestRoyaltyBpsCompliance(t *testing.T) {
	owner := testutils.TestAddress("owner")
	payee := testutils.TestAddress("payee")

	nft := NewNFTWithRoyalty(dummyNFTName, dummyNFTSymbol)
	testing.SetOriginCaller(owner)
	nft.mint(owner, TokenID("1"))

	tests := []struct {
		name       string
		bps        int64
		salePrice  int64
		wantAmount int64
	}{
		{"2.5% on 1000", 250, 1000, 25},
		{"0.5% on 1000", 50, 1000, 5},
		{"7.5% on 1000", 750, 1000, 75},
		{"10% on 1000", 1000, 1000, 100},
		{"100% on 1000", 10000, 1000, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := nft.SetTokenRoyalty(TokenID("1"), RoyaltyInfo{
				PaymentAddress: payee,
				Bps:            tt.bps,
			})
			uassert.NoError(t, err, "bps value must be accepted")

			_, got, _ := nft.RoyaltyInfo(TokenID("1"), tt.salePrice)
			uassert.Equal(t, got, tt.wantAmount)
		})
	}
}
