package play

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// arrowServer serves a fixed Arrow IPC stream and counts the requests it sees,
// so a test can assert how many times the executor actually hit the wire.
func arrowServer(t *testing.T, vals []int64) (srv *httptest.Server, hits *int) {
	t.Helper()
	stream := arrowStreamBytes(t, vals)
	n := 0
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("X-ClickHouse-Summary", `{"read_rows":"2","read_bytes":"16"}`)
		_, _ = w.Write(stream)
	}))
	return srv, &n
}

// clientExecutor runs a query over HTTP and concatenates the Arrow stream.
func TestClientExecutorExecutesArrowStream(t *testing.T) {
	srv, hits := arrowServer(t, []int64{10, 20, 30})
	defer srv.Close()

	exec := clientExecutor{client: NewClient(ClientConfig{URL: srv.URL}, srv.Client())}
	rec, schema, err := exec.execute(context.Background(), "SELECT n FROM t", memory.NewGoAllocator())
	require.NoError(t, err)
	require.NotNil(t, rec)
	defer rec.Release()
	require.Equal(t, int64(3), rec.NumRows())
	require.Equal(t, "n", schema.Field(0).Name)
	require.Equal(t, 1, *hits)
}

// End to end: the queryGraph runs a real (httptest-served) query through
// clientExecutor, and minimality holds over the real executor — demanding the
// unchanged node twice hits the wire once.
func TestQueryGraphRunsRealExecutorWithMinimality(t *testing.T) {
	srv, hits := arrowServer(t, []int64{1, 2})
	defer srv.Close()

	g := newQueryGraph(clientExecutor{client: NewClient(ClientConfig{URL: srv.URL}, srv.Client())}, memory.NewGoAllocator())
	defer g.close()
	g.addNode(&Node{ID: "main", Compile: func(SignalEnvI) (string, error) { return "SELECT n FROM t", nil }})

	r1, err := g.demand(context.Background(), "main")
	require.NoError(t, err)
	require.NoError(t, r1.err)
	require.Equal(t, int64(2), r1.rec.NumRows())
	require.Equal(t, 1, *hits)

	// Unchanged compiled SQL ⇒ memo hit, no second wire hit (minimality, SD1).
	_, err = g.demand(context.Background(), "main")
	require.NoError(t, err)
	require.Equal(t, 1, *hits, "an unchanged node must not re-hit the wire")
}
