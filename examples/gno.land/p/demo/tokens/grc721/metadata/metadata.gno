// Package metadata provides an on-chain NFT metadata extension for grc721.
//
//   - MetadataNFT composes *grc721.NFT with a private metadata ledger.
//   - MetadataNFT and *PrivateLedger never inspect runtime.PreviousRealm
//     or runtime.CurrentRealm. Possessing *PrivateLedger IS the
//     authorization for admin-grade SetTokenMetadata.
//   - Caller-resolved access lives on metadata.Teller, which embeds
//     grc721.Teller and adds metadata-specific methods.
//
// The Metadata struct mirrors OpenSea's metadata schema for tokens whose
// metadata lives on chain rather than behind a TokenURI fetch.
package metadata

import (
	"chain/runtime"
	"errors"

	"gno.land/p/demo/tokens/grc721"
	"gno.land/p/nt/bptree/v0"
)

// Trait is one OpenSea-style attribute entry.
type Trait struct {
	DisplayType string
	TraitType   string
	Value       string
}

// Metadata is the on-chain metadata schema for a single NFT.
//
// Field semantics follow https://docs.opensea.io/docs/metadata-standards.
type Metadata struct {
	Image           string
	ImageData       string
	ExternalURL     string
	Description     string
	Name            string
	Attributes      []Trait
	BackgroundColor string
	AnimationURL    string
	YoutubeURL      string
}

// IMetadata is the on-chain metadata read interface.
type IMetadata interface {
	TokenMetadata(tid grc721.TokenID) (Metadata, error)
}

var ErrMetadataNotSet = errors.New("metadata not set for token")

// MetadataNFT is an *grc721.NFT extended with per-token on-chain
// metadata.
type MetadataNFT struct {
	*grc721.NFT
	mLedger *PrivateLedger
}

// PrivateLedger holds metadata state and exposes admin-grade metadata
// operations. Possessing this pointer is the authorization gate.
type PrivateLedger struct {
	entries    bptree.BPTree
	mNFT       *MetadataNFT
	coreLedger *grc721.PrivateLedger
}

// NewMetadataNFT creates a new metadata-enabled NFT collection.
//
// Expected usage:
//
//	var Token, coreLedger, mLedger = metadata.NewMetadataNFT("Foo", "FOO")
//
// Both ledger pointers must be kept private.
func NewMetadataNFT(name, symbol string) (*MetadataNFT, *grc721.PrivateLedger, *PrivateLedger) {
	nft, coreLedger := grc721.NewNFT(name, symbol)
	mLedger := &PrivateLedger{
		coreLedger: coreLedger,
	}
	mNFT := &MetadataNFT{
		NFT:     nft,
		mLedger: mLedger,
	}
	mLedger.mNFT = mNFT
	return mNFT, coreLedger, mLedger
}

// SetTokenMetadata stores metadata for tid. Admin-grade: no caller check.
//
// Validity invariants:
//   - tid exists
func (l *PrivateLedger) SetTokenMetadata(tid grc721.TokenID, m Metadata) error {
	if _, err := l.mNFT.NFT.OwnerOf(tid); err != nil {
		return err
	}
	l.entries.Set(tid.String(), m)
	return nil
}

// TokenMetadata returns the metadata for tid, satisfying IMetadata.
func (m *MetadataNFT) TokenMetadata(tid grc721.TokenID) (Metadata, error) {
	if _, err := m.NFT.OwnerOf(tid); err != nil {
		return Metadata{}, err
	}
	v, found := m.mLedger.entries.Get(tid.String())
	if !found {
		return Metadata{}, ErrMetadataNotSet
	}
	return v.(Metadata), nil
}

// Teller is the metadata extension's high-level API. It composes
// grc721.Teller with metadata-specific methods.
type Teller interface {
	grc721.Teller
	SetTokenMetadata(tid grc721.TokenID, m Metadata) error
	TokenMetadata(tid grc721.TokenID) (Metadata, error)
}

// fnTeller embeds a grc721.Teller and adds metadata-specific methods
// that resolve their own caller via accountFn.
type fnTeller struct {
	grc721.Teller
	mNFT      *MetadataNFT
	accountFn func() address
}

// CallerTeller returns a Teller that resolves the acting principal from
// runtime.PreviousRealm() per call.
func (m *MetadataNFT) CallerTeller() Teller {
	return &fnTeller{
		Teller:    m.NFT.CallerTeller(),
		mNFT:      m,
		accountFn: func() address { return runtime.PreviousRealm().Address() },
	}
}

// ReadonlyTeller returns a Teller whose write methods all return
// grc721.ErrReadonly.
func (m *MetadataNFT) ReadonlyTeller() Teller {
	return &fnTeller{
		Teller:    m.NFT.ReadonlyTeller(),
		mNFT:      m,
		accountFn: nil,
	}
}

// RealmTeller captures runtime.PreviousRealm().Address() at construction
// for both core and metadata methods.
//
// WARN: must be invoked inside a crossing function so PreviousRealm
// resolves to the realm that should own the resulting Teller.
func (m *MetadataNFT) RealmTeller() Teller {
	caller := runtime.PreviousRealm().Address()
	return &fnTeller{
		Teller:    m.NFT.RealmTeller(),
		mNFT:      m,
		accountFn: func() address { return caller },
	}
}

// ImpersonateTeller returns a Teller acting as addr for both core and
// metadata methods.
func (l *PrivateLedger) ImpersonateTeller(addr address) Teller {
	return &fnTeller{
		Teller:    l.coreLedger.ImpersonateTeller(addr),
		mNFT:      l.mNFT,
		accountFn: func() address { return addr },
	}
}

// SetTokenMetadata (Teller). Caller must be the owner of tid.
func (ft *fnTeller) SetTokenMetadata(tid grc721.TokenID, m Metadata) error {
	if ft.accountFn == nil {
		return grc721.ErrReadonly
	}
	caller := ft.accountFn()
	owner, err := ft.mNFT.NFT.OwnerOf(tid)
	if err != nil {
		return err
	}
	if caller != owner {
		return grc721.ErrCallerIsNotOwner
	}
	return ft.mNFT.mLedger.SetTokenMetadata(tid, m)
}

// TokenMetadata (Teller). Read-only delegation; no caller check needed.
func (ft *fnTeller) TokenMetadata(tid grc721.TokenID) (Metadata, error) {
	return ft.mNFT.TokenMetadata(tid)
}

// Compile-time interface satisfaction.
var (
	_ IMetadata = (*MetadataNFT)(nil)
	_ Teller    = (*fnTeller)(nil)
)
