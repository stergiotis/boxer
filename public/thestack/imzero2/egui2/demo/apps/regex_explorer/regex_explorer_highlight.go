package regex_explorer

// Inline match highlighting.
//
// Match offsets are computed locally via Go's regexp (RE2). See ADR-0054
// for why this is engine-compatible with ClickHouse's single-pattern regex
// functions, and how the SD1 tripwire guards against implementation drift
// between Go's regexp and ClickHouse's libre2.
//
// The haystack is painted as a single LabelAtoms with interleaved plain
// (AtomsFluid.Text) and colored-rich (AtomsFluid.StyledTextColored) segments,
// one colored scope per match. Compiled patterns are cached on the [App]
// keyed by pattern string; an invalid pattern is cached too, so the compile
// cost is paid once per unique input.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexedit"
)

// compileResult pairs a compiled regexp with any compile error so both
// success and failure are cacheable via the same map.
type compileResult struct {
	re  *regexp.Regexp
	err error
}

// maxCompileCache bounds the compile cache. Typing a pattern leaves every
// prefix of it behind — under each flag combination, and once per line of
// the pattern list — so an uncapped cache grows for the life of the
// window. The patterns on screen are a handful; the cap only has to sit
// well above that.
const maxCompileCache = 256

// getCompiledRegexp returns the cached compile for pattern, compiling it on
// the first call. Errors are cached too; a compile failure is the expected
// case during interactive typing and must not stall the UI. A full cache
// is emptied rather than evicted from: what is on screen recompiles once,
// on the next frame, which is cheaper than keeping an eviction order.
func (inst *App) getCompiledRegexp(pattern string) (re *regexp.Regexp, err error) {
	inst.compileCacheMu.Lock()
	defer inst.compileCacheMu.Unlock()
	if inst.compileCache == nil {
		inst.compileCache = map[string]compileResult{}
	}
	if r, ok := inst.compileCache[pattern]; ok {
		re, err = r.re, r.err
		return
	}
	if len(inst.compileCache) >= maxCompileCache {
		clear(inst.compileCache)
	}
	re, err = regexp.Compile(pattern)
	inst.compileCache[pattern] = compileResult{re: re, err: err}
	return
}

// flagPrefix returns the inline-flag group for the current toggle state —
// see [inlineFlags]. Also serves as the flag component of a query key, so a
// lane re-runs when a toggle changes even though the raw input text did not.
func (inst *App) flagPrefix() (prefix string) {
	prefix = inlineFlags(inst.caseInsensitive, inst.multiline, inst.dotAll)
	return
}

// effectivePattern prepends the inline-flag group to a user-entered
// pattern. Empty patterns are returned unchanged. The flag group is
// understood by both Go regexp and ClickHouse RE2, so the Go-side preview
// and the ClickHouse queries see equivalent patterns.
func (inst *App) effectivePattern(base string) (out string) {
	out = base
	if base == "" {
		return
	}
	out = inst.flagPrefix() + base
	return
}

// nonEmptySubmatches returns the matches in all — FindAllStringSubmatchIndex
// form: m[0],m[1] the whole match, m[2k],m[2k+1] capture group k (-1 for a
// group that did not participate) — with zero-width whole matches dropped.
//
// These are the matches the preview highlights and counts, the way
// ClickHouse's countMatches counts them: a zero-width match has nothing to
// highlight, and counting it would have the status bar report "4 matches"
// for `a*` over "xyz". Which of them extractAll returns is a different
// question — see [extractAllCount].
//
// A capture group that participated but matched the empty string is kept:
// only the whole match decides.
func nonEmptySubmatches(all [][]int) (matches [][]int) {
	matches = make([][]int, 0, len(all))
	for _, m := range all {
		if m[0] == m[1] {
			continue
		}
		matches = append(matches, m)
	}
	return
}

// patternAnalysis is what Go's regexp says about the single-pattern input
// over the haystack: whether the pattern is usable, and every match with
// its capture groups. The preview, the capture-group breakdown, the status
// bar, the query dispatch and the playground hand-off all read this one
// value, so they cannot disagree about which matches exist.
type patternAnalysis struct {
	// pattern is the effective pattern (flag prefix applied), "" when
	// nothing is entered; haystack is the haystack it was run over. Both
	// together are the memo key.
	pattern  string
	haystack string
	computed bool

	state patternStateE
	re    *regexp.Regexp
	// err is the compile error when state is patternInvalid.
	err error
	// all is every match FindAll reports, zero-width ones included — the
	// enumeration extractAllGroups shares, and the match_idx numbering of
	// the playground hand-off. matches is its non-empty subset, which the
	// preview highlights and counts. Both nil when the pattern is not
	// valid.
	all     [][]int
	matches [][]int
	// extractAllN is how many leading entries of all ClickHouse's
	// extractAll returns, and extractAllStop the byte offset of the
	// zero-width match it stopped on, -1 when it did not stop early — see
	// [extractAllCount].
	extractAllN    int
	extractAllStop int
}

// analysis returns the [patternAnalysis] for the inputs as they are now,
// recomputing it only when the effective pattern or the haystack changed —
// so a frame with no edit runs no regexp at all. Render-thread only, like
// the inputs it reads.
func (inst *App) analysis() (a *patternAnalysis) {
	a = &inst.analysisMemo
	pattern := inst.effectivePattern(inst.pattern)
	if a.computed && a.pattern == pattern && a.haystack == inst.haystack {
		return
	}
	*a = patternAnalysis{pattern: pattern, haystack: inst.haystack, computed: true, extractAllStop: -1}
	if pattern == "" {
		a.state = patternEmpty
		return
	}
	a.re, a.err = inst.getCompiledRegexp(pattern)
	if a.err != nil {
		a.state = patternInvalid
		return
	}
	a.state = patternValid
	// An empty haystack is analysed like any other: `^$` matches it, and
	// the Functions tab asks ClickHouse about it.
	a.all = a.re.FindAllStringSubmatchIndex(a.haystack, -1)
	a.matches = nonEmptySubmatches(a.all)
	a.extractAllN, a.extractAllStop = extractAllCount(a.re, a.haystack, a.all)
	return
}

// renderHighlightedHaystack paints the haystack as one LabelAtoms with the
// matches highlighted, in monospace like the editor above it, so a matched
// space or tab has a width to show. Consecutive matches alternate between
// two accent tones: with one tone, `\w` over "abc" is a single block and
// reads as one match. The caller handles the empty and invalid pattern.
//
// Highlighting stops after maxHighlightedMatches. The haystack itself is
// still painted in full: the tail simply falls into the trailing plain
// segment, and a weak note says so.
func (inst *App) renderHighlightedHaystack() {
	a := inst.analysis()
	haystack := a.haystack
	if haystack == "" {
		weakLabel("(empty haystack)")
		return
	}
	if len(a.matches) == 0 {
		weakLabel("No match.")
		for rt := range c.RichTextLabel(haystack) {
			rt.Monospace()
		}
		return
	}

	// Match highlight uses the IDS Accent role (ADR-0031 §SD2 reserves
	// accent for "branded highlights, selection, focus rings") — same
	// recipe as markdown's inline `==text==` highlighter pen (commit
	// 85cb26d4). Dark text on the bright accent fill keeps the match
	// visually pop without the saturation of the pre-IDS yellow.
	matchFg := color.Hex(styletokens.NeutralBgExtreme.AsHex()).Keep()
	matchBg := [2]color.Color{
		color.Hex(styletokens.AccentDefault.AsHex()).Keep(),
		// Strong, not Subtle: Subtle is the dark palette's near-black
		// surface tint, and the match text on it is dark too. Default and
		// Strong are both light under the dark palette and both mid-to-dark
		// under the light one, so the match text reads on either.
		color.Hex(styletokens.AccentStrong.AsHex()).Keep(),
	}

	// Past maxHighlightedMatches the tail falls into the trailing plain
	// segment below, so the haystack still reads in full — only the
	// styling stops. Unlike the row caps, this one drops no content.
	styled := a.matches
	if len(styled) > maxHighlightedMatches {
		styled = styled[:maxHighlightedMatches]
	}

	atoms := c.Atoms()
	plain := func(text string) {
		atoms = atoms.BeginRichText(text).Monospace().End()
	}
	cursor := 0
	for i, match := range styled {
		start, end := match[0], match[1]
		if start > cursor {
			plain(haystack[cursor:start])
		}
		for rt := range atoms.StyledTextColored(matchFg, matchBg[i%2], haystack[start:end]) {
			rt.Monospace()
		}
		cursor = end
	}
	if cursor < len(haystack) {
		plain(haystack[cursor:])
	}
	c.LabelAtoms(atoms.Keep()).Send()

	if len(styled) < len(a.matches) {
		weakLabel(fmt.Sprintf("highlighting the first %d of %d matches — the rest of the haystack is shown unstyled",
			len(styled), len(a.matches)))
	}
}

// renderCaptureGroups draws the per-match capture-group breakdown under
// the highlighted haystack: one row per match, one tinted cell per group,
// with the group's byte range. Capped at maxMatchRows — the heading above
// still reports the exact match count.
//
// This is the half of ADR-0054's premise that had never been built. The
// ADR chose Go as the offset authority precisely because
// FindAllStringSubmatchIndex returns offsets "for the full match and each
// capture group, in one call" — but the painter only ever used
// FindAllStringIndex, so group offsets were computed nowhere and SD5's
// capture-group-numbering parity assumption had nothing to compare.
//
// Silent when the pattern has no capture group: there is nothing to say,
// and an empty table below every plain pattern is noise. Rows are the
// analysis' non-empty matches, the ones the status bar counts.
func (inst *App) renderCaptureGroups() {
	a := inst.analysis()
	if len(a.matches) == 0 || a.re.NumSubexp() == 0 {
		return
	}
	re, matches, haystack := a.re, a.matches, a.haystack

	names := re.SubexpNames()
	c.Separator().Horizontal().Send()
	c.Label(fmt.Sprintf("Capture groups (%d per match, %d match(es)):", re.NumSubexp(), len(matches))).Send()

	for mi, m := range matches {
		if mi >= maxMatchRows {
			break
		}
		for range c.IdScope(inst.ids.PrepareSeq(uint64(mi))) {
			for range c.Horizontal().KeepIter() {
				c.Label(fmt.Sprintf("%d:", mi)).Send()
				// m[0],m[1] is the full match; group k lives at
				// m[2k],m[2k+1]. A group that did not participate in this
				// match has -1 for both.
				for k := 1; k*2+1 < len(m); k++ {
					start, end := m[2*k], m[2*k+1]
					label := groupLabel(names, k)
					if start < 0 || end < 0 {
						c.Label(label + "=(unset)").Send()
						continue
					}
					// Group k always takes cycle slot k-1, so one group
					// keeps one colour down the whole haystack and
					// adjacent groups stay distinguishable. QualitativeCycle
					// is the IDS categorical palette (ADR-0031), which
					// wraps on its own past the last entry.
					fg := color.Hex(styletokens.NeutralBgExtreme.AsHex()).Keep()
					bg := color.Hex(styletokens.QualitativeCycle(k - 1).AsHex()).Keep()
					atoms := c.Atoms().Text(label + "=")
					for range atoms.StyledTextColored(fg, bg, haystack[start:end]) {
					}
					atoms.Text(fmt.Sprintf(" [%d:%d]", start, end))
					c.LabelAtoms(atoms.Keep()).Send()
				}
			}
		}
	}
	renderTruncationNote(min(maxMatchRows, len(matches)), len(matches))
}

// groupLabel names capture group k for display: its (?P<name>…) name when
// it has one, otherwise its number.
func groupLabel(names []string, k int) (label string) {
	label = subexpName(names, k)
	if label == "" {
		label = strconv.Itoa(k)
	}
	return
}

// subexpName returns group k's (?P<name>…) name, or "" when it has none.
// names[0] is always empty — the whole match has no name.
func subexpName(names []string, k int) (name string) {
	if k < len(names) {
		name = names[k]
	}
	return
}

// patternStateE is the single-pattern input's readiness, as the result
// surfaces need it. "Nothing typed yet" and "typed something that does
// not compile" are different situations for the user and get different
// messages: the invalid case has a compile error rendered next to the
// input to point at, the empty case has nothing to point at.
type patternStateE uint8

const (
	// patternEmpty — no pattern entered; nothing to dispatch, nothing to explain.
	patternEmpty patternStateE = iota
	// patternInvalid — entered but rejected by Go's regexp under the current flags.
	patternInvalid
	// patternValid — compiles; queries may dispatch.
	patternValid
)

// patternState classifies the single-pattern input under the current flag
// set — see [App.analysis].
func (inst *App) patternState() (state patternStateE) {
	state = inst.analysis().state
	return
}

// renderPatternNotReady draws the placeholder a CH-backed result surface
// shows when there is no valid pattern to have queried, and reports
// whether it drew anything. Keeps the empty/invalid wording in one place
// so the tabs cannot drift apart.
func (inst *App) renderPatternNotReady() (drew bool) {
	switch inst.patternState() {
	case patternEmpty:
		weakLabel("Enter a pattern to ask ClickHouse about it.")
		drew = true
	case patternInvalid:
		weakLabel("The pattern does not compile — the error is under the Pattern input.")
		drew = true
	}
	return
}

// multiLine is one non-empty line of the multi-pattern input together
// with its per-line state.
//
// Invalid means Go's regexp rejected the line, which is a *proxy* for what
// the multi-pattern query actually runs on. That query is VectorScan-backed
// (multiMatchAllIndices), and VectorScan is a different engine accepting a
// different language from RE2 — so Go-validity is a useful pre-filter, not
// an authority. Two consequences the UI has to live with:
//
//   - a line Go accepts but VectorScan rejects fails the whole query, and
//     every line loses its hit state behind one error, because
//     multiMatchAllIndices is a single call over the whole set;
//   - a line Go rejects is skipped, even if VectorScan would have taken it.
//
// The SD1 tripwire checks that VectorScan accepts every pattern in its
// fixed corpus, not the user's lines, so nothing proves the two languages
// agree on any given line. When VectorScan refuses one, the query finds
// which and says so on that line (Rejected), rather than failing the set.
type multiLine struct {
	Text    string
	Invalid bool
	Hit     bool
	// Rejected is ClickHouse's message when VectorScan refused this line
	// (see [runMultiLinesBlocking]); empty otherwise.
	Rejected string
}

// parseAndValidatePatternList splits the patternList textarea into
// non-empty lines, and tags each line with Invalid=true when Go regexp
// rejects it under the current flag set. Hit is always false; the
// dispatcher fills it in after ClickHouse's response.
func (inst *App) parseAndValidatePatternList(raw string) (lines []multiLine) {
	for s := range strings.SplitSeq(raw, "\n") {
		if strings.TrimSpace(s) == "" {
			continue
		}
		line := multiLine{Text: s}
		if _, err := inst.getCompiledRegexp(inst.effectivePattern(s)); err != nil {
			line.Invalid = true
		}
		lines = append(lines, line)
	}
	return
}

// countValidMultiLines returns the number of lines in the slice that
// compile cleanly. Used by the header summary.
func countValidMultiLines(lines []multiLine) (n int) {
	for _, l := range lines {
		if !l.Invalid {
			n++
		}
	}
	return
}

// renderPatternCompileError draws an error label below the single-pattern
// input if the pattern fails to compile. Empty patterns are silent (the
// hint text already communicates "enter something").
func (inst *App) renderPatternCompileError() {
	if err := inst.analysis().err; err != nil {
		regexedit.ErrorLabel("regex compile error: " + err.Error())
	}
}

// renderPatternListCompileErrors draws a red error label below the
// multi-pattern input summarising any invalid lines. Reports the first
// bad line's message plus the count of bad lines overall, so the user
// has one concrete message to read and the scope of the damage. Per-line
// ⚠ markers in [App.renderMultiLines] are the visual counterpart; this
// label carries the full Go regexp error text.
//
// Walks the lines [App.parseAndValidatePatternList] already produced
// rather than re-splitting the textarea. The two used to disagree about
// nothing in particular, but they each carried their own definition of
// "which lines count", and only one of them had a test.
func (inst *App) renderPatternListCompileErrors(lines []multiLine) {
	var firstBadLine int
	var firstErr error
	badCount := 0
	for i, line := range lines {
		if !line.Invalid {
			continue
		}
		badCount++
		if firstErr == nil {
			firstBadLine = i + 1
			// Re-fetch from the cache purely for the message text; the
			// Invalid flag above is the authority on whether it failed.
			_, firstErr = inst.getCompiledRegexp(inst.effectivePattern(line.Text))
		}
	}
	if badCount == 0 || firstErr == nil {
		return
	}
	var msg string
	if badCount == 1 {
		msg = "line " + strconv.Itoa(firstBadLine) + ": " + firstErr.Error()
	} else {
		msg = "line " + strconv.Itoa(firstBadLine) + ": " + firstErr.Error() + " (and " + strconv.Itoa(badCount-1) + " more line(s) invalid)"
	}
	// The error affordance moved to widgets/regexedit with the editor
	// itself (ADR-0164 §SD4), so every regex input renders compile
	// errors the same way.
	regexedit.ErrorLabel(msg)
}
