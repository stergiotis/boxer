package splashscreen

import (
	"fmt"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
	"github.com/stergiotis/boxer/public/observability/vcs"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// The window has two rows of tabs. The first names each credited party —
// the program, when one is registered, then boxer — and Build, the running
// binary's provenance. The second, under a party, picks its page: Splash,
// its artwork, and About, its identity, licence and NOTICE.

// buildKey is the first-row entry of the Build tab.
const buildKey = "build"

// pageE selects a party's page.
type pageE uint8

const (
	pageSplash pageE = iota
	pageAbout
)

// App is the per-window splashscreen instance.
type App struct {
	// ids is the per-instance WidgetIdStack. The host pre-pushes a
	// window-unique salt onto it before every Frame() call (ADR-0026
	// §SD9, windowhost.renderWindowBody), so every widget id the app
	// derives is unique across all concurrently open instances. Captured
	// from ctx.Ids() in Mount; the app must NOT Reset() it.
	ids *c.WidgetIdStack

	density styletokens.DensityE
	// sel is the first-row selection: a party's key or buildKey.
	sel  string
	page pageE
	// imgTracker gates each party's texture upload: full pixels on the
	// first render, an empty slice ("use cached") thereafter.
	// Per-instance because the widget id — and thus the Rust-side GPU
	// cache key — is scoped by the host-salted inst.ids stack.
	imgTracker *c.ImageVersionTracker[string]
}

var _ app.AppI = (*App)(nil)

// newApp opens on the program's splash when a program is registered,
// otherwise on boxer's.
func newApp() (inst *App) {
	inst = &App{
		ids:        c.NewWidgetIdStack(),
		density:    styletokens.ActiveDensity(),
		sel:        boxerKey,
		page:       pageSplash,
		imgTracker: c.NewImageVersionTracker[string](),
	}
	if programRegistered() {
		inst.sel = programKey
	}
	return
}

// Manifest is the declared manifest with the window sized to the program's
// artwork, as the registry holds it.
func (inst *App) Manifest() (m app.Manifest) {
	m = manifest
	m.SurfaceHints = windowHints()
	return
}

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	loadAssets()
	if boxerArtErr != nil {
		// A build without the artwork: the Splash page shows a one-line
		// notice and the other pages are unaffected. Logged at info — it is
		// not a fault.
		logger := ctx.Log()
		logger.Info().Err(boxerArtErr).Msg("splashscreen: splash image not bundled; showing placeholder")
	}
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

// Frame renders one frame of the splashscreen window body. The host has
// already pre-pushed a window-unique salt onto inst.ids via c.IdScope
// (windowhost.renderWindowBody, ADR-0026 §SD9), so widget ids derived
// from inst.ids are unique across all concurrently open instances — the
// app must not Reset() the stack or wrap the body in its own instance
// salt (doing so discards the host salt and collides with sibling apps
// that share a label string). When driven outside the host (the tour
// calls Frame directly), the caller must bind inst.ids to its own
// scoped stack first.
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	inst.renderBody()
	return
}

func (inst *App) renderBody() {
	ps := parties()
	for range c.PanelTopInside(inst.ids.PrepareStr("tabs")).Resizable(false).KeepIter() {
		inst.renderTabBar(ps)
	}
	for range c.PanelCentralInside().KeepIter() {
		if p, ok := lookupParty(ps, inst.sel); ok {
			switch inst.page {
			case pageAbout:
				inst.renderAbout(p)
			default:
				inst.renderSplash(p)
			}
		} else {
			inst.renderBuild()
		}
	}
}

// renderTabBar draws the party row and, under a party, its page row. A
// selection that no longer exists — the program cleared, a party without
// artwork — falls back to the first party or to About.
func (inst *App) renderTabBar(ps []party) {
	if _, ok := lookupParty(ps, inst.sel); !ok && inst.sel != buildKey {
		inst.sel = ps[0].key
	}
	row := selector.Segmented(inst.ids, "splash-parties", &inst.sel).
		Style(selector.StyleSelectable)
	for _, p := range ps {
		row = row.Option(p.key, p.label())
	}
	changed := row.Option(buildKey, icons.PhWrench+" Build").SendResp()

	p, ok := lookupParty(ps, inst.sel)
	if !ok {
		return
	}
	if p.image == nil && inst.page == pageSplash {
		inst.page = pageAbout
	}
	pages := selector.Segmented(inst.ids, "splash-pages", &inst.page).
		Style(selector.StyleSelectable)
	if p.image != nil {
		pages = pages.Option(pageSplash, icons.PhImage+" Splash")
	}
	if pages.Option(pageAbout, icons.PhInfo+" About").SendResp() {
		changed = true
	}
	if changed && inst.page == pageSplash {
		// Force one re-upload on re-entry: while another page was shown the
		// Image widget did not render, so the Rust-side texture may have
		// been evicted. Forgetting the tracked version makes the next
		// render ship full pixels again.
		inst.imgTracker.Forget(p.imgKey())
	}
}

// renderSplash centers the party's artwork and scales it to the pane,
// preserving its aspect.
func (inst *App) renderSplash(p party) {
	img, err := p.image()
	if err != nil || len(img.pixels) == 0 {
		c.Label("Splash image unavailable.").Send()
		return
	}
	key := p.imgKey()
	pixels := inst.imgTracker.PixelsToSend(key, img.version, img.pixels)
	// VerticalCentered — not HorizontalCentered — is what centers content on
	// the *horizontal* axis in egui: it lays widgets out vertically and
	// centers them left-to-right. horizontal_centered centers on the vertical
	// axis instead, which would leave a portrait image left-aligned.
	//
	// FitAspectMax with a zero (0,0) bounding box tells the client to scale
	// the image aspect-preserved into its *local* ui.available_size(). We
	// deliberately do NOT compute the box from GetAvailableSize/
	// CaptureAvailableSize: that register is global and shared across windows,
	// so when another app renders in the same frame it clobbers the value and
	// the image shrinks to whatever box that other window left behind.
	for range c.VerticalCentered().KeepIter() {
		c.Image(inst.ids.PrepareStr(key), img.w, img.h, 1,
			uint8(c.FitAspectMaxE), 0, 0,
			uint8(c.FilterLinearE), c.TintNoneRgba, pixels).
			Send()
	}
}

// renderAbout shows the party's identity and licence, then its NOTICE
// verbatim, one source line per label. Rendering line by line (rather than
// through the markdown widget) keeps file paths like h3o_wasm intact —
// markdown would read the underscores as emphasis.
func (inst *App) renderAbout(p party) {
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		heading(p.name)
		for _, line := range p.identityLines() {
			c.Label(line).Send()
		}
		if p.notice != "" {
			c.AddSpace(styletokens.PaddingOuter(inst.density))
			c.Separator().Horizontal().Send()
			c.AddSpace(styletokens.GapItems(inst.density))
			heading("NOTICE")
			c.AddSpace(styletokens.GapItems(inst.density))
			inst.renderVerbatim(p.notice)
		}
	}
}

// renderVerbatim draws text one source line per label, an empty line as a
// gap.
func (inst *App) renderVerbatim(text string) {
	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		if line == "" {
			c.AddSpace(styletokens.GapItems(inst.density))
			continue
		}
		c.Label(line).Send()
	}
}

// renderBuild shows the running binary's provenance.
func (inst *App) renderBuild() {
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		heading("Build")
		c.Label("The running binary, as built.").Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		for _, f := range buildInfo() {
			kv(f[0], f[1])
		}
	}
}

// buildInfo is the Build tab as key-value pairs. VCS fields come straight
// from the embedded build info (no init needed); run identity is read from
// runinfo when the process initialised it (the carousel does at startup).
func buildInfo() (kvs [][2]string) {
	rev, modified, revErr := vcs.GetVcsRevision()
	switch {
	case revErr != nil || rev == "":
		kvs = append(kvs, [2]string{"Revision", vcs.NoBuildInfo})
	case modified:
		kvs = append(kvs, [2]string{"Revision", rev + " (modified)"})
	default:
		kvs = append(kvs, [2]string{"Revision", rev + " (clean)"})
	}
	kvs = append(kvs, [2]string{"Module", vcs.ModuleInfo()})
	if ri, err := runinfo.Get(); err == nil {
		kvs = append(kvs,
			[2]string{"Go", ri.GoVersion},
			[2]string{"Host", ri.Hostname},
			[2]string{"PID", fmt.Sprintf("%d", ri.Pid)},
			[2]string{"Run ID", ri.RunId},
			[2]string{"Started", ri.StartedAt.Format(time.RFC3339)})
	} else {
		kvs = append(kvs, [2]string{"Run", "metadata unavailable (runinfo not initialised)"})
	}
	return
}

// heading emits a heading-styled label. Mirrors the helper in capinspector:
// the bindings expose Heading() on the rich-text scope but not as a widget
// shortcut.
func heading(text string) {
	c.LabelAtoms(c.Atoms().BeginRichText(text).Heading().End().Keep()).Send()
}

// kv renders a "key: value" line with the value in monospace so hashes and
// paths stay legible.
func kv(key string, val string) {
	c.LabelAtoms(c.Atoms().
		BeginRichText(key + ": ").End().
		BeginRichText(val).Monospace().End().
		Keep()).Send()
}
