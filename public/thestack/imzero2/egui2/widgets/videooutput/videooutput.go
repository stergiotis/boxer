// Package videooutput is the ADR-0088 "video output" control for the imzero2
// remote-stream pipeline. It has two parts:
//
//   - [ShowStatus] — a compact active-codec indicator for the status bar that
//     refreshes the model from the host each frame (the single capability
//     fetch) and toggles the settings dialog on click;
//   - [ShowDialog] — a floating settings window (rendered at the frame top
//     level) with the full codec picker, each codec annotated by how well the
//     connected viewer can play it.
//
// The control is Go-owned state (ADR-0088 SD1/SD10): the caller holds a [State]
// across frames. Both parts render nothing when no remote viewer has reported
// capabilities (so the control is invisible under the desktop host), and
// selecting a codec drives the runtime switch via bindings.SetVideoPipeline.
package videooutput

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/videopipeline"
)

// State is the control's persistent state across frames: the pipeline model
// (capabilities + active codec) plus whether the settings dialog is open.
type State struct {
	model      videopipeline.Model
	dialogOpen bool
}

// ShowStatus refreshes the model from the host (the single per-frame capability
// fetch) and renders the compact active-codec indicator. Clicking it toggles
// the settings dialog. Renders nothing — and closes any open dialog — when no
// remote viewer has reported capabilities.
func ShowStatus(ids *c.WidgetIdStack, st *State) {
	st.model.Update(videopipeline.Decode(c.NewFetcher().FetchVideoCapabilities()))
	if len(st.model.Offered()) == 0 {
		st.dialogOpen = false
		return
	}
	label := "codec: " + st.model.Active.String()
	if c.SelectableLabel(ids.PrepareStr("videoout-ind"), st.dialogOpen, label).SendResp().HasPrimaryClicked() {
		st.dialogOpen = !st.dialogOpen
	}
}

// ShowDialog renders the video-output settings window when open. Call it at the
// frame top level (outside the panels) so it floats over the app. It reads the
// model that ShowStatus refreshed this frame — it does not fetch again, so call
// ShowStatus earlier in the same frame.
func ShowDialog(ids *c.WidgetIdStack, st *State) {
	if !st.dialogOpen || len(st.model.Offered()) == 0 {
		return
	}
	for range c.Window(ids.PrepareStr("videoout-win"), c.WidgetText().Text("Video output").Keep()).
		Resizable(false).Collapsible(false).TitleBar(true).DefaultWidth(300).KeepIter() {
		c.Label("Stream codec — what the connected viewer plays:").Send()
		for _, cc := range st.model.Offered() {
			if c.SelectableLabel(ids.PrepareStr("codec-"+cc.Codec.String()), st.model.Active == cc.Codec, dialogCodecLabel(cc)).
				SendResp().HasPrimaryClicked() {
				if cc.Offerable() && st.model.Active != cc.Codec {
					st.model.Active = cc.Codec
					c.SetVideoPipeline(uint32(cc.Codec))
				}
			}
		}
		c.Separator().Horizontal().Send()
		if c.Button(ids.PrepareStr("videoout-close"), c.Atoms().Text("Close").Keep()).SendResp().HasPrimaryClicked() {
			st.dialogOpen = false
		}
	}
}

// dialogCodecLabel describes a codec's playability for the dialog rows — it
// surfaces the AV1-is-software / not-decodable cases rather than hiding them
// (ADR-0088 SD8).
func dialogCodecLabel(cc videopipeline.CodecCaps) string {
	name := cc.Codec.String()
	switch {
	case !cc.DecodeSupported:
		return name + " — not decodable in this viewer"
	case cc.PowerEfficient:
		return name + " — hardware-accelerated"
	case cc.Smooth:
		return name + " — software (smooth)"
	default:
		return name + " — software (may stutter)"
	}
}
