package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	// Process softfloat64.go file
	processSoftFloat64File()

	// Process softfloat64_test.go file
	processSoftFloat64TestFile()

	// Run mvdan.cc/gofumpt
	gofumpt()

	fmt.Println("Files processed successfully.")
}

func processSoftFloat64File() {
	// Read source file
	content, err := os.ReadFile(fmt.Sprintf("%s/src/runtime/softfloat64.go", runtime.GOROOT()))
	if err != nil {
		log.Fatal("Error reading source file:", err)
	}

	// Prepare header
	header := `// Code generated by github.com/gnolang/gno/gnovm/pkg/gnolang/internal/softfloat/gen. DO NOT EDIT.
// This file is copied from $GOROOT/src/runtime/softfloat64.go.
// It is the software floating point implementation used by the Go runtime.

`

	// Combine header with content
	newContent := header + string(content)

	// Replace package name
	newContent = strings.Replace(newContent, "package runtime", "package softfloat", 1)

	// Write to destination file
	err = os.WriteFile("runtime_softfloat64.go", []byte(newContent), 0o644)
	if err != nil {
		log.Fatal("Error writing to destination file:", err)
	}
}

func processSoftFloat64TestFile() {
	// Read source test file
	content, err := os.ReadFile(fmt.Sprintf("%s/src/runtime/softfloat64_test.go", runtime.GOROOT()))
	if err != nil {
		log.Fatal("Error reading source test file:", err)
	}

	// Prepare header
	header := `// Code generated by github.com/gnolang/gno/gnovm/pkg/gnolang/internal/softfloat/gen. DO NOT EDIT.
// This file is copied from $GOROOT/src/runtime/softfloat64_test.go.
// It is the tests for the software floating point implementation
// used by the Go runtime.

`

	// Combine header with content
	newContent := header + string(content)

	// Replace package name and imports
	newContent = strings.Replace(newContent, "package runtime_test", "package softfloat_test", 1)
	newContent = strings.Replace(newContent, "\t. \"runtime\"", "\t\"runtime\"", 1)
	newContent = strings.Replace(newContent, "GOARCH", "runtime.GOARCH", 1)

	newContent = strings.Replace(newContent, "import (", "import (\n\t. \"github.com/gnolang/gno/gnovm/pkg/gnolang/internal/softfloat\"", 1)

	// Write to destination file
	err = os.WriteFile("runtime_softfloat64_test.go", []byte(newContent), 0o644)
	if err != nil {
		log.Fatal("Error writing to destination test file:", err)
	}
}

func gitRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	p := wd
	for {
		if _, e := os.Stat(filepath.Join(p, ".git")); e == nil {
			return p, nil
		}

		if strings.HasSuffix(p, string(filepath.Separator)) {
			return "", errors.New("root git not found")
		}

		p = filepath.Dir(p)
	}
}

func gofumpt() {
	rootPath, err := gitRoot()
	if err != nil {
		log.Fatal("error finding git root:", err)
	}

	cmd := exec.Command("go", "run", "-modfile", filepath.Join(strings.TrimSpace(rootPath), "misc/devdeps/go.mod"), "mvdan.cc/gofumpt", "-w", ".")
	_, err = cmd.Output()
	if err != nil {
		log.Fatal("error gofumpt:", err)
	}
}
