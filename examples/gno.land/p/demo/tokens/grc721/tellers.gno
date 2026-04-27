package grc721

import (
	"chain"
	"chain/runtime"
)

// Teller is the high-level NFT API. It composes the read methods exposed
// by *NFT with caller-resolved write methods.
//
// All write methods source the acting principal from the Teller's
// accountFn, which is fixed at construction by one of the factory
// methods below (CallerTeller, ReadonlyTeller, RealmTeller,
// RealmSubTeller, ImpersonateTeller). This is the only place where
// runtime.PreviousRealm or runtime.CurrentRealm is consulted in the
// package; *NFT and *PrivateLedger never inspect them.
type Teller interface {
	// Reads (mirror *NFT for ergonomics).
	Name() string
	Symbol() string
	BalanceOf(addr address) (int64, error)
	OwnerOf(tid TokenID) (address, error)
	TokenURI(tid TokenID) (string, error)
	GetApproved(tid TokenID) (address, error)
	IsApprovedForAll(owner, op address) bool

	// Writes (caller-resolved via accountFn).
	Transfer(to address, tid TokenID) error
	TransferFrom(from, to address, tid TokenID) error
	SafeTransferFrom(from, to address, tid TokenID) error
	Approve(to address, tid TokenID) error
	SetApprovalForAll(op address, approved bool) error
	SetTokenURI(tid TokenID, uri TokenURI) error
}

// fnTeller is the canonical Teller implementation. It wraps an *NFT
// (which provides the read API via embedding) with an accountFn that
// supplies the acting principal for write methods.
//
// A nil accountFn marks the Teller as read-only; write methods return
// ErrReadonly.
type fnTeller struct {
	accountFn func() address
	*NFT
}

var _ Teller = (*fnTeller)(nil)

// CallerTeller returns a Teller that resolves the acting principal from
// runtime.PreviousRealm() on every write call. It is safe to expose
// publicly so users can manipulate their own tokens directly, or so
// realms can act through their allowance.
func (n *NFT) CallerTeller() Teller {
	return &fnTeller{
		accountFn: func() address {
			return runtime.PreviousRealm().Address()
		},
		NFT: n,
	}
}

// ReadonlyTeller returns a Teller whose write methods all return
// ErrReadonly. Read methods pass through to *NFT unchanged.
func (n *NFT) ReadonlyTeller() Teller {
	return &fnTeller{
		accountFn: nil,
		NFT:       n,
	}
}

// RealmTeller captures runtime.PreviousRealm().Address() at construction
// and uses it for every write call. The captured realm becomes the
// permanent identity of the resulting Teller — useful for DAOs/treasuries
// that hold NFTs on behalf of the realm itself.
//
// WARN: Should be initialized within a crossing function. This way the
// realm that created the teller will match what PreviousRealm reports.
func (n *NFT) RealmTeller() Teller {
	caller := runtime.PreviousRealm().Address()
	return &fnTeller{
		accountFn: func() address { return caller },
		NFT:       n,
	}
}

// RealmSubTeller is like RealmTeller but uses chain.PackageAddress of
// `caller + "/" + slug` as the account, deriving a sub-account address.
//
// WARN: Same crossing-function caveat as RealmTeller.
func (n *NFT) RealmSubTeller(slug string) Teller {
	caller := runtime.PreviousRealm().Address()
	account := accountSlugAddr(caller, slug)
	return &fnTeller{
		accountFn: func() address { return account },
		NFT:       n,
	}
}

// ImpersonateTeller acts as if its caller is the supplied address.
// Possessing *PrivateLedger is the authorization gate; reserved for
// admin/migration use.
func (l *PrivateLedger) ImpersonateTeller(addr address) Teller {
	return &fnTeller{
		accountFn: func() address { return addr },
		NFT:       l.nft,
	}
}

// Transfer is a sugar over TransferFrom for the case where the caller is
// the current owner. EIP-721 does not specify Transfer, so this method
// is a Gno-only convenience.
func (ft *fnTeller) Transfer(to address, tid TokenID) error {
	if ft.accountFn == nil {
		return ErrReadonly
	}
	caller := ft.accountFn()
	return ft.NFT.ledger.Transfer(caller, to, tid)
}

// TransferFrom moves token tid from `from` to `to`. The caller must be
// the owner of tid, an approved operator for the owner, or the address
// approved for tid.
func (ft *fnTeller) TransferFrom(from, to address, tid TokenID) error {
	if ft.accountFn == nil {
		return ErrReadonly
	}
	caller := ft.accountFn()
	if !ft.isApprovedOrOwner(caller, tid) {
		return ErrCallerIsNotOwnerOrApproved
	}
	return ft.NFT.ledger.Transfer(from, to, tid)
}

// SafeTransferFrom currently aliases TransferFrom: there is no
// registry-based receiver check yet (see
// https://github.com/gnolang/gno/issues/5546). New code should call
// TransferFrom directly.
func (ft *fnTeller) SafeTransferFrom(from, to address, tid TokenID) error {
	return ft.TransferFrom(from, to, tid)
}

// Approve: caller must be the owner of tid or an approved-for-all
// operator of the owner.
func (ft *fnTeller) Approve(to address, tid TokenID) error {
	if ft.accountFn == nil {
		return ErrReadonly
	}
	caller := ft.accountFn()
	owner, err := ft.NFT.OwnerOf(tid)
	if err != nil {
		return err
	}
	if caller != owner && !ft.NFT.IsApprovedForAll(owner, caller) {
		return ErrCallerIsNotOwnerOrApproved
	}
	return ft.NFT.ledger.Approve(to, tid)
}

func (ft *fnTeller) SetApprovalForAll(op address, approved bool) error {
	if ft.accountFn == nil {
		return ErrReadonly
	}
	caller := ft.accountFn()
	return ft.NFT.ledger.SetApprovalForAll(caller, op, approved)
}

func (ft *fnTeller) SetTokenURI(tid TokenID, uri TokenURI) error {
	if ft.accountFn == nil {
		return ErrReadonly
	}
	caller := ft.accountFn()
	owner, err := ft.NFT.OwnerOf(tid)
	if err != nil {
		return err
	}
	if caller != owner {
		return ErrCallerIsNotOwner
	}
	return ft.NFT.ledger.SetTokenURI(tid, uri)
}

// isApprovedOrOwner reports whether addr is allowed to act on tid:
// either it owns tid, the owner has approved-for-all addr, or addr is
// the per-token approved address for tid.
func (ft *fnTeller) isApprovedOrOwner(addr address, tid TokenID) bool {
	owner, err := ft.NFT.OwnerOf(tid)
	if err != nil {
		return false
	}
	if addr == owner || ft.NFT.IsApprovedForAll(owner, addr) {
		return true
	}
	approved, err := ft.NFT.GetApproved(tid)
	if err != nil {
		return false
	}
	return approved == addr
}

func accountSlugAddr(addr address, slug string) address {
	if slug == "" {
		return addr
	}
	key := addr.String() + "/" + slug
	return chain.PackageAddress(key)
}
