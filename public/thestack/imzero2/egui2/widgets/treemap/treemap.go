//go:build llm_generated_opus47

// Package treemap provides an interactive Frame-based treemap widget with
// zoom-from-rect transitions on drill-in and drill-up. Cells are egui Frames
// with .SenseClick() so hover and click handling flows through egui's response
// system. The zoom tween is driven by egui::Context::animate_bool_with_time
// via the bindings.AnimateBoolWithTimeBind wrapper.
//
// The squarified layout algorithm and the tree data type (Node, Rect,
// ComputeLayoutAt) live in the sibling package treemap/layout so they can be
// used by callers that only need tile placement, without pulling in the
// egui/FFFI widget machinery.
//
// Basic usage:
//
//	var tm = treemap.New(ids, "disk-usage", scanDisk(),
//	    treemap.WithContainerSize(700, 450),
//	    treemap.WithAnimationDuration(0.28))
//	for range c.Window(...).KeepIter() {
//	    tm.Render()
//	}
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
//     Examples: WithContainerSize(0,0), New(nil ids, …), WithColoring(nil).
//
//   - Error return: caller-controlled runtime input where the caller can
//     react. Returns a documented sentinel error and leaves state unchanged.
//     Examples: NavigateTo, DrillTo.
//
//   - Log + safe default: construction-time data input with an obvious
//     recovery, where there is no caller to return to. Emits a single
//     log.Warn (Str("pkg","treemap")) and falls back to a documented default.
//     Example: WithInitialPath with a stale path → ignored, root view kept.
//
// Each option/method states its tier explicitly in its godoc.
package treemap

import (
	"fmt"
	"math"

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

	// Cell ids iterate from this seq, stepping by 2 per cell to preserve
	// the historical even-id convention. Well above bcButtonMaxLevel so the
	// two ranges don't collide within a single IdScope.
	cellSeqBase uint64 = 0xcc00ff0000
	// Breadcrumb buttons use PrepareSeq(level); keep this well below cellSeqBase.
	bcButtonMaxLevel uint64 = 1 << 20
)

// Option configures a Treemap at construction time.
type Option func(*Treemap)

// WithContainerSize sets the fixed treemap canvas size in logical pixels.
// Default 700x450. Panics if w or h is non-positive.
func WithContainerSize(w, h float32) Option {
	if w <= 0 || h <= 0 {
		panic(fmt.Sprintf("treemap: WithContainerSize requires positive w,h (got %v,%v)", w, h))
	}
	return func(t *Treemap) { t.containerW, t.containerH = w, h }
}

// WithAnimationDuration sets the zoom-transition duration in seconds.
// Default is styletokens.MotionSlowSecs() (the IDS motion ladder's slow
// rung, 320 ms; ADR-0032 §SD5). Panics if secs is negative; 0 means
// instant (no animation).
func WithAnimationDuration(secs float32) Option {
	if secs < 0 {
		panic(fmt.Sprintf("treemap: WithAnimationDuration requires secs >= 0 (got %v)", secs))
	}
	return func(t *Treemap) { t.animDurSecs = secs }
}

// WithColoring overrides the active ColoringI. Defaults to
// DepthColoring(DefaultDepthColors).
//
// Compose multiple effects with CompositeColoring:
//
//	treemap.WithColoring(treemap.CompositeColoring(
//	    treemap.DepthColoring(treemap.DefaultDepthColors),
//	    treemap.CategoricalColoring(brewerSet1, errorSeverity),
//	))
func WithColoring(coloring ColoringI) Option {
	if coloring == nil {
		panic("treemap: WithColoring requires a non-nil ColoringI")
	}
	return func(t *Treemap) { t.coloring = coloring }
}

// WithDepthColors is a backwards-compatible shortcut for
// WithColoring(DepthColoring(palette)). Prefer WithColoring directly.
//
// Deprecated: use WithColoring(DepthColoring(palette)).
func WithDepthColors(colors []uint32) Option {
	return WithColoring(DepthColoring(colors))
}

// WithInitialPath opens the widget pre-drilled along the given path from
// root. Path must begin with root (pointer-equal to the `root` argument of
// New) and walk down Children links.
//
// Validation tier: log + safe default. If the path is invalid, a single
// log.Warn is emitted (Str("pkg","treemap")) and the breadcrumb stays at
// [root]. The widget remains usable.
func WithInitialPath(path []*layout.Node) Option {
	return func(t *Treemap) {
		if !t.validPath(path) {
			log.Warn().
				Str("pkg", "treemap").
				Int("pathLen", len(path)).
				Msg("WithInitialPath: invalid path, falling back to root")
			return
		}
		t.breadcrumb = append(t.breadcrumb[:0], path...)
	}
}

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
	BorderWidth     float32 // 0 = no border
	CornerRadius    float32
	Hatch           HatchSpec // zero value = no hatch overlay
	UseDimFill      bool      // use ColoringI.DimFill instead of ColoringI.Fill
	UseHoverFill    bool      // when CellStateHovered: use ColoringI.HoverFill (overrides UseDimFill)
	UseAccentBorder bool      // use ColoringI.AccentBorder instead of ColoringI.Border
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

// WithCellColor is a backwards-compatible shortcut that composes a CategoricalColoring on top of the default depth coloring.
//
// Equivalent to:
//
//	WithColoring(CompositeColoring(
//	    DepthColoring(DefaultDepthColors),
//	    CategoricalColoring(palette, fn),
//	))
//
// Deprecated: prefer WithColoring with explicit CompositeColoring so the
// fall-through semantics (fn returning negative idx → falls back to the
// previous layer) are visible at the call site.
func WithCellColor(palette []uint32, fn CellColorFn) Option {
	return WithColoring(CompositeColoring(
		DepthColoring(DefaultDepthColors),
		CategoricalColoring(palette, fn),
	))
}

// WithStyle overrides the per-cell geometry/decoration style. Defaults to
// DefaultStyle(). Panics if s is nil.
func WithStyle(s StyleI) Option {
	if s == nil {
		panic("treemap: WithStyle requires a non-nil StyleI")
	}
	return func(t *Treemap) { t.style = s }
}

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
	ids      *c.WidgetIdStack
	scopeKey string
	root     *layout.Node

	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2). Read once at construction from
	// styletokens.DensityFromEnv() — the overlay is applied at Rust
	// startup with the same env var, so a runtime toggle here would
	// diverge from the visible state.
	density styletokens.DensityE

	// Config
	containerW  float32
	containerH  float32
	animDurSecs float32
	style       StyleI
	coloring    ColoringI

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

	// Zoom-transition animation state machine.
	anim animMachine

	// Subscribers to NavEvent. dispatching guards against re-entry from
	// inside an OnNavigate handler.
	navSubs     []func(NavEvent)
	dispatching bool
}

// New constructs a Treemap widget rooted at root. scopeKey must be unique
// among Treemap instances sharing the same id stack — it's the label passed
// to c.IdScope so the id stack isolates each instance's widget ids.
//
// Panics if ids or root is nil, or if scopeKey is the empty string.
func New(ids *c.WidgetIdStack, scopeKey string, root *layout.Node, opts ...Option) *Treemap {
	if ids == nil {
		panic("treemap: New requires a non-nil ids stack")
	}
	if scopeKey == "" {
		panic("treemap: New requires a non-empty scopeKey")
	}
	if root == nil {
		panic("treemap: New requires a non-nil root")
	}
	t := &Treemap{
		ids:         ids,
		scopeKey:    scopeKey,
		root:        root,
		density:     styletokens.DensityFromEnv(),
		breadcrumb:  []*layout.Node{root},
		containerW:  defaultContainerW,
		containerH:  defaultContainerH,
		animDurSecs: styletokens.MotionSlowSecs(),
		style:       DefaultStyle(),
		coloring:    DepthColoring(DefaultDepthColors),
	}

	// Chrome (breadcrumb / frame / container / leaf surfaces) sources from
	// the IDS neutral spine (ADR-0031 §SD4). Per-cell *content* coloring
	// still flows through the ColoringI strategy (DepthColoring,
	// CategoricalColoring, …) so the treemap stays palette-pluggable.
	t.colorBreadcrumbBg = color.Hex(styletokens.NeutralBgSurface.AsHex()).Keep()
	t.colorFrameStroke = color.Hex(styletokens.NeutralBorderFaint.AsHex()).Keep()
	t.colorBreadcrumbFg = color.Hex(styletokens.NeutralTextExtreme.AsHex()).Keep()
	t.colorBreadcrumbSep = color.Hex(styletokens.NeutralTextDisabled.AsHex()).Keep()
	t.colorTransparentBg = color.Transparent.Keep()
	t.colorLeafText = color.Hex(styletokens.NeutralTextPrimary.AsHex()).Keep()
	t.colorContainerBg = color.Hex(styletokens.NeutralBgPanel.AsHex()).Keep()
	t.colorLeafBg = color.Hex(styletokens.NeutralBgSurface.AsHex()).Keep()

	for _, opt := range opts {
		opt(t)
	}
	return t
}

// SetContainerSize updates the treemap canvas size after construction.
// Mirrors WithContainerSize but lets callers resize each frame — e.g.,
// to fill ui.available_size via a captureAvailableSize fetcher. Panics
// on non-positive w or h, matching WithContainerSize's contract.
func (t *Treemap) SetContainerSize(w, h float32) {
	if w <= 0 || h <= 0 {
		panic(fmt.Sprintf("treemap: SetContainerSize requires positive w,h (got %v,%v)", w, h))
	}
	t.containerW, t.containerH = w, h
}

// Focused returns the current tail of the breadcrumb.
func (t *Treemap) Focused() *layout.Node { return t.breadcrumb[len(t.breadcrumb)-1] }

// Depth returns how deep the user has drilled (0 = root).
func (t *Treemap) Depth() int { return len(t.breadcrumb) - 1 }

// Breadcrumb returns a copy of the path from root to the current focus.
func (t *Treemap) Breadcrumb() []*layout.Node {
	out := make([]*layout.Node, len(t.breadcrumb))
	copy(out, t.breadcrumb)
	return out
}

// resolveColors picks the appropriate fill, border, and text color from
// colors using the StyleI's role selectors and the cell's state. The text
// color tracks whichever fill slot is selected so WCAG-picked contrast
// stays consistent under dim/hover transitions. Kept as a method so test
// code can exercise it with arbitrary (colors, visuals, state) triples.
func (t *Treemap) resolveColors(colors CellColors, visuals CellVisuals, state CellStateE) (fill, border, text color.Color) {
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
	if visuals.UseAccentBorder {
		border = colors.AccentBorder
	}
	return
}

func (t *Treemap) containerRect() layout.Rect {
	return layout.Rect{W: float64(t.containerW), H: float64(t.containerH)}
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

// startZoom triggers a zoom-from-rect animation by delegating to animMachine.
// Kept as a thin wrapper so Render's call sites stay readable.
func (t *Treemap) startZoom(fromRect layout.Rect) { t.anim.Start(fromRect) }

// cellIds derives matching ids for a cell's Frame and its response handle.
// Two PrepareSeq+Derive cycles with the same seq produce the same scoped id
// under the active IdScope, so the Frame's server-side id equals cellHandle's.
func (t *Treemap) cellIds(seq uint64) (frameCreator c.WidgetIdCreatorI, handle widgethandle.WidgetHandle) {
	id := t.ids.PrepareSeq(seq).Derive()
	return c.AbsoluteWidgetId(id), widgethandle.Make(c.AbsoluteWidgetId(id).Derive())
}

// paintHatch draws a diagonal line pattern at `r` per spec, marking the cell
// as non-interactive. Painter primitives queue into paint_cmds on the Rust
// side and only render when a PaintCanvas drains them, so we allocate a
// sibling Ui at the cell's rect and emit a no-background PaintCanvas to
// flush the lines on top of the previously-drawn cell Frame. seq must be
// unique per hatch instance within the enclosing IdScope.
//
// Currently only ±45° angles are supported; other AngleDeg values fall back
// to -45°. (Generalizing to arbitrary angles is straightforward but unused.)
func (t *Treemap) paintHatch(r layout.Rect, seq uint64, spec HatchSpec) {
	if spec.IsZero() {
		return
	}
	spacing := float64(spec.Spacing)
	for range c.AllocateUiAtRect(float32(r.X), float32(r.Y), float32(r.X+r.W), float32(r.Y+r.H)).KeepIter() {
		// Lines parameterized by cc = x + y (slope -1). Positive AngleDeg
		// reflects horizontally to slope +1 (cc = y - x + H).
		positive := spec.AngleDeg > 0
		for cc := 0.0; cc <= r.W+r.H; cc += spacing {
			var x0, y0, x1, y1 float64
			if positive {
				// slope +1: y = x + (cc - H), parameterize same as before
				// but mirror Y.
				x0 = math.Max(0, cc-r.H)
				y0 = r.H - math.Min(r.H, cc)
				x1 = math.Min(r.W, cc)
				y1 = r.H - math.Max(0, cc-r.W)
			} else {
				x0 = math.Max(0, cc-r.H)
				y0 = math.Min(r.H, cc)
				x1 = math.Min(r.W, cc)
				y1 = math.Max(0, cc-r.W)
			}
			if math.Abs(x1-x0) < 0.5 && math.Abs(y1-y0) < 0.5 {
				continue
			}
			c.PaintLine(
				float32(x0), float32(y0),
				float32(x1), float32(y1),
				color.Hex(spec.Color), spec.Width).Send()
		}
		// Cell seqs are even (stepped by 2); |1 gives a collision-free odd seq
		// for the hatch canvas id.
		c.PaintCanvas(t.ids.PrepareSeq(seq|1), float32(r.W), float32(r.H)).Send()
	}
}

// renderZoom recursively paints cells following the breadcrumb path.
func (t *Treemap) renderZoom(node *layout.Node, bounds layout.Rect, depth, bcLevel int, cellSeq *uint64) {
	if len(node.Children) == 0 {
		return
	}
	lay := layout.ComputeLayoutAt(node, bounds)

	var activeChild *layout.Node
	if bcLevel+1 < len(t.breadcrumb) {
		activeChild = t.breadcrumb[bcLevel+1]
	}
	atFrontier := bcLevel+1 >= len(t.breadcrumb)

	for _, child := range node.Children {
		r := lay.RectOf(child)
		if r.W < 6 || r.H < 6 {
			continue
		}

		*cellSeq += 2
		frameCreator, cellHandle := t.cellIds(*cellSeq)

		resp := c.CurrentApplicationState.StateManager.GetResponse(cellHandle)
		hovered := resp.HasHovered()

		isActive := child == activeChild
		hasChildren := len(child.Children) > 0

		drillable := atFrontier && hasChildren
		drillUpTo := 0
		if isActive && bcLevel+2 < len(t.breadcrumb) {
			drillUpTo = bcLevel + 2
		}

		state := computeZoomState(isActive, atFrontier, hasChildren, drillable, drillUpTo > 0, hovered, bcLevel, len(t.breadcrumb))
		info := CellInfo{Node: child, Depth: depth, State: state}
		visuals := t.style.Visuals(info)
		colors, _ := t.coloring.Colors(info)

		fill, strokeColor, textColor := t.resolveColors(colors, visuals, state)

		t.cells = append(t.cells, cellDesc{
			node: child, handle: cellHandle,
			drillable: drillable, drillUpTo: drillUpTo, rect: r,
			state: state, depth: depth,
		})

		cellW := float32(r.W)
		cellH := float32(r.H)
		for range c.AllocateUiAtRect(float32(r.X), float32(r.Y), float32(r.X+r.W), float32(r.Y+r.H)).KeepIter() {
			frame := c.Frame(frameCreator).
				Fill(fill).
				CornerRadius(visuals.CornerRadius).
				Stroke(visuals.BorderWidth, strokeColor).
				InnerMarginSides(3, 3, 2, 2)
			if drillable || drillUpTo > 0 {
				frame = frame.SenseClick()
			}
			for range frame.KeepIter() {
				c.UiSetMinWidth(cellW - 7)
				c.UiSetMinHeight(cellH - 5)

				if r.W > 40 && r.H > 18 {
					// Truncate with ellipsis when the name doesn't fit the cell
					// horizontally, rather than wrapping or overflowing into
					// neighbors. egui uses the Ui's available_width, which is
					// the frame's inner content box. Text color is WCAG-picked
					// against the resolved fill so labels stay readable across
					// arbitrary palettes.
					c.LabelAtoms(c.Atoms().
						BeginRichTextColored(textColor, t.colorTransparentBg, child.Name).
						End().Keep()).
						Truncate().Send()
				}
			}
		}

		// Hatch is a StyleI decision — zero spec = no hatch. Callers who want
		// "colored cells never hatched" can wrap DefaultStyle in their own
		// StyleI that zeros Hatch when their own ColoringI applied.
		if !visuals.Hatch.IsZero() {
			t.paintHatch(r, *cellSeq, visuals.Hatch)
		}

		if isActive && len(child.Children) > 0 {
			inner := innerRect(r)
			if inner.W > 8 && inner.H > 8 {
				t.renderZoom(child, inner, depth+1, bcLevel+1, cellSeq)
			}
		} else if atFrontier && len(child.Children) > 0 {
			inner := innerRect(r)
			if inner.W > 8 && inner.H > 8 {
				t.renderLeafChildren(child, inner, depth+1, cellSeq)
			}
		}
	}
}

// renderLeafChildren paints a non-interactive one-level preview.
func (t *Treemap) renderLeafChildren(node *layout.Node, bounds layout.Rect, depth int, cellSeq *uint64) {
	if len(node.Children) == 0 {
		return
	}
	lay := layout.ComputeLayoutAt(node, bounds)

	for _, child := range node.Children {
		r := lay.RectOf(child)
		if r.W < 4 || r.H < 4 {
			continue
		}

		*cellSeq += 2
		frameCreator, cellHandle := t.cellIds(*cellSeq)

		resp := c.CurrentApplicationState.StateManager.GetResponse(cellHandle)
		state := computePreviewState(len(child.Children) > 0, resp.HasHovered())
		info := CellInfo{Node: child, Depth: depth, State: state}
		visuals := t.style.Visuals(info)
		colors, _ := t.coloring.Colors(info)
		fill, strokeColor, textColor := t.resolveColors(colors, visuals, state)

		t.cells = append(t.cells, cellDesc{
			node: child, handle: cellHandle, rect: r,
			state: state, depth: depth,
		})

		cellW := float32(r.W)
		cellH := float32(r.H)
		for range c.AllocateUiAtRect(float32(r.X), float32(r.Y), float32(r.X+r.W), float32(r.Y+r.H)).KeepIter() {
			for range c.Frame(frameCreator).
				Fill(fill).
				CornerRadius(visuals.CornerRadius).
				Stroke(visuals.BorderWidth, strokeColor).
				InnerMarginSides(2, 2, 1, 1).
				KeepIter() {

				c.UiSetMinWidth(cellW - 5)
				c.UiSetMinHeight(cellH - 3)

				if r.W > 35 && r.H > 14 {
					c.LabelAtoms(c.Atoms().
						BeginRichTextColored(textColor, t.colorTransparentBg, child.Name).
						End().Keep()).
						Truncate().Send()
				}
			}
		}
		// StyleI is responsible for deciding whether preview cells are hatched.
		// DefaultStyle returns no hatch for CellStatePreview; custom styles can
		// override that policy.
		if !visuals.Hatch.IsZero() {
			t.paintHatch(r, *cellSeq, visuals.Hatch)
		}
	}
}

// Render emits the full treemap view (breadcrumb bar + treemap area + status
// label), processes clicks and hovers, and drives the zoom animation.
// Wraps its body in c.IdScope(scopeKey) so multiple instances sharing the
// same WidgetIdStack don't collide.
func (t *Treemap) Render() {
	for range c.IdScope(t.ids.PrepareStr(t.scopeKey)) {
		t.renderBody()
	}
}

func (t *Treemap) renderBody() {
	cur := t.Focused()

	// --- Breadcrumb bar ---
	// Per-segment pills: ancestors are framed buttons; the tail is a
	// distinct non-interactive chip (white-bordered). Chevrons ( › ) between
	// segments signal hierarchy direction. No enclosing frame — segments
	// sit directly on the window background so their shapes read as
	// individual clickable units rather than text inside a single bar.
	for range c.Horizontal().KeepIter() {
		for level, node := range t.breadcrumb {
			if level > 0 {
				c.LabelAtoms(c.Atoms().
					BeginRichTextColored(t.colorBreadcrumbSep, t.colorTransparentBg, " › ").
					End().Keep()).Send()
			}
			if level < len(t.breadcrumb)-1 {
				if c.Button(t.ids.PrepareSeq(uint64(level)),
					c.Atoms().BeginRichTextColored(t.colorBreadcrumbFg, t.colorTransparentBg, node.Name).End().Keep()).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					newPath := append([]*layout.Node(nil), t.breadcrumb[:level+1]...)
					t.applyNavigation(newPath, NavTriggerBreadcrumbClick)
					return
				}
			} else {
				// Tail chip: non-interactive, white-bordered so "you are here" reads at a glance.
				for range c.Frame(t.ids.PrepareStr("bc-tail")).
					Fill(t.colorBreadcrumbBg).
					CornerRadius(styletokens.RoundingSm).
					Stroke(styletokens.StrokeRegular, t.colorBreadcrumbFg).
					InnerMarginSides(8, 8, 3, 3).
					KeepIter() {
					c.LabelAtoms(c.Atoms().
						BeginRichTextColored(t.colorLeafText, t.colorTransparentBg, node.Name).
						Strong().End().Keep()).Send()
				}
			}
		}
	}

	c.AddSpace(styletokens.PaddingInner(t.density))

	// --- Treemap area ---
	if len(cur.Children) > 0 || len(t.breadcrumb) > 1 {
		t.cells = t.cells[:0]

		// Animated render bounds: during a transition the treemap content
		// is painted inside a rect that expands from anim.FromRect() to
		// the full container. animMachine.Tick returns the effective
		// progress (always 0→1) and transitions itself to AnimStateIdle when
		// done — the renderer just consumes the value.
		renderBounds := t.containerRect()
		if effT, running := t.anim.Tick(); running {
			renderBounds = lerpRect(t.anim.FromRect(), renderBounds, effT)
		}

		for range c.Frame(t.ids.PrepareStr("container")).
			Fill(t.colorContainerBg).
			CornerRadius(styletokens.RoundingMd).
			InnerMargin(0).
			KeepIter() {

			c.UiSetMinWidth(t.containerW)
			c.UiSetMinHeight(t.containerH)

			cellSeq := cellSeqBase
			t.renderZoom(t.root, renderBounds, 0, 0, &cellSeq)
		}

		// Drive the tween every frame to keep egui's AnimationManager primed.
		animId := t.ids.PrepareStr("anim").Derive()
		c.AnimateBoolWithTimeBind(animId, t.anim.Target(), t.animDurSecs, t.anim.TPtr())

		// --- Interaction pass ---
		sm := c.CurrentApplicationState.StateManager
		hoverInfo := ""
		var drillTarget *layout.Node
		drillUpToLen := 0
		var drillUpTarget *layout.Node

		for i := len(t.cells) - 1; i >= 0; i-- {
			resp := sm.GetResponse(t.cells[i].handle)
			if resp.HasHovered() && hoverInfo == "" {
				hoverInfo = fmt.Sprintf("%s  |  size: %.0f", t.cells[i].node.Name, t.cells[i].node.TotalSize())
			}
			if resp.HasPrimaryClicked() && drillTarget == nil && drillUpTarget == nil {
				switch {
				case t.cells[i].drillable:
					drillTarget = t.cells[i].node
				case t.cells[i].drillUpTo > 0:
					drillUpTarget = t.cells[i].node
					drillUpToLen = t.cells[i].drillUpTo
				}
			}
		}

		if hoverInfo != "" {
			c.Label(hoverInfo).Send()
		} else {
			c.Label(fmt.Sprintf("%d items  |  total size: %.0f  |  hover for info, click to drill",
				len(cur.Children), cur.TotalSize())).Send()
		}

		switch {
		case drillTarget != nil:
			newPath := append([]*layout.Node(nil), t.breadcrumb...)
			newPath = append(newPath, drillTarget)
			t.applyNavigation(newPath, NavTriggerCellClick)
		case drillUpTarget != nil:
			newPath := append([]*layout.Node(nil), t.breadcrumb[:drillUpToLen]...)
			t.applyNavigation(newPath, NavTriggerDrillUpCellClick)
		}
	} else {
		c.Label(fmt.Sprintf("leaf: %s  |  size: %.0f", cur.Name, cur.TotalSize())).Send()
		for range c.Frame(t.ids.PrepareStr("leaf")).
			Fill(t.colorLeafBg).
			InnerMargin(styletokens.PaddingLoose(t.density)).
			CornerRadius(styletokens.RoundingLg).
			KeepIter() {
			c.Label(fmt.Sprintf("File: %s", cur.Name)).Send()
			c.Label(fmt.Sprintf("Size: %.0f bytes", cur.TotalSize())).Send()
		}
	}
}
