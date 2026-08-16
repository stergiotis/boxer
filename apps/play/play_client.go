package play

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/chhttp"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine/chserver"
	"github.com/stergiotis/boxer/public/keelson/runtime/runid"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type ClientConfig struct {
	URL      string
	User     string
	Password string
}

type Client struct {
	cfg  ClientConfig
	http *http.Client

	// passes supplies the registered pre-execute rewrites (ADR-0108 §SD6),
	// e.g. LW_ID_* macro expansion. Defaults to passreg.Default — the host
	// fills that at wiring time via passreg/defaults; tests inject their own
	// registry here.
	passes *passreg.Registry

	// passBinding is the per-consumer value the pre-execute stage's late-bound
	// factories are realised against (ADR-0108 §SD7) — here, the leeway schema
	// resolver installLeewayNameResolution builds, which closes over this
	// client's live endpoint (ADR-0116 §SD6). It stays nil until installed;
	// ApplyBestEffortBound then declines every factory, so the client applies
	// only the concrete entries (e.g. identsql), exactly as before.
	passBinding any

	// conditionsPass is the opt-in selection-condition rewrite (ADR-0121),
	// realised by installLeewayNameResolution against this client's schema
	// probe. It is NOT in the pass registry — it changes a query's result
	// schema, so it is a per-host opt-in rather than part of the standard
	// pre-execute set. A zero Pass (never installed) means the toggle does
	// nothing.
	conditionsPass nanopass.Pass
	// exposeConditions is the toggle itself, default off. Written from the render
	// thread (the top-bar checkbox) and read wherever a query is built, hence
	// atomic.
	exposeConditions atomic.Bool

	// mu guards targetURL, the live endpoint. It starts at cfg.URL and can be
	// switched at runtime via SetURL — e.g. play's endpoint switcher points at
	// the in-process keelson introspection /query endpoint (ADR-0094 §SD6).
	// cfg.User/cfg.Password are not switchable in v1.
	//
	// targetURL is the *manual base* now, not the target: what a request is
	// actually sent to comes from a dispatchDecision (play_dispatch.go), and
	// the resolver is what turns the base into one. Requests never read this
	// field; only the resolver and the UI do.
	// exprValues is the live-tier expression binding (ADR-0187
	// §SD3), guarded by mu like the other mutable bindings. Replaced whole by
	// SetExprValues; never mutated in place, so a reader holding it after the
	// unlock sees a consistent set.
	exprValues map[string]string

	mu        sync.RWMutex
	targetURL string
	// resolver is the E2 seam. nil means staticResolver — every run goes to
	// the manual base, which is what play did before the seam existed.
	resolver endpointResolverI

	// lastDecision is the most recent resolution, for the toolbar to report.
	// Written on lane goroutines, read on the render thread, so it lives
	// outside mu rather than widening that lock's reach for a display value.
	lastDecision atomic.Pointer[dispatchDecision]
	// stampRunId / stampAppId are the SD7 identity halves of the
	// log_comment stamp (play_stamp.go), set once via SetStampIdentity
	// at Mount; empty outside the runtime (standalone CLI, tests).
	stampRunId string
	stampAppId string

	// reach remembers which endpoints have demonstrated they can fetch from
	// this process's loopback plane (ADR-0145 §SD5). Consulted by the
	// confinement wall; never written on a dispatch path.
	reach *reachProver

	// datasetBindings maps a stable ad-hoc dataset alias to the ephemeral
	// handle an embedder bound it to (ADR-0134 §SD4). Guarded by mu; read
	// by buildResidual on lane goroutines, written by bindDataset on the
	// render thread. keelson('<alias>') rewrites to keelson('<handle>')
	// before the request leaves play; unbound names pass through.
	datasetBindings map[string]string
}

func NewClient(cfg ClientConfig, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{cfg: cfg, http: httpClient, passes: passreg.Default, targetURL: cfg.URL, reach: newReachProver()}
}

// PassRegistry exposes the registry Execute applies at StagePreExecute — the
// Passes tab draws its catalog (ADR-0119 M3).
func (inst *Client) PassRegistry() *passreg.Registry { return inst.passes }

// ExecOptions carries per-lane execution settings for ExecuteArrowStream.
// QueryID is a stable per-lane ClickHouse query_id: combined with
// ReplaceRunningQuery, a superseding run REPLACES its still-running
// predecessor server-side (ADR-0097 SD5 / ADR-0096 SD9). Context cancel alone
// only closes the HTTP connection, which ClickHouse by default does NOT treat
// as a kill for read-only queries — without this, superseded raster/bands
// queries pile up on the server. Endpoints that don't know these params
// ignore them (the keelson introspection /query reads only cols/query/param_*).
type ExecOptions struct {
	QueryID             string
	ReplaceRunningQuery bool
	// Label is the human lane name ("main", "map", "diagnostics", …) the
	// QueryID embeds — carried separately so the SD7 log_comment stamp
	// can record it without parsing it back out of the id.
	Label string
	// OnProgress, when set, opts the request into ClickHouse's in-band
	// progress headers (ADR-0115 plane A): the server streams
	// X-ClickHouse-Progress lines inside the open response-header block,
	// and the engine's streaming transport fires this callback for each one
	// while the query still runs. Called from the transport goroutine —
	// keep it cheap and thread-safe. Live delivery needs a plain-http
	// endpoint; elsewhere the run degrades to the final summary with no
	// mid-run ticks. nil = off (no wire change).
	//
	// The ticks are a callback rather than progress frames because they
	// arrive inside the response-header block — before the result stream
	// exists. Frames are how a party that is NOT the connection holder sees
	// progress, and this lane is the connection holder.
	OnProgress func(p runstream.Progress)

	// WrapStatement, when set, wraps the WIRE body only — applied to the
	// residual after the client-side rewrites and before the FORMAT step
	// (a FORMAT inside a wrapper's parens would bind to the inner
	// statement). Everything else — the routing decision, the URL params,
	// the pre-execute rewrites, the row cap — is derived from the plain
	// statement, so a wrapped run routes and rewrites exactly as the
	// statement it wraps would: the ADR-0141 probe precedent ("resolve from
	// the statement it wraps") generalized to lanes. The Flow tab's EXPLAIN
	// lenses are the consumer: index structure and schema are
	// endpoint-local, so the EXPLAIN must interrogate the endpoint the
	// query would actually run on. The wrapper is machine-built and must
	// not carry its own FORMAT clause.
	WrapStatement func(residual string) string
}

// newExecOptions mints a lane's stable ExecOptions.
//
// # The run-identity contract (E4)
//
// The minted QueryID is the join key for everything said about a run, and
// the parties that need to agree on it never meet: the server's
// system.processes row while it runs, its query_log row once it finishes,
// the queryrunfacts pin, a KILL QUERY addressed at it, and the progress
// frames a poller publishes for it. Because it is minted by the client
// rather than assigned by the server, all of them can name a run before it
// exists, and a second observer can watch a run it did not issue (R7/R8 of
// doc/explanation/query-system-requirements.md).
//
// The id's shape and its uniqueness scope belong to runid, which owns the
// contract; `<app>-<label>-<host>-<pid>-<seq>` distinguishes lanes,
// processes and boxes. What matters here is the consequence: it is stable,
// not novel. A lane reuses its id across runs, so a superseding run
// REPLACES its still-running predecessor server-side (ReplaceRunningQuery),
// and one lane therefore has at most one live run.
//
// The label names the lane in server-side observability, and rides the
// log_comment stamp separately so a consumer need not parse it back out of
// the id — nothing does, which is what made widening the id safe.
//
// Both endpoints receive the id, but they can do different things with it.
// A real server registers it in system.processes and query_log, where it is
// observable and killable. The in-process introspection endpoint echoes it
// on the response and logs it, and can offer no more: its workers are
// one-shot and their system tables die with them (R10).
func newExecOptions(label string) *ExecOptions {
	return &ExecOptions{
		QueryID:             runid.Mint("play", label),
		ReplaceRunningQuery: true,
		Label:               label,
	}
}

// BuildStatement performs the client-side rewrite of a raw editor buffer
// into the statement body and URL params that ExecuteArrowStream ships:
//
//  1. Harvest top-level `SET param_*=...` statements (ExtractParams) so
//     they can ride the HTTP `param_*` channel rather than being inlined —
//     values can be larger than fits comfortably in a single SQL literal,
//     and the typed substitution from `{name:Type}` placeholders is what
//     ClickHouse expects this way.
//  2. Apply the registered pre-execute rewrites (ADR-0108 §SD6) — e.g.
//     LW_ID_* macro expansion — best-effort: a pass that fails is skipped
//     and the SQL from before it ships instead.
//  3. Rewrite the query so it ends with `FORMAT ArrowStream`, replacing
//     any existing FORMAT clause; falls back to a textual append when the
//     SQL is outside Grammar1.
//
// Every step degrades rather than fails, so a usable body always comes
// back and the server reports the real problem to the user. The Preview
// tab's "as sent" view calls this too, so what it shows can never drift
// from what executes.
func (inst *Client) BuildStatement(sql string) (body string, params map[string]string) {
	return inst.buildStatementObserved(sql, nil)
}

// buildStatementObserved is BuildStatement with an observer over its degrade
// points. Every step of the client-side rewrite reports what it did — applied,
// applied-and-changed, or skipped with the error that caused it — so a UI can
// account for the difference between the buffer the user wrote and the body
// that shipped. BuildStatement is this with a nil observer, which is what makes
// the trace load-bearing: it describes the same code path that executes, never
// a re-derivation of it.
func (inst *Client) buildStatementObserved(sql string, observe func(passreg.ApplyObservation)) (body string, params map[string]string) {
	residual, params := inst.buildResidualObserved(sql, observe)
	// ADR-0181 §SD8 M3: an INSERT wrapper takes no FORMAT clause — the
	// appended FORMAT is exactly why DDL from play fails, and a write
	// answers with a summary, not a stream. The step still reports itself
	// (applied, unchanged), so the Preview trace accounts for the wire body
	// carrying no FORMAT rather than looking like a skipped rewrite.
	if pr, perr := nanopass.Parse(residual); perr == nil && pr.InsertStmt() != nil {
		body = residual
		observeStep(observe, rewriteStepSetFormat, orderSetFormat, nil, residual, body)
		return
	}
	body, setErr := passes.SetFormat("ArrowStream").Run(residual)
	if setErr != nil {
		log.Debug().Err(setErr).Msg("play: SetFormat failed, falling back to textual append")
		body = strings.TrimRight(residual, "; \t\n\r")
		if !strings.Contains(strings.ToUpper(body), "FORMAT ") {
			body += " FORMAT ArrowStream"
		}
		observeStep(observe, rewriteStepSetFormat, orderSetFormat, setErr, "", "")
		return
	}
	observeStep(observe, rewriteStepSetFormat, orderSetFormat, nil, residual, body)
	return
}

// The client-side rewrite's own steps sit outside the pass registry but degrade
// the same way, so the trace carries them as observations too. Their orders
// place them around the registry stage (whose registered orders start at 100)
// in execution order; nothing sorts on them, they exist so a reader can tell a
// play step from a registered pass at a glance.
const (
	orderExtractParams    = -100
	orderSpliceExpr       = -90
	orderExposeConditions = 1_000_000
	orderSetFormat        = 1_000_100

	rewriteStepExtractParams    = "extract-params"
	rewriteStepSpliceExpr       = "splice-expr"
	rewriteStepExposeConditions = "expose-conditions"
	rewriteStepSetFormat        = "set-format"
)

// observeStep reports one non-registry step of the client-side rewrite. A
// non-nil err is a skip (the step's output was discarded); otherwise the step
// applied, and before/after decide whether it changed anything. A nil observe
// makes this a no-op, so the un-observed path pays only a nil check.
func observeStep(observe func(passreg.ApplyObservation), name string, order int, err error, before, after string) {
	if observe == nil {
		return
	}
	obs := passreg.ApplyObservation{Name: name, Order: order, Outcome: passreg.ApplyOutcomeApplied, Changed: before != after}
	if err != nil {
		obs.Outcome, obs.Changed, obs.Err = passreg.ApplyOutcomeSkipped, false, err
	}
	observe(obs)
}

// RewriteTrace runs the client-side rewrite of sql for its per-step outcomes
// and discards the statement. Callers get what the registry stage and play's
// own steps did to this buffer — including passes that failed and were skipped,
// which otherwise appear only as a warn line in the process log (ADR-0108 §SD3
// makes every unit degrade rather than fail, so a skipped rewrite is invisible
// in the result).
//
// It costs a full rewrite — the passes re-parse — so callers memoise it by
// buffer rather than recomputing per frame. Safe from any goroutine: the state
// the rewrite reads is either immutable after wiring or guarded (Client.mu, the
// exposeConditions atomic).
func (inst *Client) RewriteTrace(sql string) (obs []passreg.ApplyObservation) {
	_, _ = inst.buildStatementObserved(sql, func(o passreg.ApplyObservation) { obs = append(obs, o) })
	return
}

// ProbeStatement POSTs sql verbatim (params riding the URL exactly as in
// ExecuteArrowStream) and reports only whether the server accepted it — no
// FORMAT rewrite, no Arrow decode. The diagnostics EXPLAIN probe consumes the
// verdict, not the rows: a FORMAT appended to `EXPLAIN AST <stmt>` would bind
// to the inner statement and leave EXPLAIN's own output undecodable, so the
// probe must stay off the Arrow pipeline. Non-200 responses fold the server's
// diagnostic into the error exactly like ExecuteArrowStream ("clickhouse http
// <code>: <body>"), which classifyProbeError keys on.
//
// dec is required (play_dispatch.go). A probe that resolved its own endpoint
// from its own `EXPLAIN AST …` text would answer about a server the run it
// describes never talks to, so the caller passes the decision the run uses.
func (inst *Client) ProbeStatement(ctx context.Context, sql string, params map[string]string, opts *ExecOptions, dec dispatchDecision) (err error) {
	eng, err := inst.engineFor(dec)
	if err != nil {
		return
	}
	settings := map[string]string{}
	// Attribution-only SD7 stamp — a probe is not an executed definition,
	// so it carries identity without fingerprints (play_stamp.go).
	if lc := inst.composeProbeLogComment(opts); lc != "" {
		settings["log_comment"] = lc
	}
	req := queryengine.Request{
		SQL:         sql,
		Params:      bareParams(nil, params),
		Settings:    settings,
		Sensitivity: dec.sensitivity,
	}
	if opts != nil && opts.QueryID != "" {
		req.RunID = opts.QueryID
		if opts.ReplaceRunningQuery {
			// Only ever alongside an id: supersession is by query_id, so
			// the flag on its own would say nothing.
			settings["replace_running_query"] = "1"
		}
	}
	st, _, err := eng.Deliver(ctx, req)
	if err != nil {
		return
	}
	defer func() { _ = st.Close() }()
	// The verdict is the terminal, not the rows: a statement the server
	// rejected comes back as a failed terminal carrying its diagnostic.
	// Frames are dropped as they arrive rather than collected — draining is
	// what lets the connection be reused, and a probe has no use for a body
	// however large it turns out to be.
	var term runstream.Terminal
	var answered bool
	for {
		f, ok := st.Next()
		if !ok {
			break
		}
		if f.Kind == runstream.KindTerminal {
			term, answered = f.Terminal, true
		}
	}
	if !answered {
		err = runstream.ErrIncomplete
		return
	}
	if term.State == runstream.TerminalFailed {
		err = term.Err
	}
	return
}

// bareParams merges the caller's signal and SET-harvested bindings into the
// bare `{name:Type}` names the engine expects, dropping the `param_` prefix
// play carries them under.
//
// A SET-bound name SHADOWS a same-named signal (ADR-0097 slice-5 D1: a SET
// pins a signal into a constant), which is why params are applied second.
func bareParams(signals map[string]string, params map[string]string) (out map[string]string) {
	if len(signals) == 0 && len(params) == 0 {
		return
	}
	out = make(map[string]string, len(signals)+len(params))
	add := func(src map[string]string) {
		for k, v := range src {
			out[strings.TrimPrefix(k, chhttp.ParamPrefix)] = v
		}
	}
	add(signals)
	add(params)
	return
}

// engineFor builds the delivery engine a decision names (ADR-0144).
//
// One engine per run rather than one per client: placement is resolved per
// run, and an engine is bound to ONE server precisely because the roles
// beyond delivery — observing a run in `system.processes`, killing it — only
// mean anything against the member that ran it. Construction is a struct
// literal over the client's shared HTTP client, so per-run is not per-cost.
func (inst *Client) engineFor(dec dispatchDecision) (eng *chserver.Engine, err error) {
	target, err := dec.target()
	if err != nil {
		return
	}
	eng, err = chserver.New(chserver.Config{
		Endpoint:   target,
		User:       inst.cfg.User,
		Password:   inst.cfg.Password,
		HTTPClient: inst.http,
		// Two ways an endpoint may see sealed plaintext (ADR-0145 §SD5):
		// it IS this process's plane — exempt by identity, since the string
		// was minted by a server this process started, which is not the same
		// act as recognising a loopback address in a configured URL — or it
		// has DEMONSTRATED it can fetch from that plane. Derived from the
		// target, never from the decision's own label, so the two gates
		// cannot agree by construction.
		ServesConfined: target == introspect.LocalQueryEndpoint() || inst.reach.isProven(target),
	})
	return
}

// fetchColumnNames returns the physical column names of db.table in position
// order by querying system.columns directly. It deliberately bypasses the pass
// registry (so it cannot recurse through the leeway-name resolver that calls
// it) and the Arrow decode. An empty db resolves to the server's current
// database. The schema-aware pre-execute resolver uses this to learn a leeway
// table's schema before a query ships; failures degrade to "no schema".
func (inst *Client) fetchColumnNames(ctx context.Context, db string, table string) (names []string, err error) {
	const q = "SELECT name FROM system.columns " +
		"WHERE table = {tbl:String} AND database = if({db:String} = '', currentDatabase(), {db:String}) " +
		"ORDER BY position FORMAT TabSeparated"
	reqURL := inst.URL()
	qs := url.Values{}
	qs.Set("param_tbl", table)
	qs.Set("param_db", db)
	sep := "?"
	if strings.Contains(reqURL, "?") {
		sep = "&"
	}
	reqURL = reqURL + sep + qs.Encode()

	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(q))
	if err != nil {
		err = eh.Errorf("unable to build system.columns request: %w", err)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if inst.cfg.User != "" {
		req.Header.Set("X-ClickHouse-User", inst.cfg.User)
	}
	if inst.cfg.Password != "" {
		req.Header.Set("X-ClickHouse-Key", inst.cfg.Password)
	}
	var resp *http.Response
	resp, err = inst.http.Do(req)
	if err != nil {
		err = eh.Errorf("system.columns request failed: %w", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		err = eb.Build().Int("statusCode", resp.StatusCode).Str("body", strings.TrimSpace(string(raw))).Errorf("system.columns http %d", resp.StatusCode)
		return
	}
	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if rerr != nil {
		err = eh.Errorf("unable to read system.columns response: %w", rerr)
		return
	}
	// Single-column TabSeparated: one name per line. Physical leeway names
	// contain only ':' and identifier characters, so no TSV unescaping is
	// needed.
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		names = append(names, line)
	}
	return
}

// buildResidual is steps 1–2 of BuildStatement — the SET-prelude harvest and
// the pre-execute rewrites — shared with the diagnostics EXPLAIN probe, which
// wraps the residual in `EXPLAIN AST` instead of appending a FORMAT clause
// (step 3). Keeping the probe on this path is what makes its verdict match a
// real Run byte-for-byte: both degrade identically on unparseable input.
func (inst *Client) buildResidual(sql string) (residual string, params map[string]string) {
	return inst.buildResidualObserved(sql, nil)
}

// buildResidualObserved is buildResidual with the observer buildStatementObserved
// documents. A nil observe is the plain path.
func (inst *Client) buildResidualObserved(sql string, observe func(passreg.ApplyObservation)) (residual string, params map[string]string) {
	// Ad-hoc dataset alias→handle rewrite runs first, before the SET-param
	// harvest and pre-execute passes, so keelson('<alias>') becomes
	// keelson('<handle>') for every downstream consumer and the Preview
	// "as sent" caption (ADR-0134 §SD4). Classification already ran on the
	// authored buffer, which still names the alias. Not traced: it is a
	// textual substitution over bindings the user made explicitly, with no
	// failure mode to report.
	sql = inst.rewriteDatasetAliases(sql)
	residual, params, exErr := ExtractParams(sql)
	if exErr != nil {
		log.Debug().Err(exErr).Msg("play: ExtractParams failed, sending sql verbatim")
		residual = sql
		params = nil
	}
	observeStep(observe, rewriteStepExtractParams, orderExtractParams, exErr, sql, residual)
	residual = inst.applyExprSplice(residual, observe)
	residual = inst.passes.ApplyBestEffortBoundObserved(passreg.StagePreExecute, residual, inst.passBinding, log.Logger, observe)
	residual = inst.applyExposeConditions(residual, observe)
	return
}

// applyExprSplice substitutes the buffer's SQL-valued placeholders (ADR-0187
// §SD4). It runs between the SET-param harvest and the pass
// registry, which is the one window where both hold: the prelude is already
// gone, so a `param_*` value cannot be confused with an expression, and no
// registered pass has yet seen a body with an `Expr` slot in it — a type
// ClickHouse does not know.
//
// The values are the buffer's own `-- play: expr` lines over this client's
// live binding (see [Client.exprValuesFor]) — the same pair of sources for the
// Preview's "as sent" view and for the run, so the two cannot resolve different
// bodies. At the pinned tier that makes the substitution a function of the text
// alone; at the live tier the value is a client binding, exactly as
// SetExposeConditions and bindDataset already are.
//
// Degrades like every step here: an unparseable buffer reports a skip and ships
// unchanged, and the server answers with the real error.
func (inst *Client) applyExprSplice(sql string, observe func(passreg.ApplyObservation)) (out string) {
	out, _, err := spliceExprSlots(sql, inst.exprValuesFor(sql))
	if err != nil {
		log.Debug().Err(err).Msg("play: expression splice skipped, sending sql verbatim")
		out = sql
	}
	observeStep(observe, rewriteStepSpliceExpr, orderSpliceExpr, err, sql, out)
	return
}

// SetExprValues publishes the LIVE-tier expression values (ADR-0187
// §SD3) — the ones a panel or the pane holds in the signal store rather than in
// the buffer's `-- play: expr` lines.
//
// They cannot travel the way an ordinary live value does. A `param_*` entry on
// the request URL is a VALUE to ClickHouse, so a predicate sent that way is a
// string; the only route into the query is the splice, and the splice reads
// text. Hence a client binding, in the shape SetExposeConditions and
// bindDataset already established: written from the render thread, read
// wherever a query is built.
//
// Whole-map replacement, so a name that left the live tier stops being
// substituted rather than lingering as a stale binding.
func (inst *Client) SetExprValues(values map[string]string) {
	next := make(map[string]string, len(values))
	maps.Copy(next, values)
	inst.mu.Lock()
	inst.exprValues = next
	inst.mu.Unlock()
}

// exprValuesFor merges the two tiers into what the splice substitutes: the
// buffer's own declarations over the live values.
//
// The declaration wins, which is §SD4's shadowing rule applied to this tier
// pair — a pinned name is buffer-owned, and a panel co-writing the same name
// must not silently override what the document states. It is also what makes
// the Preview honest: everything the "as sent" view shows for a pinned name is
// in the text the reader is looking at.
func (inst *Client) exprValuesFor(sql string) (values map[string]string) {
	inst.mu.RLock()
	live := inst.exprValues
	inst.mu.RUnlock()
	declared := scanExprHints(sql)
	if len(live) == 0 {
		return declared
	}
	values = make(map[string]string, len(live)+len(declared))
	maps.Copy(values, live)
	maps.Copy(values, declared)
	return
}

// ExprSubstituted is sql with its SQL-valued placeholders replaced — the body
// ADR-0187 §SD5 classifies.
//
// It substitutes and stops. That is the whole point of its existing separately
// from [Client.buildResidual]: the class must be judged AFTER the user's
// expression enters and BEFORE the pass registry runs, because ADR-0132 §SD5
// classifies the authored buffer precisely so a `keelson('…')` macro is not
// mistaken for the `url()` it may expand into. Running the passes first would
// reverse that decision silently.
//
// The prelude is left in place, so the only difference from the buffer the
// classifier reads today is the substitution.
//
// An unparseable buffer comes back unchanged with the error; the caller then
// classifies what the user wrote, which the classifier already fails closed on.
func (inst *Client) ExprSubstituted(sql string) (out string, err error) {
	out, _, err = spliceExprSlots(sql, inst.exprValuesFor(sql))
	return
}

// bindDataset binds an alias to a dataset handle for the client-side
// alias→handle rewrite (ADR-0134 §SD4). Called from PlayApp.BindDataset.
func (inst *Client) bindDataset(alias, handle string) {
	inst.mu.Lock()
	if inst.datasetBindings == nil {
		inst.datasetBindings = make(map[string]string)
	}
	inst.datasetBindings[alias] = handle
	inst.mu.Unlock()
}

// unbindDataset drops one alias binding (ADR-0188 §SD3). Called from
// PlayApp.UnbindDataset; a no-op for an unbound alias.
func (inst *Client) unbindDataset(alias string) {
	inst.mu.Lock()
	delete(inst.datasetBindings, alias)
	inst.mu.Unlock()
}

// DatasetAliases are the ad-hoc dataset aliases bound in this session, sorted.
//
// They are tables no catalogue enumerates — they exist because this session
// bound them (ADR-0134 §SD4) — which is why the completion engine's table
// answers are routed per buffer rather than per build (ADR-0190 §SD12).
func (inst *Client) DatasetAliases() (aliases []string) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	aliases = make([]string, 0, len(inst.datasetBindings))
	for alias := range inst.datasetBindings {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return
}

// rewriteDatasetAliases applies the bound ad-hoc dataset alias→handle
// rewrite; a no-op when nothing is bound.
func (inst *Client) rewriteDatasetAliases(sql string) string {
	inst.mu.RLock()
	if len(inst.datasetBindings) == 0 {
		inst.mu.RUnlock()
		return sql
	}
	bindings := make(map[string]string, len(inst.datasetBindings))
	maps.Copy(bindings, inst.datasetBindings)
	inst.mu.RUnlock()
	return keelsonsql.RewriteAliases(sql, bindings)
}

// applyExposeConditions runs the opt-in selection-condition rewrite (ADR-0121) when the
// top-bar toggle is on, naming the WHERE predicate's condition columns as columns of the
// result. It sits outside the pass registry deliberately: the rewrite changes a
// query's result schema, so it is this host's opt-in rather than part of the
// standard pre-execute set every consumer shares. It runs after that stage so a
// condition lifted out of the WHERE carries physical column names, not friendly
// leeway handles.
//
// Best-effort, like the registry stage: a refusal — a condition name colliding
// with a real column of the table (§SD4), say — logs and ships the query as the
// user wrote it, rather than failing the Run.
func (inst *Client) applyExposeConditions(sql string, observe func(passreg.ApplyObservation)) (out string) {
	out = sql
	if !inst.exposeConditions.Load() || inst.conditionsPass.Apply == nil {
		// Off, or never installed — not a degraded rewrite, so nothing to
		// report. The toggle is its own visible state.
		return
	}
	next, err := inst.conditionsPass.Run(sql)
	if err != nil {
		log.Warn().Err(err).Msg("play: selection-condition rewrite declined; query sent as written")
		observeStep(observe, rewriteStepExposeConditions, orderExposeConditions, err, "", "")
		return
	}
	out = next
	observeStep(observe, rewriteStepExposeConditions, orderExposeConditions, nil, sql, out)
	return
}

// SetExposeConditions turns the opt-in selection-condition rewrite (ADR-0121)
// on or off.
func (inst *Client) SetExposeConditions(on bool) {
	inst.exposeConditions.Store(on)
}

// ExposeConditions reports whether the selection-condition rewrite is on.
func (inst *Client) ExposeConditions() (on bool) {
	on = inst.exposeConditions.Load()
	return
}

// URL returns the current target endpoint.
func (inst *Client) URL() (u string) {
	inst.mu.RLock()
	u = inst.targetURL
	inst.mu.RUnlock()
	return
}

// SetURL switches the target endpoint. Safe to call from the UI goroutine
// while a query runs on another: ExecuteArrowStream reads the target once at
// request-build time. An empty url is ignored (keeps the current target).
func (inst *Client) SetURL(u string) {
	if u == "" {
		return
	}
	inst.mu.Lock()
	inst.targetURL = u
	inst.mu.Unlock()
}

// ExecuteArrowStream rewrites the query's FORMAT clause to `ArrowStream` via
// the nanopass pipeline, POSTs it, and returns an ipc.Reader over the response
// body and the body closer. The caller must close the body after fully
// draining the reader.
//
// Top-level `SET param_*=...` statements in sql are extracted by ExtractParams
// and shipped on the URL query string (`?param_<name>=<value>`); the residual
// SQL goes in the body. ClickHouse rejects multi-statement bodies, so this
// split is what makes a script like `SET param_a=1; SELECT {a:UInt64}` work
// over a single HTTP request.
//
// # Size limits
//
// We do not use multipart/form-data, so the only relevant cap is the request
// URI cap. Concretely:
//
//   - ClickHouse's `http_max_uri_size` (default 1 MiB) bounds the *total*
//     URL length, including the URL-encoded param names and `&` separators.
//     Exceeding it returns HTTP 414 / "URI is too long" from the server.
//   - Reverse proxies may impose tighter caps (nginx default
//     `large_client_header_buffers` is 8 KiB). When deployed behind one,
//     bump that knob or move to a temp-table strategy for large values.
//   - For reference: ClickHouse's `http_max_field_value_size` (default
//     128 KiB) is the *multipart/form-data* per-field cap. It is stricter
//     per-value than the URL channel, so switching to multipart only helps
//     when the *number* of params (not the size of any one) is the
//     bottleneck — and that switch is not implemented here.
//
// For a single value above the URL cap, stage it in a temp table or raise
// `http_max_uri_size` server-side; there is no client-side fall-back.
//
// opts may be nil; when set, its query_id / replace_running_query ride the URL
// alongside the params (see ExecOptions).
//
// signals carries the caller's resolved signal values (ADR-0097 slice 5a),
// URL-keyed (`param_<name>` → raw); nil/empty means none. They ride the same
// `param_*` channel as the SET-bound constants BuildStatement harvests from
// the body's prelude, and a SET-bound name SHADOWS a same-named signal
// (slice-5 D1: a SET pins a signal into a constant) — the harvested params
// are applied second.
//
// dec is required and names the endpoint (play_dispatch.go): obtain it from
// Client.Dispatch. It is a parameter rather than something read here so that
// every issuer of a request is enumerated by the compiler, and so a run and
// the diagnostic probe describing it provably share one decision.
func (inst *Client) ExecuteArrowStream(ctx context.Context, sql string, alloc memory.Allocator, opts *ExecOptions, signals map[string]string, dec dispatchDecision) (rdr *ipc.Reader, rs *resultStream, summary Summary, err error) {
	// The decision was resolved before the request, so a concurrent SetURL
	// cannot tear it.
	eng, err := inst.engineFor(dec)
	if err != nil {
		return
	}
	var q string
	var params map[string]string
	rowCap := readResultRowCap(sql)
	if opts != nil && opts.WrapStatement != nil {
		// Wire-body wrap (see ExecOptions.WrapStatement): the rewrites and
		// the param harvest ran on the plain statement; the outer FORMAT is
		// appended textually because the machine-built wrapper carries none
		// and one inside the parens would bind to the inner statement. The
		// row cap is the wire statement's own declaration — the inner LIMIT
		// bounds the query being explained, not the wrapper's result.
		var residual string
		residual, params = inst.buildResidualObserved(sql, nil)
		q = opts.WrapStatement(residual) + " FORMAT ArrowStream"
		rowCap = readResultRowCap(q)
	} else {
		q, params = inst.BuildStatement(sql)
	}
	req := queryengine.Request{
		SQL: q,
		// Params ride the URL rather than the body: ClickHouse reads the
		// body verbatim as SQL, and the typed substitution from
		// `{name:Type}` placeholders is what it expects on that channel.
		// See the function doc for the size limits that bounds.
		Params:   bareParams(signals, params),
		Settings: map[string]string{},
		// What the statement declared about its own result size, for the
		// engine to judge the delivery against (R9). play parses it; the
		// engine, which is the only party that sees the response counters,
		// decides.
		Cap: rowCap,
		// What the run touches, decided at the dispatch seam. It rides here
		// so the engine refuses a confined run on its own account rather
		// than trusting whatever placed it (ADR-0145 §SD4).
		Sensitivity: dec.sensitivity,
	}
	if opts != nil {
		if opts.QueryID != "" {
			req.RunID = opts.QueryID
			if opts.ReplaceRunningQuery {
				// Only ever alongside an id: supersession is by query_id,
				// so the flag on its own would say nothing.
				req.Settings["replace_running_query"] = "1"
			}
		}
		req.OnProgress = opts.OnProgress
	}
	// SD7 identity stamp (ADR-0115): {run_id, app, lane, four
	// fingerprints} as compact JSON, so the server's query_log row is
	// attributable and the capture pipeline lifts the identity. Endpoints
	// that don't know the setting ignore the parameter, like query_id.
	if lc := inst.composeLogComment(sql, q, params, signals, opts); lc != "" {
		req.Settings["log_comment"] = lc
	}

	st, res, err := eng.Deliver(ctx, req)
	if err != nil {
		return
	}
	summary = summaryFrom(res.Summary)

	// A run the server rejected ends before any bytes arrive, and opening
	// the stream is what surfaces its diagnostic — handing an empty body to
	// the Arrow decoder would replace "clickhouse http 400: <the actual
	// problem>" with a complaint about a missing IPC header.
	rs, err = openResultStream(st)
	if err != nil {
		return
	}
	rdr, err = ipc.NewReader(rs, ipc.WithAllocator(alloc))
	if err != nil {
		_ = rs.Close()
		rs = nil
		err = eh.Errorf("unable to create arrow ipc reader: %w", err)
		return
	}
	return
}

// summaryFrom lifts the engine's counters into play's display shape. The two
// structs carry the same fields; the conversion is here rather than a shared
// type because play's Summary is what a dozen panels already render.
func summaryFrom(s queryengine.Summary) (out Summary) {
	out = Summary{
		ReadRows:        s.ReadRows,
		ReadBytes:       s.ReadBytes,
		WrittenRows:     s.WrittenRows,
		WrittenBytes:    s.WrittenBytes,
		TotalRowsToRead: s.TotalRowsToRead,
		ResultRows:      s.ResultRows,
		ResultBytes:     s.ResultBytes,
		ElapsedNs:       s.ElapsedNs,
		MemoryUsage:     s.MemoryUsage,
	}
	return
}

// Summary mirrors ClickHouse's X-ClickHouse-Summary JSON-ish header values.
//
// Parsing the header is the engine adapter's job (chserver.ParseSummary);
// this is play's display shape, and [summaryFrom] is the one place the two
// meet.
type Summary struct {
	ReadRows        uint64
	ReadBytes       uint64
	WrittenRows     uint64
	WrittenBytes    uint64
	TotalRowsToRead uint64
	ResultRows      uint64
	ResultBytes     uint64
	ElapsedNs       uint64
	MemoryUsage     uint64
}

func (inst Summary) String() string {
	return fmt.Sprintf("read %d rows / %d bytes", inst.ReadRows, inst.ReadBytes)
}
