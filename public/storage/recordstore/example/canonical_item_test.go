package example

import (
	"bytes"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// CanonicalItem (gen.Input.CanonWire) is the canonical wire item of one
// entity as the store would write it: the same bytes DrainCanonical
// writes for the row the entity was decoded from, so a verifier reading
// rows back computes what the writer chained (ADR-0327 (proposed) §SD2).
func TestCanonicalItemIsTheDrainedRow(t *testing.T) {
	st := NewRevStore(nil, nil, RevStoreConfig{})
	defer st.Close()
	require.NoError(t, st.Begin("r1", 7, RevEnvelope{}).AddRevNote(RevNote{Text: "a"}).Commit())
	var drained bytes.Buffer
	n, err := st.DrainCanonical(&drained)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	ent := &RevEntity{ID: "r1", Ord: 7}
	ent.RevNote.Has, ent.RevNote.Val = true, RevNote{Text: "a"}
	item, err := st.CanonicalItem(ent)
	require.NoError(t, err)
	require.Equal(t, drained.Bytes(), item, "the entity's item is the row's")

	other := &RevEntity{ID: "r1", Ord: 7}
	other.RevNote.Has, other.RevNote.Val = true, RevNote{Text: "b"}
	changed, err := st.CanonicalItem(other)
	require.NoError(t, err)
	require.NotEqual(t, item, changed)

	bare := &RevEntity{ID: "r1", Ord: 7}
	empty, err := st.CanonicalItem(bare)
	require.NoError(t, err)
	require.NotEqual(t, item, empty, "a component present and absent are different items")
	require.Zero(t, st.Buffered(), "the store's own buffer is untouched")

	_, err = st.CanonicalItem(nil)
	require.Error(t, err)
}

func TestCanonicalItemIsSafeForConcurrentUse(t *testing.T) {
	st := NewRevStore(nil, nil, RevStoreConfig{})
	defer st.Close()
	ent := &RevEntity{ID: "r1", Ord: 1}
	ent.RevNote.Has, ent.RevNote.Val = true, RevNote{Text: "a"}
	want, err := st.CanonicalItem(ent)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				got, err := st.CanonicalItem(ent)
				if err != nil || !bytes.Equal(got, want) {
					t.Error("a concurrent CanonicalItem differed")
					return
				}
			}
		}()
	}
	wg.Wait()
}
