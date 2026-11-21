package chstore

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identgen/leased/memalloc"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// sentRequest is one request the fake server received: the statement, and
// for an insert the columns of the Arrow body.
type sentRequest struct {
	query string
	cols  []string
}

// fakeServer answers every statement with an empty body and records what it
// was sent. It stands in for ClickHouse so that a store's statements can be
// read without one.
type fakeServer struct {
	mu   sync.Mutex
	sent []sentRequest
}

func (inst *fakeServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s := sentRequest{query: r.URL.Query().Get("query")}
	if s.query == "" {
		s.query = string(body)
	} else if rdr, err := ipc.NewFileReader(bytes.NewReader(body)); err == nil {
		for _, f := range rdr.Schema().Fields() {
			s.cols = append(s.cols, f.Name)
		}
		_ = rdr.Close()
	}
	inst.mu.Lock()
	inst.sent = append(inst.sent, s)
	inst.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (inst *fakeServer) requests() (out []sentRequest) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	out = append(out, inst.sent...)
	inst.sent = nil
	return
}

// newFakeStore is a store over a fake server, minting ids in process so
// that no lease statement is sent.
func newFakeStore(t *testing.T, cfg Config) (st *Store, srv *fakeServer) {
	t.Helper()
	srv = &fakeServer{}
	hs := httptest.NewServer(http.HandlerFunc(srv.handle))
	t.Cleanup(hs.Close)
	cfg.URL = hs.URL + "/"
	cfg.RowIds.Allocator = memalloc.NewAllocator()
	st, err := New(cfg)
	require.NoError(t, err)
	return
}

// A write of the hand-written store reaches the server through the
// executor, so a transform registered for its table rewrites it (ADR-0402
// §SD1): the insert names the transform's table and carries its columns.
func TestWrites_GoThroughTheRegisteredTransform(t *testing.T) {
	st, srv := newFakeStore(t, Config{Database: "scratch_chstore_tr", Table: "facts"})
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	var seenCols int
	unregister := recordstore.RegisterInsertTransform("scratch_chstore_tr.facts", recordstore.InsertTransformFunc(
		func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			seenCols = int(records[0].NumCols())
			b := array.NewUint64Builder(alloc)
			defer b.Release()
			b.Append(uint64(records[0].NumRows()))
			arr := b.NewArray()
			defer arr.Release()
			schema := arrow.NewSchema([]arrow.Field{{Name: "rows", Type: arrow.PrimitiveTypes.Uint64}}, nil)
			return "scratch_chstore_tr.carrier", []arrow.RecordBatch{array.NewRecordBatch(schema, []arrow.Array{arr}, 1)}, nil
		}))
	defer unregister()

	ctx := context.Background()
	_, err := st.WriteGrant(ctx, factsstore.GrantRow{AppId: "example.app", Pattern: "a.>", Direction: app.CapDirectionPub})
	require.NoError(t, err)
	_, err = st.WriteLogs(ctx, []factsstore.LogRow{{Level: "info", Message: "one"}, {Level: "info", Message: "two"}})
	require.NoError(t, err)

	got := srv.requests()
	require.Len(t, got, 2)
	for _, s := range got {
		assert.Equal(t, "INSERT INTO scratch_chstore_tr.carrier FORMAT Arrow", s.query)
		assert.Equal(t, []string{"rows"}, s.cols)
	}
	assert.Greater(t, seenCols, 20, "the transform receives the batches in the facts columns")

	unregister()
	_, err = st.WriteGrant(ctx, factsstore.GrantRow{AppId: "example.app", Pattern: "a.>", Direction: app.CapDirectionPub})
	require.NoError(t, err)
	got = srv.requests()
	require.Len(t, got, 1)
	assert.Equal(t, "INSERT INTO scratch_chstore_tr.facts FORMAT Arrow", got[0].query, "with nothing registered the facts table is written")
}
