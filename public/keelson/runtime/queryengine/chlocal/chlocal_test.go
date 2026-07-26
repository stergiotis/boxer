package chlocal

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// The engine is exercised against a real broker over a real in-process bus.
// A test double would only prove the adapter agrees with itself; what is
// worth knowing is that a one-shot worker's whole-result reply really does
// arrive as [Data, Terminal], and that a worker's failure really does become
// a failed terminal rather than an empty success.

const testPool = "scratchpad"

// arrowFileWithRows builds a single-column Arrow IPC file (footer form —
// the `Arrow` format, not ArrowStream) to bind as an input table.
func arrowFileWithRows(t *testing.T, rows int32) (b []byte) {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int32}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	for i := range rows {
		rb.Field(0).(*array.Int32Builder).Append(i)
	}
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w, err := ipc.NewFileWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(memory.DefaultAllocator))
	require.NoError(t, err)
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	b = buf.Bytes()
	return
}

func requireBinary(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed at %s: %v", chlocalpool.DefaultBinaryPath, err)
	}
}

func newTestEngine(t *testing.T) (eng *Engine) {
	t.Helper()
	requireBinary(t)
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(15 * time.Second)

	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir:          t.TempDir(),
		MinIdle:             1,
		MaxConcurrent:       2,
		SpawnConcurrency:    1,
		MaxMemoryPerWorker:  256 << 20,
		SpawnTimeout:        5 * time.Second,
		WatchdogMaxLifetime: 60 * time.Second,
		KillGrace:           250 * time.Millisecond,
		StderrCapBytes:      4096,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})

	caller := bus.NewClient("test.caller", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	eng, err = New(Config{Bus: caller, PoolName: testPool})
	require.NoError(t, err)
	return
}

func TestNewRequiresBus(t *testing.T) {
	t.Parallel()
	_, err := New(Config{})
	assert.Error(t, err)
}

func TestEngineImplementsOnlyDelivery(t *testing.T) {
	t.Parallel()
	// The absence is the design: a one-shot worker has no process list to
	// observe and nothing to kill, and saying so by not implementing the
	// interfaces makes it a fact about the type rather than a runtime
	// "unsupported" every consumer has to handle.
	var eng any = &Engine{}
	_, isDelivery := eng.(queryengine.DeliveryI)
	assert.True(t, isDelivery)
	_, isObservation := eng.(queryengine.ObservationI)
	assert.False(t, isObservation, "there is no system.processes to poll")
	_, isControl := eng.(queryengine.ControlI)
	assert.False(t, isControl, "a worker that already exited cannot be killed")
}

func TestDeliverYieldsOneDataFrameThenTerminal(t *testing.T) {
	eng := newTestEngine(t)

	st, res, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL:    "SELECT 1",
		Format: "TabSeparated",
		RunID:  "test-main-box-1-1",
	})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	var kinds []runstream.KindE
	var col runstream.Collector[[]byte]
	for {
		f, ok := st.Next()
		if !ok {
			break
		}
		kinds = append(kinds, f.Kind)
		require.NoError(t, col.Push(f))
	}
	// One data frame, not an artificial chunking: the result arrived whole
	// in one bus reply, and that is what a buffered engine has to say.
	assert.Equal(t, []runstream.KindE{runstream.KindData, runstream.KindTerminal}, kinds)

	term, err := col.Terminal()
	require.NoError(t, err)
	assert.Equal(t, runstream.TerminalComplete, term.State)
	assert.Equal(t, "1\n", string(col.Data()[0]))
	assert.Equal(t, "text/tab-separated-values", res.ContentType)
	assert.Greater(t, res.Summary.ElapsedNs, uint64(0))
}

func TestDeliverBindsParams(t *testing.T) {
	eng := newTestEngine(t)

	st, _, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL:    "SELECT {n:UInt64}",
		Format: "TabSeparated",
		// Bare names: the engine applies its own prefixing, so a caller
		// never spells param_ twice.
		Params: map[string]string{"n": "42"},
	})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	body, term, err := queryengine.Collect(st)
	require.NoError(t, err)
	require.Equal(t, runstream.TerminalComplete, term.State, "%s", string(body))
	assert.Equal(t, "42\n", string(body))
}

func TestDeliverBindsInputTables(t *testing.T) {
	eng := newTestEngine(t)

	st, _, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL:    "SELECT count() FROM t",
		Format: "TabSeparated",
		Inputs: map[string][]byte{"t": arrowFileWithRows(t, 3)},
	})
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	body, term, err := queryengine.Collect(st)
	require.NoError(t, err)
	require.Equal(t, runstream.TerminalComplete, term.State, "%s", string(body))
	assert.Equal(t, "3\n", string(body))
}

func TestDeliverBadStatementIsAFailedTerminal(t *testing.T) {
	eng := newTestEngine(t)

	st, _, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL:    "SELECT nonexistent_column_xyz FROM system.one",
		Format: "TabSeparated",
	})
	// A statement the engine refused is an outcome of the run, so it lands
	// in the terminal rather than as an error from the submission.
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	_, term, cErr := queryengine.Collect(st)
	require.NoError(t, cErr)
	assert.Equal(t, runstream.TerminalFailed, term.State)
	require.Error(t, term.Err)
}

func TestDeliverRefusesAnUnknownExtension(t *testing.T) {
	t.Parallel()
	eng := &Engine{}
	_, _, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL:   "SELECT 1",
		Extra: struct{ Foreign string }{"x"},
	})
	// An extension silently dropped becomes a query failing later against a
	// table the engine never received, with nothing on the wire saying why.
	assert.Error(t, err)
}

func TestDeliverValidatesTheRunId(t *testing.T) {
	t.Parallel()
	eng := &Engine{}
	_, _, err := eng.Deliver(context.Background(), queryengine.Request{
		SQL:   "SELECT 1",
		RunID: "bad'id",
	})
	assert.Error(t, err)
}

func TestUnjudgeableCapIsReportedAsPossiblyAPrefix(t *testing.T) {
	t.Parallel()
	// This engine reports no result row count, so it cannot tell a capped
	// result from a whole one. Saying "complete" would be the exact mistake
	// R9 exists to prevent, so the ambiguity is reported loudly instead.
	assert.Equal(t, runstream.TerminalComplete, terminalFor(queryengine.RowCap{}).State)
	assert.Equal(t, runstream.TerminalComplete,
		terminalFor(queryengine.RowCap{MaxResultRows: 10}).State,
		"without break mode the engine raises instead of truncating")

	term := terminalFor(queryengine.RowCap{MaxResultRows: 10, Breaks: true})
	assert.Equal(t, runstream.TerminalTruncated, term.State)
	assert.Contains(t, term.Reason, "may be a prefix")
}
