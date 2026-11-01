package trail

import (
	"context"
	"errors"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gateExec is a server that takes rows only while open, and can hold an
// insert until released.
type gateExec struct {
	mu      sync.Mutex
	open    bool
	rows    int64
	held    chan struct{}
	entered chan struct{}
}

func (inst *gateExec) Exec(context.Context, string) error { return nil }

func (inst *gateExec) QueryArrow(context.Context, string) iter.Seq2[arrow.RecordBatch, error] {
	return func(func(arrow.RecordBatch, error) bool) {}
}

func (inst *gateExec) InsertArrow(ctx context.Context, _ string, records []arrow.RecordBatch) error {
	if inst.held != nil {
		inst.entered <- struct{}{}
		<-inst.held
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if !inst.open {
		return errors.New("server down")
	}
	for _, r := range records {
		inst.rows += r.NumRows()
	}
	return nil
}

func (inst *gateExec) landed() (n int64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.rows
}

// A flush waiting on the server does not hold up writers: rows keep being
// buffered while the insert is outstanding.
func TestFlushDoesNotBlockWriters(t *testing.T) {
	exec := &gateExec{open: true, held: make(chan struct{}), entered: make(chan struct{}, 1)}
	rec := NewRecorder(exec, "run-1", zerolog.Nop())
	require.NoError(t, rec.LlmCall(time.Now(), Context{}, LlmCall{CallId: "c1"}))
	flushed := make(chan error, 1)
	go func() { flushed <- rec.Flush(context.Background()) }()
	<-exec.entered
	wrote := make(chan error, 1)
	go func() { wrote <- rec.LlmCall(time.Now(), Context{}, LlmCall{CallId: "c2"}) }()
	select {
	case err := <-wrote:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("a write waited on the flush's insert")
	}
	close(exec.held)
	require.NoError(t, <-flushed)
	assert.EqualValues(t, 2, exec.landed(), "the call and the batch's seal")
	exec.held = nil
	rec.Close()
	assert.EqualValues(t, 4, exec.landed(), "Close lands what was written during the flush, sealed")
}

// Rows a down server did not take are held and land, in order, with the
// next flush that reaches it.
func TestHeldRowsLandOnRecovery(t *testing.T) {
	exec := &gateExec{}
	rec := NewRecorder(exec, "run-1", zerolog.Nop())
	defer rec.Close()
	require.NoError(t, rec.LlmCall(time.Now(), Context{}, LlmCall{CallId: "c1"}))
	require.Error(t, rec.Flush(context.Background()))
	require.NoError(t, rec.LlmCall(time.Now(), Context{}, LlmCall{CallId: "c2"}))
	require.Error(t, rec.Flush(context.Background()))
	exec.mu.Lock()
	exec.open = true
	exec.mu.Unlock()
	require.NoError(t, rec.Flush(context.Background()))
	assert.EqualValues(t, 4, exec.landed(), "two held batches, each with its seal")
}

// A server that stays down does not grow the recorder without bound.
func TestBacklogIsBounded(t *testing.T) {
	exec := &gateExec{}
	const maxBacklog, batch = 100, 10
	rec := NewRecorder(exec, "run-1", zerolog.Nop(), WithMaxBacklogForTest(maxBacklog))
	defer rec.Close()
	for i := 0; i < 2*maxBacklog/batch; i++ {
		for j := 0; j < batch; j++ {
			require.NoError(t, rec.HttpFetch(time.Now(), Context{}, HttpFetch{}))
		}
		require.Error(t, rec.Flush(context.Background()))
	}
	c := rec.Counts()
	require.Zero(t, c.Written)
	held := int(c.Buffered - c.Dropped)
	assert.LessOrEqual(t, held, maxBacklog)
	assert.Greater(t, held, maxBacklog-batch)
}
