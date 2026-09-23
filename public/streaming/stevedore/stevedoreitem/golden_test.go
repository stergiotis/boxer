package stevedoreitem_test

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/codec/factswrapper"
)

var updateGolden = flag.Bool("update", false, "rewrite stevedoreitem.out.go with the current generator output")

// TestGeneratorMatchesCheckedInOutput runs the keelson codec generator on
// stevedoreitem.go against the stevedore vocabulary and asserts the bytes
// match the committed stevedoreitem.out.go. Rerun with `-update` after
// changing the DTO or the vocabulary.
func TestGeneratorMatchesCheckedInOutput(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	inputPath := filepath.Join(dir, "stevedoreitem.go")
	outputPath := filepath.Join(dir, "stevedoreitem.out.go")

	generated, err := factswrapper.FactsWrapper{
		VocabImportPath: "github.com/stergiotis/boxer/public/streaming/stevedore/stevedorevocab",
		VocabQualifier:  "stevedorevocab",
	}.Generate(inputPath, "")
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
	newPath := outputPath + ".new"
	_ = os.WriteFile(newPath, generated, 0644)
	t.Fatalf("generator output differs from %s; wrote %s; rerun with -update to overwrite the golden", outputPath, newPath)
}
