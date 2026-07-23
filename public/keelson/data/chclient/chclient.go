// Package chclient is a HTTP-based ClickHouse client for runtime services
// per ADR-0026 M2.5b. Modelled on play.Client (the SQL-playground HTTP
// client) plus the card_anchor integration test's minimal InsertArrow
// path. Provides:
//
//   - Ping for skip-if-unavailable test gating.
//   - Exec for DDL and side-effect SQL.
//   - Query for SELECT with caller-managed body decoding.
//   - InsertArrow for Arrow IPC bulk writes via FORMAT Arrow.
//
// CGO-free; HTTP port 8123 by convention. The project's localhost CH
// reference is in memory: reference_clickhouse_localhost_defaults.
package chclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Config carries the connection URL + credentials. URL is the base HTTP
// endpoint (e.g. "http://localhost:8123/"); operations append query
// strings as needed.
type Config struct {
	URL      string
	User     string
	Password string
}

// Defaults returns the project's localhost ClickHouse coordinates per the
// user-confirmed defaults (memory: reference_clickhouse_localhost_defaults).
func Defaults() (c Config) {
	c = Config{
		URL:  "http://localhost:8123/",
		User: "default",
	}
	return
}

// Client wraps net/http with the small set of CH-specific shaping the
// runtime needs (headers, FORMAT Arrow URL building). Goroutine-safe.
type Client struct {
	cfg  Config
	http *http.Client
}

// New constructs a Client. Passing nil for httpClient applies a 30s timeout.
func New(cfg Config, httpClient *http.Client) (inst *Client) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	inst = &Client{cfg: cfg, http: httpClient}
	return
}

func (inst *Client) injectHeaders(req *http.Request) {
	if inst.cfg.User != "" {
		req.Header.Set("X-ClickHouse-User", inst.cfg.User)
	}
	if inst.cfg.Password != "" {
		req.Header.Set("X-ClickHouse-Key", inst.cfg.Password)
	}
}

// Ping returns nil when the CH server answers HTTP /ping with 200. Tests
// use this for skip-if-unavailable logic so the suite is green without a
// running server.
func (inst *Client) Ping(ctx context.Context) (err error) {
	pingURL := strings.TrimRight(inst.cfg.URL, "/") + "/ping"
	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, pingURL, nil)
	if err != nil {
		err = eh.Errorf("chclient ping: build: %w", err)
		return
	}
	inst.injectHeaders(req)
	var resp *http.Response
	resp, err = inst.http.Do(req)
	if err != nil {
		err = eb.Build().Str("url", pingURL).Errorf("chclient ping: do: %w", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err = eb.Build().Int("status", resp.StatusCode).Errorf("chclient ping: non-200")
		return
	}
	return
}

// Exec POSTs sql to the base URL and discards the response body. Used for
// DDL and any SQL that does not return rows.
func (inst *Client) Exec(ctx context.Context, sql string) (err error) {
	var body io.ReadCloser
	body, err = inst.postSQL(ctx, sql, nil)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
	return
}

// Query POSTs sql and returns the response body for caller consumption.
// Caller MUST close. Format is whatever the SQL FORMAT clause specifies.
func (inst *Client) Query(ctx context.Context, sql string) (body io.ReadCloser, err error) {
	body, err = inst.postSQL(ctx, sql, nil)
	return
}

// QueryParams is Query with server-side parameter binding: each params entry
// rides the ClickHouse HTTP `param_<name>` URL channel, where the server
// substitutes it into the matching `{<name>:Type}` placeholder in sql. The SQL
// text itself stays constant, so values — including user-supplied ones — are
// never concatenated into the statement.
//
// Keys are the bare placeholder names: params["q"] binds `{q:String}`. Values
// are the raw ClickHouse text form for the placeholder's declared type
// (`[1,2,3]` for an Array(UInt64), an unquoted string for a String). A nil or
// empty map behaves exactly like Query.
//
// Caller MUST close the returned body.
func (inst *Client) QueryParams(ctx context.Context, sql string, params map[string]string) (body io.ReadCloser, err error) {
	body, err = inst.postSQL(ctx, sql, params)
	return
}

// InsertArrow POSTs Arrow IPC records to INSERT INTO {table} FORMAT Arrow.
// The records slice may be empty (no-op). Caller is responsible for
// releasing the records after the call returns.
func (inst *Client) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	if len(records) == 0 {
		return
	}
	buf := &bytes.Buffer{}
	var w *ipc.FileWriter
	w, err = ipc.NewFileWriter(buf, ipc.WithSchema(records[0].Schema()))
	if err != nil {
		err = eh.Errorf("chclient insertArrow: writer: %w", err)
		return
	}
	for _, rec := range records {
		err = w.Write(rec)
		if err != nil {
			err = eh.Errorf("chclient insertArrow: write record: %w", err)
			return
		}
	}
	err = w.Close()
	if err != nil {
		err = eh.Errorf("chclient insertArrow: close writer: %w", err)
		return
	}
	fullURL := inst.queryURL("INSERT INTO " + table + " FORMAT Arrow")
	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, fullURL, buf)
	if err != nil {
		err = eh.Errorf("chclient insertArrow: build: %w", err)
		return
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	inst.injectHeaders(req)
	var resp *http.Response
	resp, err = inst.http.Do(req)
	if err != nil {
		err = eb.Build().Str("url", fullURL).Errorf("chclient insertArrow: do: %w", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Warn().Str("body", string(bodyBytes)).Int("status", resp.StatusCode).
			Str("table", table).Msg("chclient insertArrow: non-200")
		err = eb.Build().Int("status", resp.StatusCode).Str("response", string(bodyBytes)).
			Errorf("chclient insertArrow: failed")
		return
	}
	return
}

func (inst *Client) queryURL(queryParam string) (u string) {
	base := inst.cfg.URL
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	u = base + sep + "query=" + url.QueryEscape(queryParam)
	return
}

// paramsURL appends the `param_<name>` query fields the ClickHouse HTTP
// interface reads server-side bindings from. An empty params map leaves the
// configured URL untouched, so the no-parameter path allocates nothing.
func (inst *Client) paramsURL(params map[string]string) (u string) {
	u = inst.cfg.URL
	if len(params) == 0 {
		return
	}
	vals := make(url.Values, len(params))
	for k, v := range params {
		vals.Set("param_"+k, v)
	}
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	u += sep + vals.Encode()
	return
}

func (inst *Client) postSQL(ctx context.Context, sql string, params map[string]string) (body io.ReadCloser, err error) {
	reqURL := inst.paramsURL(params)
	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(sql))
	if err != nil {
		err = eh.Errorf("chclient post: build: %w", err)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	inst.injectHeaders(req)
	var resp *http.Response
	resp, err = inst.http.Do(req)
	if err != nil {
		// cfg.URL, not reqURL: the latter carries the bound parameter values,
		// which are caller data (search terms, ids) and must not land in logs.
		err = eb.Build().Str("url", inst.cfg.URL).Errorf("chclient post: do: %w", err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		// The body carries ClickHouse's own diagnostic ("Code: 47. Unknown
		// expression identifier …"), which names the offending column or
		// placeholder. It goes into the message, not only the structured
		// field, because consumers that surface errors to a human — a GUI
		// panel, a CLI — render Error() and would otherwise show a bare
		// "non-200" with the one useful sentence stripped out. Truncated in
		// the message; the field keeps it whole for the log sink.
		err = eb.Build().Int("status", resp.StatusCode).Str("response", string(bodyBytes)).
			Errorf("chclient post: non-200: %s", truncateForMessage(bodyBytes))
		return
	}
	body = resp.Body
	return
}

// maxMessageBody bounds how much of a ClickHouse error body is inlined into an
// error message. Long enough for the "Code: N. Name: detail" prefix that
// carries the diagnosis, short enough not to flood a log line or a GUI label.
const maxMessageBody = 400

func truncateForMessage(body []byte) (s string) {
	s = strings.TrimSpace(string(body))
	if len(s) > maxMessageBody {
		s = s[:maxMessageBody] + "…"
	}
	return
}
