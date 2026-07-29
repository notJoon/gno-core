# GRC20 Event Token Identifier Specification

## Status

Accepted implementation specification for new-chain activation.

## Contract

Every `grc20.NewToken(name, symbol, decimals, rlm)` call reserves an opaque,
chain-local identifier before returning:

```text
<origin-realm>@<decimal-realm-time>
```

The encoding is an implementation detail. Callers and event consumers must:

- treat the complete value as opaque;
- use `(chain-id, token-id)` as the off-chain key;
- read the origin through `Token.OriginRealm()`, never by parsing the ID;
- assume gaps are allowed and infer no count or ordering from the suffix.

Allocation and issuance share the hidden `gnolang.Realm.Time` clock. Object
finalization may therefore create gaps between token IDs, while a reservation
may advance later object IDs.

## Issuance

`chain.NewRealmInstanceID(_ int, rlm realm) string` is the only Gno interface.
It accepts only the live primary persistent realm whose package path matches the
machine storage realm. Stale, spoofed, subrealm, ephemeral, nil, non-`/r/`, and
storage-mismatched contexts panic before mutation.

The native validates the storage realm independently, checks
`math.MaxUint64` before mutation, increments the exact `m.Realm.Time`, and
immediately persists it through the transaction store. Failed transactions
roll the reservation back; successful unused reservations may leave gaps.
`Realm.Time` and its numeric value are not exposed to Gno callers.

Object-ID finalization performs the same pre-mutation overflow check.

## GRC20

`NewToken` validates the live realm and all metadata before reserving an ID. It
stores immutable `id` and `originRealm` strings on `Token`, then links the
existing `Token.ledger` and `PrivateLedger.token` back-pointers.

Every mutation, event, and teller-construction path validates those
back-pointers and routes through `token.ledger`. A shallow `PrivateLedger` copy
therefore shares canonical balances, allowances, total supply, and event ID.
Zero, nil, or mismatched handles panic deterministically.

`Transfer` and `Approval` events retain their existing shape and use
`Token.ID()` for the `token` attribute. Mint-before-register remains valid.

## Registry

`grc20reg.Register` checks `cur.IsCurrent()` before `cur.Previous()`, compares
the registering realm with `Token.OriginRealm()`, and never parses `Token.ID()`.
The existing one-token-per-`(realm, symbol)` locator policy remains unchanged.
Registry listing and pagination are outside this change.

## Non-goals

There is no `Realm.InstanceSequence`, public numeric getter, chain-global
counter, transaction-hash issuer, early object finalization, new ledger core,
constructor compatibility wrapper, or registry listing refactor.
