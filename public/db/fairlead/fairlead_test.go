package fairlead

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLeadWithNothingInstalledIsTheIdentity(t *testing.T) {
	ctx := context.Background()
	const sql = "SELECT 1"
	out, err := Lead(ctx, ExitServer, "test.site", sql)
	require.NoError(t, err)
	require.Equal(t, sql, out)
	allocs := testing.AllocsPerRun(100, func() {
		_, _ = Lead(ctx, ExitServer, "test.site", sql)
	})
	require.Zero(t, allocs)
}

func TestPassesRunInInstallOrderAndSeeEachOthersText(t *testing.T) {
	removeA := Install(PassFunc(func(_ context.Context, st Statement) (string, error) {
		return st.SQL + " /*a*/", nil
	}))
	defer removeA()
	var seenByB Statement
	removeB := Install(PassFunc(func(_ context.Context, st Statement) (string, error) {
		seenByB = st
		return st.SQL + " /*b*/", nil
	}))
	defer removeB()

	out, err := Lead(context.Background(), ExitLocal, "test.order", "SELECT 1")
	require.NoError(t, err)
	require.Equal(t, "SELECT 1 /*a*/ /*b*/", out)
	require.Equal(t, Statement{SQL: "SELECT 1 /*a*/", Exit: ExitLocal, Site: "test.order"}, seenByB)

	removeA()
	out, err = Lead(context.Background(), ExitLocal, "test.order", "SELECT 1")
	require.NoError(t, err)
	require.Equal(t, "SELECT 1 /*b*/", out)

	removeB()
	removeB()
	out, err = Lead(context.Background(), ExitLocal, "test.order", "SELECT 1")
	require.NoError(t, err)
	require.Equal(t, "SELECT 1", out)
	require.Nil(t, installed.Load())
}

func TestARefusalStopsTheStatement(t *testing.T) {
	refusal := errors.New("not here")
	remove := Install(PassFunc(func(context.Context, Statement) (string, error) {
		return "", refusal
	}))
	defer remove()
	reached := false
	removeLater := Install(PassFunc(func(_ context.Context, st Statement) (string, error) {
		reached = true
		return st.SQL, nil
	}))
	defer removeLater()

	out, err := Lead(context.Background(), ExitServer, "test.refuse", "DROP TABLE t")
	require.ErrorIs(t, err, refusal)
	require.Empty(t, out)
	require.False(t, reached)
}

func TestJournalWritesEachDistinctStatementOnce(t *testing.T) {
	var buf bytes.Buffer
	j := NewJournal(&buf)
	ctx := context.Background()
	for _, st := range []Statement{
		{SQL: "SELECT 1", Exit: ExitServer, Site: "a"},
		{SQL: "SELECT 1", Exit: ExitServer, Site: "a"},
		{SQL: "SELECT 1", Exit: ExitServer, Site: "b"},
		{SQL: "SELECT 1", Exit: ExitLocal, Site: "a"},
		{SQL: "SELECT 'x\ny'", Exit: ExitServer, Site: "a"},
	} {
		out, err := j.Lead(ctx, st)
		require.NoError(t, err)
		require.Equal(t, st.SQL, out)
	}
	written, dropped, failed := j.Stats()
	require.EqualValues(t, 4, written)
	require.Zero(t, dropped)
	require.Zero(t, failed)

	var got []JournalEntry
	for e, err := range ReadJournal(&buf) {
		require.NoError(t, err)
		got = append(got, e)
	}
	require.Equal(t, []JournalEntry{
		{Site: "a", Exit: "server", SQL: "SELECT 1"},
		{Site: "b", Exit: "server", SQL: "SELECT 1"},
		{Site: "a", Exit: "local", SQL: "SELECT 1"},
		{Site: "a", Exit: "server", SQL: "SELECT 'x\ny'"},
	}, got)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestJournalNeverHoldsAStatementBack(t *testing.T) {
	j := NewJournal(failingWriter{})
	out, err := j.Lead(context.Background(), Statement{SQL: "SELECT 1", Site: "a"})
	require.NoError(t, err)
	require.Equal(t, "SELECT 1", out)
	written, _, failed := j.Stats()
	require.Zero(t, written)
	require.EqualValues(t, 1, failed)
}

func TestReadJournalStopsAtATornLine(t *testing.T) {
	in := `{"site":"a","exit":"server","sql":"SELECT 1"}` + "\n" + `{"site":"a","exit":"ser`
	var got []JournalEntry
	var last error
	for e, err := range ReadJournal(strings.NewReader(in)) {
		if err != nil {
			last = err
			continue
		}
		got = append(got, e)
	}
	require.Len(t, got, 1)
	require.Error(t, last)
}

func TestJournalInADirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "journal")
	remove, err := installJournalIn(dir)
	require.NoError(t, err)
	_, err = Lead(context.Background(), ExitServer, "test.dir", "SELECT 42")
	require.NoError(t, err)
	remove()

	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	f, err := os.Open(files[0])
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var got []JournalEntry
	for e, rErr := range ReadJournal(f) {
		require.NoError(t, rErr)
		got = append(got, e)
	}
	require.Equal(t, []JournalEntry{{Site: "test.dir", Exit: "server", SQL: "SELECT 42"}}, got)
	require.Nil(t, installed.Load())
}
