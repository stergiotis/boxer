// Package chserver delivers query results from a ClickHouse server over
// the synchronous HTTP interface — engine 2 of [ADR-0144]'s three, and the
// only one that plays all three roles.
//
// A server is the engine with something to say about a run while it runs.
// It registers the client-minted id in `system.processes` and retains it in
// `query_log`, so a party that never held the connection can watch the run
// ([queryengine.ObservationI], backed by the E7 poller) and stop it
// ([queryengine.ControlI], by `KILL QUERY`). Both of those address the run
// by the same id the results are correlated with, which is the point of
// minting it client-side (R7).
//
// Delivery is a stream of byte frames in the caller's requested FORMAT.
// Decoding is the consumer's business, deliberately: the engine's job ends
// at "here are the bytes the server sent, and here is how the run ended".
//
// # An engine is one server
//
// An instance is bound to one endpoint, because everything the optional
// roles do is per-server: `system.processes` is not shared between members,
// and a `KILL QUERY` only reaches the member that ran the query (R11).
// Pointing one instance at a load-balanced address would silently observe
// and cancel on whichever member happened to answer. Construction is cheap
// — a caller resolving placement per run builds one per run.
//
// [ADR-0144]: https://github.com/stergiotis/boxer/blob/main/doc/adr/0144-query-engine-adapters.md
package chserver

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/chhttp"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runid"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// maxErrorBodyBytes caps how much of a failed response is folded into the
// error. ClickHouse reports the real problem — the SQL error and its hint —
// in the body, and a reader who is shown a bare status code has to go
// looking for what actually broke.
const maxErrorBodyBytes = 64 << 10

// reservedKeys are the query-string keys this adapter owns. A caller
// setting one through [queryengine.Request.Settings] is rejected rather
// than silently overridden: two `query_id` values means a run with two
// identities, and two `default_format` values means a body nobody can
// decode. `param_*` is reserved for the same reason — bindings belong in
// Params, where they are named once.
var reservedKeys = map[string]struct{}{
	"query_id":                          {},
	"default_format":                    {},
	"send_progress_in_http_headers":     {},
	"http_headers_progress_interval_ms": {},
}

// Config parameterises an [Engine]. Endpoint is required.
type Config struct {
	// Endpoint is the ClickHouse HTTP endpoint of the ONE server this
	// engine talks to.
	Endpoint string
	// User and Password authenticate every request.
	User     string
	Password string
	// HTTPClient is the client ordinary requests use; nil means a stock
	// one. A request asking for live progress swaps in its own transport
	// (see progress.go) and this client is not consulted for it.
	HTTPClient *http.Client
	// ServesConfined declares that this server may see the plaintext of
	// sealed data (ADR-0145 §SD4). Default false: an engine refuses a
	// confined run unless the party that built it said otherwise.
	//
	// This is a discipline gate, not a security boundary — the caller
	// asserts it, so it is only as good as the caller. Its value is that
	// forgetting is loud and local: a new issuer that never considered
	// sealed data gets a refusal naming the reason, rather than a query
	// that works until the endpoint moves. The wall a router cannot
	// override lives above, at the dispatch seam.
	ServesConfined bool
}

// Engine delivers results from one ClickHouse server, and cancels runs on
// it.
type Engine struct {
	endpoint       string
	user           string
	password       string
	http           *http.Client
	servesConfined bool
}

var (
	_ queryengine.DeliveryI = (*Engine)(nil)
	_ queryengine.ControlI  = (*Engine)(nil)
)

// New returns an Engine bound to one endpoint.
func New(cfg Config) (inst *Engine, err error) {
	if cfg.Endpoint == "" {
		err = eh.Errorf("chserver: endpoint is required")
		return
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	inst = &Engine{
		endpoint:       cfg.Endpoint,
		user:           cfg.User,
		password:       cfg.Password,
		http:           httpClient,
		servesConfined: cfg.ServesConfined,
	}
	return
}

// Endpoint reports the server this engine is bound to.
func (inst *Engine) Endpoint() (endpoint string) {
	endpoint = inst.endpoint
	return
}

// Deliver POSTs the statement and returns the response body as a frame
// stream.
//
// The result stream carries the body in chunks and then exactly one
// terminal frame. A statement the server rejected is a failed terminal, not
// an error from this call: the run happened and ended badly, which is an
// outcome the contract already has a shape for.
func (inst *Engine) Deliver(ctx context.Context, req queryengine.Request) (st queryengine.StreamI, res queryengine.Result, err error) {
	err = req.Validate()
	if err != nil {
		return
	}
	if req.Sensitivity == queryengine.SensitivityConfined && !inst.servesConfined {
		// The backstop of ADR-0145 §SD4. Whatever placed this run, this
		// engine was not told it may see sealed plaintext, so it does not
		// execute — and says which of the two facts is missing.
		err = eb.Build().Str("endpoint", inst.endpoint).Errorf("chserver: refusing a confined run: this endpoint is not declared as allowed to see sealed data")
		return
	}
	if len(req.Inputs) > 0 {
		// A server reached over HTTP has no way to receive an in-memory
		// dataset as a temporary table on the same request. Refusing beats
		// dropping them: the query would otherwise fail against the server
		// with "unknown table" and nothing would say the inputs never left.
		err = eb.Build().Int("inputs", len(req.Inputs)).
			Errorf("chserver: this engine cannot bind in-memory input tables; stage them server-side first")
		return
	}
	qs, err := inst.queryString(req)
	if err != nil {
		return
	}

	httpClient := inst.http
	if req.OnProgress != nil {
		// ADR-0115 plane A: ask the server to stream progress inside the
		// still-open response-header block, and swap in the transport that
		// can surface those lines mid-run. An endpoint that cannot do it
		// keeps the stock client and the run simply reports nothing until
		// it finishes — progress is advisory, so degrading loses nothing
		// the contract promised.
		qs.Set("send_progress_in_http_headers", "1")
		qs.Set("http_headers_progress_interval_ms", strconv.Itoa(progressIntervalMs))
		if pc := newProgressClient(inst.endpoint, req.OnProgress); pc != nil {
			httpClient = pc
		}
	}

	reqURL := inst.endpoint
	if len(qs) > 0 {
		sep := "?"
		if strings.Contains(reqURL, "?") {
			sep = "&"
		}
		reqURL = reqURL + sep + qs.Encode()
	}
	httpReq, buildErr := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(req.SQL))
	if buildErr != nil {
		err = eh.Errorf("chserver: unable to build request: %w", buildErr)
		return
	}
	httpReq.Header.Set("Content-Type", "text/plain; charset=utf-8")
	inst.authenticate(httpReq)

	resp, doErr := httpClient.Do(httpReq)
	if doErr != nil {
		// The request never got an answer. That is how this run ended.
		st = queryengine.NewSliceStream(nil, runstream.Failed(eh.Errorf("chserver: request failed: %w", doErr)), nil)
		return
	}
	if resp.StatusCode != http.StatusOK {
		st = queryengine.NewSliceStream(nil, runstream.Failed(errorFromResponse(resp)), nil)
		return
	}
	summary := ParseSummary(resp.Header.Get(chhttp.HeaderSummary))
	res = queryengine.Result{
		ContentType: resp.Header.Get("Content-Type"),
		Summary:     summary,
	}
	st = queryengine.NewReaderStream(resp.Body, req.Cap.TerminalFor(summary.ResultRows), resp.Body, 0)
	return
}

// queryString builds the request's URL parameters, rejecting a caller that
// reaches for a key this adapter owns.
func (inst *Engine) queryString(req queryengine.Request) (qs url.Values, err error) {
	qs = url.Values{}
	for name, value := range req.Params {
		qs.Set(chhttp.ParamPrefix+name, value)
	}
	for key, value := range req.Settings {
		if _, reserved := reservedKeys[key]; reserved || strings.HasPrefix(key, chhttp.ParamPrefix) {
			qs = nil
			err = eb.Build().Str("setting", key).
				Errorf("chserver: this setting is owned by the adapter and must not be set by the caller")
			return
		}
		qs.Set(key, value)
	}
	if req.RunID != "" {
		qs.Set("query_id", req.RunID)
	}
	if req.Format != "" {
		// default_format rather than a FORMAT clause appended to the
		// statement: the statement is finalized by the time it arrives here
		// and this adapter does not rewrite SQL. A FORMAT the statement
		// carries itself still wins, which is the server's rule, not ours.
		qs.Set("default_format", req.Format)
	}
	return
}

func (inst *Engine) authenticate(req *http.Request) {
	if inst.user != "" {
		req.Header.Set("X-ClickHouse-User", inst.user)
	}
	if inst.password != "" {
		req.Header.Set("X-ClickHouse-Key", inst.password)
	}
}

// errorFromResponse folds a non-200 into an error carrying the server's own
// diagnostic, in the shape callers parse ("clickhouse http <code>: <body>").
func errorFromResponse(resp *http.Response) (err error) {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	_ = resp.Body.Close()
	detail := strings.TrimSpace(string(raw))
	bld := eb.Build().Int("statusCode", resp.StatusCode).Str("body", detail)
	if detail == "" {
		detail = "(empty response body)"
	}
	err = bld.Errorf("clickhouse http %d: %s", resp.StatusCode, detail) //boxer:lint disable=CS013 reason="play/play_diagnostics.go classifies on this exact prefix; the message is a cross-component contract"
	return
}

// Kill asks the server to stop the run named by runID (R11).
//
// A nil error is not evidence that anything stopped. `KILL QUERY` matching
// no row is a success, and a run that had already finished, one that never
// existed, and one this call ended are indistinguishable from here — as is
// an endpoint that speaks the dialect but has no process list to search,
// such as the in-process introspection plane, whose one-shot workers are
// gone before anything could be addressed to them. Terminal truth comes
// from the result path, never from this.
func (inst *Engine) Kill(ctx context.Context, runID string) (err error) {
	if !runid.Valid(runID) {
		err = eb.Build().Str("runId", runID).
			Errorf("chserver: refusing to kill by an id that is not safe as a SQL literal")
		return
	}
	// The id is interpolated as a literal, which is safe only because the
	// charset check above rejected everything a quote could hide in.
	sql := "KILL QUERY WHERE query_id='" + runID + "' ASYNC"
	req, buildErr := http.NewRequestWithContext(ctx, "POST", inst.endpoint, strings.NewReader(sql))
	if buildErr != nil {
		err = eh.Errorf("chserver: unable to build kill request: %w", buildErr)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	inst.authenticate(req)
	resp, doErr := inst.http.Do(req)
	if doErr != nil {
		err = eh.Errorf("chserver: kill request failed: %w", doErr)
		return
	}
	if resp.StatusCode != http.StatusOK {
		err = errorFromResponse(resp)
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
	_ = resp.Body.Close()
	return
}
