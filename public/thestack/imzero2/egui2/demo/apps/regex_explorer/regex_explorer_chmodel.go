package regex_explorer

// A Go-side model of how ClickHouse enumerates matches.
//
// Go's regexp and ClickHouse's libre2 agree on what matches (ADR-0054); they
// do not agree on how a caller walks repeated matches, and ClickHouse does
// not walk them the same way in every function. Checked against
// clickhouse-local:
//
//   - countMatches counts the non-empty matches.
//   - extractAllGroups agrees with FindAll until an empty match is in
//     play, and then does not: it reports the empty match abutting a
//     match (FindAll skips it), steps one byte past an empty match (FindAll
//     steps one rune, so the two disagree inside a multi-byte character),
//     and never matches at the very end of the text. The model predicts it
//     only when the walk meets no empty match ([patternAnalysis.groupsModelled]);
//     emulating byte steps through the middle of a character would be a
//     guess about RE2 dressed as a prediction.
//   - extractAll stops at the first zero-width match it meets — including
//     the one right after a non-empty match that FindAll skips. So `a*`
//     over "aax" is ['aa'], over "xaay" is [], and `\w*` over "ab cd" is
//     ['ab'] where Go finds "ab" and "cd". When the pattern captures, each
//     element is capture group 1 (empty when the group did not take part),
//     not the whole match.
//
// The model is what lets the preview say which of its matches extractAll
// will return, and what the SD1 tripwire checks ClickHouse against.

import (
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode/utf8"
)

// inlineFlags returns the RE2 inline-flag group for a flag state. The dot
// flag is always stated, on or off: ClickHouse compiles RE2 with
// dot-matches-newline on, Go with it off, so a pattern sent without it
// means different things to the two engines. Stating it makes the Go
// preview, the RE2 functions and VectorScan read one pattern. Both RE2 and
// VectorScan accept the "(?i-s)" form.
func inlineFlags(caseInsensitive bool, multiline bool, dotAll bool) (prefix string) {
	var b strings.Builder
	b.WriteString("(?")
	if caseInsensitive {
		b.WriteByte('i')
	}
	if multiline {
		b.WriteByte('m')
	}
	if dotAll {
		b.WriteByte('s')
	} else {
		b.WriteString("-s")
	}
	b.WriteByte(')')
	prefix = b.String()
	return
}

// clickHouseDefaults makes Go read a pattern the way ClickHouse's RE2 does
// when the pattern states no dot flag: with dot-matches-newline on. An
// inline flag in the pattern itself still wins, as it does in ClickHouse.
// The interactive path does not need it — [inlineFlags] always states the
// flag — but the SD1 corpus sends patterns as written.
func clickHouseDefaults(pattern string) (out string) {
	out = "(?s)" + pattern
	return
}

// extractAllCount returns how many leading entries of all — Go's FindAll
// enumeration of re over haystack, zero-width matches included — ClickHouse's
// extractAll returns, and the byte offset at which it stopped early (-1 when
// it ran through every match).
//
// extractAll searches from the end of its last match and stops on an empty
// result. FindAll searches from the same place, so up to the stop the two
// see the same matches; the one case FindAll hides is a zero-width match
// right where a non-empty one ended, which it discards and extractAll stops
// on. That case is decided by [emptyMatchAt]: if the next FindAll match does
// not start where the last one ended, any match starting there was a
// discarded empty one.
func extractAllCount(re *regexp.Regexp, haystack string, all [][]int) (n int, stopAt int) {
	stopAt = -1
	var prog *syntax.Prog
	for i, m := range all {
		if m[0] == m[1] {
			stopAt = m[0]
			return
		}
		n = i + 1
		end := m[1]
		if i+1 < len(all) && all[i+1][0] == end {
			continue
		}
		if prog == nil {
			prog = compileProg(re)
			if prog == nil {
				return
			}
		}
		if emptyMatchAt(prog, haystack, end) {
			stopAt = end
			return
		}
	}
	return
}

// compileProg recompiles re's source the way regexp.Compile does, for the
// instruction walk in [emptyMatchAt]. Nil if it does not parse, which
// cannot happen for a source that regexp.Compile accepted.
func compileProg(re *regexp.Regexp) (prog *syntax.Prog) {
	parsed, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return
	}
	prog, err = syntax.Compile(parsed.Simplify())
	if err != nil {
		prog = nil
	}
	return
}

// emptyMatchAt reports whether prog can match the empty string at byte
// offset pos of haystack. An empty match consumes nothing, so it depends
// only on the zero-width assertions at pos (^ $ \A \z \b \B and their
// multiline forms), which depend only on the runes either side — so this
// walks the program's non-consuming instructions from the start under that
// context and asks whether one reaches a match.
func emptyMatchAt(prog *syntax.Prog, haystack string, pos int) bool {
	before, after := rune(-1), rune(-1)
	if pos > 0 {
		before, _ = utf8.DecodeLastRuneInString(haystack[:pos])
	}
	if pos < len(haystack) {
		after, _ = utf8.DecodeRuneInString(haystack[pos:])
	}
	ctx := syntax.EmptyOpContext(before, after)

	seen := make([]bool, len(prog.Inst))
	stack := []uint32{uint32(prog.Start)}
	for len(stack) > 0 {
		pc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[pc] {
			continue
		}
		seen[pc] = true
		inst := &prog.Inst[pc]
		switch inst.Op {
		case syntax.InstMatch:
			return true
		case syntax.InstAlt, syntax.InstAltMatch:
			stack = append(stack, inst.Out, inst.Arg)
		case syntax.InstCapture, syntax.InstNop:
			stack = append(stack, inst.Out)
		case syntax.InstEmptyWidth:
			if syntax.EmptyOp(inst.Arg)&^ctx == 0 {
				stack = append(stack, inst.Out)
			}
		}
		// InstRune*, InstFail: consumes input or fails — no empty match.
	}
	return false
}

// predictExtractAll returns what ClickHouse's extractAll(haystack, pattern)
// should return, given re compiled from pattern the way ClickHouse reads it.
func predictExtractAll(re *regexp.Regexp, haystack string) (out []string) {
	all := re.FindAllStringSubmatchIndex(haystack, -1)
	n, _ := extractAllCount(re, haystack, all)
	out = extractAllFrom(re, haystack, all[:n])
	return
}

// extractAllFrom renders the matches extractAll returns: each one's capture
// group 1 when the pattern captures (empty when the group did not take
// part), otherwise the whole match.
func extractAllFrom(re *regexp.Regexp, haystack string, returned [][]int) (out []string) {
	out = make([]string, 0, len(returned))
	group := 0
	if re.NumSubexp() > 0 {
		group = 1
	}
	for _, m := range returned {
		out = append(out, submatchText(haystack, m, group))
	}
	return
}

// submatchText returns group k of match m, "" when the group did not take
// part — the way ClickHouse renders an unset group.
func submatchText(haystack string, m []int, k int) (text string) {
	start, end := m[2*k], m[2*k+1]
	if start >= 0 {
		text = haystack[start:end]
	}
	return
}

// predictFunctions is the Go side of the Functions tab: what each modelled
// ClickHouse function should return for the analysed input. Groups is
// filled only when [patternAnalysis.groupsModelled]. The replace functions
// are not modelled — replaceRegexpAll replaces the empty match abutting a
// match that FindAll skips — and the tab shows no prediction for them
// rather than a wrong one.
func predictFunctions(a *patternAnalysis) (out fnOutcome) {
	re, haystack := a.re, a.haystack
	out.Match = len(a.all) > 0
	out.Count = uint64(len(a.matches))
	out.ExtractAll = extractAllFrom(re, haystack, a.all[:a.extractAllN])
	if len(a.all) > 0 {
		first := a.all[0]
		out.RegexpExtract = submatchText(haystack, first, 0)
		out.Extract = out.RegexpExtract
		if re.NumSubexp() > 0 {
			out.Extract = submatchText(haystack, first, 1)
		}
	}
	if re.NumSubexp() > 0 {
		out.YieldsGroups = true
	}
	if out.YieldsGroups && a.groupsModelled() {
		out.Groups = make([][]string, 0, len(a.all))
		for _, m := range a.all {
			row := make([]string, 0, re.NumSubexp())
			for k := 1; k <= re.NumSubexp(); k++ {
				row = append(row, submatchText(haystack, m, k))
			}
			out.Groups = append(out.Groups, row)
		}
	}
	return
}

// groupsModelled reports whether the model predicts extractAllGroups for
// the analysed input: when ClickHouse's walk meets no empty match, where
// its enumeration and FindAll's coincide (see the file comment).
func (a *patternAnalysis) groupsModelled() bool {
	return a.extractAllStop < 0
}
