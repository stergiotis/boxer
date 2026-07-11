package play

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_diagnostics.go is the Diagnostics dock tab: the single owner of the
// playground's error prose. The result tabs render only a short pointer here
// (renderResultsFailed); this pane carries the full texts, in three sections:
//
//   - Statement — what the parsers make of the current editor buffer. When
//     boxer's grammar rejects it, an `EXPLAIN AST` probe against the LIVE
//     endpoint classifies the failure: ClickHouse accepting the probe means
//     the statement is fine and merely outside boxer's built-in grammar (the
//     degraded-features list is spelled out); ClickHouse rejecting it means
//     the SQL itself is broken, and the server's message — usually better
//     positioned than the grammar's — is shown.
//   - Query graph — the ADR-0097 split status of the last Run (a split
//     failure demotes the buffer to a single raw statement).
//   - Last run — the active result's execution error, or the same summary
//     line the status bar shows.
//
// The probe mirrors a real Run precisely: it wraps Client.buildResidual — the
// same SET-prelude harvest and pre-execute rewrites BuildStatement applies —
// so its verdict cannot drift from what Run would send. It runs on its own
// nodeLane (non-blocking, latest-wins, last-good), fires only for buffers the
// grammar rejected (the common, parseable case costs nothing), and inherits
// the preview debounce because it is keyed off updatePreview's outcome.

const (
	// diagProbeTimeout bounds one EXPLAIN AST round-trip. Parse-only server
	// work is fast; the ceiling covers slow links to remote endpoints.
	diagProbeTimeout = 15 * time.Second
	// diagProbePrefix precedes the probed residual. The newline (rather than a
	// space) makes ClickHouse report `(line N, col M)` positions that are off
	// by exactly one line, which adjustProbeLineNumbers corrects — so the
	// positions shown match the editor buffer.
	diagProbePrefix = "EXPLAIN AST\n"
)

// probeVerdictE classifies the EXPLAIN AST probe of a grammar-rejected buffer.
type probeVerdictE uint8

const (
	probeNone        probeVerdictE = iota // nothing probed (no client, or buffer parses)
	probePending                          // probe in flight — no verdict yet
	probeAccepted                         // ClickHouse parses the statement
	probeRejected                         // ClickHouse rejects it (detail = server text)
	probeUnavailable                      // probe failed for a non-parse reason (network, auth, …)
)

// DiagnosticsDriver owns the probe lane and its render-thread state. All
// methods are render-thread-only; the lane does the async work.
type DiagnosticsDriver struct {
	lane *nodeLane
	// buildResidual mirrors the Run path's client-side rewrite (steps 1–2 of
	// BuildStatement). Injected so tests can run the driver on a mock executor
	// without a Client.
	buildResidual func(string) (string, map[string]string)

	// probeFor is the raw buffer the current probe belongs to ("" = no probe
	// wanted); probeNode is its compiled EXPLAIN AST demand.
	probeFor  string
	probeNode compiledNode
}

// probeExecutor is the probe lane's nodeExecutorI: a verdict-only POST via
// Client.ProbeStatement — deliberately NOT clientExecutor, whose Arrow path
// (BuildStatement's FORMAT append + IPC decode) cannot represent EXPLAIN
// output. A nil error with a nil record is the "server accepted" result.
type probeExecutor struct {
	client *Client
	opts   *ExecOptions
}

var _ nodeExecutorI = probeExecutor{}

func (inst probeExecutor) execute(ctx context.Context, cn compiledNode, alloc memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, summary Summary, err error) {
	err = inst.client.ProbeStatement(ctx, cn.SQL, cn.Params, inst.opts)
	return
}

// NewDiagnosticsDriver wires the probe against the live endpoint. A nil
// client (tests, legacy CLI) leaves the lane nil and the verdict at
// probeNone — the pane then shows the grammar error without a server check.
func NewDiagnosticsDriver(client *Client) *DiagnosticsDriver {
	d := &DiagnosticsDriver{}
	if client != nil {
		d.lane = newNodeLane(probeExecutor{client: client, opts: newExecOptions("diagnostics")},
			memory.NewGoAllocator(), diagProbeTimeout)
		d.buildResidual = client.buildResidual
	}
	return d
}

// close tears down the probe lane (PlayApp.Close). Idempotent, nil-safe.
func (inst *DiagnosticsDriver) close() {
	if inst.lane != nil {
		inst.lane.close()
	}
}

// noteParse is updatePreview's hook: called once per debounced buffer with
// the grammar verdict. A parse failure arms the probe for that buffer; a
// success (or an empty buffer) disarms it. The lane memo is deliberately
// kept — flipping back to a recently probed buffer serves the memo without
// re-asking the server.
func (inst *DiagnosticsDriver) noteParse(raw string, parseErr error) {
	if parseErr == nil || raw == "" || inst.lane == nil || inst.buildResidual == nil {
		inst.probeFor = ""
		return
	}
	if inst.probeFor == raw {
		return
	}
	residual, params := inst.buildResidual(raw)
	inst.probeFor = raw
	inst.probeNode = compiledNode{SQL: diagProbePrefix + residual, Params: params}
}

// probeView demands the armed probe (non-blocking; unchanged demands memo-hit)
// and classifies the served result. Called each frame from Render so the
// verdict is warm before the user opens the tab.
func (inst *DiagnosticsDriver) probeView() (verdict probeVerdictE, detail string) {
	if inst.probeFor == "" || inst.lane == nil {
		return probeNone, ""
	}
	view := inst.lane.demand(inst.probeNode)
	if view.rec != nil {
		view.rec.Release() // the AST rows themselves are not consumed
	}
	if view.key != inst.probeNode.key() {
		// Nothing served yet, or a stale last-good from an older probe.
		return probePending, ""
	}
	if view.err == nil {
		return probeAccepted, ""
	}
	return classifyProbeError(view.err)
}

// classifyProbeError splits a probe failure into "the server parsed and said
// no" versus "the probe never got a parse verdict". It keys on the Client's
// stable error shapes: an HTTP 400 carries ClickHouse's syntax diagnostic in
// the folded body; any other failure (network, auth, non-400 status) is not
// a statement verdict.
func classifyProbeError(err error) (verdict probeVerdictE, detail string) {
	text := err.Error()
	if !strings.Contains(text, "clickhouse http 400") {
		return probeUnavailable, text
	}
	// Slice from the ClickHouse diagnostic ("Code: 62. DB::Exception: …")
	// when present — the transport prefix adds nothing for the user.
	if i := strings.Index(text, "Code:"); i >= 0 {
		text = text[i:]
	}
	return probeRejected, adjustProbeLineNumbers(text)
}

// probeLineRe matches ClickHouse's "(line N, col M)" position suffix.
var probeLineRe = regexp.MustCompile(`\(line (\d+), col (\d+)\)`)

// adjustProbeLineNumbers shifts "(line N, col M)" positions down by the one
// line the EXPLAIN AST probe prefix occupies, so they address the editor
// buffer. Texts without the pattern pass through verbatim.
func adjustProbeLineNumbers(s string) string {
	return probeLineRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := probeLineRe.FindStringSubmatch(m)
		n, err := strconv.Atoi(sub[1])
		if err != nil || n <= 1 {
			return m
		}
		return "(line " + strconv.Itoa(n-1) + ", col " + sub[2] + ")"
	})
}

// --- pane rendering -------------------------------------------------------

// renderDiagnosticsTab is the Diagnostics dock tab body. The caller wraps it
// in a ScrollArea (server error texts can be long).
func (inst *PlayApp) renderDiagnosticsTab(numRows int64, elapsed time.Duration, summary Summary, executed time.Time, err error) {
	inst.renderDiagStatement()
	c.Separator().Send()
	inst.renderDiagSplit()
	c.Separator().Send()
	inst.renderDiagLastRun(numRows, elapsed, summary, executed, err)
}

func diagHeading(text string) {
	for rt := range c.RichTextLabel(text) {
		rt.Strong()
	}
}

func diagWeak(text string) {
	for rt := range c.RichTextLabel(text) {
		rt.Small().Weak()
	}
}

// renderDiagStatement is the parse-status section: the grammar verdict for
// the debounced buffer, refined by the EXPLAIN probe when the grammar said no.
func (inst *PlayApp) renderDiagStatement() {
	diagHeading("Statement")
	raw := strings.TrimSpace(inst.sql)
	switch {
	case raw == "":
		diagWeak("Type SQL in the Editor tab.")
	case inst.sql != inst.formattedFor:
		diagWeak("Waiting for the editor to settle…")
	case inst.formattedErr == nil:
		diagWeak("Parses in boxer's grammar — canonical preview, parameter widgets and the query graph are available.")
	default:
		verdict, detail := inst.diag.probeView()
		switch verdict {
		case probeAccepted:
			c.Label("ClickHouse parses this statement — it is outside boxer's built-in SQL grammar.").Wrap().Send()
			diagWeak("Run sends the buffer verbatim. Unavailable for this statement: the canonical preview, " +
				"parameter widgets, the query-graph split, and pre-execute rewrites (e.g. lw_id() macros).")
		case probeRejected:
			c.Label("ClickHouse rejects this statement:").Send()
			c.Label(detail).Wrap().Selectable(true).Send()
		case probePending:
			diagWeak("Boxer's grammar does not parse this statement — checking with ClickHouse (EXPLAIN AST)…")
		case probeUnavailable:
			c.Label("Boxer's grammar does not parse this statement, and the ClickHouse check did not reach a verdict:").Wrap().Send()
			c.Label(detail).Wrap().Selectable(true).Send()
		default: // probeNone — no endpoint to ask
			diagWeak("Boxer's grammar does not parse this statement (no endpoint available to verify it against).")
		}
		diagWeak("boxer parser: " + inst.formattedErr.Error())
	}
}

// renderDiagSplit is the ADR-0097 split status of the last Run.
func (inst *PlayApp) renderDiagSplit() {
	diagHeading("Query graph")
	switch {
	case inst.lastSentSql == "":
		diagWeak("Run a query to see how it splits.")
	case inst.splitErr != nil:
		c.Label("The buffer did not split into a query graph — it executed as a single statement:").Wrap().Send()
		c.Label(inst.splitErr.Error()).Wrap().Selectable(true).Send()
	default:
		diagWeak("Split into " + strconv.Itoa(len(inst.currentSplit.Nodes)) + " node(s); the panels observe \"" +
			string(inst.activeNodeID()) + "\".")
	}
}

// renderDiagLastRun is the active result's outcome: the full execution error
// (whose only other surface is the status bar's truncated first line), or the
// same summary line the status bar shows.
func (inst *PlayApp) renderDiagLastRun(numRows int64, elapsed time.Duration, summary Summary, executed time.Time, err error) {
	diagHeading("Last run")
	if err != nil {
		c.Label("The query failed:").Send()
		// Selectable so the ClickHouse diagnostic (folded into err.Error() by
		// ExecuteArrowStream) can be copied out to search or a bug report.
		c.Label(err.Error()).Wrap().Selectable(true).Send()
		return
	}
	if s := inst.querySummaryLine(numRows, elapsed, summary, executed, err); s != "" {
		diagWeak(s)
	}
}
