// Package videooutput is the ADR-0088 "video output" control for the imzero2
// remote-stream pipeline. It has two parts:
//
//   - [ShowStatus] — a compact active-codec indicator for the status bar that
//     refreshes the model from the host each frame (the capability + stream
//     fetches) and toggles the settings dialog on click;
//   - [ShowDialog] — a floating settings window (rendered at the frame top
//     level) showing the stream geometry/fps and the codec picker, each codec
//     annotated with the host encode backend and the browser decode standing.
//
// The control is Go-owned state (ADR-0088 SD1/SD10): the caller holds a [State]
// across frames. Both parts render nothing when no remote viewer has reported
// capabilities (so the control is invisible under the desktop host), and
// selecting a codec drives the runtime switch via bindings.SetVideoPipeline.
package videooutput

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/videopipeline"
)

// State is the control's persistent state across frames: the pipeline model
// (capabilities + active codec + stream info) plus whether the dialog is open.
type State struct {
	model      videopipeline.Model
	dialogOpen bool
}

// ShowStatus refreshes the model from the host (the per-frame capability and
// stream-info fetches) and renders the compact active-codec indicator. Clicking
// it toggles the settings dialog. Renders nothing — and closes any open dialog
// — when no remote viewer has reported capabilities.
func ShowStatus(ids *c.WidgetIdStack, st *State) {
	st.model.Update(videopipeline.Decode(c.NewFetcher().FetchVideoCapabilities()))
	st.model.Stream = videopipeline.DecodeStreamInfo(c.NewFetcher().FetchVideoStreamInfo())
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
// model ShowStatus refreshed this frame — it does not fetch again, so call
// ShowStatus earlier in the same frame.
func ShowDialog(ids *c.WidgetIdStack, st *State) {
	if !st.dialogOpen || len(st.model.Offered()) == 0 {
		return
	}
	for range c.Window(ids.PrepareStr("videoout-win"), c.WidgetText().Text("Video output").Keep()).
		Resizable(false).Collapsible(false).TitleBar(true).DefaultWidth(340).KeepIter() {
		if s := st.model.Stream; s.Valid() {
			c.Label(fmt.Sprintf("Stream: %d×%d @ %d fps", s.Width, s.Height, s.Fps)).Send()
			c.Separator().Horizontal().Send()
		}
		c.Label("Codec — encode (host) · decode (this viewer):").Send()
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

// dialogCodecLabel describes a codec's host-encode backend and browser-decode
// standing for the dialog rows (ADR-0088 SD8 — differentiate the two HW sides
// instead of conflating them).
func dialogCodecLabel(cc videopipeline.CodecCaps) string {
	enc := "software"
	if cc.EncodeHardware {
		enc = "hardware"
	}
	dec := "unsupported"
	switch {
	case !cc.DecodeSupported:
		dec = "unsupported"
	case cc.DecodeHardware:
		dec = "hardware"
	case cc.DecodeSmooth:
		dec = "software"
	default:
		dec = "software (may stutter)"
	}
	return fmt.Sprintf("%-6s  enc %s · dec %s", cc.Codec.String(), enc, dec)
}
