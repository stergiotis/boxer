package attention

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restartItems are the restart questions waiting in qs.
func restartItems(qs *Questions) (items []Item) {
	for it := range qs.WaitingItems() {
		if it.App == restartAskApp {
			items = append(items, it)
		}
	}
	return
}

// While a restart is pending one question waits. "Later" asks again after
// the snooze, "Restart now" restarts once, and nothing is asked after that.
func TestRestartAskAsksUntilThePersonAgrees(t *testing.T) {
	qs := NewQuestions(nil)
	pending := PendingRestart{Service: "gui", Since: t0}
	isPending := false
	restarts := 0
	ask := NewRestartAsk(qs, func() (PendingRestart, bool, error) { return pending, isPending, nil }, func() { restarts++ }, time.Hour, zerolog.Nop())

	ask.Tick(t0)
	assert.Empty(t, restartItems(qs), "nothing pending, nothing asked")

	isPending = true
	ask.Tick(t0)
	ask.Tick(t0.Add(time.Second))
	items := restartItems(qs)
	require.Len(t, items, 1, "one question, however often it looks")
	q, _, _, ok := qs.Lookup(items[0].Key)
	require.True(t, ok)
	assert.True(t, q.Quick(), "answered on its card")
	assert.Equal(t, []string{"Restart now", "Later"}, []string{q.Options[0].Label, q.Options[1].Label})
	assert.NotContains(t, q.Text, "Without an answer", "no deadline, no promise of one")
	_, from, _, _ := qs.Lookup(items[0].Key)
	assert.True(t, IsHostQuestion(from), "the host asks in its own words")
	assert.False(t, IsHostQuestion("chat"))

	require.True(t, qs.Decide(items[0].Key, false, [][]int{{1}}))
	ask.Tick(t0.Add(2 * time.Second))
	assert.Zero(t, restarts, "later is not now")
	ask.Tick(t0.Add(30 * time.Minute))
	assert.Empty(t, restartItems(qs), "not asked again within the snooze")
	ask.Tick(t0.Add(61 * time.Minute))
	items = restartItems(qs)
	require.Len(t, items, 1, "asked again after it")

	require.True(t, qs.Decide(items[0].Key, true, nil))
	ask.Tick(t0.Add(62 * time.Minute))
	assert.Zero(t, restarts, "a skip is not an agreement")
	ask.Tick(t0.Add(3 * time.Hour))
	items = restartItems(qs)
	require.Len(t, items, 1)

	require.True(t, qs.Decide(items[0].Key, false, [][]int{{0}}))
	ask.Tick(t0.Add(3*time.Hour + time.Second))
	assert.Equal(t, 1, restarts)
	ask.Tick(t0.Add(9 * time.Hour))
	assert.Equal(t, 1, restarts, "restarted once")
	assert.Empty(t, restartItems(qs))
}

// A restart that is no longer pending takes its question back, and a later
// one is asked about anew.
func TestRestartAskWithdrawsWhenNothingIsPending(t *testing.T) {
	qs := NewQuestions(nil)
	pending := PendingRestart{Service: "gui", Since: t0}
	isPending := true
	ask := NewRestartAsk(qs, func() (PendingRestart, bool, error) { return pending, isPending, nil }, func() {}, time.Hour, zerolog.Nop())
	ask.Tick(t0)
	require.Len(t, restartItems(qs), 1)

	isPending = false
	ask.Tick(t0.Add(time.Second))
	assert.Empty(t, restartItems(qs), "the host restarted by other means, or the change was taken back")

	isPending, pending.Since = true, t0.Add(time.Minute)
	ask.Tick(t0.Add(2 * time.Minute))
	assert.Len(t, restartItems(qs), 1)

	ask.Close()
	assert.Empty(t, restartItems(qs))
}

// With a deadline the question says when the host restarts regardless, the
// next question comes no later than half the time left, and none is asked
// once the deadline has come.
func TestRestartAskKnowsItsDeadline(t *testing.T) {
	qs := NewQuestions(nil)
	deadline := t0.Add(40 * time.Minute)
	ask := NewRestartAsk(qs, func() (PendingRestart, bool, error) {
		return PendingRestart{Service: "gui", Since: t0, Deadline: deadline}, true, nil
	}, func() {}, 2*time.Hour, zerolog.Nop())
	ask.Tick(t0)
	items := restartItems(qs)
	require.Len(t, items, 1)
	q, _, _, _ := qs.Lookup(items[0].Key)
	assert.Contains(t, q.Text, "Without an answer it restarts on")

	require.True(t, qs.Decide(items[0].Key, false, [][]int{{1}}))
	ask.Tick(t0.Add(time.Second))
	ask.Tick(t0.Add(19 * time.Minute))
	assert.Empty(t, restartItems(qs))
	ask.Tick(t0.Add(21 * time.Minute))
	items = restartItems(qs)
	require.Len(t, items, 1, "half the time left, not the whole snooze")

	require.True(t, qs.Decide(items[0].Key, true, nil))
	ask.Tick(t0.Add(22 * time.Minute))
	ask.Tick(deadline.Add(time.Second))
	assert.Empty(t, restartItems(qs), "at the deadline whoever set it restarts the host")

	assert.Equal(t, time.Minute, snoozeFor(time.Hour, t0.Add(30*time.Second), t0), "never busier than once a minute")
}

func TestReadPendingRestart(t *testing.T) {
	dir := t.TempDir()
	_, pending, err := ReadPendingRestart(filepath.Join(dir, "absent.json"), "gui")
	require.NoError(t, err)
	assert.False(t, pending, "no file, nothing pending")

	path := filepath.Join(dir, "restart-pending.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"schema":"elle.deployment-restart-pending/1","units":[
		{"unit":"alice-queue.service","service":"queue","since":"2026-11-19T12:00:00Z"},
		{"unit":"alice-gui.service","service":"gui","since":"2026-11-19T12:00:00Z","deadline":"2026-11-20T12:00:00Z"}]}`), 0o644))
	p, pending, err := ReadPendingRestart(path, "gui")
	require.NoError(t, err)
	require.True(t, pending)
	assert.Equal(t, time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), p.Deadline.UTC())
	_, pending, err = ReadPendingRestart(path, "other")
	require.NoError(t, err)
	assert.False(t, pending)

	require.NoError(t, os.WriteFile(path, []byte(`{`), 0o644))
	_, _, err = ReadPendingRestart(path, "gui")
	assert.Error(t, err)
}
