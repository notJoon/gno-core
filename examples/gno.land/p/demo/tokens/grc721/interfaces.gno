package grc721

// IGRC721Core is the core EIP-721 surface, separated from metadata and
// enumerable concerns. See ADR-0001 §3.6 for the segregation rationale.
//
// SetTokenURI lives on IGRC721Metadata, not here. In the legacy
// monolithic IGRC721 interface (igrc721.gno) it was bundled in core,
// which prevented metadata-less collections from satisfying the type.
type IGRC721Core interface {
	BalanceOf(addr address) (int64, error)
	OwnerOf(tid TokenID) (address, error)
	GetApproved(tid TokenID) (address, error)
	IsApprovedForAll(owner, op address) bool
	Approve(to address, tid TokenID) error
	SetApprovalForAll(op address, approved bool) error
	TransferFrom(from, to address, tid TokenID) error
	SafeTransferFrom(from, to address, tid TokenID) error
}

// IGRC721Enumerable exposes the supply-counting portion of the EIP-721
// enumerable extension. TokenByIndex and TokenOfOwnerByIndex are out of
// scope for this refactor.
type IGRC721Enumerable interface {
	TotalSupply() int64
}

// Compile-time interface satisfaction.
//
// These witnesses make the layering from ADR-0001 §3.6 testable from a
// glance: a reviewer can verify the design by reading this block alone.
// The legacy IGRC721 / IGRC721Metadata witnesses are retained until
// commit 10 deletes the legacy types; *fnTeller continues to satisfy
// both shapes throughout the migration.
var (
	_ IGRC721CollectionMetadata = (*NFT)(nil)      // collection name/symbol
	_ IGRC721Enumerable         = (*NFT)(nil)      // total supply view
	_ IGRC721Core               = (*fnTeller)(nil) // caller-resolved core writes
	_ IGRC721Metadata           = (*fnTeller)(nil) // legacy metadata reads (TokenURI via embedded *NFT)
	_ IGRC721                   = (*fnTeller)(nil) // legacy monolithic — removed in commit 10
)
