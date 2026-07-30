package grc721

import "errors"

var (
	ErrInvalidTokenID             = errors.New("invalid token id")
	ErrTokenNotFound              = errors.New("token not found")
	ErrTokenIDAlreadyUsed         = errors.New("token id already used")
	ErrInvalidAddress             = errors.New("invalid address")
	ErrCannotTransferToSelf       = errors.New("cannot send transfer to self")
	ErrTransferFromIncorrectOwner = errors.New("transfer from incorrect owner")
	ErrCallerIsNotOwnerOrApproved = errors.New("caller is not token owner or approved")
	ErrApprovalToCurrentOwner     = errors.New("approval to current owner")
	ErrSelfApproval               = errors.New("cannot approve self as operator")

	ErrSpoofedRealm  = errors.New("rlm does not match the current crossing frame")
	ErrNotRealm      = errors.New("rlm must be a realm (got EOA/origin)")
	ErrInvalidName   = errors.New("invalid token name (empty, too long, or contains control chars)")
	ErrInvalidSymbol = errors.New("invalid token symbol (empty, too long, or contains chars outside [A-Za-z0-9_-])")

	ErrMetadataNotSet               = errors.New("metadata not set")
	ErrInvalidSalePrice             = errors.New("invalid sale price")
	ErrInvalidRoyaltyBPS            = errors.New("invalid royalty bps")
	ErrInvalidRoyaltyPaymentAddress = errors.New("invalid royalty payment address")
)
