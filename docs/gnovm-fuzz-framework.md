# gno 퍼저

## 도입 배경

최근 gno VM의 preprocessor 회귀가 잦아지고 있어(#5240, #5587, #5291 회귀) 일반 단위 테스트로는 잘 잡히지 않는 panic·crash 케이스를 자동으로 탐색할 수단이 필요했습니다. Persistence 레이어 또한 amino 회귀(#5590)와 `values_fill.go`/`values_copy.go` 주변 변경이 잦고, amino 자체도 reflect codec과 genproto2 fast-path 사이의 drift가 합의 안전성에 직결되어 동일한 검증이 필요합니다. 외부 감사를 진행하기 전에 내부 퍼저로 핵심 레이어부터 검증하는 것이 비용 대비 효율적이라는 판단입니다.

이전에 동일한 패턴의 transpiler 퍼저(PR #3457, 강화 #3715)가 존재했으나 PR #4192 리팩토링 과정에서 함께 제거됐습니다. 이번 작업은 그 패턴을 (1) parser/typechecker/preprocessor 라인, (2) realm persistence 라인, (3) amino cross-codec parity 라인에 맞춰 다시 도입한 것입니다.

## 검증 범위

Go 네이티브 fuzzer 5종이 세 파일에 들어 있습니다.

**`gnovm/pkg/gnolang/preprocess_fuzz_test.go`** — 프론트엔드 파이프라인

| Fuzzer | 검증 대상 | 통과 조건 |
|---|---|---|
| `FuzzParse` | `(*Machine).ParseFile` | 모든 panic이 error로 정상 회수되어야 함. ParseFile을 빠져나오는 panic은 버그. |
| `FuzzTypeCheck` | `gnolang.TypeCheckMemPackage` | 5초 안에 결과 반환. timeout 또는 panic은 버그. |
| `FuzzPreprocess` | `(*Machine).PreprocessFiles` | recover된 값이 `*PreprocessError`이면 의도된 user-error report로 통과. 그 외 panic은 버그. |

시드 코퍼스: `examples/**/*.gno` + `gnovm/tests/files/*.gno` (약 3,400개). 비-gno 더미 파일은 시딩 단계 ParseFile 게이트로 자동 제외.

**`gnovm/pkg/gnolang/persist_fuzz_test.go`** — realm persistence

| Fuzzer | 검증 대상 | 통과 조건 |
|---|---|---|
| `FuzzPersist` | `(*Machine).RunMemPackage` (realm crawl save 포함) | (A) Go-level `runtime.Error` 파장 없음. (B) 두 개의 독립 store에서 동일 입력 실행 시 op-log와 user-error 분류가 일치(determinism). |

같은 파일에 `TestRealmSeedsRunCleanly`도 포함 — 99개 zrealm 시드를 비-fuzz 서브테스트로 실행해 일반 `go test`(non-short)에서 회귀 감지.

시드 코퍼스: `gnovm/tests/files/zrealm_*.gno` (`// PKGPATH:` 디렉티브가 있는 realm 시나리오 파일).

**`tm2/pkg/amino/tests/fuzz/binary/parity_fuzz_test.go`** — amino cross-codec parity

| Fuzzer | 검증 대상 | 통과 조건 |
|---|---|---|
| `FuzzCodecParity` | reflect codec(`MarshalReflect`/`UnmarshalReflect`) vs genproto2(`MarshalBinary2`/`UnmarshalBinary2`) | 등록 타입으로 decode 성공한 입력에 대해 `aminotest.AssertCodecParity`의 4 invariant 모두 만족 — 인코더 byte-equal, size invariant, 두 디코더 결과 DeepEqual, round-trip fidelity. |

`FuzzNilElements`는 `amino:"nil_elements"` 태그로 인해 nil과 non-nil-zero pointer가 wire상 동일하므로 **strict round-trip이 성립하지 않아 의도적으로 타겟 제외**.

시드 코퍼스: `tests.Package`에 등록된 타입의 marshal 출력으로 부트스트랩(InterfaceHeavy, GnoVMTypedValue, AminoMarshalerStruct1, FuzzFixedInt 등 ~10개).

## 실행 방법

모든 fuzzer는 `testing.Short()`로 skip 처리되어 있어 일반 CI/`-short` 테스트 패스에는 영향이 없습니다. on-demand 전용입니다.

```bash
# 개별 실행 (시간은 늘릴수록 커버리지가 좋아짐)
go test -run='^$' -fuzz=FuzzPreprocess  -fuzztime=15m ./gnovm/pkg/gnolang/
go test -run='^$' -fuzz=FuzzTypeCheck   -fuzztime=15m ./gnovm/pkg/gnolang/
go test -run='^$' -fuzz=FuzzParse       -fuzztime=15m ./gnovm/pkg/gnolang/
go test -run='^$' -fuzz=FuzzPersist     -fuzztime=30m ./gnovm/pkg/gnolang/
go test -run='^$' -fuzz=FuzzCodecParity -fuzztime=30m ./tm2/pkg/amino/tests/fuzz/binary/

# 발견된 크래시 재현
go test -run=FuzzPreprocess/<hash> -v ./gnovm/pkg/gnolang/

# 시드 회귀 테스트만 (FuzzPersist용, fuzz 없이 빠르게)
go test -run='^TestRealmSeedsRunCleanly$' ./gnovm/pkg/gnolang/
```

크래시 입력은 자동으로 `<package>/testdata/fuzz/<FuzzName>/<hash>`에 저장됩니다. 회귀 방지를 위해 함께 commit하면 다음 실행부터 시드로 사용됩니다. 단, upstream 의존(예: `go/types` 버그) 크래시는 해당 외부 fix가 들어간 Go 버전 사용자에서는 false PASS이므로 commit하지 않는 것이 좋습니다. 또한 검증 대상이 의도적으로 lossy한 케이스(예: amino `nil_elements`)는 false positive이므로 oracle 또는 타겟 리스트에서 제외해야 합니다.
