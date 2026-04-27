package grc721

import (
	"chain/runtime"

	"gno.land/p/nt/bptree/v0"
	"gno.land/p/nt/ufmt/v0"
)

// NFT is an NFT collection's public-safe identity and read-only view.
//
// Holding an *NFT lets a caller read collection metadata and token state
// but never mutate it: admin powers (Mint, Burn, unauthorised Transfer,
// etc.) live on the paired *PrivateLedger returned by NewNFT. Realms
// should keep the *PrivateLedger reference unexported and may freely
// expose the *NFT.
type NFT struct {
	name      string
	symbol    string
	origRealm string
	ledger    *PrivateLedger
}

// PrivateLedger holds an NFT collection's mutable state and its
// admin-grade operations.
//
// Methods on *PrivateLedger take explicit from/owner arguments and never
// inspect runtime.PreviousRealm or runtime.CurrentRealm. Possessing a
// *PrivateLedger pointer IS the authorisation. The host realm must keep
// this pointer in an unexported variable; public exposure leaks
// unrestricted admin access (Mint, Burn, override-Approve, ...).
type PrivateLedger struct {
	owners            bptree.BPTree // TokenID    -> address
	balances          bptree.BPTree // address    -> int64
	tokenApprovals    bptree.BPTree // TokenID    -> address
	tokenURIs         bptree.BPTree // TokenID    -> string
	operatorApprovals bptree.BPTree // owner+":"+op -> bool
	nft               *NFT
}

// NewNFT creates a new NFT collection and returns the public *NFT
// identity alongside the private *PrivateLedger admin view.
//
// Expected usage:
//
//	var Token, ledger = grc721.NewNFT("FooNFT", "FNFT")
//
// Do not export the returned *PrivateLedger.
func NewNFT(name, symbol string) (*NFT, *PrivateLedger) {
	if name == "" {
		panic("name should not be empty")
	}
	if symbol == "" {
		panic("symbol should not be empty")
	}

	ledger := &PrivateLedger{}
	nft := &NFT{
		name:      name,
		symbol:    symbol,
		origRealm: runtime.CurrentRealm().PkgPath(),
		ledger:    ledger,
	}
	ledger.nft = nft
	return nft, ledger
}

func (n *NFT) Name() string      { return n.name }
func (n *NFT) Symbol() string    { return n.symbol }
func (n *NFT) ID() string        { return n.origRealm + "." + n.symbol }
func (n *NFT) TokenCount() int64 { return int64(n.ledger.owners.Size()) }

// TotalSupply is an alias of TokenCount, aligning naming with grc20.
func (n *NFT) TotalSupply() int64 { return n.TokenCount() }

func (n *NFT) BalanceOf(addr address) (int64, error) {
	if err := isValidAddress(addr); err != nil {
		return 0, err
	}
	bal, found := n.ledger.balances.Get(addr.String())
	if !found {
		return 0, nil
	}
	return bal.(int64), nil
}

// OwnerOf returns the address that currently owns tid, or
// ErrInvalidTokenId if the token does not exist.
func (n *NFT) OwnerOf(tid TokenID) (address, error) {
	owner, found := n.ledger.owners.Get(tid.String())
	if !found {
		return "", ErrInvalidTokenId
	}
	return owner.(address), nil
}

func (n *NFT) TokenURI(tid TokenID) (string, error) {
	uri, found := n.ledger.tokenURIs.Get(tid.String())
	if !found {
		return "", ErrInvalidTokenId
	}
	return uri.(string), nil
}

// GetApproved returns the address approved to act on tid, or
// the empty address with ErrTokenIdNotHasApproved when no approval is set.
func (n *NFT) GetApproved(tid TokenID) (address, error) {
	addr, found := n.ledger.tokenApprovals.Get(tid.String())
	if !found {
		return "", ErrTokenIdNotHasApproved
	}
	return addr.(address), nil
}

func (n *NFT) IsApprovedForAll(owner, op address) bool {
	key := owner.String() + ":" + op.String()
	v, found := n.ledger.operatorApprovals.Get(key)
	if !found {
		return false
	}
	return v.(bool)
}

func (n *NFT) RenderHome() string {
	return ufmt.Sprintf(
		"# %s ($%s)\n\n* **Total supply**: %d\n* **Known accounts**: %d\n",
		n.name, n.symbol, n.TokenCount(), n.ledger.balances.Size(),
	)
}
