package deadletter

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identgen/leased"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/memalloc"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/recordstoretest"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorefacts"
)

func TestNaturalKeyIsStable(t *testing.T) {
	a := NaturalKey("t", 3, 99)
	require.Equal(t, a, NaturalKey("t", 3, 99))
	require.NotEqual(t, a, NaturalKey("t", 3, 100))
	require.NotEqual(t, a, NaturalKey("u", 3, 99))
	require.Len(t, a, 32)
}

// The store mints a row's id from its key stream, whatever the row came
// with: a failure delivered twice is two rows under one natural key
// (ADR-0405), and a store built without the stream refuses.
func TestStoreMintsTheRowId(t *testing.T) {
	var ids []uint64
	exec := &recordstoretest.HookExecutor{
		OnInsertArrowWith: func(_ context.Context, _ string, records []arrow.RecordBatch, _ recordstore.InsertSettings) (bool, error) {
			for _, r := range records {
				col := r.Column(r.Schema().FieldIndices("id:id:u64:47::0:")[0]).(*array.Uint64)
				ids = append(ids, col.Uint64Values()...)
			}
			return true, nil
		},
	}
	const tag identifier.TagValue = 57314
	keys, err := leased.NewSequence(memalloc.NewAllocator(), tag, 8)
	require.NoError(t, err)
	st := stevedorefacts.NewStevedoreStore(exec, nil, stevedorefacts.StevedoreStoreConfig{Keys: keys})
	defer st.Close()
	dead := &Store{Store: st, Run: "run-1"}

	row := New(time.Unix(1_700_000_000, 0).UTC())
	row.NaturalKey = NaturalKey("t", 3, 99)
	row.Id = 12345 // ignored
	ctx := context.Background()
	require.NoError(t, dead.Add(ctx, row))
	require.NoError(t, dead.Add(ctx, row))
	require.NoError(t, dead.Flush(ctx))
	require.Len(t, ids, 2)
	require.NotEqual(t, ids[0], ids[1], "a redelivery is another row")
	for _, id := range ids {
		require.Equal(t, tag, identifier.TaggedId(id).GetTag().GetValue())
	}

	bare := stevedorefacts.NewStevedoreStore(exec, nil, stevedorefacts.StevedoreStoreConfig{})
	defer bare.Close()
	require.Error(t, (&Store{Store: bare}).Add(ctx, row), "without a key stream there is no id to give the row")
}
