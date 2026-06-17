package videooutput

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/videopipeline"
)

// This file is the demo/gallery seam (ADR-0088). The widget gallery and the
// screenshot tour have no live remote viewer to fetch capabilities from, so
// they cannot drive the control through [ShowStatus] (which fetches each
// frame). NewGalleryState builds a State over a fixed model, and ShowGallery
// renders the dialog *body* inline — embedded in the demo panel rather than as
// a floating window, which a gallery scene wants. Production never calls them:
// it holds a zero [State], lets [ShowStatus] populate it, and opens the
// floating [ShowDialog].

// NewGalleryState builds a State over a fixed model, for the widget gallery and
// the screenshot tour.
func NewGalleryState(model videopipeline.Model) *State {
	return &State{model: model, dialogOpen: true}
}

// ShowGallery renders the settings content inline (no floating window, no Close
// button) so a gallery scene embeds it in the demo panel. The body mirrors
// [ShowDialog] and must track it; it is duplicated rather than shared because
// extracting a common body would have to edit videooutput.go while its
// CodecString call is mid-revision in a parallel change. For the same reason
// the WebCodecs cell uses a representative string ([webcodecsGalleryString])
// instead of CodecCaps.CodecString. Once that revision lands, fold both render
// paths onto one extracted body and drop the local string.
func ShowGallery(ids *c.WidgetIdStack, st *State) {
	if len(st.model.Offered()) == 0 {
		return
	}
	for range c.IdScope(ids.PrepareStr("videoout-panel")) {
		for rt := range c.RichTextLabel("Video output") {
			rt.Strong()
		}
		c.Separator().Horizontal().Send()
		if s := st.model.Stream; s.Valid() {
			c.Label(fmt.Sprintf("Stream: %d×%d @ %d fps · %s", s.Width, s.Height, s.Fps, s.CadenceName())).Send()
			c.Label(fmt.Sprintf("%.1f Mbps · %d sent · %d coalesced · %d behind",
				float64(s.BitrateKbps)/1000.0, s.FramesSent, s.FramesDropped, s.FramesInFlight)).Send()
			c.Separator().Horizontal().Send()
		}
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
				c.Label(webcodecsGalleryString(cc.Codec)).Send()
				c.Label("4:2:0 8-bit").Send()
				c.EndRow()
			}
		}
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
	}
}

// webcodecsGalleryString is a representative WebCodecs string per codec for the
// gallery panel — a fixed value so the gallery stays decoupled from
// CodecCaps.CodecString while that method is mid-revision (see [ShowGallery]).
// Production reads the real string, whose level is derived from the live stream
// resolution.
func webcodecsGalleryString(codec videopipeline.Codec) string {
	switch codec {
	case videopipeline.CodecVP9:
		return "vp09.00.31.08"
	case videopipeline.CodecAV1:
		return "av01.0.05M.08"
	default:
		return "avc1.42E01E"
	}
}
