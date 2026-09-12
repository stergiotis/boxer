package graphview

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	cam "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/camera"
)

// HostCanvas names the canvas a hosted render draws inside and reads its
// input from: the two handles the host's own sense surfaces use, the canvas
// size, and the transform to draw with (ADR-0228 §SD1).
//
// Canvas is the host's PaintCanvas handle, which carries containment, hover
// and the R24 cursor row; Area is the sense region the host emits last, which
// carries clicks and drags. A host that emits only a canvas may pass the same
// handle twice.
type HostCanvas struct {
	Canvas, Area widgethandle.WidgetHandle
	W, H         float32
	Camera       cam.Camera
}

// HostClaim is what a hosted view took from the pointer this frame, read by
// the host before it handles its own gestures (ADR-0228 §SD2).
//
// Pointer is the whole arbitration rule: a gesture that starts on a node —
// or a rectangle selection, or a drag already in flight — is the guest's, and
// everything else is the host's. A host that honours it starts no drag, no
// box zoom and no click-driven zoom from this frame's pointer.
type HostClaim struct {
	Pointer bool
	// Node is the node under the pointer when there is one; HasNode says so.
	Node    uint64
	HasNode bool
}

// HostedInput reads this frame's pointer from the host's registers, applies
// everything the view takes from it — hover, clicks, selection, a node drag,
// a rectangle selection — and reports what it claimed (ADR-0228 §SD1, §SD2).
// It must run *before* the host applies its own input, which is why hosting
// is two calls: a host handles the pointer at the top of its frame and offers
// its paint slot at the bottom, so a single call could only ever claim a
// gesture the host had already acted on.
//
// The camera comes from h and is reinstalled every frame, so SetCamera,
// FitNow and FitNodes have no lasting effect while hosted; a consumer that
// wants to frame a subset moves the *host's* view instead, from Bounds and
// the host's own unprojection.
//
// The pick runs against the previous frame's geometry, which is the frame the
// pointer was actually over — a node first declared in the frame that follows
// becomes pickable one frame later than it would under Render. Read Events
// after HostedPaint, as after Render.
func (v *View) HostedInput(h HostCanvas) (claim HostClaim) {
	v.events = v.events[:0]
	v.camMoved = false
	v.hosted = true
	v.style = v.Opts.Style.withDefaults()
	v.cam = h.Camera
	v.lastW, v.lastH = h.W, h.H
	if h.W <= 0 || h.H <= 0 {
		return
	}

	sm := c.CurrentApplicationState.StateManager
	canvasFlags := sm.GetResponse(h.Canvas)
	areaFlags := sm.GetResponse(h.Area)
	wheel := sm.GetCanvasWheel(h.Canvas)
	mods := sm.GetModifiers()

	px, py, posOk := float32(0), float32(0), false
	if cur, ok := sm.GetCanvasCursor(h.Canvas); ok {
		v.originX, v.originY, v.originOk = cur.OriginX, cur.OriginY, true
		if ptr := sm.GetPointer(); ptr.Valid {
			px, py, posOk = ptr.X-cur.OriginX, ptr.Y-cur.OriginY, true
		}
	}
	if areaCur, ok := sm.GetCanvasCursor(h.Area); ok && !isNaN32(areaCur.PosX) {
		px, py, posOk = areaCur.PosX, areaCur.PosY, true
	}
	inside := posOk && canvasFlags.HasContainsPointer() && px >= 0 && py >= 0 && px <= h.W && py <= h.H

	// The claim is decided before the input is applied, since applying it may
	// end the very drag that makes this frame the guest's.
	hit := int32(-1)
	if posOk && (inside || v.drag.active) {
		hit = v.pickNode(px, py)
	}
	o := &v.Opts
	startsRect := areaFlags.HasDragStarted() && mods.Shift && o.RectSelection && o.NodeSelection && hit < 0
	claim.Pointer = hit >= 0 || startsRect || (v.drag.active && (v.drag.isNode || v.drag.isRect))
	if hit >= 0 {
		claim.Node, claim.HasNode = v.g.ids[hit], true
	}

	v.applyInput(h.W, h.H, px, py, posOk, inside, areaFlags, wheel, mods)
	return
}

// SetHostCamera refreshes the transform a hosted paint will draw with, for a
// host whose own view moved between HostedInput and its paint slot — which is
// every host that applies input at the top of its frame and offers the slot at
// the bottom. Without it the graph is painted with the camera the *pick* used,
// one frame behind what the host drew, and slides against it under a pan.
//
// Each phase wants its own camera and this is why: the pick belongs to the
// frame the pointer was over, the paint to the frame being drawn. Outside a
// hosted render it does nothing — Render owns its camera.
func (v *View) SetHostCamera(c cam.Camera) {
	if v.hosted {
		v.cam = c
	}
}

// HostedPaint reconciles the declaration, advances the layout and paints into
// the canvas that is current — the host's (ADR-0228 §SD1). It emits no
// PaintCanvas, no sense region and no background, so a graph over a host's
// own drawing is transparent by construction, and it never stamps the aura
// legend's rows, which under the host's area region could not be clicked:
// use AuraLegendExternal and paint them in a canvas the caller owns
// (ADR-0224 §SD15).
//
// Call it inside the host's paint slot, after HostedInput, with the same
// declaration Render would take.
func (v *View) HostedPaint(nodes []NodeSpec, edges []EdgeSpec) {
	w, h := v.lastW, v.lastH
	if w <= 0 || h <= 0 {
		v.hosted = false
		return
	}
	fp := v.Opts.Force.withDefaults()
	hp := v.Opts.Hier.withDefaults()
	rp := v.Opts.Radial.withDefaults()
	ap := v.Opts.Auras.withDefaults()

	topoChanged, created, n := v.reconcileAndPlace(nodes, edges, w, h, fp, hp, rp)
	if s := v.dragSlot(); s >= 0 {
		v.g.fixed[s] = true
	}
	v.stepAndPaint(w, h, fp, ap, topoChanged, created, n, false)
	v.emitAuraLegend(ap, w, h, false)
	v.hosted = false
}
