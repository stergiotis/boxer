package example

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// TestRevStoreSupersedesInPlace is the Revision-role acceptance: a later
// revision of one (Key, Order) supersedes the earlier one in every read —
// Scan, Latest, Replay — with one insert and no delete; a superseding row
// that carries no component hides the row it supersedes from the
// component Scan rather than letting it through; and two keys sharing an
// Order are positions of their own.
func TestRevStoreSupersedesInPlace(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewRevStore(exec, nil, RevStoreConfig{})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))
	require.NoError(t, st.VerifySchema(ctx), "fresh table must match the generated schema")

	write := func(id string, ord uint64, rev uint32, text *string) {
		b := st.Begin(id, ord, RevEnvelope{Rev: rev})
		if text != nil {
			b.AddRevNote(RevNote{Text: *text})
		}
		require.NoError(t, b.Commit())
	}
	s := func(v string) *string { return &v }
	write("r1", 1, 0, s("a"))
	write("r1", 2, 0, s("b"))
	write("r1", 3, 0, s("c"))
	write("r2", 1, 0, s("x"))
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	// Supersede r1@1 with a blanked revision, and r1@3 with a row that
	// carries no component at all.
	write("r1", 1, 1, s(""))
	write("r1", 3, 1, nil)
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	type row struct {
		ord  uint64
		rev  uint32
		text string
	}
	scan := func(opts recordstore.ScanOpts) (rows []row) {
		for ent, serr := range st.ScanRevNote(ctx, opts) {
			require.NoError(t, serr)
			rows = append(rows, row{ent.Ord, ent.Rev, ent.RevNote.Val.Text})
		}
		return
	}
	require.Equal(t, []row{{1, 1, ""}, {2, 0, "b"}}, scan(recordstore.ScanOpts{KeyPrefix: "r1"}),
		"r1@1 reads its revision 1; r1@3 is superseded by a row without the component")
	require.Equal(t, []row{{1, 0, "x"}}, scan(recordstore.ScanOpts{ExtraPredicate: RevColKey + " = 'r2'"}),
		"another key at the same Order is its own position")
	require.Len(t, scan(recordstore.ScanOpts{}), 3)

	got, found, err := st.Latest(ctx, "r1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(3), got.Ord)
	require.Equal(t, uint32(1), got.Rev, "Latest takes the highest revision of the newest Order")
	require.False(t, got.RevNote.Has)

	var replayed []row
	for ent, rerr := range st.Replay(ctx, "r1", 0, recordstore.ReplayOptsU64{}) {
		require.NoError(t, rerr)
		replayed = append(replayed, row{ent.Ord, ent.Rev, ent.RevNote.Val.Text})
	}
	require.Equal(t, []row{{1, 1, ""}, {2, 0, "b"}, {3, 1, ""}}, replayed, "one row per Order, its highest revision")
	replayed = replayed[:0]
	for ent, rerr := range st.Replay(ctx, "r1", 0, recordstore.ReplayOptsU64{Limit: 2}) {
		require.NoError(t, rerr)
		replayed = append(replayed, row{ent.Ord, ent.Rev, ent.RevNote.Val.Text})
	}
	require.Equal(t, []row{{1, 1, ""}, {2, 0, "b"}}, replayed, "the limit counts positions, not rows")
}
