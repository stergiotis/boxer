package storeexec

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// arrowStreamFixture renders nBatches single-column batches as the
// ArrowStream bytes ClickHouse answers `FORMAT ArrowStream` with. Values are
// batch index * 10, so a decoded batch identifies itself.
func arrowStreamFixture(t *testing.T, nBatches int) []byte {
	t.Helper()
	alloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(alloc))
	for i := range nBatches {
		b := array.NewInt64Builder(alloc)
		b.Append(int64(i) * 10)
		arr := b.NewArray()
		rec := array.NewRecordBatch(schema, []arrow.Array{arr}, 1)
		require.NoError(t, w.Write(rec))
		rec.Release()
		arr.Release()
		b.Release()
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// newTestExecutor stands up an httptest server running handler and returns an
// Executor pointed at it, plus the checked allocator the decode path uses so
// a test can assert the ownership contract left nothing behind.
func newTestExecutor(t *testing.T, handler http.HandlerFunc) (*Executor, *memory.CheckedAllocator) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	exec, err := New(chclient.New(chclient.Config{URL: srv.URL + "/"}, nil), alloc)
	require.NoError(t, err)
	return exec, alloc
}

func TestWithArrowStreamFormat(t *testing.T) {
	// The generated stores end their statements at a SETTINGS clause, and
	// ClickHouse's grammar puts FORMAT after SETTINGS — this is the shape
	// that actually goes on the wire.
	assert.Equal(t,
		"SELECT 1 SETTINGS output_format_arrow_string_as_string=1 FORMAT ArrowStream",
		withArrowStreamFormat("SELECT 1 SETTINGS output_format_arrow_string_as_string=1"))
	assert.Equal(t, "SELECT 1 FORMAT ArrowStream", withArrowStreamFormat("SELECT 1"))
	// A trailing semicolon is legal on a single statement but would leave the
	// clause past the end of it.
	assert.Equal(t, "SELECT 1 FORMAT ArrowStream", withArrowStreamFormat("SELECT 1;"))
	assert.Equal(t, "SELECT 1 FORMAT ArrowStream", withArrowStreamFormat("SELECT 1 ;\n"))
}

func TestQueryArrow_SendsFormatClause(t *testing.T) {
	var got string
	exec, _ := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = string(body)
		_, _ = w.Write(arrowStreamFixture(t, 1))
	})
	for rec, err := range exec.QueryArrow(context.Background(), "SELECT 1") {
		require.NoError(t, err)
		rec.Release()
	}
	assert.Equal(t, "SELECT 1 FORMAT ArrowStream", got)
}

func TestQueryArrow_YieldsEveryBatch(t *testing.T) {
	exec, alloc := newTestExecutor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(arrowStreamFixture(t, 3))
	})
	var values []int64
	for rec, err := range exec.QueryArrow(context.Background(), "SELECT v FROM t") {
		require.NoError(t, err)
		values = append(values, rec.Column(0).(*array.Int64).Value(0))
		rec.Release()
	}
	assert.Equal(t, []int64{0, 10, 20}, values)
	// Every batch the consumer received was released; nothing the reader held
	// back leaked either.
	alloc.AssertSize(t, 0)
}

func TestQueryArrow_BreakLeavesConsumerOwningTheLastBatch(t *testing.T) {
	exec, alloc := newTestExecutor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(arrowStreamFixture(t, 3))
	})
	var held arrow.RecordBatch
	for rec, err := range exec.QueryArrow(context.Background(), "SELECT v FROM t") {
		require.NoError(t, err)
		held = rec
		break
	}
	// The contract hands the broken-on batch to the consumer, so it must still
	// be readable after the sequence has torn down.
	require.NotNil(t, held)
	assert.EqualValues(t, 0, held.Column(0).(*array.Int64).Value(0))
	held.Release()
	alloc.AssertSize(t, 0)
}

func TestQueryArrow_EmptyBodyIsAnEmptySequence(t *testing.T) {
	exec, alloc := newTestExecutor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	n := 0
	for _, err := range exec.QueryArrow(context.Background(), "SELECT 1") {
		require.NoError(t, err)
		n++
	}
	assert.Zero(t, n, "a statement with no result set yields nothing rather than an EOF error")
	alloc.AssertSize(t, 0)
}

func TestQueryArrow_ServerErrorEndsTheSequence(t *testing.T) {
	exec, _ := newTestExecutor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "Code: 47. DB::Exception: Unknown expression identifier")
	})
	n := 0
	var last error
	for rec, err := range exec.QueryArrow(context.Background(), "SELECT nope") {
		assert.Nil(t, rec)
		last = err
		n++
	}
	require.Equal(t, 1, n, "an error ends the sequence as one final pair")
	require.Error(t, last)
	// ClickHouse's own diagnostic is the useful half of the message.
	assert.Contains(t, last.Error(), "Unknown expression identifier")
}

func TestQueryArrow_CorruptStreamSurfacesADecodeError(t *testing.T) {
	// The mid-result failure mode: a 200 is already sent, so the failure can
	// only show up as a broken Arrow stream.
	exec, _ := newTestExecutor(t, func(w http.ResponseWriter, _ *http.Request) {
		full := arrowStreamFixture(t, 2)
		_, _ = w.Write(full[:len(full)/2])
	})
	var last error
	for _, err := range exec.QueryArrow(context.Background(), "SELECT v FROM t") {
		if err != nil {
			last = err
		}
	}
	require.Error(t, last)
}

func TestExec_PropagatesServerError(t *testing.T) {
	exec, _ := newTestExecutor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "Code: 62. DB::Exception: Syntax error")
	})
	err := exec.Exec(context.Background(), "NOT SQL")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Syntax error")
}

func TestNewRejectsNilClient(t *testing.T) {
	_, err := New(nil, nil)
	require.Error(t, err)
}

// Each verb carries the context's identity as the log_comment URL setting
// (ADR-0295 §SD3); a bare context, or DisableStamp, carries none.
func TestExecutor_StampsLogComment(t *testing.T) {
	var got []url.Values
	handler := func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		got = append(got, r.URL.Query())
		if strings.Contains(r.URL.Query().Get("query"), "INSERT") {
			return
		}
		_, _ = w.Write(arrowStreamFixture(t, 1))
	}
	exec, alloc := newTestExecutor(t, handler)
	defer alloc.AssertSize(t, 0)

	ci := callident.CallIdentity{
		Origin: callident.Origin{Run: "r1", App: "example.grant"},
		Claims: callident.Claims{Principal: "p:1", Purpose: "audit"},
	}
	ctx := recordstore.WithBatchId(callident.WithCallIdentity(context.Background(), ci), "B9")
	want := logcomment.Stamp{RunId: "r1", App: "example.grant", Principal: "p:1", Purpose: "audit", Batch: "B9"}

	require.NoError(t, exec.Exec(ctx, "SELECT 1"))
	for rec, err := range exec.QueryArrow(ctx, "SELECT 1") {
		require.NoError(t, err)
		rec.Release()
	}
	rec := arrowRecord(t)
	require.NoError(t, exec.InsertArrow(ctx, "db.t", []arrow.RecordBatch{rec}))
	rec.Release()
	require.Len(t, got, 3)
	for i, q := range got {
		st, ok := logcomment.Parse(q.Get("log_comment"))
		require.True(t, ok, "call %d: %q", i, q.Get("log_comment"))
		assert.Equal(t, want, st, "call %d", i)
	}

	got = nil
	require.NoError(t, exec.Exec(context.Background(), "SELECT 1"))
	assert.False(t, got[0].Has("log_comment"), "nothing to stamp, no setting")

	got = nil
	quiet, err := NewWithOptions(exec.client, alloc, Options{DisableStamp: true})
	require.NoError(t, err)
	require.NoError(t, quiet.Exec(ctx, "SELECT 1"))
	assert.False(t, got[0].Has("log_comment"), "DisableStamp sends none")
}

func arrowRecord(t *testing.T) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	b := array.NewInt64Builder(alloc)
	defer b.Release()
	b.Append(1)
	arr := b.NewArray()
	defer arr.Release()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	return array.NewRecordBatch(schema, []arrow.Array{arr}, 1)
}

// An insert into a table a transform is registered for sends what the
// transform returns, to its table, with the insert's settings — the
// deduplication token among them (ADR-0402 §SD1, §SD2); another table's
// insert is sent as it is.
func TestExecutor_AppliesTheRegisteredTransform(t *testing.T) {
	type sent struct {
		query string
		token string
		cols  []string
	}
	var got []sent
	exec, alloc := newTestExecutor(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := sent{query: r.URL.Query().Get("query"), token: r.URL.Query().Get("insert_deduplication_token")}
		rdr, err := ipc.NewFileReader(bytes.NewReader(body))
		if err == nil {
			for _, f := range rdr.Schema().Fields() {
				s.cols = append(s.cols, f.Name)
			}
			_ = rdr.Close()
		}
		got = append(got, s)
	})
	defer alloc.AssertSize(t, 0)
	tAlloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer tAlloc.AssertSize(t, 0)

	unregister := recordstore.RegisterInsertTransform("scratch_storeexec.facts", recordstore.InsertTransformFunc(
		func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			b := array.NewInt64Builder(tAlloc)
			defer b.Release()
			b.Append(records[0].Column(0).(*array.Int64).Value(0))
			arr := b.NewArray()
			defer arr.Release()
			schema := arrow.NewSchema([]arrow.Field{{Name: "carried", Type: arrow.PrimitiveTypes.Int64}}, nil)
			return "scratch_storeexec.carrier", []arrow.RecordBatch{array.NewRecordBatch(schema, []arrow.Array{arr}, 1)}, nil
		}))
	defer unregister()

	rec := arrowRecord(t)
	defer rec.Release()
	_, err := exec.InsertArrowWith(context.Background(), "scratch_storeexec.facts", []arrow.RecordBatch{rec},
		recordstore.InsertSettings{}.WithDeduplicationToken("tok-7"))
	require.NoError(t, err)
	require.NoError(t, exec.InsertArrow(context.Background(), "scratch_storeexec.facts", []arrow.RecordBatch{rec}))
	require.NoError(t, exec.InsertArrow(context.Background(), "scratch_storeexec.other", []arrow.RecordBatch{rec}))

	require.Len(t, got, 3)
	assert.Equal(t, sent{query: "INSERT INTO scratch_storeexec.carrier (`carried`) FORMAT Arrow", token: "tok-7", cols: []string{"carried"}}, got[0])
	assert.Equal(t, sent{query: "INSERT INTO scratch_storeexec.carrier (`carried`) FORMAT Arrow", cols: []string{"carried"}}, got[1])
	assert.Equal(t, sent{query: "INSERT INTO scratch_storeexec.other (`v`) FORMAT Arrow", cols: []string{"v"}}, got[2])

	// Another spelling of the table is the same table; a bare name that
	// could be it sends nothing; an insert a transform already rewrote is
	// sent as it is.
	got = nil
	require.NoError(t, exec.InsertArrow(context.Background(), "`scratch_storeexec`.`facts`", []arrow.RecordBatch{rec}))
	err = exec.InsertArrow(context.Background(), "facts", []arrow.RecordBatch{rec})
	require.ErrorIs(t, err, recordstore.ErrUnqualifiedTransformedTable)
	require.NoError(t, exec.InsertArrow(recordstore.WithTransformApplied(context.Background()), "scratch_storeexec.facts", []arrow.RecordBatch{rec}))
	require.Len(t, got, 2)
	assert.Equal(t, sent{query: "INSERT INTO scratch_storeexec.carrier (`carried`) FORMAT Arrow", cols: []string{"carried"}}, got[0])
	assert.Equal(t, sent{query: "INSERT INTO scratch_storeexec.facts (`v`) FORMAT Arrow", cols: []string{"v"}}, got[1])
}
