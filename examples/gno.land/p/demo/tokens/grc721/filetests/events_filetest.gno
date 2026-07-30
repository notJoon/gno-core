// PKGPATH: gno.land/r/demo/grc721events

package grc721events

import (
	"chain"

	"gno.land/p/demo/tokens/grc721"
)

func main(cur realm) {
	owner := chain.PackageAddress("owner")
	approved := chain.PackageAddress("approved")
	operator := chain.PackageAddress("operator")
	receiver := chain.PackageAddress("receiver")
	_, ledger := grc721.NewNFTWithRoyalty("Events", "EVT", cur)

	must(ledger.Mint(owner, "1"))
	must(ledger.SetTokenRoyalty("1", grc721.RoyaltyInfo{PaymentAddress: owner, BPS: 100}))
	must(ledger.SetTokenURI("1", "ipfs://one"))
	must(ledger.SetTokenMetadata("1", grc721.Metadata{Name: "One"}))
	must(ledger.Approve(owner, approved, "1"))
	must(ledger.SetApprovalForAll(owner, operator, true))
	must(ledger.TransferFrom(approved, owner, receiver, "1"))
	must(ledger.Burn("1"))
	if ledger.Mint(owner, "1") == nil {
		panic("duplicate mint succeeded")
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// Events:
// [
//   {
//     "type": "Mint",
//     "attrs": [
//       {
//         "key": "token",
//         "value": "gno.land/r/demo/grc721events.EVT"
//       },
//       {
//         "key": "to",
//         "value": "g19zen3q7mdt28xdf8lskh2ftk35yvcj8lvcka38"
//       },
//       {
//         "key": "tokenId",
//         "value": "1"
//       }
//     ],
//     "pkg_path": "gno.land/p/demo/tokens/grc721"
//   },
//   {
//     "type": "TokenUriUpdate",
//     "attrs": [
//       {
//         "key": "token",
//         "value": "gno.land/r/demo/grc721events.EVT"
//       },
//       {
//         "key": "tokenId",
//         "value": "1"
//       }
//     ],
//     "pkg_path": "gno.land/p/demo/tokens/grc721"
//   },
//   {
//     "type": "MetadataUpdate",
//     "attrs": [
//       {
//         "key": "token",
//         "value": "gno.land/r/demo/grc721events.EVT"
//       },
//       {
//         "key": "tokenId",
//         "value": "1"
//       }
//     ],
//     "pkg_path": "gno.land/p/demo/tokens/grc721"
//   },
//   {
//     "type": "Approval",
//     "attrs": [
//       {
//         "key": "token",
//         "value": "gno.land/r/demo/grc721events.EVT"
//       },
//       {
//         "key": "owner",
//         "value": "g19zen3q7mdt28xdf8lskh2ftk35yvcj8lvcka38"
//       },
//       {
//         "key": "to",
//         "value": "g1f79h8s85d0zhhkytgpqca0n4gdhn6m8953cmw7"
//       },
//       {
//         "key": "tokenId",
//         "value": "1"
//       }
//     ],
//     "pkg_path": "gno.land/p/demo/tokens/grc721"
//   },
//   {
//     "type": "ApprovalForAll",
//     "attrs": [
//       {
//         "key": "token",
//         "value": "gno.land/r/demo/grc721events.EVT"
//       },
//       {
//         "key": "owner",
//         "value": "g19zen3q7mdt28xdf8lskh2ftk35yvcj8lvcka38"
//       },
//       {
//         "key": "to",
//         "value": "g1vs5676y7mrwga8y4euczupennv27czrzasuhxn"
//       },
//       {
//         "key": "approved",
//         "value": "true"
//       }
//     ],
//     "pkg_path": "gno.land/p/demo/tokens/grc721"
//   },
//   {
//     "type": "Transfer",
//     "attrs": [
//       {
//         "key": "token",
//         "value": "gno.land/r/demo/grc721events.EVT"
//       },
//       {
//         "key": "from",
//         "value": "g19zen3q7mdt28xdf8lskh2ftk35yvcj8lvcka38"
//       },
//       {
//         "key": "to",
//         "value": "g104vs83fccmwfgxx7teta8em5d36nlkedama8xv"
//       },
//       {
//         "key": "tokenId",
//         "value": "1"
//       }
//     ],
//     "pkg_path": "gno.land/p/demo/tokens/grc721"
//   },
//   {
//     "type": "Burn",
//     "attrs": [
//       {
//         "key": "token",
//         "value": "gno.land/r/demo/grc721events.EVT"
//       },
//       {
//         "key": "from",
//         "value": "g104vs83fccmwfgxx7teta8em5d36nlkedama8xv"
//       },
//       {
//         "key": "tokenId",
//         "value": "1"
//       }
//     ],
//     "pkg_path": "gno.land/p/demo/tokens/grc721"
//   }
// ]
