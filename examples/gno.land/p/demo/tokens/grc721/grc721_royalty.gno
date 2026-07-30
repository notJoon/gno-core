package grc721

import "gno.land/p/nt/avl/v0"

const royaltyBPSDenominator int64 = 10000

// RoyaltyNFT is a public metadata NFT view with token royalty reads.
type RoyaltyNFT struct {
	*MetadataNFT
	royalties *avl.Tree
}

// RoyaltyLedger is the collection realm's royalty write capability.
type RoyaltyLedger struct {
	*MetadataLedger
	nft *RoyaltyNFT
}

var _ IGRC2981 = (*RoyaltyNFT)(nil)

func NewNFTWithRoyalty(name, symbol string, rlm realm) (*RoyaltyNFT, *RoyaltyLedger) {
	nft, ledger := NewNFTWithMetadata(name, symbol, rlm)
	public := &RoyaltyNFT{
		MetadataNFT: nft,
		royalties:   avl.NewTree(),
	}
	return public, &RoyaltyLedger{
		MetadataLedger: ledger,
		nft:            public,
	}
}

func (n *RoyaltyNFT) RoyaltyInfo(tid TokenID, salePrice int64) (address, int64, error) {
	if _, err := n.ledger.activeToken(tid); err != nil {
		return "", 0, err
	}
	if salePrice < 0 {
		return "", 0, ErrInvalidSalePrice
	}

	value := n.royalties.Get(tid.String())
	if value == nil {
		return "", 0, nil
	}
	royalty := value.(RoyaltyInfo)
	quotient := salePrice / royaltyBPSDenominator
	remainder := salePrice % royaltyBPSDenominator
	amount := quotient*royalty.BPS + remainder*royalty.BPS/royaltyBPSDenominator
	return royalty.PaymentAddress, amount, nil
}

func (l *RoyaltyLedger) SetTokenRoyalty(tid TokenID, royalty RoyaltyInfo) error {
	if _, err := l.activeToken(tid); err != nil {
		return err
	}
	if royalty.BPS < 0 || royalty.BPS > royaltyBPSDenominator {
		return ErrInvalidRoyaltyBPS
	}
	if royalty.BPS > 0 && !validAddress(royalty.PaymentAddress) {
		return ErrInvalidRoyaltyPaymentAddress
	}
	if royalty.BPS == 0 {
		l.nft.royalties.Remove(tid.String())
	} else {
		l.nft.royalties.Set(tid.String(), royalty)
	}
	return nil
}
