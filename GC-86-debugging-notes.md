# GC-86 Gas Simulation Debugging Notes

## Issue

- Jira: https://onbloc.atlassian.net/browse/GC-86
- Title: Gas Simulation 디버깅
- Project: Gno Core
- Status: In Progress
- Priority: Medium
- Assignee: Lee ByeongJun
- Reporter: Andrew Kang

## Problem Summary

When Adena simulates a GnoSwap `Swap` transaction, the chain sometimes returns a `GasUsed` value that appears lower than the gas actually needed.

The response itself is not an error. Events may also be returned, so the response can look like a normal successful execution. Because of this, the wallet cannot reliably tell whether the returned simulation data is correct or underestimated.

Current wallet-side mitigation:

- Refetch/re-simulate the transaction.
- Compare the current `GasUsed` with the newly returned `GasUsed`.
- Use the higher value.

This is a workaround. The core debugging target is why simulation can return a lower `GasUsed` and still look successful.

## Reported Reproduction Flow

1. Open `beta.gnoswap.io/swap`.
2. Select swap token and amount.
3. Open the Adena injection popup.
4. Wait while Network Fee is periodically refetched.
5. Occasionally the Network Fee is calculated too low.
6. Approving in that state can fail with Out of Gas.

## Known Failing Transaction

- RPC: `https://test13.rpc.onbloc.xyz:443`
- Chain ID: `test-13`
- Tx hash, base64: `/6Axcihq4VHsynXVRxvogdrF8geCQIvLD5WY30KAoig=`
- Tx hash, hex: `ffa03172286ae151ecca75d5471be881dac5f20782408bcb0f9598df4280a228`
- Gnoscan: `https://gnoscan.io/transactions/details?txhash=/6Axcihq4VHsynXVRxvogdrF8geCQIvLD5WY30KAoig=`
- RPC tx query: `https://test13.rpc.onbloc.xyz:443/tx?hash=0xffa03172286ae151ecca75d5471be881dac5f20782408bcb0f9598df4280a228`

RPC `/tx` result:

```text
height: 688225
index: 0
GasWanted: 230372626
GasUsed: 230396157
Error: /std.OutOfGasError
Log: out of gas, gasWanted: 230372626, gasUsed: 230396157 location: WriteFlat
```

Raw tx from RPC:

```text
CpMBCgovdm0ubV9jYWxsEoQBCihnMTdtOGhsdm0zazBhZ25nejh2dzI5ZXRwY2pkMnl2Y2VsNnB2dDNrIhlnbm8ubGFuZC9yL2dub2xhbmQvd3Vnbm90KgdBcHByb3ZlMihnMXZjODgzZ3NodTV6N3l0azVjZHluaGM4YzJkaDY3cGRwNGNzemtwMgoxMjM0MDAwMDAwCm0KCi92bS5tX2NhbGwSXwooZzE3bThobHZtM2swYWduZ3o4dncyOWV0cGNqZDJ5dmNlbDZwdnQzaxIPMTIzNDAwMDAwMHVnbm90Ihlnbm8ubGFuZC9yL2dub2xhbmQvd3Vnbm90KgdEZXBvc2l0CvsBCgovdm0ubV9jYWxsEuwBCihnMTdtOGhsdm0zazBhZ25nejh2dzI5ZXRwY2pkMnl2Y2VsNnB2dDNrIhlnbm8ubGFuZC9yL2dub3N3YXAvcm91dGVyKhBFeGFjdEluU3dhcFJvdXRlMhlnbm8ubGFuZC9yL2dub2xhbmQvd3Vnbm90MhZnbm8ubGFuZC9yL2dub3N3YXAvZ25zMgoxMjM0MDAwMDAwMjRnbm8ubGFuZC9yL2dub2xhbmQvd3Vnbm90Omduby5sYW5kL3IvZ25vc3dhcC9nbnM6MTAwMgMxMDAyCzE3Mzk3MTk3NjA1MgoxNzgzMzk4ODEzMgAKiQEKCi92bS5tX2NhbGwSewooZzE3bThobHZtM2swYWduZ3o4dncyOWV0cGNqZDJ5dmNlbDZwdnQzayIZZ25vLmxhbmQvci9nbm9sYW5kL3d1Z25vdCoHQXBwcm92ZTIoZzF2Yzg4M2dzaHU1ejd5dGs1Y2R5bmhjOGMyZGg2N3BkcDRjc3prcDIBMBIUCKTU2dsBEgwyMzAzNzI3dWdub3Qafgo6ChMvdG0uUHViS2V5U2VjcDI1NmsxEiMKIQOoJWxOiINEf2+rUXOPvk8gNO5TVpwI7F0C9nQVX4OnxBJA7upVGywubwvUNdJdWPR2rgXWWI9j0QiCMHznpbP/jAI/NeF1FyCCsKFav+nn4qbTuvgku8n94wr06aOdq33BUSIbRXhlY3V0ZWQgdGhyb3VnaCBnbm9zd2FwLmlv
```

## Prior Simulation Reproduction From Jira Comment

Different transaction tested on `test13`:

- Tx hash: `ytM66+6fp6nUVYPDeNXQLANPSjGlGk1+2hZDA2rCeIQ=`
- Original DeliverTx height: `693902`
- Original DeliverTx `GasWanted`: `203090159`
- Original DeliverTx `GasUsed`: `185374778`
- Original DeliverTx error: `null`

Repeated simulation with the same raw tx returned different `GasUsed` values:

```text
1: 185058668
2: 185058668
3: 185058668
4: 185458465
5: 184654593
```

This proves `GasUsed` can vary across repeated simulation calls against the active RPC. It does not yet prove simulation is nondeterministic, because state and block height were not fixed.

## Important Unknowns

- Does the issue reproduce with the same DB snapshot and same app state?
- Does the issue reproduce at a fixed block height?
- Are repeated simulate responses coming from the same RPC node or different nodes?
- Do low-gas and high-gas simulate responses have different events?
- Does the underestimated simulation stop before a write path such as `WriteFlat`?

## Minimum Next Debug Data

For the failing tx above, collect repeated `.app/simulate` responses using the same raw tx:

```text
chain_id:
rpc:
tx_hash:
raw_tx_base64:

deliver_tx:
  height:
  gas_wanted:
  gas_used:
  events_count:
  error:
  log:

simulate_runs:
  - time:
    node:
    height:
    gas_used:
    events_count:
    error:
    log:
    raw_response_file:
```

The highest-value artifact is a diff between a low-`GasUsed` simulate response and a high-`GasUsed` simulate response for the same raw tx.
