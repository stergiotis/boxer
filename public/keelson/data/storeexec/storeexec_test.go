package storeexec

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
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
