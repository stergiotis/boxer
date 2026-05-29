//go:build llm_generated_opus47

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
// call (regex tester for multiMatchIndexAny, time-range picker for
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

// extractCallArgs scans the source bytes spanned by a function call (i.e.
// the inclusive range covered by an ANTLR ColumnExprFunctionContext) and
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
	if src.Empty() || src.Start < 0 || src.Stop >= len(sql) {
		return nil
	}
	body := sql[src.Start : src.Stop+1]
	open := strings.IndexByte(body, '(')
	if open < 0 {
		return nil
	}
	body = body[open+1:]

	var args []extractedArg
	depth := 0
	argStart := 0
	for j := 0; j < len(body); j++ {
		switch body[j] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
				continue
			}
			args = append(args, parseArg(body[argStart:j]))
			return args
		case ',':
			if depth == 0 {
				args = append(args, parseArg(body[argStart:j]))
				argStart = j + 1
			}
		case '\'':
			// Skip past the literal so commas/parens inside don't fool us.
			k := j + 1
			for k < len(body) {
				if body[k] == '\\' && k+1 < len(body) {
					k += 2
					continue
				}
				if body[k] == '\'' {
					k++
					break
				}
				k++
			}
			j = k - 1
		}
	}
	return args
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

// multiMatchAffordance renders a regex tester for ClickHouse's
// multiMatch* family. arg[0] is the haystack (often a column ref,
// hence FunctionEvaluator's Evaluated=false); arg[1..N] are needles.
// String-literal needles are compiled with regexp.Compile and tested
// against the shared affordanceTestInput field on PlayApp.
type multiMatchAffordance struct {
	// Currently no per-affordance state beyond what's on PlayApp.
	// Future: per-call test inputs would key on (Src.Start, Src.Stop).
}

func (a *multiMatchAffordance) Matches(obs nanopass.Observation) bool {
	return obs.Name == "multimatchindexany"
}

func (a *multiMatchAffordance) Render(ctx *affordanceCtx) {
	args := extractCallArgs(ctx.SQL, ctx.Obs.Src)
	for range c.Vertical().KeepIter() {
		// Header: function name + char range + non-literal hint.
		header := fmt.Sprintf("multiMatchIndexAny @ %d–%d",
			ctx.Obs.Src.Start, ctx.Obs.Src.Stop+1)
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

		if len(args) < 2 {
			for rt := range c.RichTextLabel("(no patterns extracted)") {
				rt.Small().Weak()
			}
			return
		}

		// args[0] is the haystack — labelled but not regex-tested.
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel("haystack") {
				rt.Weak()
			}
			c.Label(args[0].Text).Truncate().Send()
		}

		// args[1..] are needles — compile each and report match counts.
		for i := 1; i < len(args); i++ {
			arg := args[i]
			for range c.Horizontal().KeepIter() {
				for rt := range c.RichTextLabel(fmt.Sprintf("[%d]", i-1)) {
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
