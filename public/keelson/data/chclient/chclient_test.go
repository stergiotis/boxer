package chclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

const (
	configFromEnvChildKey = "CHCLIENT_CONFIG_FROM_ENV_CHILD"
	configFromEnvMarker   = "RESOLVED\t"
)

// TestConfigFromEnvChild is the child half of TestConfigFromEnv_Precedence. It
// prints the resolved Config on a marker line and is skipped in a normal run.
func TestConfigFromEnvChild(t *testing.T) {
	if os.Getenv(configFromEnvChildKey) != "1" {
		t.Skip("child-process helper for TestConfigFromEnv_Precedence")
	}
	c := ConfigFromEnv()
	fmt.Printf("\n%s%s\t%s\t%s\n", configFromEnvMarker, c.URL, c.User, c.Password)
}

// ConfigFromEnv reads the entries through env.StringVar.Get, which caches on
// first call, so the precedence cannot be exercised by mutating the
// environment in-process — the first case to run would fix the values for
// every later one. Each case therefore runs in a fresh child of this test
// binary, with inherited CLICKHOUSE_* scrubbed so a developer's shell cannot
// colour the result.
func TestConfigFromEnv_Precedence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      []string
		wantURL  string
		wantUser string
		wantPass string
	}{
		{
			name:     "nothing set falls back to Defaults",
			wantURL:  Defaults().URL,
			wantUser: Defaults().User,
		},
		{
			name:     "endpoint is used",
			env:      []string{"CLICKHOUSE_ENDPOINT=http://endpoint:8123"},
			wantURL:  "http://endpoint:8123",
			wantUser: Defaults().User,
		},
		{
			name:     "url is used when endpoint is unset",
			env:      []string{"CLICKHOUSE_URL=http://url:8123/"},
			wantURL:  "http://url:8123/",
			wantUser: Defaults().User,
		},
		{
			name:     "endpoint beats url",
			env:      []string{"CLICKHOUSE_ENDPOINT=http://endpoint:8123", "CLICKHOUSE_URL=http://url:8123"},
			wantURL:  "http://endpoint:8123",
			wantUser: Defaults().User,
		},
		{
			// An exported-but-empty endpoint is indistinguishable from unset,
			// which is what lets a wrapper export it unconditionally.
			name:     "empty endpoint defers to url",
			env:      []string{"CLICKHOUSE_ENDPOINT=", "CLICKHOUSE_URL=http://url:8123"},
			wantURL:  "http://url:8123",
			wantUser: Defaults().User,
		},
		{
			name:     "credentials override the defaults",
			env:      []string{"CLICKHOUSE_USER=alice", "CLICKHOUSE_PASSWORD=hunter2"},
			wantURL:  Defaults().URL,
			wantUser: "alice",
			wantPass: "hunter2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotUser, gotPass := runConfigFromEnvChild(t, tc.env)
			assert.Equal(t, tc.wantURL, gotURL, "URL")
			assert.Equal(t, tc.wantUser, gotUser, "User")
			assert.Equal(t, tc.wantPass, gotPass, "Password")
		})
	}
}

func runConfigFromEnvChild(t *testing.T, extra []string) (url string, user string, password string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestConfigFromEnvChild$", "-test.v")
	cmd.Env = append(scrubClickHouseEnv(os.Environ()), configFromEnvChildKey+"=1")
	cmd.Env = append(cmd.Env, extra...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, configFromEnvMarker); ok {
			f := strings.Split(after, "\t")
			require.Len(t, f, 3, line)
			return f[0], f[1], f[2]
		}
	}
	require.FailNow(t, "child printed no marker line", string(out))
	return
}

func scrubClickHouseEnv(environ []string) (out []string) {
	out = make([]string, 0, len(environ))
	for _, kv := range environ {
		if strings.HasPrefix(kv, "CLICKHOUSE_") {
			continue
		}
		out = append(out, kv)
	}
	return
}
