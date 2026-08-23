package play

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// play_treemap_panel.go is the Treemap dock tab (ADR-0166): the active result
// as nested rectangles whose AREA is the value. It reads the same two column
// contracts the Icicle tab does — play_hierarchy.go owns them — so a query that
// draws as a flamegraph draws here unchanged, and the choice between the two is
// about the question, not about the SQL.
//
// The two forms answer different questions and this one is for "what is big".
// A treemap discards the ordering and the depth of a path, which is why the
// pprof ladder chose the icicle for stack profiles; what it gives back is the
// whole pane spent on magnitude, and a second data channel — `color` — that the
// icicle has no room for.
//
// NAVIGATION AND SELECTION ARE LOCAL. Clicking a container drills into it and
// clicking the breadcrumb drills back out; neither is published, because a
// drill position is a place in a view rather than a fact about the result.
// Clicking a leaf pins it and publishes its label as `selection_key`, matching
// Network, Sankey and Icicle: in folded mode a cell is a path PREFIX, so it
// spans many rows and no single row cursor would be honest about it.

// treemapForm is this pane's vocabulary in a shared reject message.
var treemapForm = hierForm{noun: "treemap", elem: "cell"}

// treemapIDSalt namespaces the panel's pane probe — distinct from the other
// panels' salts; the per-instance idSeed (nextVizSeed) keeps two live PlayApps
// apart.
const treemapIDSalt uint64 = 0x5a11c0de17f10007

// treemapPaneFill is the picture's box — the shared pane rule
// (play_pane_box.go). The floor is where the cells stop carrying their labels;
// a leaf under it scrolls rather than drawing a mosaic.
//
// It sits below the Icicle's, though the two draw the same tree, because this
// pane spends more of its leaf on chrome than any of its neighbours: a
// colourscale legend above the picture and the widget's own breadcrumb bar
// inside it (treemapWidgetChromePx). At the 300 it inherited from the fixed
// aspect, floor plus chrome came to more than a default body leaf has, so the
// pane scrolled a picture that had just been sized to fit it.
var treemapPaneFill = paneFill{
	slack: 12, minW: 360, maxW: 1600, minH: 260,
	fallbackW: 760, fallbackH: 460,
	chrome: treemapWidgetChromePx,
}

// treemapWidgetChromePx is the room the widget takes for itself around the
// container it is handed: Render draws the breadcrumb bar, then the box of
// exactly SetContainerSize. A caller filling the pane has to leave the bar its
// height or the picture ends that far past the leaf — the widget documents this
// as the caller's "chrome budget" (treemap.containerRect), and its neighbours
// carry one too, imztop's deeper because it keeps the summary line this pane
// turns off (ensureWidget).
//
// Measured off a tour capture at 43pt (a text row inside a bordered chip, plus
// the spacing either side) and rounded up, because the two failures are not
// symmetric: over-reserving leaves a few points of background nobody sees,
// where under-reserving clips a row of cells and holds a scrollbar open.
const treemapWidgetChromePx float32 = 48

const (
	// treemapDepthStops is the length of the depth ramp. Eight matches the
	// widget's own named palettes, which is what a caller-supplied palette is
	// compared against.
	treemapDepthStops = 8
	// treemapValueStops samples the sequential palette for the numeric colour
	// arm. Denser than the depth ramp because it encodes a continuum rather
	// than a handful of levels.
	treemapValueStops = 64
	// treemapRootName labels the synthetic container a FOREST is wrapped in.
	// Only used when the result has more than one root — a single-rooted tree
	// is handed to the widget as-is, so its own root names the breadcrumb.
	treemapRootName = "all"
	// treemapMaxCatRunes bounds a category key in the status line.
	treemapMaxCatRunes = 24
	// treemapLegendW/H size the numeric colour bar. The height is not free:
	// the widget spends 55% of it on the gradient, 5px on tick marks and the
	// rest on labels, so at the 34 first tried the 10px labels ran past the
	// bottom and were clipped mid-glyph. 48 leaves them a full line. The width
	// is what keeps the SI-suffixed labels from colliding at the right end.
	treemapLegendW = 360
	treemapLegendH = 48
	// treemapLegendTicks is below the widget's default of 6: these labels are
	// SI-suffixed and so wider than the bare numbers that default assumes.
	treemapLegendTicks = 5
	// treemapSwatchPx is one categorical chip's side.
	treemapSwatchPx = 10
	// treemapLegendMaxCats caps the category key. A legend is not a table, and
	// the status line carries the full count.
	treemapLegendMaxCats = 12
)

// treemapLegendSalt namespaces the category swatches' frame ids.
const treemapLegendSalt uint64 = 0x5a11c0de17f1000e

// treemapColorModeE is what a cell's fill encodes.
type treemapColorModeE uint8

const (
	// treemapColorData — the `color` column, as a colormap or a category cycle.
	// Falls through to the depth ramp for any node the column did not describe,
	// which in folded mode is every synthesised interior node.
	treemapColorData treemapColorModeE = iota
	// treemapColorDepth — the depth ramp alone: structure rather than identity.
	treemapColorDepth
)

// treemapNestingE is how much of the tree renders below the frontier.
type treemapNestingE uint8

const (
	// treemapNestDrill — the frontier's children plus one preview level. The
	// default, and what bounds the frame cost: cells are egui Frames, so the
	// budget is the frontier's FANOUT rather than the tree's size.
	treemapNestDrill treemapNestingE = iota
	// treemapNestAll — the whole subtree at once, bounded only by the minimum
	// cell size. Readable for a shallow tree and expensive for a wide one.
	treemapNestAll
)

func (n treemapNestingE) depth() int {
	if n == treemapNestAll {
		return 0 // the widget's "unlimited", capped internally
	}
	return 1
}

// treemapColorInfo is what the colour channel resolved to for one built tree:
// the numeric range a colormap spans, or the category keys a cycle was assigned
// in first-seen order.
type treemapColorInfo struct {
	kind hierColorKindE
	// min/max bound the numeric arm. Equal bounds are widened by
	// treemapColorRange so a single-valued column does not divide by zero.
	min, max float64
	// declared reports that min/max came from the query's `color_min` /
	// `color_max` rather than from surveying the result. Kept because the two
	// are different claims and the status line says which one is on: a ramp
	// pinned to 0–100 and one stretched over 12–68 look identical.
	declared bool
	// unit labels the colour channel on the legend ticks and in the readout.
	// The value's `unit` cannot serve — area and tint are different measures,
	// and here one is statements and the other a percentage.
	unit string
	// cats maps a category key to its cycle index, and catOrder keeps the
	// first-seen order the indices were handed out in — first-seen rather than
	// sorted so adding a row cannot recolour the rows above it.
	cats     map[string]int
	catOrder []string
	// wrapped counts categories past the palette's length, which share a hue
	// with an earlier one. Counted rather than prevented: seven is the honest
	// size of a CVD-safe qualitative set (ADR-0156), and a picture that is
	// lying about a category should say how many.
	wrapped int
}

// treemapDriver owns the Treemap tab state: the built tree, its pointer-tree
// projection, the widget, and the locally-pinned leaf.
type treemapDriver struct {
	ids    *c.WidgetIdStack
	idSeed uint64

	tm *treemap.Treemap

	// paneW / paneH are the last box the pane probe reported. Held across
	// frames rather than read fresh: the probe answers nothing on the first
	// frame and again on the frame a hidden tab comes back (a seq that did not
	// capture is absent from the drain), and resizing the picture to a fallback
	// on those frames would flash.
	paneW, paneH float32

	colorMode treemapColorModeE
	nesting   treemapNestingE

	// Tree cache: the result identity (the executed timestamp, the same
	// freshness token the pager, World and Kanban use) plus the schema the claim
	// was resolved from. Interning a big result is per-node work and has no
	// business running every frame.
	tree        hierTree
	stats       hierStats
	forExecuted time.Time
	forSchema   *arrow.Schema
	treeGen     uint64

	// root is the pointer tree the widget navigates; idxOf maps a node of it
	// back to its hierTree index, which is how the colorings reach the colour
	// channel from a *layout.Node they are handed.
	root  *layout.Node
	idxOf map[*layout.Node]int32
	color treemapColorInfo

	// cmap is the colormap the numeric arm colours cells from, held so the
	// legend can render the SAME instance rather than a lookalike built from
	// the same numbers — which is what treemap.ContinuousColoringFromMap exists
	// for. scale is the legend over it, rebuilt with the colormap because a
	// ColorScale binds its Config at construction.
	cmap  *treemap.Colormap
	scale *colorscale.ColorScale

	// effColorNum / effColorKey are the EFFECTIVE colour per flat index: the
	// node's own where the result gave it one, otherwise what it inherited from
	// its descendants (inheritColors). inst.tree keeps what the result actually
	// said, so the readout can tell the two apart.
	effColorNum []float64
	effColorKey []string
	// inherited counts nodes coloured from below; mixed counts containers left
	// to the depth ramp because their described descendants disagreed. Both are
	// in the status line, which is what makes the inheritance rule
	// self-diagnosing: a picture that is still grey says how much of it is
	// genuinely heterogeneous.
	inherited int
	mixed     int

	// selected is the click-pinned LEAF label, published as selection_key. The
	// drill position is the widget's and is deliberately not mirrored here.
	selected string

	// pendingExecuted is stashed by renderTreemapTab before dispatch — the
	// PanelI Render signature carries no result metadata (the World pane's
	// noteExecuted handoff).
	pendingExecuted time.Time
}

// newTreemapDriver builds the driver. It takes no client: like Table, World and
// Icicle the panel reads the ACTIVE RESULT, so it has no lane of its own.
func newTreemapDriver(ids *c.WidgetIdStack) (inst *treemapDriver) {
	return &treemapDriver{ids: ids, idSeed: nextVizSeed()}
}

// noteExecuted hands the driver the active result's freshness token before
// dispatch; the tree cache keys on it.
func (inst *treemapDriver) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// treemapPanel is the PanelI face. Acceptance is schema-only and cheap — it runs
// every frame — because both contracts are questions about column names and one
// column's type, which the schema answers on its own.
type treemapPanel struct {
	driver *treemapDriver
}

func (inst treemapPanel) ID() PanelID { return "treemap" }

func (inst treemapPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "cells"}}
}

func (inst treemapPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query with a `stack` array and a `value` (or `id`/`parent`/`value`) to see a treemap."
		return
	}
	cl, r := resolveHierarchy(schema, treemapForm)
	if r != "" {
		reason = r
		return
	}
	claim = cl
	return
}

func (inst treemapPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main, ok := filled[chMain]
	if !ok {
		return
	}
	cl, isClaim := main.Claim.(hierClaim)
	if !isClaim {
		return
	}
	inst.driver.render(main.Rec, main.Rec.Schema(), cl, emit)
}

// render builds the tree (cached on the result identity), projects it onto the
// widget's pointer tree, draws it, and tracks the pinned leaf.
func (inst *treemapDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, cl hierClaim, emit SignalEmitterI) {
	if schema != inst.forSchema || !inst.pendingExecuted.Equal(inst.forExecuted) || inst.treeGen == 0 {
		inst.tree, inst.stats = buildHierarchy(rec, cl)
		inst.forSchema, inst.forExecuted = schema, inst.pendingExecuted
		inst.treeGen++
		inst.rebuildTree()
		// A new tree invalidates a pin taken against the old one.
		inst.selected = ""
	}
	inst.renderControls()

	if inst.tree.Len() == 0 || inst.root == nil {
		c.Label(inst.statusLine()).Send()
		for rt := range c.RichTextLabel("No cells: every row was missing a path and an id, or carried a value " +
			"that is not a finite, non-negative number.") {
			rt.Small().Weak()
		}
		return
	}

	// The readouts go ABOVE the plot, as the Icicle tab's do and for the same
	// reason: a hover is a register read and so is a frame behind anyway, and
	// this keeps the total and the warnings where a pane too short for the
	// picture cannot push them out of sight.
	c.Label(inst.pointerLine()).Send()
	inst.renderLegend()
	// The hint goes above the picture too, and here it is what lets the picture
	// be the LAST widget in the body: the probe below reports the room left for
	// the next widget, so with the hint underneath, taking that room would push
	// it past the fold and hold a scrollbar open — which narrows the pane, which
	// resizes the picture.
	for rt := range c.RichTextLabel("hover a cell for its share; click a container to drill in, the breadcrumb " +
		"to drill back out, a leaf to pin it") {
		rt.Small().Weak()
	}
	c.Separator().Horizontal().Send()

	// Fill the pane. The height used to be a fixed 0.56 of the width — a
	// squarer aspect than the Icicle's, since that form spends its height on
	// depth rows where this one spends both dimensions on area. The pane's own
	// height says it better: a letterbox is what drives the squarify aspect
	// ratios toward the slivers the algorithm exists to avoid, and the leaf is
	// what decides how much of one this pane is.
	if availW, availH, ok := c.CapturePaneSize(treemapIDSalt ^ inst.idSeed ^ 0x1); ok {
		inst.paneW, inst.paneH = availW, availH
	}
	w, h := treemapPaneFill.box(inst.paneW, inst.paneH)

	inst.ensureWidget()
	inst.tm.SetContainerSize(w, h)
	inst.tm.SetMaxNestingDepth(inst.nesting.depth())
	inst.tm.Render()

	// A clicked leaf is the pin; clicking the pinned one again clears it, so
	// the gesture is its own undo. Container clicks are the widget's drill and
	// never reach here. An empty key is the honest "nothing focused" value a
	// query reading {selection_key:String} sees before anything is clicked.
	if leaf := inst.tm.ClickedLeaf(); leaf != nil {
		if leaf.Name == inst.selected {
			inst.selected = ""
		} else {
			inst.selected = leaf.Name
		}
		if emit != nil {
			emit.Emit(signalSelectionKey, inst.selected)
		}
	}
}

// ensureWidget constructs the widget on first use. It is built here rather than
// in the constructor because the coloring it takes depends on the first tree —
// the numeric range and the category assignment both do.
func (inst *treemapDriver) ensureWidget() {
	if inst.tm != nil {
		return
	}
	inst.tm = treemap.New(inst.ids, "play-treemap", inst.root,
		treemap.WithColoring(inst.coloring()),
		treemap.WithLeafClickSensing(true),
		treemap.WithCellLabel(inst.cellLabel),
		treemap.WithSelfCellLabel(inst.selfCellLabel),
		// This pane draws its own readout above the picture — pointerLine, which
		// reads a cell in the result's OWN unit and against the whole. The
		// widget's line under the container would say the same thing again in
		// bytes, and would be a row of the pane's height to reserve for it.
		treemap.WithStatusLine(false),
	)
}

// rebuildTree projects the flat tree onto the pointer tree the widget takes,
// resolves the colour channel over it, and hands both to the widget.
//
// A FOREST is wrapped in a synthetic container. That container carries no size
// of its own, so it invents no value — it exists because the widget navigates
// from a single root, and a result with several roots is an ordinary shape
// (§SD1's forest). A single-rooted tree is passed through unwrapped, so the
// breadcrumb names the result's own root rather than a placeholder.
func (inst *treemapDriver) rebuildTree() {
	n := inst.tree.Len()
	inst.idxOf = make(map[*layout.Node]int32, n)
	if n == 0 {
		inst.root, inst.color = nil, treemapColorInfo{}
		return
	}

	nodes := make([]layout.Node, n)
	var roots []*layout.Node
	for i := range n {
		nodes[i].Name = inst.tree.Labels[i]
		nodes[i].Size = inst.tree.Self[i]
		inst.idxOf[&nodes[i]] = int32(i)
	}
	for i := range n {
		p := inst.tree.Parents[i]
		if p < 0 || int(p) >= n || int(p) == i {
			roots = append(roots, &nodes[i])
			continue
		}
		nodes[p].Children = append(nodes[p].Children, &nodes[i])
	}

	switch len(roots) {
	case 0:
		// Every node claims a parent, so the parents form a cycle. The builders
		// cannot produce this from either contract — a folded trie is acyclic by
		// construction and node mode demotes an unresolvable parent to a root —
		// but drawing nothing beats recursing forever if one ever does.
		inst.root = nil
	case 1:
		inst.root = roots[0]
	default:
		inst.root = &layout.Node{Name: treemapRootName, Children: roots}
	}
	inst.inheritColors()
	inst.color = inst.resolveColorInfo()
	inst.rebuildColormap()
	if inst.tm != nil && inst.root != nil {
		inst.tm.SetRoot(inst.root)
		inst.tm.SetColoring(inst.coloring())
	}
}

// inheritColors fills the effective colour of every node the `color` column did
// not describe, from what its descendants say (ADR-0166 §SD2).
//
// It exists because without it the default view is nearly colourless: colour
// attaches to the node a row's VALUE lands on, which in folded mode is a leaf,
// while the drill nesting shows containers. The encoding was only visible under
// `full`, which is not where a reader starts.
//
// A node's OWN colour always wins. Inheritance fills silence; it never
// overwrites something the query said.
//
// The two arms aggregate differently because the data types do:
//
//   - NUMERIC — the value-weighted mean of the children's effective colours,
//     weighted by the total each child occupies, since area is what the reader
//     is comparing. An unweighted mean would let a sliver outvote the subtree
//     next to it. Children with no effective colour are excluded from both
//     sums rather than counted as zero.
//   - CATEGORICAL — inherited only when the described descendants AGREE.
//     A mean of nominal categories does not exist, and the modal one would
//     claim a category for a container that has several. Leaving a mixed
//     container to the depth ramp makes neutral mean "look inside", which is a
//     reading; "mostly fs" is a claim the query never made.
//
// Post-order over the pointer tree, so a node is resolved after its children.
// Written into the effective slices rather than into inst.tree, which stays the
// record of what the result actually said — the pointer readout distinguishes
// the two.
func (inst *treemapDriver) inheritColors() {
	inst.effColorNum, inst.effColorKey = nil, nil
	inst.inherited, inst.mixed = 0, 0
	n := inst.tree.Len()
	if n == 0 || inst.root == nil {
		return
	}
	switch inst.tree.ColorKind {
	case hierColorNumeric:
		inst.effColorNum = make([]float64, n)
		copy(inst.effColorNum, inst.tree.ColorNum)
	case hierColorCategorical:
		inst.effColorKey = make([]string, n)
		copy(inst.effColorKey, inst.tree.ColorKey)
	default:
		return
	}

	// resolve returns the node's effective colour and whether it has one.
	var resolve func(*layout.Node) (num float64, key string, ok bool)
	resolve = func(nd *layout.Node) (num float64, key string, ok bool) {
		i, known := inst.idxOf[nd]
		// An unknown node is the synthetic forest container: it has no row and
		// no slot, so it can still aggregate but has nothing of its own.
		if known {
			switch inst.tree.ColorKind {
			case hierColorNumeric:
				if v := inst.effColorNum[i]; !math.IsNaN(v) {
					for _, ch := range nd.Children {
						resolve(ch) // still resolve the subtree below it
					}
					return v, "", true
				}
			case hierColorCategorical:
				if k := inst.effColorKey[i]; k != "" {
					for _, ch := range nd.Children {
						resolve(ch)
					}
					return 0, k, true
				}
			}
		}
		if len(nd.Children) == 0 {
			return 0, "", false
		}

		var wsum, vsum float64
		var only string
		agree, any := true, false
		for _, ch := range nd.Children {
			cn, ck, cok := resolve(ch)
			if !cok {
				continue
			}
			any = true
			switch inst.tree.ColorKind {
			case hierColorNumeric:
				// Weight by what the child occupies, which is what its area is.
				w := ch.TotalSize()
				if w <= 0 || math.IsNaN(w) || math.IsInf(w, 0) {
					continue
				}
				wsum += w
				vsum += w * cn
			case hierColorCategorical:
				if only == "" {
					only = ck
					continue
				}
				if only != ck {
					agree = false
				}
			}
		}
		if !any {
			return 0, "", false
		}
		switch inst.tree.ColorKind {
		case hierColorNumeric:
			if wsum <= 0 {
				return 0, "", false
			}
			num, ok = vsum/wsum, true
		case hierColorCategorical:
			if !agree || only == "" {
				if known {
					inst.mixed++
				}
				return 0, "", false
			}
			key, ok = only, true
		}
		if known && ok {
			inst.inherited++
			if inst.tree.ColorKind == hierColorNumeric {
				inst.effColorNum[i] = num
			} else {
				inst.effColorKey[i] = key
			}
		}
		return
	}
	resolve(inst.root)
}

// ownColorAt reports whether the RESULT described this node's colour, as
// opposed to it having been inherited from below.
func (inst *treemapDriver) ownColorAt(i int32) bool {
	switch inst.tree.ColorKind {
	case hierColorNumeric:
		return int(i) < len(inst.tree.ColorNum) && !math.IsNaN(inst.tree.ColorNum[i])
	case hierColorCategorical:
		return int(i) < len(inst.tree.ColorKey) && inst.tree.ColorKey[i] != ""
	}
	return false
}

// rebuildColormap rebuilds the numeric colormap and the legend bound to it.
//
// One Colormap instance serves both the cells and the legend, which is the
// point of treemap.ContinuousColoringFromMap: a legend built from the same two
// numbers rather than the same object would drift the moment either side
// changed how it samples the palette, and a legend that disagrees with the
// picture is worse than none.
//
// The ColorScale binds its Config at construction and has no setter, so it is
// dropped here and rebuilt on the next render. That also drops its cached tick
// axis, which is correct — the range it was computed for is gone.
//
// Only the colormap is built here. The widget is not: this runs from the tree
// rebuild, which is pure data and reachable without a UI (its tests build a
// driver with no id stack at all), while colorscale.New requires one.
func (inst *treemapDriver) rebuildColormap() {
	inst.cmap, inst.scale = nil, nil
	if inst.color.kind != hierColorNumeric {
		return
	}
	inst.cmap = treemap.NewColormap(treemapValuePalette(), inst.color.min, inst.color.max)
}

// ensureScale constructs the legend on first render after a colormap change,
// mirroring ensureWidget.
func (inst *treemapDriver) ensureScale() {
	if inst.scale != nil || inst.cmap == nil {
		return
	}
	inst.scale = colorscale.New(inst.ids, "play-treemap-legend", inst.cmap.Config(),
		colorscale.WithSize(treemapLegendW, treemapLegendH),
		colorscale.WithDesiredTicks(treemapLegendTicks),
		// `unit` labels the VALUE, so it cannot serve here; the colour channel
		// has its own, `color_unit`, and reads as a bare SI-suffixed number
		// without one. Read through inst rather than captured, so a re-resolve
		// that keeps the same colormap cannot leave the ticks labelled for the
		// previous result.
		colorscale.WithLabelFormat(func(v float64) string { return treemapQty(v, inst.color.unit) }),
	)
}

// renderLegend says what a fill means, for the mode that is actually on.
//
// Only for the data mode: the depth ramp encodes structure rather than
// identity, so a key mapping its colours to depth numbers would be chrome
// explaining an axis nobody reads off. And only when a `color` column resolved
// — with none, the control row already says so in words.
//
// It sits ABOVE the canvas with the other readouts, for the reason ADR-0160
// §SD9 gives: a pane too short for the picture must not push the thing that
// explains it out of sight.
func (inst *treemapDriver) renderLegend() {
	if inst.colorMode != treemapColorData {
		return
	}
	switch inst.color.kind {
	case hierColorNumeric:
		inst.ensureScale()
		if inst.scale == nil {
			return
		}
		inst.scale.Render()
	case hierColorCategorical:
		inst.renderCategoryKey()
	}
}

// renderCategoryKey draws one swatch per category, in the first-seen order the
// cycle was assigned in, so the key reads in the same order the picture
// allocated its hues.
//
// Past the palette's length the swatches REPEAT, which is honest — that is what
// the cells do — and the trailing note is what stops two identical chips
// reading as a rendering fault. The list is capped because a key is a legend,
// not a table; the status line carries the full count either way.
func (inst *treemapDriver) renderCategoryKey() {
	if len(inst.color.catOrder) == 0 {
		return
	}
	gap := styletokens.GapItems(styletokens.ActiveDensity())
	for range c.HorizontalTop().KeepIter() {
		for i, key := range inst.color.catOrder {
			if i >= treemapLegendMaxCats {
				for rt := range c.RichTextLabel(fmt.Sprintf("+%d more",
					len(inst.color.catOrder)-treemapLegendMaxCats)) {
					rt.Small().Weak()
				}
				break
			}
			// A square chip: there is no radius token, and a literal here would
			// be one more number to keep in step with the cells it stands for.
			swatch := c.Frame(inst.ids.PrepareSeq(treemapLegendSalt ^ uint64(i))).
				Fill(color.Hex(styletokens.QualitativeCycle(i).AsHex()))
			for range swatch.KeepIter() {
				c.UiSetMinWidth(treemapSwatchPx)
				c.UiSetMinHeight(treemapSwatchPx)
			}
			for rt := range c.RichTextLabel(truncateRunes(key, treemapMaxCatRunes)) {
				rt.Small()
			}
			c.AddSpace(gap)
		}
	}
}

// resolveColorInfo surveys the colour channel of the built tree: the range a
// colormap spans, or the categories a cycle is assigned to.
//
// It reads what the RESULT said (inst.tree) rather than the effective colours,
// and that is sufficient rather than an oversight: an inherited numeric colour
// is a weighted mean of values already in the range, and an inherited category
// is a key that already exists. Neither can widen what this finds, so surveying
// the smaller set gives the same answer.
//
// A DECLARED scale short-circuits the survey entirely. The query has said what
// the measure's endpoints are, and a value outside them clamps to the palette
// end rather than stretching the ramp (colormap.Config.At) — which is what lets
// two runs of the same query be compared by colour at all.
func (inst *treemapDriver) resolveColorInfo() (info treemapColorInfo) {
	info.kind = inst.tree.ColorKind
	switch info.kind {
	case hierColorNumeric:
		info.unit = inst.stats.colorScale.unit
		if sc := inst.stats.colorScale; sc.declared {
			info.min, info.max, info.declared = sc.min, sc.max, true
			return
		}
		info.min, info.max = math.Inf(1), math.Inf(-1)
		for _, v := range inst.tree.ColorNum {
			if math.IsNaN(v) {
				continue
			}
			info.min, info.max = math.Min(info.min, v), math.Max(info.max, v)
		}
		info.min, info.max = treemapColorRange(info.min, info.max)
	case hierColorCategorical:
		info.cats = make(map[string]int, 8)
		for _, k := range inst.tree.ColorKey {
			if k == "" {
				continue
			}
			if _, seen := info.cats[k]; seen {
				continue
			}
			idx := len(info.catOrder)
			info.cats[k] = idx
			info.catOrder = append(info.catOrder, k)
			if idx >= styletokens.QualitativeCycleLen {
				info.wrapped++
			}
		}
	}
	return
}

// treemapColorRange widens a degenerate numeric range. A column with one
// distinct value, or none at all, would otherwise normalise to a division by
// zero; widening puts every cell at the same point of the ramp instead, which
// is the truthful picture of a constant column.
func treemapColorRange(min, max float64) (lo, hi float64) {
	if math.IsInf(min, 0) || math.IsInf(max, 0) {
		return 0, 1
	}
	if max <= min {
		return min, min + 1
	}
	return min, max
}

// coloring composes the depth ramp with the data layer. Depth is FIRST because
// it always has an opinion and CompositeColoring is last-ok-wins, so the data
// layer overrides it wherever the column described a node — and where it did
// not (a synthesised interior node, an unreadable cell), the ramp shows through
// rather than leaving a hole. The imztop Proc Map idiom.
//
// The data layer is wrapped in a ConditionalColoring reading the driver's mode,
// so the colour switch is a draw-time decision that neither re-lays-out nor
// rebuilds the widget.
func (inst *treemapDriver) coloring() treemap.ColoringI {
	depth := treemap.DepthColoring(treemapDepthPalette())
	data := inst.dataColoring()
	if data == nil {
		return depth
	}
	return treemap.CompositeColoring(depth, treemap.ConditionalColoring(
		func(treemap.CellInfo) bool { return inst.colorMode == treemapColorData },
		data,
	))
}

// dataColoring is the `color` column's layer, or nil when there is no usable
// column — in which case the depth ramp is the whole coloring and the mode
// switch has nothing to switch to.
func (inst *treemapDriver) dataColoring() treemap.ColoringI {
	switch inst.color.kind {
	case hierColorNumeric:
		if inst.cmap == nil {
			return nil
		}
		// The SAME Colormap the legend renders, not a second one built from the
		// same numbers (rebuildColormap).
		return treemap.ContinuousColoringFromMap(inst.cmap, func(n *layout.Node) float64 {
			i, ok := inst.idxOf[n]
			if !ok || int(i) >= len(inst.effColorNum) {
				return math.NaN() // no opinion; the depth ramp keeps the cell
			}
			return inst.effColorNum[i]
		})
	case hierColorCategorical:
		return treemap.CategoricalColoring(treemapCategoryPalette(), func(n *layout.Node) int {
			i, ok := inst.idxOf[n]
			if !ok || int(i) >= len(inst.effColorKey) {
				return -1 // no opinion
			}
			idx, seen := inst.color.cats[inst.effColorKey[i]]
			if !seen {
				return -1
			}
			return idx
		})
	}
	return nil
}

// treemapDepthPalette is a neutral ramp, dark at the root and lighter inward.
// Neutral rather than a hue ramp because depth is the BASE layer: when a colour
// column is driving, the levels it did not describe must read as structure
// without competing with the data channel. The topology panel's palette, for
// the same reason.
func treemapDepthPalette() (p []uint32) {
	const tDark, tLight = 0.85, 0.32
	p = make([]uint32, treemapDepthStops)
	for i := range p {
		t := tDark + (tLight-tDark)*float64(i)/float64(treemapDepthStops-1)
		p[i] = rgba8ToHex(styletokens.Sequential(styletokens.SequentialGrayC, float32(t)))
	}
	return
}

// treemapValuePalette samples the user's sequential default for the numeric
// colour arm — the same palette every other ordered-data encoding in the tree
// reads (IDS_PALETTE_SEQUENTIAL).
func treemapValuePalette() (p []uint32) {
	s := styletokens.SequentialDefault()
	p = make([]uint32, treemapValueStops)
	for i := range p {
		p[i] = rgba8ToHex(styletokens.Sequential(s, float32(i)/float32(treemapValueStops-1)))
	}
	return
}

// treemapCategoryPalette is the IDS qualitative cycle (ADR-0156, Okabe-Ito).
// Seven entries is the honest count; a result with more categories wraps, and
// the status line says how many did.
func treemapCategoryPalette() (p []uint32) {
	p = make([]uint32, styletokens.QualitativeCycleLen)
	for i := range p {
		p[i] = rgba8ToHex(styletokens.QualitativeCycle(i))
	}
	return
}

// rgba8ToHex packs an IDS colour as the 0xRRGGBBAA the treemap palettes take.
func rgba8ToHex(c styletokens.RGBA8) uint32 {
	return uint32(c.R)<<24 | uint32(c.G)<<16 | uint32(c.B)<<8 | uint32(c.A)
}

// cellLabel is a cell's secondary line: the subtree TOTAL, which is what its
// area encodes.
func (inst *treemapDriver) cellLabel(n *layout.Node) string {
	return treemapQty(n.TotalSize(), inst.stats.unit)
}

// selfCellLabel is the secondary line of a container's own cell, which encodes
// its OWN value rather than its subtree's (ADR-0166 §SD3).
func (inst *treemapDriver) selfCellLabel(n *layout.Node) string {
	return treemapQty(n.Size, inst.stats.unit)
}

// pointerLine describes what the pointer is over, falling back to the pinned
// leaf and then to the tree's own summary.
func (inst *treemapDriver) pointerLine() string {
	if inst.tm == nil {
		return inst.statusLine()
	}
	if n := inst.tm.HoveredNode(); n != nil {
		return inst.describeNode(n)
	}
	if inst.selected != "" {
		return "pinned: " + truncateRunes(inst.selected, 64)
	}
	return inst.statusLine()
}

// describeNode reads a cell by its share of the total, its own value, and its
// category or measure when the colour column gave it one.
func (inst *treemapDriver) describeNode(n *layout.Node) string {
	var b strings.Builder
	b.WriteString(truncateRunes(n.Name, 64))
	total := n.TotalSize()
	fmt.Fprintf(&b, " — %s", treemapQty(total, inst.stats.unit))
	if root := inst.root; root != nil {
		if rt := root.TotalSize(); rt > 0 {
			fmt.Fprintf(&b, " (%.1f%%)", 100*total/rt)
		}
	}
	if len(n.Children) > 0 {
		fmt.Fprintf(&b, " · %s child(ren)", humanize.Comma(int64(len(n.Children))))
		if n.Size > 0 {
			fmt.Fprintf(&b, " · own %s", treemapQty(n.Size, inst.stats.unit))
		}
	}
	// An inherited colour is marked as one. The cell is drawn from it either
	// way, but a container whose colour came from below is not making the same
	// claim as a leaf the result described, and the readout is where that
	// difference can be said without a second visual channel.
	if i, ok := inst.idxOf[n]; ok {
		from := ""
		if !inst.ownColorAt(i) {
			from = " (inherited)"
		}
		switch inst.color.kind {
		case hierColorNumeric:
			if int(i) < len(inst.effColorNum) && !math.IsNaN(inst.effColorNum[i]) {
				fmt.Fprintf(&b, " · colour %s%s", treemapQty(inst.effColorNum[i], inst.color.unit), from)
			}
		case hierColorCategorical:
			if int(i) < len(inst.effColorKey) && inst.effColorKey[i] != "" {
				fmt.Fprintf(&b, " · %s%s", truncateRunes(inst.effColorKey[i], treemapMaxCatRunes), from)
			}
		}
	}
	return b.String()
}

// renderControls draws the colour and nesting switches. Both are draw-time
// only: neither rebuilds the tree, and the colour one does not even re-lay-out.
//
// The bars are StyleSelectable and emphatically NOT .Frameless() — the trap
// ADR-0160 §SD9 documents, where a frameless segmented bar draws its selected
// and unselected options identically. Groups are separated by AddSpace, never
// by c.Separator(), which in a horizontal row is a vertical rule sized to the
// pane's whole height.
func (inst *treemapDriver) renderControls() {
	gap := styletokens.GapSections(styletokens.ActiveDensity())
	for range c.HorizontalTop().KeepIter() {
		c.Label("colour").Send() // designlint:ignore=L1 (field caption; lowercase matches its control's own options)
		if inst.color.kind == hierColorNone {
			// Nothing to switch to: with no `color` column the depth ramp is the
			// whole coloring. Saying so beats a two-option bar whose other
			// option silently does nothing.
			for rt := range c.RichTextLabel("depth (no `color` column)") {
				rt.Small().Weak()
			}
		} else {
			selector.Segmented(inst.ids, "treemap-color", &inst.colorMode).
				Inline().
				Style(selector.StyleSelectable).
				Option(treemapColorData, treemapColorDataLabel(inst.color.kind)).
				Option(treemapColorDepth, "depth").
				SendResp()
		}
		c.AddSpace(gap)
		c.Label("show").Send() // designlint:ignore=L1 (field caption; lowercase matches its control's own options)
		selector.Segmented(inst.ids, "treemap-nesting", &inst.nesting).
			Inline().
			Style(selector.StyleSelectable).
			Option(treemapNestDrill, "drill").
			// "full", not "all": a forest's synthetic container is named `all`
			// and sits in the breadcrumb directly below this row, so two
			// different meanings of the word would share one pane — and an
			// accessibility-tree locator could not tell them apart either.
			Option(treemapNestAll, "full").
			SendResp()
	}
}

// treemapColorDataLabel names the data option by what the column turned out to
// be, so the bar says which encoding is on offer rather than a bare "colour".
func treemapColorDataLabel(k hierColorKindE) string {
	if k == hierColorCategorical {
		return "category"
	}
	return "value"
}

// statusLine reports the tree's shape and everything the build noticed but
// could not decide.
func (inst *treemapDriver) statusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s cells", humanize.Comma(int64(inst.stats.nodes)))
	if inst.root != nil {
		fmt.Fprintf(&b, " · %s total", treemapQty(inst.root.TotalSize(), inst.stats.unit))
	}
	fmt.Fprintf(&b, " · %s input", inst.stats.mode)
	switch inst.color.kind {
	case hierColorNumeric:
		fmt.Fprintf(&b, " · colour %s–%s", treemapQty(inst.color.min, inst.color.unit),
			treemapQty(inst.color.max, inst.color.unit))
		// Which range is on is not visible in the picture — a ramp pinned to
		// the measure and one stretched over what happens to be here draw the
		// same cells in different colours — so the line says it.
		if inst.color.declared {
			b.WriteString(" (declared scale)")
		}
		if inst.stats.colorScale.rejected {
			b.WriteString(" · declared scale ignored: `color_min` and `color_max` must be finite with min < max")
		}
	case hierColorCategorical:
		fmt.Fprintf(&b, " · %d categor%s", len(inst.color.catOrder), plural(len(inst.color.catOrder), "y", "ies"))
		if inst.color.wrapped > 0 {
			fmt.Fprintf(&b, " (%d share a colour past the palette's %d)",
				inst.color.wrapped, styletokens.QualitativeCycleLen)
		}
	}
	if inst.inherited > 0 {
		fmt.Fprintf(&b, " · %s coloured from below", humanize.Comma(int64(inst.inherited)))
	}
	if inst.mixed > 0 {
		fmt.Fprintf(&b, " · %s mixed", humanize.Comma(int64(inst.mixed)))
	}
	if inst.stats.colorConflicts > 0 {
		fmt.Fprintf(&b, " · %s cell(s) given two colours, first kept",
			humanize.Comma(int64(inst.stats.colorConflicts)))
	}
	if inst.stats.droppedPath > 0 {
		fmt.Fprintf(&b, " · %s row(s) without a path", humanize.Comma(int64(inst.stats.droppedPath)))
	}
	if inst.stats.droppedValue > 0 {
		fmt.Fprintf(&b, " · %s row(s) without a finite, non-negative value", humanize.Comma(int64(inst.stats.droppedValue)))
	}
	if inst.stats.droppedDup > 0 {
		fmt.Fprintf(&b, " · %s duplicate id(s) dropped", humanize.Comma(int64(inst.stats.droppedDup)))
	}
	if inst.stats.reparented > 0 {
		fmt.Fprintf(&b, " · %s row(s) with an unknown parent, drawn as roots", humanize.Comma(int64(inst.stats.reparented)))
	}
	if inst.stats.truncated > 0 {
		fmt.Fprintf(&b, " · %s path(s) cut at depth %d",
			humanize.Comma(int64(inst.stats.truncated)), hierMaxDepth)
	}
	if inst.stats.capped {
		fmt.Fprintf(&b, " · capped at %s cells (aggregate the tail)", humanize.Comma(hierMaxNodes))
		if inst.stats.droppedCapped > 0 {
			fmt.Fprintf(&b, ", %s row(s) past it", humanize.Comma(int64(inst.stats.droppedCapped)))
		}
	}
	return b.String()
}

// plural picks a suffix. Small enough not to earn a dependency, and the status
// line is the only caller.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// treemapQty formats a quantity for the status, pointer and legend-tick lines,
// suffixing the unit when the result declared one. The job is only to keep a
// big total from crowding the line out.
//
// A unit starting with a LETTER is spaced off the number ("9.0G bytes"); one
// starting with anything else is not ("72.5%"). That is the typographic
// convention for the sign-like units — %, °, currency — and no book emits one
// today, so it changes no existing picture.
func treemapQty(v float64, unit string) string {
	var s string
	switch av := math.Abs(v); {
	case av >= 1e9:
		s = strconv.FormatFloat(v/1e9, 'f', 1, 64) + "G"
	case av >= 1e6:
		s = strconv.FormatFloat(v/1e6, 'f', 1, 64) + "M"
	case av >= 1e4:
		s = strconv.FormatFloat(v/1e3, 'f', 1, 64) + "k"
	default:
		s = strconv.FormatFloat(v, 'g', 4, 64)
	}
	if unit != "" {
		if r, _ := utf8.DecodeRuneInString(unit); unicode.IsLetter(r) {
			s += " "
		}
		s += unit
	}
	return s
}

// renderTreemapTab is the Treemap dock tab body (ADR-0166): the active result as
// nested rectangles. A plain PanelI observer with the same guards as the World,
// Kanban and Icicle tabs, plus the executed timestamp handed to the driver as
// its tree-cache key.
func (inst *PlayApp) renderTreemapTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query with a `stack` array and a `value` column — or one row per " +
			"node with `id`, `parent` and `value` — to see a treemap.") {
			rt.Small().Weak()
		}
		return
	}
	inst.treemapDriver.noteExecuted(executed)
	reject := dispatchPanel(treemapPanel{driver: inst.treemapDriver}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("treemap"), rec: rec, schema: schema, sig: inst.frameSig},
	}, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}
