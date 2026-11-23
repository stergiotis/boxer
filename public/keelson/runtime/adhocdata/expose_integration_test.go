//go:build integration

package adhocdata

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
)

// These run against the server the endpoint variables name
// (chclient.ConfigFromEnv), which must run on this machine, and skip when
// it does not answer. They create session temporary tables only. The
// server's user needs READ ON URL, and its remote_url_allow_hosts must
// admit 127.0.0.1.

// liveServer is the server's URL with its credentials applied, or a skip.
func liveServer(t *testing.T) (cfg chclient.Config) {
	t.Helper()
	cfg = chclient.ConfigFromEnv()
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(strings.TrimRight(cfg.URL, "/") + "/ping")
	if err != nil {
		t.Skipf("no ClickHouse server at %s: %v", cfg.URL, err)
	}
	_ = resp.Body.Close()
	return
}

// chQuery runs sql in session on the server and returns its TSV answer, or
// the server's error text with ok false.
func chQuery(t *testing.T, cfg chclient.Config, session string, sql string) (out string, ok bool) {
	t.Helper()
	q := url.Values{"session_id": {session}, "readonly": {"2"}}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.URL, "/")+"/?"+q.Encode(), strings.NewReader(sql))
	require.NoError(t, err)
	if cfg.User != "" {
		req.SetBasicAuth(cfg.User, cfg.Password)
	}
	resp, err := (&http.Client{Timeout: time.Minute}).Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return strings.TrimSpace(string(body)), resp.StatusCode == http.StatusOK
}

// wideStream is n rows of (v Int64, label String), v from 0.
func wideStream(t *testing.T, n int) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}, {Name: "label", Type: arrow.BinaryTypes.String}}, nil)
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	const batch = 65_536
	for start := 0; start < n; start += batch {
		for i := start; i < min(n, start+batch); i++ {
			b.Field(0).(*array.Int64Builder).Append(int64(i))
			b.Field(1).(*array.StringBuilder).Append("label-" + strconv.Itoa(i%97))
		}
		rec := b.NewRecordBatch()
		require.NoError(t, w.Write(rec))
		rec.Release()
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// A server on this machine reads an exposed dataset through url() and
// joins it with a table of its own; a plain HTTP client fetches it; each
// fetch is on the trail with what the request declared; a revoked exposure
// is a failed read for the server too.
func TestLiveAServerOnThisMachineJoinsAnExposedDataset(t *testing.T) {
	cfg := liveServer(t)
	r := newExposeRig(t)
	const rows = 300_000
	stream := wideStream(t, rows)
	_, err := r.svc.Publish(PublishInput{Alias: "kernel_result", ArrowIPCStream: stream, By: windowA})
	require.NoError(t, err)
	res, err := r.svc.Expose(t.Context(), ExposeInput{Alias: "kernel_result", By: windowA})
	require.NoError(t, err)
	t.Logf("stream bytes %d; url() expression length %d", len(stream), len(res.ClickHouse))

	var sid [8]byte
	_, _ = rand.Read(sid[:])
	session := "adhoc-expose-it-" + hex.EncodeToString(sid[:])
	out, ok := chQuery(t, cfg, session, "CREATE TEMPORARY TABLE weights AS SELECT toInt64(number) AS id, number % 7 AS w FROM numbers(1000)")
	require.True(t, ok, "the server refused a session temporary table: %s", out)

	// The key is Int64 on both sides: a server before 26.9 finds no common
	// type for an Int64 key and a UInt64 one.
	out, ok = chQuery(t, cfg, session, "SELECT count(), sum(e.v * t.w), uniqExact(e.label) FROM "+res.ClickHouse+
		" AS e INNER JOIN weights AS t ON e.v = t.id FORMAT TSV")
	require.True(t, ok, "the server could not read the exposure; its user needs READ ON URL and remote_url_allow_hosts must admit 127.0.0.1: %s", out)
	var want int64
	for i := range int64(1000) {
		want += i * (i % 7)
	}
	assert.Equal(t, "1000\t"+strconv.FormatInt(want, 10)+"\t97", out)

	out, ok = chQuery(t, cfg, session, "SELECT count() FROM "+res.ClickHouse+" FORMAT TSV")
	require.True(t, ok, out)
	assert.Equal(t, strconv.Itoa(rows), out)

	reads := 0
	for _, rec := range r.records() {
		if rec.Action == ExposureAuditRead {
			reads++
			assert.Contains(t, rec.UserAgent, "ClickHouse", "what the server's request declared")
		}
	}
	assert.Positive(t, reads)
	t.Logf("the server's two statements made %d reads on the trail", reads)

	req, err := http.NewRequest(http.MethodGet, res.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+res.Token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, stream, body, "a plain client receives the stream as sealed")

	require.NoError(t, r.svc.RevokeExposure(t.Context(), res.Id, windowA, nil))
	out, ok = chQuery(t, cfg, session, "SELECT count() FROM "+res.ClickHouse+" FORMAT TSV")
	assert.False(t, ok, "a revoked exposure is a failed read")
	assert.Contains(t, out, "404")
	assert.NotContains(t, out, res.Token[len(res.Token)-16:], "the server's error does not echo the token")
}
