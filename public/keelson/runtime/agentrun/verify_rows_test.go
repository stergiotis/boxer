package agentrun

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// VerifyRunRows over rows read once is VerifyRun over the entries, so a
// caller judging several laws reads the rows once.
func TestVerifyRunRowsIsVerifyRun(t *testing.T) {
	ctx := context.Background()
	journal := NewMemStore()
	w, err := Open(ctx, journal, Run{Id: 11, Cell: "c", OwnerApp: "app.chat"})
	require.NoError(t, err)
	for _, body := range []string{"a", "b", "c"} {
		_, err = w.Append(ctx, Entry{Kind: KindUser, Body: body, Turn: "t1"})
		require.NoError(t, err)
	}
	runs, err := journal.ListRuns(ctx, "", 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	entries, err := journal.Entries(ctx, 11, 0)
	require.NoError(t, err)
	broken, err := VerifyRun(runs[0], entries)
	require.NoError(t, err)
	require.Zero(t, broken)
	rows, err := Rows(entries)
	require.NoError(t, err)
	broken, err = VerifyRunRows(runs[0], rows)
	require.NoError(t, err)
	require.Zero(t, broken)

	entries[1].Body = "changed"
	broken, err = VerifyRun(runs[0], entries)
	require.NoError(t, err)
	require.EqualValues(t, 2, broken)
	rows, err = Rows(entries)
	require.NoError(t, err)
	broken, err = VerifyRunRows(runs[0], rows)
	require.NoError(t, err)
	require.EqualValues(t, 2, broken)

	bad := runs[0]
	bad.Digest = "not-the-seed"
	_, err = VerifyRunRows(bad, rows)
	require.Error(t, err)
}
