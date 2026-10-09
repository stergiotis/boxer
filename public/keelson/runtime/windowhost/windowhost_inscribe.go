package windowhost

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/inscribe"
)

// The mark overlay (ADR-0297): a component of the window host,
// framed once per frame after every window, the shell chrome and the host's
// dialogs, outside every window's capture span.

// Marks is the scene of the overlay: agents' marks, written
// through the agent service, cleared by the person from the Window menu.
func (inst *Inst) Marks() *inscribe.Scene { return inst.overlay.Scene() }

// FrameOverlay draws the marks. hostModal is whether a host dialog
// framed outside the window host — the Powerbox file dialog — is open; the
// agent chrome's modals the host knows itself. Either hides the overlay
// (ADR-0297 §SD8). Call it once per frame, after Frame and every other host
// dialog. Render goroutine only.
func (inst *Inst) FrameOverlay(hostModal bool) {
	if inst.overlay.Scene().Len() == 0 {
		return
	}
	inst.mu.Lock()
	geoms := make([]GeomEntry, 0, len(inst.windows))
	for _, w := range inst.windows {
		if w.closeReq {
			continue
		}
		geoms = append(geoms, GeomEntry{Key: w.key, Geom: w.geom})
	}
	d := inst.desktop
	inst.mu.Unlock()
	bounds := inscribe.Rect{X: 0, Y: 0, W: 1 << 14, H: 1 << 14}
	if d.WorkShown {
		bounds = inscribe.Rect{X: d.Work.MinX, Y: d.Work.MinY, W: d.Work.W(), H: d.Work.H()}
	}
	inst.overlay.Frame(geomResolver(geoms), bounds, hostModal || inst.dialogModal)
}
