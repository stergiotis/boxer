// Package chhttp implements the server side of the ClickHouse HTTP dialect
// (ADR-0133 §SD1): what an in-process endpoint needs to answer HTTP like a
// ClickHouse server, closely enough for clients written against the real
// one (apps/play posts a statement and reads an ArrowStream back).
//
// Request side: statement extraction (POST body, `?query` fallback),
// `param_*` harvest with name validation, and settings tolerance — every
// non-param query-string key is reported, none is rejected, mimicking the
// server's tolerance for clients that stamp (`log_comment`) or harden
// (`readonly`) their requests. Response side: the `X-ClickHouse-Summary`
// header, the FORMAT-tail content type, and the `Code: N. DB::Exception: …`
// exception envelope play's probe classifier parses.
//
// Deliberately out of scope: the client half of the dialect (it lives with
// the clients), auth/TLS (ADR-0082), and bind policy (each host keeps its
// own loopback gate).
package chhttp

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// ParamPrefix marks a query-string key as a statement parameter: the URL key
// `param_<name>` binds the `{<name>:Type}` placeholder server-side.
const ParamPrefix = "param_"

const (
	// HeaderSummary carries per-statement counters as a JSON object with
	// string-encoded numbers (the ClickHouse wire shape).
	HeaderSummary = "X-ClickHouse-Summary"
	// HeaderExceptionCode carries the numeric ClickHouse error code beside
	// the exception body.
	HeaderExceptionCode = "X-ClickHouse-Exception-Code"
)

// Request is the parsed dialect-level content of one /query-style request.
type Request struct {
	// SQL is the statement: the POST body when non-empty, else the `?query`
	// key. May be empty — whether an empty statement is an error is the
	// endpoint's policy, not the dialect's.
	SQL string
	// Params maps placeholder names (the `param_` prefix stripped) to their
	// raw string values; typed substitution stays the engine's job. nil
	// when the request carries none.
	Params map[string]string
	// Ignored lists the non-param, non-query query-string keys, sorted —
	// settings and endpoint-specific hints the dialect tolerates without
	// interpreting. Consumers may pick their own keys out (the introspection
	// endpoint's `cols`) and debug-log the rest; see KnownIgnorableSetting.
	Ignored []string
}

// ParseRequest extracts the dialect-level content of r. The statement is
// read from the POST body first (capped at maxSQLBytes — exceeding the cap
// is an error, never a silent truncation), falling back to `?query`. A
// malformed or duplicated `param_*` key is an error naming the key; every
// other query-string key lands in Ignored.
func ParseRequest(r *http.Request, maxSQLBytes int64) (req Request, err error) {
	b, rerr := io.ReadAll(io.LimitReader(r.Body, maxSQLBytes+1))
	if rerr != nil {
		err = eh.Errorf("chhttp: read statement body: %w", rerr)
		return
	}
	if int64(len(b)) > maxSQLBytes {
		err = eh.Errorf("chhttp: statement exceeds %d bytes", maxSQLBytes)
		return
	}
	req.SQL = strings.TrimSpace(string(b))
	q := r.URL.Query()
	if req.SQL == "" {
		req.SQL = strings.TrimSpace(q.Get("query"))
	}
	for key, values := range q {
		switch {
		case key == "query":
			// consumed above
		case strings.HasPrefix(key, ParamPrefix):
			name := key[len(ParamPrefix):]
			if !validParamName(name) {
				err = eh.Errorf("chhttp: invalid parameter name %q", key)
				return
			}
			if len(values) != 1 {
				err = eh.Errorf("chhttp: parameter %q given %d times", key, len(values))
				return
			}
			if req.Params == nil {
				req.Params = make(map[string]string, 4)
			}
			req.Params[name] = values[0]
		default:
			req.Ignored = append(req.Ignored, key)
		}
	}
	sort.Strings(req.Ignored)
	return
}

// validParamName bounds parameter names to the identifier charset the
// chlocal broker's input tables already enforce: `[A-Za-z_][A-Za-z0-9_]*`,
// at most 64 bytes.
func validParamName(name string) (ok bool) {
	if name == "" || len(name) > 64 {
		return
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		valid := c == '_' ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(i > 0 && c >= '0' && c <= '9')
		if !valid {
			return
		}
	}
	ok = true
	return
}

// KnownIgnorableSetting reports whether an Ignored key is on the documented
// tolerance list (ADR-0133 §SD1) — request decoration a real server would
// consume and an in-process endpoint can safely drop. Keys off the list are
// still tolerated; this predicate only lets a consumer log the genuinely
// unknown ones at debug level.
func KnownIgnorableSetting(key string) (ok bool) {
	switch key {
	case "log_comment", // the ADR-0115 identity stamp
		"query_id",                      // client-chosen statement identity
		"readonly",                      // the ADR-0132 §SD5 enforcement knob
		"send_progress_in_http_headers", // no mid-flight exists on a buffered reply
		"wait_end_of_query",             // buffered replies always wait
		"default_format",                // clients here set FORMAT in the statement
		"database":                      // no database namespace in-process (lossy but tolerated)
		ok = true
	}
	return
}

// Summary carries the per-statement counters HeaderSummary reports. Zero
// values are reported as zero — an in-process transport that surfaces no
// read counters stays honest rather than inventing them (ADR-0133 §SD4).
type Summary struct {
	ReadRows    uint64
	ReadBytes   uint64
	ResultBytes uint64
	Elapsed     time.Duration
}

// JSON renders the ClickHouse wire shape: a JSON object whose numbers are
// string-encoded, field order fixed.
func (inst Summary) JSON() string {
	return fmt.Sprintf(`{"read_rows":"%d","read_bytes":"%d","result_bytes":"%d","elapsed_ns":"%d"}`,
		inst.ReadRows, inst.ReadBytes, inst.ResultBytes, inst.Elapsed.Nanoseconds())
}

// WriteSummary sets HeaderSummary on w. Call before the status is written.
func WriteSummary(w http.ResponseWriter, s Summary) {
	w.Header().Set(HeaderSummary, s.JSON())
}

// ContentTypeForStatement maps the trailing FORMAT clause of sql to a
// best-effort Content-Type. Informational only — a client that set the
// format itself already knows how to read the body. The FORMAT keyword is
// matched case-insensitively but the format name is matched verbatim (the
// engine's format names are case-sensitive identifiers); an unknown or
// absent format is application/octet-stream.
func ContentTypeForStatement(sql string) (ct string) {
	ct = "application/octet-stream"
	i := strings.LastIndex(strings.ToUpper(sql), "FORMAT ")
	if i < 0 {
		return
	}
	name := strings.TrimSpace(sql[i+len("FORMAT "):])
	if j := strings.IndexAny(name, " \t\r\n;"); j >= 0 {
		name = name[:j]
	}
	switch name {
	case "ArrowStream", "Arrow":
		ct = "application/vnd.apache.arrow.stream"
	case "Parquet":
		ct = "application/vnd.apache.parquet"
	case "JSON", "JSONEachRow", "JSONCompact", "JSONStrings":
		ct = "application/json"
	case "CSV", "CSVWithNames":
		ct = "text/csv"
	case "TabSeparated", "TSV", "TabSeparatedWithNames":
		ct = "text/tab-separated-values"
	}
	return
}

// exceptionCodeRe finds the first ClickHouse error code in a message —
// clickhouse-local stderr and server bodies both spell it `Code: N`.
var exceptionCodeRe = regexp.MustCompile(`Code:\s*([0-9]+)`)

// ExtractExceptionCode returns the first `Code: N` occurrence in msg.
func ExtractExceptionCode(msg string) (code int, ok bool) {
	m := exceptionCodeRe.FindStringSubmatch(msg)
	if m == nil {
		return
	}
	n, perr := strconv.Atoi(m[1])
	if perr != nil {
		return
	}
	code = n
	ok = true
	return
}

// WriteException emits the ClickHouse error envelope: plain text, the
// message carrying (or already containing) a `Code: N. DB::Exception: …`
// shape, plus HeaderExceptionCode when a code is known — the body play's
// probe classifier parses. A message with no extractable code passes
// through verbatim with no code header: an invented code would be worse
// than an absent one.
func WriteException(w http.ResponseWriter, httpStatus int, msg string) {
	body := msg
	if code, ok := ExtractExceptionCode(msg); ok {
		w.Header().Set(HeaderExceptionCode, strconv.Itoa(code))
	} else if httpStatus >= 500 {
		// Server-shaped failures without an engine code still get the
		// exception prefix so clients report them uniformly.
		body = "DB::Exception: " + msg
	}
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(httpStatus)
	_, _ = io.WriteString(w, body)
	if !strings.HasSuffix(body, "\n") {
		_, _ = io.WriteString(w, "\n")
	}
}
