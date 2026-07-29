# GRC20 Event Token Identifier Design Alternatives

## Status

- Status: historical design exploration; non-normative
- Related specification: `docs/grc20-event-token-identifier-spec.md`
- Scope: distinguishing independent GRC20 ledgers in events when they share an
  origin realm and token symbol

This document records the evaluation of whether a separate VM-managed
`Realm.InstanceSequence` was the smallest appropriate solution. The related
implementation specification is normative.

## Conclusion

The superseded proposal combined three independent concerns:

1. issuing a unique event identifier;
2. preventing copied `PrivateLedger` handles from forking state;
3. replacing the callback-bearing registry listing interface.

They should be designed and reviewed separately.

For the full requirement set—unique identifiers for unregistered ledgers,
available before the first event—the recommended design is:

- do not add `Realm.InstanceSequence`;
- expose one opaque issuance interface:

  ```gno
  package chain

  func NewRealmInstanceID(_ int, rlm realm) string
  ```

- implement it by reserving a value from the existing `Realm.Time` counter;
- keep `Realm.Time`, sequence encoding, persistence, and realm validation hidden
  behind the interface;
- keep the existing `Token` and `PrivateLedger` shape, unless handle-copy
  semantics are independently required.

The separate `InstanceSequence` field remains justified only if independence
from object allocation is a concrete compatibility requirement. The current
specification declares the identifier opaque, permits gaps, forbids count and
ordering semantics, and assumes a new genesis. Under those constraints,
allocation independence does not provide observable correctness.

## Why Some State Is Unavoidable

`grc20` is a pure `/p/` package and cannot own persistent package state. Given
the same origin realm, symbol, and caller-provided ID, a stateless constructor
cannot produce a provably unique deterministic result.

Therefore, strict uniqueness requires one of:

- caller-owned persistent state;
- registry-owned persistent state;
- VM or SDK-owned persistent state;
- a unique transaction identity plus a deterministic per-execution ordinal.

The design question is where that state belongs, not whether it can be removed.

## Recommended Module

### Interface

```gno
package chain

// NewRealmInstanceID returns an opaque, realm-owned, chain-local identifier.
func NewRealmInstanceID(_ int, rlm realm) string
```

The leading argument keeps the `realm` parameter out of first position so the
wrapper remains non-crossing.

The interface contract is:

- only the current primary persistent realm may issue an ID;
- stale, spoofed, subrealm, ephemeral, and storage-mismatched contexts fail
  before mutation;
- the returned string is opaque;
- two successful issuances from one realm return different strings;
- a failed transaction rolls back the reservation;
- successful unused reservations may create gaps;
- overflow panics deterministically before mutation;
- off-chain consumers use `(chain-id, instance-id)` as their primary key.

Callers do not observe `Realm.Time`, a numeric sequence, or persistence
details.

### Hidden Implementation

Illustrative native logic:

```go
func X_newRealmInstanceID(m *gnolang.Machine, pkgPath string) string {
    rlm := m.Realm
    if rlm == nil ||
        rlm.Path != pkgPath ||
        !gnolang.IsRealmPath(rlm.Path) ||
        rlm.ID != gnolang.PkgIDFromPkgPath(rlm.Path) ||
        !rlm.ID.IsRealmPkg() {
        m.PanicString("invalid instance ID realm")
    }
    if rlm.Time == math.MaxUint64 {
        m.PanicString("realm instance ID exhausted")
    }

    rlm.Time++
    m.Store.SetPackageRealm(rlm)
    return pkgPath + "@" + strconv.FormatUint(rlm.Time, 10)
}
```

The Gno wrapper retains the `rlm.IsCurrent()`, subrealm, and ephemeral checks
from the current proposal. The native independently validates the machine
storage realm.

The existing object-ID allocation path must also reject `Realm.Time` overflow
before incrementing it. It currently increments `targetRlm.Time` directly.

### Construction Order

```gno
func NewToken(
    name string,
    symbol string,
    decimals int,
    rlm realm,
) (*Token, *PrivateLedger) {
    // Verify realm and validate metadata first.
    id := chain.NewRealmInstanceID(0, rlm)

    ledger := &PrivateLedger{}
    token := &Token{
        id:          id,
        originRealm: rlm.PkgPath(),
        name:        name,
        symbol:      symbol,
        decimals:    decimals,
        ledger:      ledger,
    }
    ledger.token = token
    return token, ledger
}
```

Invalid metadata must fail before issuance. The constructor may emit through
the returned ledger immediately because the ID is final before allocation
returns.

## Consequences of Reusing `Realm.Time`

Benefits:

- no new `Realm` field;
- no realm protobuf or generated serialization change;
- no separate counter persistence path;
- no transaction-hash or transaction-index plumbing;
- existing transaction-store rollback semantics are reused;
- the public interface is one opaque operation rather than a counter getter.

Costs:

- an instance reservation advances subsequent object IDs;
- object allocation creates gaps between instance IDs;
- object IDs and instance IDs share the same exhaustion limit;
- exact instance numbers may change when allocation behavior changes.

These costs are acceptable only while instance IDs remain opaque. Consumers
must not infer token counts, object counts, ordering, or stable exact sequence
values.

Regression tests should assert uniqueness and canonical encoding. They should
not generally assert consecutive values such as `@1` and `@2` unless the
fixture controls all preceding allocations.

## Ledger Handle Copy Semantics

Unique issuance and ledger copy behavior are separate concerns.

If copied `PrivateLedger` handles must remain usable and share state, the
existing `Token.ledger` and `PrivateLedger.token` back-pointers can identify the
canonical ledger without adding a third shared core object:

```gno
func (led *PrivateLedger) canonical() *PrivateLedger {
    if led == nil ||
        led.token == nil ||
        led.token.ledger == nil ||
        led.token.ledger.token != led.token {
        panic("grc20: invalid canonical ledger")
    }
    return led.token.ledger
}
```

Each mutation and event path begins with:

```gno
led = led.canonical()
```

A shallow copy retains the original token pointer, and that token points back
to the original ledger. Mutations through the copy therefore route to the
canonical state and emit its ID.

If copied private-ledger handles do not need to remain usable, the smaller
contract is to reject `led.token.ledger != led` and fail closed.

The separate shared `ledger` core is warranted only if measurements or other
invariants show that the existing back-pointer seam is insufficient.

## Registry Scope

Replacing `GetRegistry()` with a bounded, value-only listing interface addresses
a real callback/realm-authority concern, but it is unrelated to identifier
issuance. It should be retained as a separate security change so its behavior,
callers, and tests can be reviewed independently.

The registry also provides a smaller alternative identity model:

- registration becomes mandatory before event-producing mutations;
- the registry rejects duplicate canonical identifiers;
- consumers ignore events before successful registration.

This avoids all VM changes, but it intentionally drops support for canonical
unregistered ledgers and mint-before-register. It is the preferred design if
those two requirements can be removed.

## Alternatives

| Design | Strict uniqueness | First immediate event | Main cost | Assessment |
|---|---:|---:|---|---|
| Caller-owned `seqid.ID` | No; depends on caller discipline | Yes | None in VM | Keep only if realm authors own the risk |
| Mandatory registry | Registered tokens only | Only after registration | Workflow change | Smallest narrowed design |
| Existing `Realm.Time` | Yes | Yes | Couples instance and object clocks | Recommended full design |
| New `Realm.InstanceSequence` | Yes | Yes | Realm schema, protobuf, native, gas | Use only for proven clock independence |
| Chain-global KV counter | Yes | Yes | New store key and persistence seam | No advantage over existing state |
| Transaction hash plus ordinal | Collision-resistant | Yes in transaction paths | BaseApp-to-VM identity plumbing; genesis rules | More complex than it first appears |
| Persisted object ID | Yes after finalization | No | Early finalization or deferred events | Reject |
| Event occurrence coordinates | Per-event only | Yes | No stable cross-event ledger identity | Reject |

### Transaction Identity

The SDK context has transaction bytes, but the VM execution context currently
does not carry a transaction hash or transaction index. A transaction-based
issuer would need:

- transaction identity plumbing into every VM execution path;
- one ordinal shared across messages and machines;
- deterministic genesis and non-transaction execution identities;
- a cryptographic collision assumption.

It removes a persistent counter write but does not reduce the overall
interface or implementation.

### Chain-Global Counter

A single transactional KV counter avoids changing `Realm`, but creates a new
state namespace and persistence module. Since `Realm.Time` already provides
monotonic persisted state with realm locality, a new global counter adds
implementation without additional correctness.

## Decision Rule

Use the following order:

1. If only registered tokens are canonical, make registration mandatory and
   avoid VM changes.
2. If unregistered ledgers and first-event identity are required, use the
   opaque `NewRealmInstanceID` interface backed by `Realm.Time`.
3. Add an independent `Realm.InstanceSequence` only if a concrete consumer
   requires IDs to remain unaffected by object allocation behavior.

## Suggested Specification Changes

If the recommended design is accepted:

- replace the `Realm Instance Sequence Module` section with an opaque realm
  instance-ID module;
- remove `InstanceSequence` from the `Realm` data model and protobuf;
- state explicitly that issuance and object allocation share a hidden clock;
- replace exact consecutive-ID acceptance criteria with uniqueness and format
  criteria;
- restore the existing token/ledger layout and add canonical back-pointer
  routing if copied handles must remain usable;
- move registry listing hardening into a separate security specification or
  change set;
- retain the new-genesis activation assumption.

## Verification

The minimum behavioral checks are:

- two same-realm, same-symbol tokens created in one execution receive distinct
  IDs and emit distinguishable events;
- allocation before, between, and after issuance preserves uniqueness;
- a failed transaction rolls back the reserved value;
- commit and store reopen preserve monotonicity;
- invalid realm contexts do not mutate `Realm.Time`;
- overflow fails before mutation in both issuance and object allocation;
- copied ledger handles either route to canonical state or fail closed,
  according to the chosen contract;
- registry behavior is tested independently from issuance.

Because this design changes allocation-adjacent VM state and native gas, the
repository-required verification includes:

```sh
go test ./gno.land/pkg/sdk/vm/ -run Gas
go test ./gno.land/pkg/integration/ -run TestTestdata
go test ./gnovm/pkg/gnolang/ -run Files -test.short
```

Run the affected GRC20 and native tests as well, then run `/simplify` before
presenting an implementation as complete.
