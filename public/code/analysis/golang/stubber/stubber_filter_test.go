//go:build llm_generated_gemini3pro

package stubber

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGoFilter_Compilation(t *testing.T) {
	// Complex source covering:
	// - Generics
	// - Private/Public mixes
	// - Struct tags
	// - Interface methods
	// - Global variables with private field initialization
	// - Unused imports
	src := `//go:build myflag
package main

import (
	"fmt"
	"net/http"
	"os"
)

// PublicConst remains.
const PublicConst = 1

// privateConst is removed.
const privateConst = 2

// PublicStruct is cleaned.
type PublicStruct struct {
	PublicField  int
	privateField int
}

// privateStruct is removed.
type privateStruct struct {
	data string
}

// PublicInterface is cleaned.
type PublicInterface interface {
	PublicMethod()
	privateMethod()
}

// PublicGeneric remains.
type PublicGeneric[T any] struct {
	Data T
}

// Global variable initialization.
var (
	// Valid: Lambda body should be stubbed.
	GlobalFunc = func() {
		fmt.Println("This should be gone")
	}

	// Valid: Composite literal with private keys should be sanitized.
	GlobalData = PublicStruct{
		PublicField:  10,
		privateField: 20,
	}

	// Invalid: Variable of private type should be removed.
	// This ensures we check ValueSpec.Type
	BadVar privateStruct

	// Valid: Public typed variable
	GoodVar int
)

// PublicFunc remains, body stubbed.
func PublicFunc() {
	fmt.Println("Hello")
}

// PrivateFunc is removed.
func privateFunc() {
	os.Exit(1)
}

// FuncWithPrivateArg is removed.
func FuncWithPrivateArg(p privateStruct) {}

// FuncWithPrivateReturn is removed.
func FuncWithPrivateReturn() *privateStruct { return nil }

// GenericFunc remains.
func GenericFunc[T any](t T) T {
	return t
}

// Unused import 'net/http' should be removed.
// 'os' is used only in privateFunc, so it should be removed too.
// 'fmt' is used in GlobalFunc (stubbed) and PublicFunc (stubbed), 
// so strictly 'fmt' should also be removed if stubs are empty panics!
// Our stub is panic("stub"), which uses no packages.
// So ALL imports should technically be removed.

func main() {
	PublicFunc()
}`

	// 1. Process
	inst := NewGoFilter("goFilterTag", false)
	var out bytes.Buffer
	err := inst.Process(context.Background(), "myfile.go", strings.NewReader(src), &out)
	require.NoError(t, err)

	outputCode := out.String()
	//fmt.Print(outputCode)

	// 2. Assertions on Text Content
	require.Contains(t, outputCode, "type PublicStruct struct {")
	require.Contains(t, outputCode, "PublicField int")
	require.NotContains(t, outputCode, "privateField int")

	require.Contains(t, outputCode, "type PublicInterface interface {")
	require.Contains(t, outputCode, "PublicMethod()")
	require.NotContains(t, outputCode, "privateMethod()")

	require.Contains(t, outputCode, "GlobalData = PublicStruct{")
	require.NotContains(t, outputCode, "privateField: 20") // KeyValueExpr sanitized

	require.NotContains(t, outputCode, "BadVar") // Private type var removed
	require.Contains(t, outputCode, "GoodVar")

	require.Contains(t, outputCode, `panic("stub")`)
	require.NotContains(t, outputCode, "os.Exit")

	require.Contains(t, outputCode, "//go:build myflag && goFilterTag")

	// 3. Compilation Check
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "gofilter_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Write output to file
	outFile := filepath.Join(tmpDir, "main.go")
	err = os.WriteFile(outFile, out.Bytes(), 0644)
	require.NoError(t, err)

	// Run 'go build'
	// We build into /dev/null (or discard) to just check compilation
	cmd := exec.Command("go", "build", "-o", os.DevNull, outFile)
	// cmd.CombinedOutput() is useful for debugging if it fails
	outBytes, err := cmd.CombinedOutput()
	require.NoError(t, err, "Compilation failed:\n%s", string(outBytes))
}

func TestGoFilter_DeletePrivate(t *testing.T) {
	// Covers: unreferenced private funcs/types/vars removed together with their
	// doc comments; private decls referenced from surviving public signatures,
	// type definitions or var/const initializers are kept (with bodies stubbed);
	// reachability is transitive across private → private references.
	src := `package stub

// PublicStruct references privateA, which transitively references privateB.
type PublicStruct struct {
	F privateA
}

// privateA is reachable via PublicStruct.F.
type privateA struct {
	G privateB
}

// privateB is reachable transitively through privateA.
type privateB int

// privateOrphanType is unreachable — this whole decl AND comment go.
type privateOrphanType struct{ x int }

// privateOrphanFunc is unreachable — removed along with this doc.
func privateOrphanFunc() {}

// privateOrphanConst is unreachable — removed along with this doc.
const privateOrphanConst = 99

// privateInit is called by a surviving public var initializer; must be kept.
func privateInit() int { return 42 }

// PublicInitVar keeps privateInit reachable.
var PublicInitVar = privateInit()

// PublicFunc is kept; its body stubbed. Takes a reachable private type.
func PublicFunc(p privateA) {}
`
	inst := NewGoFilter("", true)
	var out bytes.Buffer
	err := inst.Process(context.Background(), "stub.go", strings.NewReader(src), &out)
	require.NoError(t, err)
	outputCode := out.String()

	// Reachable privates are kept (type, func) with stubbed bodies.
	require.Contains(t, outputCode, "type privateA struct")
	require.Contains(t, outputCode, "type privateB int")
	require.Contains(t, outputCode, "func privateInit() int")
	require.Contains(t, outputCode, `panic("stub")`)
	require.Contains(t, outputCode, "PublicInitVar = privateInit()")

	// Unreachable privates and their doc comments are gone.
	require.NotContains(t, outputCode, "privateOrphanType")
	require.NotContains(t, outputCode, "privateOrphanFunc")
	require.NotContains(t, outputCode, "privateOrphanConst")
	require.NotContains(t, outputCode, "is unreachable")

	// Compilation check.
	tmpDir, err := os.MkdirTemp("", "gofilter_deletePrivate")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	outFile := filepath.Join(tmpDir, "stub.go")
	err = os.WriteFile(outFile, out.Bytes(), 0644)
	require.NoError(t, err)
	cmd := exec.Command("go", "build", "-o", os.DevNull, outFile)
	outBytes, err := cmd.CombinedOutput()
	require.NoError(t, err, "Compilation failed:\n%s", string(outBytes))
}
