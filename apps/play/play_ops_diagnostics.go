package play

// The Diagnostics pane as an agent reads it (ADR-0270, update of
// 2026-10-04): the seven sections the pane draws — the statement's parse
// status and ClickHouse's verdict, the skipped rewrites, unresolved leeway
// handles, the security context, the query graph, dropped signal emits and
// the last run — as fields, read from what the pane reads, so a model
// learns why a statement failed without guessing from a one-line error.

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

const opGetDiagnostics = "get_diagnostics"

// StatementDiag is the Statement section.
type StatementDiag struct {
	Status string `desc:"empty, settling (the editor has not settled), parses (boxer's grammar parses it), outside-grammar (boxer cannot parse it, ClickHouse can: it runs verbatim, without the canonical preview, parameter widgets, the query graph or the rewrites), rejected (ClickHouse rejects it too), checking (ClickHouse's EXPLAIN AST is in flight), unverified (the check did not reach a verdict) or no-endpoint"`
	// ClickHouse is the server's own text when it rejected the statement.
	ClickHouse string `json:",omitzero" desc:"ClickHouse's diagnostic, its line positions matching the buffer, or why the check reached no verdict"`
	Parser     string `json:",omitzero" desc:"boxer's parser error, when its grammar rejects the statement"`
}

// RewriteFailure is one skipped or declined rewrite.
type RewriteFailure struct {
	Pass    string `desc:"the rewrite step"`
	Outcome string `desc:"skipped (it failed; the statement ships without it) or declined (it does not support this endpoint's client)"`
	Error   string `json:",omitzero" desc:"why"`
}

// RewritesDiag is the Pre-execute rewrites section.
type RewritesDiag struct {
	Measured bool             `desc:"true when the rewrite of this buffer was measured; false until the Passes or Diagnostics pane has measured it — validate_sql reports a statement's failed rewrites without it"`
	Summary  string           `json:",omitzero" desc:"how many steps applied, changed, were skipped or declined"`
	Failed   []RewriteFailure `json:",omitzero" desc:"the steps that did not run; the statement still runs without them"`
}

// HandleDiag is one leeway handle that does not resolve.
type HandleDiag struct {
	Handle     string   `desc:"the handle as written"`
	Message    string   `desc:"why it does not resolve"`
	Candidates []string `json:",omitzero" desc:"handles it may have meant"`
}

// SecurityDiag is the Security context section.
type SecurityDiag struct {
	Class string `desc:"read (retrieval against the endpoint's own data), read-egress (it reaches beyond the endpoint), mutating, or unclassified (boxer cannot parse it, so it counts as mutating); an agent's run needs read"`
	// Witnesses are the constructs that raised the class.
	Witnesses []string `json:",omitzero" desc:"the constructs that raised the class above read"`
	// Passthrough are the tables returned 1:1.
	Passthrough []string `json:",omitzero" desc:"the tables whose stored rows the statement returns 1:1"`
}

// DiagnosticsState is get_diagnostics' result.
type DiagnosticsState struct {
	Statement StatementDiag `desc:"the buffer's parse status"`
	Rewrites  RewritesDiag  `desc:"play's client-side rewrite of the buffer"`
	// HandlesChecked is false when the window has no column resolver.
	HandlesChecked bool         `desc:"true when leeway handles are checked in this window"`
	Handles        []HandleDiag `json:",omitzero" desc:"leeway handles in the buffer that name no section or column"`
	Security       SecurityDiag `desc:"the buffer's security class and the tables it returns as stored"`
	QueryGraph     string       `desc:"how the last run split into a query graph, or why it ran as one statement"`
	// DroppedEmits are panel signal writes the store refused.
	DroppedEmits []string `json:",omitzero" desc:"signal writes from panels the store dropped; the signal keeps its previous value"`
	LastRun      string   `desc:"the last run's outcome: the full error when it failed, else its summary"`
	LastRunError bool     `desc:"true when the last run failed"`
}

// snapshotDiagnostics copies the pane's sections on the render goroutine;
// it reads what the pane reads and starts no measurement or probe the
// frame would not.
func snapshotDiagnostics(p *PlayApp) (out DiagnosticsState) {
	raw := strings.TrimSpace(p.sql)
	settled := p.sql == p.formattedFor
	st := &out.Statement
	switch {
	case raw == "":
		st.Status = "empty"
	case !settled:
		st.Status = "settling"
	case p.formattedErr == nil:
		st.Status = "parses"
	default:
		st.Parser = p.formattedErr.Error()
		st.Status = "no-endpoint"
		if p.diag != nil {
			verdict, detail := p.diag.probeView()
			switch verdict {
			case probeAccepted:
				st.Status = "outside-grammar"
			case probeRejected:
				st.Status, st.ClickHouse = "rejected", detail
			case probePending:
				st.Status = "checking"
			case probeUnavailable:
				st.Status, st.ClickHouse = "unverified", detail
			}
		}
	}

	if obs, ok := p.rewriteTraceMeasured(); ok {
		out.Rewrites.Measured, out.Rewrites.Summary = true, rewriteOutcomeSummary(obs)
		for _, o := range skippedRewrites(obs) {
			f := RewriteFailure{Pass: o.Name, Outcome: rewriteOutcomeText(o)}
			if o.Err != nil {
				f.Error = o.Err.Error()
			}
			out.Rewrites.Failed = append(out.Rewrites.Failed, f)
		}
	}

	if p.diag != nil {
		out.HandlesChecked = p.diag.resolveDiag != nil
		for _, d := range p.diag.columnDiagnostics() {
			out.Handles = append(out.Handles, HandleDiag{Handle: d.Handle, Message: d.Message, Candidates: d.Candidates})
		}
		class, witnesses, known := p.diag.securityClass()
		out.Security.Class = class.String()
		if !known {
			out.Security.Class = "unclassified"
		}
		if raw == "" || !settled {
			out.Security.Class = "unclassified"
		}
		for _, w := range witnesses {
			out.Security.Witnesses = append(out.Security.Witnesses, w.Name+" — "+w.Describe())
		}
		for _, t := range p.diag.securityContext() {
			out.Security.Passthrough = append(out.Security.Passthrough, passthroughTableName(t))
		}
	}

	switch {
	case p.lastSentSql == "":
		out.QueryGraph = "no run yet"
	case p.splitErr != nil:
		out.QueryGraph = "did not split into a query graph and ran as a single statement: " + p.splitErr.Error()
	default:
		out.QueryGraph = "split into " + strconv.Itoa(len(p.currentSplit.Nodes)) + " node(s); the panes observe " + strconv.Quote(string(p.activeNodeID()))
	}

	if p.graph != nil {
		for _, d := range p.graph.emitDrops() {
			out.DroppedEmits = append(out.DroppedEmits, d.Name+": "+d.Writer+" emitted a "+d.ValueType+", which has no raw form")
		}
		rec, _, numRows, _, elapsed, summary, executed, err, _ := p.graph.MainSnapshot()
		if rec != nil {
			rec.Release()
		}
		if err != nil {
			out.LastRun, out.LastRunError = err.Error(), true
		} else {
			out.LastRun = p.querySummaryLine(numRows, elapsed, summary, executed, err, p.activeTruncation())
		}
	}
	return
}

func addDiagnosticsOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Query(s, app.OperationSpec{Name: opGetDiagnostics, Version: 1,
		Summary: "read the Diagnostics pane: whether the buffer parses and ClickHouse's verdict when it does not, skipped rewrites, unresolved leeway handles, the security class, the query graph split, dropped signal emits and the last run's full error",
		Reads:   []string{opsResSql, opsResResult, opsResSignals}, Agents: true, Untrusted: true,
		Follows: []string{"validate_sql checks a statement you have not set; get_diagnostics reads the buffer as it stands and the last run"}},
		func(sn opsSnap, in appops.None) (DiagnosticsState, error) {
			if !sn.mounted {
				return DiagnosticsState{}, app.RefuseOperation("the window has not mounted")
			}
			return sn.diagnostics, nil
		})
}
