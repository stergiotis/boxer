package trailviewer

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Saved views (ADR-0350 §SD11, proposed): a case, a window, a filter and a
// tab under a name, kept as one small JSON document in the persist store
// under the viewer's app id. The window reads the list once at mount and
// writes it whole on each save or delete, off the frame; two windows
// saving at once keep the later write. A launch config may name a view,
// which the window opens once the list is read.

// viewsKey is the persist key the list is stored under; the manifest
// declares it, so the host grants the persist subject.
const viewsKey = "views"

// viewsVersion is the document's revision; a document of another revision
// is not read.
const viewsVersion = 1

// maxSavedViews bounds the list.
const maxSavedViews = 200

// SavedView is one saved view as the store holds it. The window is the
// span ending now when Last is set, else From and To; FromExpr, ToExpr and
// Tz are the picker's expressions, shown again when the view opens.
type SavedView struct {
	Name     string    `json:"name"`
	Case     string    `json:"case"`
	Value    string    `json:"value,omitempty"`
	Last     string    `json:"last,omitempty"`
	From     time.Time `json:"from,omitzero"`
	To       time.Time `json:"to,omitzero"`
	FromExpr string    `json:"fromExpr,omitempty"`
	ToExpr   string    `json:"toExpr,omitempty"`
	Tz       string    `json:"tz,omitempty"`
	Filter   []TermArg `json:"filter,omitempty"`
	Tab      string    `json:"tab,omitempty"`
}

type savedViewsDoc struct {
	Version int         `json:"version"`
	Views   []SavedView `json:"views"`
}

func encodeViews(views []SavedView) (b []byte, err error) {
	b, err = json.Marshal(savedViewsDoc{Version: viewsVersion, Views: views})
	return
}

func decodeViews(b []byte) (views []SavedView, err error) {
	var doc savedViewsDoc
	if err = json.Unmarshal(b, &doc); err != nil {
		return nil, eh.Errorf("trailviewer: the saved views do not read: %w", err)
	}
	if doc.Version != viewsVersion {
		return nil, eh.Errorf("trailviewer: the saved views are of revision %d, this build reads %d", doc.Version, viewsVersion)
	}
	return doc.Views, nil
}

// loadViews reads the list; none stored is an empty list.
func loadViews(st app.StorageI) (views []SavedView, err error) {
	b, found, err := st.Get(viewsKey)
	if err != nil || !found || len(b) == 0 {
		return
	}
	return decodeViews(b)
}

// storeViews writes the list whole.
func storeViews(st app.StorageI, views []SavedView) (err error) {
	b, err := encodeViews(views)
	if err != nil {
		return
	}
	return st.Set(viewsKey, b)
}

// withView is views with v in place of the view of its name, or appended.
func withView(views []SavedView, v SavedView) (out []SavedView) {
	out = make([]SavedView, 0, len(views)+1)
	replaced := false
	for _, x := range views {
		if x.Name == v.Name {
			out, replaced = append(out, v), true
			continue
		}
		out = append(out, x)
	}
	if !replaced {
		out = append(out, v)
	}
	return
}

// withoutView is views without the view named name.
func withoutView(views []SavedView, name string) (out []SavedView) {
	for _, x := range views {
		if x.Name != name {
			out = append(out, x)
		}
	}
	return
}

func findView(views []SavedView, name string) (v SavedView, ok bool) {
	for _, x := range views {
		if x.Name == name {
			return x, true
		}
	}
	return
}

// currentView is the window's case, window, filter and tab under name.
func (inst *App) currentView(name string) (v SavedView) {
	v = SavedView{Name: name, Case: inst.caseValue.Kind.String(), Value: inst.caseValue.Value, Filter: termArgs(inst.filter),
		Tab: inst.tab.String(), FromExpr: inst.picker.fromExpr, ToExpr: inst.picker.toExpr, Tz: inst.picker.tzName}
	if inst.window.Last > 0 {
		v.Last = inst.window.Last.String()
	} else {
		v.From, v.To = inst.window.From, inst.window.To
	}
	return
}

// parsed is the view as the window's state, or why it does not read.
func (inst SavedView) parsed() (cs Case, w Window, f Filter, t TabE, err error) {
	if cs, err = parseCaseArgs(inst.Case, inst.Value); err != nil {
		return
	}
	if w, err = parseRangeArgs(RangeArgs{Last: inst.Last, From: inst.From, To: inst.To}); err != nil {
		return
	}
	if f, err = parseTerms(inst.Filter); err != nil {
		return
	}
	t = TabFields
	if inst.Tab != "" {
		var ok bool
		if t, ok = ParseTab(inst.Tab); !ok || !t.availableFor(cs.Kind) {
			t = TabFields
		}
	}
	return
}

// openView applies a saved view: its case, window, filter and tab, and
// the picker's expressions. The Open button and open_view both land here.
func (inst *App) openView(v SavedView) (err error) {
	cs, w, f, t, err := v.parsed()
	if err != nil {
		return app.RefuseOperation("the view " + strconv.Quote(v.Name) + " does not open: " + err.Error())
	}
	inst.applyCase(cs)
	inst.applyWindow(w)
	inst.applyFilter(f)
	inst.applyTab(t)
	if v.FromExpr != "" && v.ToExpr != "" {
		inst.picker.fromExpr, inst.picker.toExpr = v.FromExpr, v.ToExpr
		if v.Tz != "" {
			inst.picker.tzName = v.Tz
		}
	}
	inst.views.note = "opened " + v.Name
	return
}

// viewStore is the window's saved views: the list as last read or
// written, and the worker that reads and writes it.
type viewStore struct {
	store  app.StorageI
	runner bgjob.Runner[[]SavedView]
	list   []SavedView
	loaded bool
	asked  bool
	err    error
	note   string
	// wantOpen is a view a launch config named, opened once the list is
	// read.
	wantOpen string
	name     string
	// writing is the list a write in flight carries, so the views resource
	// moves with the command that wrote it rather than with the frame the
	// write lands in; nil when none is in flight.
	writing []SavedView
}

// tick adopts a finished read or write, and starts the first read.
func (inst *App) tickViews() {
	vs := &inst.views
	if res, _, ok := vs.runner.TakeResult(); ok {
		vs.list, vs.loaded, vs.err, vs.writing = *res, true, nil, nil
	}
	if snap := vs.runner.Snapshot(); snap.State == bgjob.StateFailed && snap.Err != nil {
		vs.err, vs.loaded, vs.writing = snap.Err, true, nil
		vs.runner.Invalidate()
	}
	if vs.store == nil {
		return
	}
	if !vs.asked {
		vs.asked = true
		st := vs.store
		vs.runner.Start(nil, bgjob.Spec{Kind: "trailviewer.views", Title: "Trail viewer saved views"},
			func(ctx context.Context) (out *[]SavedView, err error) {
				views, err := loadViews(st)
				if err != nil {
					return
				}
				return &views, nil
			})
		return
	}
	if vs.loaded && vs.wantOpen != "" && !vs.runner.Running() {
		name := vs.wantOpen
		vs.wantOpen = ""
		if _, ok := findView(vs.list, name); ok {
			inst.openViewGesture(name)
		} else {
			vs.err = eh.Errorf("the launch config names the view %q, which is not saved", name)
		}
	}
}

// writeViews stores list off the frame; a write in flight refuses the
// next, as a click on a busy control is dropped.
func (inst *App) writeViews(list []SavedView, note string) (err error) {
	vs := &inst.views
	switch {
	case vs.store == nil:
		return app.RefuseOperation("this window has no persist store, so a view cannot be saved or deleted")
	case vs.runner.Running():
		return app.RefuseOperation("the saved views are being written; ask again shortly")
	}
	st := vs.store
	vs.err, vs.note, vs.writing = nil, note, list
	vs.runner.Start(nil, bgjob.Spec{Kind: "trailviewer.views", Title: "Trail viewer saved views"},
		func(ctx context.Context) (out *[]SavedView, err error) {
			if err = storeViews(st, list); err != nil {
				return
			}
			return &list, nil
		})
	return
}

// saveView saves the current state under name; the Save button and
// save_view both land here.
func (inst *App) saveView(name string) (err error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return app.RefuseOperation("a saved view needs a name")
	case !inst.views.loaded:
		return app.RefuseOperation("the saved views have not been read yet; ask again shortly")
	}
	list := withView(inst.views.list, inst.currentView(name))
	if len(list) > maxSavedViews {
		return app.RefuseOperation(fmt.Sprintf("at most %d views are kept; delete one first", maxSavedViews))
	}
	return inst.writeViews(list, "saved "+name)
}

// deleteView removes the view named name; the Delete button and
// delete_view both land here.
func (inst *App) deleteView(name string) (err error) {
	name = strings.TrimSpace(name)
	if !inst.views.loaded {
		return app.RefuseOperation("the saved views have not been read yet; ask again shortly")
	}
	if _, ok := findView(inst.views.list, name); !ok {
		return app.RefuseOperation(noSuchViewReason)
	}
	return inst.writeViews(withoutView(inst.views.list, name), "deleted "+name)
}

// openViewNamed opens the saved view named name.
func (inst *App) openViewNamed(name string) (err error) {
	v, ok := findView(inst.views.list, strings.TrimSpace(name))
	if !ok {
		return app.RefuseOperation(noSuchViewReason)
	}
	return inst.openView(v)
}

const noSuchViewReason = "no saved view of that name; get_view lists them under saved_views"

// The pane's buttons route through the catalog (ADR-0345 §SD3); a
// refusal, from either path, is shown under the name box.
func (inst *App) saveViewGesture(name string) {
	gestureOr(inst, opSaveView, ViewNameArgs{Name: name}, func() { inst.views.err = inst.saveView(name) })
}

func (inst *App) deleteViewGesture(name string) {
	gestureOr(inst, opDeleteView, ViewNameArgs{Name: name}, func() { inst.views.err = inst.deleteView(name) })
}

func (inst *App) openViewGesture(name string) {
	gestureOr(inst, opOpenView, ViewNameArgs{Name: name}, func() { inst.views.err = inst.openViewNamed(name) })
}

// namesKey is the saved views' names as one comparable value, the views
// resource's: the list a write in flight carries, else the list held.
func (inst *viewStore) namesKey() string {
	list := inst.list
	if inst.writing != nil {
		list = inst.writing
	}
	names := make([]string, 0, len(list))
	for _, v := range list {
		names = append(names, v.Name)
	}
	return strings.Join(names, "\x00")
}

// viewNames lists the saved views' names, for get_view.
func (inst *viewStore) names() (names []string) {
	for _, v := range inst.list {
		names = append(names, v.Name)
	}
	return
}

// renderViews is the Views pane: a name box and Save, then each saved view
// with Open and Delete.
func (inst *App) renderViews() {
	vs := &inst.views
	for range c.ScrollArea().Vscroll(true).Hscroll(false).AutoShrink(false, false).KeepIter() {
		c.Label("A saved view keeps the case, the time range, the filter and the tab under a name.").Wrap().Send()
		for range c.HorizontalTop().KeepIter() {
			c.TextEdit(inst.ids.PrepareStr("view-name"), vs.name, false).HintText("name").DesiredWidth(150).SendRespVal(&vs.name)
			if c.Button(inst.ids.PrepareStr("view-save"), c.Atoms().Text("Save").Keep()).SendResp().HasPrimaryClicked() {
				inst.saveViewGesture(vs.name)
			}
		}
		switch {
		case vs.err != nil:
			c.LabelAtoms(c.Atoms().BeginRichTextColored(detailErrorFg, detailBgClear, vs.err.Error()).Small().End().Keep()).Wrap().Send()
		case vs.runner.Running():
			c.Label("(reading or writing the saved views…)").Send()
		case vs.note != "":
			c.LabelAtoms(c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear, vs.note).Small().End().Keep()).Send()
		}
		if vs.loaded && len(vs.list) == 0 {
			c.Label("(no saved view)").Send()
		}
		c.AddSpace(styletokens.PaddingHair(inst.density))
		for range c.IdScope(inst.ids.PrepareStr("views")) {
			for i, v := range vs.list {
				for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
					for range c.HorizontalTop().KeepIter() {
						if c.Button(inst.ids.PrepareStr("open"), c.Atoms().Text("Open").Keep()).SendResp().HasPrimaryClicked() {
							inst.openViewGesture(v.Name)
						}
						if c.Button(inst.ids.PrepareStr("delete"), c.Atoms().Text("Delete").Keep()).SendResp().HasPrimaryClicked() {
							inst.deleteViewGesture(v.Name)
						}
						c.Label(fmt.Sprintf("%s — %s %s, %s", v.Name, v.Case, v.Value, viewRange(v))).Truncate().Send()
					}
				}
			}
		}
	}
}

func viewRange(v SavedView) string {
	if v.Last != "" {
		return "last " + v.Last
	}
	return v.From.UTC().Format(time.RFC3339) + " – " + v.To.UTC().Format(time.RFC3339)
}
