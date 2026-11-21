package codegen_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema/codegen"
)

// TestCheckedInOutputMatchesGenerator regenerates every runtime facts-schema
// artefact into a temporary directory and compares it with the committed file
// at its default path. These outputs are written by `runtimecodegen all`
// (scripts/dev/generate.sh), not by a test, so without this comparison a
// change to the leeway generators or the facts schema would leave them stale
// and still compile. It never writes into the tree: on a difference,
// regenerate with scripts/dev/generate.sh.
func TestCheckedInOutputMatchesGenerator(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root not found from %s: %v", thisFile, err)
	}
	cases := []struct {
		name     string
		path     string
		generate func(outPath string) error
	}{
		{"dml", codegen.DefaultDMLOutputPath, codegen.GenerateDML},
		{"dml-cbor", codegen.DefaultDMLCBOROutputPath, codegen.GenerateDMLCBOR},
		{"readaccess", codegen.DefaultReadAccessOutputPath, codegen.GenerateReadAccess},
		{"canonwire", codegen.DefaultCanonWireOutputPath, codegen.GenerateCanonWire},
		{"ddl", codegen.DefaultDDLOutputPath, codegen.GenerateDDL},
	}
	tmp := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := filepath.Join(tmp, c.name+".out.go")
			if err := c.generate(out); err != nil {
				t.Fatalf("generate: %v", err)
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("read generated: %v", err)
			}
			want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.path)))
			if err != nil {
				t.Fatalf("read committed: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s differs from what runtimecodegen %s generates now (%d bytes generated, %d committed); regenerate with scripts/dev/generate.sh",
					c.path, c.name, len(got), len(want))
			}
		})
	}
}
