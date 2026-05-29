//go:build llm_generated_opus47

package widgets

import (
	"sort"
	"strings"
	"unicode/utf8"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// categoryIcons maps each demo category to the glyph rendered alongside its
// CollapsingHeader in the gallery. Keys are the exact Category strings used
// at Demo registration. Missing entries fall back to no prefix — that keeps
// `(other)` and any future ad-hoc category usable without an icons update.
var categoryIcons = map[string]string{
	"Charts & plots":        icons.IconChartLine,
	"Debug":                 icons.IconGear,
	"Design system":         icons.IconPalette,
	"Graphics & canvas":     icons.IconPaintBucket,
	"Inputs & pickers":      icons.IconSliders,
	"Inspectors & feedback": icons.IconSearch,
	"Layout & widgets":      icons.IconLightning,
	"Maps & geo":            icons.IconMap,
	"Tables":                icons.IconTable,
	"Text & code":           icons.IconFile,
}

// categoryHeader returns the CollapsingHeader label for a category — icon
// prefix (when known) + non-breaking space + category name. The space stops
// egui from wrapping the glyph onto a line by itself when the panel is
// narrow.
func categoryHeader(category string) (label string) {
	glyph, ok := categoryIcons[category]
	if !ok {
		label = category
		return
	}
	label = glyph + " " + category
	return
}

// hasIconPrefix reports whether the first rune of title sits in the Unicode
// Private-Use Area (U+E000–U+F8FF), the range every icon font we embed
// (Phosphor, the NF brand subset) lives in. We treat any leading PUA rune
// as "the demo already supplies its own icon" so displayTitle leaves it
// alone instead of stacking a category icon in front.
func hasIconPrefix(title string) (yes bool) {
	r, _ := utf8.DecodeRuneInString(title)
	yes = r >= 0xE000 && r <= 0xF8FF
	return
}

// displayTitle is the title rendered in the per-demo CollapsingHeader.
// If the demo's Title already starts with an icon glyph it passes
// through verbatim; otherwise the category icon is prefixed so every
// gallery row has the same visual rhythm. Demos in categories without
// a registered icon (or with an empty Category) fall back to the bare
// title — the helper never invents a glyph.
func displayTitle(d registry.Demo) (label string) {
	if d.Title == "" {
		label = d.Name
		return
	}
	if hasIconPrefix(d.Title) {
		label = d.Title
		return
	}
	glyph, ok := categoryIcons[d.Category]
	if !ok {
		label = d.Title
		return
	}
	label = glyph + " " + d.Title
	return
}

const interactiveEmbedFirstFrame = 2

// App is the per-window Demo-gallery instance. Each Open() yields a
// fresh App with its own filter selection, frame counter, and per-demo
// state map — two open windows of the gallery have independent filters
// and independent state for any demo that has migrated to the stateful
// path (Init + RenderStateful in [registry.Demo]).
//
// Unmigrated demos (Render only) continue to capture package-level
// state via the closure in egui2_hl_*.go and remain shared across
// gallery windows: a slider fiddled in window 1 moves window 2's
// identical demo. The plumb-only Phase 1 migration keeps both paths
// alive simultaneously — once every demo opts into Init+RenderStateful,
// the package-level [ids] stack and the legacy Render field can be
// retired.
type App struct {
	// ids is the per-window WidgetIdStack the host pre-prepares with
	// a window-unique salt every frame (windowhost wraps Frame in
	// c.IdScope keyed on the window key). Captured from
	// MountCtx.Ids() at Mount time; the gallery chrome (filter input,
	// CollapsingHeaders) derives ids from here. Stateful demos see
	// the same stack via their Init/RenderStateful closures.
	ids *c.WidgetIdStack

	// bus is the per-instance BusI captured at Mount. Demos that opt
	// into [registry.Demo.BusInit] receive this pointer for capability
	// publishes (currently: the timerangepicker demo's evaluator on
	// ch.local.exec.timerangepicker, ADR-0016 Phase 4).
	bus runtimeapp.BusI

	// demoState carries per-demo, per-window state structs returned
	// by Demo.Init at Mount. Keyed on Demo.Name. Stateless demos
	// (Render-only) are absent. Embed forwards demoState[name] back
	// to RenderStateful every frame.
	demoState map[string]any

	filter string
	// Bodies always emit per ADR-0012 (the structural BLOCK_SKIPPED
	// gate was removed to fix click-to-open flicker), so this counter
	// short-circuits the expensive per-demo work itself: frame 1 has
	// no r7 yet, frames 2+ read the previous frame's advisory
	// IsBlockSkipped and skip RenderDemoIntro / Embed / RenderDemoOutro
	// for collapsed demos. Without the guard, all the demo Render
	// closures (walkers tile fetch, graphs force-layout, treemap2
	// layout) would fire every frame — the original ~11s ADR-0008
	// startup stall, but recurring.
	frame int32
}

var _ runtimeapp.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids:       c.NewWidgetIdStack(),
		demoState: make(map[string]any, 32),
	}
	return
}

func (inst *App) Manifest() (m runtimeapp.Manifest) { m = manifest; return }

// Mount captures the host-supplied WidgetIdStack and runs Init for
// every registered demo that opted into the stateful path. The
// per-demo state pointers Init returns hold references to inst.ids,
// so the host's per-window IdScope salt scopes their pre-built
// widget singletons correctly.
func (inst *App) Mount(ctx runtimeapp.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.bus = ctx.Bus()
	for _, d := range registry.All() {
		switch {
		case d.BusInit != nil:
			inst.demoState[d.Name] = d.BusInit(inst.ids, inst.bus)
		case d.Init != nil:
			inst.demoState[d.Name] = d.Init(inst.ids)
		}
	}
	return
}
func (inst *App) Unmount(ctx runtimeapp.MountContextI) (err error) { return }

// Frame is the Demo gallery body. Renders all demos registered with
// the local registry (registry.All) grouped by Demo.Category, with a
// substring filter at the top. Per ADR-0026 M3 C3, this is invoked
// inside the runtime-created window scope — no outer scope is
// created here, no Launch buttons (multi-app dispatch lives in the
// host's Apps menu, not in this gallery).
//
// Animation freeze is never set from this path; animations work
// normally. The gallery chrome (filter input, category collapsibles,
// per-demo collapsibles) derives ids from inst.ids so two open
// gallery windows have disjoint chrome ids; unmigrated demos still
// capture the package-level [ids] stack via closure and remain
// shared.
func (inst *App) Frame(ctx runtimeapp.FrameContextI) (err error) {
	inst.frame++
	c.Label("Filter (substring match on demo name or category):").Send()
	c.TextEdit(inst.ids.PrepareStr("demo-filter"), inst.filter, false).
		SendRespVal(&inst.filter)

	needle := strings.ToLower(strings.TrimSpace(inst.filter))
	grouped := galleryGroupByCategory(registry.All(), needle)
	if len(grouped) == 0 {
		c.Label("(no demos match filter)").Send()
	}

	// AutoShrink(false, false) so the gallery fills the host Window in
	// both axes. Default ScrollArea auto_shrink = [true, true] makes the
	// scroll area collapse to its widest child (here a CollapsingHeader
	// label like "etable (sparse, 10k × 30% fill)"), leaving the
	// rest of the Window's width as empty dark space to the right.
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		for _, group := range grouped {
			for range c.CollapsingHeader(
				inst.ids.PrepareStr("cat-"+group.category),
				c.WidgetText().Text(categoryHeader(group.category)).Keep(),
			).DefaultOpen(true).KeepIter() {
				for _, d := range group.demos {
					inst.renderGalleryEntry(d)
				}
			}
		}
	}
	return
}

func (inst *App) renderGalleryEntry(d registry.Demo) {
	ch := c.CollapsingHeader(
		inst.ids.PrepareStr("demo-"+d.Name),
		c.WidgetText().Text(displayTitle(d)).Keep(),
	)
	handle := ch.Handle()
	for range ch.KeepIter() {
		if inst.frame < interactiveEmbedFirstFrame || c.IsBlockSkipped(handle) {
			continue
		}
		RenderDemoIntro(inst.ids, &d)
		// Stateful demos receive their per-window state; stateless
		// demos pass nil through (Embed's RenderStateful branch is
		// skipped) and reach their per-frame ids exclusively via the
		// closure argument.
		registry.Embed(inst.ids, d.Name, inst.demoState[d.Name])
		RenderDemoOutro(inst.ids, &d)
	}
}

type galleryCategoryGroup struct {
	category string
	demos    []registry.Demo
}

// galleryGroupByCategory buckets the demo registry by Demo.Category
// (stable, alphabetic order) and applies the filter substring on Name
// or Category. Demos with empty Category land under "(other)".
func galleryGroupByCategory(all []registry.Demo, needle string) (groups []galleryCategoryGroup) {
	byCat := make(map[string][]registry.Demo, 8)
	for _, d := range all {
		if needle != "" {
			haystack := strings.ToLower(d.Name + " " + d.Title + " " + d.Category)
			if !strings.Contains(haystack, needle) {
				continue
			}
		}
		cat := d.Category
		if cat == "" {
			cat = "(other)"
		}
		byCat[cat] = append(byCat[cat], d)
	}
	cats := make([]string, 0, len(byCat))
	for cat := range byCat {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	groups = make([]galleryCategoryGroup, 0, len(cats))
	for _, cat := range cats {
		demosInCat := byCat[cat]
		sort.Slice(demosInCat, func(i, j int) bool {
			return demosInCat[i].Name < demosInCat[j].Name
		})
		groups = append(groups, galleryCategoryGroup{category: cat, demos: demosInCat})
	}
	return
}
