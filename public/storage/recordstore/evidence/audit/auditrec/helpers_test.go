package auditrec_test

import (
	"context"
	"errors"
	"iter"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec/auditrectest"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec/testdata/example"
)

// gateExec is a server that takes rows only while open. With lostAcks
// set it stores the next inserts and still reports failure, as a lost
// acknowledgement does; inner, when set, is where the rows really go.
type gateExec struct {
	mu       sync.Mutex
	open     bool
	lostAcks int
	rows     int64
	inner    recordstore.ExecutorI
}

func (inst *gateExec) Exec(ctx context.Context, sql string) error {
	if inst.inner != nil {
		return inst.inner.Exec(ctx, sql)
	}
	return nil
}

func (inst *gateExec) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	if inst.inner != nil {
		return inst.inner.QueryArrow(ctx, sql)
	}
	return func(func(arrow.RecordBatch, error) bool) {}
}

func (inst *gateExec) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if !inst.open {
		return errors.New("server down")
	}
	if inst.inner != nil {
		if err := inst.inner.InsertArrow(ctx, table, records); err != nil {
			return err
		}
	}
	for _, r := range records {
		inst.rows += r.NumRows()
	}
	if inst.lostAcks > 0 {
		inst.lostAcks--
		return errors.New("acknowledgement lost")
	}
	return nil
}

func (inst *gateExec) set(open bool) {
	inst.mu.Lock()
	inst.open = open
	inst.mu.Unlock()
}

func (inst *gateExec) landed() int64 {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.rows
}

// exampleStore binds the example store over exec.
func exampleStore(t *testing.T, exec recordstore.ExecutorI) *example.Store {
	t.Helper()
	st, err := example.NewStore(exec, "")
	require.NoError(t, err)
	return st
}

// newRecorder is a recorder over the example store on exec, flushing only
// when asked.
func newRecorder(t *testing.T, exec recordstore.ExecutorI, opts ...auditrec.Option) *auditrec.Recorder[*example.Batch] {
	t.Helper()
	opts = append([]auditrec.Option{auditrec.WithFlushInterval(0)}, opts...)
	rec, err := auditrec.New[*example.Batch](exampleStore(t, exec), "run-1", auditrectest.Scheme, zerolog.Nop(), opts...)
	require.NoError(t, err)
	return rec
}

// localExec is clickhouse-local with the example table provisioned, or a
// skip.
func localExec(t *testing.T) (exec *chexec.LocalExecutor) {
	t.Helper()
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	st := exampleStore(t, exec)
	defer st.Close()
	require.NoError(t, st.EnsureTable(context.Background()))
	return exec
}
