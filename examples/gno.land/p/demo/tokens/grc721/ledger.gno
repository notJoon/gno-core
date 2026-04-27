package grc721

import (
	"chain"
	"math/overflow"
	"strconv"
)

// Mint creates token tid owned by to.
//
// Validity invariants:
//   - to is a valid address
//   - tid does not already exist
func (l *PrivateLedger) Mint(to address, tid TokenID) error {
	if err := isValidAddress(to); err != nil {
		return err
	}
	if l.exists(tid) {
		return ErrTokenIdAlreadyExists
	}

	bal, err := l.balanceOf(to)
	if err != nil {
		return err
	}
	bal = overflow.Add64p(bal, 1)

	toStr := to.String()
	tidStr := tid.String()
	l.balances.Set(toStr, bal)
	l.owners.Set(tidStr, to)

	chain.Emit(
		MintEvent,
		"slug", l.nft.symbol,
		"to", toStr,
		"tokenId", tidStr,
	)
	return nil
}

// SafeMint currently aliases Mint: there is no registry-based receiver
// check yet (see https://github.com/gnolang/gno/issues/5546).
func (l *PrivateLedger) SafeMint(to address, tid TokenID) error {
	return l.Mint(to, tid)
}

// Burn destroys token tid.
//
// Validity invariants:
//   - tid exists
func (l *PrivateLedger) Burn(tid TokenID) error {
	owner, err := l.nft.OwnerOf(tid)
	if err != nil {
		return err
	}

	tidStr := tid.String()
	l.tokenApprovals.Remove(tidStr)

	bal, err := l.balanceOf(owner)
	if err != nil {
		return err
	}
	bal = overflow.Sub64p(bal, 1)

	ownerStr := owner.String()
	if bal == 0 {
		l.balances.Remove(ownerStr)
	} else {
		l.balances.Set(ownerStr, bal)
	}
	l.owners.Remove(tidStr)

	chain.Emit(
		BurnEvent,
		"slug", l.nft.symbol,
		"from", ownerStr,
		"tokenId", tidStr,
	)
	return nil
}

// Transfer moves token tid from `from` to `to` without authorization
// checks. Possessing *PrivateLedger IS the authorization; caller-resolved
// transfers belong on the Teller layer.
//
// Validity invariants:
//   - from and to are valid addresses
//   - from == OwnerOf(tid)
//   - from != to
func (l *PrivateLedger) Transfer(from, to address, tid TokenID) error {
	if err := isValidAddress(from); err != nil {
		return err
	}
	if err := isValidAddress(to); err != nil {
		return err
	}
	if from == to {
		return ErrCannotTransferToSelf
	}

	owner, err := l.nft.OwnerOf(tid)
	if err != nil {
		return err
	}
	if owner != from {
		return ErrTransferFromIncorrectOwner
	}

	tidStr := tid.String()
	l.tokenApprovals.Remove(tidStr)

	fromBal, err := l.balanceOf(from)
	if err != nil {
		return err
	}
	toBal, err := l.balanceOf(to)
	if err != nil {
		return err
	}
	fromBal = overflow.Sub64p(fromBal, 1)
	toBal = overflow.Add64p(toBal, 1)

	fromStr := from.String()
	toStr := to.String()
	if fromBal == 0 {
		l.balances.Remove(fromStr)
	} else {
		l.balances.Set(fromStr, fromBal)
	}
	l.balances.Set(toStr, toBal)
	l.owners.Set(tidStr, to)

	chain.Emit(
		TransferEvent,
		"slug", l.nft.symbol,
		"from", fromStr,
		"to", toStr,
		"tokenId", tidStr,
	)
	return nil
}

// Approve sets `to` as the approved address for tid. The owner is derived
// from OwnerOf(tid); for NFTs no separate owner argument is needed.
//
// Validity invariants:
//   - tid exists
//   - to is a valid address
//   - to != OwnerOf(tid)
func (l *PrivateLedger) Approve(to address, tid TokenID) error {
	if err := isValidAddress(to); err != nil {
		return err
	}
	owner, err := l.nft.OwnerOf(tid)
	if err != nil {
		return err
	}
	if owner == to {
		return ErrApprovalToCurrentOwner
	}

	tidStr := tid.String()
	l.tokenApprovals.Set(tidStr, to)

	chain.Emit(
		ApprovalEvent,
		"slug", l.nft.symbol,
		"owner", owner.String(),
		"to", to.String(),
		"tokenId", tidStr,
	)
	return nil
}

// SetApprovalForAll grants or revokes op as operator over all of owner's
// tokens.
//
// Validity invariants:
//   - owner and op are valid addresses
//   - owner != op
func (l *PrivateLedger) SetApprovalForAll(owner, op address, approved bool) error {
	if err := isValidAddress(owner); err != nil {
		return err
	}
	if err := isValidAddress(op); err != nil {
		return err
	}
	if owner == op {
		return ErrSelfApproval
	}

	ownerStr := owner.String()
	opStr := op.String()
	l.operatorApprovals.Set(ownerStr+":"+opStr, approved)

	chain.Emit(
		ApprovalForAllEvent,
		"slug", l.nft.symbol,
		"owner", ownerStr,
		"to", opStr,
		"approved", strconv.FormatBool(approved),
	)
	return nil
}

// SetTokenURI sets the URI of tid without authorization check.
//
// Validity invariants:
//   - tid exists
func (l *PrivateLedger) SetTokenURI(tid TokenID, uri TokenURI) error {
	if !l.exists(tid) {
		return ErrInvalidTokenId
	}
	l.tokenURIs.Set(tid.String(), uri.String())
	return nil
}

func (l *PrivateLedger) exists(tid TokenID) bool {
	_, found := l.owners.Get(tid.String())
	return found
}

func (l *PrivateLedger) balanceOf(addr address) (int64, error) {
	if err := isValidAddress(addr); err != nil {
		return 0, err
	}
	v, found := l.balances.Get(addr.String())
	if !found {
		return 0, nil
	}
	return v.(int64), nil
}
