package imztop

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	egcolor "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/heatmapscroll"
)

// cpuHeatmapWidthSlots is the ring-buffer width in time steps. At the
// default 1Hz sampler this is 10 minutes of history; at the tour's
// 10× cadence (100ms) it's ~1 minute. Width is fixed at construction
// because the underlying scrollingTexture allocates a GPU texture
// keyed off this size.
const cpuHeatmapWidthSlots uint32 = 600

// cpuHeatmapBandHeight is the number of texture rows allocated per
// CPU core. Each core's value is replicated across the band so the
// resulting heatmap is `nCores * bandHeight` pixels tall — without
// this, a 32-core box renders as a 32 px-tall strip, which is too
// short to read. 6 px per core gives ~192 px on a 32-core machine
// and ~48 px on a 4-core machine; both legible while leaving room
// for the per-core sparkline grid below on small viewports.
const cpuHeatmapBandHeight uint32 = 6

// cpuHeatmapCursorStripHeight is the height (px) of the thin canvas
// below the heatmap that shows the hover cursor as a vertical line.
// Kept small so it reads as an indicator strip, not a second plot.
const cpuHeatmapCursorStripHeight float32 = 8

// cpuHeatmapReservedBelowPx estimates the vertical space the chrome
// + sparkline grid below the heatmap will need. Used to size the
// heatmap's stretched height: heatmap_h = clamp(available_h -
// reservedBelow, slot_h, slot_h*maxStretch). A rough estimate is
// fine — the clamp keeps the result sane even when the actual
// content below is taller or shorter than expected.
//   cursor strip   ~12 px
//   tick labels    ~20 px
//   hover label    ~20 px
//   sparkline grid ~400 px (32 cores in 4 cols = 8 rows × ~50 px)
//   gaps/padding   ~28 px
const cpuHeatmapReservedBelowPx float32 = 480

// cpuHeatmapMaxStretch is the maximum multiple of the slot-count
// baseline the heatmap can stretch to. Caps growth so a huge panel
// doesn't force the heatmap to swallow the whole leaf and push the
// per-core sparklines off the visible region.
const cpuHeatmapMaxStretch float32 = 4.0

// cpuHeatmapAxisPad reserves width for item spacing/margin when the
// heatmap stretches to fill the panel, so the texture and its x-axis
// don't trip a horizontal scrollbar. cpuHeatmapMinTexW is the floor
// below which the native slot-count width is kept instead of stretching.
// Mirrors imzrt's spectroAxisPad / spectroMinTexW.
const (
	cpuHeatmapAxisPad float32 = 16
	cpuHeatmapMinTexW float32 = 240
)

// cpuHeatmapXAxisH is the height of the time-axis tick+label row painted
// under the heatmap; cpuHeatmapXAxisFont is that row's label font size.
// Mirrors imzrt's spectroXAxisH / spectroYAxisFont.
const (
	cpuHeatmapXAxisH    float32 = 18
	cpuHeatmapXAxisFont float32 = 10
)

// cpuHeatmapCellBgAlpha is the alpha applied to palette colours when
// reused as cell backgrounds (process table CPU% column). Full opacity
// would dominate the cell text; ~50% lets the dark theme text show
// through clearly while keeping the colour readable as an intensity
// cue. Same idiom as the progress-bar fill literals elsewhere in the
// app (colorMemFill 0xcc884488, colorDiskFill 0x4488cc88, …).
const cpuHeatmapCellBgAlpha uint32 = 0x88

// cpuHeatmapPaletteStops is the resolution at which the global IDS
// sequential LUT is resampled into the []uint32 0xRRGGBBAA stop list
// colormap.NewConfig consumes. 256 matches the underlying LUT width
// so no fidelity is lost; the resulting 1 KiB slice is allocated once
// per heatmap-state init, not per frame.
const cpuHeatmapPaletteStops = 256

// cpuHeatmapPalette builds the heatmap's colormap palette by sampling
// the globally-configured IDS sequential palette
// (styletokens.SequentialDefault — Tier-1 IDS_PALETTE_SEQUENTIAL with
// the Tier-2 IDS_ACCESSIBILITY override). Pulling the palette from the
// IDS env layer instead of a local ComboBox keeps imztop's data-encoding
// surface aligned with every other keelson app: the user touches one
// knob (IDS_PALETTE_SEQUENTIAL / IDS_ACCESSIBILITY) and every heatmap,
// boxenplot, and treemap responds. See ADR-0031 §SD3 and palette_env.go.
func cpuHeatmapPalette() (palette []uint32) {
	s := styletokens.SequentialDefault()
	palette = make([]uint32, cpuHeatmapPaletteStops)
	for i := range palette {
		t := float32(i) / float32(cpuHeatmapPaletteStops-1)
		rgba := styletokens.Sequential(s, t)
		// colormap.Config expects 0xRRGGBBAA per palettes.go header
		// comment; Sequential always returns A=0xFF.
		palette[i] = uint32(rgba.R)<<24 | uint32(rgba.G)<<16 | uint32(rgba.B)<<8 | uint32(rgba.A)
	}
	return
}

// cpuHeatmapState is per-window state for the CPU per-core heatmap.
// Initialised lazily on the first frame that brings non-empty
// PerCorePercent so we can lock the row count to the logical core
// count without guessing. Stays nil-equivalent for tour fixtures
// that never see a published snapshot.
type cpuHeatmapState struct {
	hs           *heatmapscroll.HeatmapScroll
	cfg          *colormap.Config
	nCores       uint32 // logical core count, locked at first push
	heightSlots  uint32 // = nCores * cpuHeatmapBandHeight
	lastPushedMs int64
	colBuf       []float32
}

// renderCPUHeatmap lazy-initialises the heatmap on first non-empty
// snapshot, drains any newly arrived sample as one column, then
// renders the colormap selector + heatmap + timeticks x-axis labels.
//
// Push policy: one column per published snapshot (keyed on
// SampledAtUnixMs). If multiple frames arrive between snapshots, the
// heatmap stays still — matches the sampler's 1 Hz cadence.
func (inst *App) renderCPUHeatmap(snap *PublishedSnapshot) {
	if snap.LatestCPU == nil || len(snap.LatestCPU.PerCorePercent) == 0 {
		c.Label("Per-core data not yet available…").Send()
		return
	}
	nCores := uint32(len(snap.LatestCPU.PerCorePercent))
	st := &inst.cpuHeatmap
	if st.hs == nil {
		st.nCores = nCores
		st.heightSlots = nCores * cpuHeatmapBandHeight
		st.cfg = colormap.NewConfig(cpuHeatmapPalette(), 0, 100)
		// BadColor takes NaN samples; we use it as the "no data yet"
		// fill so the prepopulation step shows a uniform dark-grey
		// background instead of leaving the texture transparent.
		// IDS NeutralBgSurface — sits just above the panel background
		// so the pre-filled NaN ring reads as a rectangle without
		// competing with real palette stops once samples land.
		st.cfg.BadColor = color.NRGBA{
			R: styletokens.NeutralBgSurface.R,
			G: styletokens.NeutralBgSurface.G,
			B: styletokens.NeutralBgSurface.B,
			A: 0xff,
		}
		st.hs = heatmapscroll.New(inst.ids, "cpu-heatmap", st.cfg, cpuHeatmapWidthSlots, st.heightSlots)
		// ScrollLeft: newest column on the RIGHT, oldest on the LEFT,
		// scrolling right-to-left like the per-core sparklines below and
		// every other plot in the app. X tick labels render in the same
		// order (newest rightmost) so motion and labels point the same way.
		// (Until 2026-06-17 this used ScrollRight/newest-left; flipped in
		// tandem with imzrt's spectrogram so both dashboards scroll alike.)
		st.hs.SetOrientation(heatmapscroll.ScrollLeft)
		st.colBuf = make([]float32, st.heightSlots)
		// Prefill the ring with NaN columns so the widget shows a
		// full rectangle on first open instead of a sparse strip of
		// "real" data on one edge with transparent void on the other.
		// NaN maps to BadColor (set above) per colormap.Map semantics.
		for i := range st.colBuf {
			st.colBuf[i] = float32(math.NaN())
		}
		for j := uint32(0); j < cpuHeatmapWidthSlots; j++ {
			st.hs.PushColumn(st.colBuf)
		}
	}

	// Drain new sample. The bundleSnap timestamp is the publication clock;
	// per-tick monotonic. Guards against double-pushing the same column
	// across many render frames between sampler ticks.
	if snap.SampledAtUnixMs > st.lastPushedMs {
		// Replicate each core's value across its band so a 32-core
		// box renders as a 32*bandHeight px-tall heatmap instead of
		// the unreadable 32 px strip the raw row-count would give.
		for i := uint32(0); i < st.nCores; i++ {
			v := float32(0)
			if int(i) < len(snap.LatestCPU.PerCorePercent) {
				v = float32(snap.LatestCPU.PerCorePercent[i])
			}
			base := i * cpuHeatmapBandHeight
			for k := uint32(0); k < cpuHeatmapBandHeight; k++ {
				st.colBuf[base+k] = v
			}
		}
		st.hs.PushColumn(st.colBuf)
		st.lastPushedMs = snap.SampledAtUnixMs
	}

	c.AddSpace(inst.spaceTight())

	// Stretch the heatmap to fill the panel: full width (minus a small pad)
	// so the newest column lands on the panel's right edge — matching the
	// per-core sparklines, the line plots, and imzrt's spectrogram — and as
	// much vertical space as the chrome + sparkline grid below leave free, so
	// users see it grow when they enlarge the panel. 0 along an axis keeps the
	// native slot-count size as a first-frame fallback before the available
	// size is known. One-frame lag is fine — the captured size reflects the
	// previous frame's available_size, stable across frames at a fixed leaf.
	c.CaptureAvailableSize()
	avail := c.CurrentApplicationState.StateManager.GetAvailableSize()
	texW := float32(0)
	if avail.W > 0 && !math.IsNaN(float64(avail.W)) {
		if cand := avail.W - cpuHeatmapAxisPad; cand > cpuHeatmapMinTexW {
			texW = cand
		}
	}
	texH := float32(0)
	if avail.H > 0 && !math.IsNaN(float64(avail.H)) {
		minH := float32(st.heightSlots)
		maxH := minH * cpuHeatmapMaxStretch
		texH = avail.H - cpuHeatmapReservedBelowPx
		if texH < minH {
			texH = minH
		}
		if texH > maxH {
			texH = maxH
		}
	}
	st.hs.SetDisplaySize(texW, texH)

	st.hs.Render()

	// Effective rendered width for the x-axis + cursor strip below: the
	// stretched texW, or the native slot count before the size is known.
	xw := texW
	if xw <= 0 {
		xw = float32(cpuHeatmapWidthSlots)
	}

	// Layout below the heatmap, top-to-bottom:
	//   1. Time-axis tick labels — kept adjacent to the plot so the
	//      reader can read column → time without their eye travelling
	//      past the hover row first. Labels are positioned by time
	//      across the texture width and read as "approximately at
	//      this time".
	//   2. Cursor strip — a transparent canvas the same width as the
	//      heatmap with a single vertical line at the hovered column's
	//      screen-x. Stays column-aligned because both widgets render
	//      at the same stretched width (xw).
	//   3. "hover: X ago" readout — the descriptive text floats lowest
	//      so it doesn't crowd the axis labels.
	if len(snap.HistoryTimeUnixSec) >= 2 {
		inst.renderCPUHeatmapXTicks(snap.HistoryTimeUnixSec, xw)
	}
	st.renderCPUHeatmapCursor(inst, xw)
}

// cpuPercentBgColor maps a CPU percentage through the heatmap
// palette and returns it as a semi-transparent egui colour suitable
// for a TintedScope cell background. Used by the process table to
// colour-code its CPU% column with the same palette the heatmap is
// showing — both surfaces resolve their palette through
// cpuHeatmapPalette() so the IDS env layer's choice (Tier-1
// IDS_PALETTE_SEQUENTIAL + Tier-2 IDS_ACCESSIBILITY) propagates to
// every CPU-coloured cell. Lazy-initialises cpuHeatmap.cfg if the
// heatmap hasn't rendered yet (e.g. CPU tab inactive on first frame
// while the process tab is visible).
func (inst *App) cpuPercentBgColor(pct float32) (col egcolor.Color) {
	st := &inst.cpuHeatmap
	if st.cfg == nil {
		st.cfg = colormap.NewConfig(cpuHeatmapPalette(), 0, 100)
		// IDS NeutralBgSurface — sits just above the panel background
		// so the pre-filled NaN ring reads as a rectangle without
		// competing with real palette stops once samples land.
		st.cfg.BadColor = color.NRGBA{
			R: styletokens.NeutralBgSurface.R,
			G: styletokens.NeutralBgSurface.G,
			B: styletokens.NeutralBgSurface.B,
			A: 0xff,
		}
	}
	var src [1]float32
	var dst [1]uint32
	src[0] = pct
	st.cfg.Map(src[:], dst[:])
	// Palette stops ship with alpha 0xff; substitute our semi-opaque
	// alpha so the egui dark-theme text still reads against the tint.
	col = egcolor.Hex((dst[0] & 0xffffff00) | cpuHeatmapCellBgAlpha)
	return
}

// renderCPUHeatmapCursor emits the cursor strip + "X ago" readout.
// Layout: a transparent canvas the same width as the heatmap (w) with a
// single vertical line at the hovered column's screen-x, followed by a
// label rendered through go-humanize so durations format the same way the
// rest of the runtime does ("5 seconds ago", "2 minutes ago", "an hour
// ago"). When the pointer isn't over the heatmap the label falls back to
// "—" so the row's vertical space stays stable.
func (st *cpuHeatmapState) renderCPUHeatmapCursor(inst *App, w float32) {
	_, col, hovered := st.hs.HoveredCell()
	slots := cpuHeatmapWidthSlots

	tickInterval := 1 * time.Second
	if sampler != nil {
		tickInterval = sampler.Interval()
	}

	label := "—"
	var pxCursor float32
	if hovered {
		// age = ticks since the newest column; independent of scroll
		// direction. head-1 is the just-pushed (newest) ring slot, so the
		// gap back to the hovered col (mod slots) is how many ticks ago it
		// was pushed.
		head := st.hs.Head()
		age := (head + slots - 1 - col) % slots
		// ScrollLeft puts the newest column on the RIGHT (slot x = slots-1)
		// and the oldest on the left, so slot x = (slots-1) - age. Scale
		// that slot to a pixel on the stretched strip (cell centre).
		slotX := slots - 1 - age
		pxCursor = (float32(slotX) + 0.5) / float32(slots) * w
		dur := time.Duration(int64(age)) * tickInterval
		// humanize.Time wraps the relative format ("X ago" / "in X").
		// Negative offset from now gives "X ago" for any positive age.
		label = humanize.Time(time.Now().Add(-dur))
	}

	if hovered {
		c.PaintLine(
			pxCursor, 0,
			pxCursor, cpuHeatmapCursorStripHeight,
			colorCursor, 1.5,
		).Send()
	}
	c.PaintCanvas(
		inst.ids.PrepareStr("cpu-heatmap-cursor"),
		w, cpuHeatmapCursorStripHeight,
	).Background(egcolor.Transparent).Send()

	c.AddSpace(inst.spaceHair())
	c.Label(fmt.Sprintf("hover: %s", label)).Send()
}

// renderCPUHeatmapXTicks paints calendar-aware time labels under the heatmap,
// spanning the texture width w. The heatmap scrolls ScrollLeft (newest on the
// right), so the newest tick sits at x=w and time increases left-to-right —
// matching the per-core sparklines and every other plot in the app. Labels are
// positioned by time across the full width (not fixed-gap) so they stay aligned
// with the stretched texture. Only the last cpuHeatmapWidthSlots samples are
// visible, so the range comes from that tail of the history. Edge ticks anchor
// inward to avoid clipping. Uses boxer's calendar-aware tick generator so
// labels are stable across the run ("15:42:00", "15:43:00", …).
func (inst *App) renderCPUHeatmapXTicks(timeUnixSec []float64, w float32) {
	n := len(timeUnixSec)
	if n < 2 || w <= 0 {
		return
	}
	lo := 0
	if n > int(cpuHeatmapWidthSlots) {
		lo = n - int(cpuHeatmapWidthSlots)
	}
	minT := time.Unix(int64(timeUnixSec[lo]), 0).Local()
	maxT := time.Unix(int64(timeUnixSec[n-1]), 0).Local()
	minMS := minT.UnixMilli()
	spanMS := maxT.UnixMilli() - minMS
	if spanMS <= 0 {
		return
	}
	layout := timeticks.TimeTicks(minT, maxT, timeticks.TimeTickOptions{
		PanelWidthPx:    int32(w),
		TargetSpacingPx: 120,
		Location:        time.Local,
	})
	const edgeGuard float32 = 18
	for i, tv := range layout.TickValues {
		if i >= len(layout.TickLabels) {
			break
		}
		// newest-right: oldest → x=0, newest → right edge (x=w).
		norm := float64(tv.UnixMilli()-minMS) / float64(spanMS)
		px := float32(norm * float64(w))
		if px < 0 || px > w {
			continue
		}
		c.PaintLine(px, 0, px, 4, colorAxisTick, 1.0).Send()
		ah := uint8(1)
		switch {
		case px < edgeGuard:
			ah = 0
		case px > w-edgeGuard:
			ah = 2
		}
		c.PaintText(px, 6, ah, 0, layout.TickLabels[i], cpuHeatmapXAxisFont, colorAxisLabel).Send()
	}
	c.PaintCanvas(inst.ids.PrepareStr("cpu-heatmap-xaxis"), w, cpuHeatmapXAxisH).
		Background(colorBgClear).
		Send()
}
