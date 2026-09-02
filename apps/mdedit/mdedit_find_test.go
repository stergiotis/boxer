package mdedit

// Tests for find and replace (ADR-0178 M3).
//
// Everything the bar DOES is reachable without a frame — the buttons only call
// the helpers below — so the coverage here is over the logic rather than over
// the rendering. What is deliberately not covered: that egui honours a stored
// cursor range, and that the styled overlay lands where the split says it
// should. Both are on the far side of the FFI and were checked by driving a
// live window, which the ADR's verification plan records as the weakest part
// of the plan.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
)

// ---------------------------------------------------------------------------
// findMatches
// ---------------------------------------------------------------------------

func TestFindMatches_CaseSensitive(t *testing.T) {
	const src = "cat CAT cat"
	got := findMatches(src, "cat", true)
	require.Len(t, got, 2)
	assert.Equal(t, matchSpan{0, 3}, got[0])
	assert.Equal(t, matchSpan{8, 11}, got[1])
}

func TestFindMatches_EmptyInputsFindNothing(t *testing.T) {
	assert.Empty(t, findMatches("", "cat", true))
	assert.Empty(t, findMatches("cat", "", true))
	// An empty query with folding on must not loop either — a zero-width
	// match would never advance the scan.
	assert.Empty(t, findMatches("cat", "", false))
}

// TestFindMatches_NonOverlapping pins the scan resuming past a match rather
// than one byte on: "aa" in "aaaa" is two occurrences, not three.
func TestFindMatches_NonOverlapping(t *testing.T) {
	assert.Equal(t, []matchSpan{{0, 2}, {2, 4}}, findMatches("aaaa", "aa", true))
	assert.Equal(t, []matchSpan{{0, 2}, {2, 4}}, findMatches("aAaA", "aa", false))
}

// TestFindMatches_FoldedOffsetsIndexTheSource is the reason the fold is done
// rune by rune instead of over a lowercased copy: strings.ToLower can change a
// string's LENGTH, and offsets taken against the shorter or longer copy would
// not describe the buffer they are meant to index.
func TestFindMatches_FoldedOffsetsIndexTheSource(t *testing.T) {
	const src = "İstanbul and ÄPFEL and äpfel"
	got := findMatches(src, "äpfel", false)
	require.Len(t, got, 2)
	for _, m := range got {
		assert.True(t, strings.EqualFold("äpfel", src[m.Start:m.Stop]),
			"span %v holds %q", m, src[m.Start:m.Stop])
	}
	// The 'İ' above is the trap: it lowercases to TWO runes, so a folded-copy
	// implementation would have every later offset shifted by one byte.
	assert.Equal(t, "ÄPFEL", src[got[0].Start:got[0].Stop])
}

// TestFindMatches_FoldMeasuresTheSourceNotTheQuery covers the case where the
// match and the query are different byte lengths: 'K' (U+212A, three bytes)
// folds to 'k' (one byte), so a span sized from len(query) would run past the
// occurrence and cut the next character in half.
func TestFindMatches_FoldMeasuresTheSourceNotTheQuery(t *testing.T) {
	const src = "300Km walk" // U+212A KELVIN SIGN
	got := findMatches(src, "km", false)
	require.Len(t, got, 1)
	assert.Equal(t, "Km", src[got[0].Start:got[0].Stop])
}

func TestFindMatches_CaseSensitiveIsNotFolded(t *testing.T) {
	assert.Empty(t, findMatches("ÄPFEL", "äpfel", true))
}

// ---------------------------------------------------------------------------
// Index bookkeeping
// ---------------------------------------------------------------------------

func TestStepIndex_Wraps(t *testing.T) {
	assert.Equal(t, 1, stepIndex(0, 3, 1))
	assert.Equal(t, 0, stepIndex(2, 3, 1), "past the last match wraps to the first")
	assert.Equal(t, 2, stepIndex(0, 3, -1), "before the first wraps to the last")
	assert.Equal(t, 0, stepIndex(5, 0, 1), "an empty list has nowhere to step")
}

func TestMatchAtOrAfter(t *testing.T) {
	ms := []matchSpan{{10, 13}, {40, 43}, {80, 83}}
	assert.Equal(t, 0, matchAtOrAfter(ms, 0))
	assert.Equal(t, 1, matchAtOrAfter(ms, 11), "inside a match looks forward")
	assert.Equal(t, 1, matchAtOrAfter(ms, 40), "at a match start is that match")
	assert.Equal(t, 0, matchAtOrAfter(ms, 999), "past the end wraps to the first")
	assert.Equal(t, 0, matchAtOrAfter(nil, 50))
}

// TestEnsureMatches_KeepsTheReaderWhereTheyWere is the behaviour that makes
// typing into the find field bearable: each new character recomputes the whole
// list, and without an anchor the bar would throw the reader back to the top
// of the document on every keystroke.
func TestEnsureMatches_KeepsTheReaderWhereTheyWere(t *testing.T) {
	inst := &App{src: "one two one two one twain"}
	inst.find.query = "two"
	inst.ensureMatches()
	require.Len(t, inst.find.matches, 2)

	// Move to the second match, then extend the query so the first one drops
	// out of the list. The bar must stay on what is still nearby, not reset.
	inst.gotoMatch(1, true)
	inst.find.query = "twa"
	inst.ensureMatches()
	require.Len(t, inst.find.matches, 1)
	m, ok := inst.find.current()
	require.True(t, ok)
	assert.Equal(t, "twa", inst.src[m.Start:m.Stop])
}

func TestEnsureMatches_EmptyQueryHoldsNothing(t *testing.T) {
	inst := &App{src: "cat cat"}
	inst.find.query = "cat"
	inst.ensureMatches()
	require.Len(t, inst.find.matches, 2)

	inst.find.query = ""
	inst.ensureMatches()
	assert.Empty(t, inst.find.matches, "a cleared query must not keep a stale match list")
}

// TestEnsureMatches_FollowsTheBuffer covers the gate's third input: an edit
// invalidates the list even though the query did not move.
func TestEnsureMatches_FollowsTheBuffer(t *testing.T) {
	inst := &App{src: "cat"}
	inst.find.query = "cat"
	inst.ensureMatches()
	require.Len(t, inst.find.matches, 1)

	inst.src = "cat cat cat"
	inst.ensureMatches()
	assert.Len(t, inst.find.matches, 3)
}

// ---------------------------------------------------------------------------
// Painting
// ---------------------------------------------------------------------------

func TestPaintWindow_CentresOnTheCurrentMatch(t *testing.T) {
	lo, hi := paintWindow(10, 3, 100)
	assert.Equal(t, [2]int{0, 10}, [2]int{lo, hi}, "few enough matches paint whole")

	lo, hi = paintWindow(1000, 500, 100)
	assert.Equal(t, [2]int{450, 550}, [2]int{lo, hi})

	lo, hi = paintWindow(1000, 2, 100)
	assert.Equal(t, [2]int{0, 100}, [2]int{lo, hi}, "near the start the window clamps forward")

	lo, hi = paintWindow(1000, 998, 100)
	assert.Equal(t, [2]int{900, 1000}, [2]int{lo, hi}, "near the end it clamps back")

	// Whatever it clamps to, the current match must be inside it — the window
	// exists to keep the one the reader is on visible.
	for _, idx := range []int{0, 1, 49, 50, 51, 998, 999} {
		lo, hi := paintWindow(1000, idx, 100)
		assert.True(t, idx >= lo && idx < hi, "idx %d fell outside [%d,%d)", idx, lo, hi)
	}
}

func TestMatchSections_MarksTheCurrentOneDifferently(t *testing.T) {
	ms := []matchSpan{{0, 3}, {10, 13}}
	got := matchSections(ms, 1, maxPaintedMatches)
	require.Len(t, got, 2)
	assert.Equal(t, codeview.StyleBackground, got[0].Flags)
	assert.Equal(t, codeview.StyleBackground|codeview.StyleUnderline, got[1].Flags)
	assert.NotEqual(t, got[0].Color, got[1].Color, "the current match needs its own tone")
}

func TestMatchSections_EmptyListPaintsNothing(t *testing.T) {
	assert.Nil(t, matchSections(nil, 0, maxPaintedMatches))
}

// ---------------------------------------------------------------------------
// Colour-section splitting
// ---------------------------------------------------------------------------

// TestSplitSpansAt_CutsWithoutLosingCoverage is the invariant the colour tier
// rests on: the Rust normalizer gap-fills uncovered bytes and a LayoutJob that
// skips bytes drops glyphs, so a split that lost a byte would lose a character
// on screen.
func TestSplitSpansAt_CutsWithoutLosingCoverage(t *testing.T) {
	spans := []markdownhighlight.Span{
		{Start: 0, Stop: 2, Category: markdownhighlight.CategoryHeadingMarker},
		{Start: 2, Stop: 20, Category: markdownhighlight.CategoryPlain},
	}
	got := splitSpansAt(spans, []int{5, 8, 12})
	require.NotEmpty(t, got)

	assert.EqualValues(t, 0, got[0].Start)
	assert.EqualValues(t, 20, got[len(got)-1].Stop)
	for i := 1; i < len(got); i++ {
		assert.Equal(t, got[i-1].Stop, got[i].Start, "spans must stay contiguous")
	}
	// Cutting changes where sections begin, never what colour they are.
	for _, s := range got {
		want := markdownhighlight.CategoryPlain
		if s.Start < 2 {
			want = markdownhighlight.CategoryHeadingMarker
		}
		assert.Equal(t, want, s.Category)
	}
}

// TestSplitSpansAt_PutsAMatchInItsOwnSection is the whole point of the split:
// a styled overlay is merged into every colour section it OVERLAPS, so unless
// the match's bytes are a section of their own the background reaches the
// entire prose run the lexer coalesced around it.
func TestSplitSpansAt_PutsAMatchInItsOwnSection(t *testing.T) {
	src := "a long paragraph mentioning cats in the middle of it"
	i := strings.Index(src, "cats")
	require.Positive(t, i)
	spans := []markdownhighlight.Span{{Start: 0, Stop: int32(len(src)), Category: markdownhighlight.CategoryPlain}}

	got := splitSpansAt(spans, matchCuts([]matchSpan{{i, i + 4}}, 0, maxPaintedMatches))

	found := false
	for _, s := range got {
		if int(s.Start) == i && int(s.Stop) == i+4 {
			found = true
		}
	}
	assert.True(t, found, "the match must be its own section; got %v", got)
}

func TestSplitSpansAt_NoCutsIsIdentity(t *testing.T) {
	spans := []markdownhighlight.Span{{Start: 0, Stop: 9, Category: markdownhighlight.CategoryPlain}}
	assert.Equal(t, spans, splitSpansAt(spans, nil))
}

// TestSplitSpansAt_CutsOnExistingBoundariesChangeNothing guards against the
// split emitting empty sections, which the normalizer drops but which would
// still have been paid for on the wire.
func TestSplitSpansAt_CutsOnExistingBoundariesChangeNothing(t *testing.T) {
	spans := []markdownhighlight.Span{
		{Start: 0, Stop: 4, Category: markdownhighlight.CategoryPlain},
		{Start: 4, Stop: 9, Category: markdownhighlight.CategoryHeadingText},
	}
	assert.Equal(t, spans, splitSpansAt(spans, []int{0, 4, 9}))
}

// TestMatchCuts_AreAscendingAndDeduplicated is splitSpansAt's precondition:
// the single pass over the cut list assumes ascending order, and back-to-back
// matches would otherwise repeat the offset where one ends and the next
// begins.
func TestMatchCuts_AreAscendingAndDeduplicated(t *testing.T) {
	cuts := matchCuts([]matchSpan{{0, 2}, {2, 4}, {9, 11}}, 0, maxPaintedMatches)
	assert.Equal(t, []int{0, 2, 4, 9, 11}, cuts)
}

// ---------------------------------------------------------------------------
// Replacing
// ---------------------------------------------------------------------------

func TestReplaceSpans(t *testing.T) {
	const src = "one cat, two cat, three cat"
	ms := findMatches(src, "cat", true)
	require.Len(t, ms, 3)

	got, n := replaceSpans(src, ms, "dog")
	assert.Equal(t, "one dog, two dog, three dog", got)
	assert.Equal(t, 3, n)

	// An empty replacement deletes.
	got, n = replaceSpans(src, ms, "")
	assert.Equal(t, "one , two , three ", got)
	assert.Equal(t, 3, n)
}

func TestReplaceSpans_NothingToDo(t *testing.T) {
	got, n := replaceSpans("unchanged", nil, "x")
	assert.Equal(t, "unchanged", got)
	assert.Zero(t, n)
}

// TestReplaceSpans_SkipsSpansTheBufferDoesNotHold covers the defensive arm: a
// span list computed against a different buffer would splice at offsets that
// mean nothing in this one. Losing a replacement is recoverable; corrupting
// the document is not.
func TestReplaceSpans_SkipsSpansTheBufferDoesNotHold(t *testing.T) {
	got, n := replaceSpans("short", []matchSpan{{0, 2}, {99, 120}}, "X")
	assert.Equal(t, "Xort", got)
	assert.Equal(t, 1, n)
}

// TestReplaceSpans_LongerReplacementDoesNotShiftLaterSpans pins that the
// splice reads offsets against the ORIGINAL buffer throughout — the bug a
// left-to-right rewrite invites is applying the second span to the string the
// first one already lengthened.
func TestReplaceSpans_LongerReplacementDoesNotShiftLaterSpans(t *testing.T) {
	const src = "a-b-c"
	got, n := replaceSpans(src, findMatches(src, "-", true), "___")
	assert.Equal(t, "a___b___c", got)
	assert.Equal(t, 2, n)
}

// ---------------------------------------------------------------------------
// The app-level replace paths
// ---------------------------------------------------------------------------

func TestReplaceCurrent_RewritesOneAndStaysInPlace(t *testing.T) {
	inst := &App{src: "cat cat cat"}
	inst.find.query = "cat"
	inst.find.replacement = "dog"
	inst.ensureMatches()
	inst.gotoMatch(1, true)

	inst.replaceCurrent()

	assert.Equal(t, "cat dog cat", inst.src)
	assert.True(t, inst.rebindSrc, "a buffer the app wrote needs the databinding override")
	assert.True(t, inst.dirty(), "a replacement leaves the document modified")

	// The list is stale until the next refresh; once refreshed, the index
	// lands on what is now in the replaced match's place — the NEXT match, so
	// a replacement containing the query cannot re-find itself.
	inst.ensureMatches()
	require.Len(t, inst.find.matches, 2)
	m, ok := inst.find.current()
	require.True(t, ok)
	assert.Equal(t, 8, m.Start, "the bar must move past what it just wrote")
}

// TestReplaceCurrent_ResumesPastWhatItWrote is the trap the resume offset
// exists for: replacing "cat" with "cats" leaves a match at the very offset
// the replacement started at, so a bar that re-anchored on the replaced
// match's own start would sit on the text it had just produced and grow it by
// one "s" per click.
//
// One pass over the document is what this asserts. Clicking on past the end
// wraps and does find the replacements — that is what a wrapping find does
// everywhere, and the alternative is remembering which bytes this session
// wrote.
func TestReplaceCurrent_ResumesPastWhatItWrote(t *testing.T) {
	inst := &App{src: "cat cat"}
	inst.find.query = "cat"
	inst.find.replacement = "cats"
	inst.ensureMatches()
	inst.gotoMatch(0, true)

	inst.replaceCurrent()
	inst.ensureMatches()
	require.Equal(t, "cats cat", inst.src)

	inst.replaceCurrent()
	inst.ensureMatches()
	assert.Equal(t, "cats cats", inst.src, "the second click must not land inside the first")
}

func TestReplaceCurrent_WithNoMatchDoesNothing(t *testing.T) {
	inst := &App{src: "unchanged"}
	inst.find.query = "absent"
	inst.find.replacement = "x"
	inst.ensureMatches()

	inst.replaceCurrent()

	assert.Equal(t, "unchanged", inst.src)
	assert.False(t, inst.rebindSrc, "nothing was written, so nothing needs rebinding")
}

func TestReplaceAll(t *testing.T) {
	inst := &App{src: "cat CAT cat"}
	inst.find.query = "cat"
	inst.find.replacement = "dog"
	inst.find.matchCase = true
	inst.ensureMatches()

	inst.replaceAll()

	assert.Equal(t, "dog CAT dog", inst.src)
	assert.True(t, inst.rebindSrc)
	assert.Equal(t, "replaced 2", inst.status)
}

func TestReplaceAll_Folded(t *testing.T) {
	inst := &App{src: "cat CAT Cat"}
	inst.find.query = "cat"
	inst.find.replacement = "dog"
	inst.ensureMatches()

	inst.replaceAll()

	assert.Equal(t, "dog dog dog", inst.src)
}

// TestRebindBuffer_NoopOnAnIdenticalBuffer keeps a replacement that changed
// nothing — replacing a word with itself — from raising the override and the
// dirty marker over an edit that did not happen.
func TestRebindBuffer_NoopOnAnIdenticalBuffer(t *testing.T) {
	inst := &App{src: "cat", saved: "cat"}
	inst.rebindBuffer("cat")
	assert.False(t, inst.rebindSrc)
	assert.False(t, inst.dirty())
}

// ---------------------------------------------------------------------------
// Navigation
// ---------------------------------------------------------------------------

// TestGotoMatch_SelectsTheMatchAndTakesFocus pins both halves of what a
// navigation gesture asks for: the caret SELECTS the match rather than sitting
// at its start, and it asks for focus, because an unfocused TextEdit paints
// neither caret nor selection and the reader would see nothing happen.
func TestGotoMatch_SelectsTheMatchAndTakesFocus(t *testing.T) {
	inst := &App{src: "one cat two cat"}
	inst.find.query = "cat"
	inst.ensureMatches()
	require.Len(t, inst.find.matches, 2)

	inst.gotoMatch(1, true)

	require.True(t, inst.pendingCaretOk)
	assert.Equal(t, caretRequest{Start: 12, Stop: 15, Focus: true}, inst.pendingCaret)
	assert.Equal(t, "cat", inst.src[inst.pendingCaret.Start:inst.pendingCaret.Stop])
}

// TestGotoMatch_ScrollsThePreviewToTheMatchSection is the compensation for
// what setCursor cannot do. It positions the caret but cannot reveal it — the
// seam reports no geometry for a byte range — so the preview going to the
// match's section is the only thing that shows the reader where they were
// sent.
func TestGotoMatch_ScrollsThePreviewToTheMatchSection(t *testing.T) {
	inst := &App{src: headingDoc}
	inst.doc = markdown.Parse([]byte(headingDoc))
	inst.find.query = "body two"
	inst.ensureMatches()
	require.Len(t, inst.find.matches, 1)

	inst.gotoMatch(0, true)

	assert.Equal(t, "second", inst.pendingScroll)
	assert.Equal(t, "", inst.caretSlug,
		"the caret has not moved yet — claiming its section now drags the preview back")
}

// TestGotoMatch_WithNoMatchesIsANoop keeps the two navigation buttons safe to
// click against an empty result, where stepIndex reports 0 and there is no
// match 0 to go to.
func TestGotoMatch_WithNoMatchesIsANoop(t *testing.T) {
	inst := &App{src: "prose"}
	inst.gotoMatch(0, true)
	assert.False(t, inst.pendingCaretOk)
}

// TestGotoMatch_WithoutFocusLeavesTheField pins the Enter path's half of the
// ADR-0130 focus rule: stepping from inside the query field must not pull
// focus into the source, or the second Enter would type a newline into the
// document instead of stepping again.
func TestGotoMatch_WithoutFocusLeavesTheField(t *testing.T) {
	inst := &App{src: "one cat two cat"}
	inst.find.query = "cat"
	inst.ensureMatches()
	require.Len(t, inst.find.matches, 2)

	inst.gotoMatch(1, false)

	require.True(t, inst.pendingCaretOk)
	assert.False(t, inst.pendingCaret.Focus)
}

// TestFindKeyDelta maps the captured keys the query field eats to match steps.
// The capture mask matches Enter ALONE (ADR-0177 SD5), so Shift+Enter arrives
// as Enter with the shift bit set — the mapping is where the two part ways.
func TestFindKeyDelta(t *testing.T) {
	cases := []struct {
		name  string
		key   c.CapturedKey
		delta int
		ok    bool
	}{
		{"enter advances", c.CapturedKey{Code: keycodes.Enter}, 1, true},
		{"shift+enter goes back", c.CapturedKey{Code: keycodes.Enter, Mods: 1}, -1, true},
		{"ctrl+enter still steps", c.CapturedKey{Code: keycodes.Enter, Mods: 2}, 1, true},
		{"another key steps nothing", c.CapturedKey{Code: keycodes.Escape}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delta, ok := findKeyDelta(tc.key)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.delta, delta)
		})
	}
}

// ---------------------------------------------------------------------------
// The readout
// ---------------------------------------------------------------------------

func TestFindSummary(t *testing.T) {
	inst := &App{src: "cat cat cat"}
	assert.Equal(t, "", inst.findSummary(), "no query, nothing to report")

	inst.find.query = "absent"
	inst.ensureMatches()
	assert.Equal(t, "no matches", inst.findSummary())

	inst.find.query = "cat"
	inst.ensureMatches()
	inst.gotoMatch(1, true)
	assert.Equal(t, "2 of 3", inst.findSummary())
}

// TestTourQueryMatchesTheScene guards the demo fixture. The scene's whole
// contribution to M3's coverage is a capture showing both match tones, and a
// query that stopped matching would leave the bar empty in every screenshot
// without failing anything.
func TestTourQueryMatchesTheScene(t *testing.T) {
	assert.Len(t, findMatches(sampleDoc, tourQuery, false), 2,
		"the tour needs a current match AND another one to differ from")
}

// TestFindSummary_SaysWhenThePaintingIsBounded is the no-silent-caps rule: the
// overlay is windowed, and a count that read "1 of 5000" beside 400 painted
// matches would say the highlighting is complete when it is not.
func TestFindSummary_SaysWhenThePaintingIsBounded(t *testing.T) {
	inst := &App{src: strings.Repeat("a ", maxPaintedMatches+50)}
	inst.find.query = "a"
	inst.ensureMatches()
	require.Greater(t, len(inst.find.matches), maxPaintedMatches)
	assert.Contains(t, inst.findSummary(), "nearby")
}
