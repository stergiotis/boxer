package appcenter

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The operations catalog (ADR-0345 §SD1, §SD6 M3). Every row a page shows is
// a `keelson()` table already, so the catalog carries the view alone: which
// app's page is open, and the list's filter. Commands set them and never
// toggle.

const (
	resSelection = "selection"
	resFilter    = "filter"

	opGetView   = "get_view"
	opShowApp   = "show_app"
	opSetFilter = "set_filter"
)

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
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.selected, sn.filter = inst.selected, inst.filter
	snap := inst.snapshot()
	sn.reads = snap.reads
	sn.registered = len(snap.global.apps.cols.Id)
	sn.listed = len(matchingApps(&snap.global.apps.cols, inst.filter))
	sn.pageRead, sn.pageFor = snap.page.read, snap.page.appId
	return
}

// View is get_view's result.
type View struct {
	Selected   string    `desc:"the app whose page is open; empty shows the list alone"`
	Filter     string    `desc:"the list's filter: a case-insensitive substring of an app's id, name or summary"`
	Listed     int       `desc:"apps the list shows under the filter"`
	Registered int       `desc:"apps the window read from keelson('apps'); 0 before the first read"`
	Reads      bool      `desc:"the window has a bus to read the introspection tables through"`
	PageReadAt time.Time `desc:"when the open page's tables were last read; zero while unread"`
}

// ShowAppArgs opens an app's page.
type ShowAppArgs struct {
	AppId string `desc:"a registered app id; empty closes the page and shows the list alone"`
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

	appops.Command(s, app.OperationSpec{Name: opShowApp, Version: 1,
		Summary: "open an app's page, which reads its tables; an empty id closes the page",
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
