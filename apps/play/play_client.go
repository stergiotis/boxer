package play

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
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
	mu        sync.RWMutex
	targetURL string
}

func NewClient(cfg ClientConfig, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{cfg: cfg, http: httpClient, passes: passreg.Default, targetURL: cfg.URL}
}

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
}

// execQueryIDSeq disambiguates lanes within one process; the pid disambiguates
// processes sharing a server.
var execQueryIDSeq atomic.Uint64

// newExecOptions mints a lane's stable ExecOptions. The label names the lane
// in server-side observability (system.processes / query_log).
func newExecOptions(label string) *ExecOptions {
	return &ExecOptions{
		QueryID:             fmt.Sprintf("play-%s-%d-%d", label, os.Getpid(), execQueryIDSeq.Add(1)),
		ReplaceRunningQuery: true,
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
	residual, params := inst.buildResidual(sql)
	body, setErr := passes.SetFormat("ArrowStream").Run(residual)
	if setErr != nil {
		log.Debug().Err(setErr).Msg("play: SetFormat failed, falling back to textual append")
		body = strings.TrimRight(residual, "; \t\n\r")
		if !strings.Contains(strings.ToUpper(body), "FORMAT ") {
			body += " FORMAT ArrowStream"
		}
	}
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
func (inst *Client) ProbeStatement(ctx context.Context, sql string, params map[string]string, opts *ExecOptions) (err error) {
	reqURL := inst.URL()
	qs := url.Values{}
	for k, v := range params {
		qs.Set(k, v)
	}
	if opts != nil && opts.QueryID != "" {
		qs.Set("query_id", opts.QueryID)
		if opts.ReplaceRunningQuery {
			qs.Set("replace_running_query", "1")
		}
	}
	if len(qs) > 0 {
		sep := "?"
		if strings.Contains(reqURL, "?") {
			sep = "&"
		}
		reqURL = reqURL + sep + qs.Encode()
	}
	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(sql))
	if err != nil {
		err = eh.Errorf("unable to build clickhouse request: %w", err)
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
		err = eh.Errorf("clickhouse request failed: %w", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		detail := strings.TrimSpace(string(raw))
		bld := eb.Build().Int("statusCode", resp.StatusCode).Str("body", detail)
		if detail == "" {
			detail = "(empty response body)"
		}
		err = bld.Errorf("clickhouse http %d: %s", resp.StatusCode, detail)
		return
	}
	// Drain (bounded) so the connection can be reused; the content is unused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
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
	residual, params, exErr := ExtractParams(sql)
	if exErr != nil {
		log.Debug().Err(exErr).Msg("play: ExtractParams failed, sending sql verbatim")
		residual = sql
		params = nil
	}
	residual = inst.passes.ApplyBestEffortBound(passreg.StagePreExecute, residual, inst.passBinding, log.Logger)
	residual = inst.applyExposeConditions(residual)
	return
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
func (inst *Client) applyExposeConditions(sql string) (out string) {
	out = sql
	if !inst.exposeConditions.Load() || inst.conditionsPass.Apply == nil {
		return
	}
	next, err := inst.conditionsPass.Run(sql)
	if err != nil {
		log.Warn().Err(err).Msg("play: selection-condition rewrite declined; query sent as written")
		return
	}
	out = next
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
func (inst *Client) ExecuteArrowStream(ctx context.Context, sql string, alloc memory.Allocator, opts *ExecOptions, signals map[string]string) (rdr *ipc.Reader, body io.Closer, summary Summary, err error) {
	q, params := inst.BuildStatement(sql)
	// ClickHouse reads the body verbatim as SQL — params must ride the URL
	// query string. See the function doc for size limits. The target is read
	// once here so a concurrent SetURL never tears a request mid-build.
	reqURL := inst.URL()
	qs := url.Values{}
	for k, v := range signals {
		qs.Set(k, v)
	}
	for k, v := range params { // SET-bound constants shadow same-named signals
		qs.Set(k, v)
	}
	if opts != nil && opts.QueryID != "" {
		qs.Set("query_id", opts.QueryID)
		if opts.ReplaceRunningQuery {
			qs.Set("replace_running_query", "1")
		}
	}
	if len(qs) > 0 {
		sep := "?"
		if strings.Contains(reqURL, "?") {
			sep = "&"
		}
		reqURL = reqURL + sep + qs.Encode()
	}
	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(q))
	if err != nil {
		err = eh.Errorf("unable to build clickhouse request: %w", err)
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
		err = eh.Errorf("clickhouse request failed: %w", err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		// ClickHouse reports the real problem — the SQL error text and its help
		// hint — in the response body. Fold it into the error *message* (not
		// only the structured field) so the play UI, which renders err.Error()
		// in the Table tab and the status bar, shows the user what actually
		// failed instead of a bare "clickhouse http error".
		detail := strings.TrimSpace(string(raw))
		bld := eb.Build().Int("statusCode", resp.StatusCode).Str("body", detail)
		if detail == "" {
			detail = "(empty response body)"
		}
		err = bld.Errorf("clickhouse http %d: %s", resp.StatusCode, detail)
		return
	}
	summary = parseSummaryHeader(resp.Header.Get("X-ClickHouse-Summary"))

	rdr, err = ipc.NewReader(resp.Body, ipc.WithAllocator(alloc))
	if err != nil {
		_ = resp.Body.Close()
		err = eh.Errorf("unable to create arrow ipc reader: %w", err)
		return
	}
	body = resp.Body
	return
}

// Summary mirrors ClickHouse's X-ClickHouse-Summary JSON-ish header values.
type Summary struct {
	ReadRows        uint64
	ReadBytes       uint64
	WrittenRows     uint64
	WrittenBytes    uint64
	TotalRowsToRead uint64
	ResultRows      uint64
	ResultBytes     uint64
	ElapsedNs       uint64
}

func parseSummaryHeader(s string) (out Summary) {
	if s == "" {
		return
	}
	// ClickHouse emits a flat JSON object of string-typed counters, e.g.
	// `{"read_rows":"123","read_bytes":"456",...}`.
	kv := map[string]string{}
	if err := json.Unmarshal([]byte(s), &kv); err != nil {
		log.Debug().Err(err).Str("header", s).Msg("play: malformed X-ClickHouse-Summary header")
		return
	}
	parseU64 := func(k string) uint64 {
		n, _ := strconv.ParseUint(kv[k], 10, 64)
		return n
	}
	out.ReadRows = parseU64("read_rows")
	out.ReadBytes = parseU64("read_bytes")
	out.WrittenRows = parseU64("written_rows")
	out.WrittenBytes = parseU64("written_bytes")
	out.TotalRowsToRead = parseU64("total_rows_to_read")
	out.ResultRows = parseU64("result_rows")
	out.ResultBytes = parseU64("result_bytes")
	out.ElapsedNs = parseU64("elapsed_ns")
	return
}

func (inst Summary) String() string {
	return fmt.Sprintf("read %d rows / %d bytes", inst.ReadRows, inst.ReadBytes)
}
