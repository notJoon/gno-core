package grc721

// IGRC721Core is the core EIP-721 surface, separated from metadata and
// enumerable concerns.
//
// SetTokenURI lives on IGRC721Metadata, not here, so collections without
// metadata can satisfy IGRC721Core directly.
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

// IGRC721CollectionMetadata exposes name/symbol for an NFT collection.
type IGRC721CollectionMetadata interface {
	Name() string
	Symbol() string
}

// IGRC721Metadata is the per-token metadata extension surface.
type IGRC721Metadata interface {
	IGRC721CollectionMetadata
	TokenURI(tid TokenID) (string, error)
	SetTokenURI(tid TokenID, uri TokenURI) (bool, error)
}

// IGRC721Enumerable exposes the supply-counting portion of the EIP-721
// enumerable extension. TokenByIndex and TokenOfOwnerByIndex are not
// implemented yet.
type IGRC721Enumerable interface {
	TotalSupply() int64
}

var (
	_ IGRC721CollectionMetadata = (*NFT)(nil)
	_ IGRC721Enumerable         = (*NFT)(nil)
	_ IGRC721Core               = (*fnTeller)(nil)
	_ IGRC721Metadata           = (*fnTeller)(nil)
)
