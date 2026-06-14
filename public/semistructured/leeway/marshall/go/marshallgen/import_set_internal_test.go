package marshallgen

import (
	"strings"
	"testing"
)

// TestImportSet_DedupsByPath pins the 2026-06-14 import-builder seam fix: the
// core and a WrapperEmitterI each declare the imports their own emitted code
// uses, and importSet collapses overlaps by path — so a wrapper may declare an
// import the core also declares without producing a duplicate-import compile
// error, and without either side reasoning about the other's gating.
func TestImportSet_DedupsByPath(t *testing.T) {
	s := newImportSet()
	s.group(`"iter"`, `"github.com/x/eb"`)                          // core-style group
	s.group(`"bytes"`, `"github.com/x/eb"`, `al "github.com/x/eb"`) // wrapper re-declares eb (plain + aliased)
	var sb strings.Builder
	s.render(&sb)
	out := sb.String()

	if got := strings.Count(out, `"github.com/x/eb"`); got != 1 {
		t.Errorf("eb path must appear exactly once, got %d:\n%s", got, out)
	}
	if !strings.Contains(out, `"iter"`) || !strings.Contains(out, `"bytes"`) {
		t.Errorf("non-overlapping imports must survive:\n%s", out)
	}

	// "" separators carry no path and must neither be emitted nor open a group.
	s2 := newImportSet()
	s2.group(``)           // empty group → not rendered
	s2.group(`"iter"`, ``) // blank dropped, group survives
	var sb2 strings.Builder
	s2.render(&sb2)
	if strings.Contains(sb2.String(), "\n\n\n") {
		t.Errorf("blank specs must not create stray groups:\n%q", sb2.String())
	}
}

// TestImportSpecPath covers the path extraction the dedup keys on.
func TestImportSpecPath(t *testing.T) {
	cases := map[string]string{
		`"iter"`:                 "iter",
		`cbdml "github.com/x/y"`: "github.com/x/y",
		`"github.com/a/b/c"`:     "github.com/a/b/c",
		``:                       "",
		`not an import`:          "",
	}
	for spec, want := range cases {
		if got := importSpecPath(spec); got != want {
			t.Errorf("importSpecPath(%q) = %q, want %q", spec, got, want)
		}
	}
}
