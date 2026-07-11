package dimension

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identgen/mem"
	"github.com/stretchr/testify/require"
)

// recordingSink counts emits; Resolve serves from the recorded map.
type recordingSink struct {
	emits map[uint64]string
}

func (s *recordingSink) Emit(_ context.Context, id uint64, d string) error {
	s.emits[id] = d
	return nil
}
func (s *recordingSink) Resolve(_ context.Context, id uint64) (string, bool, error) {
	d, ok := s.emits[id]
	return d, ok, nil
}
func (s *recordingSink) Flush(_ context.Context) (int, error) { return 0, nil }

// TestReferenceEmitsOncePerKey pins the intern → emit-once loop without a
// database: the first sight of a key emits, a repeat returns the same id and
// emits nothing, a distinct key gets a distinct id.
func TestReferenceEmitsOncePerKey(t *testing.T) {
	ctx := context.Background()
	gen, err := mem.NewIdInternalizer(1, 8)
	require.NoError(t, err)
	sink := &recordingSink{emits: map[uint64]string{}}
	st := New(gen, sink)

	a1, err := st.Reference(ctx, []byte("a"), func() string { return "descA" })
	require.NoError(t, err)
	a2, err := st.Reference(ctx, []byte("a"), func() string { t.Fatal("describe on a seen key"); return "" })
	require.NoError(t, err)
	require.Equal(t, a1, a2)
	require.Len(t, sink.emits, 1)

	b, err := st.Reference(ctx, []byte("b"), func() string { return "descB" })
	require.NoError(t, err)
	require.NotEqual(t, a1, b)
	require.Len(t, sink.emits, 2)

	d, found, err := st.Resolve(ctx, a1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "descA", d)
}

// failingSink fails the first n Emit calls, then delegates to recordingSink.
type failingSink struct {
	recordingSink
	failuresLeft int
}

func (s *failingSink) Emit(ctx context.Context, id uint64, d string) error {
	if s.failuresLeft > 0 {
		s.failuresLeft--
		return context.DeadlineExceeded
	}
	return s.recordingSink.Emit(ctx, id, d)
}

// TestReferenceRetriesFailedEmit: a failed Emit must not orphan the interned
// id — the generator never reports the key fresh again, so Reference has to
// retry the emission on the next sight instead of leaving the descriptor
// unwritten for the generator's lifetime.
func TestReferenceRetriesFailedEmit(t *testing.T) {
	ctx := context.Background()
	gen, err := mem.NewIdInternalizer(1, 8)
	require.NoError(t, err)
	sink := &failingSink{recordingSink: recordingSink{emits: map[uint64]string{}}, failuresLeft: 1}
	st := New(gen, sink)

	// First sight: interned, but the emission fails — surfaced fail-fast.
	id1, err := st.Reference(ctx, []byte("k"), func() string { return "desc" })
	require.Error(t, err)
	require.Empty(t, sink.emits)

	// Next sight of the same key: not fresh, but the emission is retried.
	id2, err := st.Reference(ctx, []byte("k"), func() string { return "desc" })
	require.NoError(t, err)
	require.Equal(t, id1, id2)
	require.Len(t, sink.emits, 1)

	// Settled: further sights emit nothing.
	_, err = st.Reference(ctx, []byte("k"), func() string { t.Fatal("describe after settled emit"); return "" })
	require.NoError(t, err)
	require.Len(t, sink.emits, 1)
}

// reentrantSink calls back into the Store it is the sink for — the shape of a
// stamper registered on its own descriptor store.
type reentrantSink struct{ st *Store[string] }

func (s *reentrantSink) Emit(ctx context.Context, _ uint64, _ string) error {
	_, err := s.st.Reference(ctx, []byte("nested"), func() string { return "nested" })
	return err
}
func (s *reentrantSink) Resolve(_ context.Context, _ uint64) (string, bool, error) {
	return "", false, nil
}
func (s *reentrantSink) Flush(_ context.Context) (int, error) { return 0, nil }

// TestReferenceRefusesReentrancy: an Emit that finds its way back into the
// same Store's Reference must error out, not recurse minting garbage ids.
func TestReferenceRefusesReentrancy(t *testing.T) {
	gen, err := mem.NewIdInternalizer(1, 8)
	require.NoError(t, err)
	sink := &reentrantSink{}
	st := New(gen, sink)
	sink.st = st
	_, err = st.Reference(context.Background(), []byte("outer"), func() string { return "outer" })
	require.ErrorContains(t, err, "re-entrant")
}
