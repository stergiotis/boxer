package play

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
)

func waitFetched(t *testing.T, d *runsHistoryDriver) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, _, inFlight, fetched, _ := d.snapshot()
		return fetched && !inFlight
	}, 2*time.Second, 5*time.Millisecond)
}

func TestRunsHistoryDriverFetchAndError(t *testing.T) {
	d := &runsHistoryDriver{}
	d.fetch = func(ctx context.Context) ([]queryrunfacts.HistoryRow, error) {
		return []queryrunfacts.HistoryRow{{Id: 7, QueryId: "q1"}}, nil
	}
	d.refresh()
	waitFetched(t, d)
	rows, err, _, _, asOf := d.snapshot()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "q1", rows[0].QueryId)
	require.False(t, asOf.IsZero())

	d.fetch = func(ctx context.Context) ([]queryrunfacts.HistoryRow, error) {
		return nil, eh.Errorf("table gone")
	}
	d.refresh()
	require.Eventually(t, func() bool {
		_, err, inFlight, _, _ := d.snapshot()
		return !inFlight && err != nil
	}, 2*time.Second, 5*time.Millisecond)
	_, err, _, _, _ = d.snapshot()
	require.Contains(t, err.Error(), "table gone")
}

func TestRunsHistoryDriverSingleFlight(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	d := &runsHistoryDriver{}
	d.fetch = func(ctx context.Context) ([]queryrunfacts.HistoryRow, error) {
		calls.Add(1)
		<-release
		return nil, nil
	}
	d.refresh()
	d.refresh() // in flight — must not start a second fetch
	close(release)
	waitFetched(t, d)
	require.EqualValues(t, 1, calls.Load())
}

func TestRunsHistoryDriverRevealFiresOnce(t *testing.T) {
	var calls atomic.Int32
	d := &runsHistoryDriver{}
	d.fetch = func(ctx context.Context) ([]queryrunfacts.HistoryRow, error) {
		calls.Add(1)
		return nil, nil
	}
	d.maybeRefreshOnReveal()
	waitFetched(t, d)
	d.maybeRefreshOnReveal() // fetched — reveal must not re-fire
	require.EqualValues(t, 1, calls.Load())
}

func TestRunsHistoryDriverNilEndpoint(t *testing.T) {
	d := newRunsHistoryDriver(nil)
	require.Nil(t, d.fetch)
	d.refresh()             // must not panic
	d.maybeRefreshOnReveal() // must not panic
	_, err, inFlight, fetched, _ := d.snapshot()
	require.NoError(t, err)
	require.False(t, inFlight)
	require.False(t, fetched)
}

func TestRunRowLabel(t *testing.T) {
	row := queryrunfacts.HistoryRow{
		Ts:         time.Date(2026, 7, 17, 12, 30, 5, 0, time.UTC),
		DurationMs: 42,
		Kind:       "Select",
		Event:      "QueryFinish",
		QueryText:  "SELECT 1\nFROM two_lines",
	}
	label := runRowLabel(row)
	require.Contains(t, label, "42 ms")
	require.Contains(t, label, "Select")
	require.Contains(t, label, "SELECT 1")
	require.NotContains(t, label, "two_lines", "only the first line belongs in the list")
	require.False(t, strings.HasPrefix(label, "! "))

	row.Event = "ExceptionWhileProcessing"
	row.ExceptionCode = 47
	require.True(t, strings.HasPrefix(runRowLabel(row), "! "))
}
