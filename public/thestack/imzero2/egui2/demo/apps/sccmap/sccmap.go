package sccmap

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/distsummary"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
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
	// Humanize renders a value of this metric for compact in-cell display
	// (the treemap secondary label): counts use a terse SI form (1.5k),
	// bytes use dustin/go-humanize's decimal units (1.2 MB).
	Humanize func(float64) string
}

// sccMetrics is the registry surfaced by the metric ComboBoxes. The
// fallback fill metric ("Code lines") is index 0; "Complexity" is last
// to mirror the historical default colorWeight.
var sccMetrics = []sccMetric{
	{"Code lines", scctree.WeightCode, humanizeCount},
	{"Total lines", scctree.WeightLines, humanizeCount},
	{"Bytes", scctree.WeightBytes, humanizeBytes},
	{"Complexity", scctree.WeightComplexity, humanizeCount},
}

// humanizeCount renders a non-negative count compactly for an in-cell
// label: a bare integer below 1000, otherwise one fractional digit with a
// k / M / G suffix (1500 → "1.5k", 12000 → "12k", 2e9 → "2G"). A trailing
// ".0" is trimmed so round values read cleanly. Negatives clamp to "0".
func humanizeCount(v float64) (s string) {
	if v < 1000 {
		if v < 0 {
			v = 0
		}
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	var div float64
	var suffix string
	switch {
	case v < 1e6:
		div, suffix = 1e3, "k"
	case v < 1e9:
		div, suffix = 1e6, "M"
	default:
		div, suffix = 1e9, "G"
	}
	s = strconv.FormatFloat(v/div, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return s + suffix
}

// humanizeBytes renders a byte count via dustin/go-humanize (decimal SI:
// "1.2 kB", "3.4 MB"), matching the filepicker's size readout. Negative or
// fractional inputs are floored to a uint64 first.
func humanizeBytes(v float64) string {
	if v < 0 {
		v = 0
	}
	return humanize.Bytes(uint64(v))
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
	// The two summary rows below the controls — the per-metric distsummary
	// row and the aggregate "Σ" totals row (renderTotals) — are each one
	// monospace line tall, so both reserve distSummaryRowH. They sit above
	// the captureAvailableSize point, mirroring each other's budget.
	treemapChromeH float32 = 96 + 56 + 8 + 2*distSummaryRowH // chrome + colorscale + inter-row gap + dist row + totals row
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
	// repoPathWidth is the desired width of the header "Repository:" path box.
	repoPathWidth float32 = 420
	// scanProgressWidth bounds the header progress block (bar + note + Cancel)
	// so it reads as a compact widget beside the path box rather than filling
	// the row and shoving the Cancel button off-screen.
	scanProgressWidth float32 = 380
)

// repoEnv seeds the initial scan target when a window opens; empty scans the
// git worktree the host runs in (resolved to its toplevel by the first scan).
// Each window can then retarget through the header path box.
var repoEnv = env.NewPath(env.Spec{
	Name:        "SCCMAP_REPO",
	Description: "initial repository path explored by the Repo code exploration app; empty scans the host's worktree",
	Category:    env.CategoryDev,
})

// sccData is one repository's scc result: the raw per-language groups plus the
// resolved root and its basename (the treemap root label). It is built off the
// render thread by scanScc and handed to the render thread consume-once via
// the bgjob Runner, so render code never reads a half-built scan. Caching the
// raw groups (rather than the final tree) lets the metric switcher rebuild the
// *layout.Node + colormap cheaply without re-running the scc subprocess.
type sccData struct {
	repoPath     string // the path the scan was started with
	resolvedRoot string // git toplevel of repoPath (or the path/worktree itself)
	rootName     string // basename of resolvedRoot; the treemap root label
	groups       []scctree.SccGroup
}

// scanScc resolves path to a git worktree root — falling back to the path
// itself, then the working directory, when it is not a git repository (scc
// needs no VCS) — and runs scc there. It executes on the bgjob worker
// goroutine; a cancelled ctx kills the scc child. report publishes the two
// indeterminate phases so the header bar animates while scc runs.
func scanScc(ctx context.Context, path string, report bgjob.Reporter) (data *sccData, err error) {
	if report == nil {
		report = func(uint64, uint64, string) {}
	}
	report(0, 0, "resolving worktree")
	root, rootErr := scctree.RepoRootAt(path)
	if rootErr != nil || root == "" {
		// Not a git worktree (or git unavailable): visualize the entered
		// directory directly. An empty path resolves to the working dir so the
		// header box can show a concrete target after the scan lands.
		root = path
		if root == "" {
			if wd, wdErr := os.Getwd(); wdErr == nil {
				root = wd
			}
		}
	}
	report(0, 0, "running scc")
	var groups []scctree.SccGroup
	groups, err = scctree.RunSccContext(ctx, root)
	if err != nil {
		err = eh.Errorf("scctree.RunScc: %w", err)
		return
	}
	data = &sccData{
		repoPath:     path,
		resolvedRoot: root,
		rootName:     displayName(root),
		groups:       groups,
	}
	return
}

// scanSccSync runs a scan on the calling goroutine. The demo tour uses it:
// blocking is fine in a screenshot harness, and each scene needs its data
// ready at Init time rather than a frame or two later.
func scanSccSync(path string) (data *sccData, err error) {
	return scanScc(context.Background(), path, nil)
}

// displayName names the treemap root after the repository directory, falling
// back to "repo" for a root with no usable basename.
func displayName(root string) (name string) {
	name = filepath.Base(root)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "repo"
	}
	return
}

// buildTreeForMetrics builds a fresh tree + value extractor + colormap
// upper-bound under the chosen size/color weights from the instance's current
// scan (inst.data). Always returns a usable (non-nil) root so the widget never
// sees nil — the no-data and empty-tree cases collapse to a single-leaf
// placeholder.
//
// keep is the leaf-inclusion predicate (see App.keepFunc): nil keeps every
// file, otherwise a file is included only when keep(f) is true. The same
// predicate is fed to computeMetricDigest so the treemap and the distsummary
// digests survey the identical leaf set.
//
// maxValue is the colormap upper bound. minValue is fixed at 1 to keep
// NewLogColormap valid; raw values below 1 (or zero-complexity leaves)
// clamp to palette[0] on the legend.
func (inst *App) buildTreeForMetrics(sizeIdx, colorIdx int, keep func(*scctree.SccFile) bool) (root *layout.Node, valueFn func(*layout.Node) float64, maxValue float64) {
	if inst.data == nil {
		root = &layout.Node{Name: "no scan data", Size: 1}
		valueFn = func(*layout.Node) float64 { return 0 }
		maxValue = 1
	} else {
		sizeW := sccMetrics[sizeIdx].W
		colorW := sccMetrics[colorIdx].W
		root, valueFn, maxValue = scctree.BuildColormappedTree(
			inst.data.groups, inst.data.rootName,
			sizeW, colorW,
			keep,
		)
		if len(root.Children) == 0 {
			root = &layout.Node{Name: "no files with non-zero size", Size: 1}
			valueFn = func(*layout.Node) float64 { return 0 }
			maxValue = 1
		}
	}
	// NewLogColormap requires strictly min < max with both > 0. Clamp the
	// upper bound to a value safely above 1 so the panic contract holds for
	// every degenerate dataset. This must run on ALL paths — the no-data
	// branch above returns maxValue=1, which would otherwise slip past the
	// clamp and panic NewLogColormap(palette, 1, 1) before the first scan
	// lands (or when a repo has no source tree to analyse).
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

	// repoPath is the scan target, bound to the header path box. job runs the
	// scc scan off the render thread; data is the last completed scan, owned by
	// the render thread and read by every render helper. tasks wires the host
	// background-task UI; the Cancel button's id is a salted relative id off
	// ids, so it needs no per-instance qualifier.
	repoPath string
	job      bgjob.Runner[sccData]
	data     *sccData
	tasks    task.TaskApiI

	tm *treemap.Treemap
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
	// includeTests toggles whether files scctree.IsTest flags (test naming
	// conventions + canonical test directories) are surveyed. False
	// (default) keeps the view focused on non-test code, mirroring
	// includeGenerated. Changing it rebuilds the tree and digests.
	includeTests bool
	// showValues toggles the per-cell humanized size/color value label
	// drawn under each tile name (treemap.SetCellLabel). It does NOT trigger
	// a rebuild — the label closure reads this flag live — so flipping it
	// preserves the user's drill position.
	showValues bool

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
	// sizeTotal / colorTotal are the aggregate sums of the per-file weights
	// under the current size and color metrics across the same kept leaf set
	// the digests survey — the "Σ" totals rendered by renderTotals. Computed
	// alongside the digests in computeMetricDigest (a TDigest tracks the
	// observation count, not the value-sum — Weight() == n since every Push
	// has weight 1 — so the total cannot be read back from it). colorTotal
	// mirrors sizeTotal when the two metric indices coincide.
	sizeTotal  float64
	colorTotal float64
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
		showValues:     true,
		density:        styletokens.DensityFromEnv(),
		distRenderer:   distsummary.New("scc-dist"),
	}
	return
}

func (inst *App) Manifest() (m runtimeapp.Manifest) { m = manifest; return }

// computeMetricDigest streams every kept file in groups through a fresh
// TDigest under the given weight, and returns the running sum of those same
// weights as total. Mirrors buildTreeForMetrics' file-walk filter
// (scctree.IsGenerated when keep is set) so the digest and total cover
// exactly the leaves the treemap visualises — switching the include-generated
// checkbox or either metric ComboBox propagates the same set into all three
// surfaces (treemap, distribution, total).
//
// total is summed here rather than read back from the digest because a
// TDigest tracks observation count (Weight() == n, every Push has weight 1),
// not the sum of the pushed values; folding it into this single walk keeps
// the total exactly consistent with the digest by construction.
func computeMetricDigest(groups []scctree.SccGroup, w scctree.Weight, keep func(*scctree.SccFile) bool) (d *tdigest.TDigest, total float64) {
	d = tdigest.NewTDigest()
	for gi := range groups {
		g := &groups[gi]
		for fi := range g.Files {
			f := &g.Files[fi]
			if keep != nil && !keep(f) {
				continue
			}
			v := w(f)
			d.Push(v)
			total += v
		}
	}
	return
}

// keepFunc returns the leaf-inclusion predicate matching the current
// include-generated / include-tests toggles, or nil when both are enabled
// (keep every file — the BuildColormappedTree / computeMetricDigest fast
// path). A file is dropped when it is generated and generated files are
// excluded, or a test and test files are excluded. The single predicate is
// shared by the treemap build and both distsummary digests so every surface
// surveys the identical leaf set.
func (inst *App) keepFunc() (keep func(*scctree.SccFile) bool) {
	incGen := inst.includeGenerated
	incTest := inst.includeTests
	if incGen && incTest {
		return nil
	}
	return func(f *scctree.SccFile) bool {
		if !incGen && scctree.IsGenerated(f) {
			return false
		}
		if !incTest && scctree.IsTest(f) {
			return false
		}
		return true
	}
}

// makeCellLabelFn builds the treemap's per-cell secondary-label closure:
// the cell's size-metric and color-metric values in humanized form
// ("1.2k · 34"). It returns "" when the Show values toggle is off, so the
// toggle takes effect without a rebuild (preserving the drill position).
// The size dimension reads the node's own aggregated area (TotalSize); the
// color dimension reads valueFn, the extractor captured from this build. The
// two collapse to a single value when the size and color metrics coincide.
func (inst *App) makeCellLabelFn(valueFn func(*layout.Node) float64) func(*layout.Node) string {
	sizeM := sccMetrics[inst.sizeMetricIdx]
	colorM := sccMetrics[inst.colorMetricIdx]
	sameMetric := inst.sizeMetricIdx == inst.colorMetricIdx
	return func(n *layout.Node) string {
		if !inst.showValues {
			return ""
		}
		sizeStr := sizeM.Humanize(n.TotalSize())
		if sameMetric {
			return sizeStr
		}
		return sizeStr + " · " + colorM.Humanize(valueFn(n))
	}
}

// rebuildTreemap constructs a fresh *Treemap and a matching ColorScale
// legend for the current (sizeMetricIdx, colorMetricIdx). Replaces
// inst.tm wholesale so the breadcrumb resets to root — the file ordering
// changes whenever the size weight changes, so preserving the old path
// would point at the wrong cells anyway. The Colormap is shared between
// treemap.ContinuousColoringFromMap and colorscale.New so the legend
// gradient and the treemap cell colors are guaranteed to agree.
func (inst *App) rebuildTreemap() {
	keep := inst.keepFunc()
	root, valueFn, maxValue := inst.buildTreeForMetrics(inst.sizeMetricIdx, inst.colorMetricIdx, keep)
	var groups []scctree.SccGroup
	if inst.data != nil {
		groups = inst.data.groups
	}
	inst.sizeDigest, inst.sizeTotal = computeMetricDigest(groups, sccMetrics[inst.sizeMetricIdx].W, keep)
	if inst.colorMetricIdx == inst.sizeMetricIdx {
		// Both axes share a single distribution — alias the pointer so
		// downstream code can detect the collapse via pointer equality
		// without recomputing. The total collapses the same way.
		inst.colorDigest = inst.sizeDigest
		inst.colorTotal = inst.sizeTotal
	} else {
		inst.colorDigest, inst.colorTotal = computeMetricDigest(groups, sccMetrics[inst.colorMetricIdx].W, keep)
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
	// Secondary in-cell label: humanized size/color values, gated live on
	// inst.showValues so toggling needs no rebuild. Captures this build's
	// valueFn so it tracks the current color metric.
	inst.tm.SetCellLabel(inst.makeCellLabelFn(valueFn))
	// ColorScale layout: gradient = 55% of height, then a 5 px tick row +
	// 2 px gap + fontSize-10 labels = ~17 px of axis chrome. h=32 placed
	// the label baseline at ~25, with text descending past the canvas's
	// clip rect and clipping the digits vertically. h=56 puts the
	// gradient at 30 px (still legible) with ~26 px of room for the
	// axis below.
	inst.cs = colorscale.New(inst.ids, "scc-colorscale", cm.Config(),
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
	inst.tasks = task.ForApp(ctx)
	if inst.repoPath == "" {
		inst.repoPath = repoEnv.Get()
	}
	// The treemap + digests are built by drainScan when the first scan lands;
	// until then Frame renders the scanning placeholder (inst.data == nil).
	inst.startScan()
	return
}

func (inst *App) Unmount(ctx runtimeapp.MountContextI) (err error) {
	inst.job.Invalidate()
	return
}

func (inst *App) Frame(ctx runtimeapp.FrameContextI) (err error) {
	// Consume a completed scan (if any) before rendering, so the header box and
	// the treemap reflect the newest data this frame.
	inst.drainScan()

	inst.renderHeader()
	c.AddSpace(styletokens.GapItems(inst.density))

	// Before the first scan lands (or after a failure) there is no tree yet;
	// show the reason and skip the metric controls / treemap entirely.
	if inst.data == nil {
		inst.renderNoData()
		return
	}

	prevSize, prevColor := inst.sizeMetricIdx, inst.colorMetricIdx
	// Checkbox uses r10 databinding for state, which has one-frame lag:
	// inst.includeGenerated is updated by StateManager.Sync BEFORE Frame
	// runs, so a "snapshot at start of frame" comparison can never see
	// the transition (the snapshot is the post-Sync value). Use the
	// response flag from SendRespVal directly — egui Checkbox marks the
	// response changed on toggle (unlike RadioButton; see
	// [[feedback-radio-haspricked]]).
	genChanged := false
	testsChanged := false
	// HorizontalTop (Align::Min), not Horizontal (Align::Center): the combos
	// and checkboxes are all interact_size.y tall, but egui's centered
	// horizontal layout anchors the *first* item in the row a few pixels
	// higher than the rest, leaving the control row with a ragged baseline.
	// Top-aligning equal-height items sidesteps that per-item centering and
	// lands every control on one stable line.
	for range c.HorizontalTop().KeepIter() {
		inst.sizeMetricIdx = renderMetricCombo(inst.ids, "size-metric", "Size", inst.sizeMetricIdx)
		c.AddSpace(styletokens.GapItems(inst.density))
		inst.colorMetricIdx = renderMetricCombo(inst.ids, "color-metric", "Color", inst.colorMetricIdx)
		c.AddSpace(styletokens.GapSections(inst.density))
		if c.Checkbox(inst.ids.PrepareStr("include-gen"), inst.includeGenerated, "Include generated").
			SendRespVal(&inst.includeGenerated).HasChanged() {
			genChanged = true
		}
		c.AddSpace(styletokens.GapItems(inst.density))
		if c.Checkbox(inst.ids.PrepareStr("include-tests"), inst.includeTests, "Include tests").
			SendRespVal(&inst.includeTests).HasChanged() {
			testsChanged = true
		}
		c.AddSpace(styletokens.GapSections(inst.density))
		// Show values flips the in-cell label live (the closure reads
		// inst.showValues), so unlike the metric/filter controls it needs no
		// rebuild — toggling it preserves the user's drill position.
		c.Checkbox(inst.ids.PrepareStr("show-values"), inst.showValues, "Show values").
			SendRespVal(&inst.showValues)
	}
	if inst.sizeMetricIdx != prevSize || inst.colorMetricIdx != prevColor || genChanged || testsChanged {
		inst.rebuildTreemap()
	}

	inst.renderDistSummaries()
	inst.renderTotals()

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

// startScan launches the background scc scan for the current path. The path is
// snapshotted here on the render thread; the worker only shells out and builds
// pure data, so it never touches imzero2 state.
func (inst *App) startScan() {
	path := inst.repoPath
	title := "scc scan: " + path
	if path == "" {
		title = "scc scan: current repository"
	}
	inst.job.StartReporting(inst.tasks, bgjob.Spec{
		Kind:  "sccmap.scan",
		Title: title,
	}, func(ctx context.Context, report bgjob.Reporter) (*sccData, error) {
		return scanScc(ctx, path, report)
	})
}

// drainScan consumes a completed scan exactly once on the render thread: it
// swaps in the new dataset, upgrades an empty/"." path box to the resolved
// root so the box shows a concrete target, and rebuilds the treemap + digests
// against the new groups.
func (inst *App) drainScan() {
	res, _, ok := inst.job.TakeResult()
	if !ok {
		return
	}
	inst.data = res
	if (inst.repoPath == "" || inst.repoPath == ".") && res.resolvedRoot != "" {
		inst.repoPath = res.resolvedRoot
	}
	inst.rebuildTreemap()
}

// renderHeader draws the "Repository:" path box and the scan/rescan control
// inline (no panel), so the same Frame body renders both in the windowed host
// and in the demo tour's host Ui scope.
func (inst *App) renderHeader() {
	for range c.Horizontal().KeepIter() {
		c.Label("Repository:").Send()
		c.TextEdit(inst.ids.PrepareStr("repo-path"), inst.repoPath, false).
			DesiredWidth(repoPathWidth).SendRespVal(&inst.repoPath)
		inst.renderScanOrProgress()
	}
}

// renderScanOrProgress renders the jobprogress bar + Cancel while a scan is in
// flight (with a render-thread heartbeat repaint), or the Scan button and the
// last error otherwise. The worker never calls c.* itself.
func (inst *App) renderScanOrProgress() {
	snap := inst.job.Snapshot()
	if snap.State == bgjob.StateRunning {
		// jobprogress stacks title / bar / status+Cancel vertically; give it a
		// bounded block of its own — inline in the header row the bar would
		// fill the remaining width and push the Cancel button off-screen.
		for range c.Vertical().KeepIter() {
			c.UiSetMaxWidth(scanProgressWidth)
			if jobprogress.Render(jobprogress.Input{
				Title:    "Scanning repository",
				Fraction: snap.Fraction,
				EtaMs:    snap.EtaMs,
				Note:     snap.Note,
				CancelId: inst.ids.PrepareStr("scan-cancel"),
			}) {
				inst.job.Cancel()
			}
		}
		c.RequestRepaintAfter(0.05)
		return
	}
	if c.Button(inst.ids.PrepareStr("scan-btn"), c.Atoms().Text("Scan").Keep()).
		SendResp().HasPrimaryClicked() {
		inst.startScan()
	}
	if snap.State == bgjob.StateFailed && snap.Err != nil {
		c.Label("⚠ scan failed: " + snap.Err.Error()).Send()
	}
}

// renderNoData fills the body before the first scan completes (or after a
// failure), reading the job snapshot for the reason.
func (inst *App) renderNoData() {
	snap := inst.job.Snapshot()
	switch snap.State {
	case bgjob.StateRunning:
		c.Label("Scanning repository…").Send()
	case bgjob.StateFailed:
		c.Label("The scan failed. Check the repository path and press Scan.").Send()
		if snap.Err != nil {
			c.Label(snap.Err.Error()).Send()
		}
	default:
		c.Label("No scan yet. Set a repository path and press Scan.").Send()
	}
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

// gutterLabel draws text left-aligned in a fixed distGutterW-wide cell so the
// distsummary rendered after it starts at the same x regardless of how wide the
// metric name makes the label. The min-width must be set on this nested scope,
// not on the enclosing Horizontal row: UiSetMinWidth applies to the current Ui,
// so calling it on the row only widens the whole row and lets the bare label —
// and the summary after it — still shift as the metric name changes. min (not
// min+max) so an over-long label grows the cell rather than truncating; the
// distGutterW const is sized for the widest gutter form, so in practice every
// label fits and the cell stays exactly distGutterW wide.
func (inst *App) gutterLabel(text string) {
	for range c.Vertical().KeepIter() {
		c.UiSetMinWidth(distGutterW)
		c.Label(text).Send()
	}
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
		if aliased {
			inst.gutterLabel("Size & color (" + sizeName + "):")
		} else {
			inst.gutterLabel("Size (" + sizeName + "):")
		}
		c.AddSpace(styletokens.GapItems(inst.density))
		inst.distRenderer.Render(inst.ids.PrepareStr("size-dist"), inst.sizeDigest, nil)
		if !aliased {
			c.AddSpace(styletokens.GapSections(inst.density))
			inst.gutterLabel("Color (" + colorName + "):")
			c.AddSpace(styletokens.GapItems(inst.density))
			inst.distRenderer.Render(inst.ids.PrepareStr("color-dist"), inst.colorDigest, nil)
		}
	}
}

// renderTotals paints a single compact line below the distribution row
// giving the aggregate sum ("Σ") of each axis's metric over the same kept
// leaf set the distributions survey — total code lines, total complexity,
// etc. — so the treemap's relative proportions have an absolute scale to
// read against. Each value is rendered with its own metric humanizer (counts
// as "1.2M", bytes as "1.2 MB"), matching the in-cell tile labels rather
// than the distsummary's SI form. When the size and color metrics coincide
// the two totals are identical, so the line collapses to the single metric —
// mirroring the aliasing in renderDistSummaries.
func (inst *App) renderTotals() {
	if inst.sizeDigest == nil {
		return
	}
	sizeM := sccMetrics[inst.sizeMetricIdx]
	colorM := sccMetrics[inst.colorMetricIdx]
	aliased := inst.sizeDigest == inst.colorDigest
	var b strings.Builder
	b.WriteString("Σ  ")
	b.WriteString(sizeM.Name)
	b.WriteString(" ")
	b.WriteString(sizeM.Humanize(inst.sizeTotal))
	if !aliased {
		b.WriteString("   ·   ")
		b.WriteString(colorM.Name)
		b.WriteString(" ")
		b.WriteString(colorM.Humanize(inst.colorTotal))
	}
	c.LabelAtoms(c.Atoms().BeginRichText(b.String()).Monospace().End().Keep()).Send()
}
