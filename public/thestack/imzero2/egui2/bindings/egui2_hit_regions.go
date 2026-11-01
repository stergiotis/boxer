package bindings

// Hit regions on painted marks (ADR-0149, update of 2026-11-01). Code that
// paints a mark declares the mark's geometry beside it, under an id, with a
// label and a tooltip; the canvas hit-tests the regions against the pointer
// itself and shows the winner's tooltip at the pointer, in egui's tooltip
// layer — above other content and not clipped by the canvas.
//
//	c.PaintCircleFilled(x, y, r, col).Send()
//	if c.PaintHitCircle(ids.PrepareSeq(uint64(i)), x, y, r).
//		Label(name).Tooltip("Loss about €2.1m").
//		SendResp().HasHovered() {
//		// last frame's hover; see SendResp
//	}
//
// A region must be emitted before the PaintCanvas that drains it, like any
// paint command. Of the regions under the pointer the one emitted last wins.
// A labelled region carries an accessibility node named by its label, and
// Focusable makes it a Tab stop that shows its tooltip while focused.

// SendResp sends the region and returns its response flags from the previous
// frame: HasHovered for the hover winner, HasPrimaryClicked for a click on
// it (or Enter/Space while focused), HasFocus when focused. The id is the
// read-back key, so it must be stable across frames.
func (inst PaintHitRectFluid) SendResp() ResponseFlagsE {
	inst.Send()
	return CurrentApplicationState.StateManager.GetResponseByIdRaw(inst.id)
}

// SendResp: see [PaintHitRectFluid.SendResp].
func (inst PaintHitCircleFluid) SendResp() ResponseFlagsE {
	inst.Send()
	return CurrentApplicationState.StateManager.GetResponseByIdRaw(inst.id)
}

// SendResp: see [PaintHitRectFluid.SendResp].
func (inst PaintHitPolygonFluid) SendResp() ResponseFlagsE {
	inst.Send()
	return CurrentApplicationState.StateManager.GetResponseByIdRaw(inst.id)
}

// TooltipUi captures body as a rich tooltip body, drawn under the label and
// the Tooltip text. body emits ordinary widgets; it is captured every frame
// and replayed only while the tooltip shows.
func (inst PaintHitRectFluid) TooltipUi(body func()) PaintHitRectFluid {
	inst.BeginTip(0)
	body()
	return inst.EndTip()
}

// TooltipUi: see [PaintHitRectFluid.TooltipUi].
func (inst PaintHitCircleFluid) TooltipUi(body func()) PaintHitCircleFluid {
	inst.BeginTip(0)
	body()
	return inst.EndTip()
}

// TooltipUi: see [PaintHitRectFluid.TooltipUi].
func (inst PaintHitPolygonFluid) TooltipUi(body func()) PaintHitPolygonFluid {
	inst.BeginTip(0)
	body()
	return inst.EndTip()
}
