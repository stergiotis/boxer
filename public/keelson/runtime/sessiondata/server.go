package sessiondata

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/chhttp"
	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/db/fairlead"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The fairlead sites of the capability's own statements (ADR-0367 §SD1).
// Each is led with the task mark cleared: the text is the capability's,
// whoever's call caused it.
const (
	// SiteCreate is a definition's first create.
	SiteCreate = "sessiondata.create"
	// SiteReplay is a create that makes a recorded definition's table again.
	SiteReplay = "sessiondata.replay"
	// SiteProbe is a read of the session's tables, sizes and columns.
	SiteProbe = "sessiondata.probe"
	// SiteDrop is a drop of a table the service made.
	SiteDrop = "sessiondata.drop"
)

// The ClickHouse error codes the capability acts on.
const (
	codeReadonly              = 164
	codeSessionNotFound       = 372
	codeSessionIsLocked       = 373
	codeInvalidSessionTimeout = 374
)

// ServerError is a statement the server answered with an error: its code,
// when the server named one, and its diagnostic.
type ServerError struct {
	Status  int
	Code    int
	Message string
}

func (inst *ServerError) Error() (s string) {
	return "clickhouse http " + strconv.Itoa(inst.Status) + ": " + inst.Message
}

// IsSessionLost reports whether err is the server saying a session it was
// asked to continue is gone: expired, or never there. err is one of the
// capability's own, or a run's error carrying the server's diagnostic, as
// the server engine reports it. A caller whose run failed so retries
// [Service.Prepare], which replays what the run reads.
func IsSessionLost(err error) (lost bool) {
	return serverCode(err) == codeSessionNotFound
}

// IsSessionBusy reports whether err is the server refusing a statement
// because another is running in the same session.
func IsSessionBusy(err error) (busy bool) {
	return serverCode(err) == codeSessionIsLocked
}

// serverCode is the ClickHouse error code err carries, 0 when none.
func serverCode(err error) (code int) {
	if err == nil {
		return 0
	}
	var se *ServerError
	if errors.As(err, &se) {
		return se.Code
	}
	if c, ok := chhttp.ExtractExceptionCode(err.Error()); ok {
		return c
	}
	return 0
}

var codeRe = regexp.MustCompile(`Code: (\d+)\.`)

// maxResponseBytes bounds what the capability reads back from one of its
// own statements: a table list or a column list.
const maxResponseBytes = 4 << 20

// serverConn is one endpoint under one ClickHouse identity.
type serverConn struct {
	url      string
	user     string
	password string
	http     *http.Client
}

// execSpec is how one of the capability's statements is sent.
type execSpec struct {
	site      string
	sessionID string
	timeout   time.Duration
	// check sends session_check=1: the server refuses, rather than opens, a
	// session it does not hold.
	check    bool
	external []queryengine.ExternalTable
	stamp    logcomment.Stamp
}

// exec leads sql through the fairlead and sends it, returning the body.
// The statement is the capability's own text, so the task mark is cleared
// on the context Lead sees (ADR-0403 §SD1); the stamp in log_comment names
// the task whose work it is, for the server's query log.
func (inst *serverConn) exec(ctx context.Context, spec execSpec, sql string) (body []byte, err error) {
	sql, err = fairlead.Lead(fairlead.WithTask(ctx, ""), fairlead.ExitServer, spec.site, sql)
	if err != nil {
		return
	}
	qs := url.Values{}
	qs.Set("readonly", "2")
	qs.Set("default_format", "JSONEachRow")
	qs.Set("output_format_json_quote_64bit_integers", "0")
	if spec.sessionID != "" {
		qs.Set("session_id", spec.sessionID)
		qs.Set("session_timeout", strconv.FormatInt(int64(spec.timeout/time.Second), 10))
		if spec.check {
			qs.Set("session_check", "1")
		}
	}
	if lc := logcomment.Marshal(spec.stamp); lc != "" {
		qs.Set("log_comment", lc)
	}
	for _, x := range spec.external {
		qs.Set(x.Name+"_format", x.Format)
		qs.Set(x.Name+"_structure", x.Structure)
	}
	reqBody, contentType, err := statementBody(sql, spec.external)
	if err != nil {
		return
	}
	target := inst.url
	if strings.Contains(target, "?") {
		target += "&" + qs.Encode()
	} else {
		target += "?" + qs.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, reqBody)
	if err != nil {
		return nil, eh.Errorf("build the request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if inst.user != "" {
		req.Header.Set("X-ClickHouse-User", inst.user)
	}
	if inst.password != "" {
		req.Header.Set("X-ClickHouse-Key", inst.password)
	}
	resp, err := inst.http.Do(req)
	if err != nil {
		return nil, eb.Build().Str("endpoint", env.RedactURL(inst.url)).Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		code, _ := strconv.Atoi(resp.Header.Get(chhttp.HeaderExceptionCode))
		if code == 0 {
			if m := codeRe.FindStringSubmatch(msg); m != nil {
				code, _ = strconv.Atoi(m[1])
			}
		}
		if len(msg) > 8<<10 {
			msg = msg[:8<<10]
		}
		return nil, &ServerError{Status: resp.StatusCode, Code: code, Message: msg}
	}
	if readErr != nil {
		return nil, eh.Errorf("read the response: %w", readErr)
	}
	if len(raw) > maxResponseBytes {
		return nil, eh.Errorf("the response is larger than the capability reads")
	}
	return raw, nil
}

// statementBody is the statement as the body, or a multipart form whose
// query field is the statement and whose file parts are the external
// tables — the shape the server's external-data interface reads.
func statementBody(sql string, external []queryengine.ExternalTable) (body io.Reader, contentType string, err error) {
	if len(external) == 0 {
		return strings.NewReader(sql), "text/plain; charset=utf-8", nil
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err = mw.WriteField("query", sql); err != nil {
		return nil, "", eh.Errorf("write the statement field: %w", err)
	}
	for _, x := range external {
		part, pErr := mw.CreateFormFile(x.Name, x.Name)
		if pErr == nil {
			_, pErr = part.Write(x.Data)
		}
		if pErr != nil {
			return nil, "", eb.Build().Str("name", x.Name).Errorf("write an external table: %w", pErr)
		}
	}
	if err = mw.Close(); err != nil {
		return nil, "", eh.Errorf("close the form: %w", err)
	}
	return bytes.NewReader(buf.Bytes()), mw.FormDataContentType(), nil
}

// tableStat is one row of the probe.
type tableStat struct {
	Name       string  `json:"name"`
	TotalRows  *uint64 `json:"total_rows"`
	TotalBytes *uint64 `json:"total_bytes"`
}

// columnRow is one row of DESCRIBE TABLE.
type columnRow struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// decodeRows decodes a JSONEachRow body.
func decodeRows[T any](body []byte) (rows []T, err error) {
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64<<10), maxResponseBytes)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var row T
		if err = json.Unmarshal(line, &row); err != nil {
			return nil, eh.Errorf("decode a row of the server's answer: %w", err)
		}
		rows = append(rows, row)
	}
	if err = sc.Err(); err != nil {
		return nil, eh.Errorf("read the server's answer: %w", err)
	}
	return
}

// serverKey names one endpoint under one identity: a server session is
// per key, because a ClickHouse session belongs to the user that opened
// it, and the password is part of the key so that a caller cannot reach a
// session another opened by naming the same user.
func serverKey(e Endpoint) (key string) {
	h := blake3.Sum256([]byte(e.URL + "\x00" + e.User + "\x00" + e.Password))
	return hex.EncodeToString(h[:12])
}

// reservedURLParams are the URL parameters an endpoint's URL may not carry:
// the server reads `query` as the start of the statement, and the others
// are the capability's to set on its own statements.
var reservedURLParams = []string{"query", "readonly", "session_id", "session_timeout", "session_check", "log_comment", "default_format"}

// checkEndpointURL refuses an endpoint URL that is not an http(s) URL with
// a host, or whose parameters would change the statements the capability
// sends on it.
func checkEndpointURL(raw string) (err error) {
	u, pErr := url.Parse(raw)
	if pErr != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return eb.Build().Str("endpoint", env.RedactURL(raw)).Errorf("an endpoint is an http or https URL with a host")
	}
	for key := range u.Query() {
		lower := strings.ToLower(key)
		if slices.Contains(reservedURLParams, lower) || strings.HasPrefix(lower, "param_") ||
			strings.HasSuffix(lower, "_format") || strings.HasSuffix(lower, "_structure") || strings.HasSuffix(lower, "_types") {
			return eb.Build().Str("parameter", key).Errorf("an endpoint's URL may not carry this parameter; the capability sets its statements' parameters itself")
		}
	}
	return
}

// EndpointHost is how a grant names an endpoint: its host and port, or the
// URL itself when it does not parse.
func EndpointHost(rawURL string) (host string) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return strings.TrimSpace(rawURL)
	}
	return u.Host
}

// newToken is a random identifier: prefix and 32 lowercase hex digits
// (128 bits from crypto/rand).
func newToken(prefix string) (token string, err error) {
	var b [16]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", eh.Errorf("random identifier: %w", err)
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
