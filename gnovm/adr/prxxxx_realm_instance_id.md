# Opaque Realm Instance IDs Reuse `Realm.Time`

## Context

GRC20 ledgers need a deterministic identifier before their first event.
Caller-managed IDs can collide, persisted object IDs arrive too late, and a
new realm counter would duplicate transaction-aware state already maintained
by the VM.

## Decision

Add one opaque interface:

```gno
chain.NewRealmInstanceID(_ int, rlm realm) string
```

The wrapper rejects non-current, subrealm, and ephemeral values. The native
independently requires a non-nil storage realm whose path is a persistent
`/r/` path, matches the validated package path, and derives to the same realm
package ID.

After validation, the native checks `Realm.Time != math.MaxUint64`, increments
the exact machine realm, immediately calls `SetPackageRealm`, and returns
`<realm-path>@<decimal-time>`. Object-ID finalization now performs the same
pre-mutation overflow check.

The value is opaque and chain-local. Gaps are allowed. Instance issuance and
object allocation intentionally share the hidden clock, and off-chain keys
must include chain ID.

Native CPU gas has a flat calibrated entry. Realm encoding and store writes
remain charged by the existing store meter. Activation requires validators to
run the same binary; no state migration is defined.

## Alternatives considered

- `Realm.InstanceSequence`: rejected because independence from allocation is
  not observable under the opaque, gap-tolerant contract.
- Caller-owned `seqid`: rejected because uniqueness depends on caller
  discipline.
- Registry or chain-global counters: rejected because they add participation
  requirements or a new state seam.
- Transaction hash plus ordinal: rejected because it requires execution
  identity plumbing.
- Persisted object ID: rejected because it is unavailable before immediate
  events.

## Consequences

The implementation adds no realm schema or protobuf field. Reservations commit
and roll back with the transaction store. Allocation changes may change exact
ID suffixes, so consumers must not assign them numeric meaning.

The GRC20-facing consequences are recorded in
`gno.land/adr/prxxxx_grc20_event_identity.md`.
