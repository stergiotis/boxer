package mdedit

// Find and replace (ADR-0178 M3).
//
// Three seams do the work and none of them is new. Occurrences are painted
// through `sectionStyled` (ADR-0130 L3), the sparse overlay channel that
// decorates text without touching it — which is what lets the bar show every
// match at once instead of selecting them one at a time. Going to a match
// writes the caret back through `setCursor`, the inbound half of the caret
// channel. And a replacement is a whole-buffer rebind: Go computes the new
// text, hands it to the TextEdit and drops the frontend's cached copy with
// OverrideDatabindingSPtr — the same idiom the session restore uses.
//
// Three consequences are visible to whoever uses the bar, so they are stated
// here rather than left to be discovered.
//
// **A styled overlay reaches a whole COLOUR section, not only the bytes it
// covers.** The Rust side merges an overlapping styled section into the format
// of every colour section it touches, deliberately, so that the colour tier
// alone decides where sections begin (`text_edit_highlight.rs`,
// `apply_styles`). The markdown lexer coalesces runs of one category, so a
// paragraph of prose is a SINGLE section — and a match inside it would tint
// the paragraph. The repair is on this side of the seam and asks nothing of
// it: the colour spans are split at every painted match boundary before the
// job is built, so each match is its own section and the overlay lands on
// exactly its bytes.
//
// **A replacement leaves the widget's own edit path.** It is written from Go
// rather than typed, so it is not an edit egui saw; what Ctrl+Z does after one
// is the undoer's business — it snapshots the buffer on a timer, and an
// externally rewritten buffer is just another state to it. Not arranged here,
// and not relied on.
//
// **A match that is off-screen is selected but not revealed.** The seam has no
// byte-range-to-rect channel, so nothing here can scroll the source pane to
// the caret it just moved (ADR-0130, 2026-08-08). The preview is the partial
// compensation: going to a match also takes the preview to the section the
// match is in, which is somewhere the reader can see.

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdownhighlight"
)

const (
	// maxPaintedMatches bounds the overlay list. A one-character query over a
	// long document has thousands of matches, and every painted one costs a
	// styled section AND a split in the colour job — so the cost of showing
	// them all is paid twice. The window below is centred on the current
	// match, so the ones a reader is looking at are always among those drawn,
	// and the readout says when it is not showing all of them.
	//
	// Navigation and replace are NOT bounded by this: they read the full match
	// list, so "replace all" means all of them.
	maxPaintedMatches = 400

	// findFieldWidthPx sizes the two text fields. A TextEdit that states no
	// width takes spacing().text_edit_width — 280pt each, which pushes the
	// buttons off the end of a bar that has two of them.
	findFieldWidthPx = float32(150)
)

// Match tones. Accent for "here is an occurrence", warning for "and this is
// the one you are on" — two hues rather than two intensities of one, because
// on a code-editor background the intensity difference alone did not separate
// them.
//
// Alpha, not a palette background token, for the reason sqleditor's subquery
// tint records: the palette's background family sits within a few levels of
// the editor's near-black, so a flat token is invisible behind text. egui
// blends these over whatever is behind them.
var (
	// designlint:ignore=L2 (no mid-tone background token exists; see above)
	toneMatch = color.RGBA(styletokens.AccentDefault.R,
		styletokens.AccentDefault.G, styletokens.AccentDefault.B, 0x38)
	// designlint:ignore=L2 (no mid-tone background token exists; see above)
	toneMatchCurrent = color.RGBA(styletokens.WarningDefault.R,
		styletokens.WarningDefault.G, styletokens.WarningDefault.B, 0x70)
)

// Hover help for the bar.
const (
	tipFind = "Find and replace inside the document. Matches are painted in the source pane; the current one is the warmer colour."

	tipFindQuery = "Text to find. Matching is literal — no regular expressions, no whole-word option."

	tipFindCase = "Match case. With it off, letters fold both ways, including accented and non-Latin ones."

	tipFindPrev = "Go to the previous match: it is selected in the source and the preview scrolls to its section. The source pane itself cannot be scrolled to the caret from here — the editing seam reports no geometry for a byte range — so a match far off-screen is selected where you cannot see it."

	tipFindNext = "Go to the next match: it is selected in the source and the preview scrolls to its section. The source pane itself cannot be scrolled to the caret from here — the editing seam reports no geometry for a byte range — so a match far off-screen is selected where you cannot see it."

	tipReplaceWith = "Text each replaced match becomes. Empty deletes the match."

	tipReplaceOne = "Replace the current match and move to the next one."

	tipReplaceAll = "Replace every match in the document at once. This rewrites the buffer from outside the editor rather than typing into it, so it is not an edit the widget's own undo history describes."

	hintFindQuery   = "find"
	hintReplaceWith = "replace with"
)

// matchSpan is one occurrence, as byte offsets into the buffer — the same unit
// the styled overlays and the heading offsets speak, and one conversion away
// from the caret channel's chars.
type matchSpan struct {
	Start int
	Stop  int
}

// findState is everything the bar owns. Kept as one field on App rather than
// nine, so the milestone's state is as separable as its file is.
type findState struct {
	show        bool
	query       string
	replacement string
	matchCase   bool

	// matches is every occurrence in document order, and idx which of them is
	// current. idx is meaningless when matches is empty.
	matches []matchSpan
	idx     int

	// forSrc / forQuery / forCase / forShow are the inputs the match list was
	// computed from — the same text-keyed gate shape the lex and the preview
	// use, widened to the two other things a match depends on. have separates
	// "computed, no matches" from "not computed".
	forSrc   string
	forQuery string
	forCase  bool
	forShow  bool
	have     bool

	// resumeAt is a byte offset the next recompute must resume from, set by a
	// replacement to the END of what it wrote. Without it the recompute
	// anchors on the replaced match's own start — and a replacement that
	// contains the query ("cat" → "cats") puts a match right back there, so
	// the bar would sit on the text it just produced and rewrite it again on
	// every click.
	resumeAt   int
	resumeAtOk bool
}

// current returns the match the bar is on.
func (inst *findState) current() (m matchSpan, ok bool) {
	if inst.idx < 0 || inst.idx >= len(inst.matches) {
		return m, false
	}
	return inst.matches[inst.idx], true
}

// ---------------------------------------------------------------------------
// Matching
// ---------------------------------------------------------------------------

// findMatches returns the byte ranges of every non-overlapping occurrence of
// query in src, in document order.
//
// Case-insensitive matching folds rune by rune rather than lowercasing both
// strings, and that is not fussiness: [strings.ToLower] can change a string's
// LENGTH — 'İ' lowercases to two runes — so offsets taken against a folded
// copy would not describe the buffer they are meant to index. Comparing rune
// pairs keeps every offset a real offset into src.
func findMatches(src, query string, matchCase bool) (out []matchSpan) {
	if src == "" || query == "" {
		return nil
	}
	if matchCase {
		for at := 0; at < len(src); {
			i := strings.Index(src[at:], query)
			if i < 0 {
				break
			}
			// A literal query is valid UTF-8 and so is the buffer, so a byte
			// match cannot begin mid-rune: UTF-8 lead bytes never appear as
			// continuation bytes.
			start := at + i
			out = append(out, matchSpan{Start: start, Stop: start + len(query)})
			at = start + len(query)
		}
		return out
	}
	for at := 0; at < len(src); {
		n, ok := foldMatchAt(src, query, at)
		if ok {
			out = append(out, matchSpan{Start: at, Stop: at + n})
			at += n
			continue
		}
		_, w := utf8.DecodeRuneInString(src[at:])
		at += w
	}
	return out
}

// foldMatchAt reports whether src[at:] begins with query under simple case
// folding, and how many bytes of SRC that took. The two lengths can differ —
// a two-byte 'ä' matches a two-byte 'Ä' here, but folding is per-rune and
// nothing guarantees the encodings are the same width in general, which is
// exactly why the answer is measured against src rather than assumed from
// len(query).
func foldMatchAt(src, query string, at int) (n int, ok bool) {
	s, q := at, 0
	for q < len(query) {
		if s >= len(src) {
			return 0, false
		}
		rs, ws := utf8.DecodeRuneInString(src[s:])
		rq, wq := utf8.DecodeRuneInString(query[q:])
		if !foldEqual(rs, rq) {
			return 0, false
		}
		s += ws
		q += wq
	}
	return s - at, true
}

// foldEqual is case-insensitive rune equality. [unicode.SimpleFold] walks the
// orbit of a rune's case variants, which covers the pairs [unicode.ToLower]
// alone misses (Kelvin sign, final sigma) without ever leaving one rune.
func foldEqual(a, b rune) (yes bool) {
	if a == b {
		return true
	}
	if a > b {
		a, b = b, a
	}
	for r := unicode.SimpleFold(a); r != a; r = unicode.SimpleFold(r) {
		if r == b {
			return true
		}
	}
	return false
}

// matchAtOrAfter returns the index of the first match starting at or after
// off, wrapping to the first match when off is past all of them — the same
// wrap the two navigation buttons do, so "carry on from here" means the same
// thing however the bar got here. Never -1: a caller always has somewhere to
// be. An empty list returns 0, which current() then reports as absent.
func matchAtOrAfter(ms []matchSpan, off int) (idx int) {
	for i, m := range ms {
		if m.Start >= off {
			return i
		}
	}
	return 0
}

// stepIndex moves the current match by delta, wrapping at both ends. Wrapping
// rather than stopping is what makes a find bar usable with two buttons: the
// reader never has to know they are at the last match to keep going.
func stepIndex(idx, n, delta int) (next int) {
	if n <= 0 {
		return 0
	}
	next = (idx + delta) % n
	if next < 0 {
		next += n
	}
	return next
}

// ensureMatches recomputes the match list when anything it depends on moved.
//
// Where the current match lands after a recompute is the one judgment here,
// and the anchor answers it in three cases. A replacement says explicitly
// where to resume, past what it wrote. Otherwise the current match's own start
// keeps the bar roughly where the reader was looking, so typing another
// character into the query does not throw them back to the top of the
// document. And with no current match — the bar has just been opened — the
// caret is where the reader is, which is a better place to start than byte
// zero.
func (inst *App) ensureMatches() {
	f := &inst.find
	if f.have && f.forSrc == inst.src && f.forQuery == f.query &&
		f.forCase == f.matchCase && f.forShow == f.show {
		return
	}
	anchor := 0
	switch m, ok := f.current(); {
	case f.resumeAtOk:
		anchor = f.resumeAt
	case ok:
		anchor = m.Start
	default:
		caret, _ := c.UnpackCursorRange(inst.cursor)
		anchor = charToByte(inst.src, caret)
	}
	f.resumeAtOk = false

	f.forSrc, f.forQuery, f.forCase, f.forShow, f.have =
		inst.src, f.query, f.matchCase, f.show, true
	if !f.show || f.query == "" {
		f.matches, f.idx = nil, 0
		return
	}
	f.matches = findMatches(inst.src, f.query, f.matchCase)
	f.idx = matchAtOrAfter(f.matches, anchor)
}

// ---------------------------------------------------------------------------
// Painting
// ---------------------------------------------------------------------------

// paintWindow is the slice of matches to paint: all of them when there are few
// enough, and otherwise a run of maxPaintedMatches centred on the current one.
// Centred rather than leading, so the matches around the one the reader is on
// are the ones that get drawn.
func paintWindow(n, idx, limit int) (lo, hi int) {
	if limit <= 0 || n <= limit {
		return 0, n
	}
	lo = max(idx-limit/2, 0)
	hi = lo + limit
	if hi > n {
		hi, lo = n, n-limit
	}
	return lo, hi
}

// matchSections is the overlay list: a background on every painted match, in
// the warmer tone for the current one.
//
// Each match is emitted once. Overlapping styled sections merge property by
// property on the Rust side with the last one winning, so a second section
// over the same bytes would be a way to be surprised rather than a way to
// compose — and the matches are disjoint by construction anyway.
func matchSections(ms []matchSpan, idx, limit int) (out []codeview.StyledSection) {
	lo, hi := paintWindow(len(ms), idx, limit)
	if hi <= lo {
		return nil
	}
	out = make([]codeview.StyledSection, 0, hi-lo)
	for i := lo; i < hi; i++ {
		tone, flags := toneMatch, codeview.StyleBackground
		if i == idx {
			tone, flags = toneMatchCurrent, codeview.StyleBackground|codeview.StyleUnderline
		}
		out = append(out, codeview.StyledSection{
			Start: uint32(ms[i].Start), Stop: uint32(ms[i].Stop),
			Flags: flags, Color: tone,
		})
	}
	return out
}

// matchCuts is the ascending, deduplicated list of byte offsets the colour
// spans must be split at so the overlays above land on exactly their bytes and
// not on the whole prose run around them — the coarseness this file's header
// explains. Only PAINTED matches earn a cut; an unpainted one has no overlay
// to keep honest.
func matchCuts(ms []matchSpan, idx, limit int) (cuts []int) {
	lo, hi := paintWindow(len(ms), idx, limit)
	if hi <= lo {
		return nil
	}
	cuts = make([]int, 0, 2*(hi-lo))
	for i := lo; i < hi; i++ {
		// Matches are disjoint and in order, so appending start-then-stop
		// keeps the list ascending; only the seam between a match and the one
		// that begins where it ends can repeat.
		if n := len(cuts); n == 0 || cuts[n-1] != ms[i].Start {
			cuts = append(cuts, ms[i].Start)
		}
		cuts = append(cuts, ms[i].Stop)
	}
	return cuts
}

// splitSpansAt cuts spans at the given ascending offsets, producing the same
// colouring in more sections. Offsets outside a span, or falling on a boundary
// it already has, change nothing.
//
// Span.Text is dropped rather than re-sliced: the lex tier leaves it empty and
// the job builder never reads it, so carrying it would be inventing a field's
// contents to satisfy a field nobody consults.
func splitSpansAt(spans []markdownhighlight.Span, cuts []int) (out []markdownhighlight.Span) {
	if len(cuts) == 0 {
		return spans
	}
	out = make([]markdownhighlight.Span, 0, len(spans)+len(cuts))
	ci := 0
	for _, s := range spans {
		start := s.Start
		for ci < len(cuts) && int32(cuts[ci]) <= start {
			ci++
		}
		for ci < len(cuts) && int32(cuts[ci]) < s.Stop {
			out = append(out, markdownhighlight.Span{Start: start, Stop: int32(cuts[ci]), Category: s.Category})
			start = int32(cuts[ci])
			ci++
		}
		out = append(out, markdownhighlight.Span{Start: start, Stop: s.Stop, Category: s.Category})
	}
	return out
}

// ---------------------------------------------------------------------------
// Replacing
// ---------------------------------------------------------------------------

// replaceSpans rewrites src with `with` at each of the given byte ranges,
// which must be ascending and disjoint — which findMatches guarantees.
//
// Splicing right to left would avoid the offset bookkeeping, but building the
// result left to right into one Builder is a single allocation instead of one
// per match, and replace-all over a long document is exactly where that shows.
func replaceSpans(src string, ms []matchSpan, with string) (out string, n int) {
	if len(ms) == 0 {
		return src, 0
	}
	var b strings.Builder
	b.Grow(len(src) + len(ms)*(len(with)-1))
	at := 0
	for _, m := range ms {
		if m.Start < at || m.Stop > len(src) || m.Stop < m.Start {
			// A stale span cannot be spliced onto a buffer it does not
			// describe. Skipping it loses a replacement; splicing it would
			// corrupt the document.
			continue
		}
		b.WriteString(src[at:m.Start])
		b.WriteString(with)
		at = m.Stop
		n++
	}
	b.WriteString(src[at:])
	return b.String(), n
}

// replaceCurrent replaces the match the bar is on and resumes past what it
// wrote, so one click cannot land inside its own replacement.
//
// It does not stop the search from coming back around: once the bar has been
// through the document, a replacement that contains the query ("cat" → "cats")
// gives the next pass something to find again. That is what a wrapping find
// does everywhere, and stopping it would mean remembering which bytes this
// session produced.
func (inst *App) replaceCurrent() {
	f := &inst.find
	m, ok := f.current()
	if !ok {
		return
	}
	next, n := replaceSpans(inst.src, []matchSpan{m}, f.replacement)
	if n == 0 || !inst.rebindBuffer(next) {
		return
	}
	end := m.Start + len(f.replacement)
	f.resumeAt, f.resumeAtOk = end, true
	// Leave the caret just past what was written, collapsed and WITHOUT
	// focus. The reader is in the bar and may well click again; pulling focus
	// into the source on every replacement would take the bar out from under
	// them. The painted matches are what shows where the edit landed.
	inst.requestCaret(end, end, false)
	inst.status = "replaced one"
}

// replaceAll rewrites every match in one pass over the buffer.
func (inst *App) replaceAll() {
	f := &inst.find
	if len(f.matches) == 0 {
		return
	}
	next, n := replaceSpans(inst.src, f.matches, f.replacement)
	if n == 0 || !inst.rebindBuffer(next) {
		return
	}
	inst.status = "replaced " + itoa(n)
}

// rebindBuffer installs a buffer Go computed rather than the reader typed, and
// reports whether that changed anything — replacing a word with itself is a
// gesture that should not raise the dirty marker.
//
// The override cannot be issued here: it resolves the pointer through the
// databindings registered for the CURRENT frame, and the source pane has not
// registered them yet — the bar renders first. So the flag travels to
// renderSource, which drops the frontend's cached copy immediately after the
// emit. Without that, the editor's own (pre-edit) value wins at the next Sync
// and the replacement silently undoes itself.
func (inst *App) rebindBuffer(next string) (changed bool) {
	if next == inst.src {
		return false
	}
	inst.src = next
	inst.rebindSrc = true
	return true
}

// ---------------------------------------------------------------------------
// Navigation
// ---------------------------------------------------------------------------

// gotoMatch moves the bar to a match and takes both panes to it: the caret
// selects it in the source, and the preview scrolls to the section it is in.
//
// The preview half is not decoration. setCursor positions the caret but cannot
// reveal it — egui scrolls to a selection only when the widget changed it
// itself, and there is no byte-range-to-rect channel to do it from outside
// (ADR-0130, 2026-08-08). Taking the preview to the match's section is the one
// thing this app CAN do to show the reader where they were sent.
func (inst *App) gotoMatch(idx int) {
	f := &inst.find
	if len(f.matches) == 0 {
		return
	}
	f.idx = idx
	m := f.matches[idx]
	// Focus, because this gesture means "take me there" and an unfocused
	// TextEdit paints neither caret nor selection — the ADR-0130 rule that
	// find-as-you-type passes false and a commit passes true.
	inst.requestCaret(m.Start, m.Stop, true)
	if inst.doc != nil {
		// Deliberately not touching caretSlug, for the reason the outline
		// click records: the caret has not moved yet, and claiming its new
		// section now would make the next frame read the real one as a change
		// and drag the preview back.
		inst.pendingScroll = headingSlugAt(inst.src, inst.doc.Headings(), m.Start)
	}
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// findSummary is the count readout: which match of how many, and whether the
// painting is showing all of them.
func (inst *App) findSummary() (s string) {
	f := &inst.find
	if f.query == "" {
		return ""
	}
	n := len(f.matches)
	if n == 0 {
		return "no matches"
	}
	var b strings.Builder
	b.WriteString(itoa(f.idx + 1))
	b.WriteString(" of ")
	b.WriteString(itoa(n))
	if n > maxPaintedMatches {
		// Never let a bounded painting read as a complete one.
		b.WriteString(" · showing ")
		b.WriteString(itoa(maxPaintedMatches))
		b.WriteString(" nearby")
	}
	return b.String()
}

// renderFindBar draws the third row of the action bar. Callers own the
// enclosing horizontal layout.
//
// Every button acts through the helpers above rather than editing state here,
// so the whole of what find and replace DO is testable without a frame.
func (inst *App) renderFindBar() {
	f := &inst.find
	for range c.HoverText(tipFindQuery).KeepIter() {
		c.TextEdit(inst.ids.PrepareStr("find-query"), f.query, false).
			DesiredWidth(findFieldWidthPx).
			HintText(hintFindQuery).
			SendRespVal(&f.query)
	}
	for range c.HoverText(tipReplaceWith).KeepIter() {
		c.TextEdit(inst.ids.PrepareStr("find-replacement"), f.replacement, false).
			DesiredWidth(findFieldWidthPx).
			HintText(hintReplaceWith).
			SendRespVal(&f.replacement)
	}
	for range c.HoverText(tipFindCase).KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("find-case"), f.matchCase, "Aa").
			SendRespVal(&f.matchCase)
	}

	prev, next := false, false
	for range c.HoverText(tipFindPrev).KeepIter() {
		prev = c.Button(inst.ids.PrepareStr("find-prev"), atomsFindPrev).
			SendResp().HasPrimaryClicked()
	}
	for range c.HoverText(tipFindNext).KeepIter() {
		next = c.Button(inst.ids.PrepareStr("find-next"), atomsFindNext).
			SendResp().HasPrimaryClicked()
	}
	if prev || next {
		delta := 1
		if prev {
			delta = -1
		}
		inst.gotoMatch(stepIndex(f.idx, len(f.matches), delta))
	}

	if s := inst.findSummary(); s != "" {
		c.Label(s).Send()
	}

	replaceOne, replaceEvery := false, false
	for range c.HoverText(tipReplaceOne).KeepIter() {
		replaceOne = c.Button(inst.ids.PrepareStr("find-replace"), atomsReplaceOne).
			SendResp().HasPrimaryClicked()
	}
	for range c.HoverText(tipReplaceAll).KeepIter() {
		replaceEvery = c.Button(inst.ids.PrepareStr("find-replace-all"), atomsReplaceAll).
			SendResp().HasPrimaryClicked()
	}
	// One rewrite per frame. Both buttons cannot be clicked in the same frame
	// in practice, but replace-all after replace-one would splice the whole
	// match list onto a buffer one replacement has already moved.
	switch {
	case replaceEvery:
		inst.replaceAll()
	case replaceOne:
		inst.replaceCurrent()
	}
}

// Button labels, built once — identical retained bytes intern to one blob.
var (
	atomsFindPrev   = c.Atoms().Text(icons.PhCaretLeft).Keep()
	atomsFindNext   = c.Atoms().Text(icons.PhCaretRight).Keep()
	atomsReplaceOne = c.Atoms().Text("Replace").Keep()
	atomsReplaceAll = c.Atoms().Text("Replace all").Keep()
)

// findToggleLabel is the checkbox in the row above, kept here so the bar's
// vocabulary lives with the bar.
const findToggleLabel = icons.PhMagnifyingGlass + " Find"
