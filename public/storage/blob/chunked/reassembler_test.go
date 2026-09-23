package chunked

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/identity/identifier"
)

// fixedIdGen answers one id, the way a stevedore host hands the chunker an
// origin-derived reference rather than minting.
type fixedIdGen struct{ id identifier.TaggedId }

func (inst fixedIdGen) GetId(context.Context, []byte) (identifier.TaggedId, bool, error) {
	return inst.id, false, nil
}

func (inst fixedIdGen) GetUntaggedId(context.Context, []byte) (identifier.UntaggedId, bool, error) {
	return inst.id.RemoveTag(), false, nil
}

func (inst fixedIdGen) Release() error           { return nil }
func (inst fixedIdGen) GetTag() identifier.IdTag { return inst.id.GetTag() }

func tagged(body uint64) identifier.TaggedId {
	return identifier.TagValue(2178400).GetTag().ComposeId(identifier.UntaggedId(body))
}

// The writer and the reassembler agree: whatever the Chunker cuts, the
// Collector puts back, whatever the chunk size and the write sizes.
func TestChunkerRoundTripsThroughTheCollector(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		data := rapid.SliceOfN(rapid.Byte(), 1, 4096).Draw(rt, "data")
		chunkSize := rapid.IntRange(1, 512).Draw(rt, "chunkSize")
		writeSize := rapid.IntRange(1, 700).Draw(rt, "writeSize")
		var got []byte
		var gotId identifier.TaggedId
		col := &Collector{Reassembler: &Reassembler{}, OnComplete: func(id identifier.TaggedId, d []byte) error {
			gotId = id
			got = d
			return nil
		}}
		id := tagged(7)
		c := NewChunker(chunkSize)
		require.NoError(rt, c.Prepare(context.Background(), fixedIdGen{id}, nil, col))
		for off := 0; off < len(data); off += writeSize {
			end := min(off+writeSize, len(data))
			_, err := c.Write(data[off:end])
			require.NoError(rt, err)
		}
		require.NoError(rt, c.Close())
		require.Equal(rt, id, gotId)
		require.Equal(rt, data, got)
		require.Zero(rt, col.Reassembler.Open())
		require.Zero(rt, col.Reassembler.Held())
	})
}

func TestReassemblerOrderDuplicatesAndInterleaving(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nSets := rapid.IntRange(1, 4).Draw(rt, "sets")
		type chunk struct {
			id    identifier.TaggedId
			index uint32
			last  bool
			total uint32
			data  []byte
		}
		var all []chunk
		want := map[identifier.TaggedId][]byte{}
		for s := range nSets {
			id := tagged(uint64(s + 1))
			n := rapid.IntRange(1, 6).Draw(rt, "n")
			var whole []byte
			for i := range n {
				d := rapid.SliceOfN(rapid.Byte(), 0, 16).Draw(rt, "d")
				whole = append(whole, d...)
				all = append(all, chunk{id, uint32(i), i == n-1, uint32(n), d})
			}
			want[id] = whole
		}
		// Shuffle and duplicate a few.
		rng := rand.New(rand.NewPCG(rapid.Uint64().Draw(rt, "seed"), 1))
		rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
		if len(all) > 1 {
			all = append(all, all[rng.IntN(len(all))])
		}
		r := &Reassembler{}
		got := map[identifier.TaggedId][]byte{}
		for _, c := range all {
			total := uint32(0)
			if c.last {
				total = c.total
			}
			data, done, err := r.Add(c.id, c.index, c.last, total, c.data)
			require.NoError(rt, err)
			if done {
				_, twice := got[c.id]
				require.False(rt, twice, "a set completes once")
				got[c.id] = data
				r.Done(c.id)
			}
		}
		require.Equal(rt, len(want), len(got))
		for id, w := range want {
			require.Equal(rt, string(w), string(got[id]))
		}
		require.Zero(rt, r.Open())
	})
}

func TestReassemblerBounds(t *testing.T) {
	t.Run("byte bound refuses the chunk that overflows, and opens no set for it", func(t *testing.T) {
		r := &Reassembler{MaxBytes: 10}
		_, _, err := r.Add(tagged(1), 0, false, 0, make([]byte, 6))
		require.NoError(t, err)
		_, _, err = r.Add(tagged(2), 0, false, 0, make([]byte, 6))
		require.ErrorIs(t, err, ErrReassemblyFull)
		require.Equal(t, int64(6), r.Held())
		require.Equal(t, 1, r.Open())
	})
	t.Run("an index held before the total is judged when the total arrives", func(t *testing.T) {
		r := &Reassembler{}
		_, _, err := r.Add(tagged(1), 5, false, 0, []byte("z"))
		require.NoError(t, err)
		_, _, err = r.Add(tagged(1), 2, true, 3, []byte("c"))
		require.ErrorIs(t, err, ErrChunkDisagrees)
	})
	t.Run("a whole set is yielded again until Done", func(t *testing.T) {
		r := &Reassembler{}
		data, done, err := r.Add(tagged(1), 0, true, 1, []byte("a"))
		require.NoError(t, err)
		require.True(t, done)
		require.Equal(t, "a", string(data))
		data, done, err = r.Add(tagged(1), 0, true, 1, []byte("a"))
		require.NoError(t, err)
		require.True(t, done, "the handoff can be retried")
		require.Equal(t, "a", string(data))
		r.Done(tagged(1))
		_, done, err = r.Add(tagged(1), 0, true, 1, []byte("a"))
		require.NoError(t, err)
		require.False(t, done)
		require.Zero(t, r.Open())
	})
	t.Run("age bound expires and releases", func(t *testing.T) {
		now := time.Unix(1000, 0)
		r := &Reassembler{MaxAge: time.Minute, now: func() time.Time { return now }}
		_, _, err := r.Add(tagged(1), 0, false, 0, []byte("a"))
		require.NoError(t, err)
		require.Empty(t, r.Expire())
		now = now.Add(2 * time.Minute)
		_, _, err = r.Add(tagged(2), 0, false, 0, []byte("b"))
		require.NoError(t, err)
		expired := r.Expire()
		require.Equal(t, []identifier.TaggedId{tagged(1)}, expired)
		require.Equal(t, 1, r.Open())
		require.Equal(t, int64(1), r.Held())
	})
	t.Run("a late duplicate of a released set is dropped", func(t *testing.T) {
		r := &Reassembler{MaxRecent: 2}
		_, done, err := r.Add(tagged(1), 0, true, 1, []byte("a"))
		require.NoError(t, err)
		require.True(t, done)
		r.Done(tagged(1))
		_, done, err = r.Add(tagged(1), 0, true, 1, []byte("a"))
		require.NoError(t, err)
		require.False(t, done)
		require.Zero(t, r.Open())
		// The memory is a ring: two more releases evict the first id.
		_, _, _ = r.Add(tagged(2), 0, true, 1, nil)
		r.Done(tagged(2))
		_, _, _ = r.Add(tagged(3), 0, true, 1, nil)
		r.Done(tagged(3))
		_, done, err = r.Add(tagged(1), 0, true, 1, []byte("a"))
		require.NoError(t, err)
		require.True(t, done, "forgotten, so it completes again")
	})
	t.Run("disagreements are refused", func(t *testing.T) {
		r := &Reassembler{}
		_, _, err := r.Add(tagged(1), 0, false, 0, []byte("a"))
		require.NoError(t, err)
		_, _, err = r.Add(tagged(1), 0, false, 0, []byte("b"))
		require.ErrorIs(t, err, ErrChunkDisagrees)
		_, _, err = r.Add(tagged(1), 5, true, 3, []byte("z"))
		require.ErrorIs(t, err, ErrChunkDisagrees)
		_, _, err = r.Add(tagged(1), 1, true, 0, []byte("z"))
		require.Error(t, err)
		_, done, err := r.Add(tagged(1), 1, true, 2, []byte("z"))
		require.NoError(t, err)
		require.True(t, done)
		_, _, err = r.Add(tagged(3), 0, true, 1, []byte("only"))
		require.NoError(t, err)
	})
	t.Run("a failing handoff keeps the set for the retried write", func(t *testing.T) {
		fail := true
		var got []byte
		col := &Collector{Reassembler: &Reassembler{}, OnComplete: func(_ identifier.TaggedId, d []byte) error {
			if fail {
				return context.DeadlineExceeded
			}
			got = d
			return nil
		}}
		_, err := col.WriteFirstAndLastChunk(tagged(9), []byte("once"))
		require.Error(t, err)
		require.Equal(t, 1, col.Reassembler.Open())
		fail = false
		_, err = col.WriteFirstAndLastChunk(tagged(9), []byte("once"))
		require.NoError(t, err)
		require.Equal(t, "once", string(got))
		require.Zero(t, col.Reassembler.Open())
	})
}
