package play

// trace_rewrite: the client-side rewrite of a statement as an agent reads it
// — every step in the order it ran, what it did, how long it took and why it
// failed, and the body that would ship. It is the trace the Passes tab and
// the Diagnostics pane draw (Client.RewriteTrace), over the code path that
// executes, so what it reports is what a run would send.
//
// An external read like the schema reads (ADR-0270, 2026-10-02): the
// late-bound passes resolve handles and LW_GET against the endpoint's catalog,
// so without the endpoint among an agent's destinations they are left out and
// the trace shows them declined.

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

const opTraceRewrite = "trace_rewrite"

// Bounds on what trace_rewrite returns.
const (
	traceMaxBody      = 32 << 10
	traceMaxCostNodes = 200
)

// playStepDocs describe play's own steps, which sit outside the registry and
// so carry no catalog description.
var playStepDocs = map[string]string{
	rewriteStepExtractParams:    "lift the SET param_* prelude into query parameters",
	rewriteStepSpliceExpr:       "substitute the buffer's `-- play: expr` SQL-valued placeholders",
	rewriteStepExposeConditions: "expose leeway selection conditions as columns, when the toolbar toggle is on (ADR-0121)",
	rewriteStepSetFormat:        "append the wire format, ArrowStream; an INSERT takes none",
}

// TraceArgs is trace_rewrite's argument.
type TraceArgs struct {
	Sql   string `json:",omitzero" desc:"the statement to trace; the buffer when left out"`
	Costs bool   `json:",omitzero" desc:"true adds each pass's internal breakdown: the pass invocations it made, with durations"`
}

// TraceCost is one invocation inside a pass's breakdown.
type TraceCost struct {
	Depth   int    `desc:"nesting under the step, 0 for the step's own pass"`
	Name    string `desc:"the invoked pass"`
	Micros  int64  `desc:"wall-clock microseconds, children included"`
	Iters   int    `json:",omitzero" desc:"fixed-point iterations, when the pass loops"`
	Changed bool   `json:",omitzero" desc:"true when this invocation rewrote its input"`
	Error   string `json:",omitzero" desc:"the error this invocation returned"`
}

// TraceStep is one step of the rewrite.
type TraceStep struct {
	Name        string      `desc:"the step: a registered pass's name, or one of play's own steps"`
	Order       int         `desc:"its position key; steps are listed in the order they ran"`
	Kind        string      `desc:"play for play's own steps, registered for a registry pass, late-bound for one built against this window's binding (handles, LW_GET, fs())"`
	Description string      `json:",omitzero" desc:"what the step does"`
	Outcome     string      `desc:"applied, skipped (it failed; the statement went on without its rewrite) or declined (a late-bound step left out: no binding, or no endpoint in the grant)"`
	Changed     bool        `desc:"true when the step rewrote the statement"`
	Micros      int64       `desc:"wall-clock microseconds"`
	Error       string      `json:",omitzero" desc:"why a skipped step failed"`
	Costs       []TraceCost `json:",omitzero" desc:"the step's internal breakdown, when costs was asked for"`
}

// TraceParam is one query parameter the rewrite lifted out of the prelude.
type TraceParam struct {
	Name  string `desc:"the parameter, as {name:Type} names it"`
	Value string `desc:"its value"`
}

// RewriteTrace is trace_rewrite's result.
type RewriteTrace struct {
	ParseError string       `json:",omitzero" desc:"the statement as written does not parse; every pass then fails on it"`
	Steps      []TraceStep  `desc:"every step, in the order it ran"`
	Expanded   bool         `desc:"true for play's whole rewrite; false when the grant does not list the endpoint, so the steps that read its catalog show as declined"`
	Needs      []string     `json:",omitzero" desc:"the destination request_access would have to add for the whole rewrite"`
	Micros     int64        `desc:"the steps' total wall-clock microseconds"`
	Summary    string       `desc:"one line: how many applied, rewrote, were skipped or declined"`
	Body       string       `desc:"the statement exactly as it would ship, FORMAT clause included"`
	Truncated  bool         `desc:"true when body was cut at 32 KiB"`
	Params     []TraceParam `json:",omitzero" desc:"query parameters sent beside the body"`
	Handles    []string     `json:",omitzero" desc:"leeway handles that name no section or column, each with candidates; they ship unresolved"`
	Target     string       `desc:"where the run would go: the endpoint's host, or introspection for this process's keelson() plane"`
	Refused    string       `json:",omitzero" desc:"why no endpoint may serve the statement, when dispatch refuses it"`
	Confined   bool         `desc:"true when the statement reads confined data"`
}

func addRewriteOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	appops.ExternalRead(s, app.OperationSpec{Name: opTraceRewrite, Version: 1,
		Summary: "trace play's client-side rewrite of a statement: every pass in order with its outcome, time and error, and the body that would ship",
		Reads:   []string{opsResSql}, Agents: true,
		Follows: []string{"nothing runs and the buffer is unchanged; validate_sql is the short verdict"}},
		func(sn opsSnap, call app.OperationCall, in TraceArgs) (RewriteTrace, error) {
			switch {
			case !sn.mounted:
				return RewriteTrace{}, app.RefuseOperation("the window has not mounted")
			case sn.client == nil:
				return RewriteTrace{}, app.RefuseOperation("the window has no endpoint")
			}
			stmt := in.Sql
			if strings.TrimSpace(stmt) == "" {
				stmt = sn.state.Sql
			}
			if err := statementBounds(stmt); err != nil {
				return RewriteTrace{}, err
			}
			return traceRewrite(sn.client, call.OnBehalfOf, stmt, in.Costs), nil
		})
}

func traceRewrite(client *Client, obo *app.OnBehalfOf, stmt string, costs bool) (out RewriteTrace) {
	if _, err := nanopass.Parse(stmt); err != nil {
		out.ParseError = err.Error()
	}
	described := map[string]passreg.CatalogRow{}
	if client.passes != nil {
		for _, r := range client.passes.Catalog() {
			if r.Stage == passreg.StagePreExecute {
				described[r.Name] = r
			}
		}
	}
	var obs []passreg.ApplyObservation
	observe := func(o passreg.ApplyObservation) { obs = append(obs, o) }
	residual, params, catalog := client.rewriteFor(obo, stmt, observe)
	if catalog {
		out.Handles = unresolvedHandles(client, stmt)
	}
	out.Expanded = catalog
	body := finishStatementObserved(residual, observe)
	var total time.Duration
	for _, o := range obs {
		total += o.Dur
		st := TraceStep{Name: o.Name, Order: o.Order, Kind: "registered", Outcome: o.Outcome.String(),
			Changed: o.Changed, Micros: o.Dur.Microseconds()}
		if doc, ok := playStepDocs[o.Name]; ok {
			st.Kind, st.Description = "play", doc
		} else if r, ok := described[o.Name]; ok {
			st.Description = r.Description
		}
		if o.LateBound {
			st.Kind = "late-bound"
		}
		if o.Err != nil {
			st.Error = o.Err.Error()
		}
		if costs && o.Cost.Name != "" {
			st.Costs = flattenCost(o.Cost, 0, nil)
		}
		out.Steps = append(out.Steps, st)
	}
	out.Micros = total.Microseconds()
	out.Summary = rewriteOutcomeSummary(obs)
	out.Body = body
	if len(out.Body) > traceMaxBody {
		out.Body, out.Truncated = out.Body[:traceMaxBody], true
	}
	for _, k := range slices.Sorted(maps.Keys(params)) {
		// The wire key carries ClickHouse's param_ marker; the model writes
		// the placeholder's own name.
		out.Params = append(out.Params, TraceParam{Name: strings.TrimPrefix(k, "param_"), Value: params[k]})
	}
	dec := client.previewDispatch(residual, "")
	switch dec.class {
	case dispatchClassIntrospection:
		out.Target = "introspection"
	case dispatchClassRefused:
		out.Target, out.Refused = "refused", dec.reason
	default:
		out.Target = endpointHost(dec.targetURL)
	}
	out.Confined = dec.sensitivity == queryengine.SensitivityConfined
	// As validate_sql: a statement dispatch sends to the introspection plane
	// does not ask for the endpoint.
	if !catalog && dec.class != dispatchClassIntrospection {
		out.Needs = []string{endpointDestination(client)}
	}
	return
}

// flattenCost lists a pass's invocation tree depth-first, bounded.
func flattenCost(c nanopass.StepCost, depth int, acc []TraceCost) (out []TraceCost) {
	out = acc
	if len(out) >= traceMaxCostNodes {
		return
	}
	n := TraceCost{Depth: depth, Name: c.Name, Micros: c.Dur.Microseconds(), Iters: c.Iters, Changed: c.Changed}
	if c.Err != nil {
		n.Error = c.Err.Error()
	}
	out = append(out, n)
	for _, ch := range c.Children {
		out = flattenCost(ch, depth+1, out)
	}
	return
}
