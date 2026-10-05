// Package videooutput is the ADR-0088 "video output" control for the imzero2
// remote-stream pipeline, an immediate-mode widget (ADR-0267) in two parts:
//
//   - [RenderStatus] — a compact active-codec indicator for the status bar
//     that refreshes the model from the host each frame (the capability +
//     stream fetches) and toggles the settings dialog on click;
//   - [RenderDialog] — a floating settings window (rendered at the frame top
//     level) showing the stream geometry/fps and the codec picker, each codec
//     annotated with the host encode backend and the browser decode standing.
//
// The control is Go-owned state (ADR-0088 SD1/SD10): the caller holds a [State]
// across frames and passes it on [Input]. Both parts render nothing when no remote viewer has reported
// capabilities (so the control is invisible under the desktop host), and
// selecting a codec drives the runtime switch via bindings.SetVideoPipeline.
package videooutput

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/videopipeline"
)

// State is the control's persistent state across frames: the pipeline model
// (capabilities + active codec + stream info) plus whether the dialog is open.
// The zero value is usable.
type State struct {
	model      videopipeline.Model
	dialogOpen bool
}

// Input places the control.
type Input struct {
	// Ids is the host's widget id stack; every part opens one IdScope under
	// it. ScopeKey names the control within it; empty uses "videooutput".
	Ids      *c.WidgetIdStack
	ScopeKey string
	// State is the host-owned model and dialog flag. Required.
	State *State
}

// Result is what one Render* reports.
type Result struct {
	// DialogOpen is whether the settings dialog is open after this frame.
	DialogOpen bool
	// Err is a host programming error — a nil Ids or State.
	Err error
}

func (in Input) scopeKey() string {
	if in.ScopeKey == "" {
		return "videooutput"
	}
	return in.ScopeKey
}

func (in Input) check() (res Result, ok bool) {
	if in.Ids == nil || in.State == nil {
		res.Err = eh.Errorf("videooutput: Input.Ids and Input.State are required")
		return res, false
	}
	return res, true
}

// RenderStatus refreshes the model from the host (the per-frame capability
// and stream-info fetches) and renders the compact active-codec indicator.
// Clicking it toggles the settings dialog. Renders nothing — and closes any
// open dialog — when no remote viewer has reported capabilities.
func RenderStatus(in Input) (res Result) {
	res, ok := in.check()
	if !ok {
		return
	}
	for range c.IdScope(in.Ids.PrepareStr(in.scopeKey())) {
		in.renderStatus()
	}
	res.DialogOpen = in.State.dialogOpen
	return
}

func (in Input) renderStatus() {
	ids, st := in.Ids, in.State
	st.model.Update(videopipeline.Decode(c.NewFetcher().FetchVideoCapabilities()))
	st.model.Stream = videopipeline.DecodeStreamInfo(c.NewFetcher().FetchVideoStreamInfo())
	if len(st.model.Offered()) == 0 {
		st.dialogOpen = false
		return
	}
	// The indicator names the lane the host serves, not the one this side
	// last asked for: after a host-side degradation (no usable encoder → mesh)
	// the two differ, and the label used to keep the stale request.
	if st.model.Stream.Valid() && st.model.Stream.CodecKnown {
		st.model.Active = st.model.Stream.Codec
	}
	label := "codec: " + st.model.Active.String()
	if c.SelectableLabel(ids.PrepareStr("indicator"), st.dialogOpen, label).SendResp().HasPrimaryClicked() {
		st.dialogOpen = !st.dialogOpen
	}
}

// HostReactive reports the host's *live* render cadence as last refreshed by
// [RenderStatus] from the remote stream: reactive is true when a stream is live
// and the host is in reactive cadence. ok is false when no stream is live (the
// desktop host, or before a viewer connects) — the caller then keeps its own
// default.
//
// The headless host's cadence is runtime-switchable from the viewer (its
// toggle sends SetCadence over the wire), whereas the launch-time
// IMZERO2_RENDER_CADENCE the Go renderer captured at startup is not. The host
// echoes its active cadence back in the fetchVideoStreamInfo telemetry; the
// carousel's decorateRenderer reads it here so a viewer-driven switch reaches
// the Go side too. Otherwise Go keeps emitting an immediate RequestRepaint
// every frame, and because egui takes the soonest repaint deadline that pins
// the host's reactive loop back to the full frame rate.
func (st *State) HostReactive() (reactive bool, ok bool) {
	if st.model.Stream.Valid() {
		return st.model.Stream.Reactive, true
	}
	return false, false
}

// RenderDialog renders the video-output settings window when open. Call it
// at the frame top level (outside the panels) so it floats over the app. It
// reads the model RenderStatus refreshed this frame — it does not fetch
// again, so call RenderStatus earlier in the same frame. The window's id is
// the widget's one absolute id, derived from its scope (ADR-0267 W6).
func RenderDialog(in Input) (res Result) {
	res, ok := in.check()
	if !ok {
		return
	}
	for range c.IdScope(in.Ids.PrepareStr(in.scopeKey())) {
		in.renderDialog()
	}
	res.DialogOpen = in.State.dialogOpen
	return
}

func (in Input) renderDialog() {
	ids, st := in.Ids, in.State
	if !st.dialogOpen || len(st.model.Offered()) == 0 {
		return
	}
	winId := c.MakeAbsoluteIdHighEntropy(ids.PrepareStr("window").Derive())
	for range c.Window(winId, c.WidgetText().Text("Video output").Keep()).
		Resizable(false).Collapsible(false).TitleBar(true).DefaultWidth(560).KeepIter() {
		renderContent(ids, st)
		c.Separator().Horizontal().Send()
		if c.Button(ids.PrepareStr("close"), c.Atoms().Text("Close").Keep()).SendResp().HasPrimaryClicked() {
			st.dialogOpen = false
		}
	}
}

// renderContent renders the dialog body — the stream readouts, the codec
// table, and the disabled-encoder table — without the window chrome or the
// Close button. Shared by [RenderDialog] (inside a c.Window) and the
// gallery's [RenderGallery] (inline); both guard on a non-empty Offered set
// before calling it, so the body always renders at least the codec table and
// returns with the id stack back in its initial state.
func renderContent(ids *c.WidgetIdStack, st *State) {
	if s := st.model.Stream; s.Valid() {
		c.Label(fmt.Sprintf("Stream: %d×%d @ %d fps · %s", s.Width, s.Height, s.Fps, s.CadenceName())).Send()
		c.Label(fmt.Sprintf("%.1f Mbps · %d sent · %d coalesced · %d behind",
			float64(s.BitrateKbps)/1000.0, s.FramesSent, s.FramesDropped, s.FramesInFlight)).Send()
		c.Separator().Horizontal().Send()
	}
	// Codec table (egui Grid). The codec name cell is selectable — click it
	// to switch; the rest are read-only columns. "encode" is the host
	// backend, "decode" is this browser — the two are reported separately.
	for range c.Grid(ids.PrepareStr("codecs")).NumColumns(6).Striped(true).KeepIter() {
		c.Label("Codec").Send()
		c.Label("Encoder").Send()
		c.Label("Encode").Send()
		c.Label("Decode").Send()
		c.Label("WebCodecs").Send()
		c.Label("Pixels").Send()
		c.EndRow()
		for _, cc := range st.model.Offered() {
			// The codec's numeric value is its stable identity across frames.
			if c.SelectableLabel(ids.PrepareSeq(uint64(cc.Codec)), st.model.Active == cc.Codec, cc.Codec.String()).
				SendResp().HasPrimaryClicked() {
				if cc.Offerable() && st.model.Active != cc.Codec {
					st.model.Active = cc.Codec
					c.SetVideoPipeline(uint32(cc.Codec))
				}
			}
			c.Label(cc.EncoderName()).Send()
			c.Label(cc.EncodeBackend()).Send()
			c.Label(cc.DecodeBackend()).Send()
			c.Label(cc.CodecString(st.model.Stream.Width, st.model.Stream.Height)).Send()
			c.Label(cc.Codec.PixelFormat()).Send()
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
		for range c.Grid(ids.PrepareStr("disabled")).NumColumns(4).Striped(true).KeepIter() {
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
}
