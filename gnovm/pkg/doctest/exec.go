package doctest

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gnolang/gno/gno.land/pkg/sdk/vm"
	"github.com/gnolang/gno/gnovm/pkg/gnoenv"
	"github.com/gnolang/gno/tm2/pkg/crypto/ed25519"
	"github.com/gnolang/gno/tm2/pkg/std"
)

const (
	optIgnore     = "ignore"       // Do not run the code block
	optShouldPanic = "should_panic" // Expect a panic
	gnoLang        = "gno"
	gnoDoctest     = "gnodoctest"   // Alternative tag for GitHub syntax highlighting (e.g. "go,gnodoctest")
)

// isGnoDoctest checks if the language tag contains "gnodoctest",
// which allows using "go,gnodoctest" for GitHub syntax highlighting
// while still being recognized as a gno code block.
func isGnoDoctest(lang string) bool {
	for _, part := range strings.Split(lang, ",") {
		if strings.TrimSpace(part) == gnoDoctest {
			return true
		}
	}
	return false
}

var (
	regexCache = make(map[string]*regexp.Regexp)

	addrRegex = regexp.MustCompile(`gno\.land/[pre]/[a-z0-9]+/[a-z_/.]+`)
)

// ExecuteCodeBlock executes a parsed code block and executes it in a gno VM.
func ExecuteCodeBlock(c codeBlock, stdlibDir string) (string, error) {
	if c.options.Ignore {
		return "IGNORED", nil
	}

	// Extract the actual language from the lang field.
	// Supports "gno" and "go,gnodoctest" (for GitHub syntax highlighting).
	lang := strings.Split(c.lang, ",")[0]
	if lang != gnoLang && !isGnoDoctest(c.lang) {
		return fmt.Sprintf("SKIPPED (Unsupported language: %s)", lang), nil
	}
	// Normalize language to gno for file naming.
	lang = gnoLang

	ctx, acck, _, vmk, stdlibCtx := setupEnv(stdlibDir)

	files := []*std.MemFile{
		{Name: fmt.Sprintf("%d.%s", c.index, lang), Body: c.content},
	}

	// create a freash account for the code block
	privKey := ed25519.GenPrivKey()
	addr := privKey.PubKey().Address()
	acc := acck.NewAccountWithAddress(ctx, addr)
	acck.SetAccount(ctx, acc)

	msg2 := vm.NewMsgRun(addr, std.Coins{}, files)

	res, err := vmk.Run(stdlibCtx, msg2)
	if c.options.PanicMessage != "" {
		return handlePanicMessage(err, c.options.PanicMessage)
	}

	// remove package path from the result and replace with `main`.
	res = replacePackagePath(res)

	if err != nil {
		return "", err
	}

	// If there is no expected output or error, It is considered
	// a simple code execution and the result is returned as is.
	if c.expectedOutput == "" && c.expectedError == "" {
		return res, nil
	}

	// Otherwise, compare the actual output with the expected output or error.
	return compareResults(res, c.expectedOutput, c.expectedError)
}

// ExecuteMatchingCodeBlock executes all code blocks in the given content that match the given pattern.
// It returns a slice of execution results as strings and any error encountered during the execution.
func ExecuteMatchingCodeBlock(
	ctx context.Context,
	content string,
	pattern string,
	stdlibsDir string,
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

		result, err := ExecuteCodeBlock(block, stdlibsDir)
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

	if strings.HasPrefix(expected, "regex:") {
		return compareRegex(actual, strings.TrimPrefix(expected, "regex:"))
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

// getCompiledRegex retrieves or compiles a regex pattern.
func getCompiledRegex(pattern string) (*regexp.Regexp, error) {
	if re, exists := regexCache[pattern]; exists {
		return re, nil
	}

	compiledPattern := regexp.QuoteMeta(pattern)
	compiledPattern = strings.ReplaceAll(compiledPattern, "\\*", ".*")
	re, err := regexp.Compile(compiledPattern)
	if err != nil {
		return nil, err
	}

	regexCache[pattern] = re
	return re, nil
}

// matchPattern checks if a name matches the specific pattern.
func matchPattern(name, pattern string) bool {
	if pattern == "" {
		return true
	}

	re, err := getCompiledRegex(pattern)
	if err != nil {
		return false
	}

	return re.MatchString(name)
}

// for display purpose, replace address string with `main.xxx` when printing type.
// ref: https://github.com/gnolang/gno/pull/2357#discussion_r1704398563
func replacePackagePath(input string) string {
	result := addrRegex.ReplaceAllStringFunc(input, func(match string) string {
		parts := strings.Split(match, "/")
		if len(parts) < 4 {
			return match
		}
		lastPart := parts[len(parts)-1]
		subParts := strings.Split(lastPart, ".")
		if len(subParts) < 2 {
			return "main." + lastPart
		}
		return "main." + subParts[len(subParts)-1]
	})

	return result
}

// GetStdlibsDir returns the path to the standard libraries directory
// based on GNOROOT.
func GetStdlibsDir() string {
	return filepath.Join(gnoenv.RootDir(), "gnovm", "stdlibs")
}

