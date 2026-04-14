package doctest

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	mast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const testPrefix = "// @test:"

var (
	outputRegex = regexp.MustCompile(`(?m)^// Output:$([\s\S]*?)(?:^(?://\s*$|// Error:|$))`)
	errorRegex  = regexp.MustCompile(`(?m)^// Error:$([\s\S]*?)(?:^(?://\s*$|// Output:|$))`)
	optionRegex = regexp.MustCompile(`@(\w+)(?:="([^"]*)")?`)
)

// codeBlock represents a block of code extracted from the input text.
type codeBlock struct {
	content        string // The content of the code block.
	start          int    // The start byte position of the code block in the input text.
	end            int    // The end byte position of the code block in the input text.
	lang           string // The language type of the code block.
	index          int    // The index of the code block in the sequence of extracted blocks.
	expectedOutput string // The expected output of the code block.
	expectedError  string // The expected error of the code block.
	name           string // The name of the code block.
	options        ExecutionOptions
}

// GetCodeBlocks parses the provided markdown text to extract all embedded code blocks.
// It returns a slice of codeBlock structs, each representing a distinct block of code found in the markdown.
func GetCodeBlocks(body string) ([]codeBlock, error) {
	md := goldmark.New()
	reader := text.NewReader([]byte(body))
	doc := md.Parser().Parse(reader)

	var codeBlocks []codeBlock
	if err := mast.Walk(doc, func(n mast.Node, entering bool) (mast.WalkStatus, error) {
		if entering {
			if cb, ok := n.(*mast.FencedCodeBlock); ok {
				codeBlock, err := createCodeBlock(cb, body, len(codeBlocks))
				if err != nil {
					return mast.WalkStop, err
				}
				codeBlock.name = codeBlockName(codeBlock.content, codeBlock.index)
				codeBlocks = append(codeBlocks, codeBlock)
			}
		}
		return mast.WalkContinue, nil
	}); err != nil {
		return nil, err
	}

	return codeBlocks, nil
}

// createCodeBlock creates a CodeBlock from a code block node.
func createCodeBlock(node *mast.FencedCodeBlock, body string, index int) (codeBlock, error) {
	var buf bytes.Buffer
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		buf.Write([]byte(body[line.Start:line.Stop]))
	}

	content := buf.String()
	language := string(node.Language([]byte(body)))
	if language == "" {
		language = "plain"
	}

	options := parseExecutionOptions(language, content)

	start := lines.At(0).Start
	end := lines.At(node.Lines().Len() - 1).Stop

	expectedOutput, expectedError, err := parseExpectedResults(content)
	if err != nil {
		return codeBlock{}, err
	}

	return codeBlock{
		content:        content,
		start:          start,
		end:            end,
		lang:           language,
		index:          index,
		expectedOutput: expectedOutput,
		expectedError:  expectedError,
		options:        options,
	}, nil
}

// parseExpectedResults scans the code block content for expecting outputs and errors,
// which are typically indicated by special comments in the code.
func parseExpectedResults(content string) (string, string, error) {
	var outputs, errors []string

	outputMatches := outputRegex.FindAllStringSubmatch(content, -1)
	for _, match := range outputMatches {
		if len(match) > 1 {
			cleaned, err := cleanSection(match[1])
			if err != nil {
				return "", "", err
			}
			if cleaned != "" {
				outputs = append(outputs, cleaned)
			}
		}
	}

	errorMatches := errorRegex.FindAllStringSubmatch(content, -1)
	for _, match := range errorMatches {
		if len(match) > 1 {
			cleaned, err := cleanSection(match[1])
			if err != nil {
				return "", "", err
			}
			if cleaned != "" {
				errors = append(errors, cleaned)
			}
		}
	}

	expectedOutput := strings.Join(outputs, "\n")
	expectedError := strings.Join(errors, "\n")

	return expectedOutput, expectedError, nil
}

func cleanSection(section string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(section))
	var cleanedLines []string

	for scanner.Scan() {
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)

		// Skip lines that are not comment lines.
		if !strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Remove the "//" prefix and at most one leading space.
		line := strings.TrimPrefix(trimmed, "//")
		line = strings.TrimPrefix(line, " ")

		cleanedLines = append(cleanedLines, line)
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to clean section: %w", err)
	}

	return strings.Join(cleanedLines, "\n"), nil
}

// codeBlockName returns the explicit name set with `// @test: <name>`,
// or a positional fallback name when none is provided.
func codeBlockName(content string, index int) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		if after, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), testPrefix); ok {
			if name := strings.TrimSpace(after); name != "" {
				return name
			}
		}
	}
	return fmt.Sprintf("block_%d", index)
}

//////////////////// Execution Options ////////////////////

type ExecutionOptions struct {
	Ignore       bool
	PanicMessage string
	// TODO: add more options
}

func parseExecutionOptions(language string, content string) ExecutionOptions {
	var options ExecutionOptions

	parts := strings.Split(language, ",")
	for _, option := range parts[1:] { // skip the first part which is the language
		switch strings.TrimSpace(option) {
		case optIgnore:
			options.Ignore = true
		case optShouldPanic:
			// specific panic message will be parsed later
		}
	}

	// Scan all comment lines for @ directives.
	// e.g. // @should_panic="some panic message here"
	//        |-option name-||-----option value-----|
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := []byte(strings.TrimSpace(scanner.Text()))
		if !bytes.HasPrefix(line, []byte("//")) {
			continue
		}
		matches := optionRegex.FindAllSubmatch(line, -1)
		for _, match := range matches {
			switch string(match[1]) {
			case optShouldPanic:
				if match[2] != nil {
					options.PanicMessage = string(match[2])
				}
			case optIgnore:
				options.Ignore = true
			}
		}
	}

	return options
}
