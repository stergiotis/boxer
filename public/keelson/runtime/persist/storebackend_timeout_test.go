package persist

import (
	"context"
	"iter"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// hangingExecutor answers through inner until hang is set; then every call
// waits for its context, as a server that stopped answering does.
type hangingExecutor struct {
	inner recordstore.ExecutorI
	hang  atomic.Bool
}

func (inst *hangingExecutor) Exec(ctx context.Context, sql string) error {
	if inst.hang.Load() {
		<-ctx.Done()
		return ctx.Err()
	}
	return inst.inner.Exec(ctx, sql)
}

func (inst *hangingExecutor) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	if inst.hang.Load() {
		return func(yield func(arrow.RecordBatch, error) bool) {
			<-ctx.Done()
			yield(nil, ctx.Err())
		}
	}
	return inst.inner.QueryArrow(ctx, sql)
}

func (inst *hangingExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error {
	_, err := inst.InsertArrowWith(ctx, table, records, recordstore.InsertSettings{})
	return err
}

// InsertArrowWith passes the store's per-insert settings through, so its
// deduplication token reaches the executor (ADR-0387).
func (inst *hangingExecutor) InsertArrowWith(ctx context.Context, table string, records []arrow.RecordBatch, settings recordstore.InsertSettings) (bool, error) {
	if inst.hang.Load() {
		<-ctx.Done()
		return true, ctx.Err()
	}
	return recordstore.InsertArrowWith(ctx, inst.inner, table, records, settings)
}

var _ recordstore.InsertSettingsI = (*hangingExecutor)(nil)

// A server that stops answering costs a call CallTimeout, then an error,
// and the mutex is free for the next call.
func TestStoreBackend_ACallAgainstAHungServerIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out CallTimeout")
	}
	inner, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	exec := &hangingExecutor{inner: inner}
	b, err := OpenStoreBackend(context.Background(), exec, nil)
	require.NoError(t, err)
	t.Cleanup(b.Close)

	exec.hang.Store(true)
	ref := StorageRef{Alias: "a", AppId: "test.app"}
	t0 := time.Now()
	err = b.Set(ref, "k", []byte("v"))
	require.Error(t, err)
	assert.Less(t, time.Since(t0), CallTimeout+5*time.Second)

	exec.hang.Store(false)
	require.NoError(t, b.Set(ref, "k", []byte("w")), "the backend answers again once the server does")
}
