package grc721

import (
	"chain"

	"gno.land/p/nt/avl/v0"
)

// MetadataNFT is a public NFT view with on-chain metadata reads.
type MetadataNFT struct {
	*NFT
	metadata *avl.Tree
}

// MetadataLedger is the collection realm's metadata write capability.
type MetadataLedger struct {
	*PrivateLedger
	nft *MetadataNFT
}

var _ IGRC721MetadataOnchain = (*MetadataNFT)(nil)

func NewNFTWithMetadata(_ int, rlm realm, name, symbol string) (*MetadataNFT, *MetadataLedger) {
	nft, ledger := NewBasicNFT(0, rlm, name, symbol)
	public := &MetadataNFT{
		NFT:      nft,
		metadata: avl.NewTree(),
	}
	return public, &MetadataLedger{
		PrivateLedger: ledger,
		nft:           public,
	}
}

func (n *MetadataNFT) TokenMetadata(tid TokenID) (Metadata, error) {
	if _, err := n.ledger.activeToken(tid); err != nil {
		return Metadata{}, err
	}
	value := n.metadata.Get(tid.String())
	if value == nil {
		return Metadata{}, ErrMetadataNotSet
	}
	return cloneMetadata(value.(Metadata)), nil
}

func (l *MetadataLedger) SetTokenMetadata(tid TokenID, metadata Metadata) error {
	if _, err := l.activeToken(tid); err != nil {
		return err
	}

	l.nft.metadata.Set(tid.String(), cloneMetadata(metadata))
	chain.Emit(
		MetadataUpdateEvent,
		"token", l.nft.ID(),
		"tokenId", tid.String(),
	)
	return nil
}

func cloneMetadata(metadata Metadata) Metadata {
	if metadata.Attributes == nil {
		return metadata
	}
	attributes := make([]Trait, len(metadata.Attributes))
	copy(attributes, metadata.Attributes)
	metadata.Attributes = attributes
	return metadata
}
