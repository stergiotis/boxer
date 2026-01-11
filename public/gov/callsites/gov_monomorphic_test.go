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
type U uint64

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
func TestAnalyzerService_Run_Generics_Detailed(t *testing.T) {
	// Setup isolated test environment
	dir := setupTestDir(t)

	// Define code with various generic instantiation scenarios
	code := `package main

type St struct { val int }
type I interface { M() }

// Single type parameter
func Gen[T any](t T) {}

// Multiple type parameters
func Gen2[A any, B any](a A, b B) {}

func main() {
	// --- Optimized Cases (Stenciled) ---
	
	// 1. Primitives
	Gen(10)         // int
	Gen(3.14)       // float64
	Gen("string")   // string

	// 2. Slices of Primitives
	Gen([]int{1, 2})
	
	// 3. Functions (Explicitly defined as "Optimized" in our logic)
	f := func() {}
	Gen(f)

	// 4. Multi-param: All Optimized
	Gen2(1, "s")


	// --- Dictionary/Generic Cases (GCShape shared) ---

	// 5. Pointers (to primitives and structs)
	i := 10
	Gen(&i)
	s := St{}
	Gen(&s)

	// 6. Structs (Non-primitive)
	Gen(s)

	// 7. Interfaces
	var iface I
	// Instantiation T=I (interface type)
	Gen(iface)

	// 8. Arrays 
	// (Arrays are typically not stenciled if treated as memory blocks, 
	// our logic defaults them to Generic/Dictionary)
	arr := [2]int{1, 2}
	Gen(arr)

	// 9. Multi-param: Mixed (One optimized, one dictionary)
	// Should result in Dictionary dispatch for the whole call
	Gen2(1, &i)
}
`
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0644)
	require.NoError(t, err)

	svc := &AnalyzerService{Pattern: dir}

	// Run analysis
	var results []CallSite
	for site, err := range svc.Run(context.Background()) {
		require.NoError(t, err)
		results = append(results, site)
	}

	// Helper to filter results by function name
	findSites := func(callee string) []CallSite {
		var out []CallSite
		for _, r := range results {
			if r.Func == callee {
				out = append(out, r)
			}
		}
		return out
	}

	// Helper to count subtypes
	countSubtype := func(sites []CallSite, sub StaticPolySubtypeE) int {
		c := 0
		for _, s := range sites {
			if s.Type == CallTypeStaticPolymorphic && s.StaticSubtype == sub {
				c++
			}
		}
		return c
	}

	// --- Assertions for Gen() ---
	genCalls := findSites("Gen")

	// We expect 10 calls to Gen in total.
	assert.Equal(t, 10, len(genCalls))

	// Expected Optimized calls (5):
	// 1. int
	// 2. float
	// 3. string
	// 4. []int
	// 5. func()
	assert.Equal(t, 5, countSubtype(genCalls, StaticPolyOptimized),
		"Expected 5 Optimized calls (primitives, slices, funcs)")

	// Expected Dictionary calls (5):
	// 1. *int
	// 2. *St
	// 3. St (Struct)
	// 4. I (Interface)
	// 5. [2]int (Array)
	assert.Equal(t, 5, countSubtype(genCalls, StaticPolyGeneric),
		"Expected 5 Dictionary calls (pointers, structs, interfaces, arrays)")

	// --- Assertions for Gen2() ---
	gen2Calls := findSites("Gen2")

	// We expect 2 calls to Gen2.
	assert.Equal(t, 2, len(gen2Calls))

	// Case 1: Gen2(1, "s") -> Both Optimized -> Result Optimized
	assert.Equal(t, 1, countSubtype(gen2Calls, StaticPolyOptimized),
		"Multi-param with all primitives should be Optimized")

	// Case 2: Gen2(1, &i) -> Mixed -> Result Dictionary
	assert.Equal(t, 1, countSubtype(gen2Calls, StaticPolyGeneric),
		"Multi-param with mixed types should be Dictionary")
}
