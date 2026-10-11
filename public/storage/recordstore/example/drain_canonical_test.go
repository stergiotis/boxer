package example

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
)

// DrainCanonical (gen.Input.CanonWire) writes one canonical wire item per
// buffered row and inserts nothing, so a store with no executor computes
// the canonical item of a row it never stores. The item is a function of
// the row: the same row drains to the same bytes, a different one does not.
func TestDrainCanonicalItemsAreTheRows(t *testing.T) {
	st := NewRevStore(nil, nil, RevStoreConfig{})
	defer st.Close()
	drain := func(rows ...RevNote) []byte {
		for i, r := range rows {
			require.NoError(t, st.Begin("r1", uint64(i+1), RevEnvelope{}).AddRevNote(r).Commit())
		}
		var buf bytes.Buffer
		n, err := st.DrainCanonical(&buf)
		require.NoError(t, err)
		require.Equal(t, len(rows), n)
		items, err := cwruntime.VerifyCanonicalSequence(buf.Bytes())
		require.NoError(t, err, "the bytes are canonical")
		require.Equal(t, len(rows), items)
		return buf.Bytes()
	}
	a := drain(RevNote{Text: "a"})
	assert.Equal(t, a, drain(RevNote{Text: "a"}), "the same row, the same item")
	assert.NotEqual(t, a, drain(RevNote{Text: "b"}))
	assert.Len(t, drain(RevNote{Text: "a"}, RevNote{Text: "b"}), len(a)*2, "one item per row")
	assert.Zero(t, st.Buffered(), "nothing stays buffered")
}

func BenchmarkDrainCanonicalOneRow(b *testing.B) {
	st := NewRevStore(nil, nil, RevStoreConfig{})
	defer st.Close()
	var buf bytes.Buffer
	for b.Loop() {
		buf.Reset()
		_ = st.Begin("r1", 1, RevEnvelope{}).AddRevNote(RevNote{Text: "a"}).Commit()
		_, _ = st.DrainCanonical(&buf)
	}
}
