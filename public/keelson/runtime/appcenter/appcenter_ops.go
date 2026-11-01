package appcenter

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
)

// The operations catalog (ADR-0345 §SD1, §SD6 M3). Every row a page shows is
// a `keelson()` table already, so the catalog carries the view alone: which
// tab is shown, which app's page is open, and the two filters. Commands set
// them and never toggle. The one exception is the Running tab's join
// (ADR-0348 §SD5): its rows are three tables composed in Go, which no single
// statement returns, so list_running hands them out as the tab last read
// them. Closing windows is not an operation; measuring their CPU is
// (ADR-0351 §SD5), since it changes nothing the person owns.

const (
	resView      = "view"
	resSelection = "selection"
	resFilter    = "filter"
	// resRunningFilter is the Running tab's filter.
	resRunningFilter = "running_filter"
	// resCpu is the Running tab's last CPU measurement.
	resCpu = "cpu_measurement"

	opGetView          = "get_view"
	opSetView          = "set_view"
	opShowApp          = "show_app"
	opSetFilter        = "set_filter"
	opSetRunningFilter = "set_running_filter"
	opListRunning      = "list_running"
	opMeasureCpu       = "measure_cpu"
)

// The tabs as the catalog names them.
const (
	viewRunningName = "running"
	viewAppsName    = "apps"
)

func viewName(v viewE) (s string) {
	if v == viewApps {
		return viewAppsName
	}
	return viewRunningName
}

func parseView(s string) (v viewE, err error) {
	switch s {
	case viewRunningName:
		v = viewRunning
	case viewAppsName:
		v = viewApps
	default:
		err = app.RefuseOperation("view is " + viewRunningName + " or " + viewAppsName)
	}
	return
}

// runningListed bounds what list_running returns of each list; Truncated
// says when it cut.
const runningListed = 200

const notRegisteredReason = "no app with that id is registered in this process; keelson('apps') lists them"

// opsSnap is what queries read: the view as the frame drew it.
type opsSnap struct {
	view       viewE
	selected   string
	filter     string
	runFilter  string
	registered int
	listed     int
	reads      bool
	pageRead   time.Time
	pageFor    string
	running    runningView
	runningAt  time.Time
	cpu        *cpuMeasurement
	cpuState   bgjob.Snapshot
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.view, sn.selected, sn.filter, sn.runFilter = inst.view, inst.selected, inst.filter, inst.runFilter
	snap := inst.snapshot()
	sn.reads = snap.reads
	sn.registered = len(snap.global.apps.cols.Id)
	sn.listed = len(matchingApps(&snap.global.apps.cols, inst.filter))
	sn.pageRead, sn.pageFor = snap.page.read, snap.page.appId
	sn.running, sn.runningAt = snap.running.view, snap.running.read
	sn.cpu, sn.cpuState = inst.cpu.last, inst.cpu.job.Snapshot()
	return
}

// View is get_view's result.
type View struct {
	View          string    `desc:"the tab shown: running, every open window, or apps, the app list and an app's page"`
	Selected      string    `desc:"the app whose page the apps tab shows; empty shows the list alone"`
	Filter        string    `desc:"the list's filter: a case-insensitive substring of an app's id, name or summary"`
	Listed        int       `desc:"apps the list shows under the filter"`
	RunningFilter string    `desc:"the running tab's filter: a case-insensitive substring of a window's app id, app name, title or task titles"`
	Registered    int       `desc:"apps the window read from keelson('apps'); 0 before the first read"`
	Reads         bool      `desc:"the window has a bus to read the introspection tables through"`
	PageReadAt    time.Time `desc:"when the open page's tables were last read; zero while unread"`
}

// ShowAppArgs opens an app's page.
type ShowAppArgs struct {
	AppId string `desc:"a registered app id; empty shows the app list alone"`
}

// SetViewArgs switches the tab.
type SetViewArgs struct {
	View string `desc:"running or apps"`
}

// RunningWindow is one open window as the Running tab last read it.
type RunningWindow struct {
	Key          uint64   `desc:"the window key, keelson('windows').key"`
	AppId        string   `desc:"the app the window runs"`
	Title        string   `desc:"the window's title, as the app sets it"`
	Active       bool     `desc:"the window input goes to"`
	Sampled      bool     `desc:"keelson('frame_times') holds frames of it; the times are 0 otherwise"`
	P50Us        int64    `desc:"median wall time of its Frame on the render goroutine, in microseconds"`
	P95Us        int64    `desc:"95th percentile of the same"`
	MessagesMean float64  `desc:"FFFI messages per Frame, on average"`
	Tasks        int      `desc:"tasks in flight it registered with the task API; a lower bound of its background work"`
	OldestTask   string   `desc:"the title of the oldest of them; empty when none"`
	OldestSince  string   `desc:"when the oldest started, RFC 3339; empty when none"`
	AgentTasks   []string `desc:"the agent tasks whose grant holds the window"`
	CpuPct       float64  `desc:"share of one core its goroutines, its tasks' included, used during the last measure_cpu, in percent; -1 when not measured: before any measurement, or opened since"`
}

// RunningTask is a task in flight that no open window owns.
type RunningTask struct {
	Title            string `desc:"the task's title"`
	Kind             string `desc:"the task's kind"`
	State            string `desc:"running, cancelling or abandoned"`
	OwnerAppId       string `desc:"the app that started it; empty for a runtime service"`
	OwnerInstanceKey uint64 `desc:"the window that started it, now closed; 0 for none"`
	CreatedAt        string `desc:"when it started, RFC 3339"`
}

// Running is list_running's result.
type Running struct {
	Windows   []RunningWindow `desc:"every open window, slowest p95 first, then those with no frames in the order they opened"`
	Unowned   []RunningTask   `desc:"tasks in flight that no open window owns: a closed window's work still finishing, a runtime service's"`
	LoopP50Us int64           `desc:"the render loop's median period, in microseconds; 0 when unread"`
	ReadAt    time.Time       `desc:"when the tab last read its tables; zero when it has not, since it reads only while shown"`
	Truncated bool            `desc:"a list was cut to 200 entries"`
	Cpu       CpuMeasurement  `desc:"the last measure_cpu, which the windows' CpuPct come from"`
}

// CpuMeasurement is what the last measure_cpu covered.
type CpuMeasurement struct {
	Measuring    bool      `desc:"a measurement is running"`
	Error        string    `desc:"why the last measurement failed; empty when it did not"`
	EndedAt      time.Time `desc:"when the last measurement ended; zero before one"`
	Seconds      float64   `desc:"how long it sampled"`
	AllPct       float64   `desc:"the whole process, in percent of one core"`
	HostPct      float64   `desc:"what no window's goroutines account for: the garbage collector, the render loop outside the windows' Frame, the host's own loops and tasks no window owns"`
	ClosedPct    float64   `desc:"windows measured and since closed"`
	ProfileAlias string    `desc:"the ad-hoc dataset holding the profile, keelson('<alias>'), split by labels['boxer.window']; empty when it was not published"`
}

// MeasureCpuArgs starts a measurement.
type MeasureCpuArgs struct {
	Seconds int `desc:"how long to sample, 1 to 30; 0 means 5"`
}

// SetRunningFilterArgs sets the running tab's filter.
type SetRunningFilterArgs struct {
	Text string `desc:"a case-insensitive substring of a window's app id, app name, title or one of its tasks' titles; a task no open window owns matches on its title, kind or owner; empty shows every window"`
}

// SetFilterArgs sets the list's filter.
type SetFilterArgs struct {
	Text string `desc:"a case-insensitive substring of an app's id, name or summary; empty lists every app"`
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resView, "the tab shown: the open windows, or the apps", func(inst *App) any { return inst.view })
	s.Restorable(resView, func(inst *App, v any) bool {
		view, ok := v.(viewE)
		if ok {
			inst.setView(view)
		}
		return ok
	})
	s.Resource(resSelection, "the app whose page is open", func(inst *App) any { return inst.selected })
	s.Restorable(resSelection, func(inst *App, v any) bool {
		id, ok := v.(string)
		if ok {
			// The selection alone: the tab is its own resource.
			inst.selected = id
			inst.mu.Lock()
			inst.want = id
			inst.mu.Unlock()
			inst.markDirty()
		}
		return ok
	})
	s.Resource(resFilter, "the list's filter text", func(inst *App) any { return inst.filter })
	s.Restorable(resFilter, func(inst *App, v any) bool {
		f, ok := v.(string)
		if ok {
			inst.filter = f
		}
		return ok
	})
	s.Editing(resFilter, func(inst *App) bool { return appops.WidgetEditing(inst.filterH) })
	s.Resource(resRunningFilter, "the running tab's filter text", func(inst *App) any { return inst.runFilter })
	s.Restorable(resRunningFilter, func(inst *App, v any) bool {
		f, ok := v.(string)
		if ok {
			inst.runFilter = f
		}
		return ok
	})
	s.Editing(resRunningFilter, func(inst *App) bool { return appops.WidgetEditing(inst.runFilterH) })
	s.Resource(resCpu, "the running tab's last CPU measurement", func(inst *App) any {
		if inst.cpu.last == nil {
			return time.Time{}
		}
		return inst.cpu.last.at
	})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the tab shown, which app's page is open and the two filters; a page's rows are the keelson() tables it is assembled from",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{View: viewName(sn.view), Selected: sn.selected, Filter: sn.filter, Listed: sn.listed,
				RunningFilter: sn.runFilter, Registered: sn.registered, Reads: sn.reads}
			if sn.view == viewApps && sn.pageFor == sn.selected && sn.selected != "" {
				out.PageReadAt = sn.pageRead
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opListRunning, Version: 1,
		Summary: "read the running tab's rows as it last read them, whatever its filter: every open window with its frame time and tasks, and the tasks no open window owns; set_view running shows the tab, which reads them",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out Running, err error) {
			out = runningResult(sn.running, sn.runningAt, sn.cpu, sn.cpuState)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opMeasureCpu, Version: 1,
		Summary: "sample the process's CPU for a few seconds and split it by window; list_running carries the result once it ends; refused while one runs",
		Effect:  app.OperationEffectView, Writes: []string{resCpu}, Agents: true,
		Gesture: "the running tab's Measure CPU", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in MeasureCpuArgs) (out appops.None, err error) {
			if in.Seconds < 0 || in.Seconds > cpuMaxSeconds {
				err = app.RefuseOperation("seconds is 1 to 30, or 0 for 5")
				return
			}
			if mErr := inst.measureCpu(in.Seconds); mErr != nil {
				err = app.RefuseOperation(mErr.Error())
			}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetView, Version: 1,
		Summary: "show the running tab, every open window, or the apps tab; the selection stays",
		Effect:  app.OperationEffectView, Writes: []string{resView}, Agents: true,
		Gesture: "the Running / Apps tabs", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetViewArgs) (out appops.None, err error) {
			v, err := parseView(in.View)
			if err != nil {
				return
			}
			if inst.view != v {
				inst.setView(v)
			}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opShowApp, Version: 1,
		Summary: "show the apps tab on an app's page, which reads its tables; an empty id shows the app list alone",
		Effect:  app.OperationEffectView, Writes: []string{resSelection, resView}, Agents: true,
		Gesture: "a row in the list", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in ShowAppArgs) (out appops.None, err error) {
			if in.AppId != "" {
				if _, ok := app.LookupManifest(app.AppIdT(in.AppId)); !ok {
					err = app.RefuseOperation(notRegisteredReason)
					return
				}
			}
			if inst.selected != in.AppId || inst.view != viewApps {
				inst.selectApp(in.AppId)
			}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetFilter, Version: 1,
		Summary: "set the list's filter text",
		Effect:  app.OperationEffectView, Writes: []string{resFilter}, Agents: true,
		Gesture: "the filter box", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetFilterArgs) (out appops.None, err error) {
			inst.filter = in.Text
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetRunningFilter, Version: 1,
		Summary: "set the running tab's filter text",
		Effect:  app.OperationEffectView, Writes: []string{resRunningFilter}, Agents: true,
		Gesture: "the running tab's filter box", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetRunningFilterArgs) (out appops.None, err error) {
			inst.runFilter = in.Text
			return
		})

	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

// gestureOr routes the person's control through the catalog (ADR-0345
// §SD3), so the command log sees it as theirs, and applies it directly where
// no host serves the catalog (tests).
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

func (inst *App) selectGesture(id string) {
	gestureOr(inst, opShowApp, ShowAppArgs{AppId: id}, func() { inst.selectApp(id) })
}

func (inst *App) measureCpuGesture() {
	gestureOr(inst, opMeasureCpu, MeasureCpuArgs{}, func() {
		if err := inst.measureCpu(0); err != nil {
			inst.logger.Debug().Err(err).Msg("appcenter: measure CPU")
		}
	})
}

func (inst *App) setViewGesture(v viewE) {
	gestureOr(inst, opSetView, SetViewArgs{View: viewName(v)}, func() { inst.setView(v) })
}

// runningResult is the tab's rows as list_running returns them, bounded.
func runningResult(v runningView, at time.Time, cpu *cpuMeasurement, cpuState bgjob.Snapshot) (out Running) {
	out.LoopP50Us, out.ReadAt = v.loopP50Us, at
	out.Cpu.Measuring = cpuState.State == bgjob.StateRunning
	if cpuState.State == bgjob.StateFailed && cpuState.Err != nil {
		out.Cpu.Error = cpuState.Err.Error()
	}
	if cpu != nil {
		out.Cpu.EndedAt, out.Cpu.Seconds = cpu.at, cpu.length.Seconds()
		out.Cpu.AllPct, out.Cpu.HostPct, out.Cpu.ClosedPct = cpu.share(cpu.total), cpu.share(cpu.host), cpu.share(cpu.closed(&v))
		if cpu.published {
			out.Cpu.ProfileAlias = cpuDatasetAlias
		}
	}
	rows, unowned := v.rows, v.unowned
	if len(rows) > runningListed {
		rows, out.Truncated = rows[:runningListed], true
	}
	if len(unowned) > runningListed {
		unowned, out.Truncated = unowned[:runningListed], true
	}
	out.Windows = make([]RunningWindow, 0, len(rows))
	for _, r := range rows {
		out.Windows = append(out.Windows, RunningWindow{Key: r.key, AppId: r.appId, Title: r.title, Active: r.active,
			Sampled: r.sampled, P50Us: r.p50Us, P95Us: r.p95Us, MessagesMean: r.messages,
			Tasks: r.tasks, OldestTask: r.oldestTitle, OldestSince: r.oldestCreateAt, AgentTasks: r.agentTasks,
			CpuPct: cpuPctOrNone(cpu, r.key)})
	}
	out.Unowned = make([]RunningTask, 0, len(unowned))
	for _, u := range unowned {
		out.Unowned = append(out.Unowned, RunningTask{Title: u.title, Kind: u.kind, State: u.state,
			OwnerAppId: u.ownerAppId, OwnerInstanceKey: u.ownerKey, CreatedAt: u.createdAt})
	}
	return
}

// cpuPctOrNone is the window's measured share, or -1.
func cpuPctOrNone(cpu *cpuMeasurement, key uint64) float64 {
	pct, ok := cpu.windowShare(key)
	if !ok {
		return -1
	}
	return pct
}
