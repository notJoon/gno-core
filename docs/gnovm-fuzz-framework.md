# gno VM 프론트엔드 퍼저

## 도입 배경

최근 gno VM의 preprocessor 회귀가 잦아지고 있어(#5240, #5587, #5291 회귀) 일반 단위 테스트로는 잘 잡히지 않는 panic·crash 케이스를 자동으로 탐색할 수단이 필요했습니다. 외부 감사를 진행하기 전에 내부 퍼저로 핵심 레이어부터 검증하는 것이 비용 대비 효율적이라는 판단입니다.

이전에 동일한 패턴의 transpiler 퍼저(PR #3457, 강화 #3715)가 존재했으나 PR #4192 리팩토링 과정에서 함께 제거됐습니다. 이번 작업은 그 패턴을 parser/typechecker/preprocessor 라인에 맞춰 다시 도입한 것입니다.

## 검증 범위

`gnovm/pkg/gnolang/preprocess_fuzz_test.go` 한 파일에 Go 네이티브 fuzzer 3종이 들어 있습니다.

| Fuzzer | 검증 대상 | 통과 조건 |
|---|---|---|
| `FuzzParse` | `(*Machine).ParseFile` | 모든 panic이 error로 정상 회수되어야 함. ParseFile을 빠져나오는 panic은 버그. |
| `FuzzTypeCheck` | `gnolang.TypeCheckMemPackage` | 5초 안에 결과 반환. timeout 또는 panic은 버그. |
| `FuzzPreprocess` | `(*Machine).PreprocessFiles` | recover된 값이 `*PreprocessError`이면 의도된 user-error report로 통과. 그 외 panic은 버그. |

**시드 코퍼스**는 `examples/**/*.gno`와 `gnovm/tests/files/*.gno`에서 수집(약 3,400개). 비-gno 더미 파일은 시딩 단계 ParseFile 게이트로 자동 제외됩니다.

## 실행 방법

세 fuzzer 모두 `testing.Short()`로 skip 처리되어 있어 일반 CI/`-short` 테스트 패스에는 영향이 없습니다. on-demand 전용입니다.

```bash
# 개별 실행 (시간은 늘릴수록 커버리지가 좋아짐)
go test -run='^$' -fuzz=FuzzPreprocess  -fuzztime=15m ./gnovm/pkg/gnolang/
go test -run='^$' -fuzz=FuzzTypeCheck   -fuzztime=15m ./gnovm/pkg/gnolang/
go test -run='^$' -fuzz=FuzzParse       -fuzztime=15m ./gnovm/pkg/gnolang/

# 발견된 크래시 재현
go test -run=FuzzPreprocess/<hash> -v ./gnovm/pkg/gnolang/
```

크래시 입력은 자동으로 `gnovm/pkg/gnolang/testdata/fuzz/<FuzzName>/<hash>`에 저장됩니다. 회귀 방지를 위해 함께 commit하면 다음 실행부터 시드로 사용됩니다.
