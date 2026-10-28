package launcher

import (
	"strings"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The operations catalog (ADR-0345 §SD1, §SD6 M3). The launcher's rows are
// `keelson('apps')` and opening one is the host's `launch` service, so the
// catalog carries the view alone: the filter — query, kind toggles, topic
// chips — and the selected row. Both belong to the one component every
// launcher window and the host's empty-state pane draw (ADR-0214 §SD2), so
// they are process-wide, and as ADR-0329 §SD3 does for imztop, one
// controlling window writes them: the first to draw, then the next after it
// unmounts. Every other window reports a fixed value and refuses commands,
// so only the controller ever sees the resources move.

const (
	resFilter    = "filter"
	resSelection = "selection"

	opGetView   = "get_view"
	opSetFilter = "set_filter"
	opShowApp   = "show_app"
)

const (
	notControllerReason = "another launcher window holds the filter and the selection; share that window instead"
	noControllerReason  = "no launcher window holds the filter and the selection yet; the role is claimed when a window draws"
	notListedReason     = "that app is not in the launcher's current list; set_filter to include it, or read keelson('apps') for every app"
)

// controller is the window that writes the component's state. The first
// window to draw claims it; it is released on Unmount and claimed by the next
// window that draws.
var controller atomic.Pointer[launcherApp]

func (inst *launcherApp) claimController() { controller.CompareAndSwap(nil, inst) }

func (inst *launcherApp) releaseController() { controller.CompareAndSwap(inst, nil) }

func (inst *launcherApp) isController() (ok bool) { return controller.Load() == inst }

func refuseUnlessController(inst *launcherApp) (err error) {
	switch controller.Load() {
	case inst:
	case nil:
		err = app.RefuseOperation(noControllerReason)
	default:
		err = app.RefuseOperation(notControllerReason)
	}
	return
}

// kindNameList and topicNameList spell the accepted names for a refusal.
func kindNameList() (s string) {
	names := make([]string, 0, len(app.AllKinds))
	for _, k := range app.AllKinds {
		names = append(names, k.String())
	}
	s = strings.Join(names, ", ")
	return
}

func topicNameList() (s string) {
	names := make([]string, 0, len(app.AllTopics))
	for _, t := range app.AllTopics {
		names = append(names, t.String())
	}
	s = strings.Join(names, ", ")
	return
}

// parseFilterArgs turns a caller's filter into the component's value. Kinds
// are the kinds shown, empty meaning all; topics the topics selected, empty
// meaning no restriction — the chips' own polarity (filter.go).
func parseFilterArgs(in SetFilterArgs) (f filterT, err error) {
	f.query = strings.TrimSpace(in.Query)
	if len(in.Kinds) > 0 {
		shown := kindFilterT(0)
		for _, name := range in.Kinds {
			found := false
			for _, k := range app.AllKinds {
				if k.String() == strings.ToLower(strings.TrimSpace(name)) {
					shown |= 1 << k
					found = true
				}
			}
			if !found {
				err = app.RefuseOperation("kinds are among " + kindNameList())
				return
			}
		}
		// The component stores what is hidden.
		for _, k := range app.AllKinds {
			if shown&(1<<k) == 0 {
				f.kinds = f.kinds.toggled(k)
			}
		}
	}
	for _, name := range in.Topics {
		idx, ok := topicIndex(app.TopicT(strings.ToLower(strings.TrimSpace(name))))
		if !ok {
			err = app.RefuseOperation("topics are among " + topicNameList())
			return
		}
		if !f.topics.selectedAt(idx) {
			f.topics = f.topics.toggledAt(idx)
		}
	}
	return
}

// applyFilter sets the component's filter whole: the query, the kind toggles
// behind their checkboxes, and the chips.
func (inst *Inst) applyFilter(f filterT) {
	inst.searchText = f.query
	inst.ensureKindShown()
	for i, k := range app.AllKinds {
		inst.kindShown[i] = f.kinds.shows(k)
	}
	inst.appliedKinds = f.kinds
	inst.topicFilter = f.topics
}

// shownKindNames and selectedTopicNames are the filter as get_view spells it.
func shownKindNames(f kindFilterT) (names []string) {
	for _, k := range app.AllKinds {
		if f.shows(k) {
			names = append(names, k.String())
		}
	}
	return
}

func selectedTopicNames(f topicFilterT) (names []string) {
	for i, t := range app.AllTopics {
		if f.selectedAt(i) {
			names = append(names, t.String())
		}
	}
	return
}

// selectAppRow moves the cursor to id's row in the current list and selects
// it; false when the list does not hold it.
func (inst *Inst) selectAppRow(id app.AppIdT) (ok bool) {
	rows := inst.buildRows(inst.visibleManifests())
	for i := range rows {
		if rows[i].heading == "" && rows[i].m.Id == id {
			inst.cursor = i
			inst.selected = id
			ok = true
			return
		}
	}
	return
}

// opsSnap is what queries read: the component's view as the frame drew it.
type opsSnap struct {
	controller     bool
	controllerFree bool
	filter         filterT
	selected       app.AppIdT
	rows           int
	total          int
}

func (inst *launcherApp) opsSnapshot() (sn opsSnap) {
	cur := controller.Load()
	sn.controller, sn.controllerFree = cur == inst, cur == nil
	c := inst.inst
	sn.filter = c.filterState()
	sn.selected = c.selected
	visible := c.visibleManifests()
	sn.total = len(c.registry.LaunchableManifests())
	sn.rows = countAppRows(c.buildRows(visible))
	return
}

// View is get_view's result.
type View struct {
	Query      string   `desc:"the search box; space-separated regular expressions, ANDed"`
	Kinds      []string `desc:"the kinds shown: app, applet, demo"`
	Topics     []string `desc:"the topics selected; empty means no restriction"`
	Selected   string   `desc:"the app id the detail pane describes; empty when the list is empty"`
	Rows       int      `desc:"app rows the list shows under the filter; browsing lists an app under every topic it declares"`
	Total      int      `desc:"launchable apps in the registry; keelson('apps') lists them"`
	Controller bool     `desc:"this window writes the filter and the selection, which every launcher window shares"`
}

// SetFilterArgs sets the filter whole.
type SetFilterArgs struct {
	Query  string   `desc:"space-separated regular expressions, ANDed; empty browses by topic"`
	Kinds  []string `desc:"the kinds to show, among app, applet, demo; empty shows all"`
	Topics []string `desc:"the topics to restrict to; empty means no restriction"`
}

// ShowAppArgs selects an app's row, which the detail pane describes.
type ShowAppArgs struct {
	AppId string `desc:"an app id the current list holds"`
}

var ops = func() (s *appops.Set[*launcherApp, opsSnap]) {
	s = appops.NewSet((*launcherApp).opsSnapshot)

	s.Resource(resFilter, "the query, the kind toggles and the topic chips; shared by every launcher window, written through the controlling one",
		func(inst *launcherApp) any {
			if !inst.isController() {
				return filterT{}
			}
			return inst.inst.filterState()
		})
	s.Restorable(resFilter, func(inst *launcherApp, v any) bool {
		f, ok := v.(filterT)
		if !ok || !inst.isController() {
			return false
		}
		inst.inst.applyFilter(f)
		return true
	})
	s.Editing(resFilter, func(inst *launcherApp) bool { return appops.WidgetEditing(inst.inst.queryH) })
	s.Resource(resSelection, "the app the detail pane describes; shared by every launcher window, written through the controlling one",
		func(inst *launcherApp) any {
			if !inst.isController() {
				return app.AppIdT("")
			}
			return inst.inst.selected
		})
	s.Restorable(resSelection, func(inst *launcherApp, v any) bool {
		id, ok := v.(app.AppIdT)
		if !ok || !inst.isController() {
			return false
		}
		if id == "" {
			return true
		}
		return inst.inst.selectAppRow(id)
	})

	controllerOnly := func(sn opsSnap) (ok bool, reason string) {
		switch {
		case sn.controller:
			ok = true
		case sn.controllerFree:
			reason = noControllerReason
		default:
			reason = notControllerReason
		}
		return
	}
	s.Available(opSetFilter, controllerOnly)
	s.Available(opShowApp, controllerOnly)

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the launcher's filter, its selection and how many apps the list shows; the apps themselves are keelson('apps'), and opening one is the host's launch",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Query: sn.filter.query, Kinds: shownKindNames(sn.filter.kinds), Topics: selectedTopicNames(sn.filter.topics),
				Selected: string(sn.selected), Rows: sn.rows, Total: sn.total, Controller: sn.controller}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetFilter, Version: 1,
		Summary: "set the launcher's filter whole: the query, the kinds shown and the topics selected",
		Effect:  app.OperationEffectView, Writes: []string{resFilter}, Agents: true,
		Follows: []string{"a changed filter moves the selection to the list's first row"},
		Gesture: "the search box and its clear button, the kind checkboxes, the topic chips", Rebuild: app.OperationRebuildVolatile},
		func(inst *launcherApp, _ app.OperationCall, in SetFilterArgs) (out appops.None, err error) {
			if err = refuseUnlessController(inst); err != nil {
				return
			}
			f, err := parseFilterArgs(in)
			if err != nil {
				return
			}
			inst.inst.applyFilter(f)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opShowApp, Version: 1,
		Summary: "select an app's row, so the detail pane describes it; the app must be in the list the filter shows",
		Effect:  app.OperationEffectView, Writes: []string{resSelection}, Agents: true,
		Gesture: "a row click, the arrow keys", Rebuild: app.OperationRebuildVolatile},
		func(inst *launcherApp, _ app.OperationCall, in ShowAppArgs) (out appops.None, err error) {
			if err = refuseUnlessController(inst); err != nil {
				return
			}
			if !inst.inst.selectAppRow(app.AppIdT(in.AppId)) {
				err = app.RefuseOperation(notListedReason)
			}
			return
		})

	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *launcherApp) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

// gestureOr routes the person's control through the catalog (ADR-0345
// §SD3), so the command log sees it as theirs, and applies it directly where
// no host serves the catalog: the empty-state pane, tests, a window that is
// not the controller (ADR-0329 §SD3).
func gestureOr[In any](inst *Inst, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

// filterArgs spells f as a caller would.
func filterArgs(f filterT) (in SetFilterArgs) {
	in = SetFilterArgs{Query: f.query, Topics: selectedTopicNames(f.topics)}
	if f.kinds.hidesAnything() {
		in.Kinds = shownKindNames(f.kinds)
	}
	return
}

// noticeKindToggles routes a kind checkbox the person clicked. A checkbox
// writes its bool at Sync, after the frame, so the mask it produced is
// noticed here on the next frame, once, and routed as the person's gesture
// (ADR-0345 §SD3); the handler sets appliedKinds, so it fires once.
func (inst *Inst) noticeKindToggles() {
	if kf := inst.kindFilter(); kf != inst.appliedKinds {
		inst.setFilterGesture(inst.filterState())
	}
}

// setFilterGesture sets the whole filter as the person's gesture.
func (inst *Inst) setFilterGesture(f filterT) {
	gestureOr(inst, opSetFilter, filterArgs(f), func() { inst.applyFilter(f) })
}

// selectGesture moves the cursor to row as the person's gesture; the direct
// path is the cursor write the click or key made.
func (inst *Inst) selectGesture(id app.AppIdT, row int) {
	gestureOr(inst, opShowApp, ShowAppArgs{AppId: string(id)}, func() { inst.cursor = row })
}
