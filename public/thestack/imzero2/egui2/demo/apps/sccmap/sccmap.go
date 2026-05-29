//go:build llm_generated_opus47

package sccmap

import (
	"fmt"
	"math"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/distsummary"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/scctree"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// sccMetric pairs a human-readable label with one of scctree's Weight
// extractors. The same registry feeds both the size-metric and the
// color-metric ComboBox, since both axes map onto the same numeric
// dimensions a file has under scc.
type sccMetric struct {
	Name string
	W    scctree.Weight
}

// sccMetrics is the registry surfaced by the metric ComboBoxes. The
// fallback fill metric ("Code lines") is index 0; "Complexity" is last
// to mirror the historical default colorWeight.
var sccMetrics = []sccMetric{
	{"Code lines", scctree.WeightCode},
	{"Total lines", scctree.WeightLines},
	{"Bytes", scctree.WeightBytes},
	{"Complexity", scctree.WeightComplexity},
}

const (
	defaultSizeMetricIdx  = 0 // Code lines
	defaultColorMetricIdx = 3 // Complexity
	// Fallback canvas size used until the first captureAvailableSize
	// landing. Matches the previous hard-coded WithContainerSize so
	// frame 1 keeps the historical visual.
	fallbackContainerW float32 = 900
	fallbackContainerH float32 = 550
	// colorscaleH is the rendered height of the legend canvas, set on
	// both the colorscale.New call and the chrome budget below so the
	// two stay aligned.
	colorscaleH float32 = 56
	// treemapChrome{W,H} reserves room on each axis so the container
	// Frame's outer rect — which is what allocate_ui_at_rect propagates
	// to the parent's min_rect — doesn't exceed the available area.
	//
	// The H budget covers the treemap's vertical chrome (breadcrumb
	// ~22 px, inter-section padding ~4 + ~9 item_spacings, status
	// label ~18 = ~53 px) AND the colorscale row rendered AFTER the
	// treemap. The legend has to be the last PaintCanvas in the frame
	// so its PaintCanvas, not a treemap hatch's, owns R14 — otherwise
	// the colorscale's hover detection reads stale pointer state from
	// the last hatched leaf cell and misfires (see
	// `feedback-egui-frame-outer-overshoot`'s sibling: R14 is global,
	// only the most-recent PaintCanvas wins).
	//
	// The W budget covers a subtle bug that only triggered at top
	// level: `egui::Frame::outer_rect = content + inner_margin + stroke
	// + outer_margin` (frame.rs). The treemap cell Frame uses
	// InnerMarginSides(3,3,2,2) + Stroke(BorderWidth) and sets content
	// via UiSetMinWidth(cellW-7). So a drillable cell (BorderWidth =
	// 1.2..1.8) allocates `(cellW-7)+6+2*1.8 = cellW+2.6` wide via
	// allocate_ui_at_rect — overshoots the requested cell rect by up
	// to 2.6 px. The bottom-right cell pushes the container Frame's
	// outer width to containerW + 2.6, which exceeds the captured
	// availW the next frame and grows the host Window monotonically.
	// Invisible after drill-in because drillUp cells use stroke 0.8
	// (smaller overshoot) and preview/frontier cells sit inside the
	// active cell's inner area, away from the container edge. Sized
	// to swallow a hover (stroke 1.8 → +2.6) with margin.
	treemapChromeW float32 = 12
	// distSummaryRowH covers the height of the Horizontal row that holds the
	// per-metric distsummary widgets — one monospace label line (font ≈ 14 px)
	// plus an item_spacing above and below. Sized loose enough to absorb the
	// chart-line icon ascent without ratcheting captureAvailableSize.
	distSummaryRowH float32 = 24
	// distGutterW pins the minimum width of the "Size: ", "Color: ", and
	// "Size & color: " gutter label inside renderDistSummaries. Sized
	// loose enough to hold "Size & color (Complexity):" (the widest of
	// the three forms) at the default font without truncation, so the
	// inline summary lines start at the same x across metric switches.
	distGutterW float32 = 200
	treemapChromeH  float32 = 96 + 56 + 8 + distSummaryRowH // chrome + colorscale + inter-row gap + dist row
	// containerGrowGuardPx is the anti-ratchet deadband applied to
	// both axes of SetContainerSize. Frame-over-frame upward deltas
	// smaller than this are almost always chrome-budget mis-estimates
	// rather than user intent and would resurrect the growth loop;
	// clamp them to the previous applied value. Downward deltas (user
	// shrinking the window) and large upward deltas (released resize,
	// content rebuild) pass through. Sized larger than typical slow-
	// drag deltas (~2 px/frame) — slow-drag-up feels sluggish, but
	// tearing down the growth loop matters more.
	containerGrowGuardPx float32 = 24
)

// sccDataOnce gates the one-shot RunScc invocation. The raw SccGroups
// and repo basename are read-only after Do() returns and safely shared
// across every open window's per-instance *Treemap. Caching the raw
// groups (rather than the final tree) lets the metric switcher rebuild
// the *layout.Node + colorFn cheaply without re-running the scc
// subprocess.
var (
	sccDataOnce sync.Once
	sccGroups   []scctree.SccGroup
	sccRootName string
	sccDataErr  error
)

func ensureSccData() {
	sccDataOnce.Do(func() {
		root, err := scctree.RepoRoot()
		if err != nil {
			sccDataErr = eh.Errorf("scctree.RepoRoot: %w", err)
			log.Warn().Err(sccDataErr).Msg("sccmap: scc subprocess failed")
			return
		}
		groups, err := scctree.RunScc(root)
		if err != nil {
			sccDataErr = eh.Errorf("scctree.RunScc: %w", err)
			log.Warn().Err(sccDataErr).Msg("sccmap: scc subprocess failed")
			return
		}
		sccGroups = groups
		sccRootName = filepath.Base(root)
	})
}

// buildTreeForMetrics builds a fresh tree + value extractor + colormap
// upper-bound under the chosen size/color weights. Always returns a usable
// (non-nil) root so the widget never sees nil — error and empty-tree cases
// collapse to a single-leaf placeholder.
//
// includeGenerated toggles whether scc files that scctree.IsGenerated
// flags (scc's --gen heuristic OR the project's .gen.go / .out.go /
// golay24 conventions) are included in the tree. False (default) keeps
// the historical sccmap view focused on hand-written code.
//
// maxValue is the colormap upper bound. minValue is fixed at 1 to keep
// NewLogColormap valid; raw values below 1 (or zero-complexity leaves)
// clamp to palette[0] on the legend.
func buildTreeForMetrics(sizeIdx, colorIdx int, includeGenerated bool) (root *layout.Node, valueFn func(*layout.Node) float64, maxValue float64) {
	if sccDataErr != nil {
		root = &layout.Node{Name: fmt.Sprintf("scc failed: %v", sccDataErr), Size: 1}
		valueFn = func(*layout.Node) float64 { return 0 }
		maxValue = 1
		return
	}
	sizeW := sccMetrics[sizeIdx].W
	colorW := sccMetrics[colorIdx].W
	var keep func(*scctree.SccFile) bool
	if !includeGenerated {
		keep = func(f *scctree.SccFile) bool { return !scctree.IsGenerated(f) }
	}
	root, valueFn, maxValue = scctree.BuildColormappedTree(
		sccGroups, sccRootName,
		sizeW, colorW,
		keep,
	)
	if len(root.Children) == 0 {
		root = &layout.Node{Name: "no files with non-zero size", Size: 1}
		valueFn = func(*layout.Node) float64 { return 0 }
		maxValue = 1
	}
	// NewLogColormap requires strictly min < max with both > 0. Clamp the
	// upper bound to a value safely above 1 so the panic contract holds
	// for empty or all-zero datasets.
	if maxValue < 2 {
		maxValue = 2
	}
	return
}

// App is the per-window sccmap instance. The treemap widget lives on
// the receiver so two open windows have independent zoom / animation
// state and independent metric selections; the underlying scc groups
// are process-static, computed once via sccDataOnce on first Mount.
type App struct {
	ids *c.WidgetIdStack
	tm  *treemap.Treemap
	// cs is the gradient legend bound to the same *treemap.Colormap that
	// drives the treemap's ContinuousColoring, so the two stay in sync
	// automatically (treemap.Colormap is shared by pointer).
	cs *colorscale.ColorScale
	// hoverBand decorates the treemap's ContinuousColoringFromMap so a
	// hover over the legend dims cells outside a ±5% normalized band
	// around the hovered value. cs.OnHover drives SetBand/ClearBand.
	hoverBand *colorscale.HoverBand

	sizeMetricIdx    int
	colorMetricIdx   int
	includeGenerated bool

	// sizeDigest / colorDigest summarise the per-file weight under the
	// currently-selected size and color metrics across every leaf the
	// treemap would draw (same keep predicate, same value extractor).
	// Rebuilt by rebuildTreemap alongside the treemap itself so the two
	// surfaces stay in lock-step. colorDigest aliases sizeDigest when
	// the two metric indices match — the second distsummary widget is
	// suppressed in that case to avoid showing the same distribution
	// twice.
	sizeDigest  *tdigest.TDigest
	colorDigest *tdigest.TDigest
	// distRenderer is the configure-once distsummary template shared by
	// both metric summaries. Renderer is value-typed and stateless, so
	// per-row .Render calls take fresh prepared ids without disturbing
	// each other.
	distRenderer distsummary.Renderer

	// density is captured once at construction (IMZERO2_DENSITY) and
	// fed to styletokens.* accessors so spacing scales with the IDS
	// density preset rather than being hardcoded in pixels.
	density styletokens.DensityE

	// lastContainerW / lastContainerH remember the size we passed to
	// SetContainerSize last frame so containerGrowGuardPx can clamp
	// tiny upward deltas on either axis caused by chrome-estimate
	// error in the capture loop. Both axes need this — see the long
	// comment on treemapChromeW for the per-cell stroke overshoot
	// that caused W to grow despite the H guard.
	lastContainerW float32
	lastContainerH float32
}

var _ runtimeapp.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids:            c.NewWidgetIdStack(),
		sizeMetricIdx:  defaultSizeMetricIdx,
		colorMetricIdx: defaultColorMetricIdx,
		density:        styletokens.DensityFromEnv(),
		distRenderer:   distsummary.New("scc-dist"),
	}
	return
}

func (inst *App) Manifest() (m runtimeapp.Manifest) { m = manifest; return }

// computeMetricDigest streams every kept file in groups through a fresh
// TDigest under the given weight. Mirrors buildTreeForMetrics' file-walk
// filter (scctree.IsGenerated when keep is set) so the resulting digest
// covers exactly the leaves the treemap visualises — switching the
// include-generated checkbox or either metric ComboBox propagates the
// same set into both surfaces.
func computeMetricDigest(groups []scctree.SccGroup, w scctree.Weight, keep func(*scctree.SccFile) bool) (d *tdigest.TDigest) {
	d = tdigest.NewTDigest()
	for gi := range groups {
		g := &groups[gi]
		for fi := range g.Files {
			f := &g.Files[fi]
			if keep != nil && !keep(f) {
				continue
			}
			d.Push(w(f))
		}
	}
	return
}

// rebuildTreemap constructs a fresh *Treemap and a matching ColorScale
// legend for the current (sizeMetricIdx, colorMetricIdx). Replaces
// inst.tm wholesale so the breadcrumb resets to root — the file ordering
// changes whenever the size weight changes, so preserving the old path
// would point at the wrong cells anyway. The Colormap is shared between
// treemap.ContinuousColoringFromMap and colorscale.New so the legend
// gradient and the treemap cell colors are guaranteed to agree.
func (inst *App) rebuildTreemap() {
	root, valueFn, maxValue := buildTreeForMetrics(inst.sizeMetricIdx, inst.colorMetricIdx, inst.includeGenerated)
	// Same keep predicate buildTreeForMetrics uses internally so the
	// distsummary digests survey the identical leaf set the treemap
	// renders. Kept local rather than threaded out of buildTreeForMetrics
	// because the function already returns three values and threading a
	// fourth (the predicate) would not help any other caller.
	var keep func(*scctree.SccFile) bool
	if !inst.includeGenerated {
		keep = func(f *scctree.SccFile) bool { return !scctree.IsGenerated(f) }
	}
	inst.sizeDigest = computeMetricDigest(sccGroups, sccMetrics[inst.sizeMetricIdx].W, keep)
	if inst.colorMetricIdx == inst.sizeMetricIdx {
		// Both axes share a single distribution — alias the pointer so
		// downstream code can detect the collapse via pointer equality
		// without recomputing.
		inst.colorDigest = inst.sizeDigest
	} else {
		inst.colorDigest = computeMetricDigest(sccGroups, sccMetrics[inst.colorMetricIdx].W, keep)
	}
	cm := treemap.NewLogColormap(scctree.ComplexityPalette, 1, maxValue)
	inst.hoverBand = colorscale.NewHoverBand(
		cm,
		treemap.ContinuousColoringFromMap(cm, valueFn),
		valueFn,
	)
	inst.tm = treemap.New(inst.ids, "scc-treemap", root,
		treemap.WithColoring(treemap.CompositeColoring(
			treemap.DepthColoring(treemap.DefaultDepthColors),
			inst.hoverBand,
		)),
	)
	// ColorScale layout: gradient = 55% of height, then a 5 px tick row +
	// 2 px gap + fontSize-10 labels = ~17 px of axis chrome. h=32 placed
	// the label baseline at ~25, with text descending past the canvas's
	// clip rect and clipping the digits vertically. h=56 puts the
	// gradient at 30 px (still legible) with ~26 px of room for the
	// axis below.
	inst.cs = colorscale.New(inst.ids, "scc-colorscale", cm,
		colorscale.WithSize(280, colorscaleH),
		colorscale.WithDesiredTicks(4),
	)
	// Wire the legend hover into the hover-band decorator. The closure
	// captures inst (not inst.hoverBand directly), so subsequent rebuilds
	// pick up the fresh *HoverBand pointer automatically — no need to
	// reattach the callback when the user switches metrics.
	inst.cs.OnHover(func(h colorscale.HoverInfo) {
		if !h.Ok {
			inst.hoverBand.ClearBand()
			return
		}
		inst.hoverBand.SetBand(h.Value)
	})
}

func (inst *App) Mount(ctx runtimeapp.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	ensureSccData()
	inst.rebuildTreemap()
	return
}

func (inst *App) Unmount(ctx runtimeapp.MountContextI) (err error) { return }

func (inst *App) Frame(ctx runtimeapp.FrameContextI) (err error) {
	prevSize, prevColor := inst.sizeMetricIdx, inst.colorMetricIdx
	// Checkbox uses r10 databinding for state, which has one-frame lag:
	// inst.includeGenerated is updated by StateManager.Sync BEFORE Frame
	// runs, so a "snapshot at start of frame" comparison can never see
	// the transition (the snapshot is the post-Sync value). Use the
	// response flag from SendRespVal directly — egui Checkbox marks the
	// response changed on toggle (unlike RadioButton; see
	// [[feedback-radio-haspricked]]).
	genChanged := false
	for range c.Horizontal().KeepIter() {
		inst.sizeMetricIdx = renderMetricCombo(inst.ids, "size-metric", "Size", inst.sizeMetricIdx)
		c.AddSpace(styletokens.GapItems(inst.density))
		inst.colorMetricIdx = renderMetricCombo(inst.ids, "color-metric", "Color", inst.colorMetricIdx)
		c.AddSpace(styletokens.GapSections(inst.density))
		if c.Checkbox(inst.ids.PrepareStr("include-gen"), inst.includeGenerated, "Include generated").
			SendRespVal(&inst.includeGenerated).HasChanged() {
			genChanged = true
		}
	}
	if inst.sizeMetricIdx != prevSize || inst.colorMetricIdx != prevColor || genChanged {
		inst.rebuildTreemap()
	}

	inst.renderDistSummaries()

	w, h := availableContainerSize()
	if inst.lastContainerW > 0 && w > inst.lastContainerW && w-inst.lastContainerW < containerGrowGuardPx {
		w = inst.lastContainerW
	}
	if inst.lastContainerH > 0 && h > inst.lastContainerH && h-inst.lastContainerH < containerGrowGuardPx {
		h = inst.lastContainerH
	}
	inst.lastContainerW = w
	inst.lastContainerH = h
	inst.tm.SetContainerSize(w, h)
	c.CaptureAvailableSize()

	inst.tm.Render()

	// Colorscale legend renders LAST so its PaintCanvas, not a treemap
	// hatch's, is the final R14 writer of the frame. The cs reads R14
	// from StateManager.GetCanvasPointer next frame to detect hovers;
	// R14 is a single global slot. Top-level leaf cells in the SCC tree
	// (files at the repo root) fall through to DefaultStyle's hatched
	// default branch — paintHatch emits a PaintCanvas per leaf, each
	// one overwriting R14. With the colorscale rendered at the top,
	// every one of those hatch canvases overwrites cs's R14 state, so
	// cs.OnHover never fires. Placing cs after the treemap makes its
	// PaintCanvas the final R14 writer; demo treemap escaped this
	// because its sample tree has no leaf children at the top level.
	inst.cs.Render()
	return
}

// availableContainerSize reports the (w, h) the treemap should fill
// this frame. Reads R18 from last frame's captureAvailableSize; falls
// back to the historical 900×550 while the value is NaN (first frame
// or capture-outside-Ui). Both axes are reduced by their respective
// chrome budgets (see treemapChromeW / treemapChromeH) so the treemap
// Frame's outer rect — including the per-cell stroke overshoot — fits
// inside the captured area and doesn't drive the host Window's
// monotonic auto-grow loop.
func availableContainerSize() (w, h float32) {
	avail := c.CurrentApplicationState.StateManager.GetAvailableSize()
	w, h = fallbackContainerW, fallbackContainerH
	if !math.IsNaN(float64(avail.W)) && avail.W > treemapChromeW {
		w = avail.W - treemapChromeW
	}
	if !math.IsNaN(float64(avail.H)) && avail.H > treemapChromeH {
		h = avail.H - treemapChromeH
	}
	return
}

// renderMetricCombo paints an egui-native labeled ComboBox ("[▾ value]
// Label") bound to a sccMetrics index and returns the (possibly
// updated) selection. The label is passed via the ComboBox's own label
// argument so it renders to the RIGHT of the dropdown — that is egui's
// built-in labeled-control layout. The dropdown frame, the selected
// text, and the label all live inside one widget id, so the egui
// Horizontal layout cross-centers the whole unit against neighbours
// (other combos, the include-generated checkbox) without the
// label-baseline-vs-button-baseline drift a separate leading c.Label
// would have introduced.
func renderMetricCombo(ids *c.WidgetIdStack, scopeKey, label string, idx int) (out int) {
	out = idx
	if out < 0 || out >= len(sccMetrics) {
		out = 0
	}
	current := sccMetrics[out].Name
	for range c.ComboBox(ids.PrepareStr(scopeKey),
		c.WidgetText().Text(label).Keep(),
		c.WidgetText().Text(current).Keep()).KeepIter() {
		for i, m := range sccMetrics {
			selected := i == out
			if c.Button(ids.PrepareStr(fmt.Sprintf("%s-opt-%d", scopeKey, i)),
				c.Atoms().Text(m.Name).Keep()).
				Selected(selected).
				FrameWhenInactive(!selected).
				Frame(true).
				SendResp().HasPrimaryClicked() {
				out = i
			}
		}
	}
	return
}

// renderDistSummaries paints one or two distsummary widgets — one per
// currently-selected metric — inside a single Horizontal row. The row
// uses a fixed-width "Size: " / "Color: " gutter label so the inline
// 5-number-summary lines align across rebuilds (different metrics
// produce different label widths otherwise). When both metric indices
// match the digests are pointer-aliased; rendering collapses to a
// single combined entry to avoid showing the same distribution twice.
func (inst *App) renderDistSummaries() {
	if inst.sizeDigest == nil {
		// rebuildTreemap always sets both digests before Frame runs, but
		// a defensive skip keeps the function safe to call before Mount
		// completes (e.g. if the call path ever changes).
		return
	}
	sizeName := sccMetrics[inst.sizeMetricIdx].Name
	colorName := sccMetrics[inst.colorMetricIdx].Name
	aliased := inst.sizeDigest == inst.colorDigest
	for range c.Horizontal().KeepIter() {
		c.UiSetMinWidth(distGutterW)
		if aliased {
			c.Label("Size & color (" + sizeName + "):").Send()
		} else {
			c.Label("Size (" + sizeName + "):").Send()
		}
		c.AddSpace(styletokens.GapItems(inst.density))
		inst.distRenderer.Render(inst.ids.PrepareStr("size-dist"), inst.sizeDigest, nil)
		if !aliased {
			c.AddSpace(styletokens.GapSections(inst.density))
			c.Label("Color (" + colorName + "):").Send()
			c.AddSpace(styletokens.GapItems(inst.density))
			inst.distRenderer.Render(inst.ids.PrepareStr("color-dist"), inst.colorDigest, nil)
		}
	}
}
