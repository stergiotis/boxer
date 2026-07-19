package play

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
)

// play_diagnostics.go is the Diagnostics dock tab: the single owner of the
// playground's error prose. The result tabs render only a short pointer here
// (renderResultsFailed); this pane carries the full texts, in these sections:
//
//   - Statement — what the parsers make of the current editor buffer. When
//     boxer's grammar rejects it, an `EXPLAIN AST` probe against the LIVE
//     endpoint classifies the failure: ClickHouse accepting the probe means
//     the statement is fine and merely outside boxer's built-in grammar (the
//     degraded-features list is spelled out); ClickHouse rejecting it means
//     the SQL itself is broken, and the server's message — usually better
//     positioned than the grammar's — is shown.
//   - Column resolution — the leeway column handles the client-side resolver
//     could not map, when a resolver is wired (SetColumnResolver).
//   - Security context — the passthrough ("1:1 as stored") base tables the
//     buffer returns verbatim (ADR-0117), an "information-retrieval" badge
//     flagging any non-empty set. Purely structural, so it speaks only for
//     buffers boxer's grammar parses.
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

	// resolveDiag runs the leeway column resolver over a buffer with a
	// collecting sink and returns the handles it could not resolve. Injected via
	// PlayApp.SetColumnResolver; nil when no resolver is wired. It is computed
	// off the render thread (a first schema probe may hit the network) and
	// polled from Render, mirroring the probe lane — latest-wins via colDiagGen.
	resolveDiag func(sql string) []passes.ColumnDiagnostic
	colDiagMu   sync.Mutex
	colDiagGen  uint64
	colDiagFor  string
	colDiags    []passes.ColumnDiagnostic

	// passthruFor is the buffer whose passthrough ("1:1 as stored") base tables
	// passthruTables holds — the security-context lens (ADR-0117). The
	// classification is purely structural (parse + BuildScopes, no round-trip),
	// so unlike the column diagnostics it is computed synchronously in noteParse
	// and read straight back — no lane, no goroutine. Render-thread-only,
	// memoised by buffer like probeFor.
	passthruFor    string
	passthruTables []analysis.TableRef

	// secClass / secWitnesses carry the ADR-0132 §SD5 security class of the
	// same buffer passthruFor names, computed from the same parse. secKnown is
	// false when the buffer did not parse — "cannot classify" — and secClass
	// then already holds the strongest class (QuerySecurityMutating is the
	// enum's fail-closed zero value), per the ClassifyQuerySecurity caller
	// contract.
	secKnown     bool
	secClass     analysis.QuerySecurityClassE
	secWitnesses []analysis.SecurityWitness
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
	// Column-resolution warnings and the passthrough-table lens are independent
	// of the EXPLAIN probe: they want the parseable case (the probe wants the
	// rejected one).
	inst.armColumnDiag(raw, parseErr)
	inst.armSecurityContext(raw, parseErr)
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

// armColumnDiag recomputes the client-side column-resolution warnings for a
// newly parsed buffer, off the render thread. A parseable buffer is resolved on
// a goroutine (the resolver's first schema probe per table may hit the network,
// cached thereafter); an unparseable or empty one simply clears the warnings.
// Latest-wins via colDiagGen; Render polls columnDiagnostics.
func (inst *DiagnosticsDriver) armColumnDiag(raw string, parseErr error) {
	if inst.resolveDiag == nil {
		return
	}
	inst.colDiagMu.Lock()
	if raw == inst.colDiagFor {
		inst.colDiagMu.Unlock()
		return
	}
	inst.colDiagGen++
	gen := inst.colDiagGen
	inst.colDiagFor = raw
	inst.colDiags = nil
	inst.colDiagMu.Unlock()
	if parseErr != nil || raw == "" {
		return
	}
	go func() {
		diags := inst.resolveDiag(raw)
		inst.colDiagMu.Lock()
		if gen == inst.colDiagGen {
			inst.colDiags = diags
		}
		inst.colDiagMu.Unlock()
	}()
}

// columnDiagnostics returns the latest computed column-resolution warnings.
// Render-thread safe.
func (inst *DiagnosticsDriver) columnDiagnostics() []passes.ColumnDiagnostic {
	inst.colDiagMu.Lock()
	defer inst.colDiagMu.Unlock()
	return inst.colDiags
}

// armSecurityContext reclassifies the newly parsed buffer's security lenses:
// the passthrough base tables it returns "1:1 as stored"
// (analysis.ExtractPassthroughTables, ADR-0117) and the ADR-0132 §SD5 security
// class (analysis.ClassifyQuerySecurity), both from one parse. The analyses are
// purely structural (no round-trip), so they run synchronously on the render
// thread and their results are read straight back — no lane. A buffer boxer's
// grammar cannot parse follows each analysis's conservative direction: no
// passthrough tables (never a false information-retrieval claim) and an
// unknown class treated as the strongest one (mutating). Memoised by buffer
// like the column diagnostics.
func (inst *DiagnosticsDriver) armSecurityContext(raw string, parseErr error) {
	if raw == inst.passthruFor {
		return
	}
	inst.passthruFor = raw
	inst.passthruTables = nil
	inst.secKnown = false
	inst.secClass = analysis.QuerySecurityMutating
	inst.secWitnesses = nil
	if parseErr != nil || raw == "" {
		return
	}
	pr, err := nanopass.Parse(raw)
	if err != nil {
		return
	}
	// The ADR-0132 §SD5 class rides the same parse. A classifier error (a
	// malformed tree) leaves secKnown=false — rendered, and to be treated,
	// as the strongest class.
	if class, wits, cerr := analysis.ClassifyQuerySecurity(pr); cerr == nil {
		inst.secKnown = true
		inst.secClass = class
		inst.secWitnesses = wits
	}
	// defaultDatabase is "": play has no configured connection default (the
	// server resolves unqualified reads via currentDatabase()), so unqualified
	// tables stay unqualified in the reported refs.
	refs, err := analysis.ExtractPassthroughTables(pr, "")
	if err != nil {
		return
	}
	inst.passthruTables = refs
}

// securityContext returns the latest passthrough-table classification of the
// current buffer. Render-thread-only (the slice is computed inline in
// noteParse, never off-thread).
func (inst *DiagnosticsDriver) securityContext() []analysis.TableRef {
	return inst.passthruTables
}

// securityClass returns the ADR-0132 §SD5 class of the current buffer plus the
// witnesses that forced a class below "read". known=false means the buffer did
// not parse — cannot classify — and class then already holds the strongest
// value (mutating), which is also how a consumer must treat it.
// Render-thread-only, like securityContext.
func (inst *DiagnosticsDriver) securityClass() (class analysis.QuerySecurityClassE, witnesses []analysis.SecurityWitness, known bool) {
	return inst.secClass, inst.secWitnesses, inst.secKnown
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
	if inst.diag != nil && inst.diag.resolveDiag != nil {
		inst.renderDiagColumnResolution()
		c.Separator().Send()
	}
	inst.renderDiagSecurityContext()
	c.Separator().Send()
	inst.renderDiagSplit()
	c.Separator().Send()
	inst.renderDiagLastRun(numRows, elapsed, summary, executed, err)
}

// renderDiagColumnResolution lists the leeway column handles the resolver could
// not resolve in the current buffer — computed client-side, so a typo like
// `geoPoint:lat` is caught before any round-trip to the server. Empty when
// every handle resolves.
func (inst *PlayApp) renderDiagColumnResolution() {
	diagHeading("Column resolution")
	if inst.diag == nil {
		return
	}
	diags := inst.diag.columnDiagnostics()
	if len(diags) == 0 {
		diagWeak("Every leeway column handle resolves.")
		return
	}
	for _, d := range diags {
		line := d.Handle + " — " + d.Message
		if len(d.Candidates) > 0 {
			line += " — did you mean: " + strings.Join(d.Candidates, ", ") + "?"
		}
		for rt := range c.RichTextLabel(line) {
			rt.Monospace()
		}
	}
}

// SetColumnResolver wires the leeway column resolver into the Diagnostics pane,
// so it can warn — client-side, before a Run — about `section:column` handles
// that name no known section or column. Safe to call with nil (no resolver).
func (inst *PlayApp) SetColumnResolver(resolver passes.ColumnResolverI) {
	if inst.diag == nil || resolver == nil {
		return
	}
	inst.diag.resolveDiag = func(sql string) (diags []passes.ColumnDiagnostic) {
		_, _ = passes.ResolveColumnNames(resolver, "", func(d passes.ColumnDiagnostic) {
			diags = append(diags, d)
		}).Run(sql)
		return diags
	}
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

// renderDiagSecurityContext is the security lens over the current buffer: the
// ADR-0132 §SD5 class line (read / read-egress / mutating, with the witnesses
// that forced a non-read class), then the passthrough-table lens (ADR-0117) —
// the base tables whose stored rows the buffer returns 1:1, set apart from the
// aggregates, derivations, and joins a policy cannot govern by table alone. A
// non-empty set earns an "information-retrieval" badge — the statement hands
// back stored rows verbatim, so a table/row/column policy bounds exactly what
// it can expose. Both analyses are structural; a buffer the grammar rejects
// gets the conservative presentation of each (strongest class, no passthrough
// claim).
func (inst *PlayApp) renderDiagSecurityContext() {
	diagHeading("Security context")
	if inst.diag == nil {
		return
	}
	// Keyed off the same debounced buffer as the classification (armSecurityContext
	// runs in noteParse on the trimmed raw): until the editor settles onto a
	// parseable buffer, there is nothing meaningful to show.
	raw := strings.TrimSpace(inst.sql)
	switch {
	case raw == "":
		diagWeak("Type SQL in the Editor tab.")
		return
	case inst.sql != inst.formattedFor:
		diagWeak("Waiting for the editor to settle…")
		return
	}
	inst.renderDiagSecurityClass()
	if inst.formattedErr != nil {
		diagWeak("Passthrough tables are classified only for statements boxer's grammar parses.")
		return
	}
	tables := inst.diag.securityContext()
	if len(tables) == 0 {
		diagWeak("No passthrough tables — this statement aggregates, derives, joins, or otherwise transforms its inputs rather than returning stored rows 1:1.")
		return
	}
	for range c.Horizontal().KeepIter() {
		badge.New(inst.ids.PrepareStr("sec-info-retrieval"), "information-retrieval").
			Tone(badge.ToneInfo).Size(badge.SizeSm).
			Tooltip("The query returns these tables' stored rows 1:1 — a pure information-retrieval read that a row/column policy governs directly.").
			Send()
	}
	for _, t := range tables {
		for rt := range c.RichTextLabel(passthroughTableName(t)) {
			rt.Monospace()
		}
	}
}

// renderDiagSecurityClass is the ADR-0132 §SD5 class line: a badge for the
// buffer's security class plus one monospace line per witness. A buffer the
// grammar rejects cannot be classified and is presented — and must be treated
// downstream — as the strongest class (mutating), the ADR's conservative
// direction.
func (inst *PlayApp) renderDiagSecurityClass() {
	class, witnesses, known := inst.diag.securityClass()
	label := class.String()
	tone := badge.ToneError
	tooltip := "The buffer changes state — the witnesses below name the construct (ADR-0132 §SD5)."
	switch {
	case !known:
		label = "mutating (unclassified)"
		tooltip = "Boxer's grammar does not parse this buffer, so it cannot be classified; the conservative direction treats it as the strongest class (ADR-0132 §SD5)."
	case class == analysis.QuerySecurityRead:
		tone = badge.ToneSuccess
		tooltip = "Provably retrieval-only against the endpoint's own data — the class a readonly setting can enforce on the wire (ADR-0132 §SD5)."
	case class == analysis.QuerySecurityReadEgress:
		tone = badge.ToneWarning
		tooltip = "Retrieval-only, but it reaches beyond the endpoint — the witnesses below name the egress constructs (ADR-0132 §SD5)."
	}
	for range c.Horizontal().KeepIter() {
		badge.New(inst.ids.PrepareStr("sec-class"), label).
			Tone(tone).Size(badge.SizeSm).
			Tooltip(tooltip).
			Send()
	}
	for _, w := range witnesses {
		for rt := range c.RichTextLabel(w.Name + " — " + w.Kind.String()) {
			rt.Monospace()
		}
	}
}

// passthroughTableName renders a classified table for the list: `db.table` when
// the reference carried a database, the bare table name otherwise (play has no
// configured default database, so unqualified reads stay unqualified — see
// armSecurityContext).
func passthroughTableName(t analysis.TableRef) string {
	if t.Database != "" {
		return t.Database + "." + t.Table
	}
	return t.Table
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
