package example

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// ScanOpts.Descending and ScanOpts.OrderBy: a bounded newest-first scan is
// one statement, any envelope column can lead the sort, and a name that is
// not one is refused rather than rendered.
func TestScanOrderAndBound(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewRevStore(exec, nil, RevStoreConfig{})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))
	for _, w := range []struct {
		id  string
		ord uint64
		rev uint32
	}{{"a", 1, 2}, {"b", 2, 0}, {"c", 3, 1}} {
		require.NoError(t, st.Begin(w.id, w.ord, RevEnvelope{Rev: w.rev}).AddRevNote(RevNote{Text: w.id}).Commit())
	}
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	ids := func(opts recordstore.ScanOpts) (out []string) {
		for ent, serr := range st.ScanRevNote(ctx, opts) {
			require.NoError(t, serr)
			out = append(out, ent.ID)
		}
		return
	}
	require.Equal(t, []string{"a", "b", "c"}, ids(recordstore.ScanOpts{}))
	require.Equal(t, []string{"c", "b"}, ids(recordstore.ScanOpts{Descending: true, Limit: 2}), "newest first, bounded in the statement")
	require.Equal(t, []string{"b", "c", "a"}, ids(recordstore.ScanOpts{OrderBy: "rev"}))

	var refused error
	for _, serr := range st.ScanRevNote(ctx, recordstore.ScanOpts{OrderBy: "nosuch; DROP"}) {
		refused = serr
	}
	require.ErrorIs(t, refused, recordstore.ErrUnknownOrderColumn)
}
