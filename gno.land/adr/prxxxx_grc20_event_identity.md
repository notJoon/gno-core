# Opaque GRC20 Event Identity

## Context

Two independent GRC20 ledgers created by one realm can share a symbol and a
caller-provided ID. Their state remains separate, but their event `token`
attributes collide.

## Decision

`grc20.NewToken` now accepts `(name, symbol, decimals, rlm)` and reserves an
opaque ID from `chain.NewRealmInstanceID` after validating realm and metadata.
`Token` stores immutable `id` and `originRealm` strings.

The existing `Token.ledger` and `PrivateLedger.token` back-pointers form the
canonical ledger seam. Every mutation, event, and teller-construction path
validates and routes through that pair. Shallow private-ledger copies therefore
share balances, allowances, total supply, and event identity without a third
ledger-core object.

`grc20reg.Register` verifies `cur.IsCurrent()` before `cur.Previous()` and
compares the registering realm with structured `Token.OriginRealm()` data. The
registry remains optional and retains one locator per `(realm, symbol)`;
mint-before-register remains valid. Registry listing hardening is separate.

Events retain their types and attribute keys. Consumers treat token IDs as
opaque, allow gaps, and key off-chain records by `(chain-id, token-id)`.
Issuance and VM object allocation share a hidden clock.

## Alternatives considered

- A new shared ledger-core object: rejected because existing back-pointers
  preserve copy safety with less state and fewer files.
- Parsing the opaque ID in the registry: rejected in favor of
  `OriginRealm()`.
- Registry-issued identity: rejected because unregistered tokens and
  mint-before-register remain supported.
- A compatibility constructor with caller-owned IDs: rejected for new-chain
  activation because it preserves the collision-prone interface.
- Registry pagination and `GetRegistry` changes: deferred as an independent
  security change.

## Consequences

Constructor callers must drop local token sequence state. Exact event ID
suffixes may change when object allocation changes, but replay remains
deterministic. Malformed zero, nil, or mismatched handles fail closed.

The VM issuance decision is recorded in
`gnovm/adr/prxxxx_realm_instance_id.md`.
