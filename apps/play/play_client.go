//go:build llm_generated_opus47

package play

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
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
}

func NewClient(cfg ClientConfig, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{cfg: cfg, http: httpClient}
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
func (inst *Client) ExecuteArrowStream(ctx context.Context, sql string, alloc memory.Allocator) (rdr *ipc.Reader, body io.Closer, summary Summary, err error) {
	// Harvest top-level `SET param_*=...` statements so they can ride the
	// HTTP `param_*` channel rather than being inlined into the body — values
	// can be larger than fits comfortably in a single SQL literal, and the
	// typed substitution from `{name:Type}` placeholders is what ClickHouse
	// expects this way. Failures here are non-fatal: we fall back to sending
	// the SQL verbatim and let the server reject it if appropriate.
	residual, params, exErr := ExtractParams(sql)
	if exErr != nil {
		log.Debug().Err(exErr).Msg("play: ExtractParams failed, sending sql verbatim")
		residual = sql
		params = nil
	}

	// Rewrite the query so it ends with `FORMAT ArrowStream`, replacing any
	// existing FORMAT clause. Falls back to a textual append when the SQL can't
	// be parsed by the nanopass grammar — some ClickHouse surface features are
	// outside Grammar1, and we still want to let the user POST the query.
	q, setErr := passes.SetFormat("ArrowStream").Run(residual)
	if setErr != nil {
		log.Debug().Err(setErr).Msg("play: SetFormat failed, falling back to textual append")
		q = strings.TrimRight(residual, "; \t\n\r")
		if !strings.Contains(strings.ToUpper(q), "FORMAT ") {
			q += " FORMAT ArrowStream"
		}
	}
	// ClickHouse reads the body verbatim as SQL — params must ride the URL
	// query string. See the function doc for size limits.
	reqURL := inst.cfg.URL
	if len(params) > 0 {
		qs := url.Values{}
		for k, v := range params {
			qs.Set(k, v)
		}
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
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		err = eb.Build().Int("statusCode", resp.StatusCode).Str("body", string(msg)).Errorf("clickhouse http error")
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
	// ClickHouse emits `{"read_rows":"123","read_bytes":"456",...}`.
	trim := strings.TrimSpace(s)
	trim = strings.TrimPrefix(trim, "{")
	trim = strings.TrimSuffix(trim, "}")
	kv := map[string]string{}
	for _, part := range strings.Split(trim, ",") {
		kvPair := strings.SplitN(part, ":", 2)
		if len(kvPair) != 2 {
			continue
		}
		k := strings.Trim(strings.TrimSpace(kvPair[0]), `"`)
		v := strings.Trim(strings.TrimSpace(kvPair[1]), `"`)
		kv[k] = v
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
