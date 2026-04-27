package metadata

import (
	"testing"

	"gno.land/p/demo/tokens/grc721"
	"gno.land/p/nt/testutils/v0"
	"gno.land/p/nt/uassert/v0"
)

const (
	collectionName   = "MetaNFT"
	collectionSymbol = "MNFT"
)

func sampleMetadata() Metadata {
	return Metadata{
		Image:       "ipfs://example/1.png",
		Name:        "Sample #1",
		Description: "test fixture",
		Attributes: []Trait{
			{TraitType: "level", Value: "1"},
		},
	}
}

func TestNewMetadataNFT(t *testing.T) {
	nft, coreLedger, mLedger := NewMetadataNFT(collectionName, collectionSymbol)
	uassert.True(t, nft != nil, "nft should not be nil")
	uassert.True(t, coreLedger != nil, "core ledger should not be nil")
	uassert.True(t, mLedger != nil, "metadata ledger should not be nil")
}

func TestPrivateLedger_SetTokenMetadata_Admin(t *testing.T) {
	nft, coreLedger, mLedger := NewMetadataNFT(collectionName, collectionSymbol)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, coreLedger.Mint(owner, grc721.TokenID("1")))
	uassert.NoError(t, mLedger.SetTokenMetadata(grc721.TokenID("1"), sampleMetadata()))

	got, err := nft.TokenMetadata(grc721.TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, "ipfs://example/1.png", got.Image)
	uassert.Equal(t, "Sample #1", got.Name)
}

func TestPrivateLedger_SetTokenMetadata_MissingToken(t *testing.T) {
	_, _, mLedger := NewMetadataNFT(collectionName, collectionSymbol)
	err := mLedger.SetTokenMetadata(grc721.TokenID("ghost"), sampleMetadata())
	uassert.ErrorIs(t, err, grc721.ErrInvalidTokenId)
}

func TestTokenMetadata_NotSet(t *testing.T) {
	nft, coreLedger, _ := NewMetadataNFT(collectionName, collectionSymbol)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, coreLedger.Mint(owner, grc721.TokenID("1")))

	_, err := nft.TokenMetadata(grc721.TokenID("1"))
	uassert.ErrorIs(t, err, ErrMetadataNotSet)
}

func TestTokenMetadata_MissingToken(t *testing.T) {
	nft, _, _ := NewMetadataNFT(collectionName, collectionSymbol)
	_, err := nft.TokenMetadata(grc721.TokenID("ghost"))
	uassert.ErrorIs(t, err, grc721.ErrInvalidTokenId)
}

func TestCallerTeller_SetTokenMetadata(t *testing.T) {
	nft, coreLedger, _ := NewMetadataNFT(collectionName, collectionSymbol)
	alice := testutils.TestAddress("alice")

	uassert.NoError(t, coreLedger.Mint(alice, grc721.TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(alice))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		uassert.NoError(t, teller.SetTokenMetadata(grc721.TokenID("1"), sampleMetadata()))
	}()

	got, err := nft.TokenMetadata(grc721.TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, "Sample #1", got.Name)
}

func TestCallerTeller_SetTokenMetadata_NotOwner(t *testing.T) {
	nft, coreLedger, _ := NewMetadataNFT(collectionName, collectionSymbol)
	alice := testutils.TestAddress("alice")
	eve := testutils.TestAddress("eve")

	uassert.NoError(t, coreLedger.Mint(alice, grc721.TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(eve))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		err := teller.SetTokenMetadata(grc721.TokenID("1"), sampleMetadata())
		uassert.ErrorIs(t, err, grc721.ErrCallerIsNotOwner)
	}()
}

func TestReadonlyTeller_SetTokenMetadata(t *testing.T) {
	nft, coreLedger, _ := NewMetadataNFT(collectionName, collectionSymbol)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, coreLedger.Mint(owner, grc721.TokenID("1")))

	teller := nft.ReadonlyTeller()
	err := teller.SetTokenMetadata(grc721.TokenID("1"), sampleMetadata())
	uassert.ErrorIs(t, err, grc721.ErrReadonly)
}

func TestImpersonateTeller_SetTokenMetadata(t *testing.T) {
	nft, coreLedger, mLedger := NewMetadataNFT(collectionName, collectionSymbol)
	alice := testutils.TestAddress("alice")

	uassert.NoError(t, coreLedger.Mint(alice, grc721.TokenID("1")))

	imp := mLedger.ImpersonateTeller(alice)
	uassert.NoError(t, imp.SetTokenMetadata(grc721.TokenID("1"), sampleMetadata()))

	got, err := nft.TokenMetadata(grc721.TokenID("1"))
	uassert.NoError(t, err)
	uassert.Equal(t, "Sample #1", got.Name)
}

func TestTeller_EmbeddedCoreMethods(t *testing.T) {
	nft, coreLedger, _ := NewMetadataNFT(collectionName, collectionSymbol)
	alice := testutils.TestAddress("alice")
	bob := testutils.TestAddress("bob")

	uassert.NoError(t, coreLedger.Mint(alice, grc721.TokenID("1")))

	teller := nft.CallerTeller()
	uassert.Equal(t, collectionName, teller.Name())
	uassert.Equal(t, collectionSymbol, teller.Symbol())

	testing.SetRealm(testing.NewUserRealm(alice))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		uassert.NoError(t, teller.TransferFrom(alice, bob, grc721.TokenID("1")))
	}()

	owner, _ := nft.OwnerOf(grc721.TokenID("1"))
	uassert.Equal(t, bob.String(), owner.String())
}
