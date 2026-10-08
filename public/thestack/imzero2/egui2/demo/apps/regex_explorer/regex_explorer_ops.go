package regex_explorer

// Operations (ADR-0269): what an agent holding a grant for an explorer
// window can read and change.
//
// The operations change the explorer's inputs and read what the window
// already computes from them. None of them asks ClickHouse anything: an
// agent sets the inputs, the window's own query lanes run as they do for
// a person, and get_functions / get_multi read the lanes, reporting
// pending until ClickHouse has answered. The window's ClickHouse traffic
// is therefore the same whoever set the inputs, and no new path is opened
// to the broker — which does not check agent limits (OnBehalfOf).
//
// deferred: the playground hand-off (compare_in_playground). It publishes
// two ad-hoc datasets and opens a play window; as an operation it would
// have to carry OnBehalfOf through both, and its effect and consent need
// deciding. The person's button is unaffected.
//
// The embedded explorer (EmbeddedApp) has no window host and serves no
// operations.

import (
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
)

const (
	resPattern     = "pattern"
	resHaystack    = "haystack"
	resReplacement = "replacement"
	resPatternList = "pattern_list"
	resFlags       = "flags"
	resTab         = "tab"

	opGetState       = "get_state"
	opSetInputs      = "set_inputs"
	opShowTab        = "show_tab"
	opApplyShowcase  = "apply_showcase"
	opGetMatches     = "get_matches"
	opGetFunctions   = "get_functions"
	opGetMulti       = "get_multi"
	opGetEngineCheck = "get_engine_check"
)

// tabNames spells the result tabs for operations, in resultTabE order.
var tabNames = [...]string{tabMatches: "matches", tabFunctions: "functions", tabMulti: "multi"}

// opsSnap is what the queries read: copies and immutable values taken on
// the render goroutine after the command stage. The analysis' slices and
// the lanes' values are replaced, never mutated, when inputs change, so
// sharing them with the query goroutine is safe.
type opsSnap struct {
	pattern, haystack, replacement, patternList string
	caseInsensitive, multiline, dotAll          bool
	tab                                         resultTabE

	analysis  patternAnalysis
	fnView    laneView[fnOutcome]
	repView   laneView[replaceOutcome]
	multiView laneView[[]multiLine]
	// multiLines is the live parse of the pattern list: what get_multi
	// reports per line while the lane has no current answer.
	multiLines []multiLine
}

func takeOpsSnap(inst *App) (s opsSnap) {
	s = opsSnap{
		pattern: inst.pattern, haystack: inst.haystack, replacement: inst.replacement, patternList: inst.patternList,
		caseInsensitive: inst.caseInsensitive, multiline: inst.multiline, dotAll: inst.dotAll,
		tab:      inst.tab,
		analysis: *inst.analysis(),
	}
	if s.analysis.state == patternValid {
		s.fnView = inst.fnLane.view(inst.singleKey())
		s.repView = inst.replaceLane.view(inst.replaceKey())
	}
	s.multiLines = inst.parseAndValidatePatternList(inst.patternList)
	s.multiView = inst.multiLane.view(inst.multiKey())
	return
}

// State is get_state's result.
type State struct {
	Pattern          string   `desc:"the single pattern, as typed"`
	Haystack         string   `desc:"the text the pattern is matched against"`
	Replacement      string   `desc:"the replacement the replace functions use"`
	PatternList      string   `desc:"the multi-pattern input, one pattern per line"`
	CaseInsensitive  bool     `desc:"the (?i) flag"`
	Multiline        bool     `desc:"the (?m) flag"`
	DotAll           bool     `desc:"the (?s) flag: dot matches a newline, ClickHouse's default"`
	EffectivePattern string   `desc:"the pattern with the flags applied, as both engines receive it"`
	CompileError     string   `desc:"why the pattern does not compile; empty when it does or is empty"`
	Matches          int      `desc:"how many non-empty matches Go's regexp finds"`
	Tab              string   `desc:"the result tab on screen: matches, functions or multi"`
	Showcases        []string `desc:"the showcase names apply_showcase accepts"`
}

// SetInputsArgs is set_inputs' argument: every field is optional, and
// one left out keeps its value.
type SetInputsArgs struct {
	Pattern         *string `desc:"the single pattern (RE2 syntax)"`
	Haystack        *string `desc:"the text to match against"`
	Replacement     *string `desc:"the replacement for replaceRegexpOne / replaceRegexpAll: \\1, \\2 … for groups, \\0 for the match"`
	PatternList     *string `desc:"the multi-pattern input, one pattern per line"`
	CaseInsensitive *bool   `desc:"the (?i) flag"`
	Multiline       *bool   `desc:"the (?m) flag"`
	DotAll          *bool   `desc:"the (?s) flag"`
}

// ShowTabArgs is show_tab's argument.
type ShowTabArgs struct {
	Tab string `desc:"matches, functions or multi"`
}

// ApplyShowcaseArgs is apply_showcase's argument.
type ApplyShowcaseArgs struct {
	Name string `desc:"a showcase name, as get_state lists them"`
}

// Group is one capture group of one match.
type Group struct {
	Number int    `desc:"the group's number, from 1"`
	Name   string `desc:"its (?P<name>…) name, empty when it has none"`
	Set    bool   `desc:"whether the group took part in this match"`
	Start  int    `desc:"byte offset where the group starts; -1 when not set"`
	End    int    `desc:"byte offset where it ends; -1 when not set"`
	Text   string `desc:"the group's text"`
}

// Match is one non-empty match.
type Match struct {
	Start  int     `desc:"byte offset where the match starts"`
	End    int     `desc:"byte offset where it ends"`
	Text   string  `desc:"the matched text"`
	Groups []Group `desc:"the capture groups, in order"`
}

// MatchesResult is get_matches' result.
type MatchesResult struct {
	Valid           bool    `desc:"whether the pattern compiles; when not, see get_state's compile_error"`
	Count           int     `desc:"how many non-empty matches there are"`
	Matches         []Match `desc:"the matches, at most the first 200"`
	Truncated       bool    `desc:"whether matches were left out past the cap"`
	ExtractAllCount int     `desc:"how many of the matches ClickHouse's extractAll returns: it stops at the first match of the empty string"`
	ExtractAllStop  int     `desc:"byte offset where extractAll stops early; -1 when it does not"`
}

// FunctionRow is one ClickHouse function's row in get_functions.
type FunctionRow struct {
	Function   string `desc:"the function, as ClickHouse spells it"`
	SQL        string `desc:"the expression evaluated, over a column named haystack"`
	ClickHouse string `desc:"ClickHouse's value, printed the way ClickHouse prints it; empty while pending or on error"`
	Prediction string `desc:"what the Go model predicts; empty when the function is not modelled for this input"`
	Modelled   bool   `desc:"whether the Go model predicts this function for this input"`
	Agrees     bool   `desc:"whether the prediction equals ClickHouse's value"`
	Pending    bool   `desc:"whether ClickHouse has not answered for the current inputs yet"`
	Error      string `desc:"ClickHouse's error for this function, if it failed"`
}

// FunctionsResult is get_functions' result.
type FunctionsResult struct {
	Valid   bool          `desc:"whether the pattern compiles; nothing is asked of ClickHouse until it does"`
	Pending bool          `desc:"whether any row still waits for ClickHouse — call again shortly"`
	Rows    []FunctionRow `desc:"one row per function"`
}

// MultiLine is one line of the multi-pattern input in get_multi.
type MultiLine struct {
	Line    int    `desc:"the line's number among the non-empty lines, from 1"`
	Pattern string `desc:"the line's pattern"`
	Status  string `desc:"hit, miss, invalid (Go's regexp rejects it, so it is not sent), refused (VectorScan rejects it), failed (the whole set failed — see error) or pending"`
	Message string `desc:"the reason for invalid or refused"`
}

// MultiResult is get_multi's result.
type MultiResult struct {
	Pending bool        `desc:"whether ClickHouse has not answered for the current inputs yet — call again shortly"`
	Error   string      `desc:"ClickHouse's error when the whole set failed"`
	Lines   []MultiLine `desc:"one entry per non-empty line"`
}

// EngineCheck is get_engine_check's result.
type EngineCheck struct {
	Status     string   `desc:"not started, running, ok, mismatch or could not run"`
	Mismatches []string `desc:"corpus cases where the app's prediction of ClickHouse is wrong"`
	Known      []string `desc:"corpus cases where the engines differ as documented and modelled"`
	Error      string   `desc:"why the check could not run"`
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet(takeOpsSnap)

	text := func(name string, summary string, field func(inst *App) *string, h func(inst *App) *widgetEdit) {
		s.Resource(name, summary, func(inst *App) any { return *field(inst) })
		s.Restorable(name, func(inst *App, v any) bool {
			t, ok := v.(string)
			if ok {
				*field(inst) = t
			}
			return ok
		})
		s.Editing(name, func(inst *App) bool { return h(inst).editing() })
	}
	text(resPattern, "the single pattern", func(inst *App) *string { return &inst.pattern }, func(inst *App) *widgetEdit { return &inst.patternEdit })
	text(resHaystack, "the haystack", func(inst *App) *string { return &inst.haystack }, func(inst *App) *widgetEdit { return &inst.haystackEdit })
	text(resReplacement, "the replacement", func(inst *App) *string { return &inst.replacement }, func(inst *App) *widgetEdit { return &inst.replacementEdit })
	text(resPatternList, "the multi-pattern input", func(inst *App) *string { return &inst.patternList }, func(inst *App) *widgetEdit { return &inst.patternListEdit })
	s.Resource(resFlags, "the case-insensitive, multiline and dot-all flags", func(inst *App) any { return inst.flagPrefix() })
	s.Resource(resTab, "the result tab on screen", func(inst *App) any { return inst.tab })

	allInputs := []string{resPattern, resHaystack, resReplacement, resPatternList, resFlags}

	appops.Query(s, app.OperationSpec{Name: opGetState, Version: 1,
		Summary: "read the inputs, the flags, whether the pattern compiles and how many matches it has",
		Reads:   append(allInputs, resTab), Agents: true, Untrusted: true},
		func(sn opsSnap, in appops.None) (out State, err error) {
			out = State{
				Pattern: sn.pattern, Haystack: sn.haystack, Replacement: sn.replacement, PatternList: sn.patternList,
				CaseInsensitive: sn.caseInsensitive, Multiline: sn.multiline, DotAll: sn.dotAll,
				EffectivePattern: sn.analysis.pattern,
				Matches:          len(sn.analysis.matches),
				Tab:              tabNames[sn.tab],
			}
			if sn.analysis.err != nil {
				out.CompileError = compileErrorText(sn.analysis.err)
			}
			for _, sc := range showcaseCases {
				out.Showcases = append(out.Showcases, sc.Title)
			}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetInputs, Version: 1,
		Summary: "set any of the pattern, haystack, replacement, multi-pattern input and flags; fields left out keep their value",
		Effect:  app.OperationEffectDocument, Writes: allInputs, Agents: true, Gesture: "typing in the inputs or ticking a flag"},
		func(inst *App, call app.OperationCall, in SetInputsArgs) (out appops.None, err error) {
			for _, f := range []struct {
				v   *string
				dst *string
			}{{in.Pattern, &inst.pattern}, {in.Haystack, &inst.haystack}, {in.Replacement, &inst.replacement}, {in.PatternList, &inst.patternList}} {
				if f.v != nil {
					*f.dst = *f.v
				}
			}
			for _, f := range []struct {
				v   *bool
				dst *bool
			}{{in.CaseInsensitive, &inst.caseInsensitive}, {in.Multiline, &inst.multiline}, {in.DotAll, &inst.dotAll}} {
				if f.v != nil {
					*f.dst = *f.v
				}
			}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opShowTab, Version: 1,
		Summary: "bring a result tab on screen: matches, functions or multi",
		Effect:  app.OperationEffectView, Writes: []string{resTab}, Agents: true, Gesture: "clicking a result tab"},
		func(inst *App, call app.OperationCall, in ShowTabArgs) (out appops.None, err error) {
			for t, name := range tabNames {
				if strings.EqualFold(in.Tab, name) {
					inst.tab = resultTabE(t)
					return
				}
			}
			err = app.RefuseOperation("the tab is one of matches, functions or multi")
			return
		})

	appops.Command(s, app.OperationSpec{Name: opApplyShowcase, Version: 1,
		Summary: "replace the pattern and haystack with a named showcase",
		Effect:  app.OperationEffectDocument, Writes: []string{resPattern, resHaystack}, Agents: true, Gesture: "clicking a showcase"},
		func(inst *App, call app.OperationCall, in ApplyShowcaseArgs) (out appops.None, err error) {
			for _, sc := range showcaseCases {
				if strings.EqualFold(in.Name, sc.Title) {
					inst.applyShowcase(sc.Pattern, sc.Haystack)
					return
				}
			}
			err = app.RefuseOperation("no showcase by that name; get_state lists them")
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetMatches, Version: 1,
		Summary: "read Go's matches of the pattern in the haystack, with byte offsets and capture groups, and where ClickHouse's extractAll stops",
		Reads:   allInputs, Agents: true, Untrusted: true},
		func(sn opsSnap, in appops.None) (out MatchesResult, err error) {
			out = matchesResult(&sn.analysis)
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetFunctions, Version: 1,
		Summary: "read what each ClickHouse regex function returns for the inputs, beside the Go model's prediction; pending until ClickHouse answers",
		Reads:   allInputs, Agents: true, Untrusted: true},
		func(sn opsSnap, in appops.None) (out FunctionsResult, err error) {
			out = functionsResult(&sn)
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetMulti, Version: 1,
		Summary: "read the multi-pattern result per line: hit, miss, invalid, or refused by VectorScan; pending until ClickHouse answers",
		Reads:   allInputs, Agents: true, Untrusted: true},
		func(sn opsSnap, in appops.None) (out MultiResult, err error) {
			out = multiResult(&sn)
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetEngineCheck, Version: 1,
		Summary: "read the engine check: whether the app's predictions of ClickHouse held on its fixed test corpus",
		Agents:  true},
		func(sn opsSnap, in appops.None) (out EngineCheck, err error) {
			out = engineCheckResult()
			return
		})
	return
}()

// Operations serves the catalog for this window.
func (inst *AppInstance) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst.state) }

var _ app.OperationsAppI = (*AppInstance)(nil)

// widgetEdit remembers an input's widget handle, taken where the input is
// built, so the operations engine can tell the person is typing in it:
// an agent's change to that input then conflicts instead of overwriting
// what is being typed. An input not drawn this frame is not being edited.
type widgetEdit struct {
	h  widgethandle.WidgetHandle
	ok bool
}

func (inst *widgetEdit) set(id uint64) {
	inst.h, inst.ok = widgethandle.Make(id), true
}

func (inst *widgetEdit) editing() bool {
	return inst.ok && appops.WidgetEditing(inst.h)
}

// gesture routes a person's click through the operation an agent would
// call (ADR-0269 §SD8, one path). Where no host serves the catalog — the
// embedded explorer, a demo scene — it applies fallback instead.
func gesture[In any](inst *App, op string, in In, fallback func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	fallback()
}

func matchesResult(a *patternAnalysis) (out MatchesResult) {
	out.ExtractAllStop = -1
	if a.state != patternValid {
		return
	}
	out.Valid = true
	out.Count = len(a.matches)
	out.ExtractAllCount = a.extractAllN
	out.ExtractAllStop = a.extractAllStop
	names := a.re.SubexpNames()
	for i, m := range a.matches {
		if i >= maxMatchRows {
			out.Truncated = true
			break
		}
		mt := Match{Start: m[0], End: m[1], Text: a.haystack[m[0]:m[1]]}
		for k := 1; 2*k+1 < len(m); k++ {
			g := Group{Number: k, Name: subexpName(names, k), Start: m[2*k], End: m[2*k+1]}
			if g.Start >= 0 {
				g.Set = true
				g.Text = a.haystack[g.Start:g.End]
			}
			mt.Groups = append(mt.Groups, g)
		}
		out.Matches = append(out.Matches, mt)
	}
	return
}

func functionsResult(sn *opsSnap) (out FunctionsResult) {
	a := &sn.analysis
	if a.state != patternValid {
		return
	}
	out.Valid = true
	predicted := predictFunctions(a)
	for _, fn := range patternFns {
		if fn == fnExtractAllGroups && !predicted.YieldsGroups {
			continue
		}
		row := FunctionRow{Function: fn.name(), SQL: fn.expr(a.pattern, sn.replacement)}
		row.Modelled = fn != fnExtractAllGroups || a.groupsModelled()
		if row.Modelled {
			row.Prediction = fnValue(fn, predicted)
		}
		fillFnRow(&row, sn.fnView, func(o fnOutcome) string { return fnValue(fn, o) })
		out.Rows = append(out.Rows, row)
	}
	for _, fn := range replaceFns {
		row := FunctionRow{Function: fn.name(), SQL: fn.expr(a.pattern, sn.replacement)}
		fillFnRow(&row, sn.repView, func(o replaceOutcome) string { return fnReplaceValue(fn, o) })
		out.Rows = append(out.Rows, row)
	}
	for _, r := range out.Rows {
		out.Pending = out.Pending || r.Pending
	}
	return
}

// fillFnRow fills a row's ClickHouse side from the lane that answers it.
func fillFnRow[T any](row *FunctionRow, v laneView[T], value func(T) string) {
	switch {
	case v.Err != nil:
		row.Error = clickHouseMessage(v.Err)
	case !v.Fresh:
		row.Pending = true
	default:
		row.ClickHouse = value(v.Value)
		row.Agrees = row.Modelled && row.Prediction == row.ClickHouse
	}
}

func multiResult(sn *opsSnap) (out MultiResult) {
	lines := sn.multiLines
	v := sn.multiView
	switch {
	case len(lines) == 0:
		return
	case v.Err != nil:
		out.Error = clickHouseMessage(v.Err)
	case v.Fresh:
		lines = v.Value
	default:
		out.Pending = true
	}
	for i, l := range lines {
		ml := MultiLine{Line: i + 1, Pattern: l.Text}
		switch {
		case l.Invalid:
			ml.Status = "invalid"
			// Recompiled here, off the render goroutine, for the message
			// alone: the compile cache belongs to the window.
			if _, err := regexp.Compile(inlineFlags(sn.caseInsensitive, sn.multiline, sn.dotAll) + l.Text); err != nil {
				ml.Message = compileErrorText(err)
			}
		case out.Error != "":
			ml.Status = "failed"
		case out.Pending:
			ml.Status = "pending"
		case l.Rejected != "":
			ml.Status, ml.Message = "refused", l.Rejected
		case l.Hit:
			ml.Status = "hit"
		default:
			ml.Status = "miss"
		}
		out.Lines = append(out.Lines, ml)
	}
	return
}

func engineCheckResult() (out EngineCheck) {
	tw, started, running := tripwireSnapshot()
	names := func(idx []int) (n []string) {
		for _, i := range idx {
			n = append(n, tripwireCorpus[i].Name)
		}
		return
	}
	switch {
	case !started:
		out.Status = "not started"
	case running:
		out.Status = "running"
	case tw.Err != nil:
		out.Status, out.Error = "could not run", clickHouseMessage(tw.Err)
	case len(tw.Drifts) > 0:
		out.Status = "mismatch"
	default:
		out.Status = "ok"
	}
	out.Mismatches, out.Known = names(tw.Drifts), names(tw.Known)
	return
}
