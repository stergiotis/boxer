//go:build llm_generated_opus47

package leewaywidgets_demo

import (
	"bytes"
	"encoding/json/jsontext"

	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// viewKeyE identifies which renderer the central panel is showing. The tree
// on the left and the (programmatic) tour both flip this.
type viewKeyE uint8

const (
	viewKeyTable2 viewKeyE = iota
	viewKeyJSON
	viewKeySchemaGo
	viewKeyFixtureGo
)

// Package-scoped state survives across render-loop frames. Per-window
// state (selectedView, ids, table2Emitter) lives on the *App value the
// registry hands back from each Open(); the codeview holders below
// stay package-level because they hold expensive-to-build text that
// every window can share.
var (
	// JSON view cache — built once on first access (driving RunFixture
	// against a JsonCardEmitter then highlighting the bytes).
	jsonViewReady bool
	jsonView      typed.RetainedFffiHolderTyped[c.CodeViewJobS]

	// Go-source view caches — built once from the embedded sources.
	// schemaGoView mirrors fixture_schema.go (the declarative TableDesc);
	// fixtureGoView mirrors fixture.go (the data populator + driver wiring).
	schemaGoViewReady  bool
	schemaGoView       typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	fixtureGoViewReady bool
	fixtureGoView      typed.RetainedFffiHolderTyped[c.CodeViewJobS]
)

// App is the per-window leewaywidgets instance. The factory ctor
// allocates a fresh App per Open() so two windows have independent
// tree selections (selectedView).
type App struct {
	// ids is the per-instance WidgetIdStack supplied by the host via
	// MountCtx.Ids() (the windowhost pre-pushes a window-unique salt
	// onto it before every Frame() call via c.IdScope so widget ids
	// the renderer derives cannot collide with another open app's
	// ids). The ctor seeds a fresh fallback stack so tests that skip
	// Mount still have a non-nil stack.
	ids *c.WidgetIdStack

	// table2Emitter binds the Table2 card view to the App's ids
	// stack; per-instance so two open windows emit widget ids under
	// distinct host salts.
	table2Emitter *leewaywidgets.Table2CardEmitter

	selectedView viewKeyE
}

var _ runtimeapp.AppI = (*App)(nil)

func newApp() (inst *App) {
	ids := c.NewWidgetIdStack()
	inst = &App{
		ids:           ids,
		table2Emitter: leewaywidgets.NewTable2CardEmitter(ids, leewaywidgets.ColorPaletteViridis, nil),
		selectedView:  viewKeyTable2,
	}
	return
}

func (inst *App) Manifest() (m runtimeapp.Manifest) { m = manifest; return }
func (inst *App) Mount(ctx runtimeapp.MountContextI) (err error) {
	// Pick up the host-supplied per-instance ids stack and rebuild
	// the Table2 emitter so it emits ids under the same stack. The
	// emitter holds a pointer to the stack so it can't just be left
	// pointing at the ctor's fallback.
	inst.ids = ctx.Ids()
	inst.table2Emitter = leewaywidgets.NewTable2CardEmitter(inst.ids, leewaywidgets.ColorPaletteViridis, nil)
	return
}
func (inst *App) Unmount(ctx runtimeapp.MountContextI) (err error) { return }

// Frame is the per-frame entry for the standalone windowed app: IDE-style
// layout (left tree picks a view, central panel renders it). Per ADR-0026
// Amendment 2026-05-12 the host wraps this in a runtime-created c.Window
// using Manifest.WindowTitle/Icon; the body uses only *Inside panels, which
// is correct here because the Window supplies the CentralPanel region the
// side panel sizes against.
//
// The screenshot tour and interactive widget gallery use renderGallery
// instead — those hosts wrap the demo in a bare scroll area with no such
// region, where these *Inside panels collapse (SKILLS.md §"Gallery
// Scroll-Host Layout").
//
// The host has already pre-pushed a window-unique salt onto inst.ids
// via c.IdScope (windowhost.renderWindowBody) so widget ids derived
// from inst.ids are scoped under that salt.
func (inst *App) Frame(ctx runtimeapp.FrameContextI) (err error) {
	for range c.PanelLeftInside(inst.ids.PrepareStr("viewTreePanel")).DefaultSize(220).Resizable(true).KeepIter() {
		inst.renderViewTree()
	}
	for range c.PanelCentralInside().KeepIter() {
		inst.renderActiveView()
	}
	return
}

const (
	// galleryTreeWidth pins the picker column so the side-by-side gallery
	// layout is stable across hosts; matches Frame's PanelLeftInside size.
	galleryTreeWidth float32 = 220
	// galleryContentH bounds the central pane height. The gallery host is an
	// unbounded-height vscroll ScrollArea (no CentralPanel region), so the
	// table2 TableBuilder and the codeview ScrollArea — each owning an inner
	// ScrollArea that reads available_height — would otherwise get no finite
	// rect and crop their tail. A fixed height gives them one and still fits
	// the tour stage (700) under the per-demo intro + outro chrome.
	galleryContentH float32 = 500
)

// renderGallery is the demo-registry layout used by the tour and the
// interactive widget gallery (leewaywidgets_tour.go). Unlike Frame it does
// NOT use PanelLeftInside/PanelCentralInside: the gallery host wraps each
// demo in an unbounded-height vscroll ScrollArea with no CentralPanel
// region, where egui side panels collapse to a sliver and the central
// content has no bounded height to fill (SKILLS.md §"Gallery Scroll-Host
// Layout"). Mirror schemaview instead — a fixed-width picker column beside a
// height-bounded content pane, both rendered directly into the scroll host.
func (inst *App) renderGallery() {
	for range c.Horizontal().KeepIter() {
		for range c.Vertical().KeepIter() {
			c.UiSetMinWidth(galleryTreeWidth)
			c.UiSetMaxWidth(galleryTreeWidth)
			inst.renderViewTree()
		}
		c.AddSpace(8)
		for range c.Vertical().KeepIter() {
			c.UiSetMinHeight(galleryContentH)
			c.UiSetMaxHeight(galleryContentH)
			inst.renderActiveView()
		}
	}
}

// renderViewTree draws the view picker as three collapsible category headers
// with selectable leaves. Selecting a leaf flips inst.selectedView; the
// central pane reads that on the next frame.
//
// CollapsingHeader + SelectableLabel rather than the egui_ltreeview
// flat-drain (NodeDir/NodeLeaf/Tree): that surface mis-renders a wide,
// multi-root tree like this one (three top-level categories), and it is
// wrapped in no ScrollArea so it survives a width-pinned gallery column,
// where a ScrollArea collapses to its first child (SKILLS.md nav-layout
// gotchas). The list is tiny — 3 groups, 4 leaves — so a host-level scroll
// is enough for tall windows.
func (inst *App) renderViewTree() {
	for range c.CollapsingHeader(inst.ids.PrepareStr("catVisual"), c.WidgetText().Text("Visual").Keep()).DefaultOpen(true).KeepIter() {
		inst.renderViewLeaf(viewKeyTable2, "leafTable2", "table2")
	}
	for range c.CollapsingHeader(inst.ids.PrepareStr("catCanonical"), c.WidgetText().Text("Canonical").Keep()).DefaultOpen(true).KeepIter() {
		inst.renderViewLeaf(viewKeyJSON, "leafJson", "json")
	}
	for range c.CollapsingHeader(inst.ids.PrepareStr("catSource"), c.WidgetText().Text("Source").Keep()).DefaultOpen(true).KeepIter() {
		inst.renderViewLeaf(viewKeySchemaGo, "leafSchemaGo", "schema.go")
		inst.renderViewLeaf(viewKeyFixtureGo, "leafFixtureGo", "fixture.go")
	}
}

// renderViewLeaf renders one selectable picker row; a primary click flips the
// active view the central pane reads on the next frame.
func (inst *App) renderViewLeaf(key viewKeyE, idStr string, label string) {
	if c.SelectableLabel(inst.ids.PrepareStr(idStr), inst.selectedView == key, label).SendResp().HasPrimaryClicked() {
		inst.selectedView = key
	}
}

// renderActiveView draws the central pane for the currently selected view.
// JSON and Go views build their highlighted holders lazily on first access
// and reuse them across frames; the table2 emitter re-runs RunFixture each
// frame because its output is widget commands, not text.
//
// The code views scroll with AutoShrink(false, false): egui's default
// auto_shrink shrinks a ScrollArea to its content, so a short source
// (schema.go, fixture.go) collapsed to a few lines and left the rest of
// the pane as dead "y space" below. Disabling shrink on both axes makes
// each view fill the bounded pane its caller supplies (the central panel
// for the windowed app, the height-clamped column for the gallery).
func (inst *App) renderActiveView() {
	switch inst.selectedView {
	case viewKeyJSON:
		ensureJSONView()
		for range c.ScrollArea().Vscroll(true).Hscroll(true).AutoShrink(false, false).KeepIter() {
			c.CodeView(inst.ids.PrepareStr("jsonView"), jsonView).Wrap().Send()
		}
	case viewKeySchemaGo:
		ensureSchemaGoView()
		for range c.ScrollArea().Vscroll(true).Hscroll(true).AutoShrink(false, false).KeepIter() {
			c.CodeView(inst.ids.PrepareStr("schemaGoView"), schemaGoView).Wrap().Send()
		}
	case viewKeyFixtureGo:
		ensureFixtureGoView()
		for range c.ScrollArea().Vscroll(true).Hscroll(true).AutoShrink(false, false).KeepIter() {
			c.CodeView(inst.ids.PrepareStr("fixtureGoView"), fixtureGoView).Wrap().Send()
		}
	default: // viewKeyTable2
		// Table2CardEmitter renders into an egui_extras::TableBuilder which
		// owns its own ScrollArea, so wrapping in another ScrollArea would
		// supply unbounded available_size and crop the tail rows.
		leewaywidgets.RunFixture(inst.table2Emitter)
	}
}

// ensureJSONView lazily builds the canonical card-JSON for the fixture and
// hands it to codeview.PrepareJson. The JsonCardEmitter is one-shot so we drain
// it into a buffer and re-highlight only when explicitly invalidated (today
// the fixture is static, so once-per-process is enough).
func ensureJSONView() {
	if jsonViewReady {
		return
	}
	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	enc := jsontext.NewEncoder(buf,
		jsontext.Multiline(true),
		jsontext.WithIndent("  "))
	sink := card.NewJsonCardEmitter(enc, nil)
	leewaywidgets.RunFixture(sink)
	jsonView = codeview.PrepareJson(buf.String())
	jsonViewReady = true
}

func ensureSchemaGoView() {
	if schemaGoViewReady {
		return
	}
	schemaGoView = codeview.PrepareGo(leewaywidgets.FixtureSource)
	schemaGoViewReady = true
}

func ensureFixtureGoView() {
	if fixtureGoViewReady {
		return
	}
	fixtureGoView = codeview.PrepareGo(leewaywidgets.FixtureBuilderSource)
	fixtureGoViewReady = true
}
