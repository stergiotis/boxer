package chclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPing_LiveServer exercises Ping against the project's localhost CH
// (per reference_clickhouse_localhost_defaults). Skips when the server is
// unreachable so the suite stays green offline.
func TestPing_LiveServer(t *testing.T) {
	c := New(Defaults(), nil)
	err := c.Ping(context.Background())
	if err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", Defaults().URL, err)
	}
}

func TestPing_HttpTestServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL + "/", User: "default"}, nil)
	err := c.Ping(context.Background())
	require.NoError(t, err)
}

func TestPing_ServerDown(t *testing.T) {
	c := New(Config{URL: "http://127.0.0.1:1/", User: "default"}, &http.Client{})
	err := c.Ping(context.Background())
	require.Error(t, err)
}

func TestExec_HttpTestServer(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL + "/", User: "default"}, nil)
	err := c.Exec(context.Background(), "CREATE TABLE foo (x UInt64) ENGINE = Memory")
	require.NoError(t, err)
	assert.Contains(t, gotBody, "CREATE TABLE foo")
}

func TestQuery_ReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "1\n2\n3\n")
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL + "/", User: "default"}, nil)
	body, err := c.Query(context.Background(), "SELECT 1")
	require.NoError(t, err)
	defer body.Close()
	out, _ := io.ReadAll(body)
	assert.Equal(t, "1\n2\n3\n", string(out))
}

func TestQuery_NonOk_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "syntax error")
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL + "/", User: "default"}, nil)
	_, err := c.Query(context.Background(), "BAD SQL")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-200")
}

// The ClickHouse diagnostic must reach Error(), not just the structured
// field: GUI and CLI consumers render the message.
func TestQuery_NonOk_MessageCarriesServerDiagnostic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "Code: 47. DB::Exception: Unknown expression identifier 'nope'")
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL + "/", User: "default"}, nil)
	_, err := c.Query(context.Background(), "SELECT nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown expression identifier 'nope'")
}

func TestQuery_NonOk_MessageTruncatesLongBody(t *testing.T) {
	long := strings.Repeat("x", maxMessageBody*3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, long)
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL + "/", User: "default"}, nil)
	_, err := c.Query(context.Background(), "SELECT 1")
	require.Error(t, err)
	assert.Less(t, len(err.Error()), maxMessageBody*2)
	assert.Contains(t, err.Error(), "…")
}

func TestQueryParams_BindsOverParamChannel(t *testing.T) {
	var gotBody, gotQ, gotKids string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotQ = r.URL.Query().Get("param_q")
		gotKids = r.URL.Query().Get("param_kids")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "{}\n")
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL + "/", User: "default"}, nil)
	body, err := c.QueryParams(context.Background(),
		"SELECT * FROM t WHERE name = {q:String} AND id IN {kids:Array(UInt64)}",
		map[string]string{"q": "peftiev", "kids": "[1,2,3]"})
	require.NoError(t, err)
	defer body.Close()
	// The values ride the URL channel; the statement text is untouched, which
	// is the property that makes user input safe to bind.
	assert.Equal(t, "peftiev", gotQ)
	assert.Equal(t, "[1,2,3]", gotKids)
	assert.Contains(t, gotBody, "{q:String}")
	assert.NotContains(t, gotBody, "peftiev")
}

// A value carrying URL metacharacters must survive encoding intact rather
// than splitting into extra query fields.
func TestQueryParams_EscapesValues(t *testing.T) {
	var gotQ string
	var fieldCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.Query().Get("param_q")
		fieldCount = len(r.URL.Query())
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL + "/", User: "default"}, nil)
	body, err := c.QueryParams(context.Background(), "SELECT {q:String}",
		map[string]string{"q": "a&param_x=1 b/c?d"})
	require.NoError(t, err)
	defer body.Close()
	assert.Equal(t, "a&param_x=1 b/c?d", gotQ)
	assert.Equal(t, 1, fieldCount)
}

func TestQueryParams_EmptyMapMatchesQuery(t *testing.T) {
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL + "/", User: "default"}, nil)
	body, err := c.QueryParams(context.Background(), "SELECT 1", nil)
	require.NoError(t, err)
	defer body.Close()
	assert.Empty(t, gotRawQuery)
}

func TestParamsURL_PreservesExistingQuery(t *testing.T) {
	c := &Client{cfg: Config{URL: "http://localhost:8123/?async_insert=1"}}
	got := c.paramsURL(map[string]string{"q": "x"})
	assert.Contains(t, got, "async_insert=1&param_q=x")
}

func TestQueryURL_AppendsQueryParam(t *testing.T) {
	c := &Client{cfg: Config{URL: "http://localhost:8123/"}}
	got := c.queryURL("INSERT INTO foo FORMAT Arrow")
	assert.True(t, strings.HasPrefix(got, "http://localhost:8123/?query="))
	assert.Contains(t, got, "INSERT")
}

func TestQueryURL_PreservesExistingQuery(t *testing.T) {
	c := &Client{cfg: Config{URL: "http://localhost:8123/?async_insert=1"}}
	got := c.queryURL("INSERT INTO foo FORMAT Arrow")
	assert.Contains(t, got, "async_insert=1&query=")
}
