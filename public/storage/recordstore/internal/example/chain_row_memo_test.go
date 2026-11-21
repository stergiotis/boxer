package example

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// A store keeps the read access and the encoder its drains use, so a
// second drain allocates far less than the first; the items are the same.
func TestDrainCanonicalKeepsItsReadAccess(t *testing.T) {
	st := NewRevStore(nil, nil, RevStoreConfig{})
	defer st.Close()
	ent := &RevEntity{ID: "r1", Ord: 1}
	ent.RevNote.Has, ent.RevNote.Val = true, RevNote{Text: "a"}
	first, err := st.CanonicalItem(ent)
	require.NoError(t, err)
	require.NotNil(t, st.scratch, "CanonicalItem drains a scratch store")
	require.NotNil(t, st.scratch.canonAcc, "whose drain keeps its table")
	again, err := st.CanonicalItem(ent)
	require.NoError(t, err)
	require.Equal(t, first, again)

	require.NoError(t, st.Begin("r2", 1, RevEnvelope{}).AddRevNote(RevNote{Text: "b"}).Commit())
	var drained bytes.Buffer
	n, err := st.DrainCanonical(&drained)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.NotNil(t, st.canonAcc, "a store's own drain keeps its table too")
	st.Close()
	require.Nil(t, st.canonAcc, "Close releases it")
	require.Nil(t, st.scratch)
}
