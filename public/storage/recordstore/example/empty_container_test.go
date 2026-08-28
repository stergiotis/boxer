package example

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/stergiotis/boxer/public/storage/recordstore/example/internal/lowlevel"
	"github.com/stergiotis/boxer/public/storage/recordstore/ipcexec"
	"github.com/stretchr/testify/require"
)

// TestEmptyContainerHasNoPresence pins a consequence of leeway's splice
// semantics (ADR-0146 M1): a container field writes zero attributes when it
// is empty, so a kind whose memberships are all containers has no presence
// signal on a row where every container is empty — it reads back absent,
// and its archetype does not list it. A kind that must distinguish "present
// with nothing" from "absent" needs a scalar membership. The store round-trips
// through the IPC stream executor, so no ClickHouse is involved.
func TestEmptyContainerHasNoPresence(t *testing.T) {
	var buf bytes.Buffer
	exec := ipcexec.NewStreamExecutor(&buf, lowlevel.CreateSchemaDeviceTable(), nil)
	st := NewDeviceStore(exec, nil, DeviceStoreConfig{})
	t0 := time.Unix(1_600_000_000, 0).UTC()
	require.NoError(t, st.Begin(1, t0).AddIdentity(Identity{ID: 1, Status: "IDLE"}).AddTagged(Tagged{ID: 1}).Commit())
	require.NoError(t, st.Begin(2, t0).AddIdentity(Identity{ID: 2, Status: "IDLE"}).AddTagged(Tagged{ID: 2, Tags: []string{"x"}}).Commit())
	_, err := st.Flush(context.Background())
	require.NoError(t, err)
	st.Close()
	require.NoError(t, exec.Close())

	r, err := ipc.NewReader(&buf)
	require.NoError(t, err)
	defer r.Release()
	var ents []*DeviceEntity
	for r.Next() {
		batch, e := decodeDeviceRecord(r.RecordBatch())
		require.NoError(t, e)
		ents = append(ents, batch...)
	}
	require.NoError(t, r.Err())
	require.Len(t, ents, 2)
	byID := map[uint64]*DeviceEntity{}
	for _, e := range ents {
		byID[e.ID] = e
	}
	require.False(t, byID[1].Tagged.Has, "an all-container kind with every container empty reads back absent")
	require.Equal(t, []string{"identity"}, byID[1].Archetype())
	require.True(t, byID[2].Tagged.Has)
	require.Equal(t, []string{"identity", "tagged"}, byID[2].Archetype())
}
