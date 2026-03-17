package grc721

import (
	"chain"
	"chain/runtime"
	"math/overflow"
	"strconv"

	"gno.land/p/nt/avl/v0"
	"gno.land/p/nt/ufmt/v0"
)

// tokenData holds all per-token state in a single struct,
// eliminating the need for separate owners/tokenURIs/tokenApprovals trees.
type tokenData struct {
	approved address
	owner    address
	uri      string
}

type basicNFT struct {
	name              string
	symbol            string
	tokens            avl.Tree // tokenId -> tokenData (owner + uri + approved)
	balances          avl.Tree // OwnerAddress -> TokenCount
	operatorApprovals avl.Tree // "OwnerAddress:OperatorAddress" -> bool
}

// Returns new basic NFT
func NewBasicNFT(name string, symbol string) *basicNFT {
	return &basicNFT{
		name:   name,
		symbol: symbol,

		tokens:            avl.Tree{},
		balances:          avl.Tree{},
		operatorApprovals: avl.Tree{},
	}
}

func (s *basicNFT) Name() string      { return s.name }
func (s *basicNFT) Symbol() string    { return s.symbol }
func (s *basicNFT) TokenCount() int64 { return int64(s.tokens.Size()) }

// getToken retrieves the tokenData for a given token ID.
func (s *basicNFT) getToken(tid TokenID) (tokenData, bool) {
	val, found := s.tokens.Get(string(tid))
	if !found {
		return tokenData{}, false
	}
	return val.(tokenData), true
}

// BalanceOf returns balance of input address
func (s *basicNFT) BalanceOf(addr address) (int64, error) {
	if err := isValidAddress(addr); err != nil {
		return 0, err
	}

	balance, found := s.balances.Get(addr.String())
	if !found {
		return 0, nil
	}

	return balance.(int64), nil
}

// OwnerOf returns owner of input token id
func (s *basicNFT) OwnerOf(tid TokenID) (address, error) {
	td, found := s.getToken(tid)
	if !found {
		return "", ErrInvalidTokenId
	}

	return td.owner, nil
}

// TokenURI returns the URI of input token id
func (s *basicNFT) TokenURI(tid TokenID) (string, error) {
	td, found := s.getToken(tid)
	if !found {
		return "", ErrInvalidTokenId
	}

	return td.uri, nil
}

func (s *basicNFT) SetTokenURI(tid TokenID, tURI TokenURI) (bool, error) {
	td, found := s.getToken(tid)
	if !found {
		return false, ErrInvalidTokenId
	}

	caller := runtime.PreviousRealm().Address()
	if caller != td.owner {
		return false, ErrCallerIsNotOwner
	}
	td.uri = tURI.String()
	s.tokens.Set(string(tid), td)
	return true, nil
}

// IsApprovedForAll returns true if operator is approved for all by the owner.
// Otherwise, returns false
func (s *basicNFT) IsApprovedForAll(owner, operator address) bool {
	key := owner.String() + ":" + operator.String()
	approved, found := s.operatorApprovals.Get(key)
	if !found {
		return false
	}

	return approved.(bool)
}

// Approve approves the input address for particular token
func (s *basicNFT) Approve(to address, tid TokenID) error {
	if err := isValidAddress(to); err != nil {
		return err
	}

	td, found := s.getToken(tid)
	if !found {
		return ErrInvalidTokenId
	}
	if td.owner == to {
		return ErrApprovalToCurrentOwner
	}

	caller := runtime.PreviousRealm().Address()
	if caller != td.owner && !s.IsApprovedForAll(td.owner, caller) {
		return ErrCallerIsNotOwnerOrApproved
	}

	tidStr := tid.String()
	td.approved = to
	s.tokens.Set(tidStr, td)

	chain.Emit(
		ApprovalEvent,
		"slug", s.symbol,
		"owner", td.owner.String(),
		"to", to.String(),
		"tokenId", tidStr,
	)

	return nil
}

// GetApproved return the approved address for token
func (s *basicNFT) GetApproved(tid TokenID) (address, error) {
	td, found := s.getToken(tid)
	if !found {
		return zeroAddress, ErrTokenIdNotHasApproved
	}
	if td.approved == zeroAddress {
		return zeroAddress, ErrTokenIdNotHasApproved
	}

	return td.approved, nil
}

// SetApprovalForAll can approve the operator to operate on all tokens
func (s *basicNFT) SetApprovalForAll(operator address, approved bool) error {
	if err := isValidAddress(operator); err != nil {
		return ErrInvalidAddress
	}

	caller := runtime.PreviousRealm().Address()
	return s.setApprovalForAll(caller, operator, approved)
}

// Safely transfers `tokenId` token from `from` to `to`, checking that
// contract recipients are aware of the GRC721 protocol to prevent
// tokens from being forever locked.
func (s *basicNFT) SafeTransferFrom(from, to address, tid TokenID) error {
	caller := runtime.PreviousRealm().Address()
	if !s.isApprovedOrOwner(caller, tid) {
		return ErrCallerIsNotOwnerOrApproved
	}

	err := s.transfer(from, to, tid)
	if err != nil {
		return err
	}

	if !s.checkOnGRC721Received(from, to, tid) {
		return ErrTransferToNonGRC721Receiver
	}

	return nil
}

// Transfers `tokenId` token from `from` to `to`.
func (s *basicNFT) TransferFrom(from, to address, tid TokenID) error {
	caller := runtime.PreviousRealm().Address()
	if !s.isApprovedOrOwner(caller, tid) {
		return ErrCallerIsNotOwnerOrApproved
	}

	err := s.transfer(from, to, tid)
	if err != nil {
		return err
	}

	return nil
}

// Mints `tokenId` and transfers it to `to`.
func (s *basicNFT) Mint(to address, tid TokenID) error {
	return s.mint(to, tid)
}

// Mints `tokenId` and transfers it to `to`. Also checks that
// contract recipients are using GRC721 protocol
func (s *basicNFT) SafeMint(to address, tid TokenID) error {
	err := s.mint(to, tid)
	if err != nil {
		return err
	}

	if !s.checkOnGRC721Received(zeroAddress, to, tid) {
		return ErrTransferToNonGRC721Receiver
	}

	return nil
}

func (s *basicNFT) Burn(tid TokenID) error {
	td, found := s.getToken(tid)
	if !found {
		return ErrInvalidTokenId
	}

	s.beforeTokenTransfer(td.owner, zeroAddress, tid, 1)

	tidStr := tid.String()
	s.tokens.Remove(tidStr)
	balance, err := s.BalanceOf(td.owner)
	if err != nil {
		return err
	}
	balance = overflow.Sub64p(balance, 1)

	ownerStr := td.owner.String()
	s.balances.Set(ownerStr, balance)

	chain.Emit(
		BurnEvent,
		"slug", s.symbol,
		"from", ownerStr,
		"tokenId", tidStr,
	)

	s.afterTokenTransfer(td.owner, zeroAddress, tid, 1)

	return nil
}

/* Helper methods */

// Helper for SetApprovalForAll()
func (s *basicNFT) setApprovalForAll(owner, operator address, approved bool) error {
	if owner == operator {
		return ErrApprovalToCurrentOwner
	}

	key := owner.String() + ":" + operator.String()
	s.operatorApprovals.Set(key, approved)

	chain.Emit(
		ApprovalForAllEvent,
		"slug", s.symbol,
		"owner", owner.String(),
		"to", operator.String(),
		"approved", strconv.FormatBool(approved),
	)

	return nil
}

// Helper for TransferFrom() and SafeTransferFrom()
func (s *basicNFT) transfer(from, to address, tid TokenID) error {
	if err := isValidAddress(from); err != nil {
		return ErrInvalidAddress
	}
	if err := isValidAddress(to); err != nil {
		return ErrInvalidAddress
	}

	if from == to {
		return ErrCannotTransferToSelf
	}

	td, found := s.getToken(tid)
	if !found {
		return ErrInvalidTokenId
	}
	if td.owner != from {
		return ErrTransferFromIncorrectOwner
	}

	s.beforeTokenTransfer(from, to, tid, 1)

	// Check that tokenId was not transferred by `beforeTokenTransfer`
	td, found = s.getToken(tid)
	if !found {
		return ErrInvalidTokenId
	}
	if td.owner != from {
		return ErrTransferFromIncorrectOwner
	}

	// Update token: new owner, clear approval
	tidStr := string(tid)
	td.owner = to
	td.approved = zeroAddress
	s.tokens.Set(tidStr, td)

	fromBalance, err := s.BalanceOf(from)
	if err != nil {
		return err
	}
	toBalance, err := s.BalanceOf(to)
	if err != nil {
		return err
	}
	fromBalance = overflow.Sub64p(fromBalance, 1)
	toBalance = overflow.Add64p(toBalance, 1)

	fromStr := from.String()
	toStr := to.String()

	s.balances.Set(fromStr, fromBalance)
	s.balances.Set(toStr, toBalance)

	chain.Emit(
		TransferEvent,
		"slug", s.symbol,
		"from", fromStr,
		"to", toStr,
		"tokenId", tidStr,
	)

	s.afterTokenTransfer(from, to, tid, 1)

	return nil
}

// Helper for Mint() and SafeMint()
func (s *basicNFT) mint(to address, tid TokenID) error {
	if err := isValidAddress(to); err != nil {
		return err
	}

	if s.exists(tid) {
		return ErrTokenIdAlreadyExists
	}

	s.beforeTokenTransfer(zeroAddress, to, tid, 1)

	// Check that tokenId was not minted by `beforeTokenTransfer`
	if s.exists(tid) {
		return ErrTokenIdAlreadyExists
	}

	toBalance, err := s.BalanceOf(to)
	if err != nil {
		return err
	}
	toBalance = overflow.Add64p(toBalance, 1)
	toStr := to.String()
	tidStr := string(tid)
	s.balances.Set(toStr, toBalance)
	s.tokens.Set(tidStr, tokenData{owner: to})

	chain.Emit(
		MintEvent,
		"slug", s.symbol,
		"to", toStr,
		"tokenId", tidStr,
	)

	s.afterTokenTransfer(zeroAddress, to, tid, 1)

	return nil
}

func (s *basicNFT) isApprovedOrOwner(addr address, tid TokenID) bool {
	td, found := s.getToken(tid)
	if !found {
		return false
	}

	if addr == td.owner || s.IsApprovedForAll(td.owner, addr) {
		return true
	}

	return td.approved == addr
}

// Checks if token id already exists
func (s *basicNFT) exists(tid TokenID) bool {
	_, found := s.tokens.Get(string(tid))
	return found
}

func (s *basicNFT) beforeTokenTransfer(from, to address, firstTokenId TokenID, batchSize int64) {
	// TODO: Implementation
}

func (s *basicNFT) afterTokenTransfer(from, to address, firstTokenId TokenID, batchSize int64) {
	// TODO: Implementation
}

func (s *basicNFT) checkOnGRC721Received(from, to address, tid TokenID) bool {
	// TODO: Implementation
	return true
}

func (s *basicNFT) RenderHome() (str string) {
	str += ufmt.Sprintf("# %s ($%s)\n\n", s.name, s.symbol)
	str += ufmt.Sprintf("* **Total supply**: %d\n", s.TokenCount())
	str += ufmt.Sprintf("* **Known accounts**: %d\n", s.balances.Size())

	return
}

func (n *basicNFT) Getter() NFTGetter {
	return func() IGRC721 {
		return n
	}
}
