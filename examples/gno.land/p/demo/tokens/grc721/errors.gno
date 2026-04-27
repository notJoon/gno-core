package grc721

import "errors"

var (
	ErrInvalidTokenId             = errors.New("invalid token id")
	ErrInvalidAddress             = errors.New("invalid address")
	ErrTokenIdNotHasApproved      = errors.New("token id not approved for anyone")
	ErrApprovalToCurrentOwner     = errors.New("approval to current owner")
	ErrCallerIsNotOwner           = errors.New("caller is not token owner")
	ErrCannotTransferToSelf       = errors.New("cannot send transfer to self")
	ErrTransferFromIncorrectOwner = errors.New("transfer from incorrect owner")
	ErrCallerIsNotOwnerOrApproved = errors.New("caller is not token owner or approved")
	ErrTokenIdAlreadyExists       = errors.New("token id already exists")
	ErrReadonly                   = errors.New("teller is readonly")
)
