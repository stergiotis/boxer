package helphost

import (
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// The operations catalog (ADR-0345 §SD1, §SD6 M3). The help center's state
// is the library's, which `runtime.agent.help` already serves to a model;
// what the window adds is a view — which book and document is open, the
// nav's expansion, the search box, rendered or source — and that view is
// all the catalog carries. Commands set it and never toggle.

const (
	resSelection = "selection"
	resExpanded  = "expanded"
	resViewMode  = "view_mode"
	resSearch    = "search"

	opGetView     = "get_view"
	opShowDoc     = "show_doc"
	opSetExpanded = "set_expanded"
	opSetViewMode = "set_view_mode"
	opSetSearch   = "set_search"
)

const (
	viewModeRenderedName = "rendered"
	viewModeSourceName   = "source"
)

// opsMaxExpanded bounds the expanded books get_view lists.
const opsMaxExpanded = 64

// expandedValue is the expanded resource's value: the expanded books' ids,
// sorted and joined, so a map compares as one value.
func (inst *HelpHost) expandedValue() (v string) {
	ids := make([]string, 0, len(inst.expandedApps))
	for id, on := range inst.expandedApps {
		if on {
			ids = append(ids, string(id))
		}
	}
	sort.Strings(ids)
	v = strings.Join(ids, "\n")
	return
}

func (inst *HelpHost) applyExpandedValue(v string) {
	inst.expandedApps = make(map[app.AppIdT]bool, 8)
	if v == "" {
		return
	}
	for _, id := range strings.Split(v, "\n") {
		inst.expandedApps[app.AppIdT(id)] = true
	}
}

func (inst *HelpHost) setExpanded(id app.AppIdT, expanded bool) {
	if inst.expandedApps == nil {
		inst.expandedApps = make(map[app.AppIdT]bool, 4)
	}
	if expanded {
		inst.expandedApps[id] = true
	} else {
		delete(inst.expandedApps, id)
	}
}

func viewModeName(m ViewModeE) (s string) {
	s = viewModeRenderedName
	if m == ViewModeSource {
		s = viewModeSourceName
	}
	return
}

func parseViewMode(s string) (m ViewModeE, err error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case viewModeRenderedName, "":
		m = ViewModeRendered
	case viewModeSourceName:
		m = ViewModeSource
	default:
		err = app.RefuseOperation("mode is " + viewModeRenderedName + " or " + viewModeSourceName)
	}
	return
}

// opsSnap is what queries read: the view as the frame drew it.
type opsSnap struct {
	ref      help.RefT
	expanded []string
	viewMode ViewModeE
	search   string
	hits     int
	books    int
}

func (inst *HelpHost) opsSnapshot() (sn opsSnap) {
	sn.ref = inst.CurrentRef()
	sn.viewMode = inst.viewMode
	sn.search = inst.searchText
	sn.hits = len(inst.searchHits)
	sn.books = len(inst.lib.Books())
	if v := inst.expandedValue(); v != "" {
		sn.expanded = strings.Split(v, "\n")
		if len(sn.expanded) > opsMaxExpanded {
			sn.expanded = sn.expanded[:opsMaxExpanded]
		}
	}
	return
}

// View is get_view's result: what the reader shows and where the nav is.
type View struct {
	AppId    string   `desc:"the app whose help book is open; empty shows the library overview"`
	Doc      string   `desc:"the open document's path within the book, such as overview or howto/replay; empty when no document is open"`
	Section  string   `desc:"the section slug the reader scrolled to; empty for the top"`
	ViewMode string   `desc:"rendered or source"`
	Expanded []string `desc:"app ids whose book is expanded in the nav, at most 64"`
	Search   string   `desc:"the nav's search box; non-empty replaces the book tree with hits"`
	Hits     int      `desc:"section hits the search found"`
	Books    int      `desc:"help books in the library"`
}

// ShowDocArgs opens a document, or the overview.
type ShowDocArgs struct {
	AppId   string `desc:"the app whose book to open; empty shows the library overview"`
	Doc     string `desc:"the document's path within the book; empty opens the book with no document"`
	Section string `desc:"a section slug to scroll to; empty for the top"`
}

// SetExpandedArgs expands or collapses one book in the nav.
type SetExpandedArgs struct {
	AppId    string `desc:"the app whose book row to expand or collapse"`
	Expanded bool   `desc:"true expands, false collapses"`
}

// SetViewModeArgs sets the reader pane's mode.
type SetViewModeArgs struct {
	Mode string `desc:"rendered or source"`
}

// SetSearchArgs sets the nav's search box.
type SetSearchArgs struct {
	Query string `desc:"space-separated regular expressions, ANDed; empty restores the book tree"`
}

const (
	noBookReason = "no help book is registered for that app; runtime.agent.help lists the apps with help"
	noDocReason  = "the book has no document at that path; runtime.agent.help lists the book's documents"
	noSecReason  = "the document has no section with that slug; runtime.agent.help lists its sections"
)

// showDoc applies a selection after checking it against the library.
func (inst *HelpHost) showDoc(in ShowDocArgs) (err error) {
	if in.AppId == "" {
		inst.selectedAppId, inst.selectedDoc, inst.selectedSection = "", "", ""
		return
	}
	id := app.AppIdT(in.AppId)
	b, ok := inst.lib.Book(id)
	if !ok {
		err = app.RefuseOperation(noBookReason)
		return
	}
	if in.Doc != "" {
		if _, _, found := b.Doc(in.Doc); !found {
			err = app.RefuseOperation(noDocReason)
			return
		}
		if in.Section != "" && !b.HasSection(in.Doc, in.Section) {
			err = app.RefuseOperation(noSecReason)
			return
		}
	} else if in.Section != "" {
		err = app.RefuseOperation("a section needs a document")
		return
	}
	inst.selectedAppId, inst.selectedDoc, inst.selectedSection = id, in.Doc, in.Section
	inst.setExpanded(id, true)
	return
}

var ops = func() (s *appops.Set[*HelpHost, opsSnap]) {
	s = appops.NewSet((*HelpHost).opsSnapshot)

	s.Resource(resSelection, "the open book, document and section", func(inst *HelpHost) any { return inst.CurrentRef() })
	s.Restorable(resSelection, func(inst *HelpHost, v any) bool {
		ref, ok := v.(help.RefT)
		if ok {
			inst.selectedAppId, inst.selectedDoc, inst.selectedSection = ref.AppId, ref.Doc, ref.Section
		}
		return ok
	})
	s.Resource(resExpanded, "the books expanded in the nav", func(inst *HelpHost) any { return inst.expandedValue() })
	s.Restorable(resExpanded, func(inst *HelpHost, v any) bool {
		e, ok := v.(string)
		if ok {
			inst.applyExpandedValue(e)
		}
		return ok
	})
	s.Resource(resViewMode, "rendered markdown or the document's source", func(inst *HelpHost) any { return inst.viewMode })
	s.Restorable(resViewMode, func(inst *HelpHost, v any) bool {
		m, ok := v.(ViewModeE)
		if ok {
			inst.viewMode = m
		}
		return ok
	})
	s.Resource(resSearch, "the nav's search box", func(inst *HelpHost) any { return inst.searchText })
	s.Restorable(resSearch, func(inst *HelpHost, v any) bool {
		q, ok := v.(string)
		if ok {
			inst.searchText = q
		}
		return ok
	})
	s.Editing(resSearch, func(inst *HelpHost) bool { return appops.WidgetEditing(inst.searchH) })

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read which book, document and section the reader shows, the nav's expansion and search, and the view mode; the help text itself is served by runtime.agent.help",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{AppId: string(sn.ref.AppId), Doc: sn.ref.Doc, Section: sn.ref.Section,
				ViewMode: viewModeName(sn.viewMode), Expanded: sn.expanded, Search: sn.search, Hits: sn.hits, Books: sn.books}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opShowDoc, Version: 1,
		Summary: "open a book's document in the reader, scrolled to a section, and expand the book in the nav; an empty app id shows the library overview",
		Effect:  app.OperationEffectView, Writes: []string{resSelection, resExpanded}, Agents: true,
		Gesture: "a document or section row in the nav, a search hit", Rebuild: app.OperationRebuildVolatile},
		func(inst *HelpHost, _ app.OperationCall, in ShowDocArgs) (out appops.None, err error) {
			err = inst.showDoc(in)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetExpanded, Version: 1,
		Summary: "expand or collapse one book's row in the nav",
		Effect:  app.OperationEffectView, Writes: []string{resExpanded}, Agents: true,
		Gesture: "a book row in the nav", Rebuild: app.OperationRebuildVolatile},
		func(inst *HelpHost, _ app.OperationCall, in SetExpandedArgs) (out appops.None, err error) {
			id := app.AppIdT(in.AppId)
			if _, ok := inst.lib.Book(id); !ok {
				err = app.RefuseOperation(noBookReason)
				return
			}
			inst.setExpanded(id, in.Expanded)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetViewMode, Version: 1,
		Summary: "show the open document rendered, or its markdown source",
		Effect:  app.OperationEffectView, Writes: []string{resViewMode}, Agents: true,
		Gesture: "the Rendered / Source toggle", Rebuild: app.OperationRebuildVolatile},
		func(inst *HelpHost, _ app.OperationCall, in SetViewModeArgs) (out appops.None, err error) {
			m, err := parseViewMode(in.Mode)
			if err != nil {
				return
			}
			inst.viewMode = m
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetSearch, Version: 1,
		Summary: "set the nav's search box; a non-empty query lists section hits across every book in place of the book tree",
		Effect:  app.OperationEffectView, Writes: []string{resSearch}, Agents: true,
		Gesture: "the search box and its clear button", Rebuild: app.OperationRebuildVolatile},
		func(inst *HelpHost, _ app.OperationCall, in SetSearchArgs) (out appops.None, err error) {
			inst.searchText = in.Query
			return
		})

	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *HelpHost) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

// gestureOr routes the person's control through the catalog (ADR-0345
// §SD3), so the command log sees it as theirs, and applies it directly
// where no host serves the catalog (tests, a host without the engine).
func gestureOr[In any](inst *HelpHost, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

func (inst *HelpHost) showDocGesture(ref help.RefT) {
	in := ShowDocArgs{AppId: string(ref.AppId), Doc: ref.Doc, Section: ref.Section}
	gestureOr(inst, opShowDoc, in, func() { _ = inst.showDoc(in) })
}

func (inst *HelpHost) setExpandedGesture(id app.AppIdT, expanded bool) {
	gestureOr(inst, opSetExpanded, SetExpandedArgs{AppId: string(id), Expanded: expanded},
		func() { inst.setExpanded(id, expanded) })
}

func (inst *HelpHost) setViewModeGesture(m ViewModeE) {
	gestureOr(inst, opSetViewMode, SetViewModeArgs{Mode: viewModeName(m)}, func() { inst.viewMode = m })
}

func (inst *HelpHost) setSearchGesture(q string) {
	gestureOr(inst, opSetSearch, SetSearchArgs{Query: q}, func() { inst.searchText = q })
}
