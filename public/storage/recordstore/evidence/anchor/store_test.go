package anchor_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// The memory store is keyed by (cell, scope, ordinal, scheme): the same
// head again is its first receipt, another digest there is refused, and
// another scheme at the ordinal is another anchor.
func TestTheMemoryStoreKeysByScopeOrdinalAndScheme(t *testing.T) {
	ctx := context.Background()
	st := anchor.NewMemStore()
	h := anchor.Head{Cell: "c", Scope: 11, Ordinal: 3, Scheme: "s/v1", Digest: "abc"}
	r, err := st.Append(ctx, h)
	require.NoError(t, err)
	assert.NotEmpty(t, r.Id)
	assert.False(t, r.Durable)
	same, err := st.AppendBy(h, "app.chat")
	require.NoError(t, err)
	assert.Equal(t, r, same, "the first receipt counts, whoever sends it again")
	_, err = st.Append(ctx, anchor.Head{Cell: "c", Scope: 11, Ordinal: 3, Scheme: "s/v1", Digest: "xyz"})
	assert.Error(t, err)
	other, err := st.AppendBy(anchor.Head{Cell: "c", Scope: 11, Ordinal: 3, Scheme: "s/v2", Digest: "xyz"}, "app.chat")
	require.NoError(t, err)
	assert.Equal(t, "app.chat", other.By)

	rows, err := st.Read(ctx, "c", 11)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	heads := anchor.HeadsOf(rows, "s/v1")
	require.Len(t, heads, 1)
	assert.Equal(t, uint64(3), heads[0].Ordinal)
	assert.Equal(t, "abc", heads[0].Digest)
	assert.Equal(t, r.Id, heads[0].ReceiptId)
	rows, err = st.Read(ctx, "c", 12)
	require.NoError(t, err)
	assert.Empty(t, rows)
	assert.Len(t, st.All(), 2)

	assert.Equal(t, "turn,close", anchor.DefaultCadence().String())
	assert.Equal(t, "never", anchor.Cadence{}.String())
	assert.Equal(t, "sync", anchor.Cadence{PerSync: true}.String())
}
