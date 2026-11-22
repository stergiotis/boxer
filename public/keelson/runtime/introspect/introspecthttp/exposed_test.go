package introspecthttp

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

const exposedToken = "bxe_0123456789abcdef0123456789abcdef"

// fakeExposures opens one alias for one token, and records what each request
// declared and what the listener reported back.
type fakeExposures struct {
	alias  string
	stream []byte
	busy   bool

	mu    sync.Mutex
	asked []introspect.ExposedRequest
	done  []uint64
	errs  []error
}

func (inst *fakeExposures) OpenExposed(alias string, token string, req introspect.ExposedRequest) (rd introspect.ExposedRead, status introspect.ExposedStatusE) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if alias != inst.alias || token != exposedToken {
		return rd, introspect.ExposedNotFound
	}
	if inst.busy {
		return rd, introspect.ExposedBusy
	}
	inst.asked = append(inst.asked, req)
	r := nopSeekCloser{bytes.NewReader(inst.stream)}
	rd = introspect.ExposedRead{Reader: r, Revision: 3, Done: func(sent uint64, err error) {
		inst.mu.Lock()
		inst.done, inst.errs = append(inst.done, sent), append(inst.errs, err)
		inst.mu.Unlock()
		_ = r.Close()
	}}
	return rd, introspect.ExposedOpen
}

type nopSeekCloser struct{ *bytes.Reader }

func (nopSeekCloser) Close() error { return nil }

// arrowStream is an Arrow IPC stream of n rows of (id Int64, name String).
func arrowStream(t *testing.T, n int) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}, {Name: "name", Type: arrow.BinaryTypes.String}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	for i := range n {
		b.Field(0).(*array.Int64Builder).Append(int64(i))
		b.Field(1).(*array.StringBuilder).Append("n" + string(rune('a'+i%26)))
	}
	rec := b.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	if n > 0 {
		require.NoError(t, w.Write(rec))
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// exposedServer is a started listener whose registry has src as its source,
// logging into logs.
func exposedServer(t *testing.T, src introspect.ExposureSourceI, logs io.Writer) (s *Server) {
	t.Helper()
	r := introspect.NewRegistry()
	if src != nil {
		r.SetExposureSource(src)
	}
	s = New(Config{Registry: r}, zerolog.New(logs))
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return
}

func exposedGet(t *testing.T, method string, url string, header map[string]string) (resp *http.Response, body []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	return
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// A request that opens nothing is answered alike whatever the reason: no
// source, no token, another scheme, a wrong token, an alias not exposed. The
// answer says nothing about whether the alias or the token exists.
func TestExposedAnswersEveryFailureAlike(t *testing.T) {
	var logs bytes.Buffer
	src := &fakeExposures{alias: "result", stream: arrowStream(t, 5)}
	s := exposedServer(t, src, &logs)
	bare := exposedServer(t, nil, &logs)
	for name, c := range map[string]struct {
		url    string
		header map[string]string
	}{
		"no source":     {bare.BaseURL() + "/exposed/result", bearer(exposedToken)},
		"no token":      {s.BaseURL() + "/exposed/result", nil},
		"basic scheme":  {s.BaseURL() + "/exposed/result", map[string]string{"Authorization": "Basic " + exposedToken}},
		"wrong token":   {s.BaseURL() + "/exposed/result", bearer(exposedToken + "x")},
		"another alias": {s.BaseURL() + "/exposed/other", bearer(exposedToken)},
		"csv, no token": {s.BaseURL() + "/exposed/result/csv", nil},
	} {
		resp, body := exposedGet(t, http.MethodGet, c.url, c.header)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, name)
		assert.Equal(t, exposedNotFound, string(body), name)
		assert.Empty(t, resp.Header.Get("WWW-Authenticate"), name)
	}
	assert.Empty(t, src.asked, "no failed request opened the dataset")
	assert.NotContains(t, logs.String(), exposedToken, "the token is never logged")
}

// A reader holding the token gets the Arrow stream, a HEAD answers its
// length without a body, and a range answers the part asked for, as
// ClickHouse's url() needs; each request is reported with the bytes sent.
func TestExposedServesTheStreamWithHeadAndRanges(t *testing.T) {
	var logs bytes.Buffer
	stream := arrowStream(t, 50)
	src := &fakeExposures{alias: "result", stream: stream}
	s := exposedServer(t, src, &logs)
	url := s.BaseURL() + "/exposed/result"

	resp, body := exposedGet(t, http.MethodGet, url, bearer(exposedToken))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, stream, body)
	assert.Equal(t, "application/vnd.apache.arrow.stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, `"result-r3"`, resp.Header.Get("ETag"))
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	resp, body = exposedGet(t, http.MethodHead, url, bearer(exposedToken))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, body)
	assert.Equal(t, int64(len(stream)), resp.ContentLength)
	assert.Equal(t, "bytes", resp.Header.Get("Accept-Ranges"))

	h := bearer(exposedToken)
	h["Range"] = "bytes=10-19"
	resp, body = exposedGet(t, http.MethodGet, url, h)
	require.Equal(t, http.StatusPartialContent, resp.StatusCode)
	assert.Equal(t, stream[10:20], body)

	src.mu.Lock()
	defer src.mu.Unlock()
	require.Len(t, src.asked, 3)
	assert.False(t, src.asked[0].Head)
	assert.True(t, src.asked[1].Head)
	assert.Equal(t, "bytes=10-19", src.asked[2].Range)
	assert.Equal(t, "Go-http-client/1.1", src.asked[0].UserAgent, "what the request declared, not an identity")
	assert.True(t, strings.HasPrefix(src.asked[0].Remote, "127.0.0.1:"))
	assert.Equal(t, []uint64{uint64(len(stream)), 0, 10}, src.done)
	assert.NotContains(t, logs.String(), exposedToken)
}

// CSV carries a header line and every row, streamed; an empty dataset is its
// header line; a HEAD answers without a body and without a length.
func TestExposedServesCSV(t *testing.T) {
	src := &fakeExposures{alias: "result", stream: arrowStream(t, 3)}
	s := exposedServer(t, src, io.Discard)
	resp, body := exposedGet(t, http.MethodGet, s.BaseURL()+"/exposed/result/csv", bearer(exposedToken))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "id,name\n0,na\n1,nb\n2,nc\n", string(body))
	assert.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv"))
	assert.Equal(t, "none", resp.Header.Get("Accept-Ranges"))

	resp, body = exposedGet(t, http.MethodHead, s.BaseURL()+"/exposed/result/csv", bearer(exposedToken))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, body)

	empty := &fakeExposures{alias: "result", stream: arrowStream(t, 0)}
	s = exposedServer(t, empty, io.Discard)
	_, body = exposedGet(t, http.MethodGet, s.BaseURL()+"/exposed/result/csv", bearer(exposedToken))
	assert.Equal(t, "id,name\n", string(body))
}

// A valid token while the exposure admits no more reads is told to retry;
// only a holder of the token learns that much.
func TestExposedBusyAsksToRetry(t *testing.T) {
	src := &fakeExposures{alias: "result", stream: arrowStream(t, 1), busy: true}
	s := exposedServer(t, src, io.Discard)
	resp, _ := exposedGet(t, http.MethodGet, s.BaseURL()+"/exposed/result", bearer(exposedToken))
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, "1", resp.Header.Get("Retry-After"))
}

// The exposed path keeps the listener's Host check, and neither path opens
// the other's: a token does not open /table, and /table's handle does not
// open /exposed.
func TestExposedKeepsTheHostCheckAndItsOwnPath(t *testing.T) {
	src := &fakeExposures{alias: "result", stream: arrowStream(t, 1)}
	s := exposedServer(t, src, io.Discard)
	_, port, err := net.SplitHostPort(s.Addr())
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodGet, s.BaseURL()+"/exposed/result", nil)
	require.NoError(t, err)
	req.Host = "attacker.example:" + port
	req.Header.Set("Authorization", "Bearer "+exposedToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusMisdirectedRequest, resp.StatusCode)

	resp, _ = exposedGet(t, http.MethodGet, s.BaseURL()+"/table/result", bearer(exposedToken))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "an exposure is not a table")
}

// The registry learns the base URL a started listener serves it at, and
// forgets it when that listener stops.
func TestTheRegistryKnowsWhereItIsServed(t *testing.T) {
	r := introspect.NewRegistry()
	assert.Empty(t, r.ServedAt())
	s := New(Config{Registry: r}, zerolog.Nop())
	require.NoError(t, s.Start())
	assert.Equal(t, s.BaseURL(), r.ServedAt())
	require.NoError(t, s.Stop(context.Background()))
	assert.Empty(t, r.ServedAt())
}
