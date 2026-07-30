package grc721

import (
	"testing"

	"gno.land/p/nt/testutils/v0"
	"gno.land/p/nt/uassert/v0"
)

func newTestMetadataNFT(cur realm) (*MetadataNFT, *MetadataLedger) {
	var nft *MetadataNFT
	var ledger *MetadataLedger
	func(cur realm) {
		nft, ledger = NewNFTWithMetadata(0, cur, dummyNFTName, dummyNFTSymbol)
	}(cross(cur))
	return nft, ledger
}

func TestMetadataLifecycleAndCopies(cur realm, t *testing.T) {
	nft, ledger := newTestMetadataNFT(cur)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, ledger.Mint(owner, "1"))
	_, err := nft.TokenMetadata("1")
	uassert.ErrorIs(t, err, ErrMetadataNotSet)

	input := Metadata{
		Name:       "one",
		Attributes: []Trait{{TraitType: "rank", Value: "first"}},
	}
	uassert.NoError(t, ledger.SetTokenMetadata(owner, "1", input))
	input.Attributes[0].Value = "changed input"

	first, err := nft.TokenMetadata("1")
	uassert.NoError(t, err)
	uassert.Equal(t, "first", first.Attributes[0].Value)
	first.Attributes[0].Value = "changed output"

	second, err := nft.TokenMetadata("1")
	uassert.NoError(t, err)
	uassert.Equal(t, "first", second.Attributes[0].Value)

	uassert.NoError(t, ledger.Burn("1"))
	_, err = nft.TokenMetadata("1")
	uassert.ErrorIs(t, err, ErrTokenNotFound)
}
