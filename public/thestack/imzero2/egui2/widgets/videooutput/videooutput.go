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
		Resizable(false).Collapsible(false).TitleBar(true).DefaultWidth(560).KeepIter() {
		if s := st.model.Stream; s.Valid() {
			c.Label(fmt.Sprintf("Stream: %d×%d @ %d fps · %s", s.Width, s.Height, s.Fps, s.CadenceName())).Send()
			c.Label(fmt.Sprintf("%.1f Mbps · %d sent · %d coalesced · %d behind",
				float64(s.BitrateKbps)/1000.0, s.FramesSent, s.FramesDropped, s.FramesInFlight)).Send()
			c.Separator().Horizontal().Send()
		}
		// Codec table (egui Grid). The codec name cell is selectable — click it
		// to switch; the rest are read-only columns. "encode" is the host
		// backend, "decode" is this browser — the two are reported separately.
		for range c.Grid(ids.PrepareStr("videoout-grid")).NumColumns(6).Striped(true).KeepIter() {
			c.Label("Codec").Send()
			c.Label("Encoder").Send()
			c.Label("Encode").Send()
			c.Label("Decode").Send()
			c.Label("WebCodecs").Send()
			c.Label("Pixels").Send()
			c.EndRow()
			for _, cc := range st.model.Offered() {
				if c.SelectableLabel(ids.PrepareStr("codec-"+cc.Codec.String()), st.model.Active == cc.Codec, cc.Codec.String()).
					SendResp().HasPrimaryClicked() {
					if cc.Offerable() && st.model.Active != cc.Codec {
						st.model.Active = cc.Codec
						c.SetVideoPipeline(uint32(cc.Codec))
					}
				}
				c.Label(cc.EncoderName()).Send()
				c.Label(cc.EncodeBackend()).Send()
				c.Label(cc.DecodeBackend()).Send()
				c.Label(cc.CodecString()).Send()
				c.Label("4:2:0 8-bit").Send()
				c.EndRow()
			}
		}
		// Disabled-encoder table: lanes the host probed but cannot use. A
		// disabled hardware lane doesn't remove the codec — software still
		// serves it — so it lives in its own table rather than greyed-out above.
		// Hidden entirely when every probed lane works.
		if dis := st.model.DisabledEncoders(); len(dis) > 0 {
			c.Separator().Horizontal().Send()
			c.Label("Disabled encoders").Send()
			for range c.Grid(ids.PrepareStr("videoout-disabled-grid")).NumColumns(4).Striped(true).KeepIter() {
				c.Label("Codec").Send()
				c.Label("Encoder").Send()
				c.Label("Backend").Send()
				c.Label("Reason").Send()
				c.EndRow()
				for _, d := range dis {
					c.Label(d.Codec.String()).Send()
					c.Label(d.Encoder).Send()
					c.Label(d.Backend).Send()
					c.Label(d.Reason).Send()
					c.EndRow()
				}
			}
		}
		c.Separator().Horizontal().Send()
		if c.Button(ids.PrepareStr("videoout-close"), c.Atoms().Text("Close").Keep()).SendResp().HasPrimaryClicked() {
			st.dialogOpen = false
		}
	}
}
