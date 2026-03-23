---
name: write-txtar-test
description: Write gno integration tests in txtar format. Use when creating new txtar tests, debugging test failures, measuring gas/storage costs, or deploying and interacting with gno realms in integration tests.
argument-hint: [test-description-or-realm-path]
---

# Gno txtar 통합 테스트 작성

txtar 파일은 gno 노드를 시작하고, 패키지를 배포하며, 함수를 호출하여 결과를 검증하는 통합 테스트이다. 이 스킬은 txtar 테스트를 처음부터 작성하는 방법을 설명한다.

자세한 디렉티브 레퍼런스와 고급 패턴은 [reference.md](reference.md)를 참조한다.

## 테스트 실행 방법

```bash
cd gno.land/pkg/integration
go test -v -run "TestTestdata/<파일명_without_extension>" -timeout 300s
```

realm stats 로그를 함께 수집하려면:

```bash
GNO_REALM_STATS_LOG=/tmp/stats.log \
  go test -v -run "TestTestdata/<파일명>" -timeout 300s
```

## 기본 구조

txtar 파일은 크게 세 부분으로 구성된다: (1) 노드 시작 전 설정, (2) 커맨드 실행 및 검증, (3) 임베디드 소스 파일.

```txtar
## 1. 노드 시작 전: 패키지 로드, 유저 추가
loadpkg gno.land/p/nt/avl/v0

gnoland start

## 2. 패키지 배포 및 함수 호출
gnokey maketx addpkg -pkgdir $WORK -pkgpath gno.land/r/test/myapp \
  -gas-fee 100000ugnot -gas-wanted 50000000 \
  -broadcast -chainid=tendermint_test test1

gnokey maketx call -pkgpath gno.land/r/test/myapp -func MyFunc \
  -insecure-password-stdin=true -broadcast=true \
  -chainid=tendermint_test -gas-fee 1000000ugnot \
  -gas-wanted 1000000000 -memo "" test1
stdout 'GAS USED'

## 3. 임베디드 파일
-- gnomod.toml --
module = "gno.land/r/test/myapp"
gno = "0.9"
-- main.gno --
package myapp

func MyFunc(cur realm) string {
    return "hello"
}
```

## 핵심 규칙

### 실행 순서

다음 디렉티브는 반드시 `gnoland start` **이전**에 호출해야 한다:

```
loadpkg    — 외부 패키지 로드
adduser    — 테스트 계정 생성
patchpkg   — 로드된 패키지의 문자열 치환
```

`gnokey` 명령은 반드시 `gnoland start` **이후**에 실행한다.

### 패키지 로드

`gno.land/p/` 또는 `gno.land/r/` 패키지를 사용하려면 `loadpkg`로 미리 로드한다. 의존성 순서대로 로드해야 한다.

```
loadpkg gno.land/p/nt/avl/v0
loadpkg gno.land/p/demo/tokens/grc20
loadpkg gno.land/r/demo/defi/foo20
```

임베디드 파일로 작성한 `$WORK` 내 코드는 `addpkg`로 배포한다.

### 함수 시그니처

realm 함수는 반드시 `cur realm` 파라미터를 포함해야 한다. 이 파라미터는 `-args`로 전달하지 **않는다**.

```go
// realm 함수 (상태 변경 가능)
func Deposit(cur realm, amount int64) { ... }

// 호출 시: -func Deposit -args 1000
// cur realm은 자동 처리됨
```

### 출력 검증

`stdout`과 `stderr`로 출력을 검증한다. 홑따옴표 안은 정규식으로 동작한다.

```
stdout 'GAS USED'                       # 문자열 포함 확인
stdout 'GAS USED:   [0-9]+'             # 정규식 매칭
stdout 'STORAGE DELTA:  [0-9]+ bytes'   # storage 변동 확인
! stdout 'error'                         # 이 문자열이 없어야 함
```

### 실패 예상

`!` 접두사로 실패를 예상하고 `stderr`로 에러 메시지를 검증한다.

```
! gnokey maketx call -pkgpath gno.land/r/test/myapp -func BadFunc \
  -insecure-password-stdin=true -broadcast=true \
  -chainid=tendermint_test -gas-fee 100000ugnot \
  -gas-wanted 1000000 test1
stderr 'permission denied'
```

### 인자 전달

`-args` 플래그를 반복하여 여러 인자를 전달한다.

```
gnokey maketx call ... -func Transfer \
  -args $alice_user_addr \
  -args 1000 \
  -args "memo text" \
  test1
```

### 코인 전송과 함께 호출

`-send` 플래그로 코인을 함께 보낸다.

```
gnokey maketx call ... -func Deposit \
  -send 1000000ugnot \
  test1
```

## gnomod.toml 형식

```toml
module = "gno.land/r/test/myapp"
gno = "0.9"
```

**주의**: `[requires]` 섹션에 슬래시(`/`)가 포함된 키를 사용하면 파싱 에러가 발생한다. 외부 패키지 의존성은 `loadpkg`로 해결한다.

## 트랜잭션 출력 형식

`gnokey maketx call` 성공 시 출력:

```
OK!
GAS WANTED: 1000000000
GAS USED:   2456761
HEIGHT:     3
STORAGE DELTA:  809 bytes
STORAGE FEE:    80900ugnot
TOTAL TX COST:  1080900ugnot
EVENTS:     [{"bytes_delta":809,...}]
```

## 변수

| 변수 | 설명 |
|------|------|
| `$WORK` | 테스트 작업 디렉토리 (임베디드 파일 위치) |
| `$GNOROOT` | gno 리포지토리 루트 |
| `$test1_user_addr` | test1 계정 주소 (기본 내장) |
| `$<name>_user_addr` | adduser로 생성한 계정 주소 |

## 가스/스토리지 측정 테스트 패턴

비용 측정이 목적인 테스트는 `stdout` 검증을 느슨하게 한다:

```txtar
# 가스 측정 — 정확한 값은 변할 수 있으므로 존재만 확인
gnokey maketx call ... -func ExpensiveOp ... test1
stdout 'GAS USED'
stdout 'STORAGE DELTA'
stdout 'STORAGE FEE'
```

realm stats 로그와 함께 실행하면 Object 단위의 상세 분석이 가능하다:

```bash
GNO_REALM_STATS_LOG=/tmp/stats.log go test -v -run "TestTestdata/my_test" -timeout 300s
python3 docs/realm-stats/analyze_realm_stats.py /tmp/stats.log
```

## 주요 참고 파일

- 테스트 파일 위치: `gno.land/pkg/integration/testdata/*.txtar`
- 디렉티브 구현: `gno.land/pkg/integration/testscript_gnoland.go`
- 테스트 러너: `gno.land/pkg/integration/testdata_test.go`
- 기존 벤치마크 예시: `base_storage_gas_measurement.txtar`, `base_map_vs_avl_benchmark.txtar`
