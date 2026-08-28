package waveform

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

const (
	minimapCanvasKey = "waveform-minimap"
	minimapAreaKey   = "waveform-minimap-area"
)

// RenderMinimap draws the whole track w×h at the current position of the
// enclosing Ui with the player's view as a window over it (ADR-0208 SD10):
// the pyramid's coarse levels drawn like the main canvas, the played part
// in the progress colour, a translucent rectangle where the view is. A click
// centres the view on the clicked time; a drag moves the window by the
// pointer's offset. It is its own canvas with its own sense region, so it
// never fights the main canvas's gestures.
func (inst *Player) RenderMinimap(w, h float32) {
	for range c.IdScope(inst.ids.PrepareStr(inst.opts.ScopeKey)) {
		inst.minimapFrame(w, h)
	}
}

func (inst *Player) minimapFrame(w, h float32) {
	sm := c.CurrentApplicationState.StateManager
	frames := inst.tr.Frames()
	if w < 1 || h < 1 || frames <= 0 || !inst.viewInit || inst.width < 1 {
		return
	}
	canvasH := widgethandle.Make(inst.ids.PrepareStr(minimapCanvasKey).Derive())
	areaH := widgethandle.Make(inst.ids.PrepareStr(minimapAreaKey).Derive())
	cur, live := sm.GetCanvasCursor(canvasH)
	flags := sm.GetResponse(areaH)
	areaCur, areaOk := sm.GetCanvasCursor(areaH)
	ptr := sm.GetPointer()
	fpp := float64(frames) / float64(w) // frames per minimap pixel

	if live {
		posX, posOk := cur.PosX, !isNaN32(cur.PosX)
		if ptr.Valid && !isNaN32(ptr.X) && !isNaN32(cur.OriginX) {
			posX, posOk = ptr.X-cur.OriginX, true
		}
		span := float64(inst.width) * inst.view.FramesPerPx
		if flags.HasDragStarted() && posOk {
			origin := posX
			if areaOk && !isNaN32(areaCur.PosX) {
				origin = areaCur.PosX
			}
			inst.miniDragging = true
			inst.miniOriginX = origin
			inst.miniDragFrom = inst.view.FromFrame
			inst.focusKeys()
		}
		if inst.miniDragging && (flags.HasDragged() || flags.HasDragStopped()) && posOk {
			v := inst.view
			v.FromFrame = inst.miniDragFrom + float64(posX-inst.miniOriginX)*fpp
			inst.view = v.clamp(frames, inst.width)
		}
		if flags.HasDragStopped() {
			inst.miniDragging = false
		}
		if posOk && flags.HasPrimaryClicked() {
			v := inst.view
			v.FromFrame = float64(posX)*fpp - span/2
			inst.view = v.clamp(frames, inst.width)
			inst.focusKeys()
		}
	}

	// ---- draw ----
	c.PaintClipPush(0, 0, w, h).Send()
	channels := int(inst.tr.Format().Channels)
	bands := channels
	if inst.opts.OverlayChannels {
		bands = 1
	}
	bandH := h / float32(max(bands, 1))
	cols := int(w)
	inst.growScratch(cols)
	pos := inst.Position()
	saved := inst.view
	// Columns are emitted through the main path with a temporary view over
	// the whole track, so the played/unplayed split lands where it does above.
	inst.view = View{FromFrame: 0, FramesPerPx: fpp}
	for ch := range channels {
		band := ch
		if inst.opts.OverlayChannels {
			band = 0
		}
		top := float32(band) * bandH
		inst.emitPyramid(0, frames, ch, cols, 1, 1, top+bandH/2, bandH/2, 1, top, top+bandH, pos, w)
	}
	inst.view = saved

	x0 := float32(saved.FromFrame / fpp)
	x1 := float32(saved.ToFrame(inst.width) / fpp)
	if x1-x0 < 2 {
		x1 = x0 + 2
	}
	c.PaintRectFilled(x0, 0, x1, h, 0, withAlpha(inst.vis.HoverText, 0x30)).Send()
	c.PaintRectStroke(x0, 0, x1, h, 0, inst.vis.HoverText, styletokens.StrokeHair).Send()
	px := float32(float64(pos) / fpp)
	c.PaintLine(px, 0, px, h, inst.vis.Playhead, styletokens.StrokeHair).Send()
	c.PaintClipPop().Send()

	c.PaintSenseRegion(inst.ids.PrepareStr(minimapAreaKey), 0, 0, w, h).Send()
	c.PaintCanvas(inst.ids.PrepareStr(minimapCanvasKey), w, h).
		Background(inst.vis.Background).
		Sense(true, false, true).
		Send()
	if inst.miniDragging {
		c.RequestRepaintAfter(1.0 / 60)
	}
}
