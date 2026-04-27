// Package royalty is an EIP-2981 royalty extension for grc721.
//
//   - RoyaltyNFT composes *grc721.NFT with a private royalty ledger.
//   - RoyaltyNFT and *PrivateLedger never inspect runtime.PreviousRealm
//     or runtime.CurrentRealm. Possessing *PrivateLedger IS the
//     authorization for admin-grade SetTokenRoyalty.
//   - Caller-resolved access lives on royalty.Teller, which embeds
//     grc721.Teller and adds royalty-specific methods.
package royalty

import (
	"chain/runtime"
	"errors"
	"math/overflow"

	"gno.land/p/demo/tokens/grc721"
	"gno.land/p/nt/bptree/v0"
)

const (
	// MaxBps is the maximum royalty representable in basis points (100%).
	MaxBps = 10_000

	// BpsDenominator is the denominator used to convert basis points to
	// a fraction. 10_000 bps = 100%.
	BpsDenominator = 10_000
)

// RoyaltyInfo records the per-token royalty configuration. Bps is the
// rate in basis points (1 bps = 0.01%, 10_000 bps = 100%) per EIP-2981.
type RoyaltyInfo struct {
	PaymentAddress address
	Bps            int64
}

// IGRC2981 follows EIP-2981.
type IGRC2981 interface {
	RoyaltyInfo(tid grc721.TokenID, salePrice int64) (address, int64, error)
}

var (
	ErrInvalidRoyaltyBps            = errors.New("invalid royalty bps")
	ErrInvalidRoyaltyPaymentAddress = errors.New("invalid royalty payment address")
	ErrInvalidMaxBps                = errors.New("invalid max bps")
)

// RoyaltyNFT is an *grc721.NFT extended with per-token royalty data.
type RoyaltyNFT struct {
	*grc721.NFT
	rLedger *PrivateLedger
}

// PrivateLedger holds royalty state and exposes admin-grade royalty
// operations. As with grc721.PrivateLedger, possessing this pointer is
// the authorization gate; it must be kept private by the host realm.
type PrivateLedger struct {
	royalties  *bptree.BPTree
	maxBps     int64
	rNFT       *RoyaltyNFT
	coreLedger *grc721.PrivateLedger
}

// NewRoyaltyNFT creates a new royalty-enabled NFT collection.
//
// maxBps caps per-token royalties; passing 0 defaults to MaxBps.
//
// Expected usage:
//
//	var Token, coreLedger, rLedger = royalty.NewRoyaltyNFT("Foo", "FOO", 1000)
//
// Both ledger pointers must be kept private. The coreLedger is the
// underlying grc721.PrivateLedger; the rLedger is royalty-specific.
func NewRoyaltyNFT(name, symbol string, maxBps int64) (*RoyaltyNFT, *grc721.PrivateLedger, *PrivateLedger) {
	if maxBps == 0 {
		maxBps = MaxBps
	}
	if maxBps < 0 || maxBps > MaxBps {
		panic(ErrInvalidMaxBps)
	}

	nft, coreLedger := grc721.NewNFT(name, symbol)
	rLedger := &PrivateLedger{
		royalties:  bptree.NewBPTree32(),
		maxBps:     maxBps,
		coreLedger: coreLedger,
	}
	rNFT := &RoyaltyNFT{
		NFT:     nft,
		rLedger: rLedger,
	}
	rLedger.rNFT = rNFT
	return rNFT, coreLedger, rLedger
}

// MaxBps returns the configured per-token royalty cap.
func (l *PrivateLedger) MaxBps() int64 { return l.maxBps }

// SetTokenRoyalty stores royalty information for tid. Admin-grade: no
// caller check. Authorization belongs to the Teller layer.
//
// Validity invariants:
//   - info.PaymentAddress is valid
//   - info.Bps is in [0, maxBps]
//   - tid exists
func (l *PrivateLedger) SetTokenRoyalty(tid grc721.TokenID, info RoyaltyInfo) error {
	if !info.PaymentAddress.IsValid() {
		return ErrInvalidRoyaltyPaymentAddress
	}
	if info.Bps < 0 || info.Bps > l.maxBps {
		return ErrInvalidRoyaltyBps
	}
	if _, err := l.rNFT.NFT.OwnerOf(tid); err != nil {
		return err
	}
	l.royalties.Set(tid.String(), info)
	return nil
}

// RoyaltyInfo returns the payment address and royalty amount due for a
// sale of tid at salePrice, satisfying IGRC2981.
func (r *RoyaltyNFT) RoyaltyInfo(tid grc721.TokenID, salePrice int64) (address, int64, error) {
	val, found := r.rLedger.royalties.Get(tid.String())
	if !found {
		return "", 0, grc721.ErrInvalidTokenId
	}
	info := val.(RoyaltyInfo)
	amount := overflow.Mul64p(salePrice, info.Bps) / BpsDenominator
	return info.PaymentAddress, amount, nil
}

// Teller is the royalty extension's high-level API. It composes
// grc721.Teller with royalty-specific methods.
type Teller interface {
	grc721.Teller
	SetTokenRoyalty(tid grc721.TokenID, info RoyaltyInfo) error
	RoyaltyInfo(tid grc721.TokenID, salePrice int64) (address, int64, error)
}

// fnTeller is the canonical extension Teller implementation. It embeds
// a grc721.Teller (for core methods) and adds royalty-specific methods
// that resolve their own caller via accountFn.
//
// The embedded grc721.Teller carries its own accountFn; this struct
// carries a separate one. Both follow runtime.PreviousRealm() per
// invocation (or the same fixed address for ImpersonateTeller), so
// consistency is structural.
type fnTeller struct {
	grc721.Teller
	rNFT      *RoyaltyNFT
	accountFn func() address
}

// CallerTeller returns a Teller that resolves the acting principal from
// runtime.PreviousRealm() per call. Safe to expose publicly.
func (r *RoyaltyNFT) CallerTeller() Teller {
	return &fnTeller{
		Teller:    r.NFT.CallerTeller(),
		rNFT:      r,
		accountFn: func() address { return runtime.PreviousRealm().Address() },
	}
}

// ReadonlyTeller returns a Teller whose write methods all return
// grc721.ErrReadonly.
func (r *RoyaltyNFT) ReadonlyTeller() Teller {
	return &fnTeller{
		Teller:    r.NFT.ReadonlyTeller(),
		rNFT:      r,
		accountFn: nil,
	}
}

// RealmTeller captures runtime.PreviousRealm().Address() at construction
// for both core and royalty methods.
//
// WARN: must be invoked inside a crossing function so PreviousRealm
// resolves to the realm that should own the resulting Teller.
func (r *RoyaltyNFT) RealmTeller() Teller {
	caller := runtime.PreviousRealm().Address()
	return &fnTeller{
		Teller:    r.NFT.RealmTeller(),
		rNFT:      r,
		accountFn: func() address { return caller },
	}
}

// ImpersonateTeller returns a Teller acting as addr for both core and
// royalty methods. Reserved for admin paths; possessing the royalty
// *PrivateLedger is the authorization gate.
func (l *PrivateLedger) ImpersonateTeller(addr address) Teller {
	return &fnTeller{
		Teller:    l.coreLedger.ImpersonateTeller(addr),
		rNFT:      l.rNFT,
		accountFn: func() address { return addr },
	}
}

// SetTokenRoyalty (Teller). Caller must be the owner of tid.
func (ft *fnTeller) SetTokenRoyalty(tid grc721.TokenID, info RoyaltyInfo) error {
	if ft.accountFn == nil {
		return grc721.ErrReadonly
	}
	caller := ft.accountFn()
	owner, err := ft.rNFT.NFT.OwnerOf(tid)
	if err != nil {
		return err
	}
	if caller != owner {
		return grc721.ErrCallerIsNotOwner
	}
	return ft.rNFT.rLedger.SetTokenRoyalty(tid, info)
}

// RoyaltyInfo (Teller) — read-only delegation; no caller check needed.
func (ft *fnTeller) RoyaltyInfo(tid grc721.TokenID, salePrice int64) (address, int64, error) {
	return ft.rNFT.RoyaltyInfo(tid, salePrice)
}

// Compile-time interface satisfaction.
var (
	_ IGRC2981 = (*RoyaltyNFT)(nil)
	_ Teller   = (*fnTeller)(nil)
)
