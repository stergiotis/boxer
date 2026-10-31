package appcenter

import (
	"cmp"
	"context"
	"slices"
	"time"
)

// The Running tab (ADR-0348): every open window of this host, what its
// Frame costs the render loop, and the tasks it owns, with the tasks no open
// window owns beneath. Its tables are read only while it is shown; this file
// reads and joins them, appcenter_running_grid.go draws them.

// tableWindows is keelson('windows'), served by providersgui beside
// frame_times.
const tableWindows = "windows"

const (
	runWindowsSql = "SELECT key, app_id, display, title, kind, active, agent_tasks FROM keelson('windows') ORDER BY key"
	runFramesSql  = "SELECT scope, instance_key, frames, samples, last_us, mean_us, p50_us, p95_us, max_us, messages_mean, mount_us FROM keelson('frame_times') " +
		"ORDER BY scope, instance_key"
	runTasksSql = "SELECT title, kind, state, owner_app_id, owner_instance_key, created_at FROM keelson('tasks') ORDER BY created_at, task_id"
)

// windowCols is keelson('windows'): one row per open window.
type windowCols struct {
	Key        []int64    `ch:"key"`
	AppId      []string   `ch:"app_id"`
	Display    []string   `ch:"display"`
	Title      []string   `ch:"title"`
	Kind       []string   `ch:"kind"`
	Active     []bool     `ch:"active"`
	AgentTasks [][]string `ch:"agent_tasks"`
}

// runTaskCols is keelson('tasks'), every task in flight, oldest first.
type runTaskCols struct {
	Title            []string `ch:"title"`
	Kind             []string `ch:"kind"`
	State            []string `ch:"state"`
	OwnerAppId       []string `ch:"owner_app_id"`
	OwnerInstanceKey []uint64 `ch:"owner_instance_key"`
	CreatedAt        []string `ch:"created_at"`
}

// running is the page's three lenses and what they fold to.
type running struct {
	windows lens[windowCols]
	frames  lens[frameCols]
	tasks   lens[runTaskCols]
	view    runningView
	read    time.Time
}

// runningRow is one open window.
type runningRow struct {
	key        uint64
	appId      string
	display    string
	title      string
	kind       string
	active     bool
	agentTasks []string
	// sampled says frame_times holds frames of it; the times are 0 otherwise.
	sampled  bool
	p50Us    int64
	p95Us    int64
	messages float64
	// tasks counts the tasks it owns; oldest is the first of them to start.
	tasks          int
	oldestTitle    string
	oldestCreateAt string
	// taskTitles are its tasks' titles, which the filter matches.
	taskTitles []string
}

// unownedTask is a task whose owner_instance_key names no open window: a
// closed window's work still draining, or a runtime service's (key 0).
type unownedTask struct {
	title      string
	kind       string
	state      string
	ownerAppId string
	ownerKey   uint64
	createdAt  string
}

// runningView is the page's rows.
type runningView struct {
	rows      []runningRow
	unowned   []unownedTask
	loopP50Us int64
}

// readRunning reads the page's tables.
func readRunning(ctx context.Context, r readerI, rn running) running {
	readInto(ctx, r, tableWindows, runWindowsSql, "", &rn.windows)
	readInto(ctx, r, tableFrameTimes, runFramesSql, "", &rn.frames)
	readInto(ctx, r, tableTasks, runTasksSql, "", &rn.tasks)
	rn.view = assembleRunning(&rn.windows.cols, &rn.frames.cols, &rn.tasks.cols)
	rn.read = time.Now()
	return rn
}

// assembleRunning joins the three tables on the window key (ADR-0348 §SD2).
// Rows run slowest p95 first; windows with no frame samples follow, in key
// order, which is the order they opened.
func assembleRunning(w *windowCols, f *frameCols, t *runTaskCols) (v runningView) {
	byKey := make(map[uint64]int, len(w.Key))
	v.rows = make([]runningRow, 0, len(w.Key))
	for i, k := range w.Key {
		key := uint64(k)
		byKey[key] = len(v.rows)
		v.rows = append(v.rows, runningRow{key: key, appId: w.AppId[i], display: w.Display[i], title: w.Title[i],
			kind: w.Kind[i], active: w.Active[i], agentTasks: w.AgentTasks[i]})
	}
	for j, scope := range f.Scope {
		if scope == "loop" {
			v.loopP50Us = f.P50Us[j]
			continue
		}
		i, ok := byKey[f.InstanceKey[j]]
		if !ok || f.Samples[j] == 0 {
			continue
		}
		row := &v.rows[i]
		row.sampled, row.p50Us, row.p95Us, row.messages = true, f.P50Us[j], f.P95Us[j], f.MessagesMean[j]
	}
	for j, key := range t.OwnerInstanceKey {
		i, ok := byKey[key]
		if !ok || key == 0 {
			v.unowned = append(v.unowned, unownedTask{title: t.Title[j], kind: t.Kind[j], state: t.State[j],
				ownerAppId: t.OwnerAppId[j], ownerKey: key, createdAt: t.CreatedAt[j]})
			continue
		}
		row := &v.rows[i]
		if row.tasks == 0 {
			row.oldestTitle, row.oldestCreateAt = t.Title[j], t.CreatedAt[j]
		}
		row.tasks++
		row.taskTitles = append(row.taskTitles, t.Title[j])
	}
	slices.SortStableFunc(v.rows, func(a, b runningRow) int {
		if a.sampled != b.sampled {
			if a.sampled {
				return -1
			}
			return 1
		}
		if d := cmp.Compare(b.p95Us, a.p95Us); d != 0 {
			return d
		}
		return cmp.Compare(a.key, b.key)
	})
	return
}

// ageOf is how long ago an RFC 3339 instant was, to the second; "" when it
// does not parse.
func ageOf(at string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return ""
	}
	return now.Sub(t).Truncate(time.Second).String()
}

// rowName is the window's app as the list names it.
func rowName(row *runningRow) string {
	if row.display != "" {
		return row.display
	}
	return shortApp(row.appId)
}
