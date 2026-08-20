package play

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle"
	icicleview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// play_icicle_panel.go is the Icicle dock tab: the active result drawn as an
// icicle plot or a flamegraph over the icicle widget (ADR-0160), whose §SD8
// deferred "a generic `play` panel over a stack/value column convention" as one
// of the two obvious next slices. This is that slice.
//
// The column contract it accepts — folded `stack`+`value`, or `id`/`parent`/
// `value` — is no longer this panel's own: it moved to play_hierarchy.go when
// the Treemap tab became its second reader (ADR-0166 §SD1). What stays here is
// the icicle reading of it: the value axis, the depth rows, and the status line
// that names the mode a result took rather than leaving it to be inferred from
// the picture.
//
// SELECTION IS LOCAL, though the panel binds the active result (chMain) as
// Table and World do and could therefore publish the row cursor. It does not:
// in folded mode a frame is a path PREFIX, so an interior frame spans many rows
// and a leaf frame is one row only when the stacks happen to be unique — a
// cursor derived from a frame would point at an arbitrary member of its
// subtree. A clicked frame publishes its label as `selection_key` instead, a
// value rather than a cursor, which is also what a follow-up query wants:
// `WHERE has(stack, {selection_key:String})`.

// icicleForm is this pane's vocabulary in a shared reject message.
var icicleForm = hierForm{noun: "flame view", elem: "frame"}

// icicleIDSalt namespaces the panel's pane probe — distinct from the other
// panels' salts; the per-instance idSeed (nextVizSeed) keeps two live PlayApps
// apart.
const icicleIDSalt uint64 = 0x5a11c0de17f10006

// iciclePaneFill is the plot's box — the shared pane rule (play_pane_box.go).
// The floor is a readability one and sits well above implot.MinBoxHeight, which
// is what the value axis needs before it clips its own tick labels;
// TestIciclePlotFloorClearsTheClipFloor holds the two in that order.
var iciclePaneFill = paneFill{
	slack: 12, minW: 360, maxW: 1600, minH: 260,
	fallbackW: 760, fallbackH: 460,
}

// iciclePruneE is the layout-time pruning control, mapped onto
// icicle.Options.MinFraction. Pruning is resolution-independent and
// reproducible, deliberately distinct from the view's sub-pixel culling
// (ADR-0160 §SD7): culling only skips what is currently invisible, where a
// pruned subtree is gone from the layout and counted in the Report, so the
// status line can say how much of the total is missing.
type iciclePruneE uint8

const (
	iciclePruneOff iciclePruneE = iota
	iciclePruneTenth
	iciclePrunePercent
)

func (p iciclePruneE) fraction() float64 {
	switch p {
	case iciclePruneTenth:
		return 0.001
	case iciclePrunePercent:
		return 0.01
	}
	return 0
}

// icicleClaim and icicleStats are this pane's names for the shared hierarchy
// contract (play_hierarchy.go). The contract is the same one the Treemap tab
// resolves; only the reading of it differs.
type (
	icicleClaim = hierClaim
	icicleStats = hierStats
)

// icicleMaxDepth is the shared path cap, named here because the status line
// quotes it.
const icicleMaxDepth = hierMaxDepth

// resolveIcicleColumns resolves the shared contract in this pane's vocabulary.
func resolveIcicleColumns(schema *arrow.Schema) (cl icicleClaim, reason string) {
	return resolveHierarchy(schema, icicleForm)
}

// buildIcicleTree builds the shared flat tree and presents it as the widget's
// own type. The three columns are the same slices — icicle.Tree IS the shared
// shape minus the colour channel, which this form has no room for: its fill
// already encodes the frame name or the depth (ADR-0160 §SD6).
func buildIcicleTree(rec arrow.RecordBatch, cl icicleClaim) (t icicle.Tree, st icicleStats) {
	h, st := buildHierarchy(rec, cl)
	return icicle.Tree{Labels: h.Labels, Parents: h.Parents, Self: h.Self}, st
}

// icicleTreeOpts is the layout half of the driver's controls.
func icicleTreeOpts(orient icicle.OrientationE, order icicle.OrderE, prune iciclePruneE, unit string) icicle.Options {
	return icicle.Options{
		Orientation: orient,
		Order:       order,
		MinFraction: prune.fraction(),
		Unit:        unit,
	}
}

// icicleLayoutKey is the layout cache key: the tree generation plus every
// control that changes GEOMETRY. Colour and label switches are draw-time only
// and deliberately absent, so recolouring never re-lays-out.
//
// A generation counter rather than a fingerprint of the tree: hashing twenty
// thousand nodes every frame to discover they did not change is the cost the
// cache exists to avoid.
type icicleLayoutKey struct {
	gen    uint64
	orient icicle.OrientationE
	order  icicle.OrderE
	prune  iciclePruneE
}

// IcicleDriver owns the Icicle tab state: the built tree and its cache, the
// laid-out geometry and its cache, the draw options, and the locally-pinned
// selection.
type IcicleDriver struct {
	ids    *c.WidgetIdStack
	idSeed uint64

	// paneW / paneH are the last box the pane probe reported. Held across
	// frames rather than read fresh: the probe answers nothing on the first
	// frame and again on the frame a hidden tab comes back (a seq that did not
	// capture is absent from the drain), and resizing the plot to a fallback on
	// those frames would flash.
	paneW, paneH float32

	// renderer holds the frame and label batch buffers across frames — one per
	// pane, per ADR-0160; the free functions allocate a throwaway per call.
	renderer icicleview.Renderer

	orient     icicle.OrientationE
	order      icicle.OrderE
	colorBy    icicleview.ColorModeE
	prune      iciclePruneE
	hideLabels bool

	// Tree cache: the result identity (the executed timestamp, the same
	// freshness token the pager, World and Kanban use) plus the schema the
	// claim was resolved from. Interning a big result is per-node work and has
	// no business running every frame.
	tree        icicle.Tree
	forExecuted time.Time
	forSchema   *arrow.Schema
	treeGen     uint64

	layout    *icicle.Layout
	layoutKey icicleLayoutKey
	layoutErr error
	// resetView is raised for exactly one frame whenever the layout is
	// recomputed. implot retains a plot's ranges per plot id and applies the
	// initial limits CondOnce, so a new tree — or the same tree pruned, or
	// flipped — would otherwise be viewed through the previous one's value
	// window (ADR-0160 §SD3).
	resetView bool

	// selected is the click-pinned frame; hover carries last frame's pointer
	// hit, since the readout line is drawn above the plot that produces it.
	// Both are LOCAL — see the file comment on why no cursor is published.
	selected icicleview.Hit
	hover    icicleview.Hit

	stats icicleStats

	// pendingExecuted is stashed by renderIcicleTab before dispatch — the
	// PanelI Render signature carries no result metadata (the World pane's
	// noteExecuted handoff).
	pendingExecuted time.Time
}

// NewIcicleDriver builds the driver. It takes no client: unlike Sankey and
// Network the panel reads the ACTIVE RESULT, so it has no lane of its own.
func NewIcicleDriver(ids *c.WidgetIdStack) (inst *IcicleDriver) {
	return &IcicleDriver{ids: ids, idSeed: nextVizSeed()}
}

// noteExecuted hands the driver the active result's freshness token before
// dispatch; the tree cache keys on it.
func (inst *IcicleDriver) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// iciclePanel is the PanelI face. Acceptance is schema-only and cheap — it runs
// every frame — because both contracts are questions about column names and
// one column's type, which the schema answers on its own.
type iciclePanel struct {
	driver *IcicleDriver
}

func (inst iciclePanel) ID() PanelID { return "icicle" }

func (inst iciclePanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "frames"}}
}

func (inst iciclePanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query with a `stack` array and a `value` (or `id`/`parent`/`value`) to see a flame view."
		return
	}
	cl, r := resolveIcicleColumns(schema)
	if r != "" {
		reason = r
		return
	}
	claim = cl
	return
}

func (inst iciclePanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main, ok := filled[chMain]
	if !ok {
		return
	}
	cl, isClaim := main.Claim.(icicleClaim)
	if !isClaim {
		return
	}
	inst.driver.render(main.Rec, main.Rec.Schema(), cl, emit)
}

// render builds the tree (cached on the result identity), lays it out (cached
// on the tree generation and the geometry controls), draws it, and tracks the
// pinned frame.
func (inst *IcicleDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, cl icicleClaim, emit SignalEmitterI) {
	if schema != inst.forSchema || !inst.pendingExecuted.Equal(inst.forExecuted) || inst.treeGen == 0 {
		inst.tree, inst.stats = buildIcicleTree(rec, cl)
		inst.forSchema, inst.forExecuted = schema, inst.pendingExecuted
		inst.treeGen++
		// A new tree invalidates a pin taken against the old one: the index
		// would still resolve, and to a different frame.
		inst.selected, inst.hover = icicleview.Hit{}, icicleview.Hit{}
	}
	inst.renderControls()

	if inst.tree.Len() == 0 {
		// Drop the cached layout with the tree that produced it. Kept, it would
		// go on reporting the previous result's total beside a pane that is now
		// empty — the one lie a status line must not tell.
		inst.layout, inst.layoutKey, inst.layoutErr = nil, icicleLayoutKey{}, nil
		c.Label(inst.statusLine()).Send()
		for rt := range c.RichTextLabel("No frames: every row was missing a path and an id, or carried a value " +
			"that is not a finite, non-negative number.") {
			rt.Small().Weak()
		}
		return
	}

	if key := (icicleLayoutKey{gen: inst.treeGen, orient: inst.orient, order: inst.order, prune: inst.prune}); key != inst.layoutKey {
		inst.layoutKey = key
		inst.layout, inst.layoutErr = icicle.Compute(inst.tree,
			icicleTreeOpts(inst.orient, inst.order, inst.prune, inst.stats.unit))
		// One frame of CondAlways limits, on the frame the geometry changed.
		inst.resetView = true
	}
	if inst.layout == nil {
		c.Label(inst.statusLine()).Send()
		msg := "the rows cannot be laid out as a hierarchy"
		if inst.layoutErr != nil {
			msg += ": " + icicleReason(inst.layoutErr)
		}
		for rt := range c.RichTextLabel(msg) {
			rt.Small().Weak()
		}
		return
	}

	// The readouts go ABOVE the plot. Both are one frame behind — a register
	// read always is — so nothing is lost by drawing them before the frame's own
	// Show, and it puts the total, the warnings and the hover readout where a
	// pane too short for the plot cannot push them out of sight (the note
	// ADR-0159's play Update left for the next consumer).
	c.Label(inst.pointerLine(inst.hover)).Send()

	// The hint goes above the plot too, and here it is what lets the plot be the
	// LAST widget in the body: the probe below reports the room left for the
	// next widget, so with the hint underneath, taking that room would push it
	// past the fold and hold a scrollbar open — which narrows the pane, which
	// resizes the plot.
	for rt := range c.RichTextLabel("hover a frame, click to zoom the value axis to it and pin it; " +
		"double-click fits the whole tree, drag scrolls the depth — over the plot the wheel is the plot's, " +
		"elsewhere it scrolls the pane") {
		rt.Small().Weak()
	}

	// Fill the pane. The height used to be a fixed 0.31 of the width, measured
	// off a tour capture of one leaf, because the pane's own height was
	// unreadable here: the one register carrying it, CaptureAvailableSize, is a
	// single slot the frame's last capture wins, so a second writer corrupts
	// both. captureUiAvailableRect retired that — it reports the free rect into
	// the same seq-keyed r21 slot this probe already uses, so the leaf says how
	// tall it is and a taller one buys what this form spends height on: more
	// depth rows above the fold, since the axis holds them at RowPx and scrolls
	// the rest.
	c.Separator().Horizontal().Send()
	if availW, availH, ok := c.CapturePaneSize(icicleIDSalt ^ inst.idSeed ^ 0x1); ok {
		inst.paneW, inst.paneH = availW, availH
	}
	w, h := iciclePaneFill.box(inst.paneW, inst.paneH)

	reset := inst.resetView
	inst.resetView = false
	// Opts.Hover is deliberately not set: Show overwrites it with what its own
	// Probe returned before drawing, so anything passed here is dead. The
	// driver's copy exists for the readout line above, which is drawn before
	// this call and so reads last frame's.
	hover, click, clicked := inst.renderer.Show(inst.ids, "frames##playicicle", w, h, inst.layout, icicleview.Opts{
		Color:      inst.colorBy,
		Selected:   inst.selected,
		HideLabels: inst.hideLabels,
		XLabel:     inst.stats.unit,
		ResetView:  reset,
	})
	inst.hover = hover
	// Any click updates the pin, including one that landed on no frame — that is
	// what clears it. Clicking the pinned frame again also clears it, so the
	// gesture is its own undo. The published key follows the pin, and an empty
	// one is the honest "nothing focused" value a query reading
	// `{selection_key:String}` sees before anything is clicked.
	if clicked {
		if click == inst.selected {
			inst.selected = icicleview.Hit{}
		} else {
			inst.selected = click
		}
		if emit != nil {
			emit.Emit(signalSelectionKey, inst.selectedLabel())
		}
	}

}

// selectedLabel is the pinned frame's label, or "" when nothing is pinned.
func (inst *IcicleDriver) selectedLabel() string {
	n := inst.nodeAt(inst.selected)
	if n == nil {
		return ""
	}
	return n.Label
}

// nodeAt resolves a hit against the current layout, or nil.
func (inst *IcicleDriver) nodeAt(h icicleview.Hit) *icicle.Node {
	if inst.layout == nil || h.None() {
		return nil
	}
	i := int(h.Node)
	if i < 0 || i >= len(inst.layout.Nodes) {
		return nil
	}
	return &inst.layout.Nodes[i]
}

// renderControls draws the orientation, order, colour and prune switches.
// Changing one of the first three re-keys the layout cache, so the next frame
// re-lays-out; the colour and label switches are draw-time only and do not.
//
// The groups are separated by AddSpace, never by c.Separator(): a separator in a
// horizontal row is a VERTICAL rule sized to the row's available height, which
// in a dock leaf is the whole pane — it balloons, makes the control row that
// tall, and shoves the plot off the bottom. The Table pane's options bar hit it
// first and the Sankey's Update recorded it for the next consumer.
//
// The bars are StyleSelectable, not the segmented default: unselected options
// stay bare text and only the selected one takes a highlight, which is the
// densest readable form and what four bars plus a checkbox need to fit one row.
//
// They are emphatically NOT .Frameless(), which is the trap that used to render
// this row: StyleSegmented shows the selection by FILLING the selected segment,
// and Frame(false) is what draws no background, so a frameless segmented bar
// draws its selected and unselected options identically. Two tour captures of
// this pane differing only in orientation came back with pixel-identical
// control rows, which is how it was found — and how the Flow, Network and
// Sankey panels were then found to have it too.
func (inst *IcicleDriver) renderControls() {
	gap := styletokens.GapSections(styletokens.DensityFromEnv())
	for range c.HorizontalTop().KeepIter() {
		selector.Segmented(inst.ids, "icicle-orient", &inst.orient).
			Inline().
			Style(selector.StyleSelectable).
			Option(icicle.OrientIcicle, "icicle").
			Option(icicle.OrientFlame, "flame").
			SendResp()
		c.AddSpace(gap)
		c.Label("order").Send() // designlint:ignore=L1 (field caption; lowercase matches its control's own options)
		selector.Segmented(inst.ids, "icicle-order", &inst.order).
			Inline().
			Style(selector.StyleSelectable).
			Option(icicle.OrderValueDesc, "value").
			Option(icicle.OrderLabel, "name").
			Option(icicle.OrderInput, "input").
			SendResp()
		c.AddSpace(gap)
		c.Label("colour").Send() // designlint:ignore=L1 (field caption; lowercase matches its control's own options)
		selector.Segmented(inst.ids, "icicle-color", &inst.colorBy).
			Inline().
			Style(selector.StyleSelectable).
			Option(icicleview.ColorByLabel, "label").
			Option(icicleview.ColorByDepth, "depth").
			SendResp()
		c.AddSpace(gap)
		c.Label("prune").Send() // designlint:ignore=L1 (field caption; lowercase matches its control's own options)
		selector.Segmented(inst.ids, "icicle-prune", &inst.prune).
			Inline().
			Style(selector.StyleSelectable).
			Option(iciclePruneOff, "off").
			Option(iciclePruneTenth, "0.1%").
			Option(iciclePrunePercent, "1%").
			SendResp()
		c.AddSpace(gap)
		c.Checkbox(inst.ids.PrepareStr("icicle-hidelabels"), inst.hideLabels, "hide labels").
			SendRespVal(&inst.hideLabels)
	}
}

// pointerLine describes what the pointer is over, falling back to the pinned
// frame and then to the tree's own summary. A frame is read by its share of the
// total and by how much of it is its own — self versus total is the question a
// stack profile is asked, and the one a treemap cannot answer.
func (inst *IcicleDriver) pointerLine(hover icicleview.Hit) string {
	lay := inst.layout
	if lay == nil {
		return inst.statusLine()
	}
	describe := func(h icicleview.Hit) string {
		n := inst.nodeAt(h)
		if n == nil {
			return ""
		}
		var b strings.Builder
		b.WriteString(icicleFramePath(lay, int(h.Node)))
		fmt.Fprintf(&b, " — %s", icicleQty(n.Total, inst.stats.unit))
		if t := lay.Report.Total; t > 0 {
			fmt.Fprintf(&b, " (%.1f%%)", 100*n.Total/t)
		}
		fmt.Fprintf(&b, " · self %s · depth %d", icicleQty(n.Self, inst.stats.unit), n.Depth)
		return b.String()
	}
	if s := describe(hover); s != "" {
		return s
	}
	if s := describe(inst.selected); s != "" {
		return "pinned: " + s
	}
	return inst.statusLine()
}

// icicleFramePath renders a frame's ancestry (PathTo is root-first) as
// `a › b › leaf`, dropping ancestors from the LEFT until it fits: the frame
// under the pointer is the leaf, and its nearest ancestors say more about it
// than the root does. The leaf itself is always kept, however long it is —
// truncated, but never dropped.
func icicleFramePath(lay *icicle.Layout, node int) string {
	path := lay.PathTo(node)
	parts := make([]string, 0, len(path))
	for _, i := range path {
		if int(i) < len(lay.Nodes) {
			parts = append(parts, truncateRunes(lay.Nodes[i].Label, 48))
		}
	}
	for from := 0; from < len(parts); from++ {
		s := strings.Join(parts[from:], " › ")
		if from > 0 {
			s = "… › " + s
		}
		if len([]rune(s)) <= icicleFramePathRunes || from == len(parts)-1 {
			return s
		}
	}
	return ""
}

// icicleFramePathRunes bounds the breadcrumb. Wide enough for a handful of Go
// frames, short enough to leave the quantities on the same line visible in a
// half-width pane.
const icicleFramePathRunes = 96

// statusLine reports the tree's shape and everything the build or the layout
// noticed but could not decide.
func (inst *IcicleDriver) statusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s frames", humanize.Comma(int64(inst.stats.nodes)))
	if lay := inst.layout; lay != nil {
		fmt.Fprintf(&b, " · %d deep · %s total", lay.Report.Rows, icicleQty(lay.Report.Total, inst.stats.unit))
		if lay.Report.Pruned > 0 {
			fmt.Fprintf(&b, " · %s frame(s) pruned (%s)", humanize.Comma(int64(lay.Report.Pruned)),
				icicleQty(lay.Report.PrunedValue, inst.stats.unit))
		}
	}
	fmt.Fprintf(&b, " · %s input", inst.stats.mode)
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
			humanize.Comma(int64(inst.stats.truncated)), icicleMaxDepth)
	}
	if inst.stats.capped {
		fmt.Fprintf(&b, " · capped at %s frames (prune, or aggregate the tail)", humanize.Comma(hierMaxNodes))
		if inst.stats.droppedCapped > 0 {
			fmt.Fprintf(&b, ", %s row(s) past it", humanize.Comma(int64(inst.stats.droppedCapped)))
		}
	}
	return b.String()
}

// icicleReason reduces an error to a status line: the first line, clipped.
// The widget's messages carry no package prefix to strip — the pane already
// says which panel this is (CODINGSTANDARDS "Message Text").
func icicleReason(err error) string {
	if err == nil {
		return ""
	}
	return truncateRunes(firstLineOf(err.Error()), 140)
}

// icicleQty formats a quantity for the status and pointer lines, suffixing the
// unit when the result declared one. The job is only to keep a big total from
// crowding the line out.
func icicleQty(v float64, unit string) string {
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
		s += " " + unit
	}
	return s
}

// renderIcicleTab is the Icicle dock tab body (ADR-0160): the active result as
// an icicle plot or a flamegraph. A plain PanelI observer with the same guards
// as the World and Kanban tabs, plus the executed timestamp handed to the driver
// as its tree-cache key.
func (inst *PlayApp) renderIcicleTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
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
			"node with `id`, `parent` and `value` — to see a flame view.") {
			rt.Small().Weak()
		}
		return
	}
	inst.icicleDriver.noteExecuted(executed)
	reject := dispatchPanel(iciclePanel{driver: inst.icicleDriver}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("icicle"), rec: rec, schema: schema, sig: inst.frameSig},
	}, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}
