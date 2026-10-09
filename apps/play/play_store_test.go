package play

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
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

	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 100, "test")
	store.Execute("SELECT n FROM t", nil, "")
	waitNotLoading(t, store)

	rec, _, numRows, loading, _, summary, executed, err, _ := store.Snapshot()
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
		t.Fatalf("history=%+v, want one 2-row entry with no error", hist)
	}
	// The entry carries what the History tab's detail shows: the server's
	// accounting, where the run went, and the class of what ran.
	e := hist[0]
	if e.Summary.ReadRows != 2 || e.Summary.ReadBytes != 16 {
		t.Errorf("entry summary=%+v, want read 2 rows / 16 bytes", e.Summary)
	}
	if e.Dispatch == "" {
		t.Error("entry dispatch should describe the decision")
	}
	if !e.Security.known || e.Security.class != analysis.QuerySecurityRead {
		t.Errorf("entry security=%+v, want a known read", e.Security)
	}
	if e.Agent != nil {
		t.Errorf("entry agent=%+v, want nil for the person's run", e.Agent)
	}
}

// classifySecurity fails closed: a statement that does not parse is unknown
// and holds the strongest class.
func TestClassifySecurity(t *testing.T) {
	v, pr := classifySecurity("SELECT 1", nil)
	if pr == nil || !v.known || v.class != analysis.QuerySecurityRead {
		t.Errorf("SELECT 1: verdict=%+v pr=%v, want a known read", v, pr)
	}
	v, _ = classifySecurity("SELECT * FROM url('http://example.invalid/x', CSV)", nil)
	if !v.known || v.class != analysis.QuerySecurityReadEgress || len(v.witnesses) == 0 {
		t.Errorf("url(): verdict=%+v, want read-egress with a witness", v)
	}
	v, pr = classifySecurity("SELEC nope (", nil)
	if pr != nil || v.known || v.class != analysis.QuerySecurityMutating {
		t.Errorf("unparseable: verdict=%+v pr=%v, want unknown mutating", v, pr)
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

	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 100, "test")
	store.Execute("SELECT n FROM t WHERE 0", nil, "")
	waitNotLoading(t, store)

	rec, schema, numRows, _, _, _, _, err, _ := store.Snapshot()
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

	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 100, "test")
	store.Execute("SELECT bad", nil, "")
	waitNotLoading(t, store)

	rec, _, _, _, _, _, _, err, _ := store.Snapshot()
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

// Close cancels the in-flight run and marks the store closed: a late finish()
// must not resurrect a result on a torn-down store (Unmount teardown, review
// finding: nothing closed the stores/lanes at all). Idempotent.
func TestQueryStoreCloseDropsLateFinish(t *testing.T) {
	gate := make(chan struct{})
	stream := arrowStreamBytes(t, []int64{1})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-gate
		_, _ = w.Write(stream)
	}))

	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 10, "test")
	store.Execute("SELECT n FROM t", nil, "")
	store.Close() // torn down while the request is in flight
	close(gate)   // let the handler answer; the goroutine's finish is late
	waitNotLoading(t, store)
	srv.Close()

	rec, schema, _, _, _, _, _, _, _ := store.Snapshot()
	if rec != nil {
		rec.Release()
		t.Error("a closed store must not resurrect a late result")
	}
	if schema != nil {
		t.Error("a closed store must not hold a schema")
	}
	store.Close() // idempotent
}

// finish() trims history to maxHist; a session that runs more queries than the
// cap keeps only the most recent.
func TestQueryStoreHistoryCap(t *testing.T) {
	stream := arrowStreamBytes(t, []int64{1})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(stream)
	}))
	defer srv.Close()

	store := NewQueryStore(NewClient(ClientConfig{URL: srv.URL}, srv.Client()), memory.NewGoAllocator(), 2, "test")
	for range 4 {
		store.Execute("SELECT 1", nil, "")
		waitNotLoading(t, store)
	}
	if got := len(store.History()); got != 2 {
		t.Errorf("history len=%d, want 2 (capped at maxHist)", got)
	}
}

// A detail draws a statement up to historySqlShowBytes, cut at a line
// break near the cap where there is one, never inside a rune.
func TestHistorySqlCut(t *testing.T) {
	short := "SELECT 1"
	if got, cut := historySqlCut(short); cut || got != short {
		t.Errorf("short: got %q cut=%v", got, cut)
	}
	lines := strings.Repeat("SELECT 1 UNION ALL\n", historySqlShowBytes/10)
	got, cut := historySqlCut(lines)
	if !cut || len(got) > historySqlShowBytes || len(got) < historySqlShowBytes*3/4 || !strings.HasSuffix(got, "ALL") {
		t.Errorf("lines: len=%d cut=%v suffix=%q", len(got), cut, got[len(got)-3:])
	}
	runes := strings.Repeat("ä", historySqlShowBytes)
	got, cut = historySqlCut(runes)
	if !cut || !utf8.ValidString(got) {
		t.Errorf("runes: cut=%v valid=%v", cut, utf8.ValidString(got))
	}
}

// The History tab holds one open run across both halves: opening a
// recorded run closes a session run and the reverse; a second click closes.
func TestHistoryOpenIsExclusive(t *testing.T) {
	var o historyOpen
	at := time.Unix(100, 0)
	o.toggle(historyOpen{executed: at}, false)
	if !o.executed.Equal(at) || o.fact != 0 {
		t.Fatalf("session open: %+v", o)
	}
	o.toggle(historyOpen{fact: 7}, false)
	if !o.executed.IsZero() || o.fact != 7 {
		t.Fatalf("recorded open should close the session run: %+v", o)
	}
	o.toggle(historyOpen{fact: 7}, true)
	if o != (historyOpen{}) {
		t.Fatalf("second click should close: %+v", o)
	}
}
