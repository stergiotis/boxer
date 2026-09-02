package transform

// The corpus gate (the sqlapplet §SD6 pattern): the embedded prompt book is
// held to zero parse errors here, so a prompt document that drifts out of the
// format fails CI rather than logging and vanishing from the picker at
// runtime.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

func TestStarterPromptCorpus(t *testing.T) {
	defs, errs := ParseBook("mdedit", help.MustSub(promptsFS, "prompts"))
	require.Empty(t, errs)
	require.GreaterOrEqual(t, len(defs), 3, "the starter book ships at least the three launch transformations")

	seen := map[string]bool{}
	for _, def := range defs {
		assert.NotEmpty(t, def.Title, "%s: title", def.Slug)
		assert.NotEmpty(t, def.Summary, "%s: summary", def.Slug)
		assert.NotEmpty(t, def.System, "%s: prompt body", def.Slug)
		assert.False(t, seen[def.Slug], "%s: duplicate slug", def.Slug)
		seen[def.Slug] = true
	}

	// The three launch prompts are durably public identity (renaming one is a
	// deprecation event, the applet-slug rule).
	for _, slug := range []string{"improve-style", "summarize", "translate-to-english"} {
		assert.True(t, seen[slug], "launch prompt %q is missing", slug)
	}
}

// TestStarterPromptScopes pins each launch prompt to the scope its wording
// assumes — a summary of a selection is not what the summarize prompt says it
// does.
func TestStarterPromptScopes(t *testing.T) {
	defs, errs := ParseBook("mdedit", help.MustSub(promptsFS, "prompts"))
	require.Empty(t, errs)
	want := map[string]ScopeE{
		"improve-style":        ScopeSelection,
		"summarize":            ScopeDocument,
		"translate-to-english": ScopeSelection,
	}
	for _, def := range defs {
		if scope, ok := want[def.Slug]; ok {
			assert.Equal(t, scope, def.Scope, def.Slug)
		}
	}
}
