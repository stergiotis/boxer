package chat

// The cite tool (ADR-0340 (proposed) §SD1, §SD2, §SD4): the model states
// each claim it drew from a result as an assertion — a read-only statement
// over the sources of a query it already made — and the window that holds
// those sources checks it through its `check` operation; each check is a
// verdict entry in the journal, keyed by the cite call and named by the
// assertion's id, which the reply refers to in square brackets. Verify,
// then write: the model calls cite, reads the outcomes, and writes the
// reply knowing which claims held.

import (
	"context"
	"encoding/json/v2"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// The bounds of a cite call (ADR-0340 §SD8).
const (
	maxCiteAssertions = 8
	maxCiteCalls      = 2
)

// The forms an assertion takes and the outcomes of a check (ADR-0340
// §SD1); the entry forms a verdict is written under.
const (
	assertionValue = "value"
	assertionHolds = "holds"

	verdictHeld      = "held"
	verdictFailed    = "failed"
	verdictRefused   = "refused"
	verdictUnchecked = "unchecked"
	verdictMoved     = "moved"

	verdictFormCheck   = "check"
	verdictFormRecheck = "recheck"
)

// opCheck is the window operation a check is dispatched to.
const opCheck = "check"

// assertionIdPattern is what an assertion may be named: letters then
// digits, so a reference [a1] is told from a checkbox [x] and a footnote
// [^1].
var assertionIdPattern = regexp.MustCompile(`^[a-z]{1,4}[0-9]{1,4}$`)

// assertion is one assertion as the model states it (ADR-0340 §SD1).
type assertion struct {
	Id        string   `json:"id"`
	RestsOn   []string `json:"rests_on"`
	Says      string   `json:"says"`
	Form      string   `json:"form"`
	Statement string   `json:"statement"`
	Expect    string   `json:"expect,omitempty"`
}

// assertionOutcome is what the model reads back per assertion.
type assertionOutcome struct {
	Id       string `json:"id"`
	Outcome  string `json:"outcome"`
	Observed string `json:"observed,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// citeResult is the cite call's result.
type citeResult struct {
	Assertions []assertionOutcome `json:"assertions"`
	Note       string             `json:"note"`
}

const citeNote = "Refer to an assertion in your reply by its id in square brackets, e.g. [a1]; state a failed claim as observed, or drop it."

// citeSchema is the cite tool's parameter schema.
const citeSchema = `{"type":"object","properties":{"assertions":{"type":"array","minItems":1,"maxItems":` + "8" + `,"items":{"type":"object","properties":{` +
	`"id":{"type":"string","description":"letters then digits, unique in this turn: a1, a2"},` +
	`"rests_on":{"type":"array","minItems":1,"items":{"type":"string"},"description":"the ids of the tool calls whose results the claim is drawn from: describe_result or sample_rows calls of one window"},` +
	`"says":{"type":"string","description":"the claim as the reply will state it"},` +
	`"form":{"type":"string","enum":["value","holds"],"description":"value: the statement returns one cell, compared with expect at the precision expect shows; holds: it returns one boolean"},` +
	`"statement":{"type":"string","description":"a read-only SQL statement over the tables the cited result's statement read, in the handles that result used, returning one row"},` +
	`"expect":{"type":"string","description":"for value: the figure as the reply states it, with its precision, e.g. 12 % or 1234.5"}},` +
	`"required":["id","rests_on","says","form","statement"],"additionalProperties":false}}},"required":["assertions"],"additionalProperties":false}`

func citeExec(ctx context.Context, inst *coordinator, _ *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
	return inst.cite(ctx, o, call, args)
}

// citedSource is where an assertion's sources were read: the window, the
// coordinator's keys of the cited calls, and the result they named.
type citedSource struct {
	window uint64
	keys   []string
	result uint64
}

// cite checks the call's assertions and journals a verdict for each.
func (inst *coordinator) cite(ctx context.Context, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
	w := inst.journalNow()
	if w == nil {
		return "error: no journal to record verdicts in", "cite: no journal"
	}
	b, err := json.Marshal(args["assertions"])
	if err != nil {
		return "error: assertions are not understood", "cite: arguments not understood"
	}
	var as []assertion
	if err = json.Unmarshal(b, &as); err != nil || len(as) == 0 {
		return "error: assertions must be a list of objects with id, rests_on, says, form and statement", "cite: arguments not understood"
	}
	if len(as) > maxCiteAssertions {
		return "error: at most " + strconv.Itoa(maxCiteAssertions) + " assertions per call", "cite: too many assertions"
	}
	entries := w.Entries()
	if n := citeCallsOfTurn(entries, o.turn); n > maxCiteCalls {
		return "error: at most " + strconv.Itoa(maxCiteCalls) + " cite calls per turn; answer with what held", "cite: budget spent"
	}
	seen := map[string]bool{}
	res := citeResult{Note: citeNote}
	counts := map[string]int{}
	var verdicts []agentrun.Entry
	for _, a := range as {
		out := assertionOutcome{Id: a.Id}
		v := &agentrun.Verdict{Form: a.Form, Outcome: verdictUnchecked}
		observed, confined := "", false
		switch {
		case !assertionIdPattern.MatchString(a.Id):
			out.Reason = "the id must be letters then digits, e.g. a1"
		case seen[a.Id]:
			out.Reason = "the id names an earlier assertion of this call"
		case a.Form != assertionValue && a.Form != assertionHolds:
			out.Reason = "the form must be value or holds"
		case strings.TrimSpace(a.Statement) == "":
			out.Reason = "the statement is empty"
		case a.Form == assertionValue && strings.TrimSpace(a.Expect) == "":
			out.Reason = "a value assertion states what it expects"
		default:
			src, why := inst.citedSource(entries, a.RestsOn)
			if why != "" {
				out.Reason = why
				break
			}
			v.RestsOn, v.Result = src.keys, src.result
			var ch checkRead
			ch, observed, confined, out.Outcome, out.Reason = inst.check(ctx, o, call.Id, a, src)
			v.Outcome, v.Digest, v.Source, v.Endpoint = out.Outcome, ch.statementDigest, ch.sourceDigest, ch.endpoint
			out.Observed = observed
		}
		if out.Outcome == "" {
			out.Outcome = verdictUnchecked
		}
		v.Reason = out.Reason
		seen[a.Id] = true
		counts[out.Outcome]++
		res.Assertions = append(res.Assertions, out)
		canon, _ := json.Marshal(a, json.Deterministic(true))
		label := agentrun.Label{Level: agentrun.LevelSystem}
		if confined {
			label.Tier = inst.confinedTier
		}
		verdicts = append(verdicts, agentrun.Entry{Kind: agentrun.KindVerdict, Key: o.key(), Name: a.Id, Form: verdictFormCheck, Turn: o.turn,
			Body: observed, Arguments: string(canon), Label: label, Verdict: v})
	}
	if _, err = inst.append(journalCtx(ctx), w, verdicts...); err != nil {
		log.Warn().Err(err).Str("conversation", inst.conversation).Msg("chat: the verdicts were not journaled")
		return "error: the verdicts could not be recorded: " + err.Error(), "cite: not journaled"
	}
	b, _ = json.Marshal(res)
	return string(b), "cite · " + citeSummary(counts)
}

// citeSummary is the activity line's count of outcomes, in a fixed order.
func citeSummary(counts map[string]int) string {
	var parts []string
	for _, k := range []string{verdictHeld, verdictFailed, verdictRefused, verdictUnchecked} {
		if n := counts[k]; n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+k)
		}
	}
	if len(parts) == 0 {
		return "nothing checked"
	}
	return strings.Join(parts, ", ")
}

// citeCallsOfTurn counts the turn's cite intents, the one in flight
// included.
func citeCallsOfTurn(entries []agentrun.Entry, turn string) (n int) {
	for _, e := range entries {
		if e.Kind == agentrun.KindIntent && e.Name == "cite" && e.Turn == turn {
			n++
		}
	}
	return
}

// citedSource resolves the tool-call ids an assertion rests on to one
// window, the coordinator's keys of those calls, and the result id their
// outcomes named (ADR-0340 §SD2's gate on the journal side).
func (inst *coordinator) citedSource(entries []agentrun.Entry, restsOn []string) (src citedSource, why string) {
	if len(restsOn) == 0 {
		return src, "rests_on names no call"
	}
	intents := make(map[string]agentrun.Entry, 8)
	for _, e := range entries {
		if e.Kind == agentrun.KindIntent {
			intents[e.Key] = e
		}
	}
	for _, id := range restsOn {
		var outcome *agentrun.Entry
		for i := len(entries) - 1; i >= 0; i-- {
			if e := &entries[i]; e.Kind == agentrun.KindOutcome && e.Role == projection.RoleTool && e.Name == id {
				outcome = e
				break
			}
		}
		if outcome == nil {
			return src, "rests on " + id + ", which is not a tool call of this conversation"
		}
		in, ok := intents[outcome.Key]
		if !ok {
			return src, "rests on " + id + ", whose call is not recorded"
		}
		window, op, isOp := inst.windowOf(in)
		if !isOp {
			return src, "rests on " + id + " (" + in.Name + "), which is not a window operation"
		}
		if in.Effect != agentrun.EffectNone && in.Effect != agentrun.EffectView {
			return src, "rests on " + id + " (" + op + "), which is a command, not a query"
		}
		if len(src.keys) > 0 && window != src.window {
			return src, "rests on calls of two windows; an assertion rests on one"
		}
		result := resultIdOf(outcome.Body)
		if result == 0 {
			return src, "rests on " + id + " (" + op + "), whose result names no result id; cite describe_result or sample_rows"
		}
		if src.result != 0 && result != src.result {
			return src, "rests on two results of the window; an assertion rests on one"
		}
		src.window, src.result = window, result
		src.keys = append(src.keys, outcome.Key)
	}
	return
}

// windowOf is the window and operation an intent called, when it called
// one: a typed tool's, or call_operation's arguments.
func (inst *coordinator) windowOf(in agentrun.Entry) (window uint64, op string, ok bool) {
	inst.mu.Lock()
	t, typed := inst.typed[in.Name]
	inst.mu.Unlock()
	if typed {
		return t.window, t.op, true
	}
	if in.Name != "call_operation" {
		return
	}
	var args struct {
		Window    float64 `json:"window"`
		Operation string  `json:"operation"`
	}
	if json.Unmarshal([]byte(in.Arguments), &args) != nil || args.Operation == "" {
		return
	}
	return uint64(args.Window), args.Operation, true
}

// resultIdOf is the result id a query outcome named — describe_result's
// id or sample_rows' result_id — read out of the outcome as the model saw
// it, delimiters stripped; 0 when it named none.
func resultIdOf(body string) (id uint64) {
	text := body
	if i := strings.Index(text, "\">>\n"); strings.HasPrefix(text, untrustedOpen) && i >= 0 {
		text = text[i+len("\">>\n"):]
		text = strings.TrimSuffix(strings.TrimRight(text, "\n"), untrustedClose)
	}
	var co struct {
		Result map[string]any `json:"result"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &co) != nil || co.Result == nil {
		return
	}
	for _, k := range []string{"result_id", "id"} {
		if f, ok := numberOf(co.Result[k]); ok && f > 0 {
			return uint64(f)
		}
	}
	return
}

// numberOf reads a number out of model JSON, where a result's numbers
// arrive as numbers or as their text.
func numberOf(v any) (f float64, ok bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return
}

// textOf is a model JSON value as text: a string as it is, a number or a
// boolean as it prints.
func textOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// checkRead is what the window's check reported beside its cells.
type checkRead struct {
	statementDigest, sourceDigest, endpoint string
}

// check dispatches one assertion to its window's check and judges the
// row it returns.
func (inst *coordinator) check(ctx context.Context, o toolOrigin, toolCall string, a assertion, src citedSource) (ch checkRead, observed string, confined bool, outcome string, reason string) {
	h := inst.handle()
	if h == "" {
		return ch, "", false, verdictUnchecked, "no task; the window is gone"
	}
	argsJSON, _ := json.Marshal(map[string]any{"result_id": strconv.FormatUint(src.result, 10), "statement": a.Statement})
	key := o.key() + "/" + a.Id
	inst.mu.Lock()
	title, tier := inst.curTitle, inst.curTier
	inst.mu.Unlock()
	out, err := inst.cli.Call(ctx, agent.CallRequest{Handle: h, Instance: src.window, Operation: opCheck, Args: string(argsJSON), Key: key,
		Reason: "check: " + a.Says, Title: title, Turn: o.turn, ModelCall: o.modelCall, ToolCall: toolCall, ToolIndex: uint32(max(o.index, 0)), Tier: uint8(tier)})
	if err == nil {
		out, err = inst.settle(ctx, h, key, out)
	}
	if err != nil {
		if agent.TaskGone(err.Error()) {
			return ch, "", false, verdictUnchecked, err.Error()
		}
		return ch, "", false, verdictRefused, err.Error()
	}
	switch out.Phase {
	case "refused", "denied", "conflict":
		inst.refuse(out.Reason)
		return ch, "", false, verdictRefused, out.Reason
	case "completed", "rendered":
	default:
		return ch, "", false, verdictUnchecked, "the check ended " + out.Phase + ": " + out.Reason
	}
	if out.ResultRef == "" {
		return ch, "", false, verdictUnchecked, "the check returned nothing"
	}
	res, rerr := inst.cli.Read(ctx, h, out.ResultRef)
	if rerr != nil {
		return ch, "", false, verdictUnchecked, "the check's result could not be read: " + rerr.Error()
	}
	if res.Untrusted {
		inst.markTainted()
	}
	// The result is model JSON: its numbers and booleans may arrive as
	// text, so the row is read tolerantly.
	var row map[string]any
	if jerr := json.Unmarshal([]byte(res.Text), &row); jerr != nil {
		return ch, "", false, verdictUnchecked, "the check's result does not parse: " + jerr.Error()
	}
	ch = checkRead{statementDigest: textOf(row["statement_digest"]), sourceDigest: textOf(row["source_digest"]), endpoint: textOf(row["endpoint"])}
	confined = res.Confined || textOf(row["confined"]) == "true"
	cells, _ := row["cells"].([]any)
	if len(cells) == 0 {
		return ch, "", confined, verdictUnchecked, "the check returned no cell"
	}
	observed = textOf(cells[0])
	if nulls, _ := row["nulls"].([]any); slices.ContainsFunc(nulls, func(n any) bool { f, ok := numberOf(n); return ok && f == 0 }) {
		observed = "NULL"
	}
	if a.Form == assertionHolds {
		if observed == "true" || observed == "1" {
			return ch, observed, confined, verdictHeld, ""
		}
		return ch, observed, confined, verdictFailed, "the statement returned " + observed + ", not true"
	}
	if holdsAtPrecision(a.Expect, observed) {
		return ch, observed, confined, verdictHeld, ""
	}
	return ch, observed, confined, verdictFailed, "expected " + a.Expect + ", observed " + observed
}

// numeralPattern is a figure as a reply states it: an optional sign,
// digits with optional thousands separators, an optional fraction.
var numeralPattern = regexp.MustCompile(`[-+]?\d[\d,']*(?:\.\d+)?`)

// holdsAtPrecision says whether observed, rounded to the precision expect
// shows, equals expect (ADR-0340 §SD1): "12 %" holds for 12.4 and not for
// 12.6, "12.4 %" for 12.41 only. An expectation that is not a figure must
// match the observed text, case and surrounding space aside.
func holdsAtPrecision(expect string, observed string) bool {
	e := numeralPattern.FindString(expect)
	if e == "" {
		return strings.EqualFold(strings.TrimSpace(expect), strings.TrimSpace(observed))
	}
	ev, err := strconv.ParseFloat(strings.NewReplacer(",", "", "'", "").Replace(e), 64)
	if err != nil {
		return false
	}
	o := numeralPattern.FindString(observed)
	if o == "" {
		return false
	}
	ov, err := strconv.ParseFloat(strings.NewReplacer(",", "", "'", "").Replace(o), 64)
	if err != nil {
		return false
	}
	decimals := 0
	if i := strings.IndexByte(e, '.'); i >= 0 {
		decimals = len(e) - i - 1
	}
	scale := math.Pow(10, float64(decimals))
	return math.Abs(math.Round(ov*scale)/scale-ev) < 0.5/scale*1e-6+1e-9
}
