package chhttp

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMaxSQL = 1 << 20

func TestParseRequestStatementSources(t *testing.T) {
	// POST body is the primary channel.
	r := httptest.NewRequest("POST", "/query", strings.NewReader("SELECT 1\n"))
	req, err := ParseRequest(r, testMaxSQL)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", req.SQL)
	assert.Nil(t, req.Params)
	assert.Empty(t, req.Ignored)

	// ?query is the fallback for an empty body.
	r = httptest.NewRequest("POST", "/query?query=SELECT+2", nil)
	req, err = ParseRequest(r, testMaxSQL)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 2", req.SQL)

	// A non-empty body wins over ?query.
	r = httptest.NewRequest("POST", "/query?query=SELECT+2", strings.NewReader("SELECT 1"))
	req, err = ParseRequest(r, testMaxSQL)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", req.SQL)

	// An empty statement is not the dialect's error — endpoint policy.
	r = httptest.NewRequest("POST", "/query", nil)
	req, err = ParseRequest(r, testMaxSQL)
	require.NoError(t, err)
	assert.Empty(t, req.SQL)

	// Exceeding the cap errors — never a silent truncation.
	r = httptest.NewRequest("POST", "/query", strings.NewReader(strings.Repeat("x", 32)))
	_, err = ParseRequest(r, 16)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds 16 bytes")
}

func TestParseRequestParams(t *testing.T) {
	r := httptest.NewRequest("POST",
		"/query?param_lim=50&param_name=&log_comment=stamp&cols=a,b", strings.NewReader("SELECT 1"))
	req, err := ParseRequest(r, testMaxSQL)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"lim": "50", "name": ""}, req.Params,
		"prefix stripped; an empty value is a legal binding")
	assert.Equal(t, []string{"cols", "log_comment"}, req.Ignored, "sorted; params and query excluded")

	// A malformed name errors, naming the key.
	r = httptest.NewRequest("POST", "/query?param_1bad=x", strings.NewReader("SELECT 1"))
	_, err = ParseRequest(r, testMaxSQL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"param_1bad"`)

	// The bare prefix is malformed too.
	r = httptest.NewRequest("POST", "/query?param_=x", strings.NewReader("SELECT 1"))
	_, err = ParseRequest(r, testMaxSQL)
	require.Error(t, err)

	// A duplicated key is ambiguous.
	r = httptest.NewRequest("POST", "/query?param_x=1&param_x=2", strings.NewReader("SELECT 1"))
	_, err = ParseRequest(r, testMaxSQL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 times")
}

func TestKnownIgnorableSetting(t *testing.T) {
	for _, key := range []string{"log_comment", "query_id", "readonly",
		"send_progress_in_http_headers", "wait_end_of_query", "default_format", "database"} {
		assert.True(t, KnownIgnorableSetting(key), key)
	}
	assert.False(t, KnownIgnorableSetting("cols"), "endpoint-specific hints are the consumer's")
	assert.False(t, KnownIgnorableSetting("frobnicate"))
}

// TestSummaryJSON pins the wire shape: string-encoded numbers, fixed field
// order — what play's status-bar parser reads off the real server.
func TestSummaryJSON(t *testing.T) {
	s := Summary{ReadRows: 1, ReadBytes: 2, ResultBytes: 3, Elapsed: 4500 * time.Microsecond}
	assert.Equal(t,
		`{"read_rows":"1","read_bytes":"2","result_bytes":"3","elapsed_ns":"4500000"}`,
		s.JSON())
	assert.Equal(t,
		`{"read_rows":"0","read_bytes":"0","result_bytes":"0","elapsed_ns":"0"}`,
		Summary{}.JSON(), "zeros stay honest zeros (ADR-0133 SD4)")

	rec := httptest.NewRecorder()
	WriteSummary(rec, s)
	assert.Equal(t, s.JSON(), rec.Header().Get(HeaderSummary))
}

// TestContentTypeForStatement pins the mapping introspecthttp shipped with
// (M3 adopts this as a drop-in), including the case quirk: the FORMAT
// keyword is found case-insensitively, the format name is matched verbatim.
func TestContentTypeForStatement(t *testing.T) {
	cases := []struct{ sql, ct string }{
		{"SELECT 1 FORMAT ArrowStream", "application/vnd.apache.arrow.stream"},
		{"SELECT 1 FORMAT Arrow", "application/vnd.apache.arrow.stream"},
		{"SELECT 1 FORMAT Parquet;", "application/vnd.apache.parquet"},
		{"SELECT 1 FORMAT JSONEachRow", "application/json"},
		{"SELECT 1 FORMAT CSVWithNames", "text/csv"},
		{"SELECT 1 FORMAT TabSeparated\n", "text/tab-separated-values"},
		{"SELECT 1 format TabSeparated", "text/tab-separated-values"},
		{"SELECT 1 FORMAT arrowstream", "application/octet-stream"},
		{"SELECT 1", "application/octet-stream"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.ct, ContentTypeForStatement(tc.sql), tc.sql)
	}
}

func TestExtractExceptionCode(t *testing.T) {
	code, ok := ExtractExceptionCode("Code: 62. DB::Exception: Syntax error")
	assert.True(t, ok)
	assert.Equal(t, 62, code)

	code, ok = ExtractExceptionCode("introspect: run failed: Code: 60. DB::Exception: no table")
	assert.True(t, ok, "wrapped messages still carry the code")
	assert.Equal(t, 60, code)

	_, ok = ExtractExceptionCode("plain failure, no engine code")
	assert.False(t, ok)
}

func TestWriteException(t *testing.T) {
	// A coded message passes through with the code header set.
	rec := httptest.NewRecorder()
	WriteException(rec, 400, "runner: Code: 62. DB::Exception: Syntax error")
	assert.Equal(t, 400, rec.Code)
	assert.Equal(t, "62", rec.Header().Get(HeaderExceptionCode))
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.Contains(t, body, "Code: 62. DB::Exception: Syntax error")
	assert.True(t, strings.HasSuffix(body, "\n"))

	// A code-less client-shaped failure passes through verbatim, no header.
	rec = httptest.NewRecorder()
	WriteException(rec, 400, "empty query")
	assert.Empty(t, rec.Header().Get(HeaderExceptionCode))
	assert.Equal(t, "empty query\n", rec.Body.String())

	// A code-less server-shaped failure gains the exception prefix so
	// clients report it uniformly; still no invented code.
	rec = httptest.NewRecorder()
	WriteException(rec, 503, "no runner configured")
	assert.Empty(t, rec.Header().Get(HeaderExceptionCode))
	assert.Equal(t, "DB::Exception: no runner configured\n", rec.Body.String())
}
