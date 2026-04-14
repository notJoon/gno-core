package doctest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/gnolang/gno/gnovm/pkg/gnoenv"
	gno "github.com/gnolang/gno/gnovm/pkg/gnolang"
	"github.com/gnolang/gno/gnovm/pkg/test"
)

const (
	optIgnore      = "ignore"       // Do not run the code block
	optShouldPanic = "should_panic" // Expect a panic
	gnoLang        = "gno"
	gnoDoctest     = "gnodoctest" // Alternative tag for GitHub syntax highlighting (e.g. "go,gnodoctest")

	mainPkgPath   = "main"
	maxAllocBytes = 500_000_000
)

// isGnoDoctest checks if the language tag contains "gnodoctest",
// which allows using "go,gnodoctest" for GitHub syntax highlighting
// while still being recognized as a gno code block.
func isGnoDoctest(lang string) bool {
	for part := range strings.SplitSeq(lang, ",") {
		if strings.TrimSpace(part) == gnoDoctest {
			return true
		}
	}
	return false
}

// ExecuteCodeBlock executes a parsed code block in a gno VM and
// returns its captured output, or compares it against the expected
// output/error when the block declares one.
func ExecuteCodeBlock(c codeBlock, rootDir string) (string, error) {
	if c.options.Ignore {
		return "IGNORED", nil
	}

	lang := strings.Split(c.lang, ",")[0]
	if lang != gnoLang && !isGnoDoctest(c.lang) {
		return fmt.Sprintf("SKIPPED (Unsupported language: %s)", lang), nil
	}

	output, runErr := runGnoBlock(rootDir, c)

	if c.options.PanicMessage != "" {
		return handlePanicMessage(runErr, c.options.PanicMessage)
	}
	if runErr != nil {
		return "", runErr
	}
	if c.expectedOutput == "" && c.expectedError == "" {
		return output, nil
	}
	return compareResults(output, c.expectedOutput, c.expectedError)
}

// runGnoBlock parses and runs the code block's content as a main
// package, returning everything written to stdout.
func runGnoBlock(rootDir string, c codeBlock) (_ string, err error) {
	buf := new(bytes.Buffer)
	_, store := test.ProdStore(rootDir, buf, nil)
	m := gno.NewMachineWithOptions(gno.MachineOptions{
		PkgPath:       mainPkgPath,
		Output:        buf,
		Store:         store,
		Context:       test.Context(test.DefaultCaller, mainPkgPath, nil),
		MaxAllocBytes: maxAllocBytes,
	})
	defer m.Release()
	defer func() {
		if r := recover(); r != nil {
			if upe, ok := r.(gno.UnhandledPanicError); ok {
				err = errors.New(upe.Error())
				return
			}
			err = fmt.Errorf("%v", r)
		}
	}()

	file, err := m.ParseFile(fmt.Sprintf("%d.gno", c.index), c.content)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	m.RunFiles(file)

	mainExpr, err := m.ParseExpr("main()")
	if err != nil {
		return "", fmt.Errorf("parse main(): %w", err)
	}
	m.Eval(mainExpr)

	return buf.String(), nil
}

// ExecuteMatchingCodeBlock executes all code blocks in the given content that match the given pattern.
// It returns a slice of execution results as strings and any error encountered during the execution.
func ExecuteMatchingCodeBlock(
	ctx context.Context,
	content string,
	pattern string,
	rootDir string,
) ([]string, error) {
	codeBlocks, err := GetCodeBlocks(content)
	if err != nil {
		return nil, err
	}

	results := make([]string, 0, len(codeBlocks))
	for _, block := range codeBlocks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if !matchPattern(block.name, pattern) {
			continue
		}

		result, err := ExecuteCodeBlock(block, rootDir)
		if err != nil {
			return nil, fmt.Errorf("failed to execute code block %s: %w", block.name, err)
		}
		results = append(results, fmt.Sprintf("\n=== %s ===\n\n%s\n", block.name, result))
	}

	return results, nil
}

func handlePanicMessage(err error, panicMessage string) (string, error) {
	if err == nil {
		return "", fmt.Errorf(
			"expected panic with message: %s, but executed successfully",
			panicMessage,
		)
	}

	if strings.Contains(err.Error(), panicMessage) {
		return fmt.Sprintf("panicked as expected: %v", err), nil
	}

	return "", fmt.Errorf(
		"expected panic with message: %s, but got: %s",
		panicMessage, err.Error(),
	)
}

// compareResults compares the actual output of code execution with the expected output or error.
func compareResults(actual, expectedOutput, expectedError string) (string, error) {
	actual = strings.TrimSpace(actual)
	expected := strings.TrimSpace(expectedOutput)
	if expected == "" {
		expected = strings.TrimSpace(expectedError)
	}

	if expected == "" {
		if actual != "" {
			return "", fmt.Errorf("expected no output, but got:\n%s", actual)
		}
		return "", nil
	}

	if pattern, ok := strings.CutPrefix(expected, "regex:"); ok {
		return compareRegex(actual, pattern)
	}

	if actual != expected {
		return "", fmt.Errorf("expected:\n%s\n\nbut got:\n%s", expected, actual)
	}

	return actual, nil
}

// compareRegex compares the actual output against a regex pattern.
// It returns an error if the regex is invalid or if the actual output does not match the pattern.
func compareRegex(actual, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	if !re.MatchString(actual) {
		return "", fmt.Errorf(
			"output did not match regex pattern:\npattern: %s\nactual: %s",
			pattern, actual,
		)
	}

	return actual, nil
}

// matchPattern checks if a name matches the specific pattern.
// An empty pattern matches everything; otherwise pattern is treated
// as a regular expression (same convention as `go test -run`).
func matchPattern(name, pattern string) bool {
	if pattern == "" {
		return true
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

// DefaultRootDir returns the gno root directory used by the doctest
// runner when one is not explicitly supplied.
func DefaultRootDir() string {
	return gnoenv.RootDir()
}
