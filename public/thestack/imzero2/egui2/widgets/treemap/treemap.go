// Package treemap is a semi-retained widget (ADR-0267): an interactive
// Frame-based treemap with zoom-from-rect transitions on drill-in and
// drill-up. Cells are egui Frames with .SenseClick() so hover and click
// handling flows through egui's response system. The zoom tween is driven by
// egui::Context::animate_bool_with_time via the bindings.AnimateBoolWithTimeBind
// wrapper.
//
// The squarified layout algorithm and the tree data type (Node, Rect,
// ComputeLayoutAt) live in the sibling package treemap/layout so they can be
// used by callers that only need tile placement, without pulling in the
// egui/FFFI widget machinery.
//
// Basic usage:
//
//	tm := treemap.New(ids, "disk-usage", scanDisk(), treemap.Options{AnimationDuration: 0.28})
//	// every frame:
//	ev := tm.Render(700, 450) // or tm.RenderFill(700, 450) in a bounded pane
//	if ev.ClickedLeaf != nil { open(ev.ClickedLeaf) }
//
// Options are a struct kept as the public [Treemap.Opts] and re-read every
// frame, so switching what a cell's colour encodes is an assignment that keeps
// the drill position. SetRoot replaces the tree; what the user did comes back
// in [Events].
//
// Multiple instances can coexist safely: Render wraps its body in c.IdScope
// keyed by scopeKey, so the imzero2 id stack automatically XORs the instance
// scope into every internal id.
//
// # Validation policy
//
// Every public symbol classifies its error handling as one of three tiers:
//
//   - Panic: programmer errors (nil where required, structurally impossible
//     input). Fails loudly at the call site so bugs surface during development.
//     Examples: New(nil ids, …), SetRoot(nil).
//
//   - Error return: caller-controlled runtime input where the caller can
//     react. Returns a documented sentinel error and leaves state unchanged.
//     Examples: NavigateTo, DrillTo.
//
//   - Log + safe default: construction-time data input with an obvious
//     recovery, where there is no caller to return to. Emits a single
//     log.Warn (Str("pkg","treemap")) and falls back to a documented default.
//     Example: Options.InitialPath with a stale path → ignored, root view kept.
//
// Each option/method states its tier explicitly in its godoc.
package treemap

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// Named palettes (Viridis8, Magma8, Inferno8, Cividis8) live in palettes.go.
// DefaultDepthColors is exported there as a Viridis8 alias for backwards
// compatibility.

const (
	defaultContainerW float32 = 700
	defaultContainerH float32 = 450
	// Default zoom-transition duration: the IDS motion ladder's "slow"
	// rung (ADR-0032 §SD5, 320 ms). The previous 0.28 const was off the
	// ladder and bypassed reduced-motion; the new ctor initializer reads
	// styletokens.MotionSlowSecs() at construction time, which collapses
	// to 0 when motion is disabled.
	// animDoneEps lives in machine.go alongside animMachine.

	// Cell seqs count one per cell under the container Frame's own scope; a
	// hatch canvas reuses its cell's seq under a "hatch" scope.

	// maxPreviewRecursion safety-caps "unlimited" preview nesting
	// (Options.MaxNestingDepth < 0); real trees terminate far sooner via the
	// minimum-cell-size cull. Guards against a pathologically deep tree.
	maxPreviewRecursion int = 64
)

// CellStateE is a bitfield of orthogonal per-cell state flags. Multiple flags
// can be set simultaneously (e.g. a drill-up cell is both OnPath and DrillUp).
// Used by ColoringI and StyleI resolution so callers can style by any
// combination of conditions without duplicating branching logic.
type CellStateE uint16

const (
	// CellStateDrillable — frontier cell with children; clicking drills DOWN.
	CellStateDrillable CellStateE = 1 << iota
	// CellStateDrillUp — active-path cell above the current focus; clicking drills UP to here.
	CellStateDrillUp
	// CellStateFocused — the deepest active cell; its children are what the user is viewing.
	CellStateFocused
	// CellStateFrontier — cell is at the deepest visible recursion level.
	CellStateFrontier
	// CellStateLeaf — node has no children.
	CellStateLeaf
	// CellStateOnPath — cell is on the breadcrumb ancestor chain (same as "active" in old code).
	CellStateOnPath
	// CellStateOffPath — cell is a context sibling, not on the active path, not at the frontier.
	CellStateOffPath
	// CellStatePreview — non-interactive preview cell rendered inside a drillable parent.
	CellStatePreview
	// CellStateHovered — mouse pointer is over the cell this frame.
	CellStateHovered
	// CellStateSelf — the cell carries a container's OWN size rather than a
	// child's (ADR-0166 §SD3). Its Node is the container itself, so a
	// ColoringI or StyleI seeing this bit is being asked about the parent's
	// own quantity, not about a distinct node. Never interactive: drilling
	// into a node from inside itself has nowhere to go.
	CellStateSelf
)

// Has reports whether every bit in flag is set in s.
func (s CellStateE) Has(flag CellStateE) bool { return s&flag == flag }

// HasAny reports whether at least one bit in flag is set in s.
func (s CellStateE) HasAny(flag CellStateE) bool { return s&flag != 0 }

// Interactive reports whether a click on this cell will trigger navigation.
func (s CellStateE) Interactive() bool { return s.HasAny(CellStateDrillable | CellStateDrillUp) }

// CellInfo is the full context passed to ColoringI and StyleI implementations.
type CellInfo struct {
	Node  *layout.Node
	Depth int
	State CellStateE
}

// HatchSpec describes a diagonal-line hatch overlay. The zero value (Width=0
// or Spacing<=0) means no hatch.
type HatchSpec struct {
	Color    uint32  // 0xRRGGBBAA
	Width    float32 // line width in logical pixels
	Spacing  float32 // gap between adjacent lines, perpendicular to AngleDeg
	AngleDeg float32 // line angle in degrees; 45 = top-left to bottom-right
}

// IsZero reports whether the spec describes no hatch.
func (h HatchSpec) IsZero() bool { return h.Width <= 0 || h.Spacing <= 0 }

// CellVisuals is a StyleI's per-cell verdict on geometry, decoration, and
// which color-slot from the active ColoringI to use. StyleI and ColoringI are
// orthogonal: StyleI decides *which* color role applies to each visual
// element; ColoringI decides *what* the colors actually are.
type CellVisuals struct {
	BorderWidth          float32 // 0 = no border
	CornerRadius         float32
	Hatch                HatchSpec // zero value = no hatch overlay
	UseDimFill           bool      // use ColoringI.DimFill instead of ColoringI.Fill
	UseHoverFill         bool      // when CellStateHovered: use ColoringI.HoverFill (overrides UseDimFill)
	UseAccentBorder      bool      // use ColoringI.AccentBorder instead of ColoringI.Border
	AccentBorderOverride uint32    // non-zero: force this stroke color (0xRRGGBBAA), overrides UseAccentBorder
}

// StyleI returns the per-cell visual properties (geometry + decoration).
// Implementations should be pure functions of CellInfo so they can be
// composed and unit-tested.
type StyleI interface {
	Visuals(CellInfo) CellVisuals
}

// DefaultStyle returns the built-in style: drillable cells get a thick
// border and hover affordance; drill-up cells get a thinner accent-color
// border on a dim fill; truly inert cells get a 45° black hatch overlay
// unless they render visible children inside.
func DefaultStyle() StyleI { return defaultStyle{} }

var defaultHatch = HatchSpec{Color: 0x000000d0, Width: 1.0, Spacing: 6.0, AngleDeg: -45}

type defaultStyle struct{}

var _ StyleI = defaultStyle{}

func (defaultStyle) Visuals(info CellInfo) CellVisuals {
	switch {
	case info.State.Has(CellStateDrillable):
		// Bright fill, hover swaps to brighter, default border.
		v := CellVisuals{BorderWidth: 1.2, CornerRadius: 3.0, UseHoverFill: true}
		if info.State.Has(CellStateHovered) {
			v.BorderWidth = 1.8
		}
		return v
	case info.State.Has(CellStateDrillUp):
		// Dim fill with bright accent border = "container you can click to focus here".
		v := CellVisuals{BorderWidth: 0.8, CornerRadius: 3.0, UseDimFill: true, UseAccentBorder: true}
		if info.State.Has(CellStateHovered) {
			v.BorderWidth = 1.4
		}
		return v
	case info.State.Has(CellStatePreview):
		// Preview cells inside drillable parents: dim fill, no hatch
		// (hatching would make the drillable parent visually read as disabled).
		return CellVisuals{BorderWidth: 0.4, CornerRadius: 2.0, UseDimFill: true}
	default:
		// Truly inert: dim fill + hatch unless the cell renders children inside
		// (e.g., the focused container shows its children, no hatch needed).
		v := CellVisuals{BorderWidth: 0.4, CornerRadius: 3.0, UseDimFill: true}
		hasInnerContent := info.State.HasAny(CellStateOnPath|CellStateFrontier) && !info.State.Has(CellStateLeaf)
		if !hasInnerContent {
			v.Hatch = defaultHatch
		}
		return v
	}
}

// CellColorFn maps a node to an index into the cell palette. Called for
// every rendered cell — leaf and directory alike — so callers can encode
// subtree-aggregate metrics on the drilled-out view and per-file metrics on
// the drilled-in view using the same function.
type CellColorFn func(node *layout.Node) int

// cellDesc captures the per-frame state needed for the post-render
// interaction pass. At most one of drillable (down) or drillUpTo>0 (up).
type cellDesc struct {
	node      *layout.Node
	handle    widgethandle.WidgetHandle
	drillable bool
	drillUpTo int
	rect      layout.Rect
	state     CellStateE
	depth     int
}

// Treemap is a Frame-based zoomable treemap widget.
type Treemap struct {
	// Opts is re-read at the top of every Render (see [Options]).
	Opts Options

	ids      *c.WidgetIdStack
	scopeKey string
	root     *layout.Node

	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2). Read once at construction from
	// styletokens.ActiveDensity() — the overlay is applied at Rust
	// startup with the same env var, so a runtime toggle here would
	// diverge from the visible state.
	density styletokens.DensityE

	// Config
	containerW  float32
	containerH  float32
	animDurSecs float32
	style       StyleI
	coloring    ColoringI
	// maxNestingDepth: preview levels rendered below the frontier (1 = the
	// historic single preview level; <=0 = all, capped by maxPreviewRecursion).
	maxNestingDepth int
	// cellLabelFn, when non-nil, supplies an optional secondary label
	// rendered on a second de-emphasized line beneath each cell's name (see
	// Options.CellLabel). Returning "" suppresses the line for that cell.
	cellLabelFn func(*layout.Node) string
	// selfCellLabelFn is cellLabelFn's counterpart for self cells (see
	// Options.SelfCellLabel). Deliberately separate rather than falling back to
	// cellLabelFn: that one is called with a container and answers for its
	// TOTAL, which is the wrong number to print on the cell that exists to
	// show the part of it the container holds itself.
	selfCellLabelFn func(*layout.Node) string
	// metrics supplies the measured label-height gates (see metrics.go).
	metrics labelMetrics

	// filterSiblings: when true and breadcrumb depth > 0, only the active
	// child at each level is laid out/rendered; siblings are excluded
	// entirely (no hatched placeholder cells).
	filterSiblings bool

	// leafClickSensing: when true, frontier leaf cells sense clicks and the
	// result is reported in Events.ClickedLeaf. Default: false.
	leafClickSensing bool

	// statusLineHidden suppresses the summary/hover line under the container
	// (Options.HideStatusLine). Stated negatively so the zero value keeps drawing it,
	// which is what every caller before the option got.
	statusLineHidden bool

	// Retained chrome colors (breadcrumb / container / leaf-view backgrounds)
	colorBreadcrumbBg  color.Color
	colorFrameStroke   color.Color
	colorBreadcrumbFg  color.Color
	colorBreadcrumbSep color.Color
	colorLeafText      color.Color
	colorTransparentBg color.Color
	colorContainerBg   color.Color
	colorLeafBg        color.Color

	// Observable state
	breadcrumb []*layout.Node

	// Per-frame transient; reset at the start of every Render.
	cells []cellDesc

	// leafClicked is the frontier or preview leaf clicked this frame (with
	// leafClickSensing); reported in Events.
	leafClicked *layout.Node
	// pendingNav collects breadcrumb changes until the next Render reports
	// them in Events.Nav.
	pendingNav []NavEvent

	// Zoom-transition animation state machine.
	anim animMachine
}

// NestingAll is the Options.MaxNestingDepth that renders the entire subtree
// below the frontier at once, bounded only by the minimum cell size.
const NestingAll = -1

// Options configures a Treemap. The zero value is the default widget: depth
// colouring, the default style, one preview level, the IDS motion duration,
// the status line shown. Render re-reads every field each frame, so a toggle
// is an assignment on [Treemap.Opts]; InitialPath alone is read at New.
type Options struct {
	// MaxNestingDepth is how many non-interactive preview levels render below
	// the focused (frontier) node. 0 is 1 — the frontier's children plus one
	// preview level; NestingAll (any negative value) shows the whole subtree,
	// capped by the minimum cell size and an internal recursion limit.
	MaxNestingDepth int
	// AnimationDuration is the zoom-transition duration in seconds; 0 is the
	// IDS motion ladder's slow rung (styletokens.MotionSlowSecs), a negative
	// value is instant.
	AnimationDuration float32
	// Coloring picks each cell's colours; nil is DepthColoring over
	// DefaultDepthColors. Compose effects with CompositeColoring.
	Coloring ColoringI
	// Style picks each cell's geometry and decoration; nil is DefaultStyle.
	Style StyleI
	// CellLabel, when set, supplies a second, de-emphasized line beneath a
	// cell's name — the place for a humanized metric. "" suppresses it for
	// that cell. Painted only on cells that do not draw their own children
	// and are tall enough for a second line.
	CellLabel func(*layout.Node) string
	// SelfCellLabel is CellLabel for SELF cells — the cell a container gets
	// for its own size (ADR-0166 §SD3). It is called with the container and
	// should format its own Size, not its TotalSize; it deliberately does
	// not fall back to CellLabel, which answers for the total.
	SelfCellLabel func(*layout.Node) string
	// LeafClickSensing makes frontier and preview leaf cells report clicks in
	// [Events.ClickedLeaf]. Drill-in and drill-up are unaffected.
	LeafClickSensing bool
	// HideStatusLine suppresses the summary/hover line under the container,
	// for a host that draws its own readout: the line speaks the widget's
	// byte vocabulary, which is wrong for a host counting something else.
	HideStatusLine bool
	// FilterSiblings lays out only the active child at each drilled level;
	// siblings are excluded rather than drawn hatched and inert. The
	// breadcrumb bar stays the way back.
	FilterSiblings bool
	// InitialPath opens the widget pre-drilled along a path from the root
	// (pointer-equal) down Children links. Read at New. An invalid path logs
	// one warning and keeps the root view.
	InitialPath []*layout.Node
}

// Events is what one Render produced.
type Events struct {
	// Nav is every breadcrumb change since the last Render, in order: the
	// user's cell and breadcrumb clicks and the host's own NavigateTo /
	// DrillTo / DrillUp / Reset calls, each tagged with its Trigger.
	Nav []NavEvent
	// ClickedLeaf is the leaf cell clicked this frame, nil for none; only
	// with Options.LeafClickSensing.
	ClickedLeaf *layout.Node
	// Hovered is the node whose cell is under the pointer this frame,
	// innermost first, or nil. Carries the usual one-frame hover lag.
	Hovered *layout.Node
}

// New constructs a Treemap widget rooted at root. scopeKey must be unique
// among Treemap instances sharing the same id stack — it's the label passed
// to c.IdScope so the id stack isolates each instance's widget ids.
//
// Panics if ids or root is nil, or if scopeKey is the empty string.
func New(ids *c.WidgetIdStack, scopeKey string, root *layout.Node, opts Options) *Treemap {
	if ids == nil {
		panic("treemap: New requires a non-nil ids stack")
	}
	if scopeKey == "" {
		panic("treemap: New requires a non-empty scopeKey")
	}
	if root == nil {
		panic("treemap: New requires a non-nil root")
	}
	inst := &Treemap{
		Opts:       opts,
		ids:        ids,
		scopeKey:   scopeKey,
		root:       root,
		density:    styletokens.ActiveDensity(),
		breadcrumb: []*layout.Node{root},
		containerW: defaultContainerW,
		containerH: defaultContainerH,
	}

	// Chrome (breadcrumb / frame / container / leaf surfaces) sources from
	// the IDS neutral spine (ADR-0031 §SD4). Per-cell *content* coloring
	// still flows through the ColoringI strategy (DepthColoring,
	// CategoricalColoring, …) so the treemap stays palette-pluggable.
	inst.colorBreadcrumbBg = color.Hex(styletokens.NeutralBgSurface.AsHex()).Keep()
	inst.colorFrameStroke = color.Hex(styletokens.NeutralBorderFaint.AsHex()).Keep()
	inst.colorBreadcrumbFg = color.Hex(styletokens.NeutralTextExtreme.AsHex()).Keep()
	inst.colorBreadcrumbSep = color.Hex(styletokens.NeutralTextDisabled.AsHex()).Keep()
	inst.colorTransparentBg = color.Transparent.Keep()
	inst.colorLeafText = color.Hex(styletokens.NeutralTextPrimary.AsHex()).Keep()
	inst.colorContainerBg = color.Hex(styletokens.NeutralBgPanel.AsHex()).Keep()
	inst.colorLeafBg = color.Hex(styletokens.NeutralBgSurface.AsHex()).Keep()

	if len(opts.InitialPath) > 0 {
		if inst.validPath(opts.InitialPath) {
			inst.breadcrumb = append(inst.breadcrumb[:0], opts.InitialPath...)
		} else {
			log.Warn().
				Str("pkg", "treemap").
				Int("pathLen", len(opts.InitialPath)).
				Msg("Options.InitialPath: invalid path, falling back to root")
		}
	}
	inst.applyOpts()
	inst.metrics.init(inst.density)
	return inst
}

// applyOpts resolves Opts into the fields the renderer reads. Called from
// New and at the top of every Render.
func (inst *Treemap) applyOpts() {
	o := &inst.Opts
	inst.maxNestingDepth = o.MaxNestingDepth
	switch {
	case inst.maxNestingDepth == 0:
		inst.maxNestingDepth = 1
	case inst.maxNestingDepth < 0:
		inst.maxNestingDepth = 0 // the renderer's "all"
	}
	switch {
	case o.AnimationDuration == 0:
		inst.animDurSecs = styletokens.MotionSlowSecs()
	case o.AnimationDuration < 0:
		inst.animDurSecs = 0
	default:
		inst.animDurSecs = o.AnimationDuration
	}
	inst.coloring = o.Coloring
	if inst.coloring == nil {
		inst.coloring = DepthColoring(DefaultDepthColors)
	}
	inst.style = o.Style
	if inst.style == nil {
		inst.style = DefaultStyle()
	}
	inst.cellLabelFn = o.CellLabel
	inst.selfCellLabelFn = o.SelfCellLabel
	inst.leafClickSensing = o.LeafClickSensing
	inst.statusLineHidden = o.HideStatusLine
	inst.filterSiblings = o.FilterSiblings
}

// SetRoot replaces the tree and resets the view to the root.
//
// The reset is not a convenience, it is the only honest option: a breadcrumb is
// a list of *layout.Node pointers INTO the tree being replaced, so keeping it
// would leave the widget focused on nodes the new tree does not contain. A
// caller that wants to restore a position across a swap has to re-resolve it
// against the new tree by name and call NavigateTo.
//
// Prefer this to constructing a new Treemap when the data changes: New re-runs
// metrics.init, whose text measurements settle a frame late, so a rebuild costs
// a frame of mis-gated labels. Options, style, coloring and container size all
// survive.
//
// Validation tier: panic — a nil root is a programmer error, as it is in New.
func (inst *Treemap) SetRoot(root *layout.Node) {
	if root == nil {
		panic("treemap: SetRoot requires a non-nil root")
	}
	inst.root = root
	inst.breadcrumb = []*layout.Node{root}
	// A zoom in flight is interpolating from a rect in the tree just replaced.
	inst.anim.Cancel()
}

// Focused returns the current tail of the breadcrumb.
func (inst *Treemap) Focused() *layout.Node { return inst.breadcrumb[len(inst.breadcrumb)-1] }

// Depth returns how deep the user has drilled (0 = root).
func (inst *Treemap) Depth() int { return len(inst.breadcrumb) - 1 }

// Breadcrumb returns a copy of the path from root to the current focus.
func (inst *Treemap) Breadcrumb() []*layout.Node {
	out := make([]*layout.Node, len(inst.breadcrumb))
	copy(out, inst.breadcrumb)
	return out
}

// resolveColors picks the appropriate fill, border, and text color from
// colors using the StyleI's role selectors and the cell's state. The text
// color tracks whichever fill slot is selected so WCAG-picked contrast
// stays consistent under dim/hover transitions. Kept as a method so test
// code can exercise it with arbitrary (colors, visuals, state) triples.
func (inst *Treemap) resolveColors(colors CellColors, visuals CellVisuals, state CellStateE) (fill, border, text color.Color) {
	fill = colors.Fill
	text = colors.Text
	if visuals.UseDimFill {
		fill = colors.DimFill
		text = colors.DimText
	}
	if visuals.UseHoverFill && state.Has(CellStateHovered) {
		fill = colors.HoverFill
		text = colors.HoverText
	}
	border = colors.Border
	if visuals.AccentBorderOverride != 0 {
		border = color.Hex(visuals.AccentBorderOverride)
	} else if visuals.UseAccentBorder {
		border = colors.AccentBorder
	}
	return
}

// containerRect returns the treemap canvas rect at its configured size.
// The size is the last one handed to Render, and is deliberately NOT derived
// from ui.available_size here. Consumers that want the treemap to track their
// pane (sccmap, imztop) measure it themselves, apply their own grow-guard and
// chrome budget, and pass the result to Render each frame; RenderFill does the
// same with its own probe for a bounded pane. Inside an auto-sizing host —
// e.g. the demo gallery's ScrollArea — a probed size would ratchet the host's
// height upward every frame, since the Frame would request the full available
// height while the breadcrumb bar and status label still need room above and
// below it, so such hosts pass a fixed size.
func (inst *Treemap) containerRect() layout.Rect {
	return layout.Rect{W: float64(inst.containerW), H: float64(inst.containerH)}
}

// innerRect computes the interior rect of a cell for recursive rendering.
func innerRect(r layout.Rect) layout.Rect {
	headerH := 20.0
	if r.H < 50 {
		headerH = 0
	}
	return layout.Rect{
		X: r.X + 3, Y: r.Y + headerH + 3,
		W: r.W - 6, H: r.H - headerH - 6,
	}
}

func lerpRect(a, b layout.Rect, s float64) layout.Rect {
	lerp := func(x, y float64) float64 { return x + (y-x)*s }
	return layout.Rect{X: lerp(a.X, b.X), Y: lerp(a.Y, b.Y), W: lerp(a.W, b.W), H: lerp(a.H, b.H)}
}

// computeZoomState builds the CellStateE bitfield for a cell rendered by
// renderZoom. Pure function of the recursion-local flags so it's easy to
// unit-test.
func computeZoomState(isActive, atFrontier, hasChildren, drillable, drillUp, hovered bool, bcLevel, bcLen int) CellStateE {
	var s CellStateE
	if drillable {
		s |= CellStateDrillable
	}
	if drillUp {
		s |= CellStateDrillUp
	}
	if isActive && bcLevel+2 == bcLen {
		s |= CellStateFocused
	}
	if atFrontier {
		s |= CellStateFrontier
	}
	if !hasChildren {
		s |= CellStateLeaf
	}
	if isActive {
		s |= CellStateOnPath
	} else if !atFrontier {
		s |= CellStateOffPath
	}
	if hovered {
		s |= CellStateHovered
	}
	return s
}

// computePreviewState builds the CellStateE bitfield for a cell rendered by
// renderLeafChildren (non-interactive preview inside a drillable parent).
func computePreviewState(hasChildren, hovered bool) CellStateE {
	s := CellStatePreview
	if !hasChildren {
		s |= CellStateLeaf
	}
	if hovered {
		s |= CellStateHovered
	}
	return s
}

// cellIds derives matching ids for a cell's Frame and its response handle.
// Two PrepareSeq+Derive cycles with the same seq produce the same scoped id
// under the active IdScope, so the Frame's server-side id equals cellHandle's.
//
// The id is derived relative to the container's scope and then held as an
// absolute creator, so a cell Frame pushes nothing onto the stack: nested
// cells keep numbering under the container's scope, not their parent cell's.
func (inst *Treemap) cellIds(seq uint64) (frameCreator c.WidgetIdCreatorI, handle widgethandle.WidgetHandle) {
	id := c.MakeAbsoluteIdHighEntropy(inst.ids.PrepareSeq(seq).Derive())
	return id, widgethandle.Make(id.Derive())
}

// paintHatch draws a diagonal line pattern at `r` per spec, marking the cell
// as non-interactive. Painter primitives queue into paint_cmds on the Rust
// side and only render when a PaintCanvas drains them, so we allocate a
// sibling Ui at the cell's rect and emit a no-background PaintCanvas to
// flush the lines on top of the previously-drawn cell Frame. seq must be
// unique per hatch instance within the enclosing IdScope.
//
// cornerRadius is the cell Frame's corner radius: hatchSegments trims the
// lines away from the rounded corners so the hatch never overhangs the
// fill's corner cutouts (a rectangular clip cannot follow the radii).
//
// Currently only ±45° angles are supported; other AngleDeg values fall back
// to -45°. (Generalizing to arbitrary angles is straightforward but unused.)
func (inst *Treemap) paintHatch(r layout.Rect, seq uint64, spec HatchSpec, cornerRadius float32) {
	if spec.IsZero() {
		return
	}
	segs := hatchSegments(r.W, r.H, float64(spec.Spacing), float64(cornerRadius), spec.AngleDeg > 0)
	if len(segs) == 0 {
		return
	}
	for range c.AllocateUiAtRect(float32(r.X), float32(r.Y), float32(r.X+r.W), float32(r.Y+r.H)).KeepIter() {
		// The segments are already clamped to r; the clip additionally
		// bounds stroke width and AA feather at the rect edge.
		c.UiClipToMaxRect()
		for _, s := range segs {
			c.PaintLine(
				float32(s.X0), float32(s.Y0),
				float32(s.X1), float32(s.Y1),
				color.Hex(spec.Color), spec.Width).Send()
		}
		// The hatch canvas is keyed by its cell's seq under a scope of its
		// own, so it cannot meet a cell id.
		for range c.IdScope(inst.ids.PrepareStr("hatch")) {
			c.PaintCanvas(inst.ids.PrepareSeq(seq), float32(r.W), float32(r.H)).Send()
		}
	}
}

// paintLabelsAboveHatch re-renders a hatched cell's name (and optional value
// line) on top of its hatch: the hatch canvas paints after — and therefore
// over — the cell Frame's body, which would strike through text drawn there,
// so hatched cells suppress the in-frame labels and emit them here instead.
// The overlay allocates a plain Ui at the frame's label position (the cell
// rect inset by the frame's inner margins — mX horizontal, mY vertical) with
// no Frame, fill, or widget id, so it adds nothing to hit-testing and the
// labels land exactly where the in-frame ones would. Clipped like the cell
// body; valueGate is the caller's measured two-line threshold.
func (inst *Treemap) paintLabelsAboveHatch(node *layout.Node, r layout.Rect, textColor color.Color, rendersInner bool, mX, mY, valueGate float64) {
	for range c.AllocateUiAtRect(float32(r.X+mX), float32(r.Y+mY), float32(r.X+r.W-mX), float32(r.Y+r.H-mY)).KeepIter() {
		c.UiClipToMaxRect()
		c.LabelAtoms(c.Atoms().
			BeginRichTextColored(textColor, inst.colorTransparentBg, node.Name).
			End().Keep()).
			Truncate().Send()
		inst.paintCellValue(node, r, textColor, rendersInner, valueGate)
	}
}

// paintCellValue renders the optional secondary value label beneath a
// cell's name. It is a no-op unless a cellLabelFn is set, the cell is tall
// enough for the two-line block (r.H > minH — the caller passes
// labelMetrics.valueMinH at its Frame geometry's vertical slack, so the
// gate tracks the measured row heights), the cell is not a container
// already showing its own children (rendersInner — the value would push
// into the nested content), and fn returns a non-empty string. textColor
// matches the name so the WCAG-picked contrast against the resolved fill
// holds; Small + Weak de-emphasizes the value relative to the name. Must be
// called inside the cell's Frame body, after the name label, so the value
// flows directly below it.
func (inst *Treemap) paintCellValue(node *layout.Node, r layout.Rect, textColor color.Color, rendersInner bool, minH float64) {
	if inst.cellLabelFn == nil || rendersInner || r.H <= minH {
		return
	}
	sub := inst.cellLabelFn(node)
	if sub == "" {
		return
	}
	c.LabelAtoms(c.Atoms().
		BeginRichTextColored(textColor, inst.colorTransparentBg, sub).
		Small().Weak().End().Keep()).
		Truncate().Send()
}

// renderZoom recursively paints cells following the breadcrumb path.
func (inst *Treemap) renderZoom(node *layout.Node, bounds layout.Rect, depth, bcLevel int, cellSeq *uint64) {
	if len(node.Children) == 0 {
		return
	}

	var activeChild *layout.Node
	if bcLevel+1 < len(inst.breadcrumb) {
		activeChild = inst.breadcrumb[bcLevel+1]
	}
	atFrontier := bcLevel+1 >= len(inst.breadcrumb)

	// When filterSiblings is enabled and we have an active breadcrumb child,
	// lay out all siblings to find the active child's rect, then replace
	// node.Children so the squarified algorithm only sees the active child
	// and fills the entire available space.
	children := node.Children
	var lay *layout.Layout
	if inst.filterSiblings && activeChild != nil {
		// Temporarily swap node.Children so ComputeLayoutAt lays out
		// only the active child, filling the entire available space.
		// node.Size goes with them: a self rect would take a share of the
		// box the active child is supposed to fill entirely, which is the
		// one thing this mode exists to guarantee.
		origChildren, origSize := node.Children, node.Size
		node.Children, node.Size = []*layout.Node{activeChild}, 0
		children = node.Children
		lay = layout.ComputeLayoutAt(node, bounds)
		node.Children, node.Size = origChildren, origSize
	} else {
		lay = layout.ComputeLayoutAt(node, bounds)
		// The container's own quantity, when it has one. Painted before the
		// children so a nested recursion draws over it rather than under.
		inst.paintSelfCell(node, lay, bounds, depth, cellSeq, 6, zoomCellVSlack, 3, 2)
	}

	for _, child := range children {
		r := lay.RectOf(child)
		if r.W < 6 || r.H < 6 {
			continue
		}

		*cellSeq++
		frameCreator, cellHandle := inst.cellIds(*cellSeq)

		resp := c.CurrentApplicationState.StateManager.GetResponse(cellHandle)
		hovered := resp.HasHovered()

		isActive := child == activeChild
		hasChildren := len(child.Children) > 0

		drillable := atFrontier && hasChildren
		drillUpTo := 0
		if isActive && bcLevel+2 < len(inst.breadcrumb) {
			drillUpTo = bcLevel + 2
		}

		state := computeZoomState(isActive, atFrontier, hasChildren, drillable, drillUpTo > 0, hovered, bcLevel, len(inst.breadcrumb))
		info := CellInfo{Node: child, Depth: depth, State: state}
		visuals := inst.style.Visuals(info)
		colors, _ := inst.coloring.Colors(info)

		fill, strokeColor, textColor := inst.resolveColors(colors, visuals, state)

		inst.cells = append(inst.cells, cellDesc{
			node: child, handle: cellHandle,
			drillable: drillable, drillUpTo: drillUpTo, rect: r,
			state: state, depth: depth,
		})

		cellW := float32(r.W)
		cellH := float32(r.H)
		// A cell shows its own children inside it when it's the drilled-in
		// (active) container or a frontier container rendering a preview;
		// such cells skip the secondary value line so it doesn't overrun
		// the header into the nested content.
		rendersInner := hasChildren && (isActive || atFrontier)
		// Height gate from measured row heights: the Frame sizes to content,
		// so a label taller than the content box (cellH - zoomCellVSlack)
		// would grow it past the cell rect and paint over the neighbors
		// below (metrics.go).
		showName := r.W > 40 && r.H > inst.metrics.nameMinH(zoomCellVSlack)
		// Hatch is a StyleI decision — zero spec = no hatch. Callers who want
		// "colored cells never hatched" can wrap DefaultStyle in their own
		// StyleI that zeros Hatch when their own ColoringI applied. Hatched
		// cells defer their labels to paintLabelsAboveHatch so the lines
		// don't strike through the text.
		hatched := !visuals.Hatch.IsZero()
		for range c.AllocateUiAtRect(float32(r.X), float32(r.Y), float32(r.X+r.W), float32(r.Y+r.H)).KeepIter() {
			// Backstop to the measured gates: the cell rect is a hard paint
			// boundary even for content the gate model doesn't cover.
			c.UiClipToMaxRect()
			frame := c.Frame(frameCreator).
				Fill(fill).
				CornerRadius(visuals.CornerRadius).
				Stroke(visuals.BorderWidth, strokeColor).
				InnerMarginSides(3, 3, 2, 2)
			if drillable || drillUpTo > 0 || (inst.leafClickSensing && atFrontier) {
				frame = frame.SenseClick()
			}
			// A cell shows its own children inside it when it's the drilled-in
			// (active) container or a frontier container rendering a preview;
			// such cells skip the secondary value line so it doesn't overrun
			// the header into the nested content.
			rendersInner := hasChildren && (isActive || atFrontier)
			for range frame.KeepIter() {
				c.UiSetMinWidth(cellW - 7)
				c.UiSetMinHeight(cellH - zoomCellVSlack)

				if showName && !hatched {
					// Truncate with ellipsis when the name doesn't fit the cell
					// horizontally, rather than wrapping or overflowing into
					// neighbors. egui uses the Ui's available_width, which is
					// the frame's inner content box. Text color is WCAG-picked
					// against the resolved fill so labels stay readable across
					// arbitrary palettes.
					c.LabelAtoms(c.Atoms().
						BeginRichTextColored(textColor, inst.colorTransparentBg, child.Name).
						End().Keep()).
						Truncate().Send()
					inst.paintCellValue(child, r, textColor, rendersInner, inst.metrics.valueMinH(zoomCellVSlack))
				}
			}
		}

		if hatched {
			inst.paintHatch(r, *cellSeq, visuals.Hatch, visuals.CornerRadius)
			if showName {
				// Inner margins (3, 3, 2, 2) — keep in sync with the Frame above.
				inst.paintLabelsAboveHatch(child, r, textColor, rendersInner, 3, 2, inst.metrics.valueMinH(zoomCellVSlack))
			}
		}

		if isActive && len(child.Children) > 0 {
			// Recurse into the active child's OWN rect r. With filterSiblings
			// off, r is its sibling-weighted cell, so the active subtree
			// nests inside it and the off-path siblings keep their own space.
			// With filterSiblings on, the active child was laid out alone and
			// already fills bounds (r ≈ bounds), so the subtree still fills
			// the canvas. Recursing into the parent's full bounds instead
			// would spill the active subtree across — and behind — the
			// off-path sibling cells painted later in this loop.
			inner := innerRect(r)
			if inner.W > 8 && inner.H > 8 {
				inst.renderZoom(child, inner, depth+1, bcLevel+1, cellSeq)
			}
		} else if atFrontier && len(child.Children) > 0 {
			inner := innerRect(r)
			if inner.W > 8 && inner.H > 8 {
				inst.renderLeafChildren(child, inner, depth+1, inst.previewDepth(), cellSeq)
			}
		}
	}
}

// paintSelfCell draws the cell a container gets for its OWN size, if the layout
// reserved one (ADR-0166 §SD3). A no-op for the trees that have none, which is
// every tree whose interior nodes carry no size of their own.
//
// The cell is deliberately inert: no SenseClick, no drillable or drillUpTo, so
// it is invisible to the interaction pass. Drilling into a node from inside
// itself would have nowhere to go, and a self cell that consumed the click
// would make the container's own area a dead zone in a picture where every
// other rectangle navigates.
//
// It is still appended to inst.cells, so Events.Hovered reports the CONTAINER when
// the pointer is over its own cell — the honest answer, and the one a host
// reading a hover into a status line wants.
func (inst *Treemap) paintSelfCell(node *layout.Node, lay *layout.Layout, bounds layout.Rect, depth int, cellSeq *uint64, minPx, vSlack float64, marginX, marginY float32) {
	r := lay.SelfRectOf(node)
	if r.W < minPx || r.H < minPx {
		return
	}
	// The self rect is squarified inside the same box as the children, so a
	// bounds clamp is theirs too; this only guards the degenerate case where a
	// caller passed a box the layout could not honour.
	if r.W <= 0 || r.H <= 0 || bounds.W <= 0 || bounds.H <= 0 {
		return
	}

	*cellSeq++
	frameCreator, cellHandle := inst.cellIds(*cellSeq)

	resp := c.CurrentApplicationState.StateManager.GetResponse(cellHandle)
	state := CellStateSelf | CellStateLeaf
	if resp.HasHovered() {
		state |= CellStateHovered
	}
	info := CellInfo{Node: node, Depth: depth, State: state}
	visuals := inst.style.Visuals(info)
	colors, _ := inst.coloring.Colors(info)
	fill, strokeColor, textColor := inst.resolveColors(colors, visuals, state)

	inst.cells = append(inst.cells, cellDesc{
		node: node, handle: cellHandle, rect: r, state: state, depth: depth,
	})

	cellW := float32(r.W)
	cellH := float32(r.H)
	showName := r.W > 35 && r.H > inst.metrics.nameMinH(vSlack)
	for range c.AllocateUiAtRect(float32(r.X), float32(r.Y), float32(r.X+r.W), float32(r.Y+r.H)).KeepIter() {
		c.UiClipToMaxRect()
		frame := c.Frame(frameCreator).
			Fill(fill).
			CornerRadius(visuals.CornerRadius).
			Stroke(visuals.BorderWidth, strokeColor).
			InnerMarginSides(marginX, marginX, marginY, marginY)
		for range frame.KeepIter() {
			// Matches the sibling cells' content-box arithmetic: the frame's
			// two inner margins, less one pixel of slack.
			c.UiSetMinWidth(cellW - 2*marginX - 1)
			c.UiSetMinHeight(cellH - float32(vSlack))
			if !showName {
				continue
			}
			c.LabelAtoms(c.Atoms().
				BeginRichTextColored(textColor, inst.colorTransparentBg, node.Name).
				End().Keep()).
				Truncate().Send()
			// rendersInner is false by construction: a self cell has no children
			// to nest, which is the whole reason it exists as a separate cell.
			if inst.selfCellLabelFn != nil && r.H > inst.metrics.valueMinH(vSlack) {
				if sub := inst.selfCellLabelFn(node); sub != "" {
					c.LabelAtoms(c.Atoms().
						BeginRichTextColored(textColor, inst.colorTransparentBg, sub).
						Small().Weak().End().Keep()).
						Truncate().Send()
				}
			}
		}
	}
}

// previewDepth resolves the effective number of preview levels to render
// below the frontier: maxNestingDepth when positive, else the "show all"
// safety cap (Options.MaxNestingDepth documents NestingAll as unlimited).
func (inst *Treemap) previewDepth() (n int) {
	if inst.maxNestingDepth <= 0 {
		return maxPreviewRecursion
	}
	return inst.maxNestingDepth
}

// renderLeafChildren paints a non-interactive preview of node's descendants,
// up to `remaining` levels deep (1 = direct children only — the historic
// behavior). Deeper levels nest inside their parent's inner rect, which is
// what lets the whole subtree show at once (see Options.MaxNestingDepth). These
// cells are display-only; interactivity stays on the frontier in renderZoom.
func (inst *Treemap) renderLeafChildren(node *layout.Node, bounds layout.Rect, depth, remaining int, cellSeq *uint64) {
	if remaining <= 0 || len(node.Children) == 0 {
		return
	}
	lay := layout.ComputeLayoutAt(node, bounds)
	inst.paintSelfCell(node, lay, bounds, depth, cellSeq, 4, previewCellVSlack, 2, 1)

	for _, child := range node.Children {
		r := lay.RectOf(child)
		if r.W < 4 || r.H < 4 {
			continue
		}

		*cellSeq++
		frameCreator, cellHandle := inst.cellIds(*cellSeq)

		resp := c.CurrentApplicationState.StateManager.GetResponse(cellHandle)
		state := computePreviewState(len(child.Children) > 0, resp.HasHovered())
		info := CellInfo{Node: child, Depth: depth, State: state}
		visuals := inst.style.Visuals(info)
		colors, _ := inst.coloring.Colors(info)
		fill, strokeColor, textColor := inst.resolveColors(colors, visuals, state)

		inst.cells = append(inst.cells, cellDesc{
			node: child, handle: cellHandle, rect: r,
			state: state, depth: depth, drillable: len(child.Children) > 0,
		})

		cellW := float32(r.W)
		cellH := float32(r.H)
		// A preview cell renders its own children inside it only when
		// the nesting budget allows another level; below the budget
		// it's a terminal block, so the value line is safe to draw.
		rendersInner := remaining > 1 && len(child.Children) > 0
		// Same measured height gate as renderZoom, at the preview
		// cells' tighter vertical chrome (metrics.go).
		showName := r.W > 35 && r.H > inst.metrics.nameMinH(previewCellVSlack)
		// StyleI is responsible for deciding whether preview cells are hatched.
		// DefaultStyle returns no hatch for CellStatePreview; custom styles can
		// override that policy. As in renderZoom, hatched cells defer their
		// labels to paintLabelsAboveHatch.
		hatched := !visuals.Hatch.IsZero()
		for range c.AllocateUiAtRect(float32(r.X), float32(r.Y), float32(r.X+r.W), float32(r.Y+r.H)).KeepIter() {
			// Same paint backstop as the renderZoom cells.
			c.UiClipToMaxRect()
			frame := c.Frame(frameCreator).
				Fill(fill).
				CornerRadius(visuals.CornerRadius).
				Stroke(visuals.BorderWidth, strokeColor).
				InnerMarginSides(2, 2, 1, 1)
			if inst.leafClickSensing {
				frame = frame.SenseClick()
			}
			for range frame.KeepIter() {

				c.UiSetMinWidth(cellW - 5)
				c.UiSetMinHeight(cellH - previewCellVSlack)

				if showName && !hatched {
					c.LabelAtoms(c.Atoms().
						BeginRichTextColored(textColor, inst.colorTransparentBg, child.Name).
						End().Keep()).
						Truncate().Send()
					inst.paintCellValue(child, r, textColor, rendersInner, inst.metrics.valueMinH(previewCellVSlack))
				}
			}
		}
		if hatched {
			inst.paintHatch(r, *cellSeq, visuals.Hatch, visuals.CornerRadius)
			if showName {
				// Inner margins (2, 2, 1, 1) — keep in sync with the Frame above.
				inst.paintLabelsAboveHatch(child, r, textColor, rendersInner, 2, 1, inst.metrics.valueMinH(previewCellVSlack))
			}
		}

		// Recurse one level deeper when the nesting budget and cell size
		// allow, so a multi-level (or full) preview shows the whole subtree
		// nested at once. remaining==1 stops here — the historic behavior.
		if remaining > 1 && len(child.Children) > 0 {
			inner := innerRect(r)
			if inner.W > 8 && inner.H > 8 {
				inst.renderLeafChildren(child, inner, depth+1, remaining-1, cellSeq)
			}
		}
	}
}

// Render emits the full treemap view (breadcrumb bar + treemap area + status
// label), processes clicks and hovers, and drives the zoom animation.
// w × h is the container canvas in logical pixels; a non-positive size keeps
// the last one (700 × 450 before the first). Wraps its body in
// c.IdScope(scopeKey) so multiple instances sharing the same WidgetIdStack
// don't collide, and returns what the frame produced.
func (inst *Treemap) Render(w, h float32) (ev Events) {
	if w > 0 && h > 0 {
		inst.containerW, inst.containerH = w, h
	}
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	inst.applyOpts()
	inst.leafClicked = nil
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.renderBody()
		ev.Hovered = inst.hovered()
	}
	ev.ClickedLeaf = inst.leafClicked
	ev.Nav, inst.pendingNav = inst.pendingNav, nil
	return
}

// RenderFill is Render sized to the pane: the room left in the enclosing Ui
// last frame, less the breadcrumb bar and the status line, with fallbackW ×
// fallbackH until the probe answers.
func (inst *Treemap) RenderFill(fallbackW, fallbackH float32) (ev Events) {
	w, h := fallbackW, fallbackH
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		if pw, ph, ok := c.CapturePaneSize(inst.ids.ProbeSeq("pane")); ok && pw > 0 && ph > fillChromeH {
			w, h = pw, ph-fillChromeH
		}
	}
	return inst.Render(w, h)
}

// fillChromeH is what RenderFill holds back from the pane height for the
// breadcrumb bar above the container and the status line below it.
const fillChromeH float32 = 64

// hovered returns the node whose cell is under the pointer this frame,
// innermost first (matching the status-label readout).
func (inst *Treemap) hovered() (node *layout.Node) {
	sm := c.CurrentApplicationState.StateManager
	for i := len(inst.cells) - 1; i >= 0; i-- {
		if sm.GetResponse(inst.cells[i].handle).HasHovered() {
			return inst.cells[i].node
		}
	}
	return nil
}

func (inst *Treemap) renderBody() {
	cur := inst.Focused()

	// Keep the measured label gates current across Sync's databind reset;
	// the real row heights land one frame after the first call (metrics.go).
	inst.metrics.bindIds(inst.ids)
	inst.metrics.renewBindings()

	// --- Breadcrumb bar ---
	// Per-segment pills: ancestors are framed buttons; the tail is a
	// distinct non-interactive chip (white-bordered). Chevrons ( › ) between
	// segments signal hierarchy direction. No enclosing frame — segments
	// sit directly on the window background so their shapes read as
	// individual clickable units rather than text inside a single bar.
	for range c.Horizontal().KeepIter() {
		for level, node := range inst.breadcrumb {
			if level > 0 {
				c.LabelAtoms(c.Atoms().
					BeginRichTextColored(inst.colorBreadcrumbSep, inst.colorTransparentBg, " › ").
					End().Keep()).Send()
			}
			if level < len(inst.breadcrumb)-1 {
				if c.Button(inst.ids.PrepareSeq(uint64(level)),
					c.Atoms().BeginRichTextColored(inst.colorBreadcrumbFg, inst.colorTransparentBg, node.Name).End().Keep()).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					newPath := append([]*layout.Node(nil), inst.breadcrumb[:level+1]...)
					inst.applyNavigation(newPath, NavTriggerBreadcrumbClick)
					return
				}
			} else {
				// Tail chip: non-interactive, white-bordered so "you are here" reads at a glance.
				for range c.Frame(inst.ids.PrepareStr("bc-tail")).
					Fill(inst.colorBreadcrumbBg).
					CornerRadius(styletokens.RoundingSm).
					Stroke(styletokens.StrokeRegular, inst.colorBreadcrumbFg).
					InnerMarginSides(8, 8, 3, 3).
					KeepIter() {
					c.LabelAtoms(c.Atoms().
						BeginRichTextColored(inst.colorLeafText, inst.colorTransparentBg, node.Name).
						Strong().End().Keep()).Send()
				}
			}
		}
	}

	c.AddSpace(styletokens.PaddingInner(inst.density))

	// --- Treemap area ---
	if len(cur.Children) > 0 || len(inst.breadcrumb) > 1 {
		inst.cells = inst.cells[:0]

		// Animated render bounds: during a transition the treemap content
		// is painted inside a rect that expands from anim.FromRect() to
		// the full container. animMachine.Tick returns the effective
		// progress (always 0→1) and transitions itself to AnimStateIdle when
		// done — the renderer just consumes the value.
		renderBounds := inst.containerRect()
		if effT, running := inst.anim.Tick(); running {
			renderBounds = lerpRect(inst.anim.FromRect(), renderBounds, effT)
		}

		for range c.Frame(inst.ids.PrepareStr("container")).
			Fill(inst.colorContainerBg).
			CornerRadius(styletokens.RoundingMd).
			InnerMargin(0).
			KeepIter() {

			// Pin the container Frame to the full canvas so it doesn't shrink
			// to the painted cell bounding box. During a zoom the cells fill
			// the lerped renderBounds (a sub-rect of the container); a
			// content-sized Frame would track that shrink and make the status
			// label below — and the host's layout — jump every animation
			// frame. The fixed minimum keeps the container background and
			// downstream layout stable.
			c.UiSetMinWidth(inst.containerW)
			c.UiSetMinHeight(inst.containerH)

			var cellSeq uint64
			inst.renderZoom(inst.root, renderBounds, 0, 0, &cellSeq)
		}

		// Drive the tween every frame to keep egui's AnimationManager primed.
		animId := inst.ids.PrepareStr("anim").Derive()
		c.AnimateBoolWithTimeBind(animId, inst.anim.Target(), inst.animDurSecs, inst.anim.TPtr())

		// --- Interaction pass ---
		sm := c.CurrentApplicationState.StateManager
		hoverInfo := ""
		var drillTarget *layout.Node
		drillUpToLen := 0
		var drillUpTarget *layout.Node

		for i := len(inst.cells) - 1; i >= 0; i-- {
			resp := sm.GetResponse(inst.cells[i].handle)
			if resp.HasHovered() && hoverInfo == "" {
				hoverInfo = fmt.Sprintf("%s  |  size: %s", inst.cells[i].node.Name, formatBytes(inst.cells[i].node.TotalSize()))
			}
			if resp.HasPrimaryClicked() && drillTarget == nil && drillUpTarget == nil {
				switch {
				case inst.cells[i].drillable:
					drillTarget = inst.cells[i].node
				case inst.cells[i].drillUpTo > 0:
					drillUpTarget = inst.cells[i].node
					drillUpToLen = inst.cells[i].drillUpTo
				case inst.leafClickSensing && inst.cells[i].state.Has(CellStateFrontier) && inst.cells[i].state.Has(CellStateLeaf):
					inst.leafClicked = inst.cells[i].node
				case inst.leafClickSensing && inst.cells[i].state.Has(CellStatePreview) && inst.cells[i].state.Has(CellStateLeaf):
					inst.leafClicked = inst.cells[i].node
				}
			}
		}

		if !inst.statusLineHidden {
			if hoverInfo != "" {
				c.Label(hoverInfo).Send()
			} else {
				c.Label(fmt.Sprintf("%d items  |  total size: %s  |  hover for info, click to drill",
					len(cur.Children), formatBytes(cur.TotalSize()))).Send()
			}
		}

		switch {
		case drillTarget != nil:
			// drillTarget is a direct child of the focus (a frontier cell) or
			// a deeper preview cell (renderLeafChildren marks previews drillable
			// when leafClickSensing is on). Resolve the full root→target path
			// via findPath so the breadcrumb stays contiguous at any drill
			// depth: inserting a single ancestor by hand skipped levels for
			// previews deeper than one, and applyNavigation does not re-validate,
			// so the resulting gap would corrupt the drill state.
			if newPath := findPath(inst.root, drillTarget, nil); newPath != nil {
				inst.applyNavigation(newPath, NavTriggerCellClick)
			}
		case drillUpTarget != nil:
			newPath := append([]*layout.Node(nil), inst.breadcrumb[:drillUpToLen]...)
			inst.applyNavigation(newPath, NavTriggerDrillUpCellClick)
		}
	} else {
		c.Label(fmt.Sprintf("leaf: %s  |  size: %s", cur.Name, formatBytes(cur.TotalSize()))).Send()
		for range c.Frame(inst.ids.PrepareStr("leaf")).
			Fill(inst.colorLeafBg).
			InnerMargin(styletokens.PaddingLoose(inst.density)).
			CornerRadius(styletokens.RoundingLg).
			KeepIter() {
			c.Label(fmt.Sprintf("File: %s", cur.Name)).Send()
			c.Label(fmt.Sprintf("Size: %s", formatBytes(cur.TotalSize()))).Send()
		}
	}
}

func formatBytes(bytes float64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%.0f B", bytes)
	}
	var suffixes = []string{"KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}
	div, exp := 1024.0, 0
	for n := bytes / 1024; n >= 1024 && exp < len(suffixes)-1; n /= 1024 {
		div *= 1024
		exp++
	}
	return fmt.Sprintf("%.2f %s", bytes/div, suffixes[exp])
}
