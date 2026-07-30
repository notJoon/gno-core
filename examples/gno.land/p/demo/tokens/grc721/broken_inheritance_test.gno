package grc721

import (
	"testing"

	"gno.land/p/nt/testutils/v0"
	"gno.land/p/nt/uassert/v0"
)

type testRegistry map[string]IGRC721Reader

func (r testRegistry) register(reader IGRC721Reader) bool {
	if _, exists := r[reader.ID()]; exists {
		return false
	}
	r[reader.ID()] = reader
	return true
}

type testMutator interface {
	Mint(to address, tid TokenID) error
}

func TestBurnedTokenRejectsMetadata(cur realm, t *testing.T) {
	nft, ledger := newTestMetadataNFT(cur)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, ledger.Mint(owner, TokenID("1")))
	uassert.NoError(t, ledger.SetTokenMetadata(owner, TokenID("1"), Metadata{Name: "gone"}))
	uassert.NoError(t, ledger.Burn(TokenID("1")))

	_, err := nft.TokenMetadata(TokenID("1"))
	uassert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestBurnedTokenRejectsRoyalty(cur realm, t *testing.T) {
	nft, ledger := newTestRoyaltyNFT(cur)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, ledger.Mint(owner, TokenID("1")))
	uassert.NoError(t, ledger.SetTokenRoyalty(owner, TokenID("1"), RoyaltyInfo{
		PaymentAddress: testutils.TestAddress("payee"),
		BPS:            1000,
	}))
	uassert.NoError(t, ledger.Burn(TokenID("1")))

	_, _, err := nft.RoyaltyInfo(TokenID("1"), 1000)
	uassert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestBurnedTokenIDCannotBeReused(cur realm, t *testing.T) {
	_, ledger := newTestMetadataNFT(cur)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, ledger.Mint(owner, TokenID("1")))
	uassert.NoError(t, ledger.Burn(TokenID("1")))
	uassert.ErrorIs(t, ledger.Mint(owner, TokenID("1")), ErrTokenIDAlreadyUsed)
}

func TestRegistryRoundTripPreservesCapabilities(cur realm, t *testing.T) {
	nft, _ := newTestRoyaltyNFT(cur)
	registry := testRegistry{}

	uassert.True(t, registry.register(nft))
	uassert.False(t, registry.register(nft), "duplicate collection ID must be rejected")

	reader := registry[nft.ID()]
	_, hasMetadata := reader.(IGRC721MetadataOnchain)
	_, hasRoyalty := reader.(IGRC2981)
	_, hasMutator := reader.(testMutator)

	uassert.True(t, hasMetadata)
	uassert.True(t, hasRoyalty)
	uassert.False(t, hasMutator)
}
