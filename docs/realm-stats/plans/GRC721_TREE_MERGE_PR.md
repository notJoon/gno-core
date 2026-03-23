# PR: refactor(grc721): merge 3 per-token trees into single tokens tree

## Summary

Merge `owners`, `tokenURIs`, and `tokenApprovals` (all keyed by tokenId) into a single `tokens` tree with a `tokenData` struct value. Reduces `basicNFT` from 5 trees to 3.

**Before:**
```go
type basicNFT struct {
    owners            avl.Tree // tokenId -> address
    balances          avl.Tree // ownerAddress -> int64
    tokenApprovals    avl.Tree // tokenId -> address
    tokenURIs         avl.Tree // tokenId -> string
    operatorApprovals avl.Tree // "owner:operator" -> bool
}
```

**After:**
```go
type tokenData struct {
    owner    address
    uri      string
    approved address
}

type basicNFT struct {
    tokens            avl.Tree // tokenId -> tokenData
    balances          avl.Tree // ownerAddress -> int64
    operatorApprovals avl.Tree // "owner:operator" -> bool
}
```

## Motivation

Each `avl.Tree` node is stored as a separate Object in the Gno VM with ~112 bytes of `ObjectInfo` overhead. Storing the same tokenId across 3 trees creates 2 redundant Objects per token.

This is especially costly in DeFi patterns like GnoSwap's `Mint → SetTokenURI → TransferFrom` flow, which touches all 3 trees and dirties them separately.

## Changes

- Add `tokenData` struct combining `owner`, `uri`, and `approved` fields
- Reduce `basicNFT` from 5 trees to 3
- Update all method internals (no public API / `IGRC721` interface changes)
- Fix: `Burn` now also removes the token URI (previously orphaned in `tokenURIs`)

## Storage Impact

### GRC721 standalone (`grc721_emit.txtar`)

| Operation | Before (B) | After (B) | Delta |
|-----------|-----------|----------|-------|
| Mint | 2,097 | 2,538 | +441 |
| **Approve** | 1,065 | **64** | **-1,001 (-94%)** |
| SetApprovalForAll | 1,074 | 1,073 | -1 |
| TransferFrom | 969 | 1,985 | +1,016 |
| Burn | -1,055 | -1,516 | -461 |
| **Lifecycle Total** | **4,150** | **4,144** | **-6** |

> Single-token lifecycle total is nearly unchanged — savings from Approve are redistributed to TransferFrom. The real benefit appears in multi-operation DeFi transactions.

### GnoSwap Position Lifecycle

| Operation | Before (B) | After (B) | Delta | % |
|-----------|-----------|----------|-------|---|
| CreatePool | 14,345 | 14,345 | 0 | 0% |
| **Mint #1 (wide)** | 32,008 | **31,470** | **-538** | **-1.7%** |
| **Mint #2 (narrow)** | 30,717 | **29,223** | **-1,494** | **-4.9%** |
| **Mint #3 (reuse ticks)** | 10,289 | **8,759** | **-1,530** | **-14.9%** |
| Swap | 6 | 6 | 0 | 0% |
| CollectFee | 2,216 | 2,216 | 0 | 0% |
| DecreaseLiquidity | 14 | 14 | 0 | 0% |

### Other tests (no change expected)

| Operation | Before (B) | After (B) | Delta |
|-----------|-----------|----------|-------|
| Mint (fee 3000) | 32,023 | 31,485 | -538 |
| Mint (fee 500) | 32,022 | 31,472 | -550 |
| IncreaseLiquidity (1st) | 61 | 49 | -12 |
| ExactInSwapRoute | 5,021 | 5,039 | +18 |
| SetPoolTier | 19,178 | 19,178 | 0 |
| Delegate / Undelegate | 17,871 / 915 | 17,871 / 915 | 0 |

### Storage fee (100 ugnot/byte)

| Operation | Saved |
|-----------|-------|
| Mint #1 (wide) | 0.054 gnot |
| Mint #2 (narrow) | 0.149 gnot |
| Mint #3 (reuse ticks) | 0.153 gnot |
| Approve GNFT | 0.100 gnot |

## Key Observations

1. **Consistent Mint savings**: -538 to -1,530 bytes across all Mint variants. Savings % increases when tick reuse makes gnft a larger share of total delta.
2. **Approve -94%**: Instead of creating a new `avl.Node` in `tokenApprovals`, the existing `tokens` node value is updated in-place.
3. **No impact on non-NFT operations**: Swap, CollectFee, Gov operations are unaffected.
4. **100% API compatible**: `IGRC721` interface unchanged. `basicNFT` is unexported.

## Test Plan

- [x] `gno test ./gno.land/p/demo/tokens/grc721/` — all 18 unit tests pass
- [x] `grc721_emit.txtar` — Mint/Approve/Transfer/Burn event verification pass
- [x] GnoSwap position lifecycle — all 10 txtar tests pass
- [ ] Verify existing consumers (`r/demo/foo721`, `r/matijamarjanovic/tokenhub`, `r/jjoptimist/eventix`, `r/demo/btree_dao`)
