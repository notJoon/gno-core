package grc721

type IGRC2981 interface {
	RoyaltyInfo(tid TokenID, salePrice int64) (address, int64, error)
}

type RoyaltyInfo struct {
	PaymentAddress address
	BPS            int64
}
