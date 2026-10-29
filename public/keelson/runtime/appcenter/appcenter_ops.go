package appcenter

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The operations catalog (ADR-0345 §SD1, §SD6 M3). Every row a page shows is
// a `keelson()` table already, so the catalog carries the view alone: which
// app's page is open, and the list's filter. Commands set them and never
// toggle. The one exception is the Running page's join (ADR-0348 §SD5): its
// rows are three tables composed in Go, which no single statement returns,
// so list_running hands them out as the page last read them. Raising and
// closing a window are not operations.

const (
	resSelection = "selection"
	resFilter    = "filter"

	opGetView     = "get_view"
	opShowApp     = "show_app"
	opSetFilter   = "set_filter"
	opListRunning = "list_running"
)

// runningListed bounds what list_running returns of each list; Truncated
// says when it cut.
const runningListed = 200

const notRegisteredReason = "no app with that id is registered in this process; keelson('apps') lists them"

// opsSnap is what queries read: the view as the frame drew it.
type opsSnap struct {
	selected   string
	filter     string
	registered int
	listed     int
	reads      bool
	pageRead   time.Time
	pageFor    string
	running    runningView
	runningAt  time.Time
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.selected, sn.filter = inst.selected, inst.filter
	snap := inst.snapshot()
	sn.reads = snap.reads
	sn.registered = len(snap.global.apps.cols.Id)
	sn.listed = len(matchingApps(&snap.global.apps.cols, inst.filter))
	sn.pageRead, sn.pageFor = snap.page.read, snap.page.appId
	sn.running, sn.runningAt = snap.running.view, snap.running.read
	return
}

// View is get_view's result.
type View struct {
	Selected   string    `desc:"the app whose page is open; empty shows the Running page, every open window"`
	Filter     string    `desc:"the list's filter: a case-insensitive substring of an app's id, name or summary"`
	Listed     int       `desc:"apps the list shows under the filter"`
	Registered int       `desc:"apps the window read from keelson('apps'); 0 before the first read"`
	Reads      bool      `desc:"the window has a bus to read the introspection tables through"`
	PageReadAt time.Time `desc:"when the open page's tables were last read; zero while unread"`
}

// ShowAppArgs opens an app's page.
type ShowAppArgs struct {
	AppId string `desc:"a registered app id; empty shows the Running page, which reads the open windows"`
}

// RunningWindow is one open window as the Running page last read it.
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
	ReadAt    time.Time       `desc:"when the page last read its tables; zero when it has not, since it reads only while shown"`
	Truncated bool            `desc:"a list was cut to 200 entries"`
}

// SetFilterArgs sets the list's filter.
type SetFilterArgs struct {
	Text string `desc:"a case-insensitive substring of an app's id, name or summary; empty lists every app"`
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resSelection, "the app whose page is open", func(inst *App) any { return inst.selected })
	s.Restorable(resSelection, func(inst *App, v any) bool {
		id, ok := v.(string)
		if ok {
			inst.selectApp(id)
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

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read which app's page is open and the list's filter; a page's rows are the keelson() tables it is assembled from",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Selected: sn.selected, Filter: sn.filter, Listed: sn.listed, Registered: sn.registered, Reads: sn.reads}
			if sn.pageFor == sn.selected && sn.selected != "" {
				out.PageReadAt = sn.pageRead
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opListRunning, Version: 1,
		Summary: "read the Running page's rows as it last read them: every open window with its frame time and tasks, and the tasks no open window owns; show_app with an empty id shows the page, which reads them",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out Running, err error) {
			out = runningResult(sn.running, sn.runningAt)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opShowApp, Version: 1,
		Summary: "open an app's page, which reads its tables; an empty id shows the Running page",
		Effect:  app.OperationEffectView, Writes: []string{resSelection}, Agents: true,
		Gesture: "a row in the list", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in ShowAppArgs) (out appops.None, err error) {
			if in.AppId != "" {
				if _, ok := app.LookupManifest(app.AppIdT(in.AppId)); !ok {
					err = app.RefuseOperation(notRegisteredReason)
					return
				}
			}
			if inst.selected != in.AppId {
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

// runningResult is the page's view as list_running returns it, bounded.
func runningResult(v runningView, at time.Time) (out Running) {
	out.LoopP50Us, out.ReadAt = v.loopP50Us, at
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
			Tasks: r.tasks, OldestTask: r.oldestTitle, OldestSince: r.oldestCreateAt, AgentTasks: r.agentTasks})
	}
	out.Unowned = make([]RunningTask, 0, len(unowned))
	for _, u := range unowned {
		out.Unowned = append(out.Unowned, RunningTask{Title: u.title, Kind: u.kind, State: u.state,
			OwnerAppId: u.ownerAppId, OwnerInstanceKey: u.ownerKey, CreatedAt: u.createdAt})
	}
	return
}
