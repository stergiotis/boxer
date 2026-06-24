package introspecthttp

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// TestHelpFS asserts the //go:embed indexed the help corpus, so a renamed or
// moved help/ directory fails loudly rather than shipping an empty book.
func TestHelpFS(t *testing.T) {
	b, err := help.NewBook("Introspection tables", help.MustSub(HelpFS, "help"))
	if err != nil {
		t.Fatalf("NewBook over HelpFS: %v", err)
	}
	if len(b.Docs()) == 0 {
		t.Fatal("help corpus is empty: //go:embed help indexed no .md docs")
	}
}
