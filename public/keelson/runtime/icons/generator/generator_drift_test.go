package generator_test

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons/generator"
)

// TestCheckedInOutputMatchesGenerator runs the icon generator over the
// vendored phosphor-icons.json in memory and compares the result, gofmt-ed as
// icons/generate.sh leaves it, with the committed phosphor.out.go and
// phosphor_lookup.out.go. It never writes into the tree: on a difference,
// regenerate with scripts/dev/generate.sh.
func TestCheckedInOutputMatchesGenerator(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	iconsDir := filepath.Join(filepath.Dir(thisFile), "..")
	doc, err := os.ReadFile(filepath.Join(iconsDir, "phosphor-icons.json"))
	if err != nil {
		t.Fatalf("read catalogue: %v", err)
	}
	gen := generator.NewGenerator()
	if err = gen.Load(doc); err != nil {
		t.Fatalf("load: %v", err)
	}
	var consts, lookup bytes.Buffer
	if err = gen.GenerateGo(&consts, &lookup, "icons"); err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, c := range []struct {
		file string
		src  []byte
	}{
		{"phosphor.out.go", consts.Bytes()},
		{"phosphor_lookup.out.go", lookup.Bytes()},
	} {
		got, fErr := format.Source(c.src)
		if fErr != nil {
			t.Fatalf("%s: gofmt: %v", c.file, fErr)
		}
		want, rErr := os.ReadFile(filepath.Join(iconsDir, c.file))
		if rErr != nil {
			t.Fatalf("read committed: %v", rErr)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from what the generator emits now (%d bytes generated, %d committed); regenerate with scripts/dev/generate.sh",
				c.file, len(got), len(want))
		}
	}
}
