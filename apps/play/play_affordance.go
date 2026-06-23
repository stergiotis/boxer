package play

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// "Affordance" — terminology note.
//
// Used here in the HCI sense (Norman / Gibson): the inline tooling an
// editor offers for a recognised piece of structure — what the UI
// invites you to do at that location. In this package it specifically
// means the small interactive panel attached to a detected SQL function
// call (regex tester for the multiMatch* family, time-range picker for
// toDateTime literals, geo-cell editor for h3, …). The term is broader
// than "inspector" (read-only) and broader than "editor" (mutates the
// buffer); it covers both phases — v1 inspection, v3 edit-back via
// byte-range patches.
//
// Naming convention in this codebase: types prefixed with
// `sqlAffordance…`, instances stored in PlayApp.affordances, registry
// drained from FunctionEvaluator's OnObservation callback.
//
// sqlAffordanceI renders inline tooling for an observed SQL function call.
// State (test inputs, expanded/collapsed, …) lives on the affordance struct
// itself so it survives across debounce-driven re-parses; the runtime
// keys the Render call by observation, so multiple call sites of the same
// function name share the affordance instance and its state.
type sqlAffordanceI interface {
	// Matches reports whether this affordance handles the observation.
	// Names arrive lower-cased; compare against the lowercased form.
	Matches(obs nanopass.Observation) bool

	// Render draws the affordance UI for one observation inside the current
	// ui scope. The caller wraps each call in an IdScope keyed by the
	// observation index, so internal stable string IDs don't collide across
	// repeats of the same affordance.
	Render(ctx *affordanceCtx)
}

// affordanceCtx is what each Render receives; grows additively over time.
type affordanceCtx struct {
	Ids       *c.WidgetIdStack
	Obs       nanopass.Observation
	SQL       string // editor buffer — used to slice obs.Src
	TestInput string // shared regex test input, owned by PlayApp
}

// extractedArg is one syntactic argument of a function call, post-scan.
// Literal=true means Text is the unescaped value of a single-quoted SQL
// string literal; Literal=false leaves Text as the raw source-text slice
// so the UI can display it as a hint ("(non-literal: foo.col)").
type extractedArg struct {
	Text    string
	Literal bool
}

// extractCallArgs scans the source bytes spanned by a function call (the
// half-open byte range covered by an ANTLR ColumnExprFunctionContext) and
// returns one entry per top-level argument.
//
// The scanner is paren-aware (skips nested calls) and quote-aware (skips
// string-literal contents including backslash-escapes). Arguments that
// aren't simple single-quoted literals come back with Literal=false and
// the raw substring in Text — useful for showing "(non-literal)" hints
// without losing the source text.
//
// Used by affordances when Args isn't sufficient — e.g. when a column-ref
// haystack disqualifies the whole call from FunctionEvaluator's all-or-
// nothing folding (Evaluated=false in the observation), but the regex
// args are still recoverable from the source.
func extractCallArgs(sql string, src nanopass.SourceRange) []extractedArg {
	if src.Empty() || src.Start < 0 || src.End > len(sql) {
		return nil
	}
	body := sql[src.Start:src.End]
	open := strings.IndexByte(body, '(')
	if open < 0 {
		return nil
	}
	return splitArgs(parenBody(body, open))
}

// splitArgs splits the comma-separated argument body of a call (or array)
// into one extractedArg per top-level argument, dropping empty segments
// (e.g. a trailing comma). Each segment is run through parseArg.
func splitArgs(body string) []extractedArg {
	segs := splitTopLevelArgs(body)
	args := make([]extractedArg, 0, len(segs))
	for _, seg := range segs {
		if strings.TrimSpace(seg) == "" {
			continue
		}
		args = append(args, parseArg(seg))
	}
	return args
}

// splitTopLevelArgs splits s on the commas that sit at nesting depth zero,
// honouring nested () / [] groups and single-quoted string literals (with
// backslash escapes) so a comma inside an array literal or a string does not
// split an argument. Segments are returned raw (the caller trims / parseArgs).
func splitTopLevelArgs(s string) []string {
	var out []string
	depth, start := 0, 0
	for j := 0; j < len(s); j++ {
		switch s[j] {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:j])
				start = j + 1
			}
		case '\'':
			j = skipSQLLiteral(s, j) - 1
		}
	}
	return append(out, s[start:])
}

// parenBody returns the substring between the '(' at index open in body and
// its matching ')', honouring nested () / [] and single-quoted literals. When
// the closing paren is missing it returns everything after the open paren.
func parenBody(body string, open int) string {
	depth := 0
	for j := open; j < len(body); j++ {
		switch body[j] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
			if depth == 0 {
				return body[open+1 : j]
			}
		case '\'':
			j = skipSQLLiteral(body, j) - 1
		}
	}
	return body[open+1:]
}

// skipSQLLiteral returns the index just past the single-quoted string literal
// that starts at s[i] == '\'' (handling backslash escapes). Returns len(s)
// when the literal is unterminated.
func skipSQLLiteral(s string, i int) int {
	for k := i + 1; k < len(s); k++ {
		if s[k] == '\\' && k+1 < len(s) {
			k++
			continue
		}
		if s[k] == '\'' {
			return k + 1
		}
	}
	return len(s)
}

// arrayStringLiterals parses a ClickHouse array literal `[a, b, …]` into its
// top-level elements, each run through parseArg (so single-quoted elements
// come back Literal=true with the unescaped value). Returns nil when s is not
// a bracketed array — the caller then falls back to treating trailing call
// arguments as the pattern list directly.
func arrayStringLiterals(s string) []extractedArg {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil
	}
	return splitArgs(s[1 : len(s)-1])
}

func parseArg(s string) extractedArg {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		unescaped, err := marshalling.UnescapeString(s)
		if err == nil {
			return extractedArg{Text: unescaped, Literal: true}
		}
	}
	return extractedArg{Text: s, Literal: false}
}

// multiMatchSpec describes one member of the ClickHouse multi-pattern regex
// family: its canonical (camelCase) display name and whether it carries a
// leading fuzzy `distance` argument between the haystack and the pattern array.
type multiMatchSpec struct {
	display string
	fuzzy   bool
}

// multiMatchRegexFamily is the canonical set of ClickHouse RE2-regex
// multi-pattern matchers, keyed by the lowercased name the FunctionEvaluator
// reports in obs.Name. In every member the patterns are the final argument —
// an `Array(String)` literal; the fuzzy three additionally take a UInt
// `distance` between the haystack and that array. multiSearch* is excluded on
// purpose: it matches plain substrings, not regexes, so a regex tester would
// mislead. Both play_observation.go (registration) and the affordance
// (matching/rendering) read this one map.
var multiMatchRegexFamily = map[string]multiMatchSpec{
	"multimatchany":             {display: "multiMatchAny", fuzzy: false},
	"multimatchanyindex":        {display: "multiMatchAnyIndex", fuzzy: false},
	"multimatchallindices":      {display: "multiMatchAllIndices", fuzzy: false},
	"multifuzzymatchany":        {display: "multiFuzzyMatchAny", fuzzy: true},
	"multifuzzymatchanyindex":   {display: "multiFuzzyMatchAnyIndex", fuzzy: true},
	"multifuzzymatchallindices": {display: "multiFuzzyMatchAllIndices", fuzzy: true},
}

// multiMatchAffordance renders a regex tester for ClickHouse's multiMatch* /
// multiFuzzyMatch* families. args[0] is the haystack (often a column ref,
// hence FunctionEvaluator's Evaluated=false); the trailing `[…]` array holds
// the needle patterns. Literal patterns are compiled with regexp.Compile and
// tested against the shared affordanceTestInput field on PlayApp.
type multiMatchAffordance struct {
	// Currently no per-affordance state beyond what's on PlayApp.
	// Future: per-call test inputs would key on (Src.Start, Src.End).
}

func (a *multiMatchAffordance) Matches(obs nanopass.Observation) bool {
	_, ok := multiMatchRegexFamily[obs.Name]
	return ok
}

// splitMultiMatchArgs separates a multiMatch* / multiFuzzyMatch* argument list
// into the haystack, the optional fuzzy `distance`, and the pattern needles.
// Patterns come from the trailing array literal; if the final argument is not
// an array (a loose / hand-written form) the trailing arguments are treated as
// patterns directly so the tester still has something to compile.
func splitMultiMatchArgs(args []extractedArg, fuzzy bool) (haystack string, distance string, patterns []extractedArg) {
	if len(args) == 0 {
		return
	}
	haystack = args[0].Text
	rest := args[1:]
	if fuzzy && len(rest) > 0 {
		distance = rest[0].Text
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return
	}
	if elems := arrayStringLiterals(rest[len(rest)-1].Text); elems != nil {
		patterns = elems
		return
	}
	patterns = rest
	return
}

func (a *multiMatchAffordance) Render(ctx *affordanceCtx) {
	spec := multiMatchRegexFamily[ctx.Obs.Name]
	args := extractCallArgs(ctx.SQL, ctx.Obs.Src)
	haystack, distance, patterns := splitMultiMatchArgs(args, spec.fuzzy)
	for range c.Vertical().KeepIter() {
		// Header: function name + char range + non-literal hint.
		header := fmt.Sprintf("%s @ %d–%d",
			spec.display, ctx.Obs.Src.Start, ctx.Obs.Src.End)
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel(header) {
				rt.Strong()
			}
			if !ctx.Obs.Evaluated {
				for rt := range c.RichTextLabel("(non-literal haystack)") {
					rt.Small().Weak()
				}
			}
		}

		// Haystack — labelled but not regex-tested.
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel("haystack") {
				rt.Weak()
			}
			c.Label(haystack).Truncate().Send()
		}

		// Fuzzy distance (max edits) — labelled, not a pattern.
		if spec.fuzzy && distance != "" {
			for range c.Horizontal().KeepIter() {
				for rt := range c.RichTextLabel("max edits") {
					rt.Weak()
				}
				c.Label(distance).Truncate().Send()
			}
		}

		if len(patterns) == 0 {
			for rt := range c.RichTextLabel("(no patterns extracted)") {
				rt.Small().Weak()
			}
			return
		}

		// Pattern needles — compile each and report match counts.
		for i, arg := range patterns {
			for range c.Horizontal().KeepIter() {
				for rt := range c.RichTextLabel(fmt.Sprintf("[%d]", i)) {
					rt.Weak()
				}
				if !arg.Literal {
					c.Label("(non-literal)").Truncate().Send()
					for rt := range c.RichTextLabel(arg.Text) {
						rt.Small().Weak()
					}
					continue
				}
				re, err := regexp.Compile(arg.Text)
				if err != nil {
					c.Label(fmt.Sprintf("'%s'", arg.Text)).Truncate().Send()
					for rt := range c.RichTextLabel("⚠ " + err.Error()) {
						rt.Small().Weak()
					}
					continue
				}
				matches := 0
				if ctx.TestInput != "" {
					matches = len(re.FindAllStringIndex(ctx.TestInput, -1))
				}
				c.Label(fmt.Sprintf("'%s'", arg.Text)).Truncate().Send()
				for rt := range c.RichTextLabel(fmt.Sprintf("→ %d match(es)", matches)) {
					rt.Small().Weak()
				}
			}
		}
	}
}

// renderAffordances renders the shared test-input plus one block per
// observation under the canonical-form preview. No-op when there are no
// observations (and no leftover state to draw).
func (inst *PlayApp) renderAffordances() {
	if len(inst.observations) == 0 {
		return
	}
	ids := inst.ids

	c.Separator().Horizontal().Send()
	for rt := range c.RichTextLabel("AFFORDANCES") {
		rt.Small().Weak()
	}

	// Shared regex test input. One TextEdit, all regex affordances read it.
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel("test input") {
			rt.Weak()
		}
		c.TextEdit(ids.PrepareStr("affordanceTestInput"),
			inst.affordanceTestInput, false).
			DesiredWidth(float32(math.Inf(1))).
			HintText("type a string to test regex matches…").
			SendRespVal(&inst.affordanceTestInput)
	}

	for i, obs := range inst.observations {
		for _, a := range inst.affordances {
			if !a.Matches(obs) {
				continue
			}
			for range c.IdScope(ids.PrepareSeq(uint64(0x02000000 + i))) {
				a.Render(&affordanceCtx{
					Ids:       ids,
					Obs:       obs,
					SQL:       inst.sql,
					TestInput: inst.affordanceTestInput,
				})
			}
			break
		}
	}
}
