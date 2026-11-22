package deadletter

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/require"

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

// The store resolves a row's id from its natural key, whatever the row came
// with: a failure delivered twice restates one id, another failure has
// another (ADR-0405), and a store built without the generator refuses.
func TestStoreResolvesTheRowIdFromItsNaturalKey(t *testing.T) {
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
	st := stevedorefacts.NewStevedoreStore(exec, nil, stevedorefacts.StevedoreStoreConfig{Keys: recordstoretest.KeyedStream(recordstoretest.FactsRowTag)})
	defer st.Close()
	dead := &Store{Store: st, Run: "run-1"}

	at := time.Unix(1_700_000_000, 0).UTC()
	row := New(at)
	row.NaturalKey = NaturalKey("t", 3, 99)
	row.Id = 12345 // ignored
	ctx := context.Background()
	require.NoError(t, dead.Add(ctx, row))
	redelivered := New(at.Add(time.Minute))
	redelivered.NaturalKey = NaturalKey("t", 3, 99)
	require.NoError(t, dead.Add(ctx, redelivered))
	another := New(at)
	another.NaturalKey = NaturalKey("t", 3, 100)
	require.NoError(t, dead.Add(ctx, another))
	require.NoError(t, dead.Flush(ctx))
	require.Len(t, ids, 3)
	require.Equal(t, ids[0], ids[1], "a redelivery restates the failure's id")
	require.NotEqual(t, ids[0], ids[2])
	for _, id := range ids {
		require.Equal(t, recordstoretest.FactsRowTag, identifier.TaggedId(id).GetTag().GetValue())
	}

	bare := stevedorefacts.NewStevedoreStore(exec, nil, stevedorefacts.StevedoreStoreConfig{})
	defer bare.Close()
	require.Error(t, (&Store{Store: bare}).Add(ctx, row), "without a generator there is no id to give the row")
}
