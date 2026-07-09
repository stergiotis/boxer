package example

import (
	"context"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestWidgetStoreEnvelopeRoundTrip is the ADR-0100 pass-through acceptance:
// write an entity carrying the full envelope (the second id, a routing scalar
// and a routing set), flush through clickhouse-local, read it back with Latest
// and check every envelope field survives — then exercise the state view
// (Delete writes a tombstone with a zero envelope; GetLive reads absent).
func TestWidgetStoreEnvelopeRoundTrip(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewWidgetStore(exec, nil, WidgetStoreConfig{})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))
	require.NoError(t, st.VerifySchema(ctx), "fresh table must match the generated schema")

	t0 := time.Unix(1_700_000_000, 0).UTC()
	env := WidgetEnvelope{Alt: 7, Region: 100, Tags: []string{"alpha", "beta"}}
	require.NoError(t, st.Begin(1, t0, env).Commit())
	n, err := st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	got, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(1), got.ID)
	require.True(t, got.Ts.Equal(t0), "Ts %v != %v", got.Ts, t0)
	require.False(t, got.IsTombstone())
	// Promoted pass-through fields (embedded WidgetEnvelope).
	require.Equal(t, uint64(7), got.Alt)
	require.Equal(t, uint64(100), got.Region)
	require.ElementsMatch(t, env.Tags, got.Tags) // tags is a set: order-independent

	// State view: Delete appends a tombstone (zero envelope), GetLive absent.
	t1 := t0.Add(time.Hour)
	require.NoError(t, st.Delete(1, t1))
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	_, live, err := st.GetLive(ctx, 1)
	require.NoError(t, err)
	require.False(t, live, "deleted key must read absent through GetLive")

	tomb, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, tomb.IsTombstone())
	require.Zero(t, tomb.Alt, "a tombstone carries a zero envelope")
	require.Empty(t, tomb.Tags)
}
