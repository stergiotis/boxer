//go:build llm_generated_opus47

// Package helphost is the keelson runtime's inline help reader app. It
// consumes [help.DefaultLibrary] and renders the indexed corpora into a
// two-pane layout: a left nav listing every app with help docs, and a
// central reader that renders the selected [markdown.Doc].
//
// The app registers itself into [app.DefaultRegistry] at init() time
// alongside every other runtime service app (logviewer, capinspector).
// Hosts that wire keelson into a windowed shell pick it up
// automatically; no explicit registration is needed in app entry
// points.
//
// Selection state lives on each HelpHost instance. Per-frame
// navigation (clicking a book to expand it, clicking a doc to read it)
// mutates that state directly; in-process callers can also set the
// initial ref via [HelpHost.OpenRef]. The bus-driven programmatic-open
// path lands in a follow-up round — until then, opening help
// programmatically means resolving the AppI from
// [app.DefaultRegistry] and calling [HelpHost.OpenRef] before Mount.
package helphost

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// ViewModeE selects between the rendered Markdown view and the raw
// .md source view in the reader pane. The mode is process-wide on
// each HelpHost instance — switching docs preserves the active mode,
// which matches the typical "show me the source for every doc I open"
// authoring workflow.
type ViewModeE uint8

const (
	// ViewModeRendered renders the parsed markdown.Doc — the default
	// experience when a user opens Help to learn how an app works.
	ViewModeRendered ViewModeE = 0
	// ViewModeSource displays the underlying .md bytes inside a
	// syntax-highlighted CodeView, useful for doc authors checking
	// frontmatter or wikilink shapes without leaving the running app.
	ViewModeSource ViewModeE = 1
)

// ManifestId is the stable AppId other code uses to address the
// HelpHost via [app.DefaultRegistry.Open] (e.g., a launcher menu, a
// future bus subscription, or a test that drives the app directly).
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/public/keelson/runtime/helphost"

// navPanelDefaultWidth is the initial width of the left nav panel in
// egui points. The user can drag the divider to resize; the value is
// chosen to fit a typical Go import-path display name (~40ch at the
// project's default font) without truncation.
const navPanelDefaultWidth = 280.0

// HelpHost is the per-tile reader instance. Each Open() from a
// factory-registered manifest yields a fresh HelpHost so two open
// tiles maintain independent selection and nav-expansion state.
//
// The library reference defaults to [help.DefaultLibrary] but tests
// can override it via [HelpHost.SetLibrary]. The host owns the
// per-instance [c.WidgetIdStack]; Mount() captures it from the
// host-supplied [app.MountContextI].
type HelpHost struct {
	manifest app.Manifest
	ids      *c.WidgetIdStack
	lib      help.LibraryI

	// Selection state. Empty AppId means "no book selected, show the
	// library overview"; empty Doc means "book selected, no doc
	// chosen". Section is reserved for the future scroll-to-anchor
	// path and is not consumed by the M1 renderer.
	selectedAppId   app.AppIdT
	selectedDoc     string
	selectedSection string

	// expandedApps tracks which book rows in the left nav are
	// expanded. Lazy-init on first toggle so a freshly-constructed
	// HelpHost is zero-cost.
	expandedApps map[app.AppIdT]bool

	// viewMode toggles between rendered markdown and raw .md source
	// in the reader pane. Default zero value is [ViewModeRendered].
	viewMode ViewModeE

	// scrolledTo remembers the (app, doc, section) the last frame
	// actually emitted a markdown.WithScrollToSection hint for. The
	// reader pane re-emits the hint only when the current selection
	// differs — re-scrolling every frame would keep snapping the
	// scrollbar back and prevent the user from reading content above
	// the anchor.
	scrolledToApp     app.AppIdT
	scrolledToDoc     string
	scrolledToSection string
}

var _ app.AppI = (*HelpHost)(nil)

// New constructs a HelpHost bound to [help.DefaultLibrary]. The
// constructor is lightweight (no I/O, no rendering); heavyweight
// resource acquisition belongs in [HelpHost.Mount]. Tests that need
// an isolated library swap one in via [HelpHost.SetLibrary] after
// construction.
func New() (h *HelpHost) {
	h = &HelpHost{
		manifest: manifest,
		ids:      c.NewWidgetIdStack(),
		lib:      help.DefaultLibrary,
	}
	return
}

// SetLibrary swaps the library this HelpHost reads from. Mainly for
// tests that want to assert against a controlled fixture instead of
// the process-wide [help.DefaultLibrary]. Panics on nil — production
// callers don't disable the library, they replace it.
func (inst *HelpHost) SetLibrary(lib help.LibraryI) {
	if lib == nil {
		panic("helphost: SetLibrary(nil)")
	}
	inst.lib = lib
}

// OpenRef sets the selection to the named ref and expands the parent
// app row in the nav. Safe to call before Mount (queues the selection
// for the first Frame) or between frames (the next Frame picks it up).
// Empty refs are a no-op.
func (inst *HelpHost) OpenRef(ref help.RefT) {
	if ref.IsZero() {
		return
	}
	inst.selectedAppId = ref.AppId
	inst.selectedDoc = ref.Doc
	inst.selectedSection = ref.Section
	if inst.expandedApps == nil {
		inst.expandedApps = make(map[app.AppIdT]bool, 4)
	}
	inst.expandedApps[ref.AppId] = true
}

// CurrentRef returns the active selection as a [help.RefT]. Useful for
// tests, breadcrumb UIs, or a future "share this help page" link
// generator.
func (inst *HelpHost) CurrentRef() (ref help.RefT) {
	ref = help.RefT{
		AppId:   inst.selectedAppId,
		Doc:     inst.selectedDoc,
		Section: inst.selectedSection,
	}
	return
}

// SetViewMode picks between rendered and raw-source view in the
// reader pane. Mainly for tests and for programmatic callers that
// want to open Help with the source view active (e.g., an "open
// source" link inside another widget). User-driven toggles go
// through the Frame's in-pane SelectableLabel pair, not this method.
func (inst *HelpHost) SetViewMode(mode ViewModeE) {
	inst.viewMode = mode
}

// ViewMode returns the current reader-pane view mode.
func (inst *HelpHost) ViewMode() (mode ViewModeE) {
	mode = inst.viewMode
	return
}

func (inst *HelpHost) Manifest() (m app.Manifest) {
	m = inst.manifest
	return
}

// Mount picks up the host-supplied per-instance WidgetIdStack. The
// host has already pre-pushed a window-unique salt onto the stack so
// widget ids derived during Frame() can't collide with another open
// app's ids in the same frame.
func (inst *HelpHost) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	return
}

func (inst *HelpHost) Unmount(ctx app.MountContextI) (err error) {
	return
}

// Frame draws the two-pane layout: a left nav listing every app with
// help docs, and a central reader for the selected doc. The host has
// already opened the window scope, so we drop straight into
// PanelLeftInside + PanelCentralInside without wrapping a Window of
// our own.
func (inst *HelpHost) Frame(ctx app.FrameContextI) (err error) {
	for range c.PanelLeftInside(inst.ids.PrepareStr("nav")).
		DefaultSize(navPanelDefaultWidth).
		Resizable(true).
		KeepIter() {
		inst.renderNav()
	}
	for range c.PanelCentralInside().KeepIter() {
		inst.renderReader()
	}
	return
}

// renderNav draws the left nav. Each book becomes an expandable row:
// the header line is a SelectableLabel that toggles inst.expandedApps
// on click; when expanded, the indented body lists every DocInfo as a
// SelectableLabel that, on click, becomes the new selection.
func (inst *HelpHost) renderNav() {
	books := inst.lib.Books()
	if len(books) == 0 {
		c.Label("No help docs registered.\n\nApps populate Manifest.Help to ship help.").Send()
		return
	}
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		for _, b := range books {
			inst.renderNavBook(b)
		}
	}
}

func (inst *HelpHost) renderNavBook(b help.BookI) {
	appId := b.AppId()
	display := bookDisplay(appId)
	expanded := inst.expandedApps[appId]
	glyph := "▶"
	if expanded {
		glyph = "▼"
	}
	if c.SelectableLabel(inst.ids.PrepareStr("nav-app-"+string(appId)), expanded, glyph+" "+display).
		SendResp().HasPrimaryClicked() {
		if inst.expandedApps == nil {
			inst.expandedApps = make(map[app.AppIdT]bool, 4)
		}
		inst.expandedApps[appId] = !expanded
	}
	if !expanded {
		return
	}
	for _, info := range b.Docs() {
		active := appId == inst.selectedAppId && info.Path == inst.selectedDoc
		label := "  " + info.Title
		if info.Type != "" {
			label += "  [" + info.Type + "]"
		}
		if c.SelectableLabel(inst.ids.PrepareStr("nav-doc-"+string(appId)+":"+info.Path), active, label).
			SendResp().HasPrimaryClicked() {
			inst.selectedAppId = appId
			inst.selectedDoc = info.Path
			inst.selectedSection = ""
		}
		if active {
			inst.renderNavSections(appId, info)
		}
	}
}

// renderNavSections lists the doc's level-2+ headings as a third
// indentation tier under the active doc row. Level-1 headings are
// skipped — there's typically one per doc and it duplicates the doc
// title that the nav row already shows. Clicking a section updates
// inst.selectedSection; the body doesn't scroll yet (scroll-to-anchor
// needs a ScrollArea.ScrollTo binding that doesn't exist today, so
// the click is currently a "you are here" marker only).
func (inst *HelpHost) renderNavSections(appId app.AppIdT, info help.DocInfo) {
	for _, s := range info.Sections {
		if s.Level < 2 {
			continue
		}
		active := s.Slug == inst.selectedSection
		// Indent by heading level so H3+ sit visually under their H2.
		indent := "    "
		for i := uint8(2); i < s.Level && i < 6; i++ {
			indent += "  "
		}
		label := indent + s.Text
		if c.SelectableLabel(inst.ids.PrepareStr("nav-sec-"+string(appId)+":"+info.Path+"#"+s.Slug), active, label).
			SendResp().HasPrimaryClicked() {
			inst.selectedSection = s.Slug
		}
	}
}

// renderReader draws the central pane. Empty selection → instructional
// placeholder; selected app+doc → the parsed markdown Doc inside a
// ScrollArea. The markdown widget's Render does not open its own
// IdScope (documented invariant), so the wrapping IdScope here is
// load-bearing — without it, two open HelpHost tiles in the same
// process would collide on the markdown widget's PrepareSeq stream.
func (inst *HelpHost) renderReader() {
	if inst.selectedAppId == "" {
		c.Label("Select a doc on the left to start reading.").Send()
		return
	}
	b, ok := inst.lib.Book(inst.selectedAppId)
	if !ok {
		c.Label("Help corpus for " + string(inst.selectedAppId) + " is not registered.").Send()
		return
	}
	if inst.selectedDoc == "" {
		c.Label(bookDisplay(inst.selectedAppId) + " — select a doc on the left.").Send()
		return
	}
	doc, _, ok := b.Doc(inst.selectedDoc)
	if !ok {
		c.Label("Doc not found: " + inst.selectedDoc).Send()
		return
	}
	// Title intentionally not rendered here — every doc carries its
	// own H1 (the Diátaxis-template starter for the new-doc.sh script
	// emits one), and the left nav already highlights the selected
	// row. A dedicated title label above the body would duplicate
	// whichever signal the user is closer to.
	inst.renderViewToggle()
	c.Separator().Horizontal().Send()
	switch inst.viewMode {
	case ViewModeSource:
		src, srcOk := b.Source(inst.selectedDoc)
		if !srcOk {
			c.Label("Source unavailable for " + inst.selectedDoc).Send()
			return
		}
		renderSource(inst.ids, src)
	default:
		section := inst.consumeScrollTarget()
		renderRendered(inst.ids, doc, section)
	}
}

// consumeScrollTarget returns the slug to pass into
// [markdown.WithScrollToSection] this frame. Returns the active
// [HelpHost.selectedSection] exactly once per selection change —
// subsequent frames with the same selection return empty so the
// markdown widget doesn't keep re-emitting the scroll op and
// fighting the user's manual scrolling.
func (inst *HelpHost) consumeScrollTarget() (section string) {
	if inst.selectedSection == "" {
		// No section pending; nothing to do. Don't bump scrolledTo —
		// future re-selection of the same (app, doc, section) tuple
		// should still trigger a scroll.
		return
	}
	if inst.scrolledToApp == inst.selectedAppId &&
		inst.scrolledToDoc == inst.selectedDoc &&
		inst.scrolledToSection == inst.selectedSection {
		return
	}
	section = inst.selectedSection
	inst.scrolledToApp = inst.selectedAppId
	inst.scrolledToDoc = inst.selectedDoc
	inst.scrolledToSection = inst.selectedSection
	return
}

// renderViewToggle draws the Rendered / Source SelectableLabel pair
// above the reader body. Two labels make the available alternatives
// discoverable (vs a single "Source" button whose return path is
// implicit); the active mode is highlighted via SelectableLabel's
// `checked` argument.
func (inst *HelpHost) renderViewToggle() {
	for range c.Horizontal().KeepIter() {
		c.Label("View:").Send()
		if c.SelectableLabel(inst.ids.PrepareStr("view-rendered"), inst.viewMode == ViewModeRendered, "Rendered").
			SendResp().HasPrimaryClicked() {
			inst.viewMode = ViewModeRendered
		}
		if c.SelectableLabel(inst.ids.PrepareStr("view-source"), inst.viewMode == ViewModeSource, "Source").
			SendResp().HasPrimaryClicked() {
			inst.viewMode = ViewModeSource
		}
	}
}

// renderRendered draws the parsed markdown body inside a scrolling
// pane. The wrapping IdScope is load-bearing per markdown.Doc.Render's
// documented invariant. scrollToSection — when non-empty — drives a
// one-shot scroll to the named heading on the next paint via
// markdown.WithScrollToSection; the caller is responsible for clearing
// the value after the scroll lands (HelpHost.consumeScrollTarget does
// this).
func renderRendered(ids *c.WidgetIdStack, doc *markdown.Doc, scrollToSection string) {
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		for range c.IdScope(ids.PrepareStr("doc-render")) {
			if scrollToSection != "" {
				doc.Render(ids, markdown.WithScrollToSection(scrollToSection))
			} else {
				doc.Render(ids)
			}
		}
	}
}

// renderSource draws the raw .md source inside a syntax-highlighted
// CodeView. codeview.PrepareMarkdown returns an interned retained
// holder (content-addressed via unique.Handle) so calling it per
// frame with the same source is amortised to a single hashmap probe
// past the first invocation — no need for HelpHost to maintain its
// own job cache.
func renderSource(ids *c.WidgetIdStack, src []byte) {
	job := codeview.PrepareMarkdown(string(src))
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		for range c.IdScope(ids.PrepareStr("doc-source")) {
			c.CodeView(ids.PrepareSeq(0), job).Send()
		}
	}
}

// bookDisplay resolves an AppId to its human label. Falls back to the
// AppId string when the app isn't registered (which only happens if a
// book was Register()-ed directly bypassing the auto-sync path, e.g.
// in a test).
func bookDisplay(id app.AppIdT) (display string) {
	m, ok := app.LookupManifest(id)
	if !ok {
		display = string(id)
		return
	}
	display = m.Display
	if display == "" {
		display = string(id)
	}
	return
}
