# Gno VM Gas 측정 내부 구조 분석

> 이 문서는 Gno 블록체인의 가상 머신(GnoVM) 내부에서 gas가 어떻게 정의되고, 트랜잭션의 제출 시점부터 실행 완료 시점까지 각 계층에서 어떤 방식으로 측정·소모되는지를 소스 코드 수준에서 추적한 기술 인수인계 문서이다. 모든 설명은 2026년 3월 기준 `master` 브랜치의 코드를 근거로 한다.

---

## 1. Gas의 정의와 GasMeter 인터페이스

Gno에서 gas는 `int64` 타입의 정수값이다(`tm2/pkg/store/types/gas.go:22`에서 `type Gas = int64`로 정의). 모든 gas 소모는 `GasMeter` 인터페이스를 통해 이루어지며, 이 인터페이스는 `ConsumeGas(amount Gas, descriptor string)` 메서드를 핵심으로 하여 현재까지 소모된 gas 총량을 추적하고, 한도 초과 시 `OutOfGasError`를 panic으로 발생시키는 역할을 한다.

### 1.1 세 가지 구현체

`GasMeter` 인터페이스에는 세 가지 구현체가 존재한다.

첫 번째는 `basicGasMeter`로, 일반적인 트랜잭션 실행에 사용된다. 생성 시 `limit` 값을 받으며, `ConsumeGas` 호출마다 `consumed` 필드에 소모량을 누적한다. 이때 중요한 설계적 특징이 있는데, gas 소모량이 한도를 초과하더라도 일단 `consumed` 필드에는 해당 값을 기록한 뒤 `OutOfGasError`를 panic으로 발생시킨다(`gas.go:101-106`). 이는 gas 초과 상황에서도 실제 소모량을 정확히 보고할 수 있도록 하기 위한 의도적 설계이다. 덧셈 연산 시에는 `overflow.Add`를 사용하여 int64 오버플로우가 발생하면 `GasOverflowError`를 별도로 panic시킨다.

두 번째는 `infiniteGasMeter`로, genesis 블록 처리와 쿼리 실행에 사용된다. `Limit()`이 0을 반환하고 `Remaining()`은 `math.MaxInt64`를 반환하며, `IsPastLimit()`과 `IsOutOfGas()`는 항상 `false`를 반환한다. 따라서 gas 한도 초과로 인한 실행 중단이 발생하지 않지만, 소모량 자체는 계속 추적한다.

세 번째는 `passthroughGasMeter`로, 트랜잭션 수준의 gas 한도와 블록 수준의 gas 한도를 동시에 관리하기 위해 설계되었다. 내부에 `Head`(로컬 한도를 가진 `basicGasMeter`)와 `Base`(상위 미터)를 두며, `ConsumeGas` 호출 시 `Base`와 `Head` 양쪽 모두에 동일한 소모량을 전달한다(`gas.go:202-205`). `IsPastLimit`이나 `IsOutOfGas` 등 한도 관련 질의는 `Head`만을 기준으로 판단한다. 이 구조 덕분에 개별 트랜잭션의 gas 한도를 넘지 않으면서도, 블록 전체의 gas 총량도 함께 추적할 수 있다.

---

## 2. 트랜잭션 구조와 수수료 체계

### 2.1 Tx와 Fee 구조체

트랜잭션은 `tm2/pkg/std/tx.go`에 정의된 `Tx` 구조체로 표현되며, 그 안에 `Fee` 구조체가 포함된다. `Fee`는 두 개의 필드로 구성되는데, `GasWanted`는 트랜잭션이 사용할 수 있는 최대 gas 한도를 `int64`로 명시하고, `GasFee`는 사용자가 지불할 총 수수료를 `Coin` 타입(예: `"1000000ugnot"`)으로 명시한다. `GasWanted`의 상한값은 `(1 << 60) - 1`로 설정되어 있으며, `ValidateBasic()`에서 이를 초과하는 값이 입력되면 `ErrGasOverflow`가 반환된다.

### 2.2 GasPrice와 최소 수수료 검증

gas 가격은 `tm2/pkg/std/gasprice.go`의 `GasPrice` 구조체로 표현된다. 이 구조체는 `Gas int64`와 `Price Coin` 두 필드를 가지며, "특정 gas 단위당 얼마의 코인을 지불해야 하는가"를 나타낸다. 문자열로는 `"1ugnot/1gas"`와 같은 형식으로 표현된다.

가격 비교 시 정수 나눗셈으로 인한 정밀도 손실을 방지하기 위해, `IsGTE` 메서드는 교차 곱셈(cross-multiplication) 방식을 사용한다. 구체적으로 `(gp.Price.Amount × gpB.Gas) >= (gp.Gas × gpB.Price.Amount)` 비교를 `math/big.Int`를 사용하여 수행한다(`gasprice.go:69-81`). 이 방식을 채택한 이유는 노드 설정에서 최소 gas 가격을 `0.00001ugnot/gas`와 같은 극소값으로 설정할 수 있기 때문이다. 부동소수점 연산이나 정수 나눗셈을 사용하면 이러한 설정에서 비교 결과가 부정확해질 수 있으므로, 교차 곱셈으로 정밀도를 보장한다.

---

## 3. 트랜잭션 실행의 전체 흐름

### 3.1 BaseApp.runTx — 실행의 시작점

트랜잭션이 `CheckTx`(멤풀 진입 검증) 또는 `DeliverTx`(블록 내 확정 실행) 경로를 통해 처리될 때, `BaseApp.runTx`(`tm2/pkg/sdk/baseapp.go:731`)가 진입점이 된다. 이 함수는 가장 먼저 실행 모드가 `RunTxModeDeliver`인지 확인하고, 만약 그렇다면 블록 gas 미터의 잔여량을 한도로 하는 `passthroughGasMeter`를 생성하여 컨텍스트에 설정한다(`baseapp.go:742-748`).

```go
if mode == RunTxModeDeliver {
    gasleft := ctx.BlockGasMeter().Remaining()
    ctx = ctx.WithGasMeter(store.NewPassthroughGasMeter(
        ctx.GasMeter(),
        gasleft,
    ))
}
```

이 시점에서 `passthroughGasMeter`의 `Head`는 블록 잔여 gas를 한도로 가지고, `Base`는 원래의 트랜잭션 gas 미터를 가리킨다. 따라서 이후 모든 gas 소모는 트랜잭션 수준과 블록 수준 양쪽에 동시에 기록된다.

블록의 gas가 이미 소진된 상태라면(`IsOutOfGas()` 반환값이 `true`), 트랜잭션은 실행 없이 즉시 거부된다.

`runTx`에는 두 개의 `defer` 함수가 등록되어 있다. 첫 번째 defer는 `OutOfGasError` panic을 recover하여 적절한 에러 응답으로 변환하고, `result.GasUsed`를 `ctx.GasMeter().GasConsumed()`로 설정한다. 두 번째 defer는 `DeliverTx` 모드에서 트랜잭션이 실제로 소모한 gas(`GasConsumedToLimit()`)를 블록 gas 미터에 반영한다(`baseapp.go:796-806`). `GasConsumedToLimit()`를 사용하는 이유는, 한도를 초과한 초과분까지 블록 gas에 반영하면 블록 gas 미터의 합산이 왜곡될 수 있기 때문이다.

### 3.2 AnteHandler — 실행 전 검증과 초기 gas 소모

`BaseApp.runTx`는 `AnteHandler`를 호출하기 전에 `cacheTxContext`로 캐시 래핑된 컨텍스트를 생성한다. AnteHandler가 abort를 반환하면 캐시를 버리고, 성공하면 `msCache.MultiWrite()`로 커밋한다.

AnteHandler(`tm2/pkg/sdk/auth/ante.go:41`)는 다음 순서로 작동한다.

**블록 gas 한도 검증**: 컨센서스 파라미터의 `Block.MaxGas`와 `tx.Fee.GasWanted`를 비교한다. `MaxGas`가 -1이면 gas 한도를 적용하지 않으며(권장되지 않음), 그 외의 경우 `GasWanted`가 `MaxGas`를 초과하면 트랜잭션을 거부한다.

**멤풀 수수료 검증** (`CheckTx` 모드에서만): `EnsureSufficientMempoolFees` 함수(`ante.go:340`)가 호출된다. 이 함수는 두 단계 검증을 수행하는데, 먼저 블록 gas price(컨텍스트에 설정된 `GasPriceContextKey`)와 비교하고, 그다음 노드의 최소 gas price 목록(`minGasPrices`)과 비교한다. 비교는 앞서 설명한 교차 곱셈 방식으로 이루어진다.

**GasMeter 생성**: `SetGasMeter` 함수(`ante.go:410-418`)가 호출된다. 블록 높이가 0(genesis)이면 `infiniteGasMeter`를, 그 외에는 `basicGasMeter(tx.Fee.GasWanted)`를 생성하여 컨텍스트에 설정한다.

**트랜잭션 크기 gas 차감**: `params.TxSizeCostPerByte × len(txBytes)` 만큼의 gas를 소모한다(`ante.go:105`). 기본값은 바이트당 10 gas이다(`auth/params.go:19`).

**메모 크기 검증**: 메모 길이가 `MaxMemoBytes`(기본 65,536바이트)를 초과하면 거부한다.

**수수료 선차감**: `DeductFees` 함수(`ante.go:310`)가 `tx.Fee.GasFee`에 해당하는 코인을 사용자 계정에서 fee collector 주소로 이체한다. 이 이체는 gas 소모가 아니라 코인의 물리적 이동이다. 잔고가 부족하면 트랜잭션이 즉시 거부된다. 중요한 점은, 수수료가 트랜잭션 실행 이전에 선차감된다는 것이다. 따라서 트랜잭션이 gas 부족으로 실패하더라도 수수료는 환불되지 않는다.

**서명 검증 gas 차감**: 각 서명자에 대해 `DefaultSigVerificationGasConsumer`(`ante.go:268`)가 호출된다. ED25519 키는 590 gas, Secp256k1 키는 1,000 gas가 소모된다. 다중 서명(multisig)의 경우 `consumeMultisignatureVerificationGas`에서 각 하위 서명에 대해 재귀적으로 gas를 소모한다.

### 3.3 메시지 핸들러 실행

AnteHandler가 성공적으로 완료되면, `runTx`는 다시 `cacheTxContext`로 컨텍스트를 캐시 래핑한 뒤 `runMsgs`(`baseapp.go:644`)를 호출한다. `runMsgs`는 `CheckTx` 모드에서는 메시지를 실제로 실행하지 않고(`mode != RunTxModeCheck` 조건), `DeliverTx`나 `Simulate` 모드에서만 실제 핸들러를 호출한다. 최종적으로 `result.GasUsed = ctx.GasMeter().GasConsumed()`로 사용된 gas를 기록한다.

---

## 4. VM Keeper — GnoVM으로의 진입

VM Keeper(`gno.land/pkg/sdk/vm/keeper.go`)는 세 가지 메시지 타입을 처리한다.

### 4.1 AddPackage (MsgAddPackage)

패키지를 온체인에 배포하는 메시지이다. `keeper.go:626-634`에서 `gno.NewMachineWithOptions`를 호출하여 GnoVM의 Machine을 생성하는데, 이때 `GasMeter: ctx.GasMeter()`로 AnteHandler에서 생성된 gas 미터를 그대로 전달한다. 또한 `Alloc: gnostore.GetAllocator()`로 메모리 할당기도 전달한다. `maxAllocTx` 상수(`keeper.go:46`)는 500MB(500,000,000 바이트)로 설정되어 있어, 단일 트랜잭션의 메모리 할당 한도를 제한한다.

패키지 실행이 완료된 후에는 `processStorageDeposit`(`keeper.go:1221`)이 호출되어 스토리지 보증금을 처리한다. 이 함수는 `gnostore.RealmStorageDiffs()`를 통해 realm별 스토리지 변화량(바이트 단위 증감)을 확인하고, 증가분에 대해 `StoragePrice × diff`만큼의 보증금을 caller로부터 스토리지 보증금 주소로 이체한다. 감소분에 대해서는 보증금을 반환한다. 이 스토리지 보증금 메커니즘은 gas 소모와는 별도의 비용 체계이다.

### 4.2 Call (MsgCall)

realm의 공개 함수를 호출하는 메시지이다. 역시 `ctx.GasMeter()`를 Machine에 전달하며, 호출 대상 함수의 첫 번째 파라미터가 `.uverse.realm` 타입(crossing 함수)이어야 한다는 제약이 있다(`keeper.go:670-672`).

### 4.3 Run (MsgRun)

임의의 Gno 코드를 실행하는 메시지이다. 이 경우 Machine이 두 개 생성되는 점이 특이한데, 첫 번째 Machine은 패키지를 파싱·실행하여 `*PackageValue`를 생성하고, 두 번째 Machine은 이 패키지의 `main()` 함수를 실행한다(`keeper.go:890-927`). 두 Machine 모두 동일한 gas 미터와 allocator를 공유하므로, gas 소모는 두 단계에 걸쳐 누적된다.

### 4.4 QueryEval

읽기 전용 쿼리 실행에 사용된다. `maxGasQuery`(3,000,000,000)를 한도로 하는 별도의 gas 미터를 사용하며, `maxAllocQuery`(1,500,000,000 바이트)로 메모리 한도를 일반 트랜잭션의 3배로 설정한다.

---

## 5. CPU Cycle Gas 측정

### 5.1 incrCPU 메커니즘

GnoVM의 Machine(`gnovm/pkg/gnolang/machine.go`)은 각 연산(Op)을 실행할 때마다 `incrCPU(cycles int64)` 메서드를 호출한다. 이 메서드는 `GasFactorCPU`(현재 `1`)를 cycle 수에 곱하여 gas 소모량을 계산하고, gas 미터가 설정되어 있으면 `ConsumeGas`를 호출한다(`machine.go:1102-1108`).

```go
const GasFactorCPU int64 = 1

func (m *Machine) incrCPU(cycles int64) {
    if m.GasMeter != nil {
        gasCPU := overflow.Mulp(cycles, GasFactorCPU)
        m.GasMeter.ConsumeGas(gasCPU, "CPUCycles")
    }
    m.Cycles += cycles
}
```

`GasFactorCPU`가 1이므로 현재 구현에서는 1 CPU cycle = 1 gas이다. 이 계수는 향후 gas 경제 모델 조정 시 변경될 수 있는 설계상의 여유를 제공한다.

### 5.2 연산별 CPU Cycle 상수

각 Op에 대한 CPU cycle 비용은 `machine.go:1110-1229`에 상수로 정의되어 있으며, 크게 다섯 범주로 나뉜다.

**제어 흐름 연산**: `OpCPUHalt`(1), `OpCPUExec`(25), `OpCPUPrecall`(207), `OpCPUCall`(256), `OpCPUCallNativeBody`(424), `OpCPUEnterCrossing`(100), `OpCPUDefer`(64), `OpCPUReturn`(38), `OpCPUPanic1`(121). 함수 호출 비용(256)이 가장 비싸고, 네이티브 함수 호출(424)은 Go 런타임으로의 전환 비용을 반영하여 더 높다. `OpCPUEnterCrossing`(100)은 realm 간 전환에 따른 보안 컨텍스트 변경 비용을 반영한다.

**단항·이항 연산**: 산술 연산(`Add`=18, `Sub`=6, `Mul`=19, `Quo`=16)은 비교적 저렴하나, 동등 비교(`Eql`=160)는 재귀적 깊은 비교가 필요하여 현저히 비싸다. 비트 연산(`Band`=9, `Bor`=23)은 산술 연산보다 저렴하다.

**복합 리터럴 연산**: `OpCPUMapLit`(475)와 `OpCPUSliceLit2`(467)가 가장 비싼데, 이는 해시 테이블 초기화와 메모리 할당을 수반하기 때문이다. `OpCPUStructLit`(179), `OpCPUArrayLit`(137)도 상대적으로 높은 비용을 가진다.

**대입·선언 연산**: `OpCPUDefine`(111), `OpCPUValueDecl`(113)은 스코프 내 이름 바인딩 처리 비용을 반영한다. 복합 대입(`OpCPUAddAssign`=85)은 단순 대입(`OpCPUAssign`=79)보다 약간 높다.

**루프·반복 연산**: `OpCPURangeIter`(105)가 가장 비싸고, 문자열 반복(`OpCPURangeIterString`=55)과 맵 반복(`OpCPURangeIterMap`=48)이 그 뒤를 잇는다.

일부 연산은 아직 구현되지 않았으며(`OpCPUGo`=1, `OpCPUSelect`=1, `OpCPUPointerType`=1 등) 이들은 임시 값인 1로 설정되어 있다. 주석에 `// Not yet implemented`로 표시되어 있다.

---

## 6. 메모리 할당 Gas 측정

### 6.1 Allocator의 구조

`Allocator`(`gnovm/pkg/gnolang/alloc.go`)는 GnoVM의 메모리 사용량을 추적하는 구조체이다. `maxBytes`(최대 허용량), `bytes`(현재 사용량), `peakBytes`(최대 도달 사용량)의 세 필드를 핵심으로 가진다.

Gas 소모는 `GasCostPerByte` 상수(`alloc.go:84`)에 의해 결정되며, 현재 바이트당 1 gas이다.

### 6.2 Peak Watermark 방식

Allocator의 gas 과금은 peak watermark 방식을 사용한다(`alloc.go:156-163`). 즉, 현재 메모리 사용량(`bytes`)이 이전 최대치(`peakBytes`)를 초과할 때만 그 초과분에 대해 gas를 과금하고, `peakBytes`를 갱신한다.

```go
if alloc.bytes > alloc.peakBytes {
    if alloc.gasMeter != nil {
        change := alloc.bytes - alloc.peakBytes
        alloc.gasMeter.ConsumeGas(overflow.Mulp(change, GasCostPerByte), "memory allocation")
    }
    alloc.peakBytes = alloc.bytes
}
```

이 설계의 의미는 GC(가비지 컬렉션) 후 메모리가 줄어들었다가 다시 증가하는 경우, 이전 peak를 다시 넘지 않는 한 추가 gas가 과금되지 않는다는 것이다. 코드의 주석(`alloc.go:154-155`)에도 "`bytes` 값은 GC 중 감소하며, `peakBytes`를 (다시) 초과할 때만 수수료가 부과된다"고 명시되어 있다. GC로 메모리가 해제되어도 gas가 환불되지는 않으므로, gas 과금은 단조 증가하는 특성을 가진다.

### 6.3 Gno 타입별 할당 크기

`alloc.go:29-82`에는 각 Gno 타입의 메모리 할당 크기가 상수로 정의되어 있다. 기본 할당 단위(`_allocBase`)는 24바이트이며, 포인터(`_allocPointer`)는 8바이트이다. 주요 Gno 값의 할당 크기는 `_allocBase + _allocPointer + 고유크기`로 계산된다. 예를 들어 `allocStruct`는 `24 + 8 + 152 = 184`바이트이고, 각 필드(`allocStructField`)는 40바이트(`_allocTypedValue`)를 추가로 소모한다. `allocMap`은 `24 + 8 + 144 = 176`바이트이며, 각 맵 항목은 `_allocTypedValue × 3 = 120`바이트를 소모한다(키·값·다음 노드 포인터). 문자열은 기본 24바이트에 바이트당 1의 비용이 추가된다.

Machine 생성 시(`machine.go:132-135`) Allocator에 gas 미터와 GC 콜백이 설정된다. GC 콜백은 `Machine.GarbageCollect()` 메서드를 가리키며, 메모리 한도 초과 시 GC를 시도하여 공간을 확보한다.

---

## 7. KVStore 레벨 Gas 측정

### 7.1 Gas 래핑 Store

`tm2/pkg/store/gas/store.go`에 정의된 `gas.Store`는 하위 KVStore를 래핑하여 모든 I/O 연산에 gas 비용을 부과한다. 이 래핑은 AnteHandler에서 생성된 gas 미터를 받아 동작하므로, KVStore에서 소모된 gas는 트랜잭션의 전체 gas 소모량에 합산된다.

### 7.2 기본 Gas 비용 (`DefaultGasConfig`)

`tm2/pkg/store/types/gas.go:229-239`에 정의된 기본 비용은 다음과 같다.

**Has 연산**은 키의 존재 여부만 확인하는 가벼운 연산으로 1,000 gas의 고정 비용을 가진다. **Delete 연산**도 1,000 gas의 고정 비용을 가지는데, 코드 주석(`gas/store.go:58`)에 "공간이 해제되지만 특정 공격 벡터를 방지하기 위해 gas를 부과한다"고 명시되어 있다. 이는 삭제 연산의 무한 반복을 통한 상태 변경 공격을 방지하기 위한 것이다.

**Read 연산**은 1,000 gas의 고정 비용에 바이트당 3 gas의 가변 비용이 추가된다. `gas/store.go:30-37`의 구현을 보면, 먼저 `ReadCostFlat`을 소모한 뒤 하위 스토어에서 값을 읽고, 읽은 값의 길이에 `ReadCostPerByte`를 곱한 값을 추가로 소모한다. 즉 읽기 비용은 `1000 + 3 × len(value)`이다.

**Write 연산**은 2,000 gas의 고정 비용에 바이트당 30 gas의 가변 비용이 추가된다. 쓰기의 고정 비용이 읽기의 2배, 바이트당 비용이 10배인 이유는 쓰기가 머클 트리 갱신과 디스크 I/O를 수반하여 실제 연산 비용이 현저히 높기 때문이다.

**Iterator Next 연산**은 30 gas의 고정 비용에 현재 값의 바이트당 3 gas가 추가된다. `gas/store.go:180-184`의 `consumeSeekGas`에서 구현되어 있으며, 반복자가 유효한 상태(`Valid()` 반환값이 `true`)일 때만 과금된다. 최초 반복자 생성 시에도 유효한 위치를 가리키고 있다면 seek gas가 한 번 소모된다(`gas/store.go:97-99`).

---

## 8. VM Object Store 레벨 Gas 측정

### 8.1 GnoVM 전용 GasConfig

GnoVM의 `defaultStore`(`gnovm/pkg/gnolang/store.go:140-172`)는 KVStore 레벨과는 별도의 `GasConfig`를 가지며, `consumeGas` 헬퍼 함수(`store.go:1114-1119`)를 통해 gas를 소모한다. 이 헬퍼는 gas 미터가 nil이 아닐 때만 동작하여, 테스트 환경에서 gas 미터 없이도 store를 사용할 수 있게 한다.

### 8.2 Object 직렬화와 Gas

**GetObject** (`store.go:459-511`): 캐시에 없는 Object를 backend에서 로드할 때, 먼저 `baseStore.Get`으로 직렬화된 바이트를 읽는다. 이 바이트 배열은 `[hash (32 bytes) | amino-encoded object]` 형식이다. Gas 비용은 `GasGetObject(16) × len(bz)`로 계산된다(`store.go:480-481`). 즉, Object의 직렬화 크기에 비례하여 바이트당 16 gas가 부과된다.

**SetObject** (`store.go:619-700`): Object를 저장할 때는 먼저 자식 객체를 `RefValue`로 치환한 복사본을 만들고(`copyValueWithRefs`), amino로 직렬화한 뒤 `GasSetObject(16) × len(bz)`의 gas를 소모한다(`store.go:636-637`). 이후 SHA-256 해시를 계산하고, 해시와 직렬화 데이터를 합쳐서 backend에 저장한다. `SetObject`는 또한 객체 크기의 차이(`diff`)를 반환하여 realm별 스토리지 변화량 추적에 사용한다.

**GetType / SetType** (`store.go:761-860`): 타입 정보는 Object보다 작은 단위로 직렬화되며, 읽기는 바이트당 5 gas(`GasGetType`), 쓰기는 바이트당 52 gas(`GasSetType`)가 부과된다. `GetTypeSafe`는 amino 디코딩 결과를 ristretto 캐시(최대 128MB)에 저장하여, 동일 타입의 반복 조회 시 디코딩 비용을 절약한다(`store.go:787-795`).

**GetPackageRealm / SetPackageRealm**: realm 데이터는 패키지의 상태 트리 전체를 포함하므로 크기가 매우 클 수 있다. 이에 따라 바이트당 524 gas로 읽기와 쓰기 모두 가장 높은 비용이 부과된다.

**AddMemPackage / GetMemPackage**: 메모리 패키지(소스 코드 포함)에 대해 바이트당 8 gas가 부과된다.

**DeleteObject**: 객체 삭제는 바이트 비례가 아닌 고정 비용 3,715 gas가 부과된다. 이는 삭제 연산이 참조 정리와 캐시 무효화를 수반하기 때문이다.

### 8.3 이중 gas 과금 구조

여기서 주의할 점은, VM Object Store의 gas 과금과 KVStore의 gas 과금이 이중으로 적용된다는 것이다. `defaultStore.SetObject`가 `ds.consumeGas`를 호출하여 VM 레벨의 gas를 소모한 뒤, `ds.baseStore.Set`을 호출하면 `gas.Store` 래퍼가 다시 KVStore 레벨의 gas를 소모한다. 따라서 하나의 Object 저장 작업은 `(GasSetObject × len(bz)) + (WriteCostFlat + WriteCostPerByte × len(hashbz))`만큼의 gas를 소모한다. 이는 의도적인 설계로, VM 레벨에서는 직렬화/역직렬화의 연산 비용을, KVStore 레벨에서는 물리적 I/O의 비용을 각각 반영한다.

---

## 9. Gas 미터의 전파 경로 요약

하나의 트랜잭션 실행에서 gas 미터가 어떻게 전파되는지를 정리하면 다음과 같다.

`AnteHandler.SetGasMeter`에서 `basicGasMeter(GasWanted)`가 생성된다. `BaseApp.runTx`에서 이 미터를 `Base`로, 블록 잔여 gas를 `Head` 한도로 하는 `passthroughGasMeter`가 만들어져 `ctx.GasMeter()`로 설정된다. 이 컨텍스트의 gas 미터는 `VMKeeper.Call/AddPackage/Run`에서 `MachineOptions.GasMeter`로 전달되고, Machine 내부에서 `m.GasMeter` 필드에 저장된다. 동시에 이 미터는 `Allocator.SetGasMeter`를 통해 Allocator에도 설정되고, `gnoStore.BeginTransaction`의 인자로도 전달되어 `defaultStore.gasMeter`에 설정된다.

결과적으로, CPU cycle 소모(`m.incrCPU`), 메모리 할당 소모(`alloc.Allocate`), VM Object I/O 소모(`ds.consumeGas`), KVStore I/O 소모(`gas.Store.Get/Set`)가 모두 동일한 `passthroughGasMeter` 인스턴스를 통해 누적되며, 이 미터의 `GasConsumed()`가 최종적으로 `result.GasUsed`로 보고된다.

---

## 10. Gas 관련 에러 처리

### 10.1 OutOfGasError

gas 소모량이 한도를 초과하면 `OutOfGasError{Descriptor: "..."}` 구조체가 panic으로 발생한다. `Descriptor` 필드에는 어떤 연산에서 gas가 소진되었는지를 나타내는 문자열이 기록된다(예: `"CPUCycles"`, `"WriteFlat"`, `"memory allocation"`, `"SetObjectPerByte"` 등).

이 panic은 세 곳에서 recover된다. `AnteHandler`의 defer(`ante.go:77-93`)에서는 AnteHandler 실행 중 발생한 OutOfGas를 처리한다. `BaseApp.runTx`의 defer(`baseapp.go:761-788`)에서는 메시지 실행 중 발생한 OutOfGas를 처리한다. 두 경우 모두 `std.ErrOutOfGas`로 변환되어 ABCI 응답으로 반환되며, `result.GasUsed`에 실제 소모량이 기록된다.

### 10.2 GasOverflowError

gas 누적 중 int64 오버플로우가 발생하면 `GasOverflowError`가 panic으로 발생한다. 이는 `overflow.Add` 함수가 오버플로우를 감지하여 `ok=false`를 반환할 때 트리거된다(`gas.go:97-100`). 블록 gas 미터에서 오버플로우가 감지되면 `std.ErrGasOverflow`가 사용된다(`baseapp.go:803-804`).

### 10.3 allocation limit exceeded

Allocator의 메모리 한도(`maxAllocTx` 또는 `maxAllocQuery`)가 초과되면 `"allocation limit exceeded"` panic이 발생한다(`alloc.go:149`). 이전에 GC 콜백이 먼저 시도되며(`alloc.go:139-144`), GC 후에도 한도를 초과하면 panic이 발생한다. 이 panic은 gas 시스템과는 별도이지만 트랜잭션 실행을 중단시키는 또 다른 자원 제한 메커니즘이다.

---

## 11. 스토리지 보증금 시스템 (Gas 외부 비용)

Gas 측정 시스템과 별도로, Gno는 realm의 영구 스토리지 사용에 대해 보증금(deposit) 시스템을 운영한다. 이 보증금은 gas로 소모되는 것이 아니라, 실제 코인(ugnot)이 스토리지 보증금 주소로 이체되는 방식이다.

`VMKeeper.processStorageDeposit`(`keeper.go:1221-1311`)은 트랜잭션 실행 후 호출되어, `gnostore.RealmStorageDiffs()`로 realm별 스토리지 변화량을 확인한다. 스토리지가 증가하면 `diff × StoragePrice` 만큼의 보증금이 caller에서 잠기고, 스토리지가 감소하면 그에 비례한 보증금이 환불된다. 다만, gnot 토큰이 잠금 상태(restricted)인 경우 환불금은 caller가 아닌 `StorageFeeCollector` 주소로 전송된다.

이 보증금 시스템은 gas 비용과 이중으로 적용된다. 즉, Object를 저장하면 gas가 소모되면서 동시에 보증금도 잠기며, Object를 삭제하면 gas가 소모되면서 보증금은 일부 반환된다.

---

## 12. 주요 파일 참조

| 구성 요소 | 파일 경로 |
|-----------|-----------|
| GasMeter 인터페이스 및 구현체 | `tm2/pkg/store/types/gas.go` |
| KVStore gas 래퍼 | `tm2/pkg/store/gas/store.go` |
| GasPrice 타입 및 파싱 | `tm2/pkg/std/gasprice.go` |
| 트랜잭션 구조체 | `tm2/pkg/std/tx.go` |
| AnteHandler 및 수수료 처리 | `tm2/pkg/sdk/auth/ante.go` |
| Auth 파라미터 (서명 검증 비용 등) | `tm2/pkg/sdk/auth/params.go` |
| BaseApp 트랜잭션 실행 | `tm2/pkg/sdk/baseapp.go` |
| VM Keeper (메시지 핸들러) | `gno.land/pkg/sdk/vm/keeper.go` |
| GnoVM Machine (CPU cycle) | `gnovm/pkg/gnolang/machine.go` |
| GnoVM Store (Object I/O gas) | `gnovm/pkg/gnolang/store.go` |
| Allocator (메모리 gas) | `gnovm/pkg/gnolang/alloc.go` |

---

## 13. 향후 고려사항

현재 코드에서 일부 CPU cycle 상수는 `// XXX` 또는 `// Todo benchmark this properly`라는 주석이 달려 있어, 벤치마크 기반의 정밀한 교정이 아직 완료되지 않은 상태이다. 특히 `OpCPUEnterCrossing`(100)과 `OpCPUReturnAfterCopy`(38) 등이 이에 해당한다. 또한 `OpCPUStaticTypeOf`(100)에는 "적절한 벤치마크 방법이 아직 결정되지 않았다"는 주석이 있다.

`GasFactorCPU`가 1로 하드코딩되어 있어 CPU gas와 스토리지 gas 사이의 상대적 가중치를 조정할 여유가 제한적이다. 향후 gas 경제 모델의 정밀 조정이 필요할 경우, 이 계수를 변경하거나 더 세분화된 gas 과금 체계를 도입해야 할 수 있다.

Allocator의 peak watermark 방식은 GC 이후 재할당 시 gas를 과금하지 않는 특성이 있어, 반복적인 할당-해제-재할당 패턴에서 실제 연산 비용에 비해 gas가 과소 측정될 가능성이 있다. 이에 대한 모니터링과 필요시 과금 모델 수정이 고려될 수 있다.
