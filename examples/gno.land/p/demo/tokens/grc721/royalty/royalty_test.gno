package royalty

import (
	"testing"

	"gno.land/p/demo/tokens/grc721"
	"gno.land/p/nt/testutils/v0"
	"gno.land/p/nt/uassert/v0"
)

const (
	collectionName   = "RoyaltyNFT"
	collectionSymbol = "RNFT"
)

func TestNewRoyaltyNFT(t *testing.T) {
	nft, ledger, rLedger := NewRoyaltyNFT(collectionName, collectionSymbol, 1000)
	uassert.True(t, nft != nil, "nft should not be nil")
	uassert.True(t, ledger != nil, "core ledger should not be nil")
	uassert.True(t, rLedger != nil, "royalty ledger should not be nil")
	uassert.Equal(t, int64(1000), rLedger.MaxBps())
}

func TestNewRoyaltyNFT_DefaultMaxBps(t *testing.T) {
	_, _, rLedger := NewRoyaltyNFT(collectionName, collectionSymbol, 0)
	uassert.Equal(t, int64(MaxBps), rLedger.MaxBps())
}

func TestPrivateLedger_SetTokenRoyalty_Admin(t *testing.T) {
	nft, ledger, rLedger := NewRoyaltyNFT(collectionName, collectionSymbol, 1000)
	owner := testutils.TestAddress("owner")
	payee := testutils.TestAddress("payee")

	uassert.NoError(t, ledger.Mint(owner, grc721.TokenID("1")))
	uassert.NoError(t, rLedger.SetTokenRoyalty(grc721.TokenID("1"), RoyaltyInfo{
		PaymentAddress: payee,
		Bps:            500, // 5%
	}))

	addr, amount, err := nft.RoyaltyInfo(grc721.TokenID("1"), 10000)
	uassert.NoError(t, err)
	uassert.Equal(t, payee.String(), addr.String())
	uassert.Equal(t, int64(500), amount) // 10000 * 500 / 10000 = 500
}

func TestPrivateLedger_SetTokenRoyalty_BpsOutOfRange(t *testing.T) {
	_, ledger, rLedger := NewRoyaltyNFT(collectionName, collectionSymbol, 1000)
	owner := testutils.TestAddress("owner")
	payee := testutils.TestAddress("payee")

	uassert.NoError(t, ledger.Mint(owner, grc721.TokenID("1")))

	err := rLedger.SetTokenRoyalty(grc721.TokenID("1"), RoyaltyInfo{
		PaymentAddress: payee,
		Bps:            5000, // exceeds maxBps=1000
	})
	uassert.ErrorIs(t, err, ErrInvalidRoyaltyBps)
}

func TestPrivateLedger_SetTokenRoyalty_InvalidPayee(t *testing.T) {
	_, ledger, rLedger := NewRoyaltyNFT(collectionName, collectionSymbol, 1000)
	owner := testutils.TestAddress("owner")

	uassert.NoError(t, ledger.Mint(owner, grc721.TokenID("1")))

	err := rLedger.SetTokenRoyalty(grc721.TokenID("1"), RoyaltyInfo{
		PaymentAddress: address(""),
		Bps:            500,
	})
	uassert.ErrorIs(t, err, ErrInvalidRoyaltyPaymentAddress)
}

func TestPrivateLedger_SetTokenRoyalty_MissingToken(t *testing.T) {
	_, _, rLedger := NewRoyaltyNFT(collectionName, collectionSymbol, 1000)
	payee := testutils.TestAddress("payee")

	err := rLedger.SetTokenRoyalty(grc721.TokenID("ghost"), RoyaltyInfo{
		PaymentAddress: payee,
		Bps:            500,
	})
	uassert.ErrorIs(t, err, grc721.ErrInvalidTokenId)
}

func TestRoyaltyInfo_FractionalPercent(t *testing.T) {
	// EIP-2981 bps lets us represent fractional rates the legacy
	// percentage-based ledger could not. Verify 2.5% (250 bps).
	_, ledger, rLedger := NewRoyaltyNFT(collectionName, collectionSymbol, 1000)
	owner := testutils.TestAddress("owner")
	payee := testutils.TestAddress("payee")

	uassert.NoError(t, ledger.Mint(owner, grc721.TokenID("1")))
	uassert.NoError(t, rLedger.SetTokenRoyalty(grc721.TokenID("1"), RoyaltyInfo{
		PaymentAddress: payee,
		Bps:            250,
	}))

	_, amount, err := rLedger.rNFT.RoyaltyInfo(grc721.TokenID("1"), 100_000)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(2500), amount) // 100000 * 250 / 10000 = 2500
}

func TestCallerTeller_SetTokenRoyalty(t *testing.T) {
	nft, ledger, _ := NewRoyaltyNFT(collectionName, collectionSymbol, 1000)
	alice := testutils.TestAddress("alice")
	payee := testutils.TestAddress("payee")

	uassert.NoError(t, ledger.Mint(alice, grc721.TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(alice))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		uassert.NoError(t, teller.SetTokenRoyalty(grc721.TokenID("1"), RoyaltyInfo{
			PaymentAddress: payee,
			Bps:            300,
		}))
	}()

	_, amount, err := nft.RoyaltyInfo(grc721.TokenID("1"), 10000)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(300), amount)
}

func TestCallerTeller_SetTokenRoyalty_NotOwner(t *testing.T) {
	nft, ledger, _ := NewRoyaltyNFT(collectionName, collectionSymbol, 1000)
	alice := testutils.TestAddress("alice")
	eve := testutils.TestAddress("eve")
	payee := testutils.TestAddress("payee")

	uassert.NoError(t, ledger.Mint(alice, grc721.TokenID("1")))

	teller := nft.CallerTeller()

	testing.SetRealm(testing.NewUserRealm(eve))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		err := teller.SetTokenRoyalty(grc721.TokenID("1"), RoyaltyInfo{
			PaymentAddress: payee,
			Bps:            300,
		})
		uassert.ErrorIs(t, err, grc721.ErrCallerIsNotOwner)
	}()
}

func TestReadonlyTeller_SetTokenRoyalty(t *testing.T) {
	nft, ledger, _ := NewRoyaltyNFT(collectionName, collectionSymbol, 1000)
	alice := testutils.TestAddress("alice")
	payee := testutils.TestAddress("payee")

	uassert.NoError(t, ledger.Mint(alice, grc721.TokenID("1")))

	teller := nft.ReadonlyTeller()
	err := teller.SetTokenRoyalty(grc721.TokenID("1"), RoyaltyInfo{
		PaymentAddress: payee,
		Bps:            300,
	})
	uassert.ErrorIs(t, err, grc721.ErrReadonly)
}

func TestImpersonateTeller_SetTokenRoyalty(t *testing.T) {
	nft, ledger, rLedger := NewRoyaltyNFT(collectionName, collectionSymbol, 1000)
	alice := testutils.TestAddress("alice")
	payee := testutils.TestAddress("payee")

	uassert.NoError(t, ledger.Mint(alice, grc721.TokenID("1")))

	imp := rLedger.ImpersonateTeller(alice)
	uassert.NoError(t, imp.SetTokenRoyalty(grc721.TokenID("1"), RoyaltyInfo{
		PaymentAddress: payee,
		Bps:            400,
	}))

	_, amount, err := nft.RoyaltyInfo(grc721.TokenID("1"), 10000)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(400), amount)
}

func TestTeller_EmbeddedCoreMethods(t *testing.T) {
	// Royalty Teller embeds grc721.Teller; verify core methods work.
	nft, ledger, _ := NewRoyaltyNFT(collectionName, collectionSymbol, 1000)
	alice := testutils.TestAddress("alice")
	bob := testutils.TestAddress("bob")

	uassert.NoError(t, ledger.Mint(alice, grc721.TokenID("1")))

	teller := nft.CallerTeller()

	uassert.Equal(t, collectionName, teller.Name())
	uassert.Equal(t, collectionSymbol, teller.Symbol())

	bal, err := teller.BalanceOf(alice)
	uassert.NoError(t, err)
	uassert.Equal(t, int64(1), bal)

	testing.SetRealm(testing.NewUserRealm(alice))
	func() {
		testing.SetRealm(testing.NewCodeRealm("gno.land/r/test/test"))
		uassert.NoError(t, teller.TransferFrom(alice, bob, grc721.TokenID("1")))
	}()

	owner, _ := nft.OwnerOf(grc721.TokenID("1"))
	uassert.Equal(t, bob.String(), owner.String())
}
