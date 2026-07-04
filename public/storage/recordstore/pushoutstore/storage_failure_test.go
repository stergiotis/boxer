package pushoutstore

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// flakyExecutor fails the failNth-th InsertArrow call (1-based), then
// delegates — the write-side fault injection for the adapter contract.
type flakyExecutor struct {
	inner   recordstore.ExecutorI
	failNth int
	inserts int
}

func (inst *flakyExecutor) Exec(ctx context.Context, sql string) error {
	return inst.inner.Exec(ctx, sql)
}

func (inst *flakyExecutor) QueryArrow(ctx context.Context, sql string) ([]arrow.RecordBatch, error) {
	return inst.inner.QueryArrow(ctx, sql)
}

func (inst *flakyExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error {
	inst.inserts++
	if inst.inserts == inst.failNth {
		return errors.New("synthetic insert failure")
	}
	return inst.inner.InsertArrow(ctx, table, records)
}

func ph(b byte) (out types.PatchHash) {
	for i := range out {
		out[i] = b
	}
	return
}

// TestAppendAppliedFailureLeavesOrderUnambiguous pins the sequence
// contract across a failed operation: the failure burns a sequence
// number, and neither LoadApplied nor a reopen may rewind onto it — no
// two log rows may ever share an order ts (a count-derived sequence did
// exactly that, making the replay order ambiguous).
func TestAppendAppliedFailureLeavesOrderUnambiguous(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	flaky := &flakyExecutor{inner: local, failNth: 2}
	stor, err := Open(ctx, flaky, nil, PushoutStoreConfig{}, PushoutCacheConfig{Capacity: 8})
	require.NoError(t, err)

	require.NoError(t, stor.AppendApplied(ctx, ph(1)))
	require.Error(t, stor.AppendApplied(ctx, ph(2)), "the injected failure — op never happened")
	require.NoError(t, stor.AppendApplied(ctx, ph(3)))

	// LoadApplied must not rewind the sequence onto the burned number.
	hs, err := stor.LoadApplied(ctx)
	require.NoError(t, err)
	require.Equal(t, []types.PatchHash{ph(1), ph(3)}, hs)
	require.NoError(t, stor.AppendApplied(ctx, ph(4)))

	// A reopen re-derives the sequence from storage; it must not collide
	// either.
	stor2, err := Open(ctx, flaky, nil, PushoutStoreConfig{}, PushoutCacheConfig{Capacity: 8})
	require.NoError(t, err)
	require.NoError(t, stor2.AppendApplied(ctx, ph(5)))

	seen := map[int64]bool{}
	for r, rerr := range stor2.st.Replay(ctx, logKey, recordstore.SeqTs(0)) {
		require.NoError(t, rerr)
		ns := r.Ts.UnixNano()
		require.False(t, seen[ns], "two log rows share order ts=%d — replay order ambiguous", ns)
		seen[ns] = true
	}

	hs, err = stor2.LoadApplied(ctx)
	require.NoError(t, err)
	require.Equal(t, []types.PatchHash{ph(1), ph(3), ph(4), ph(5)}, hs)
}
