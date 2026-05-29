//go:build llm_generated_opus47

// Package capinspector renders the capability detail / schematic that
// the carousel's status-bar segments open on click. One App per
// window; the carousel pushes a selectedCap onto an internal queue
// before opening so the next-allocated App captures the right
// initial selection.
//
// Phase 1 (this package): static descriptions per cap + a live graph
// of apps ↔ cap ↔ backend, drawn via the imzero2 graph widget. The
// graph is built every frame from app.DefaultRegistry — no caching,
// so a newly-registered app appears on the next render.
package capinspector

import (
	"embed"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// capDocsFS holds the per-cap explanation files. One markdown file
// per CapId (caps/<id>.md). Parsed once at init and rendered
// in-place every frame.
//
//go:embed caps/*.md
var capDocsFS embed.FS

// capDocs is the parse-once / render-many cache of the per-cap
// markdown explanations. Populated at init from capDocsFS; a
// missing entry means the inspector falls back to spec.Description
// for that cap.
var capDocs = map[CapId]*markdown.Doc{}

func init() {
	for _, capId := range allCapIdsOrdered() {
		path := "caps/" + string(capId) + ".md"
		body, err := capDocsFS.ReadFile(path)
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("capinspector: cap doc missing")
			continue
		}
		capDocs[capId] = markdown.Parse(body)
	}
}

// ids is the package-level WidgetIdStack. Frame wraps its body in
// IdScope(seed) so widget ids are disjoint across multiple inspector
// windows.
var ids = c.NewWidgetIdStack()

// instanceCounter feeds per-instance seeds. Stamps each newApp() with
// a unique uint64.
var instanceCounter atomic.Uint64

// pendingMu guards pendingSelections — the FIFO queue of cap ids the
// status-bar clicks push into before opening an inspector. Each
// newApp() pops the head so the window opened by click N gets the
// selection set at click N.
var (
	pendingMu         sync.Mutex
	pendingSelections []CapId
)

// PushSelection enqueues a capId for the next newApp() to consume.
// The carousel calls this immediately before host.Open(ManifestId) so
// the inspector window opens already pointing at the right cap. The
// queue is FIFO so two rapid clicks open two windows in the click
// order.
func PushSelection(capId CapId) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	pendingSelections = append(pendingSelections, capId)
}

// popSelection returns the head of the queue, or "" when empty. An
// empty pop is the "user opened the inspector from the Apps menu
// without a prior status-bar click" case — Frame renders a cap
// picker instead of a detail page.
func popSelection() (capId CapId) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	if len(pendingSelections) == 0 {
		return
	}
	capId = pendingSelections[0]
	pendingSelections = pendingSelections[1:]
	return
}

// App is the per-window inspector instance. selectedCap stays
// mutable so the in-window picker can switch caps without opening
// another window.
type App struct {
	seed        uint64
	selectedCap CapId
	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at newApp.
	density styletokens.DensityE
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		seed:        instanceCounter.Add(1),
		selectedCap: popSelection(),
		density:     styletokens.DensityFromEnv(),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest)                { m = manifest; return }
func (inst *App) Mount(ctx app.MountContextI) (err error)   { return }
func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	ids.Reset()
	for range c.IdScope(ids.PrepareSeq(inst.seed)) {
		inst.renderBody()
	}
	return
}

func (inst *App) renderBody() {
	for range c.PanelTopInside(ids.PrepareStr("hdr")).Resizable(false).KeepIter() {
		inst.renderPicker()
	}
	for range c.PanelCentralInside().KeepIter() {
		spec, ok := Registry[inst.selectedCap]
		if !ok {
			c.Label("Pick a capability above.").Send()
			return
		}
		inst.renderDetail(spec)
	}
}

// renderPicker draws a row of selectable buttons across the top of
// the inspector body. Clicking one swaps selectedCap without opening
// another window; the schematic and prose below update on the same
// frame.
func (inst *App) renderPicker() {
	for range c.Horizontal().KeepIter() {
		for _, capId := range allCapIdsOrdered() {
			spec := Registry[capId]
			active := capId == inst.selectedCap
			if c.SelectableLabel(ids.PrepareStr("pick-"+string(capId)), active, spec.Display).
				SendResp().HasPrimaryClicked() {
				inst.selectedCap = capId
			}
		}
	}
}

func (inst *App) renderDetail(spec CapSpec) {
	// AutoShrink(false, false) is load-bearing — with default
	// horizontal auto-shrink the ScrollArea fits its width to its
	// widest child, which is usually our PaintCanvas. The canvas
	// then captures avail.W = its own previous width, paints at
	// avail.W - chromeW, the ScrollArea shrinks again, repeat until
	// the canvas is stuck small. Pinning the ScrollArea to its
	// parent's full width breaks that feedback loop so the canvas
	// always reads the panel's actual available width.
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		heading(spec.Display)
		// SubjectFamily and Backend are syntactic strings (NATS pattern
		// + Go import path); render the value half in monospace so the
		// dots / braces / slashes don't get eaten by the proportional
		// font's kerning.
		c.LabelAtoms(c.Atoms().
			BeginRichText("Subject family: ").End().
			BeginRichText(spec.SubjectFamily).Monospace().End().
			Keep()).Send()
		c.LabelAtoms(c.Atoms().
			BeginRichText("Backend: ").End().
			BeginRichText(spec.Backend).Monospace().End().
			Keep()).Send()
		c.AddSpace(styletokens.PaddingOuter(inst.density))
		heading("Wiring")
		inst.renderGraph(spec)
		c.AddSpace(styletokens.GapItems(inst.density))
		inst.renderLiveConsumers(spec)
		c.AddSpace(styletokens.PaddingOuter(inst.density))
		c.Separator().Horizontal().Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		inst.renderCapDoc(spec)
	}
}

// renderCapDoc draws the per-cap markdown explanation embedded in
// caps/<id>.md. Falls back to the inline CapSpec.Description when no
// file is registered for that cap. The doc is parse-once / render-
// many — `capDocs` caches the parsed shape.
func (inst *App) renderCapDoc(spec CapSpec) {
	doc, ok := capDocs[spec.Id]
	if !ok {
		c.Label(spec.Description).Send()
		return
	}
	for range c.IdScope(ids.PrepareStr("doc-" + string(spec.Id))) {
		doc.Render(ids)
	}
}

// renderGraph paints the cap-broker architecture as a single teaching
// diagram using primitive paint ops, not the egui_graphs widget.
// Topology is fixed: one prototypical [App] node in the centre, every
// shipped capability at the six vertices of a regular hexagon around
// it, and every backend on a slightly larger ring just outside its
// cap. The selected cap is highlighted; the rest stay grey. Six caps
// land on six vertices — the geometry is the spec.
//
// The canvas grows with the surrounding ScrollArea (R18 capture) and
// re-flows every frame; box sizes scale with the shorter axis so the
// layout stays legible from ~480 px up. Anti-ratchet hysteresis on
// canvas growth mirrors sccmap's containerGrowGuardPx to break the
// available-size feedback loop.
//
//	      task ── run
//	     /    \ /    \
//	 persist   App   facts
//	     \    / \    /
//	      fs ── bus
//
//	(backends sit on an outer ring just past each cap)
func (inst *App) renderGraph(spec CapSpec) {
	const (
		bgFill              uint32 = 0x161616ff
		appFill             uint32 = 0x1f3247ff
		appStroke           uint32 = 0x4488ffff
		capFillInactive     uint32 = 0x262626ff
		capStrokeInactive   uint32 = 0x707070ff
		capFillHovered      uint32 = 0x303030ff // brightened inactive — hover feedback
		capStrokeHovered    uint32 = 0xa0a0a0ff
		capFillSelected     uint32 = 0x1f3a26ff
		capStrokeSelected   uint32 = 0x44cc88ff
		backendFill         uint32 = 0x382418ff // active impl: orange-tinted
		backendStroke       uint32 = 0xff8844ff
		backendInactiveFill uint32 = 0x222222ff // alternative impl: dim grey
		backendInactiveStrk uint32 = 0x555555ff
		edgeColor           uint32 = 0x808080cc
		edgeColorDim        uint32 = 0x55555599 // edges to non-effective impls
		labelPrimary        uint32 = 0xf0f0f0ff
		labelMuted          uint32 = 0x9aa0a6ff
		// Degraded-mode badge: caps with ActiveBackend == "" get a
		// red-orange "!" badge in their top-right corner. Same palette
		// as the system "warning" intent.
		warnFill   uint32 = 0xc83020ff
		warnStroke uint32 = 0xff5040ff
		warnText   uint32 = 0xffffffff
	)
	const (
		// chromeW reserves room for the ScrollArea's vertical scrollbar
		// + its inner padding so canvasW + scrollbar fits the captured
		// available_size without provoking a horizontal scroll.
		chromeW    float32 = 24.0
		minCanvasW float32 = 460.0
		maxCanvasW float32 = 1400.0
		minCanvasH float32 = 420.0
		maxCanvasH float32 = 1100.0
		// fallback used until the first GetAvailableSize lands.
		fallbackCanvasW float32 = 820.0
		// canvasAspect keeps the diagram roughly square so the hex
		// ring has equal breathing room on both axes.
		canvasAspect float32 = 0.82
		rounding     float32 = 6.0
	)

	// Read R18 (last frame) and refresh it for next frame. NaN until
	// the first CaptureAvailableSize lands inside a Ui scope. The
	// surrounding ScrollArea uses AutoShrink(false, false), so this
	// value reflects the panel's actual width, not the canvas's own
	// previous width — no anti-ratchet hysteresis needed.
	canvasW := fallbackCanvasW
	avail := c.CurrentApplicationState.StateManager.GetAvailableSize()
	if !math.IsNaN(float64(avail.W)) && avail.W > chromeW {
		canvasW = avail.W - chromeW
	}
	canvasW = max(min(canvasW, maxCanvasW), minCanvasW)
	canvasH := max(min(canvasW*canvasAspect, maxCanvasH), minCanvasH)

	cx := canvasW * 0.5
	cy := canvasH * 0.5
	shorter := canvasW
	if canvasH < shorter {
		shorter = canvasH
	}
	// outerExtent is the half-extent of the diagram from centre to
	// the outermost box; everything is laid out within it.
	const outerMargin float32 = 32.0
	outerExtent := (shorter - outerMargin*2.0) * 0.5
	if outerExtent < 160.0 {
		outerExtent = 160.0
	}
	rBackend := outerExtent * 0.92
	rCap := outerExtent * 0.55

	// Scale box sizes with the shorter axis so they stay proportional
	// at narrow windows; clamp the scale so labels don't shrink past
	// readability or balloon past the 1100-px-tall maxCanvasH ceiling.
	sizeScale := outerExtent / 300.0
	if sizeScale < 0.72 {
		sizeScale = 0.72
	}
	if sizeScale > 1.18 {
		sizeScale = 1.18
	}
	appW := 180.0 * sizeScale
	appH := 54.0 * sizeScale
	capW := 146.0 * sizeScale
	capH := 58.0 * sizeScale
	backendW := 108.0 * sizeScale
	backendH := 34.0 * sizeScale

	capIds := allCapIdsOrdered()

	// Cap angles start at -90° (top vertex) and step clockwise by 60°,
	// putting one cap at each hex vertex.
	capAngleRad := func(i int) (rad float32) {
		rad = float32((-90.0 + 60.0*float64(i)) * math.Pi / 180.0)
		return
	}
	capCenter := func(i int) (px, py float32) {
		rad := capAngleRad(i)
		px = cx + rCap*float32(math.Cos(float64(rad)))
		py = cy + rCap*float32(math.Sin(float64(rad)))
		return
	}
	// Backends fan radially outward from their cap; for n=2 we offset
	// by a small angular jitter so the two boxes don't stack.
	backendCenter := func(i, j, n int) (px, py float32) {
		rad := capAngleRad(i)
		var jitter float32
		if n > 1 {
			const fanRad float32 = 0.22 // ~12.6° total fan
			jitter = -fanRad*0.5 + fanRad*float32(j)/float32(n-1)
		}
		radJ := rad + jitter
		px = cx + rBackend*float32(math.Cos(float64(radJ)))
		py = cy + rBackend*float32(math.Sin(float64(radJ)))
		return
	}

	// 1. Edges painted first so node fills cover the line tips. App→cap
	//    + cap→backend. Plain lines, not PaintArrow — egui's arrow tip
	//    scales as vec.length()/4 which produces oversized arrowheads
	//    on this scale; the radial layout makes direction obvious.
	for i, capId := range capIds {
		capPx, capPy := capCenter(i)
		c.PaintLine(cx, cy, capPx, capPy, color.Hex(edgeColor), 1.4).Send()
		s := Registry[capId]
		activeId := ActiveBackend(capId)
		n := len(s.Backends)
		for j, backend := range s.Backends {
			bx, by := backendCenter(i, j, n)
			col := edgeColor
			thickness := float32(1.2)
			if backend.Id != activeId {
				col = edgeColorDim
				thickness = 0.9
			}
			c.PaintLine(capPx, capPy, bx, by, color.Hex(col), thickness).Send()
		}
	}

	// 2. App node (centre).
	paintNode(cx, cy, appW, appH, rounding,
		color.Hex(appFill), color.Hex(appStroke), 1.6)
	c.PaintText(cx, cy-7.0, 1, 1, "App", 16.0,
		color.Hex(labelPrimary)).Send()
	c.PaintText(cx, cy+10.0, 1, 1, "(declares Caps in Manifest)",
		11.0, color.Hex(labelMuted)).Send()

	// Pointer state — reused for hover highlight (step 3), degraded
	// badge sizing, and click hit-testing (step 6). R14 is global so
	// the value carries a one-frame lag, which is fine for hover. NaN
	// HoverX means the canvas was never hovered this frame.
	ptr := c.CurrentApplicationState.StateManager.GetCanvasPointer()
	hoveredCapIdx := -1
	if !math.IsNaN(float64(ptr.HoverX)) {
		for i := range capIds {
			px, py := capCenter(i)
			if pointInRect(ptr.HoverX, ptr.HoverY, px-capW*0.5, py-capH*0.5, px+capW*0.5, py+capH*0.5) {
				hoveredCapIdx = i
				break
			}
		}
	}

	// 3. Cap ring — primary label + live audit count + degraded badge.
	//    Hover highlight ranks below selected (selected styling wins
	//    when the user hovers their currently-picked cap).
	for i, capId := range capIds {
		px, py := capCenter(i)
		fill := capFillInactive
		stroke := capStrokeInactive
		strokeW := float32(1.2)
		switch {
		case capId == spec.Id:
			fill = capFillSelected
			stroke = capStrokeSelected
			strokeW = 2.0
		case i == hoveredCapIdx:
			fill = capFillHovered
			stroke = capStrokeHovered
			strokeW = 1.6
		}
		paintNode(px, py, capW, capH, 5.0,
			color.Hex(fill), color.Hex(stroke), strokeW)
		c.PaintText(px, py-9.0, 1, 1, diagramCapLabel(capId),
			12.5, color.Hex(labelPrimary)).Send()
		// Rolling-window sparkline replaces the static count: each
		// bar covers sparkBucketWidth of wall-clock activity, normalized
		// against the per-cap maximum (with a floor of minScaleMax so
		// 1-count buckets don't fill the whole strip at low rates). A
		// faint baseline keeps zero-activity caps from looking broken.
		paintCapSparkline(px, py, capW, sizeScale, capId, i == hoveredCapIdx, capId == spec.Id)
		// Degraded-mode badge — top-right of the cap box. Surfacing
		// "no active impl" here saves scanning the three backend boxes
		// below; the carousel only leaves this empty when NewService
		// for the cap failed.
		if ActiveBackend(capId) == "" {
			badgeR := 8.0 * sizeScale
			bcx := px + capW*0.5 - badgeR - 2.0
			bcy := py - capH*0.5 + badgeR + 2.0
			c.PaintCircleFilled(bcx, bcy, badgeR, color.Hex(warnFill)).Send()
			c.PaintCircleStroke(bcx, bcy, badgeR, color.Hex(warnStroke), 1.0).Send()
			c.PaintText(bcx, bcy-1.0, 1, 1, "!", badgeR*1.3,
				color.Hex(warnText)).Send()
		}
	}

	// 4. Backend ring — one box per available implementation. Active
	//    impl gets the orange fill + thicker stroke; inactive ones
	//    render dim grey. When the carousel didn't set an active
	//    backend for a cap (degraded mode), every impl renders dim
	//    so the gap surfaces visually.
	for i, capId := range capIds {
		s := Registry[capId]
		activeId := ActiveBackend(capId)
		n := len(s.Backends)
		for j, backend := range s.Backends {
			bx, by := backendCenter(i, j, n)
			isActive := backend.Id == activeId
			fill := backendInactiveFill
			stroke := backendInactiveStrk
			strokeW := float32(0.8)
			labelCol := labelMuted
			if isActive {
				fill = backendFill
				stroke = backendStroke
				strokeW = 1.4
				labelCol = labelPrimary
			}
			paintNode(bx, by, backendW, backendH, 4.0,
				color.Hex(fill), color.Hex(stroke), strokeW)
			fontSize := float32(10.5)
			if n >= 2 {
				fontSize = 9.5
			}
			c.PaintText(bx, by, 1, 1, backend.Display,
				fontSize, color.Hex(labelCol)).Send()
		}
	}

	// 5. Capture this frame's available_size for next frame, then
	//    allocate the canvas. The canvas takes (canvasW, canvasH) of
	//    Ui space; PaintCanvas drains the paint ops emitted above
	//    into the allocated rect.
	c.CaptureAvailableSize()
	c.PaintCanvas(ids.PrepareStr("graph-arch"), canvasW, canvasH).
		Background(color.Hex(bgFill)).
		Sense(true, false, true).
		Send()

	// 6. Hit-test: if the user clicked inside a cap box this frame,
	//    swap selectedCap. The picker row above the diagram + this
	//    in-diagram path are equivalent; the diagram path is the
	//    direct-manipulation affordance. Reuses hoveredCapIdx computed
	//    above so click and hover share one hit-test.
	if ptr.Clicked && hoveredCapIdx >= 0 {
		inst.selectedCap = capIds[hoveredCapIdx]
	}
}

// pointInRect returns true when (px, py) falls inside the axis-aligned
// rectangle [(minX, minY), (maxX, maxY)). Half-open on the max so two
// adjacent rects don't both register a click on a shared edge.
func pointInRect(px, py, minX, minY, maxX, maxY float32) (ok bool) {
	ok = px >= minX && px < maxX && py >= minY && py < maxY
	return
}

// paintCapSparkline draws the bottom-half activity strip inside a
// cap box. The strip sits at a fixed offset below the title; bars are
// per-bucket counts normalized against the per-cap max (floored at
// minScaleMax so a single audit doesn't render as a full-height bar
// when nothing else has happened). A faint baseline anchors the strip
// when no buckets have data.
func paintCapSparkline(cxBox, cyBox, capW, sizeScale float32, capId CapId, hovered, selected bool) {
	const (
		// minScaleMax floors the normalization denominator so caps with
		// 1-2 audits don't render as if they were saturated.
		minScaleMax  uint64  = 4
		stripPaddingX float32 = 24.0 // total side padding inside cap box
		baselineCol   uint32  = 0x40404080
		minBarHeight  float32 = 1.5 // ensure 1-count buckets stay visible
	)
	stripW := capW - stripPaddingX
	stripH := max(12.0*sizeScale, 8.0)
	stripY1 := cyBox + 4.0 + stripH // baseline (bottom of strip)
	stripX0 := cxBox - stripW*0.5
	stripX1 := stripX0 + stripW

	// Baseline first so bar fills sit on top of it visually.
	c.PaintLine(stripX0, stripY1, stripX1, stripY1,
		color.Hex(baselineCol), 1.0).Send()

	snap := Tally.Snapshot(capId)
	var maxV uint64
	for _, v := range snap {
		if v > maxV {
			maxV = v
		}
	}
	if maxV == 0 {
		return // baseline-only render; nothing else to paint.
	}
	scaleMax := max(maxV, minScaleMax)
	barCol := uint32(0x8090a0d0)
	switch {
	case selected:
		barCol = 0x44cc88e0
	case hovered:
		barCol = 0xb0b0b0e0
	}
	barW := stripW / float32(sparkBuckets)
	for k, v := range snap {
		if v == 0 {
			continue
		}
		bh := max(float32(v)/float32(scaleMax)*stripH, minBarHeight)
		bx0 := stripX0 + float32(k)*barW + 0.4
		bx1 := stripX0 + float32(k+1)*barW - 0.4
		by0 := stripY1 - bh
		c.PaintRectFilled(bx0, by0, bx1, stripY1, 0.0,
			color.Hex(barCol)).Send()
	}
}

// paintNode is the rounded-rect node body + outline. Both fill and
// stroke take the same rect so the stroke renders on top — egui's
// painter respects emission order.
func paintNode(cx, cy, w, h, rounding float32, fill, stroke color.Color, strokeW float32) {
	minX := cx - w*0.5
	minY := cy - h*0.5
	maxX := cx + w*0.5
	maxY := cy + h*0.5
	c.PaintRectFilled(minX, minY, maxX, maxY, rounding, fill).Send()
	c.PaintRectStroke(minX, minY, maxX, maxY, rounding, stroke, strokeW).Send()
}

// diagramCapLabel returns the short label the painter writes inside a
// cap box. Different from spec.Display (which is the longer prose
// label rendered above the diagram) because the box has ~140px of
// usable width and Display strings overflow.
func diagramCapLabel(capId CapId) (s string) {
	switch capId {
	case CapRun:
		s = "Run identity"
	case CapFacts:
		s = "Audit + state"
	case CapBus:
		s = "Subject router"
	case CapFs:
		s = "fs.* Powerbox"
	case CapPersist:
		s = "Persist state"
	case CapTask:
		s = "Background task"
	}
	return
}


// renderLiveConsumers is a one-line footnote naming the apps in the
// current registry that exercise this cap. Kept compact on purpose
// — the inspector is about the cap-broker contract; the live
// consumer list is informational context, not the focus. Apps with
// no per-app filter (run, facts) get an explanatory line instead.
func (inst *App) renderLiveConsumers(spec CapSpec) {
	if spec.AppFilter == nil {
		c.Label("This capability is a runtime-level service; every app inherits it implicitly.").Send()
		return
	}
	apps := matchedApps(spec)
	if len(apps) == 0 {
		c.Label("Live consumers: (none — no registered app declares this cap yet).").Send()
		return
	}
	names := make([]string, 0, len(apps))
	for _, m := range apps {
		names = append(names, shortAppName(m))
	}
	c.Label("Live consumers in this registry: " + joinCommaSpace(names)).Send()
}

// joinCommaSpace joins a slice with ", " — strings.Join would do this
// too but keeping it inline saves a stdlib import for a one-call use.
func joinCommaSpace(parts []string) (s string) {
	for i, p := range parts {
		if i > 0 {
			s += ", "
		}
		s += p
	}
	return
}

// matchedApps returns every manifest in app.DefaultRegistry whose
// Caps include at least one filter matching the cap, or whose
// HostInjected pattern would fire. Sorted by AppId so the render
// order is stable across frames.
func matchedApps(spec CapSpec) (out []app.Manifest) {
	all := app.DefaultRegistry.AllManifests()
	for _, m := range all {
		hit := false
		if spec.AppFilter != nil {
			for _, f := range m.Caps {
				if spec.AppFilter(f) {
					hit = true
					break
				}
			}
		}
		if !hit && spec.HostInjected != nil {
			if spec.HostInjected(m) != "" {
				hit = true
			}
		}
		if hit {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return
}

// shortAppName returns the last "/"-separated segment of an AppId so
// node labels stay readable. "github.com/.../play" → "play".
func shortAppName(m app.Manifest) (s string) {
	id := string(m.Id)
	if i := lastSlash(id); i >= 0 {
		s = id[i+1:]
		return
	}
	s = id
	return
}

// heading emits a heading-styled label without depending on a
// top-level c.Heading factory — the bindings expose Heading() on
// RichTextScope but not as a widget shortcut, so this wrapper
// keeps the renderer code readable.
func heading(text string) {
	c.LabelAtoms(c.Atoms().RichText(text).Heading().EndRichText().Keep()).Send()
}

func lastSlash(s string) (idx int) {
	idx = -1
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			idx = i
		}
	}
	return
}
