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
// It forces GOWORK=off to ensure strict isolation from the host environment.
func setupTestDir(t *testing.T) (dir string) {
	var err error
	dir, err = os.MkdirTemp("", "analyzer_test_*")
	require.NoError(t, err)

	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("GOWORK", "off") // Critical for go/packages to work in /tmp

	err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.23"), 0644)
	require.NoError(t, err)

	return
}

// collectResults runs the analyzer and consumes the iterator into a slice.
func collectResults(t *testing.T, dir string) []CallSite {
	svc := &AnalyzerService{Pattern: dir}
	var results []CallSite

	// Convention: Consumption of iter.Seq2
	for site, err := range svc.Run(context.Background()) {
		require.NoError(t, err)
		results = append(results, site)
	}
	return results
}

func TestAnalyzerService_Run_EndToEnd(t *testing.T) {
	dir := setupTestDir(t)

	// 1. Setup a "3rd party" sub-package simulation
	// Note: logic in determineOrigin treats any path with "." different from current as 3rdParty/External.
	libDir := filepath.Join(dir, "lib")
	err := os.Mkdir(libDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(libDir, "lib.go"), []byte(`package lib
func ExternalFunc() {}
`), 0644)
	require.NoError(t, err)

	// 2. Setup Main Package
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
func GetVariadicFunc[T any](t ...T) {}

func main() {
	fmt.Println("hello")

	Mono()        

	var i I = S{}
	i.M()           

	GenFunc(1)       

	GenFunc("s")

	f := func() {}
	f()   

	lib.ExternalFunc()

	GetVariadicFunc(1,2,3)
}
`
	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0644)
	require.NoError(t, err)

	// 3. Execution
	results := collectResults(t, dir)

	// 4. Assertions
	findSite := func(callee string) *CallSite {
		for _, r := range results {
			if r.Func == callee {
				return &r
			}
		}
		return nil
	}

	// [1] Println
	s := findSite("Println")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeMonomorphic, s.Type)
	assert.Equal(t, OriginStdLib, s.Origin)

	// [2] Mono
	s = findSite("Mono")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeMonomorphic, s.Type)
	assert.Equal(t, OriginLocal, s.Origin)

	// [3] M (Interface)
	s = findSite("M")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeDynamicPolymorphic, s.Type)

	// [4] GenFunc (Int) - We need to check if we captured two calls
	// Since findSite returns first match, let's verify count.
	genCalls := 0
	for _, r := range results {
		if r.Func == "GenFunc" {
			genCalls++
			assert.Equal(t, CallTypeStaticPolymorphic, r.Type)
			assert.NotEmpty(t, r.TypeArgs)
		}
	}
	assert.Equal(t, 2, genCalls)

	// [6] f (Closure)
	s = findSite("f")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeDynamicPolymorphic, s.Type)

	// [7] ExternalFunc
	s = findSite("ExternalFunc")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeMonomorphic, s.Type)
	assert.Equal(t, Origin3rdParty, s.Origin)

	// [8] ExternalFunc
	s = findSite("GetVariadicFunc")
	require.NotNil(t, s)
	assert.Equal(t, CallTypeStaticPolymorphic, s.Type)
	assert.Equal(t, OriginLocal, s.Origin)
}

func TestAnalyzerService_ErrorPropagation(t *testing.T) {
	dir := setupTestDir(t)

	// Create a syntax error file
	err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package main\nfunc broken( {"), 0644)
	require.NoError(t, err)

	svc := &AnalyzerService{Pattern: dir}

	errorCount := 0
	for _, err := range svc.Run(context.Background()) {
		if err != nil {
			errorCount++
			assert.Contains(t, err.Error(), "package load error")
		}
	}
	assert.Greater(t, errorCount, 0, "Expected at least one package load error due to syntax")
}

func TestAnalyzerService_Run_Generics_Detailed(t *testing.T) {
	dir := setupTestDir(t)

	code := `package main

type MySt struct { val int }
type I interface { M() }

// Single param
func G1[T any](t T) {}
// Dual param
func G2[A any, B any](a A, b B) {}

func main() {
	// --- OPTIMIZED (Stenciled) ---
	G1(10)             // [1] Basic (Int)
	G1(3.14)           // [2] Basic (Float)
	G1("str")          // [3] String
	G1([]int{1})       // [4] SliceBasic
	G1(func(){})       // [5] Func

	// --- DICTIONARY / GENERIC ---
	i := 10
	G1(&i)             // [6] Pointer
	G1(MySt{})         // [7] Struct
	var iface I
	G1(iface)          // [8] Interface
	arr := [1]int{1}
	G1(arr)            // [9] Array
	G1([]*int{})       // [10] SliceGeneric

	// --- MIXED ---
	G2(1, &i)          // [11] Basic, Pointer
	G2("s", []int{})   // [12] String, SliceBasic
}
`
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0644)
	require.NoError(t, err)

	results := collectResults(t, dir)

	// Filter for G1
	var g1 []CallSite
	var g2 []CallSite
	for _, r := range results {
		if r.Func == "G1" {
			g1 = append(g1, r)
		}
		if r.Func == "G2" {
			g2 = append(g2, r)
		}
	}

	assert.Equal(t, 10, len(g1), "Expected 10 calls to G1")
	assert.Equal(t, 2, len(g2), "Expected 2 calls to G2")

	// Helper to match TypeArgs
	assertTypes := func(c CallSite, expected ...StaticPolySubtypeE) {
		assert.Equal(t, CallTypeStaticPolymorphic, c.Type)
		assert.Equal(t, expected, c.TypeArgs, "TypeArgs mismatch at line %d", c.Line)
	}

	// We trust the order of execution in main matches result order (sequential parsing)
	// Alternatively, verify by contents.

	// [1] G1(10) -> Basic
	assertTypes(g1[0], SubtypeBasic)
	// [2] G1(3.14) -> Basic
	assertTypes(g1[1], SubtypeBasic)
	// [3] G1("str") -> String
	assertTypes(g1[2], SubtypeString)
	// [4] G1([]int) -> SliceBasic
	assertTypes(g1[3], SubtypeSliceBasic)
	// [5] G1(func) -> Func
	assertTypes(g1[4], SubtypeFunc)

	// [6] G1(&i) -> Pointer
	assertTypes(g1[5], SubtypePointer)
	// [7] G1(St) -> Struct
	assertTypes(g1[6], SubtypeStruct)
	// [8] G1(I) -> Interface
	assertTypes(g1[7], SubtypeInterface)
	// [9] G1(Arr) -> Array
	assertTypes(g1[8], SubtypeArray)
	// [10] G1([]*int) -> SliceGeneric
	assertTypes(g1[9], SubtypeSliceGeneric)

	// [11] G2(1, &i) -> Basic, Pointer
	assertTypes(g2[0], SubtypeBasic, SubtypePointer)
	// [12] G2("s", []int) -> String, SliceBasic
	assertTypes(g2[1], SubtypeString, SubtypeSliceBasic)
}
