//go:build llm_generated_gemini3pro

package callsites

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDir creates a temporary go module structure.
func setupTestDir(t *testing.T) (dir string) {
	var err error
	dir, err = os.MkdirTemp("", "analyzer_test_*")
	require.NoError(t, err)
	// This forces the 'go' command (invoked by packages.Load) to ignore your
	// parent 'go.work' file and treat the temp directory as an independent module.
	t.Setenv("GOWORK", "off")
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.23"), 0644)
	require.NoError(t, err)
	return
}

func TestAnalyzerService_Run_EndToEnd(t *testing.T) {
	var dir string
	dir = setupTestDir(t)

	// Create sub-package to test "3rdParty" logic
	// Logic: If pkg path contains "." and != current, it's 3rdParty.
	err := os.Mkdir(filepath.Join(dir, "lib"), 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "lib", "lib.go"), []byte(`package lib
func ExternalFunc() {}
`), 0644)
	require.NoError(t, err)

	// Main code
	code := `package main

import (
	"fmt"
	"example.com/test/lib"
)

type I interface { M() }
type S struct {}
func (s S) M() {}

func Mono() {}
func GenFunc[T any](t T) {}
type G[T any] struct { val T }
func (g G[T]) Method() {}

func main() {
	fmt.Println("hello") // Mono/StdLib
	Mono()               // Mono/Local
	var i I = S{}
	i.M()                // Dynamic/Local
	GenFunc(1)           // StaticPoly/Local
	g := G[int]{}
	g.Method()           // StaticPoly/Local
	f := func() {}
	f()                  // Dynamic/Local (Closure)
	lib.ExternalFunc()   // Mono/3rdParty (Simulated)
}
`
	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0644)
	require.NoError(t, err)

	svc := &AnalyzerService{Pattern: dir}

	// Collect results from iterator
	var results []CallSite
	var errors []error

	for site, err := range svc.Run(context.Background()) {
		if err != nil {
			errors = append(errors, err)
			continue
		}
		results = append(results, site)
	}

	assert.Empty(t, errors)
	assert.NotEmpty(t, results)

	// Helper to find result
	findSite := func(callee string) *CallSite {
		for _, r := range results {
			if r.Func == callee {
				return &r
			}
		}
		return nil
	}

	// Assertions
	var s *CallSite

	// 1. Mono / StdLib
	s = findSite("Println")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeMonomorphic, s.Type)
	assert.Equal(t, OriginStdLib, s.Origin)

	// 2. Mono / Local
	s = findSite("Mono")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeMonomorphic, s.Type)
	assert.Equal(t, OriginLocal, s.Origin)

	// 3. Dynamic / Local (Interface)
	s = findSite("M")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeDynamicPolymorphic, s.Type)

	// 4. Static / Local (Generic Func)
	s = findSite("GenFunc")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeStaticPolymorphic, s.Type)

	// 5. Static / Local (Generic Receiver)
	s = findSite("Method")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeStaticPolymorphic, s.Type)

	// 6. Dynamic (Closure)
	s = findSite("f")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeDynamicPolymorphic, s.Type)

	// 7. Mono / 3rdParty (lib.ExternalFunc)
	s = findSite("ExternalFunc")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeMonomorphic, s.Type)
	assert.Equal(t, Origin3rdParty, s.Origin)
}

func TestAnalyzerService_ErrorPropagation(t *testing.T) {
	var dir string
	dir = setupTestDir(t)

	// Create a broken file to force package error
	err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package main\nfunc broken( {"), 0644)
	require.NoError(t, err)

	svc := &AnalyzerService{Pattern: dir}

	var errorCount int
	for _, err := range svc.Run(context.Background()) {
		if err != nil {
			errorCount++
			// We expect "package load error"
			assert.Contains(t, err.Error(), "package load error")
		}
	}
	assert.Greater(t, errorCount, 0, "expected at least one package load error")
}
