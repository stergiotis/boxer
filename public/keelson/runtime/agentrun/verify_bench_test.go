package agentrun

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// BenchmarkVerifyRun is law L31 over a run of a hundred entries: what
// keelson('evidence_checks') pays per journal run, dominated by the rows'
// canonical items.
func BenchmarkVerifyRun(b *testing.B) {
	ctx := context.Background()
	journal := NewMemStore()
	w, err := Open(ctx, journal, Run{Id: 11, Cell: "c", OwnerApp: "app.chat"})
	require.NoError(b, err)
	for i := range 100 {
		_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "entry " + strconv.Itoa(i), Turn: "t1"})
		require.NoError(b, err)
	}
	runs, err := journal.ListRuns(ctx, "", 0)
	require.NoError(b, err)
	require.Len(b, runs, 1)
	entries, err := journal.Entries(ctx, 11, 0)
	require.NoError(b, err)
	require.Len(b, entries, 100)
	b.ReportAllocs()
	for b.Loop() {
		broken, verr := VerifyRun(runs[0], entries)
		if verr != nil || broken != 0 {
			b.Fatal(broken, verr)
		}
	}
}
