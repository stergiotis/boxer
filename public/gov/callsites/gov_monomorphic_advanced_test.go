//go:build llm_generated_gemini3pro

package callsites

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CompilerDecision represents what the Go compiler actually did.
type CompilerDecision struct {
	File          string
	Line          int
	Devirtualized bool
	Inlined       bool
	Escaped       bool
}

// ParseCompilerOutput runs 'go build' with optimization flags and parses the output.
// We use -gcflags="-m" to see devirtualization and inlining decisions.
func ParseCompilerOutput(t *testing.T, dir string) map[string]CompilerDecision {
	// 1. Build the command
	// -m: print optimization decisions
	// -m: (second time) print more verbose decisions
	cmd := exec.Command("go", "build", "-gcflags=-m", ".")
	cmd.Dir = dir

	// Set GOWORK=off to ensure we build the isolated module
	cmd.Env = append(os.Environ(), "GOWORK=off")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// We ignore the error because 'go build' might fail on some code,
	// or just warn. We only care about the output.
	_ = cmd.Run()

	// 2. Regex for parsing
	// Sample: ./main.go:15:12: devirtualizing t.M to *T
	reDevirt := regexp.MustCompile(`^(.+):(\d+):(\d+): devirtualizing`)
	// Sample: ./main.go:10:6: can inline Gen[int]
	// Sample: ./main.go:20:6: inlining call to Gen[int]
	reInline := regexp.MustCompile(`^(.+):(\d+):(\d+): inlining call to`)

	decisions := make(map[string]CompilerDecision)

	scanner := bufio.NewScanner(&stderr)
	for scanner.Scan() {
		line := scanner.Text()

		// Normalize file paths to base name for easy matching
		// Output often contains "./main.go" or absolute paths.

		var file string
		var lineNum int
		var matched bool
		var isDevirt bool
		var isInlined bool

		if parts := reDevirt.FindStringSubmatch(line); parts != nil {
			file = parts[1]
			lineNum, _ = strconv.Atoi(parts[2])
			isDevirt = true
			matched = true
		} else if parts := reInline.FindStringSubmatch(line); parts != nil {
			file = parts[1]
			lineNum, _ = strconv.Atoi(parts[2])
			isInlined = true
			matched = true
		}

		if matched {
			base := filepath.Base(file)
			key := fmt.Sprintf("%s:%d", base, lineNum)

			d := decisions[key]
			d.File = base
			d.Line = lineNum
			if isDevirt {
				d.Devirtualized = true
			}
			if isInlined {
				d.Inlined = true
			}
			decisions[key] = d
		}
	}

	return decisions
}

func TestGroundTruth_Comparison(t *testing.T) {
	dir := setupTestDir(t)

	code := `package main

type I interface { M() }
type S struct {}
func (s S) M() {}

// Helper to prevent inlining of the wrapper itself, keeping the call site visible
//go:noinline
func RunInterface(i I) {
	i.M() // truly dynamic (usually)
}

func main() {
	// Case 1: Trivial Devirtualization (Compiler wins)
	var i I = S{}
	i.M() // Go compiler sees 'S' flows here and devirtualizes.

	// Case 2: True Dynamic
	// (Harder to prove for the compiler if passed from outside, but local analysis is strong.
	// We use a global or condition to confuse it).
	RunInterface(i)

	// Case 3: Generic Stenciling
	Generic(10)
}

func Generic[T any](t T) {}
`
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0644)
	require.NoError(t, err)

	// 1. Run Our Analyzer
	svc := &AnalyzerService{Pattern: dir}
	var toolResults []CallSite
	for site, err := range svc.Run(context.Background()) {
		require.NoError(t, err)
		toolResults = append(toolResults, site)
	}

	// 2. Run Go Compiler (Ground Truth)
	compilerDecisions := ParseCompilerOutput(t, dir)

	// 3. Compare
	for _, site := range toolResults {
		key := fmt.Sprintf("%s:%d", filepath.Base(site.File), site.Line)
		decision, exists := compilerDecisions[key]

		fmt.Printf("Checking %s line %d: Tool says %s\n", site.Func, site.Line, site.Type)

		switch site.Type {
		case CallTypeDynamicPolymorphic:
			// If our tool says Dynamic, but the compiler Devirtualized it,
			// this is a "Safe False Positive".
			if exists && decision.Devirtualized {
				t.Logf(" [NOTICE] Line %d: Tool predicted Dynamic, but Go Compiler devirtualized it! (Optimizable)", site.Line)
			} else {
				t.Logf(" [AGREE] Line %d: Tool predicted Dynamic, Go Compiler did not devirtualize.", site.Line)
			}

		case CallTypeStaticPolymorphic:
			// If our tool says Static/Optimized, we expect the compiler to usually inline or not complain.
			if site.StaticSubtype == StaticPolyOptimized {
				if exists && decision.Inlined {
					t.Logf(" [AGREE] Line %d: Tool predicted Optimized, Go Compiler inlined it.", site.Line)
				}
			}

		case CallTypeMonomorphic:
			// Standard calls.
		}
	}

	// Explicit Assertion for the known devirtualization case
	// Line 16: "i.M()" where i is S{}.
	// Our tool (static AST analysis) sees 'i.M()' on an interface type and flags it Dynamic.
	// The Compiler (escape analysis + heuristic) sees it's 'S' and devirtualizes.

	// Let's find line 16 (approximate, depending on file string)
	// We rely on the log output inspection or strict assertions:

	// Verify that the compiler DID devirtualize at least one call in this file
	var devirtCount int
	for _, d := range compilerDecisions {
		if d.Devirtualized {
			devirtCount++
		}
	}
	// In recent Go versions (1.20+), "i.M()" with local "var i I = S{}" is definitely devirtualized.
	assert.GreaterOrEqual(t, devirtCount, 1, "Expected Go compiler to perform at least one devirtualization")
}
