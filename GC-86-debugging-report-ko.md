# GC-86 Gas Simulation 디버깅 보고서

## 요약

GnoSwap에서 Network Fee가 낮아진 순간 swap을 승인하면, simulation은 성공하고 낮은 `GasUsed`를 반환하지만 실제 DeliverTx가 더 많은 gas를 사용해 OutOfGas가 발생하는 현상을 재현했다.

현재까지의 증거는 결정론적 VM 랜덤성이나 특정 RPC backend 오류보다는 **simulation 시점과 DeliverTx 시점의 block/state 변화**를 가리킨다.

다만 test13의 동일 DB snapshot을 확보하지 못했으므로, 특정 historical transaction을 동일 state에서 재실행해 deterministic VM bug를 완전히 배제하지는 못했다.

## 최신 재현 트랜잭션

Gnoscan:

`https://gnoscan.io/transactions/details?txhash=KXamck+sj8VYi4MIEXO4Qvplk0J/IhA2m9chs3deq6U=&chainId=test-13`

Tx hash:

`2976a6724fac8fc5588b83081173b842fa6593427f2210369bd721b3775eaba5`

DeliverTx 결과:

| 항목 | 값 |
| --- | ---: |
| height/index | `751122/0` |
| GasWanted | `202,537,218` |
| GasUsed | `202,543,121` |
| 초과분 | `5,903` |
| Error | `/std.OutOfGasError` |
| 위치 | `ReadFlat` |
| Events | `0` |

Raw RPC 응답은 `/tmp/gc86/repro-kxam/tx.json`에 저장했다.

## 직전 Simulation

Chrome DevTools logger에서 실패 직전 simulation을 확인했다.

| 항목 | 값 |
| --- | ---: |
| 시간 | `2026-07-10T09:19:29.468Z` |
| GasWanted | `2,000,000,000` |
| GasUsed | `184,123,164` |
| Error | 없음 |
| Events | `13` |

Wallet이 simulation 결과에 약 10%를 적용한 것으로 보인다.

```text
184,123,164 * 1.1 = 202,535,480
최종 GasWanted    = 202,537,218
실제 GasUsed      = 202,543,121
```

여유를 적용했지만 실제 사용량이 최종 GasWanted보다 `5,903` 더 많아졌다.

## Simulation과 Broadcast Tx 비교

simulation tx는 gas estimation용 임시 tx라서 `GasWanted=2,000,000,000`이고, 실제 broadcast tx는 estimate와 서명이 반영된 별도 tx다. 따라서 raw base64 전체가 동일하지 않은 것은 정상이다.

두 tx의 핵심 메시지는 동일했다.

- 동일 caller
- 동일 `gnoswap/router.ExactInSwapRoute`
- 동일 GNS → wugnot 경로
- 동일 swap amount `10000000`
- 동일 pool path

동적 인자는 달랐다.

| 인자 | Simulation | DeliverTx |
| --- | ---: | ---: |
| quote 관련 값 | `2109151` | `2109340` |
| deadline | `1783675437` | `1783675425` |

비교 결과: `/tmp/gc86/repro-kxam/sim-vs-deliver.json`

## Browser Logger 결과

사용자가 저장한 `json-rsps.json`에는 총 334개의 browser RPC 기록이 있다. 이번 재현 구간은 대략 `09:18` 이후이며, `.app/simulate` 요청들이 기록되어 있다.

동일 swap 계열 simulation의 `GasUsed`가 다음처럼 변했다.

- `184,118,802`
- `184,123,164`
- `184,762,752`
- `221,856,362`
- `222,495,342`

이벤트 수도 `13`, `14`, `20`으로 변했다. 모두 성공 응답일 수 있어, Error나 이벤트 유무만으로 simulation이 안전하다고 판단할 수 없다.

원본 logger 파일: `json-rsps.json`

정규화 결과: `/tmp/gc86/browser-simulations-current.jsonl`

## 이전 재현 결과

### 이전 성공 swap

동일 raw tx를 20회 simulation했다.

- `GasUsed`: `184,083,408` ~ `184,427,687`
- Events: `15` 또는 `16`
- 같은 ABCI height/app hash에서는 반복 결과가 동일
- 두 RPC backend에서도 같은 state에서는 같은 결과

높은 결과에는 다음 추가 이벤트가 있었다.

```text
StorageDepositEvent
bytes_delta=8
fee_delta=800ugnot
pkg_path=gno.land/r/gnoswap/test_token/test_usdc
```

이 결과는 state-dependent storage/write 경로가 gas와 이벤트를 바꿀 수 있음을 보여준다.

### 과거 known failing tx

DeliverTx:

- GasWanted: `230,372,626`
- GasUsed: `230,396,157`
- Error: `/std.OutOfGasError`
- 위치: `WriteFlat`

현재는 transaction이 만료되어 expiry validation만 재현된다. 따라서 당시의 full write path는 현재 RPC에서 재실행할 수 없다.

## 코드 경로 확인

현재 `chain/test13` 코드와 repository 코드는 다음 동작을 한다.

- Gno client는 `.app/simulate`에 Amino-encoded tx를 보낸다.
- `handleQueryApp`은 `app.Simulate(req.Data)`를 호출한다.
- 응답의 `Height`는 요청의 `req.Height`를 그대로 복사한다.
- `BaseApp.Simulate`은 현재 node의 `checkState`에서 실행한다.
- RPC `height` parameter는 historical simulation state를 고정하지 않는다.

관련 위치:

- `tm2/pkg/sdk/baseapp.go:430`
- `tm2/pkg/sdk/baseapp.go:705`
- `gno.land/pkg/gnoclient/client_txs.go:491`
- `tm2/pkg/store/types/gas.go:50`

따라서 `height=751000`을 넣어도 과거 state에서 실행되는 것이 아니다.

## 판단

현재까지 확인된 내용:

1. 실제 OutOfGas는 재현됐다.
2. 직전 simulation은 성공했지만 실제 DeliverTx보다 약 `18.4M gas` 낮았다.
3. wallet의 고정 약 10% margin이 이번에는 부족했다.
4. 같은 swap 계열 simulation의 gas와 이벤트가 block/state 변화에 따라 변한다.
5. 같은 state에서 backend가 달라도 결과는 일치했다.
6. `ReadFlat`/`WriteFlat`은 OutOfGas가 발생한 gas descriptor일 뿐, 현재 자료만으로 accounting bug라고 단정할 수 없다.

## 다음 디버깅 단계

### Wallet-side

sign/send 직전에 다음 값을 함께 저장한다.

- simulation raw tx
- simulation `GasUsed`
- 적용한 margin 계산값
- 최종 `GasWanted`
- 최종 route arguments
- deadline
- account sequence
- simulation 시각과 broadcast 시각

현재 page logger는 page-side simulation은 잡지만 Adena extension background/service-worker의 최종 sign/send 호출은 직접 잡지 못할 수 있다.

### Core-side

test13 DB snapshot 또는 동일 state fixture가 필요하다.

1. 같은 raw tx를 fixed state에서 10회 이상 simulation
2. `GasUsed`, events, error 비교
3. fixed state에서 결과가 일정하면 state transition 문제로 확정
4. fixed state에서도 달라지면 VM/store gas accounting을 추가 조사

## 권고

현 단계에서 Core gas accounting 코드를 수정할 근거는 부족하다. 가장 작은 운영 대응은 최근 simulation 중 최댓값에 configurable headroom을 추가하고, simulation 성공 및 이벤트 반환을 gas 충분성의 보증으로 취급하지 않는 것이다.

## 재현 도구

임시 도구는 `/tmp/gc86/`에 있다.

- `/tmp/gc86/capture.sh`
- `/tmp/gc86/decode_browser.go`
- `/tmp/gc86/decode_tx.go`
