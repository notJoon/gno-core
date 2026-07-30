package grc20factory

import (
	"strings"
	"testing"

	"gno.land/p/demo/tokens/grc20"
	"gno.land/p/nt/testutils/v0"
	"gno.land/p/nt/uassert/v0"
	"gno.land/p/nt/ufmt/v0"
)

func callNewToken(cur realm, name, symbol string, decimals int) (token *grc20.Token, got any) {
	defer func() {
		if err := recover(); err != nil {
			got = err
		}
	}()
	token, _ = grc20.NewToken(name, symbol, decimals, cur)
	return
}

func TestNewTokenThroughWrapper(cur realm, t *testing.T) {
	tests := []struct {
		name     string
		token    string
		symbol   string
		decimals int
		want     any
	}{
		{name: "valid", token: "Same", symbol: "DUP", decimals: 6},
		{name: "duplicate symbol gets unique ID", token: "Same", symbol: "DUP", decimals: 6},
		{name: "UTF-8 name", token: "Доллар", symbol: "RUB", decimals: 2},
		{name: "maximum metadata", token: strings.Repeat("a", grc20.MaxNameLen), symbol: strings.Repeat("A", grc20.MaxSymbolLen), decimals: grc20.MaxDecimals},
		{name: "empty name", symbol: "OK", decimals: 4, want: grc20.ErrInvalidName},
		{name: "invalid symbol", token: "Name", symbol: "BA.D", decimals: 4, want: grc20.ErrInvalidSymbol},
		{name: "negative decimals", token: "Name", symbol: "OK", decimals: -1, want: grc20.ErrInvalidDecimals},
		{name: "decimals over cap", token: "Name", symbol: "OK", decimals: grc20.MaxDecimals + 1, want: grc20.ErrInvalidDecimals},
	}

	ids := make(map[string]bool)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, got := callNewToken(cur, tc.token, tc.symbol, tc.decimals)
			if got != tc.want {
				t.Fatalf("expected panic %v, got %v", tc.want, got)
			}
			if tc.want != nil {
				return
			}

			uassert.Equal(t, "gno.land/r/demo/defi/grc20factory", token.OriginRealm())
			uassert.True(t, strings.HasPrefix(token.ID(), token.OriginRealm()+"@"))
			uassert.False(t, ids[token.ID()], "token ID must be unique")
			ids[token.ID()] = true
		})
	}
}

func TestReadOnlyPublicMethods(cur realm, t *testing.T) {
	admin := testutils.TestAddress("admin")
	bob := testutils.TestAddress("bob")
	carl := testutils.TestAddress("carl")

	type test struct {
		name    string
		balance int64
		fn      func() int64
	}

	checkBalances := func(step string, totSup, balAdm, balBob, allowAdmBob, balCarl int64) {
		tests := []test{
			{"TotalSupply", totSup, func() int64 { return TotalSupply("FOO") }},
			{"BalanceOf(admin)", balAdm, func() int64 { return BalanceOf("FOO", admin) }},
			{"BalanceOf(bob)", balBob, func() int64 { return BalanceOf("FOO", bob) }},
			{"Allowance(admin, bob)", allowAdmBob, func() int64 { return Allowance("FOO", admin, bob) }},
			{"BalanceOf(carl)", balCarl, func() int64 { return BalanceOf("FOO", carl) }},
		}
		for _, tc := range tests {
			reason := ufmt.Sprintf("%s.%s - %s", step, tc.name, "balances do not match")
			uassert.Equal(t, tc.balance, tc.fn(), reason)
		}
	}

	// admin creates FOO and BAR.
	testing.SetOriginCaller(admin)
	NewWithAdmin(cross(cur), "Foo", "FOO", 3, 1_111_111_000, 5_555, admin)
	NewWithAdmin(cross(cur), "Bar", "BAR", 3, 2_222_000, 6_666, admin)
	foo := Bank("FOO")
	bar := Bank("BAR")
	fooID := foo.ID()
	barID := bar.ID()
	uassert.True(t, fooID != "")
	uassert.True(t, barID != "")
	uassert.NotEqual(t, fooID, barID)
	uassert.Equal(t, "gno.land/r/demo/defi/grc20factory", foo.OriginRealm())
	uassert.Equal(t, "gno.land/r/demo/defi/grc20factory", bar.OriginRealm())
	checkBalances("step1", 1_111_111_000, 1_111_111_000, 0, 0, 0)

	// admin mints to bob.
	mustGetInstance("FOO").ledger.Mint(bob, 333_333_000)
	checkBalances("step2", 1_444_444_000, 1_111_111_000, 333_333_000, 0, 0)

	// carl uses the faucet.
	testing.SetOriginCaller(carl)
	Faucet(cross(cur), "FOO")
	checkBalances("step3", 1_444_449_555, 1_111_111_000, 333_333_000, 0, 5_555)

	// admin gives to bob some allowance.
	testing.SetOriginCaller(admin)
	Approve(cross(cur), "FOO", bob, 1_000_000)
	checkBalances("step4", 1_444_449_555, 1_111_111_000, 333_333_000, 1_000_000, 5_555)

	// bob uses a part of the allowance.
	testing.SetOriginCaller(bob)
	TransferFrom(cross(cur), "FOO", admin, carl, 400_000)
	checkBalances("step5", 1_444_449_555, 1_110_711_000, 333_333_000, 600_000, 405_555)

	// bob uses a part of the allowance.
	testing.SetOriginCaller(bob)
	TransferFrom(cross(cur), "FOO", admin, carl, 600_000)
	checkBalances("step6", 1_444_449_555, 1_110_111_000, 333_333_000, 0, 1_005_555)
}
