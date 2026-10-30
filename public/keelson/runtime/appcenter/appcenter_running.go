package appcenter

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
)

// The Running page (ADR-0348): every open window of this host, what its
// Frame costs the render loop, and the tasks it owns, with the tasks no open
// window owns beneath. It is what the right side shows while no app is
// selected, and its tables are read only then.

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

func (inst *App) renderRunning(snap snapshot) {
	rn := &snap.running
	c.LabelAtoms(c.Atoms().BeginRichText("Running").Heading().End().Keep()).Send()
	weak("Every open window of this host, slowest first. Frame time is the window's Frame on the render goroutine, " +
		"which every window shares; tasks are the ones the window registered with the task API, so the count is a lower bound. " +
		"The apps share one process, so memory and CPU are not shown per window: imztop shows the process's.")
	c.AddSpace(styletokens.PaddingInner(inst.density))
	if rn.read.IsZero() {
		c.Label("Reading…").Send()
		return
	}
	v := &rn.view
	if !inst.lensReady("running-windows", rn.windows.state, rn.windows.note, len(v.rows), "No window is open.") {
		inst.renderUnowned(v)
		return
	}
	if rn.frames.state != lensStateOk {
		inst.lensReady("running-frames", rn.frames.state, rn.frames.note, 1, "")
	}
	if rn.tasks.state != lensStateOk {
		inst.lensReady("running-tasks", rn.tasks.state, rn.tasks.note, 1, "")
	}
	head := fmt.Sprintf("%d window(s).", len(v.rows))
	if v.loopP50Us > 0 {
		head += " The render loop's median period is " + fmtUs(v.loopP50Us) + "."
	}
	c.Label(head).Send()
	if inst.actionNote != "" {
		badge.New(inst.ids.PrepareStr("action-note"), inst.actionNote).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
	}
	inst.pruneClosing(v)
	// A host that wires none shows no actions (ADR-0348 §SD4).
	h := hostOf()
	headers := []string{"app", "window", "p50", "p95", "of loop", "messages", "tasks", "oldest task", "agent tasks"}
	if h != nil {
		headers = append(headers, "")
	}
	now := time.Now()
	inst.grid("running-grid", headers, len(v.rows), func(j int) {
		row := &v.rows[j]
		for range c.HoverText(row.appId).KeepIter() {
			if c.SelectableLabel(inst.ids.PrepareStr("app"), false, rowName(row)).SendResp().HasPrimaryClicked() {
				// The app's own page, through the catalog (ADR-0345 §SD3).
				inst.selectGesture(row.appId)
			}
		}
		title := row.title
		if row.active {
			title += "  (active)"
		}
		c.Label(title).Send()
		if !row.sampled {
			weak("no frames")
			weak("")
			weak("")
			weak("")
		} else {
			mono(fmtUs(row.p50Us))
			if row.p95Us >= slowFrameUs {
				badge.New(inst.ids.PrepareStr("slow"), fmtUs(row.p95Us)).Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
			} else {
				mono(fmtUs(row.p95Us))
			}
			if v.loopP50Us > 0 {
				mono(fmt.Sprintf("%.0f%%", 100*float64(row.p50Us)/float64(v.loopP50Us)))
			} else {
				mono("—")
			}
			mono(fmt.Sprintf("%.0f", row.messages))
		}
		mono(fmt.Sprint(row.tasks))
		if row.tasks > 0 {
			c.Label(row.oldestTitle + ", " + ageOf(row.oldestCreateAt, now)).Send()
		} else {
			weak("—")
		}
		if len(row.agentTasks) > 0 {
			mono(fmt.Sprint(len(row.agentTasks)))
		} else {
			weak("—")
		}
		if h != nil {
			inst.renderActions(h, row)
		}
	})
	inst.renderUnowned(v)
}

// renderActions is a row's Raise and Close. Close asks first, naming the
// window, since it ends the window's work as its own close button does.
func (inst *App) renderActions(h HostI, row *runningRow) {
	for range c.HorizontalTop().KeepIter() {
		switch {
		case inst.closing[row.key]:
			weak("closing…")
		case inst.closeArmed == row.key:
			if c.Button(inst.ids.PrepareStr("close-confirm"), c.Atoms().Text(icons.PhX+" Close "+row.title).Keep()).Small().SendResp().HasPrimaryClicked() {
				inst.closeWindow(h, row.key)
			}
			if c.Button(inst.ids.PrepareStr("close-keep"), c.Atoms().Text("Keep").Keep()).Small().SendResp().HasPrimaryClicked() {
				inst.closeArmed = 0
			}
		default:
			if c.Button(inst.ids.PrepareStr("raise"), c.Atoms().Text(icons.PhArrowUp+" Raise").Keep()).Small().SendResp().HasPrimaryClicked() {
				inst.raiseWindow(h, row.key)
			}
			if c.Button(inst.ids.PrepareStr("close"), c.Atoms().Text(icons.PhX+" Close").Keep()).Small().SendResp().HasPrimaryClicked() {
				inst.armClose(row.key)
			}
		}
	}
}

func (inst *App) renderUnowned(v *runningView) {
	if len(v.unowned) == 0 {
		return
	}
	c.AddSpace(styletokens.GapItems(inst.density))
	strong("Not owned by an open window")
	weak("A closed window's work still finishing, which its window's drain waits for, and runtime services' tasks, which no window owns.")
	now := time.Now()
	inst.grid("unowned-grid", []string{"task", "kind", "state", "owner", "window", "age"}, len(v.unowned), func(j int) {
		u := &v.unowned[j]
		c.Label(u.title).Send()
		mono(u.kind)
		c.Label(u.state).Send()
		for range c.HoverText(u.ownerAppId).KeepIter() {
			c.Label(shortApp(u.ownerAppId)).Send()
		}
		mono(windowOf(u.ownerKey))
		mono(ageOf(u.createdAt, now))
	})
}
