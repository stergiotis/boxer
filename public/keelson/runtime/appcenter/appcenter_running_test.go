package appcenter

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runningFixture is two open windows (keys 1 and 2), the loop, and four
// tasks: two of window 1's, one of a closed window 7's and one of a runtime
// service's.
func runningFixture() (w windowCols, f frameCols, t runTaskCols) {
	w = windowCols{Key: []int64{1, 2, 3}, AppId: []string{"a/play", "a/chat", "a/idle"}, Display: []string{"Play", "", "Idle"},
		Title: []string{"SQL", "Chat", "Idle"}, Kind: []string{"app", "app", "app"}, Active: []bool{false, true, false},
		AgentTasks: [][]string{nil, {"task-1"}, nil}}
	f = frameCols{Scope: []string{"loop", "window", "window", "window"}, InstanceKey: []uint64{0, 1, 2, 3},
		Samples: []int64{100, 50, 50, 0}, P50Us: []int64{16000, 900, 3000, 0}, P95Us: []int64{17000, 1200, 9000, 0},
		MessagesMean: []float64{0, 100, 300, 0}}
	t = runTaskCols{Title: []string{"run the query", "service", "publish", "run the write"},
		Kind: []string{"play.query", "svc", "play.publish", "play.write"}, State: []string{"running", "running", "running", "cancelling"},
		OwnerAppId: []string{"a/play", "", "a/play", "a/play"}, OwnerInstanceKey: []uint64{1, 0, 1, 7},
		CreatedAt: []string{"2026-10-29T10:00:00Z", "2026-10-29T10:00:01Z", "2026-10-29T10:00:02Z", "2026-10-29T10:00:03Z"}}
	return
}

func TestAssembleRunning(t *testing.T) {
	w, f, tk := runningFixture()
	v := assembleRunning(&w, &f, &tk)
	assert.EqualValues(t, 16000, v.loopP50Us)
	require.Len(t, v.rows, 3)
	keys := []uint64{v.rows[0].key, v.rows[1].key, v.rows[2].key}
	assert.Equal(t, []uint64{2, 1, 3}, keys, "slowest p95 first, a window with no frames last")

	chat, play, idle := v.rows[0], v.rows[1], v.rows[2]
	assert.True(t, chat.sampled)
	assert.EqualValues(t, 9000, chat.p95Us)
	assert.True(t, chat.active)
	assert.Equal(t, []string{"task-1"}, chat.agentTasks)
	assert.Equal(t, 0, chat.tasks)
	assert.Equal(t, "chat", rowName(&chat), "no display name: the id's leaf")
	assert.Equal(t, 2, play.tasks)
	assert.Equal(t, "run the query", play.oldestTitle, "the oldest task is the first to start")
	assert.False(t, idle.sampled)
	assert.EqualValues(t, 0, idle.p95Us)

	require.Len(t, v.unowned, 2)
	assert.EqualValues(t, 0, v.unowned[0].ownerKey, "a runtime service's task")
	assert.EqualValues(t, 7, v.unowned[1].ownerKey, "a closed window's work still finishing")
	assert.Equal(t, "cancelling", v.unowned[1].state)

	empty := assembleRunning(&windowCols{}, &frameCols{}, &runTaskCols{})
	assert.Empty(t, empty.rows)
	assert.Empty(t, empty.unowned)
}

func TestAgeOf(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 1, 30, 500, time.UTC)
	assert.Equal(t, "1m30s", ageOf("2026-10-29T10:00:00Z", now))
	assert.Equal(t, "", ageOf("not a time", now))
}

// The running tab's tables are read while it is shown, and only then.
func TestRefreshReadsRunningOnlyWhileShown(t *testing.T) {
	w, f, tk := runningFixture()
	fx := &fixtureReader{answers: map[string]func(dst any){
		tableWindows: func(dst any) { *dst.(*windowCols) = w },
		// frame_times answers the page's read and the Running page's alike.
		tableFrameTimes: func(dst any) { *dst.(*frameCols) = f },
		tableTasks: func(dst any) {
			switch d := dst.(type) {
			case *runTaskCols:
				*d = tk
			case *taskCols:
				*d = taskCols{}
			}
		},
	}}
	inst := newApp()
	inst.reader = fx
	inst.appCtx, inst.cancelApp = context.WithCancel(context.Background())
	defer inst.cancelApp()

	inst.refresh()
	snap := inst.snapshot()
	assert.False(t, snap.running.read.IsZero())
	assert.Equal(t, lensStateOk, snap.running.windows.state)
	assert.Len(t, snap.running.view.rows, 3)
	assert.Len(t, snap.running.view.unowned, 2)

	inst.selectApp("a/play")
	fx.tables = nil
	inst.refresh()
	assert.False(t, slices.Contains(fx.tables, tableWindows), "an app's page does not read the open windows")
	assert.Len(t, inst.snapshot().running.view.rows, 3, "the last Running read stays until the page reads again")

	inst.setView(viewRunning)
	fx.tables = nil
	inst.refresh()
	assert.True(t, slices.Contains(fx.tables, tableWindows))
	assert.False(t, slices.Contains(fx.tables, tableAppState), "nor does the running tab read the app's page")
	assert.Equal(t, "a/play", inst.selected, "the selection stays for the apps tab")
}

func TestListRunning(t *testing.T) {
	inst := opsFixture(t)
	out, err := query[struct{}, Running](t, inst, opListRunning, struct{}{})
	require.NoError(t, err)
	assert.Empty(t, out.Windows, "the fixture serves no windows")

	w, f, tk := runningFixture()
	inst.mu.Lock()
	inst.snap.running = running{view: assembleRunning(&w, &f, &tk), read: time.Now()}
	inst.mu.Unlock()
	out, err = query[struct{}, Running](t, inst, opListRunning, struct{}{})
	require.NoError(t, err)
	require.Len(t, out.Windows, 3)
	assert.EqualValues(t, 2, out.Windows[0].Key)
	assert.EqualValues(t, 9000, out.Windows[0].P95Us)
	assert.Equal(t, 2, out.Windows[1].Tasks)
	assert.Equal(t, "run the query", out.Windows[1].OldestTask)
	require.Len(t, out.Unowned, 2)
	assert.EqualValues(t, 7, out.Unowned[1].OwnerInstanceKey)
	assert.EqualValues(t, 16000, out.LoopP50Us)
	assert.False(t, out.ReadAt.IsZero())
	assert.False(t, out.Truncated)

	many := runningView{rows: make([]runningRow, runningListed+1)}
	assert.True(t, runningResult(many, time.Now()).Truncated)
	assert.Len(t, runningResult(many, time.Now()).Windows, runningListed)
}
