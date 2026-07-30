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

type mintMutator interface {
	Mint(to address, tid TokenID) error
}

type burnMutator interface {
	Burn(tid TokenID) error
}

type approveMutator interface {
	Approve(caller, to address, tid TokenID) error
}

type approvalForAllMutator interface {
	SetApprovalForAll(caller, operator address, approved bool) error
}

type transferMutator interface {
	TransferFrom(caller, from, to address, tid TokenID) error
}

type tokenURIMutator interface {
	SetTokenURI(tid TokenID, uri TokenURI) error
}

type metadataMutator interface {
	SetTokenMetadata(tid TokenID, metadata Metadata) error
}

type royaltyMutator interface {
	SetTokenRoyalty(tid TokenID, royalty RoyaltyInfo) error
}

func hasMutator(reader IGRC721Reader) bool {
	_, mint := reader.(mintMutator)
	_, burn := reader.(burnMutator)
	_, approve := reader.(approveMutator)
	_, approvalForAll := reader.(approvalForAllMutator)
	_, transfer := reader.(transferMutator)
	_, tokenURI := reader.(tokenURIMutator)
	_, metadata := reader.(metadataMutator)
	_, royalty := reader.(royaltyMutator)
	return mint || burn || approve || approvalForAll || transfer || tokenURI || metadata || royalty
}

func TestBurnedTokenRejectsMetadata(cur realm, t *testing.T) {
	nft, ledger := newTestMetadataNFT(cur)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, ledger.Mint(owner, TokenID("1")))
	uassert.NoError(t, ledger.SetTokenMetadata(TokenID("1"), Metadata{Name: "gone"}))
	uassert.NoError(t, ledger.Burn(TokenID("1")))

	_, err := nft.TokenMetadata(TokenID("1"))
	uassert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestBurnedTokenRejectsRoyalty(cur realm, t *testing.T) {
	nft, ledger := newTestRoyaltyNFT(cur)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, ledger.Mint(owner, TokenID("1")))
	uassert.NoError(t, ledger.SetTokenRoyalty(TokenID("1"), RoyaltyInfo{
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
	uassert.Equal(t, nft.ID(), reader.ID())
	_, hasMetadata := reader.(IGRC721MetadataOnchain)
	_, hasRoyalty := reader.(IGRC2981)

	uassert.True(t, hasMetadata)
	uassert.True(t, hasRoyalty)
	uassert.False(t, hasMutator(reader))
}
