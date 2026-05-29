//go:build llm_generated_opus47

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
	body, err = inst.postSQL(ctx, sql)
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
	body, err = inst.postSQL(ctx, sql)
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

func (inst *Client) postSQL(ctx context.Context, sql string) (body io.ReadCloser, err error) {
	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, inst.cfg.URL, strings.NewReader(sql))
	if err != nil {
		err = eh.Errorf("chclient post: build: %w", err)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	inst.injectHeaders(req)
	var resp *http.Response
	resp, err = inst.http.Do(req)
	if err != nil {
		err = eb.Build().Str("url", inst.cfg.URL).Errorf("chclient post: do: %w", err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		err = eb.Build().Int("status", resp.StatusCode).Str("response", string(bodyBytes)).
			Errorf("chclient post: non-200")
		return
	}
	body = resp.Body
	return
}
