package mdedit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// ---------------------------------------------------------------------------
// numberPrefixLen
// ---------------------------------------------------------------------------

func TestNumberPrefixLen(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want int
	}{
		{"single component with dot", "1. Title", 3},
		{"multi component", "2.3 Title", 4},
		{"multi component with trailing dot", "2.3. Title", 5},
		{"deep", "1.2.10 Title", 7},
		{"several spaces are part of the prefix", "1.  Title", 4},
		{"no dot is a year, not a prefix", "2024 review", 0},
		{"no space after is a word", "1.Title", 0},
		{"no digits", "Title", 0},
		{"dot alone", ". Title", 0},
		{"empty", "", 0},
		{"number is the whole string", "2.3", 0},
		// The accepted edge: the dot rule cannot tell a decimal from a
		// section number.
		{"decimal-looking title is stripped", "3.14 constants", 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, numberPrefixLen(tc.s))
		})
	}
}

// ---------------------------------------------------------------------------
// headingNumbers — the hierarchy rule
// ---------------------------------------------------------------------------

// parseHeadings runs the same parse the app derives everything from.
// markdown.Parse may leave the render goroutine (ADR-0178, Update 2026-08-09),
// which is what lets these tests run in the default lane.
func parseHeadings(t *testing.T, src string) (headings []markdown.HeadingInfo) {
	t.Helper()
	headings = markdown.Parse([]byte(src)).Headings()
	return
}

func TestHeadingNumbers_NestsLikeTheOutline(t *testing.T) {
	src := "# A\n\n## B\n\n### C\n\n## D\n\n# E\n"
	nums := headingNumbers(parseHeadings(t, src))
	assert.Equal(t, []string{"1.", "1.1", "1.1.1", "1.2", "2."}, nums)
}

func TestHeadingNumbers_SkippedLevelsNest(t *testing.T) {
	// "#" straight to "###": the ### is the #'s child, numbered x.1 — the
	// outline's own rule, not a strict level-equals-depth reading.
	src := "# A\n\n### B\n\n### C\n"
	nums := headingNumbers(parseHeadings(t, src))
	assert.Equal(t, []string{"1.", "1.1", "1.2"}, nums)
}

func TestHeadingNumbers_NoH1NumbersTheH2sAsRoots(t *testing.T) {
	src := "## A\n\n### B\n\n## C\n"
	nums := headingNumbers(parseHeadings(t, src))
	assert.Equal(t, []string{"1.", "1.1", "2."}, nums)
}

// ---------------------------------------------------------------------------
// renumberHeadings / stripHeadingNumbers
// ---------------------------------------------------------------------------

func TestRenumberHeadings_InsertsPrefixes(t *testing.T) {
	src := "# Alpha\n\nprose\n\n## Beta\n\n## Gamma\n"
	out, n := renumberHeadings(src, parseHeadings(t, src))
	assert.Equal(t, "# 1. Alpha\n\nprose\n\n## 1.1 Beta\n\n## 1.2 Gamma\n", out)
	assert.Equal(t, 3, n)
}

func TestRenumberHeadings_RefreshesStalePrefixes(t *testing.T) {
	// The document was numbered once, then a section was deleted; a second
	// pass rewrites what moved and reports only what changed.
	src := "# 1. Alpha\n\n## 1.2 Gamma\n"
	out, n := renumberHeadings(src, parseHeadings(t, src))
	assert.Equal(t, "# 1. Alpha\n\n## 1.1 Gamma\n", out)
	assert.Equal(t, 1, n)
}

func TestRenumberHeadings_IsIdempotent(t *testing.T) {
	src := "# Alpha\n\n### Skipped\n\n## Beta\n"
	once, n1 := renumberHeadings(src, parseHeadings(t, src))
	require.Positive(t, n1)
	twice, n2 := renumberHeadings(once, parseHeadings(t, once))
	assert.Equal(t, once, twice)
	assert.Zero(t, n2, "a renumbered document renumbers to itself")
}

func TestRenumberHeadings_LeavesFencesAlone(t *testing.T) {
	src := "# Alpha\n\n```\n# not a heading\n```\n\n## Beta\n"
	out, n := renumberHeadings(src, parseHeadings(t, src))
	assert.Contains(t, out, "```\n# not a heading\n```", "a fenced pseudo-heading is not numbered")
	assert.Equal(t, 2, n)
}

func TestRenumberHeadings_NumbersSetextHeadings(t *testing.T) {
	// Setext headings are exactly why the parse is the base and not the
	// source lexer, which reads them as prose (ADR-0178 §Consequences).
	src := "Alpha\n=====\n\nBeta\n----\n"
	headings := parseHeadings(t, src)
	require.Len(t, headings, 2)
	out, n := renumberHeadings(src, headings)
	assert.Equal(t, "1. Alpha\n=====\n\n1.1 Beta\n----\n", out)
	assert.Equal(t, 2, n)
}

func TestStripHeadingNumbers_RoundTripsARenumber(t *testing.T) {
	src := "# Alpha\n\n## Beta\n\n### Gamma\n\n## Delta\n"
	numbered, _ := renumberHeadings(src, parseHeadings(t, src))
	stripped, n := stripHeadingNumbers(numbered, parseHeadings(t, numbered))
	assert.Equal(t, src, stripped, "strip is the way back from renumber")
	assert.Equal(t, 4, n)
}

func TestStripHeadingNumbers_LeavesUnnumberedAlone(t *testing.T) {
	src := "# 2024 review\n\n## What happened\n"
	out, n := stripHeadingNumbers(src, parseHeadings(t, src))
	assert.Equal(t, src, out)
	assert.Zero(t, n)
}

// ---------------------------------------------------------------------------
// The deferred apply
// ---------------------------------------------------------------------------

func TestApplyPendingNumbering_RenumbersThroughTheRebind(t *testing.T) {
	inst := &App{src: "# Alpha\n\n## Beta\n"}
	inst.refreshDerived()
	inst.numberAction = numberActRenumber
	inst.applyPendingNumbering()

	assert.Equal(t, "# 1. Alpha\n\n## 1.1 Beta\n", inst.src)
	assert.True(t, inst.rebindSrc, "a numbering rewrite is a rebind, not typing")
	assert.Equal(t, numberActNone, inst.numberAction, "the gesture is consumed")
}

func TestApplyPendingNumbering_DropsAStaleGesture(t *testing.T) {
	// A file open landed in the same drain: the parse no longer describes the
	// buffer, and a gesture aimed at the old document must not fire at the
	// new one.
	inst := &App{src: "# Alpha\n"}
	inst.refreshDerived()
	inst.src = "# Replaced\n"
	inst.numberAction = numberActRenumber
	inst.applyPendingNumbering()

	assert.Equal(t, "# Replaced\n", inst.src, "the stale gesture is dropped")
	assert.False(t, inst.rebindSrc)
}

func TestApplyPendingNumbering_NoActionIsANoop(t *testing.T) {
	inst := &App{src: "# Alpha\n", status: "earlier"}
	inst.refreshDerived()
	inst.applyPendingNumbering()
	assert.Equal(t, "earlier", inst.status)
	assert.Equal(t, "# Alpha\n", inst.src)
}
