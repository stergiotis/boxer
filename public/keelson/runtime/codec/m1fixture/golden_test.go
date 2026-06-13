package m1fixture_test

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/codec/factswrapper"
)

var updateGolden = flag.Bool("update", false, "rewrite fixture.out.go with the current generator output")

// TestGeneratorMatchesCheckedInOutput runs the keelson codec generator
// on fixture.go and asserts the bytes match the committed fixture.out.go.
// Run with `-update` to refresh the checked-in file after intentional
// generator changes.
func TestGeneratorMatchesCheckedInOutput(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	inputPath := filepath.Join(dir, "fixture.go")
	outputPath := filepath.Join(dir, "fixture.out.go")

	generated, err := factswrapper.FactsWrapper{}.Generate(inputPath, "")
	if err != nil {
		t.Fatalf("factswrapper.Generate: %v", err)
	}

	if *updateGolden {
		if err := os.WriteFile(outputPath, generated, 0644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		t.Logf("golden updated: %s (%d bytes)", outputPath, len(generated))
		return
	}

	want, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(generated) == string(want) {
		return
	}

	// Mismatch — write the generator's output to a sibling .new file so a
	// reviewer can diff it against the committed golden.
	newPath := outputPath + ".new"
	_ = os.WriteFile(newPath, generated, 0644)
	t.Fatalf("generator output differs from %s\n  generator length: %d\n  golden length:    %d\n  wrote: %s (diff with `diff -u`); rerun with -update to overwrite the golden",
		outputPath, len(generated), len(want), newPath)
}
