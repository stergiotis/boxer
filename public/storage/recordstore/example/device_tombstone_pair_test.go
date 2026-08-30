package example

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestDeviceStoreCustomTombstonePair: on a schema WITH the u8 Lifecycle
// role, a configured tombstone pair overrides the default binding (ADR-0100
// Update 2026-08-30) — Delete composes the marker through TombstoneWrite on
// an ordinary builder frame (whose Lifecycle column reads LifecycleLive),
// and GetLive follows the configured detect, not Entity.IsTombstone.
func TestDeviceStoreCustomTombstonePair(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	const marker = "__tombstone__"
	st := NewDeviceStore(exec, nil, DeviceStoreConfig{
		TombstoneDetect: func(e *DeviceEntity) bool {
			return e.Tagged.Has && slices.Contains(e.Tagged.Val.Tags, marker)
		},
		TombstoneWrite: func(b *DeviceEntityBuilder) {
			b.AddTagged(Tagged{Tags: []string{marker}})
		},
	})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))

	t0 := time.Unix(1_700_000_000, 0).UTC()
	require.NoError(t, st.Begin(9, t0).AddIdentity(Identity{ID: 9, Status: "live"}).Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	require.NoError(t, st.Delete(9, t0.Add(time.Hour)))
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	_, live, err := st.GetLive(ctx, 9)
	require.NoError(t, err)
	require.False(t, live, "the configured detect must read the marker row as absent")

	tomb, found, err := st.Latest(ctx, 9)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, tomb.IsTombstone(),
		"under a configured pair the u8 column stays LifecycleLive — the pair is the marker")
	require.True(t, tomb.Tagged.Has && slices.Contains(tomb.Tagged.Val.Tags, marker))
}
