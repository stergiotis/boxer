package sqleditor

// The pieces of [Field] that are not a draw call: the error overlay's clamp,
// the lex job's per-edit memo, and the newline fold that holds a one-row field
// to one row.

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

func TestFoldNewlinesHoldsAOneRowFieldToOneRow(t *testing.T) {
	// A single-line TextEdit refuses Enter but not a PASTE, so these are the
	// shapes a multi-line SQL fragment actually arrives in.
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no line break is returned unchanged", "planes_mercator_sample100", "planes_mercator_sample100"},
		{"empty", "", ""},
		{"unix", "SELECT 1\nFROM t", "SELECT 1 FROM t"},
		{"windows folds to ONE space", "SELECT 1\r\nFROM t", "SELECT 1 FROM t"},
		{"classic mac", "SELECT 1\rFROM t", "SELECT 1 FROM t"},
		{"mixed, several", "a\nb\r\nc\rd", "a b c d"},
		{"a break is a space, not a deletion -- tokens must not fuse", "FROM\nt", "FROM t"},
		{"blank lines each become a space", "a\n\n\nb", "a   b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, foldNewlines(tc.in))
		})
	}
}

func TestFoldNewlinesLeavesOtherWhitespaceAlone(t *testing.T) {
	// Only line breaks are folded. Tabs and runs of spaces are the fragment's
	// own formatting and survive, so a round trip through the field does not
	// quietly reformat what the user typed.
	const in = "a\t b  c"
	require.Equal(t, in, foldNewlines(in))
}
