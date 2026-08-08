package mdedit

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
)

// ---------------------------------------------------------------------------
// charToByte
// ---------------------------------------------------------------------------

func TestCharToByte(t *testing.T) {
	const ascii = "hello"
	const mixed = "héllo wörld" // é and ö are two bytes each

	cases := []struct {
		name    string
		src     string
		charOff int
		want    int
	}{
		{"zero", ascii, 0, 0},
		{"negative clamps to zero", ascii, -3, 0},
		{"ascii middle", ascii, 3, 3},
		{"ascii end", ascii, 5, 5},
		{"past end clamps to len", ascii, 99, 5},
		{"empty source", "", 4, 0},
		{"multibyte before", mixed, 1, 1},
		// char 2 is the 'l' after "hé": h=1 byte, é=2 bytes.
		{"multibyte after", mixed, 2, 3},
		{"multibyte end", mixed, 11, len(mixed)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := charToByte(tc.src, tc.charOff); got != tc.want {
				t.Errorf("charToByte(%q, %d): got %d want %d", tc.src, tc.charOff, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// lineStart
// ---------------------------------------------------------------------------

func TestLineStart(t *testing.T) {
	const src = "alpha\nbeta\ngamma"

	cases := []struct {
		name string
		off  int
		want int
	}{
		{"first line start", 0, 0},
		{"first line middle", 3, 0},
		{"newline itself belongs to the line it ends", 5, 0},
		{"second line start", 6, 6},
		{"second line middle", 8, 6},
		{"third line", 13, 11},
		{"negative clamps", -4, 0},
		{"past end clamps", 999, 11},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lineStart(src, tc.off); got != tc.want {
				t.Errorf("lineStart(%d): got %d want %d", tc.off, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// headingSlugAt
// ---------------------------------------------------------------------------

// headingDoc is a source whose heading TEXT offsets are deliberately a couple
// of bytes past each line start, which is what markdown.HeadingInfo reports.
const headingDoc = "intro prose\n\n# First\n\nbody one\n\n## Second\n\nbody two\n"

// headingsOf builds the HeadingInfo slice the markdown package would produce
// for headingDoc: ByteOffset points at the heading text, not at the `#`.
func headingsOf() (hs []markdown.HeadingInfo) {
	hs = []markdown.HeadingInfo{
		{Text: "First", Slug: "first", Level: 1, ByteOffset: 15},   // after "# "
		{Text: "Second", Slug: "second", Level: 2, ByteOffset: 35}, // after "## "
	}
	return
}

// TestHeadingFixtureOffsets guards the hand-computed offsets above: a drifting
// fixture would make every assertion below vacuous rather than failing.
func TestHeadingFixtureOffsets(t *testing.T) {
	for _, h := range headingsOf() {
		got := headingDoc[h.ByteOffset : h.ByteOffset+len(h.Text)]
		if got != h.Text {
			t.Errorf("heading %q: byte %d holds %q", h.Text, h.ByteOffset, got)
		}
	}
}

func TestHeadingSlugAt(t *testing.T) {
	hs := headingsOf()

	cases := []struct {
		name string
		off  int
		want string
	}{
		{"before any heading is the doc-level section", 3, ""},
		{"on the heading marker resolves to that heading", 13, "first"},
		{"on the heading text", 16, "first"},
		{"inside the first body", 25, "first"},
		{"on the second marker", 34, "second"},
		{"inside the second body", 48, "second"},
		{"past the end stays in the last section", 999, "second"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := headingSlugAt(headingDoc, hs, tc.off); got != tc.want {
				t.Errorf("headingSlugAt(%d): got %q want %q", tc.off, got, tc.want)
			}
		})
	}
}

// TestHeadingSlugAt_MarkerNormalisation pins the reason lineStart exists: a
// caret on the `#` sits BEFORE the reported heading offset, and without
// normalising to the line start it would resolve to the previous section —
// the preview jumping backwards exactly when the reader clicks into a heading.
func TestHeadingSlugAt_MarkerNormalisation(t *testing.T) {
	hs := headingsOf()
	markerOff := 13 // the '#' of "# First"
	if headingDoc[markerOff] != '#' {
		t.Fatalf("fixture drift: byte %d is %q, want '#'", markerOff, headingDoc[markerOff])
	}
	if got := headingSlugAt(headingDoc, hs, markerOff); got != "first" {
		t.Errorf("caret on the heading marker: got %q want %q", got, "first")
	}
}

// TestHeadingSlugAt_DegenerateHeading covers the `##`-alone case, which the
// markdown package reports with ByteOffset -1 and an empty slug. It names no
// section, so it must not shadow the heading above it.
func TestHeadingSlugAt_DegenerateHeading(t *testing.T) {
	hs := []markdown.HeadingInfo{
		{Text: "First", Slug: "first", Level: 1, ByteOffset: 15},
		{Text: "", Slug: "", Level: 2, ByteOffset: -1},
	}
	if got := headingSlugAt(headingDoc, hs, 40); got != "first" {
		t.Errorf("degenerate heading: got %q want %q", got, "first")
	}
}

func TestHeadingSlugAt_NoHeadings(t *testing.T) {
	if got := headingSlugAt("just prose\n", nil, 4); got != "" {
		t.Errorf("no headings: got %q want empty", got)
	}
}

// ---------------------------------------------------------------------------
// scrollTarget — the re-scroll guard
// ---------------------------------------------------------------------------

func TestScrollTarget_ReportsChangeOnlyOnTransition(t *testing.T) {
	hs := headingsOf()

	// Caret inside the first section, nothing scrolled yet.
	slug, changed := scrollTarget(headingDoc, hs, 20, "")
	if slug != "first" || !changed {
		t.Fatalf("entering a section: got (%q, %v) want (first, true)", slug, changed)
	}

	// Same section again — this is the frame that must NOT re-scroll, or the
	// reader can never scroll the preview by hand.
	slug, changed = scrollTarget(headingDoc, hs, 24, "first")
	if slug != "first" || changed {
		t.Fatalf("staying in a section: got (%q, %v) want (first, false)", slug, changed)
	}

	// Moving into the next section transitions again.
	slug, changed = scrollTarget(headingDoc, hs, 48, "first")
	if slug != "second" || !changed {
		t.Fatalf("moving on: got (%q, %v) want (second, true)", slug, changed)
	}
}

// TestScrollTarget_LeavingIntoDocLevelSection pins the degenerate direction:
// moving the caret above the first heading is a real transition to the
// document-level section, whose empty slug markdown.WithScrollToSection then
// treats as a no-op. The preview stays put rather than scrolling to nowhere.
func TestScrollTarget_LeavingIntoDocLevelSection(t *testing.T) {
	slug, changed := scrollTarget(headingDoc, headingsOf(), 2, "first")
	if slug != "" || !changed {
		t.Fatalf("got (%q, %v) want (\"\", true)", slug, changed)
	}
}

// TestScrollTarget_CaretIsCharOffsets guards the unit boundary: the caret
// arrives in CHAR offsets and the headings are in BYTES, so a document with
// multibyte text above the heading would resolve to the wrong section if the
// conversion were dropped.
func TestScrollTarget_CaretIsCharOffsets(t *testing.T) {
	const src = "ααα\n\n# Head\n\nbody\n" // each α is two bytes
	headOff := 10                         // byte offset of "Head"
	if src[headOff:headOff+4] != "Head" {
		t.Fatalf("fixture drift: %q", src[headOff:])
	}
	hs := []markdown.HeadingInfo{{Text: "Head", Slug: "head", Level: 1, ByteOffset: headOff}}

	// Char offset 2 is inside the alphas — byte offset 4, above the heading.
	if slug, _ := scrollTarget(src, hs, 2, ""); slug != "" {
		t.Errorf("caret above the heading: got %q want empty", slug)
	}
	// Char offset 9 is inside "Head" — byte offset 12, below it.
	if slug, _ := scrollTarget(src, hs, 9, ""); slug != "head" {
		t.Errorf("caret in the heading: got %q want %q", slug, "head")
	}
}

// ---------------------------------------------------------------------------
// dirty / checkpoint
// ---------------------------------------------------------------------------

func TestDirty(t *testing.T) {
	inst := &App{}
	if inst.dirty() {
		t.Error("an empty buffer at its checkpoint is not modified")
	}
	inst.src = "typed"
	if !inst.dirty() {
		t.Error("an edit away from the checkpoint is modified")
	}
	inst.saved = "typed"
	if inst.dirty() {
		t.Error("a buffer equal to its checkpoint is not modified")
	}
}

// ---------------------------------------------------------------------------
// drainAsync
// ---------------------------------------------------------------------------

// TestDrainAsync_ExportCheckpointsWhatLanded is the case the checkpoint exists
// for: the reader keeps typing while the clipboard request is in flight, so
// what landed is NOT what the buffer holds when the reply arrives. Checkpoint
// to the text that was sent, leaving the later edits correctly marked
// modified.
func TestDrainAsync_ExportCheckpointsWhatLanded(t *testing.T) {
	inst := &App{src: "hello world", saved: ""}
	inst.exportDone = true
	inst.exportedText = "hello"

	inst.drainAsync()

	if inst.saved != "hello" {
		t.Errorf("checkpoint: got %q want %q", inst.saved, "hello")
	}
	if !inst.dirty() {
		t.Error("text typed while the copy was in flight must stay modified")
	}
	if inst.status != "copied to clipboard" {
		t.Errorf("status: got %q", inst.status)
	}
	if inst.exportDone {
		t.Error("the completion must be consumed, or it reapplies every frame")
	}
}

func TestDrainAsync_ExportFailureLeavesCheckpoint(t *testing.T) {
	inst := &App{src: "hello", saved: ""}
	inst.exportDone = true
	inst.exportedText = "hello"
	inst.exportErr = errors.New("no responder")

	inst.drainAsync()

	if inst.saved != "" {
		t.Errorf("a failed copy must not checkpoint: got %q", inst.saved)
	}
	if !inst.dirty() {
		t.Error("a failed copy leaves the document modified")
	}
	if inst.status == "" {
		t.Error("a failed copy must say so")
	}
}

// TestDrainAsync_PersistFailureRetries pins the throttle's retry path:
// persistedSrc only advances on success, so a failed persist leaves the buffer
// unequal to it and the next tick tries again.
func TestDrainAsync_PersistFailureRetries(t *testing.T) {
	inst := &App{src: "doc"}
	inst.persistDone = true
	inst.persistedText = "doc"
	inst.persistErr = errors.New("store down")

	inst.drainAsync()

	if inst.persistedSrc != "" {
		t.Errorf("a failed persist must not advance persistedSrc: got %q", inst.persistedSrc)
	}

	inst.persistDone = true
	inst.persistedText = "doc"
	inst.persistErr = nil
	inst.drainAsync()

	if inst.persistedSrc != "doc" {
		t.Errorf("a successful persist advances persistedSrc: got %q", inst.persistedSrc)
	}
}

// ---------------------------------------------------------------------------
// Split sizing
// ---------------------------------------------------------------------------

// TestSourceWidth_TracksTheWindow is the regression guard on the defect this
// sizing exists for: egui's resizable SidePanel clamps its retained width when
// the window shrinks and never grows it back, so a squeezed pane stayed a
// ribbon once the window was wide again. Deriving the width from the measured
// window every frame means every size maps to a share and nothing is retained
// to get stuck.
func TestSourceWidth_TracksTheWindow(t *testing.T) {
	inst := &App{}

	inst.winW = 1200
	wide := inst.sourceWidth()
	assert.InDelta(t, 1200*sourceSplitFrac, wide, 0.01)

	// Squeezed, then restored: the wide answer must come back exactly.
	inst.winW = 400
	narrow := inst.sourceWidth()
	assert.Less(t, narrow, wide)

	inst.winW = 1200
	assert.Equal(t, wide, inst.sourceWidth(), "the split must recover when the window does")
}

func TestSourceWidth_NarrowWindowKeepsBothPanes(t *testing.T) {
	inst := &App{}
	// Below the floor, the source may take at most half — a floor that ignored
	// the window would starve the preview instead of the source.
	inst.winW = 300
	assert.LessOrEqual(t, inst.sourceWidth(), float32(150))
	assert.Greater(t, inst.sourceWidth(), float32(0))
}

func TestSourceWidth_BeforeTheProbeReports(t *testing.T) {
	inst := &App{}
	// winW is zero on the first frame; the fallback must still be positive and
	// leave room for the preview.
	got := inst.sourceWidth()
	assert.Greater(t, got, float32(0))
	assert.Less(t, got, windowFallbackWidthPx)
}

// ---------------------------------------------------------------------------
// Source highlighting (M1)
// ---------------------------------------------------------------------------

func TestEnsureLex_CachesUntilTheTextChanges(t *testing.T) {
	inst := &App{}

	// An empty buffer has nothing to colour; the method is skipped so the hint
	// text keeps rendering as it does without a job.
	inst.ensureLex()
	if _, ok := inst.highlightJob(); ok {
		t.Fatal("an empty buffer should not produce a highlight job")
	}

	inst.src = "# Head\n\n**bold**\n"
	inst.ensureLex()
	if _, ok := inst.highlightJob(); !ok {
		t.Fatal("a non-empty buffer should produce a highlight job")
	}
	assert.Equal(t, inst.src, inst.lexSrc, "the cache must record the text it describes")
	assert.NotEmpty(t, inst.lexSpans, "the spans must be kept — the readout reads them")

	// Unchanged text reuses the cached job rather than relexing.
	inst.ensureLex()
	assert.Equal(t, inst.src, inst.lexSrc)

	// An edit invalidates it.
	inst.src = "# Head\n\n**bolder**\n"
	inst.ensureLex()
	if _, ok := inst.highlightJob(); !ok {
		t.Fatal("an edited buffer should produce a highlight job")
	}
	assert.Equal(t, inst.src, inst.lexSrc, "the cache must follow the buffer")

	// Clearing the buffer clears everything derived from it, so no stale job,
	// spans or readout can outlive the text they described.
	inst.src = ""
	inst.ensureLex()
	if _, ok := inst.highlightJob(); ok {
		t.Fatal("clearing the buffer should drop the job")
	}
	assert.Equal(t, "", inst.lexSrc)
	assert.Empty(t, inst.lexSpans)
	assert.Equal(t, docStats{}, inst.stats)
}

// ---------------------------------------------------------------------------
// Formatting bar (M2)
// ---------------------------------------------------------------------------

func actionByKey(t *testing.T, key string) (act formatAction) {
	t.Helper()
	for _, a := range formatActions {
		if a.key == key {
			return a
		}
	}
	t.Fatalf("no format action %q", key)
	return
}

// TestFormatSnippet_WrapsTheSelection is the behaviour insertAtCursor makes
// possible: the splice REPLACES the selection, so handing it the selection
// wrapped in markers turns "insert" into "wrap".
func TestFormatSnippet_WrapsTheSelection(t *testing.T) {
	src := "make this word bold\n"
	sel := strings.Index(src, "word")
	got := formatSnippet(src, actionByKey(t, "bold"), sel, sel+len("word"))
	assert.Equal(t, "**word**", got)
}

func TestFormatSnippet_NoSelectionUsesThePlaceholder(t *testing.T) {
	// A collapsed caret has nothing to wrap, so the button still produces
	// something — and something with a word in it to type over.
	got := formatSnippet("some text\n", actionByKey(t, "italic"), 5, 5)
	assert.Equal(t, "*italic*", got)
}

// TestFormatSnippet_SelectionIsCharOffsets guards the unit boundary. The caret
// report is in CHARS; slicing the buffer with them directly would cut multibyte
// text mid-rune and wrap the wrong span.
func TestFormatSnippet_SelectionIsCharOffsets(t *testing.T) {
	src := "ααα word\n" // each α is two bytes
	// "word" is chars 4..8.
	got := formatSnippet(src, actionByKey(t, "code"), 4, 8)
	assert.Equal(t, "`word`", got)
}

func TestFormatSnippet_ReversedSelectionIsNormalised(t *testing.T) {
	// A selection dragged right-to-left can arrive with start > end.
	src := "make this word bold\n"
	sel := strings.Index(src, "word")
	got := formatSnippet(src, actionByKey(t, "bold"), sel+len("word"), sel)
	assert.Equal(t, "**word**", got)
}

func TestFormatSnippet_LinkKeepsTheLabelAndLeavesTheUrl(t *testing.T) {
	src := "see the docs here\n"
	sel := strings.Index(src, "docs")
	got := formatSnippet(src, actionByKey(t, "link"), sel, sel+len("docs"))
	assert.Equal(t, "[docs](url)", got)
}

// TestFormatActions_AreInlineOnly pins the M2 descope: every action wraps a
// span. A line-level action (heading, list, quote) cannot be correct through
// insertAtCursor, which inserts at the caret and not at the line start, so one
// appearing here means that decision was undone without being revisited.
func TestFormatActions_AreInlineOnly(t *testing.T) {
	for _, a := range formatActions {
		assert.NotEmpty(t, a.open, "action %q has no opening marker", a.key)
		assert.NotEmpty(t, a.placeholder, "action %q has no placeholder", a.key)
		assert.NotContains(t, a.open, "\n", "action %q spans lines", a.key)
		assert.NotContains(t, a.close, "\n", "action %q spans lines", a.key)
	}
}

// ---------------------------------------------------------------------------
// Readout (M2)
// ---------------------------------------------------------------------------

func TestCountStats_CountsProseNotMarkup(t *testing.T) {
	src := "# Two words\n\nThree plain words here.\n"
	st := countStats(src, markdownhighlight.HighlightLex([]byte(src)))
	// "Two words" (2) + "Three plain words here." (4).
	assert.Equal(t, 6, st.Words)
	assert.Greater(t, st.Chars, 0)
}

// TestCountStats_SkipsFencedCode is the reason the readout reads spans instead
// of the raw buffer: a technical document is mostly code by volume, and
// counting it would inflate both the count and the reading estimate.
func TestCountStats_SkipsFencedCode(t *testing.T) {
	prose := "Just four plain words.\n"
	withCode := prose + "\n```go\nfunc main() { println(\"a lot of code words here\") }\n```\n"

	a := countStats(prose, markdownhighlight.HighlightLex([]byte(prose)))
	b := countStats(withCode, markdownhighlight.HighlightLex([]byte(withCode)))
	assert.Equal(t, a.Words, b.Words, "a fenced block must not add prose words")
}

func TestCountStats_SkipsFrontmatterAndUrls(t *testing.T) {
	src := "---\ntitle: Some Long Title Here\n---\n\nTwo words\n"
	st := countStats(src, markdownhighlight.HighlightLex([]byte(src)))
	assert.Equal(t, 2, st.Words, "frontmatter is metadata, not prose")

	withURL := "See [docs](https://example.com/a/very/long/path) now\n"
	stURL := countStats(withURL, markdownhighlight.HighlightLex([]byte(withURL)))
	// "See", "docs", "now" — the URL is not read.
	assert.Equal(t, 3, stURL.Words)
}

func TestCountStats_ReadingTimeRoundsUp(t *testing.T) {
	// One word still rounds to a whole minute; statsLine is what decides
	// whether a minute is worth showing.
	st := countStats("word\n", markdownhighlight.HighlightLex([]byte("word\n")))
	assert.Equal(t, 1, st.Words)
	assert.Equal(t, 1, st.ReadMinutes)

	assert.Equal(t, docStats{}, countStats("", nil))
}

func TestStatsLine_OmitsReadingTimeForShortNotes(t *testing.T) {
	inst := &App{stats: docStats{Words: 5, Chars: 20, ReadMinutes: 1}}
	assert.NotContains(t, inst.statsLine(), "min read", "a five-word note is not a one-minute read")

	inst.stats = docStats{Words: 400, Chars: 2000, ReadMinutes: 2}
	assert.Contains(t, inst.statsLine(), "2 min read")
}

// ---------------------------------------------------------------------------
// Outline (M2)
// ---------------------------------------------------------------------------

// TestOutlineClickDoesNotFightTheCaret is the reconciliation this pane needed.
// Both the caret and an outline click scroll the preview; if a click moved the
// caret's baseline, the next frame would see the caret's real section as
// "changed" and drag the preview straight back off the heading just clicked.
func TestOutlineClickDoesNotFightTheCaret(t *testing.T) {
	inst := &App{caretSlug: "first"}

	// The reader clicks "second" in the outline.
	inst.pendingScroll = "second"
	slug, ok := inst.takeScrollTarget()
	assert.True(t, ok)
	assert.Equal(t, "second", slug)

	// The caret has not moved, so nothing re-queues and the preview stays put.
	assert.Equal(t, "first", inst.caretSlug, "a click must not move the caret's baseline")
	_, ok = inst.takeScrollTarget()
	assert.False(t, ok, "the target is consumed once, not re-issued every frame")
}

func TestOutlineVisibility(t *testing.T) {
	inst := &App{}
	inst.winW = 1200

	assert.False(t, inst.outlineVisible(), "off by default")

	inst.showOutline = true
	assert.True(t, inst.outlineVisible(), "on, in a wide window")

	inst.winW = outlineMinWindowPx - 1
	assert.False(t, inst.outlineVisible(), "a narrow window hides it rather than starving the panes")

	inst.winW = outlineMinWindowPx
	assert.True(t, inst.outlineVisible(), "the threshold is inclusive")
}

func TestOutlineSummaryCountsNamedHeadings(t *testing.T) {
	hs := []markdown.HeadingInfo{
		{Text: "First", Slug: "first", Level: 1, ByteOffset: 0},
		{Text: "", Slug: "", Level: 2, ByteOffset: -1}, // degenerate `##`
		{Text: "Second", Slug: "second", Level: 2, ByteOffset: 10},
	}
	assert.Equal(t, "Outline (2)", outlineSummary(hs))
	assert.Equal(t, "Outline (0)", outlineSummary(nil))
}

func TestItoa(t *testing.T) {
	for _, n := range []int{0, 1, 7, 10, 42, 999, 1234567} {
		assert.Equal(t, strconv.Itoa(n), itoa(n))
	}
}

// ---------------------------------------------------------------------------
// Manifest
// ---------------------------------------------------------------------------

func TestManifestRegisters(t *testing.T) {
	// RegisterFactory only logs a warning when Validate rejects a manifest, so
	// a malformed one would leave the app silently missing from the launcher.
	require.NoError(t, manifest.Validate())
	got, ok := app.DefaultRegistry.LookupManifest(app.AppIdT(manifest.Id))
	require.True(t, ok, "app is not in the default registry")
	assert.Equal(t, manifest.Display, got.Display)
	assert.Equal(t, app.SurfaceWindowed, got.Surface)
}

// TestManifestDeclaresOnlyTheClipboardCap pins ADR-0178's claim that the first
// cut reaches exactly one subject. An fs.* cap appearing here means file I/O
// arrived without the decision that was supposed to precede it, and a cap the
// app never exercises is the §SD10 gate's other failure mode.
func TestManifestDeclaresOnlyTheClipboardCap(t *testing.T) {
	require.Len(t, manifest.Caps, 1)
	cap := manifest.Caps[0]
	assert.Equal(t, clipboardbroker.SubjectWrite, cap.Pattern)
	assert.Equal(t, app.CapDirectionPub, cap.Direction)
	assert.NotEmpty(t, cap.Reason, "a cap needs a reason")
}

// TestManifestPersistsTheDocument guards the one thing standing between a
// no-file-I/O editor and losing its content when the window closes.
func TestManifestPersistsTheDocument(t *testing.T) {
	assert.Equal(t, []string{docKey}, manifest.PersistedKeys)
	assert.NotContains(t, docKey, ".", "persist keys are single NATS tokens")
}

func TestDrainAsync_NothingPendingIsANoop(t *testing.T) {
	inst := &App{src: "doc", saved: "doc", status: "earlier"}
	inst.drainAsync()
	if inst.status != "earlier" {
		t.Errorf("an idle drain must not disturb the status: got %q", inst.status)
	}
	if inst.dirty() {
		t.Error("an idle drain must not disturb the checkpoint")
	}
}
