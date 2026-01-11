//go:build llm_generated_gemini3pro

package callsites

import (
	"bufio"
	"bytes"
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

// CompilerDecision definition
type CompilerDecision struct {
	Devirtualized bool
	Inlined       bool
}

func parseCompilerOutput(dir string) map[string]CompilerDecision {
	cmd := exec.Command("go", "build", "-gcflags=-m", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()

	reDevirt := regexp.MustCompile(`^(.+):(\d+):(\d+): devirtualizing`)
	reInline := regexp.MustCompile(`^(.+):(\d+):(\d+): inlining call to`)

	decisions := make(map[string]CompilerDecision)
	scanner := bufio.NewScanner(&stderr)

	for scanner.Scan() {
		line := scanner.Text()
		var file string
		var lineNum int
		var isDevirt, isInline bool

		if parts := reDevirt.FindStringSubmatch(line); parts != nil {
			file = parts[1]
			lineNum, _ = strconv.Atoi(parts[2])
			isDevirt = true
		} else if parts := reInline.FindStringSubmatch(line); parts != nil {
			file = parts[1]
			lineNum, _ = strconv.Atoi(parts[2])
			isInline = true
		} else {
			continue
		}

		key := fmt.Sprintf("%s:%d", filepath.Base(file), lineNum)
		d := decisions[key]
		if isDevirt {
			d.Devirtualized = true
		}
		if isInline {
			d.Inlined = true
		}
		decisions[key] = d
	}
	return decisions
}

func TestGroundTruth_Comparison(t *testing.T) {
	dir := setupTestDir(t)

	code := `package main

type I interface { M() }
type S struct {}
func (s S) M() {}

//go:noinline
func RunI(i I) { i.M() } // True Dynamic

func Gen[T any](t T) {}

func main() {
	// Case 1: Trivial Devirtualization
	var i I = S{}
	i.M()

	// Case 2: True Dynamic
	RunI(i)

	// Case 3: Generic Stenciled
	Gen(10)
}
`
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0644)
	require.NoError(t, err)

	// Run Tool
	toolResults := collectResults(t, dir)

	// Run Compiler
	compilerDecisions := parseCompilerOutput(dir)

	for _, site := range toolResults {
		key := fmt.Sprintf("%s:%d", filepath.Base(site.File), site.Line)
		decision := compilerDecisions[key]

		if site.Func == "M" {
			// Case 1 vs Case 2
			// Analyzer is strictly semantic: Interface recv == Dynamic
			assert.Equal(t, CallTypeDynamicPolymorphic, site.Type)

			if decision.Devirtualized {
				t.Logf("Line %d: Analyzer=Dynamic, Compiler=Devirtualized (Optimized)", site.Line)
			} else {
				t.Logf("Line %d: Analyzer=Dynamic, Compiler=Dynamic (Agreement)", site.Line)
			}
		}

		if site.Func == "Gen" {
			// Case 3
			assert.Equal(t, CallTypeStaticPolymorphic, site.Type)
			assert.Equal(t, []StaticPolySubtypeE{SubtypeBasic}, site.TypeArgs)

			if decision.Inlined {
				t.Logf("Line %d: Analyzer=Static(Basic), Compiler=Inlined (Agreement)", site.Line)
			} else {
				t.Logf("Line %d: Analyzer=Static(Basic), Compiler=NotInlined", site.Line)
			}
		}
	}
}
