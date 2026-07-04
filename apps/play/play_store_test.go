package play

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// arrowStreamBytes encodes a one-column Int64 record as an Arrow IPC stream —
// the wire shape ClickHouse's FORMAT ArrowStream produces, so the QueryStore's
// ipc.Reader path can consume it.
func arrowStreamBytes(t *testing.T, vals []int64) []byte {
	t.Helper()
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil)
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.AppendValues(vals, nil)
	arr := b.NewArray()
	defer arr.Release()
	rec := array.NewRecord(schema, []arrow.Array{arr}, int64(len(vals)))
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(mem))
	if err := w.Write(rec); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// schemaOnlyStreamBytes encodes an Arrow IPC stream carrying a schema and ZERO
// record batches — the wire shape of a query whose result is empty.
func schemaOnlyStreamBytes(t *testing.T) []byte {
	t.Helper()
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil)
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(mem))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func waitNotLoading(t *testing.T, s *QueryStore) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !s.IsLoading() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("query still loading after 3s")
}

func TestQueryStoreExecuteRows(t *testing.T) {
	stream := arrowStreamBytes(t, []int64{10, 20})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-ClickHouse-Summary", `{"read_rows":"2","read_bytes":"16"}`)
		_, _ = w.Write(stream)
	}))
	defer srv.Close()

	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 100)
	store.Execute("SELECT n FROM t")
	waitNotLoading(t, store)

	rec, _, numRows, loading, _, summary, executed, err := store.Snapshot()
	if rec != nil {
		defer rec.Release()
	}
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if loading {
		t.Error("loading should be false after finish")
	}
	if numRows != 2 || rec == nil {
		t.Fatalf("numRows=%d rec=%v, want 2 rows", numRows, rec)
	}
	if summary.ReadRows != 2 {
		t.Errorf("summary.ReadRows=%d, want 2", summary.ReadRows)
	}
	if executed.IsZero() {
		t.Error("executed timestamp should be set")
	}
	hist := store.History()
	if len(hist) != 1 || hist[0].NumRows != 2 || hist[0].ErrorText != "" {
		t.Errorf("history=%+v, want one 2-row entry with no error", hist)
	}
}

// A zero-batch (schema-only) stream keeps its schema, so "ran, empty" stays
// distinguishable from "no result" downstream (review finding: the schema was
// dropped and empty results lost their column shape everywhere).
func TestQueryStoreZeroBatchKeepsSchema(t *testing.T) {
	stream := schemaOnlyStreamBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(stream)
	}))
	defer srv.Close()

	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 100)
	store.Execute("SELECT n FROM t WHERE 0")
	waitNotLoading(t, store)

	rec, schema, numRows, _, _, _, _, err := store.Snapshot()
	if rec != nil {
		rec.Release()
		t.Error("rec should be nil for a zero-batch result")
	}
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if schema == nil {
		t.Fatal("schema must survive an empty result")
	}
	if got := schema.Field(0).Name; got != "n" {
		t.Errorf("schema field=%q, want n", got)
	}
	if numRows != 0 {
		t.Errorf("numRows=%d, want 0", numRows)
	}
}

func TestQueryStoreErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 100)
	store.Execute("SELECT bad")
	waitNotLoading(t, store)

	rec, _, _, _, _, _, _, err := store.Snapshot()
	if rec != nil {
		rec.Release()
		t.Error("rec should be nil on error")
	}
	if err == nil {
		t.Fatal("expected an error")
	}
	hist := store.History()
	if len(hist) != 1 || hist[0].ErrorText == "" {
		t.Errorf("history=%+v, want one entry with ErrorText", hist)
	}
}

// finish() trims history to maxHist; a session that runs more queries than the
// cap keeps only the most recent.
func TestQueryStoreHistoryCap(t *testing.T) {
	stream := arrowStreamBytes(t, []int64{1})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(stream)
	}))
	defer srv.Close()

	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 2)
	for range 4 {
		store.Execute("SELECT 1")
		waitNotLoading(t, store)
	}
	if got := len(store.History()); got != 2 {
		t.Errorf("history len=%d, want 2 (capped at maxHist)", got)
	}
}
