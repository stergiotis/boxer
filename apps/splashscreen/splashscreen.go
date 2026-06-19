package splashscreen

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png" // register the PNG decoder for the embedded splash asset
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
	"github.com/stergiotis/boxer/public/observability/vcs"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// tabE selects which of the three panes the body renders.
type tabE uint8

const (
	tabSplash tabE = iota
	tabAbout
	tabNotice
)

// splashImgKey is both the widget-id key and the version-tracker key for the
// splash texture; the two must stay 1:1 (see ImageVersionTracker docs).
const splashImgKey = "splash-img"

// ids is the package-level widget-id stack. Frame scopes its body under a
// per-instance seed (IdScope) so two splashscreen windows keep disjoint
// widget ids.
var ids = c.NewWidgetIdStack()

// instanceCounter stamps each newApp() with a unique seed.
var instanceCounter atomic.Uint64

const (
	splashAssetPath = "assets/splash.png"
	noticeAssetPath = "assets/NOTICE"
)

// Assets are loaded once, lazily on first Mount, into package-level state
// shared read-only across every instance: the data is immutable and each
// window's Image widget carries its own id, so sharing the upload source
// avoids re-decoding ~0.5 MB per window. Loading lazily (rather than in init)
// keeps the cost off the startup path for the common case where the user never
// opens this app.
//
// The splash image is optional — it is git-ignored and may be absent on a
// fresh checkout — so a read/decode failure is recorded in splashErr and
// surfaced as a degraded pane rather than a hard error. NOTICE is committed,
// so it is expected to be present.
var (
	assetsOnce   sync.Once
	splashPixels []uint32
	splashW      uint32
	splashH      uint32
	splashErr    error
	noticeText   string
)

func loadAssets() {
	assetsOnce.Do(func() {
		if data, err := assetsFS.ReadFile(noticeAssetPath); err == nil {
			noticeText = string(data)
		} else {
			noticeText = "NOTICE asset is missing from this build."
		}

		data, err := assetsFS.ReadFile(splashAssetPath)
		if err != nil {
			splashErr = fmt.Errorf("read splash asset: %w", err)
			return
		}
		img, _, decErr := image.Decode(bytes.NewReader(data))
		if decErr != nil {
			splashErr = fmt.Errorf("decode splash png: %w", decErr)
			return
		}
		b := img.Bounds()
		w := b.Dx()
		h := b.Dy()
		if w <= 0 || h <= 0 {
			splashErr = fmt.Errorf("splash png has empty bounds %dx%d", w, h)
			return
		}
		// Map luminance through an IDS sequential palette via a 256-entry LUT.
		// Default: the design-system grayscale ramp (Crameri grayC, which runs
		// t=0→white … t=1→black, so invert luminance to keep the artwork's
		// tonality — dark stays dark). When the KEELSON_EASTEREGG toggle is on,
		// colourise with viridis instead (t runs dark→bright, no inversion).
		// Output is packed 0xRRGGBBAA row-major — the imzero2 Image widget's
		// pixel contract; alpha is carried through per pixel.
		egg := app.EasterEgg.Get()
		var paletteLUT [256]uint32
		for n := range paletteLUT {
			var c8 styletokens.RGBA8
			if egg {
				c8 = styletokens.Sequential(styletokens.SequentialViridis, float32(n)/255.0)
			} else {
				c8 = styletokens.Sequential(styletokens.SequentialGrayC, 1.0-float32(n)/255.0)
			}
			paletteLUT[n] = uint32(c8.R)<<24 | uint32(c8.G)<<16 | uint32(c8.B)<<8
		}
		px := make([]uint32, w*h)
		i := 0
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bl, a := img.At(x, y).RGBA()
				// Rec.601 luma (weights sum to 256). Equals the gray value for
				// our grayscale asset, but stays correct if a colour image is
				// ever swapped in.
				lum := (77*(r>>8) + 150*(g>>8) + 29*(bl>>8)) >> 8
				px[i] = paletteLUT[lum] | uint32(a>>8)
				i++
			}
		}
		splashPixels = px
		splashW = uint32(w)
		splashH = uint32(h)
	})
}

// App is the per-window splashscreen instance.
type App struct {
	seed    uint64
	density styletokens.DensityE
	tab     tabE
	// imgTracker gates the splash texture upload: full pixels on the first
	// render, an empty slice ("use cached") thereafter. Per-instance because
	// the widget id — and thus the Rust-side GPU cache key — is scoped by
	// inst.seed.
	imgTracker *c.ImageVersionTracker[string]
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		seed:       instanceCounter.Add(1),
		density:    styletokens.DensityFromEnv(),
		tab:        tabSplash,
		imgTracker: c.NewImageVersionTracker[string](),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	loadAssets()
	if splashErr != nil {
		// Expected when the git-ignored artwork is absent (e.g. a fresh
		// checkout): the splash pane shows a one-line notice and the other
		// tabs are unaffected. Logged at info — it is not a fault.
		logger := ctx.Log()
		logger.Info().Err(splashErr).Msg("splashscreen: splash image not bundled; showing placeholder")
	}
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	ids.Reset()
	for range c.IdScope(ids.PrepareSeq(inst.seed)) {
		inst.renderBody()
	}
	return
}

func (inst *App) renderBody() {
	for range c.PanelTopInside(ids.PrepareStr("tabs")).Resizable(false).KeepIter() {
		inst.renderTabBar()
	}
	for range c.PanelCentralInside().KeepIter() {
		switch inst.tab {
		case tabAbout:
			inst.renderAbout()
		case tabNotice:
			inst.renderNotice()
		default:
			inst.renderSplash()
		}
	}
}

// renderTabBar draws the three selectable tab labels across the top.
func (inst *App) renderTabBar() {
	for range c.Horizontal().KeepIter() {
		inst.tabButton(tabSplash, "tab-splash", icons.PhSparkle+" Splash")
		inst.tabButton(tabAbout, "tab-about", icons.PhInfo+" About")
		inst.tabButton(tabNotice, "tab-notice", icons.PhScroll+" NOTICE")
	}
}

func (inst *App) tabButton(tab tabE, key string, label string) {
	active := inst.tab == tab
	if c.SelectableLabel(ids.PrepareStr(key), active, label).SendResp().HasPrimaryClicked() {
		if tab == tabSplash {
			// Force one re-upload on re-entry: while another tab was shown the
			// Image widget did not render, so the Rust-side texture may have
			// been evicted. Forgetting the tracked version makes the next
			// renderSplash ship full pixels again.
			inst.imgTracker.Forget(splashImgKey)
		}
		inst.tab = tab
	}
}

// renderSplash centers the artwork and scales it to the pane, preserving the
// portrait aspect.
func (inst *App) renderSplash() {
	if splashErr != nil || len(splashPixels) == 0 {
		c.Label("Splash image unavailable.").Send()
		return
	}
	pixels := inst.imgTracker.PixelsToSend(splashImgKey, 1, splashPixels)
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
		c.Image(ids.PrepareStr(splashImgKey), splashW, splashH, 1,
			uint8(c.FitAspectMaxE), 0, 0,
			uint8(c.FilterLinearE), c.TintNoneRgba, pixels).
			Send()
	}
}

// renderAbout shows identity, copyright and build provenance.
func (inst *App) renderAbout() {
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		heading("boxer")
		c.Label(manifest.Display + " · version " + manifest.Version).Send()
		c.Label(vcs.CopyrightInfo()).Send()
		c.Label("Licensed under the MIT License (see the NOTICE tab).").Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		c.Label("Data Engineering Toolbelt - DB-to-Glass. This window shows the splash artwork, the build provenance of the running binary, and the project NOTICE.").Send()
		c.AddSpace(styletokens.PaddingOuter(inst.density))
		c.Separator().Horizontal().Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		heading("Build")
		renderBuildInfo()
	}
}

// renderBuildInfo lists VCS/run metadata. VCS fields come straight from the
// embedded build info (no init needed); run identity is read from runinfo
// when the process initialised it (the carousel does at startup).
func renderBuildInfo() {
	rev, modified, revErr := vcs.GetVcsRevision()
	switch {
	case revErr != nil || rev == "":
		kv("Revision", vcs.NoBuildInfo)
	case modified:
		kv("Revision", rev+" (modified)")
	default:
		kv("Revision", rev+" (clean)")
	}
	kv("Module", vcs.ModuleInfo())
	if ri, err := runinfo.Get(); err == nil {
		kv("Go", ri.GoVersion)
		kv("Host", ri.Hostname)
		kv("PID", fmt.Sprintf("%d", ri.Pid))
		kv("Run ID", ri.RunId)
		kv("Started", ri.StartedAt.Format(time.RFC3339))
	} else {
		kv("Run", "metadata unavailable (runinfo not initialised)")
	}
}

// renderNotice shows the embedded NOTICE verbatim, one source line per label.
// Rendering line-by-line (rather than through the markdown widget) keeps file
// paths like h3o_wasm intact — markdown would read the underscores as
// emphasis.
func (inst *App) renderNotice() {
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		heading("NOTICE")
		c.AddSpace(styletokens.GapItems(inst.density))
		for line := range strings.SplitSeq(strings.TrimRight(noticeText, "\n"), "\n") {
			if line == "" {
				c.AddSpace(styletokens.GapItems(inst.density))
				continue
			}
			c.Label(line).Send()
		}
	}
}

// heading emits a heading-styled label. Mirrors the helper in capinspector:
// the bindings expose Heading() on the rich-text scope but not as a widget
// shortcut.
func heading(text string) {
	c.LabelAtoms(c.Atoms().RichText(text).Heading().EndRichText().Keep()).Send()
}

// kv renders a "key: value" line with the value in monospace so hashes and
// paths stay legible.
func kv(key string, val string) {
	c.LabelAtoms(c.Atoms().
		BeginRichText(key + ": ").End().
		BeginRichText(val).Monospace().End().
		Keep()).Send()
}
