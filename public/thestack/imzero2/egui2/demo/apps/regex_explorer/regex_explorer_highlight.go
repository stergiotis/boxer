//go:build llm_generated_opus47

package regex_explorer

// Inline match highlighting.
//
// Match offsets are computed locally via Go's regexp (RE2). See ADR-0005
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
	"regexp"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
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

// renderHighlightedHaystack paints haystack as a LabelAtoms with match
// ranges highlighted. Plain segments between matches use AtomsFluid.Text;
// match segments use StyledTextColored with a yellow background. An invalid
// pattern yields the unstyled haystack — the compile error is surfaced
// next to the pattern input (see [renderPatternCompileError]).
func renderHighlightedHaystack(pattern string, haystack string) {
	if haystack == "" {
		c.Label("(empty haystack)").Send()
		return
	}
	if pattern == "" {
		c.Label(haystack).Send()
		return
	}

	re, compileErr := app.getCompiledRegexp(effectivePattern(pattern))
	if compileErr != nil {
		c.Label(haystack).Send()
		return
	}

	matches := re.FindAllStringIndex(haystack, -1)
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

	atoms := c.Atoms()
	cursor := 0
	for _, match := range matches {
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
}

// countMatches returns the number of matches of pattern in haystack via
// Go's regexp. Compile failures are reported as 0 matches with the error;
// the UI uses this for the status-bar match count (-1 sentinel on error).
func countMatches(pattern string, haystack string) (n int, err error) {
	if pattern == "" || haystack == "" {
		return
	}
	re, compileErr := app.getCompiledRegexp(effectivePattern(pattern))
	if compileErr != nil {
		err = compileErr
		n = -1
		return
	}
	n = len(re.FindAllStringIndex(haystack, -1))
	return
}

// isPatternValid reports whether the single-pattern input compiles under
// Go's regexp with the current flag set. Empty pattern counts as invalid
// (there is nothing to dispatch). Uses the compile cache so the check is
// O(1) per call after the first frame that touched the pattern.
func isPatternValid() bool {
	if app.pattern == "" {
		return false
	}
	_, err := app.getCompiledRegexp(effectivePattern(app.pattern))
	return err == nil
}

// multiLine is one non-empty line of the multi-pattern input together
// with its per-line state: whether it compiles under Go regexp, and
// whether the most recent ClickHouse multiMatchAllIndices dispatch
// reported a hit for it. Invalid lines always have Hit==false;
// dispatchers skip them when building the call to ClickHouse.
type multiLine struct {
	Text    string
	Invalid bool
	Hit     bool
}

// multiSnapshot is the result of the most recent RunMultiMatch dispatch.
// patternListText is the exact textarea content that produced `lines`,
// so render code can detect "the user edited since this snapshot" by
// string comparison and fall back to pending-state markers rather than
// misrepresenting stale hits as current.
type multiSnapshot struct {
	patternListText string
	lines           []multiLine
}

// parseAndValidatePatternList splits the patternList textarea into
// non-empty lines, and tags each line with Invalid=true when Go regexp
// rejects it under the current flag set. Hit is always false; the
// dispatcher fills it in after ClickHouse's response.
func parseAndValidatePatternList(raw string) (lines []multiLine) {
	for _, s := range strings.Split(raw, "\n") {
		if strings.TrimSpace(s) == "" {
			continue
		}
		line := multiLine{Text: s}
		if _, err := app.getCompiledRegexp(effectivePattern(s)); err != nil {
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
func renderPatternCompileError(pattern string) {
	if pattern == "" {
		return
	}
	if _, err := app.getCompiledRegexp(effectivePattern(pattern)); err != nil {
		renderCompileErrorLabel("regex compile error: " + err.Error())
	}
}

// renderPatternListCompileErrors draws a red error label below the
// multi-pattern input summarising any invalid lines. Reports the first
// bad line's message plus the count of bad lines overall, so the user
// has one concrete message to read and the scope of the damage. Empty
// lines are skipped. Per-line ⚠ markers in [renderMultiInline] are the
// visual counterpart; this label carries the full Go regexp error text.
func renderPatternListCompileErrors(patternList string) {
	if patternList == "" {
		return
	}
	var firstBadLine int
	var firstErr error
	badCount := 0
	lineNum := 0
	for _, s := range strings.Split(patternList, "\n") {
		if strings.TrimSpace(s) == "" {
			continue
		}
		lineNum++
		if _, err := app.getCompiledRegexp(effectivePattern(s)); err != nil {
			badCount++
			if firstErr == nil {
				firstBadLine = lineNum
				firstErr = err
			}
		}
	}
	if badCount == 0 {
		return
	}
	var msg string
	if badCount == 1 {
		msg = "line " + strconv.Itoa(firstBadLine) + ": " + firstErr.Error()
	} else {
		msg = "line " + strconv.Itoa(firstBadLine) + ": " + firstErr.Error() + " (and " + strconv.Itoa(badCount-1) + " more line(s) invalid)"
	}
	renderCompileErrorLabel(msg)
}

// renderCompileErrorLabel paints msg as dark text on the IDS error fill —
// the visual "this is a compile error" affordance consistent with
// regexr.com and most IDE regex widgets, restated through the IDS
// semantic palette (ADR-0031 §SD2). Same fg-on-solid recipe as the badge
// widget's Solid variant (commit 8e5d40f3): NeutralBgExtreme reads at
// high contrast against the L≈0.80 ErrorDefault fill.
func renderCompileErrorLabel(msg string) {
	errFg := color.Hex(styletokens.NeutralBgExtreme.AsHex()).Keep()
	errBg := color.Hex(styletokens.ErrorDefault.AsHex()).Keep()
	atoms := c.Atoms()
	for range atoms.StyledTextColored(errFg, errBg, msg) {
	}
	c.LabelAtoms(atoms.Keep()).Send()
}
