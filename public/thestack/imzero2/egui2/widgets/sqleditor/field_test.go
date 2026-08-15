package sqleditor

// The two pieces of [Field] that are not a draw call: the error overlay's
// clamp, and the lex job's per-edit memo.

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stretchr/testify/require"
)

func TestFieldMarkSectionsClampsToTheFragment(t *testing.T) {
	cases := []struct {
		name   string
		srcLen int
		mark   nanopass.SourceRange
		want   []codeview.StyledSection
	}{
		{"no mark", 10, nanopass.SourceRange{}, nil},
		{"inside", 10, nanopass.SourceRange{Start: 2, End: 5}, []codeview.StyledSection{
			{Start: 2, Stop: 5, Flags: codeview.StyleUnderline, Color: ToneError},
		}},
		{"exactly the fragment", 4, nanopass.SourceRange{Start: 0, End: 4}, []codeview.StyledSection{
			{Start: 0, Stop: 4, Flags: codeview.StyleUnderline, Color: ToneError},
		}},
		// The frame-late range: the user shortened the fragment, and the
		// embedder's range still describes the longer one.
		{"overruns the end", 5, nanopass.SourceRange{Start: 2, End: 99}, []codeview.StyledSection{
			{Start: 2, Stop: 5, Flags: codeview.StyleUnderline, Color: ToneError},
		}},
		{"wholly past the end", 5, nanopass.SourceRange{Start: 7, End: 9}, nil},
		{"empty fragment", 0, nanopass.SourceRange{Start: 0, End: 2}, nil},
		{"negative start", 10, nanopass.SourceRange{Start: -3, End: 4}, []codeview.StyledSection{
			{Start: 0, Stop: 4, Flags: codeview.StyleUnderline, Color: ToneError},
		}},
		// Empty() catches this before the clamp does; asserted so an inverted
		// range never reaches the builder as a degenerate section.
		{"inverted", 10, nanopass.SourceRange{Start: 5, End: 2}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, markSections(tc.srcLen, tc.mark))
		})
	}
}

// The memo state is the only honest evidence of a rebuild: two independent
// builds of one source already share a backing array (unique.Make interns the
// serialized bytes — see codeview's memo tests), so comparing holders would
// pass with the memo removed.
func TestFieldHighlightJobRebuildsOnlyOnChange(t *testing.T) {
	f := NewField()

	_, ok := f.highlightJob("")
	require.False(t, ok, "an empty fragment has no bytes to colour")
	require.False(t, f.jobOk, "and must not arm the memo")

	_, ok = f.highlightJob("a = 1")
	require.True(t, ok)
	require.True(t, f.jobOk)
	require.Equal(t, "a = 1", f.jobFor)

	_, ok = f.highlightJob("a = 2")
	require.True(t, ok)
	require.Equal(t, "a = 2", f.jobFor, "a changed fragment re-keys the memo")

	// Back to empty: the job declines, and the memo keeps the last fragment it
	// built rather than being invalidated — the next non-empty edit re-keys it
	// anyway, and clearing here would cost a rebuild on every cleared field.
	_, ok = f.highlightJob("")
	require.False(t, ok)
	require.Equal(t, "a = 2", f.jobFor)
}
