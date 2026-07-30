package grc721

// IGRC2981 follows the Ethereum royalty standard.
type IGRC2981 interface {
	// RoyaltyInfo returns the royalty receiver and amount for a token and sale price.
	RoyaltyInfo(tid TokenID, salePrice int64) (address, int64, error)
}

// RoyaltyInfo represents royalty information for a token.
type RoyaltyInfo struct {
	PaymentAddress address // PaymentAddress is the address where royalty payment should be sent.
	BPS            int64   // BPS is the royalty rate in basis points, where 10,000 is 100%.
}
