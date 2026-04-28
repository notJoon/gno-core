# gno VM 퍼저

## 도입 배경

최근 gno VM의 preprocessor 회귀가 잦아지고 있어(#5240, #5587, #5291 회귀) 일반 단위 테스트로는 잘 잡히지 않는 panic·crash 케이스를 자동으로 탐색할 수단이 필요했습니다. Persistence 레이어 또한 amino 회귀(#5590)와 `values_fill.go`/`values_copy.go` 주변 변경이 잦아 동일한 검증이 필요한 영역입니다. 외부 감사를 진행하기 전에 내부 퍼저로 핵심 레이어부터 검증하는 것이 비용 대비 효율적이라는 판단입니다.

이전에 동일한 패턴의 transpiler 퍼저(PR #3457, 강화 #3715)가 존재했으나 PR #4192 리팩토링 과정에서 함께 제거됐습니다. 이번 작업은 그 패턴을 (1) parser/typechecker/preprocessor 라인과 (2) realm persistence 라인에 맞춰 다시 도입한 것입니다.

## 검증 범위

Go 네이티브 fuzzer 4종이 두 파일에 들어 있습니다.

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

## 실행 방법

모든 fuzzer는 `testing.Short()`로 skip 처리되어 있어 일반 CI/`-short` 테스트 패스에는 영향이 없습니다. on-demand 전용입니다.

```bash
# 개별 실행 (시간은 늘릴수록 커버리지가 좋아짐)
go test -run='^$' -fuzz=FuzzPreprocess  -fuzztime=15m ./gnovm/pkg/gnolang/
go test -run='^$' -fuzz=FuzzTypeCheck   -fuzztime=15m ./gnovm/pkg/gnolang/
go test -run='^$' -fuzz=FuzzParse       -fuzztime=15m ./gnovm/pkg/gnolang/
go test -run='^$' -fuzz=FuzzPersist     -fuzztime=30m ./gnovm/pkg/gnolang/

# 발견된 크래시 재현
go test -run=FuzzPreprocess/<hash> -v ./gnovm/pkg/gnolang/

# 시드 회귀 테스트만 (FuzzPersist용, fuzz 없이 빠르게)
go test -run='^TestRealmSeedsRunCleanly$' ./gnovm/pkg/gnolang/
```

크래시 입력은 자동으로 `gnovm/pkg/gnolang/testdata/fuzz/<FuzzName>/<hash>`에 저장됩니다. 회귀 방지를 위해 함께 commit하면 다음 실행부터 시드로 사용됩니다. 단, upstream 의존(예: `go/types` 버그) 크래시는 해당 외부 fix가 들어간 Go 버전 사용자에서는 false PASS이므로 commit하지 않는 것이 좋습니다.
