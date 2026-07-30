package grc721

import (
	"chain"
	"math/overflow"
	"strconv"

	"gno.land/p/nt/avl/v0"
	"gno.land/p/nt/ufmt/v0"
)

type tokenRecord struct {
	owner    address
	approved address
	uri      string
	burned   bool
}

// NFT is the public, read-only view of a collection.
type NFT struct {
	name      string
	symbol    string
	origRealm string
	ledger    *PrivateLedger
}

// PrivateLedger is the collection realm's private write capability.
type PrivateLedger struct {
	nft               *NFT
	tokens            avl.Tree // TokenID -> tokenRecord, including burned IDs
	balances          avl.Tree // owner -> active token count
	operatorApprovals avl.Tree // owner:operator -> bool
	activeTokens      int64
}

func NewBasicNFT(_ int, rlm realm, name, symbol string) (*NFT, *PrivateLedger) {
	if !rlm.IsCurrent() {
		panic(ErrSpoofedRealm)
	}
	if rlm.PkgPath() == "" {
		panic(ErrNotRealm)
	}
	if !validName(name) {
		panic(ErrInvalidName)
	}
	if !validSymbol(symbol) {
		panic(ErrInvalidSymbol)
	}

	ledger := &PrivateLedger{}
	nft := &NFT{
		name:      name,
		symbol:    symbol,
		origRealm: rlm.PkgPath(),
		ledger:    ledger,
	}
	ledger.nft = nft
	return nft, ledger
}

func (n *NFT) ID() string        { return n.origRealm + "." + n.symbol }
func (n *NFT) Name() string      { return n.name }
func (n *NFT) Symbol() string    { return n.symbol }
func (n *NFT) TokenCount() int64 { return n.ledger.activeTokens }

func (n *NFT) BalanceOf(owner address) (int64, error) {
	if !validAddress(owner) {
		return 0, ErrInvalidAddress
	}
	value := n.ledger.balances.Get(owner.String())
	if value == nil {
		return 0, nil
	}
	return value.(int64), nil
}

func (n *NFT) OwnerOf(tid TokenID) (address, error) {
	token, err := n.ledger.activeToken(tid)
	if err != nil {
		return "", err
	}
	return token.owner, nil
}

func (n *NFT) GetApproved(tid TokenID) (address, error) {
	token, err := n.ledger.activeToken(tid)
	if err != nil {
		return "", err
	}
	return token.approved, nil
}

func (n *NFT) IsApprovedForAll(owner, operator address) bool {
	if !validAddress(owner) || !validAddress(operator) {
		return false
	}
	return n.ledger.operatorApprovals.Has(approvalKey(owner, operator))
}

func (n *NFT) TokenURI(tid TokenID) (string, error) {
	token, err := n.ledger.activeToken(tid)
	if err != nil {
		return "", err
	}
	return token.uri, nil
}

func (n *NFT) RenderHome() string {
	return ufmt.Sprintf(
		"# %s ($%s)\n\n* **Total supply**: %d\n* **Known accounts**: %d\n",
		n.name,
		n.symbol,
		n.TokenCount(),
		n.ledger.balances.Size(),
	)
}

func (l *PrivateLedger) Mint(to address, tid TokenID) error {
	if !validTokenID(tid) {
		return ErrInvalidTokenID
	}
	if !validAddress(to) {
		return ErrInvalidAddress
	}
	if l.tokens.Has(tid.String()) {
		return ErrTokenIDAlreadyUsed
	}

	balance := overflow.Add64p(l.balanceOf(to), 1)
	tokenCount := overflow.Add64p(l.activeTokens, 1)
	toString := to.String()
	tidString := tid.String()

	l.tokens.Set(tidString, tokenRecord{owner: to})
	l.balances.Set(toString, balance)
	l.activeTokens = tokenCount

	chain.Emit(
		MintEvent,
		"token", l.nft.ID(),
		"to", toString,
		"tokenId", tidString,
	)
	return nil
}

func (l *PrivateLedger) Burn(tid TokenID) error {
	token, err := l.activeToken(tid)
	if err != nil {
		return err
	}

	balance := overflow.Sub64p(l.balanceOf(token.owner), 1)
	tokenCount := overflow.Sub64p(l.activeTokens, 1)
	ownerString := token.owner.String()
	tidString := tid.String()

	token.owner = ""
	token.approved = ""
	token.uri = ""
	token.burned = true
	l.tokens.Set(tidString, token)
	if balance == 0 {
		l.balances.Remove(ownerString)
	} else {
		l.balances.Set(ownerString, balance)
	}
	l.activeTokens = tokenCount

	chain.Emit(
		BurnEvent,
		"token", l.nft.ID(),
		"from", ownerString,
		"tokenId", tidString,
	)
	return nil
}

func (l *PrivateLedger) Approve(caller, to address, tid TokenID) error {
	token, err := l.activeToken(tid)
	if err != nil {
		return err
	}
	if to != "" && !validAddress(to) {
		return ErrInvalidAddress
	}
	if caller != token.owner && !l.nft.IsApprovedForAll(token.owner, caller) {
		return ErrCallerIsNotOwnerOrApproved
	}
	if to == token.owner {
		return ErrApprovalToCurrentOwner
	}

	token.approved = to
	l.tokens.Set(tid.String(), token)
	chain.Emit(
		ApprovalEvent,
		"token", l.nft.ID(),
		"owner", token.owner.String(),
		"to", to.String(),
		"tokenId", tid.String(),
	)
	return nil
}

func (l *PrivateLedger) SetApprovalForAll(caller, operator address, approved bool) error {
	if !validAddress(caller) || !validAddress(operator) {
		return ErrInvalidAddress
	}
	if caller == operator {
		return ErrSelfApproval
	}

	key := approvalKey(caller, operator)
	if approved {
		l.operatorApprovals.Set(key, true)
	} else {
		l.operatorApprovals.Remove(key)
	}

	chain.Emit(
		ApprovalForAllEvent,
		"token", l.nft.ID(),
		"owner", caller.String(),
		"to", operator.String(),
		"approved", strconv.FormatBool(approved),
	)
	return nil
}

func (l *PrivateLedger) TransferFrom(caller, from, to address, tid TokenID) error {
	if !validTokenID(tid) {
		return ErrInvalidTokenID
	}
	if !validAddress(caller) || !validAddress(from) || !validAddress(to) {
		return ErrInvalidAddress
	}
	if from == to {
		return ErrCannotTransferToSelf
	}

	token, err := l.activeToken(tid)
	if err != nil {
		return err
	}
	if token.owner != from {
		return ErrTransferFromIncorrectOwner
	}
	if caller != token.owner &&
		caller != token.approved &&
		!l.nft.IsApprovedForAll(token.owner, caller) {
		return ErrCallerIsNotOwnerOrApproved
	}

	fromBalance := overflow.Sub64p(l.balanceOf(from), 1)
	toBalance := overflow.Add64p(l.balanceOf(to), 1)
	fromString := from.String()
	toString := to.String()

	token.owner = to
	token.approved = ""
	l.tokens.Set(tid.String(), token)
	if fromBalance == 0 {
		l.balances.Remove(fromString)
	} else {
		l.balances.Set(fromString, fromBalance)
	}
	l.balances.Set(toString, toBalance)

	chain.Emit(
		TransferEvent,
		"token", l.nft.ID(),
		"from", fromString,
		"to", toString,
		"tokenId", tid.String(),
	)
	return nil
}

func (l *PrivateLedger) SetTokenURI(caller address, tid TokenID, uri TokenURI) error {
	token, err := l.activeToken(tid)
	if err != nil {
		return err
	}
	if caller != token.owner {
		return ErrCallerIsNotOwner
	}

	token.uri = uri.String()
	l.tokens.Set(tid.String(), token)
	chain.Emit(
		TokenURIUpdateEvent,
		"token", l.nft.ID(),
		"tokenId", tid.String(),
	)
	return nil
}

func (l *PrivateLedger) activeToken(tid TokenID) (tokenRecord, error) {
	if !validTokenID(tid) {
		return tokenRecord{}, ErrInvalidTokenID
	}
	value := l.tokens.Get(tid.String())
	if value == nil {
		return tokenRecord{}, ErrTokenNotFound
	}
	token := value.(tokenRecord)
	if token.burned {
		return tokenRecord{}, ErrTokenNotFound
	}
	return token, nil
}

func (l *PrivateLedger) balanceOf(owner address) int64 {
	value := l.balances.Get(owner.String())
	if value == nil {
		return 0
	}
	return value.(int64)
}

func approvalKey(owner, operator address) string {
	return owner.String() + ":" + operator.String()
}
