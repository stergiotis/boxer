package videooutput_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/videooutput"
)

// TestHelpCorpusConforms guards the embedded help corpus that the carousel
// registers as the "Video output" book (ADR-0088): the //go:embed must index
// at least the readouts reference, and every doc must satisfy the
// documentation standard so the Help app never logs a front-matter warning
// for it. This catches the embed-path and front-matter regressions that
// build + vet cannot — the corpus is data, not code.
func TestHelpCorpusConforms(t *testing.T) {
	b, err := help.NewBook("Video output", help.MustSub(videooutput.HelpFS, "help"))
	if err != nil {
		t.Fatalf("NewBook over HelpFS: %v", err)
	}
	docs := b.Docs()
	if len(docs) == 0 {
		t.Fatal("help corpus is empty: //go:embed help indexed no .md docs")
	}
	if _, _, ok := b.Doc("readouts"); !ok {
		paths := make([]string, 0, len(docs))
		for _, d := range docs {
			paths = append(paths, d.Path)
		}
		t.Errorf("expected the readouts doc to be indexed; got %v", paths)
	}
	for _, p := range b.Validate() {
		t.Errorf("front-matter non-conformance in %q: field=%s value=%q: %s",
			p.DocPath, p.Field, p.Value, p.Message)
	}
}
