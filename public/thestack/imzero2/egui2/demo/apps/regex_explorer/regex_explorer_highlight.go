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

// getCompiledRegexp returns the cached compile for pattern, compiling it on
// the first call. Errors are cached too; a compile failure is the expected
// case during interactive typing and must not stall the UI.
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
	re, err = regexp.Compile(pattern)
	inst.compileCache[pattern] = compileResult{re: re, err: err}
	return
}

// flagPrefix returns the RE2 inline-flag group for the current toggle
// state — "(?i)", "(?ims)", … — or the empty string when no flag is set.
// Also serves as the flag component of a query key, so a lane re-runs when
// a toggle changes even though the raw input text did not.
func (inst *App) flagPrefix() (prefix string) {
	var flags strings.Builder
	if inst.caseInsensitive {
		flags.WriteByte('i')
	}
	if inst.multiline {
		flags.WriteByte('m')
	}
	if inst.dotAll {
		flags.WriteByte('s')
	}
	if flags.Len() == 0 {
		return
	}
	prefix = "(?" + flags.String() + ")"
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

// nonEmptyMatches returns re's non-overlapping matches in haystack as
// (start, end) byte-offset pairs, with zero-width matches dropped.
//
// The filter is what makes the preview predictive. Go and ClickHouse
// enumerate repeated empty matches differently: for pattern `a*` over
// "xyz", Go's FindAllStringIndex yields one zero-width match at every
// position (4 of them) while ClickHouse's extractAll yields none. RE2
// specifies what matches, not how a caller enumerates repeated empty
// matches, so neither engine is wrong — but this app exists to predict
// ClickHouse, so the preview follows ClickHouse. Without the filter the
// status bar claims "Go: 4 match(es)" while the List tab shows 0, and
// the highlighter emits four invisible zero-width spans.
//
// The SD1 tripwire deliberately does NOT go through here — see
// [App.tripwireGoMatches].
func nonEmptyMatches(re *regexp.Regexp, haystack string) (matches [][]int) {
	all := re.FindAllStringIndex(haystack, -1)
	matches = make([][]int, 0, len(all))
	for _, m := range all {
		if m[0] == m[1] {
			continue
		}
		matches = append(matches, m)
	}
	return
}

// renderHighlightedHaystack paints haystack as a LabelAtoms with match
// ranges highlighted. Plain segments between matches use AtomsFluid.Text;
// match segments use StyledTextColored with the IDS accent fill. An
// invalid pattern yields the unstyled haystack — the compile error is
// surfaced next to the pattern input (see [renderPatternCompileError]).
//
// Highlighting stops after maxHighlightedMatches. The haystack itself is
// still painted in full: the tail simply falls into the trailing plain
// segment, and a weak note says so.
func (inst *App) renderHighlightedHaystack(pattern string, haystack string) {
	if haystack == "" {
		c.Label("(empty haystack)").Send()
		return
	}
	if pattern == "" {
		c.Label(haystack).Send()
		return
	}

	re, compileErr := inst.getCompiledRegexp(inst.effectivePattern(pattern))
	if compileErr != nil {
		c.Label(haystack).Send()
		return
	}

	matches := nonEmptyMatches(re, haystack)
	if len(matches) == 0 {
		c.Label(haystack).Send()
		return
	}

	// Match highlight uses the IDS Accent role (ADR-0031 §SD2 reserves
	// accent for "branded highlights, selection, focus rings") — same
	// recipe as markdown's inline `==text==` highlighter pen (commit
	// 85cb26d4). Dark text on the bright accent fill keeps the match
	// visually pop without the saturation of the pre-IDS yellow.
	matchFg := color.Hex(styletokens.NeutralBgExtreme.AsHex()).Keep()
	matchBg := color.Hex(styletokens.AccentDefault.AsHex()).Keep()

	// Past maxHighlightedMatches the tail falls into the trailing plain
	// segment below, so the haystack still reads in full — only the
	// styling stops. Unlike the row caps, this one drops no content.
	styled := matches
	if len(styled) > maxHighlightedMatches {
		styled = styled[:maxHighlightedMatches]
	}

	atoms := c.Atoms()
	cursor := 0
	for _, match := range styled {
		start, end := match[0], match[1]
		if start > cursor {
			atoms.Text(haystack[cursor:start])
		}
		for range atoms.StyledTextColored(matchFg, matchBg, haystack[start:end]) {
		}
		cursor = end
	}
	if cursor < len(haystack) {
		atoms.Text(haystack[cursor:])
	}
	c.LabelAtoms(atoms.Keep()).Send()

	if len(styled) < len(matches) {
		c.LabelAtoms(c.Atoms().BeginRichText(
			fmt.Sprintf("highlighting the first %d of %d matches — the rest of the haystack is shown unstyled",
				len(styled), len(matches)),
		).Weak().End().Keep()).Send()
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
// and an empty table below every plain pattern is noise.
func (inst *App) renderCaptureGroups(pattern string, haystack string) {
	if pattern == "" || haystack == "" {
		return
	}
	re, err := inst.getCompiledRegexp(inst.effectivePattern(pattern))
	if err != nil || re == nil || re.NumSubexp() == 0 {
		return
	}
	matches := re.FindAllStringSubmatchIndex(haystack, -1)
	if len(matches) == 0 {
		return
	}

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

// groupLabel names capture group k: its (?P<name>…) name when it has one,
// otherwise its number.
func groupLabel(names []string, k int) (label string) {
	if k < len(names) && names[k] != "" {
		label = names[k]
		return
	}
	label = strconv.Itoa(k)
	return
}

// countMatches returns the number of matches of pattern in haystack via
// Go's regexp, counted the way ClickHouse's extractAll counts them (see
// [nonEmptyMatches]). A compile failure is returned as the error with a
// zero count; the caller keys on err, not on the count.
func (inst *App) countMatches(pattern string, haystack string) (n int, err error) {
	if pattern == "" || haystack == "" {
		return
	}
	re, compileErr := inst.getCompiledRegexp(inst.effectivePattern(pattern))
	if compileErr != nil {
		err = compileErr
		return
	}
	n = len(nonEmptyMatches(re, haystack))
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
// set. Uses the compile cache, so the check is O(1) per call after the
// first frame that touched the pattern.
func (inst *App) patternState() (state patternStateE) {
	if inst.pattern == "" {
		state = patternEmpty
		return
	}
	if _, err := inst.getCompiledRegexp(inst.effectivePattern(inst.pattern)); err != nil {
		state = patternInvalid
		return
	}
	state = patternValid
	return
}

// renderPatternNotReady draws the placeholder a CH-backed result surface
// shows when there is no valid pattern to have queried, and reports
// whether it drew anything. Keeps the empty/invalid wording in one place
// so the tabs cannot drift apart.
func (inst *App) renderPatternNotReady() (drew bool) {
	switch inst.patternState() {
	case patternEmpty:
		c.Label("(enter a pattern above)").Send()
		drew = true
	case patternInvalid:
		c.Label("(pattern invalid — see the error under the Pattern input)").Send()
		drew = true
	}
	return
}

// isPatternValid reports whether the single-pattern input is ready to
// dispatch — non-empty and compiling under the current flag set.
func (inst *App) isPatternValid() bool {
	return inst.patternState() == patternValid
}

// patternNumSubexp returns the single-pattern input's capture-group count,
// or 0 if it does not compile. Decides whether extractAllGroups may be
// asked at all: ClickHouse rejects it for a group-less pattern.
func (inst *App) patternNumSubexp() (n int) {
	re, err := inst.getCompiledRegexp(inst.effectivePattern(inst.pattern))
	if err != nil || re == nil {
		return
	}
	n = re.NumSubexp()
	return
}

// multiLine is one non-empty line of the multi-pattern input together
// with its per-line state.
//
// Invalid means Go's regexp rejected the line, which is a *proxy* for what
// the Multi tab actually runs on. That tab is VectorScan-backed
// (multiMatchAllIndices), and VectorScan is a different engine accepting a
// different language from RE2 — so Go-validity is a useful pre-filter, not
// an authority. Two consequences the UI has to live with:
//
//   - a line Go accepts but VectorScan rejects fails the whole query, and
//     every line loses its hit state behind one error, because
//     multiMatchAllIndices is a single call over the whole set;
//   - a line Go rejects is skipped, even if VectorScan would have taken it.
//
// The SD1 tripwire covers the RE2 path only, so nothing currently proves
// the two languages agree on any given line. Err carries the ClickHouse
// error when a dispatch failed, so the per-line marker can distinguish
// "Go could not compile this" from "ClickHouse refused the set".
type multiLine struct {
	Text    string
	Invalid bool
	Hit     bool
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

// renderPatternCompileError draws a red-on-white error label below the
// single-pattern input if the pattern fails to compile. Empty patterns
// are silent (the hint-text already communicates "enter something").
// Uses the compile cache so no re-compile happens per frame.
func (inst *App) renderPatternCompileError(pattern string) {
	if pattern == "" {
		return
	}
	if _, err := inst.getCompiledRegexp(inst.effectivePattern(pattern)); err != nil {
		regexedit.ErrorLabel("regex compile error: " + err.Error())
	}
}

// renderPatternListCompileErrors draws a red error label below the
// multi-pattern input summarising any invalid lines. Reports the first
// bad line's message plus the count of bad lines overall, so the user
// has one concrete message to read and the scope of the damage. Per-line
// ⚠ markers in [App.renderMultiInline] are the visual counterpart; this
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
