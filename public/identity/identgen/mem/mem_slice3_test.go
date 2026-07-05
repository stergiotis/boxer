package mem

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

// TestAppendIds_ExactCapacityCheck pins the Badger-parity fix (ADR-0106 SD6):
// the old worst-case pre-check charged every batch key as fresh, so an
// all-duplicate or already-interned batch was rejected near exhaustion even
// though it needed no new ids.
func TestAppendIds_ExactCapacityCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("fills a 131071-id space")
	}
	const capacity = 1<<17 - 1 // widest uint32 tag: width 47, 17 body bits
	gen, err := NewIdInternalizer(identifier.TagValue(math.MaxUint32), capacity)
	require.NoError(t, err)

	var keys identgen.KeysColumn
	keys.Data = make([]byte, 0, capacity*8)
	keys.Ends = make([]uint32, 0, capacity)
	buf := make([]byte, 8)
	for i := 0; i < capacity; i++ {
		for j := range buf {
			buf[j] = byte(i >> (8 * j))
		}
		keys = keys.AppendKey(buf)
	}

	// Filling the space exactly succeeds, duplicates within the batch and
	// re-resolution afterwards cost nothing.
	ids, _, err := gen.AppendIds(nil, keys, nil)
	require.NoError(t, err)
	require.Len(t, ids, capacity)
	require.Equal(t, capacity, gen.Len())

	// All-existing batch at zero remaining: accepted (was ErrIdSpaceExhausted).
	again, _, err := gen.AppendIds(nil, keys, nil)
	require.NoError(t, err)
	require.Equal(t, ids, again)

	// One genuinely fresh key beyond capacity: rejected atomically.
	overflow := identgen.KeysColumn{}.AppendKey([]byte("fresh-overflow")).AppendKey([]byte("fresh-overflow"))
	_, _, err = gen.AppendIds(nil, overflow, nil)
	require.ErrorIs(t, err, identgen.ErrIdSpaceExhausted)
	require.Equal(t, capacity, gen.Len(), "nothing assigned by the rejected batch")

	// Existing keys still resolve via the single-key path after exhaustion.
	id0, fresh0, err := gen.GetId(keys.At(0))
	require.NoError(t, err)
	require.False(t, fresh0)
	require.Equal(t, ids[0], id0)
}

// TestAppendIds_DuplicatesChargedOnce: a batch whose duplicates would
// overflow under per-key charging fits when counted by distinct key.
func TestAppendIds_DuplicatesChargedOnce(t *testing.T) {
	gen, err := NewIdInternalizer(identifier.TagValue(math.MaxUint32), 8)
	require.NoError(t, err)
	const capacity = 1<<17 - 1

	var keys identgen.KeysColumn
	for i := 0; i < capacity+50; i++ { // 50 more entries than the space holds
		keys = keys.AppendKey([]byte{byte(i % 200)}) // but only 200 distinct keys
	}
	ids, fresh, err := gen.AppendIds(nil, keys, make([]bool, 0))
	require.NoError(t, err)
	require.Len(t, ids, capacity+50)
	require.Len(t, fresh, capacity+50)
	require.Equal(t, 200, gen.Len())
}
