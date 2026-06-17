package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/videooutput"
	"github.com/stergiotis/boxer/public/thestack/imzero2/videopipeline"
)

// =============================================================================
// videooutput demo — the ADR-0088 remote-stream codec control.
//
// In production the control opens from a status-bar indicator only when a
// remote browser viewer is connected and has reported its decode capabilities;
// the gallery and tour have no live viewer, so this scene feeds it a
// representative model. It exercises the full dialog: the stream readouts +
// telemetry, the codec table (host encode backend vs browser decode standing,
// reported separately; WebCodecs string; pixel format), and the disabled-
// encoder table — here the three VAAPI lanes a Fedora-mesa host probes and
// finds unusable, each with its specific reason.
// =============================================================================

type videoOutputDemoState struct {
	st *videooutput.State
}

func init() {
	registry.Register(registry.Demo{
		Name:     "videooutput",
		Category: "Inspectors & feedback",
		Title:    icons.PhScreencast + " video output",
		Stage:    [2]float32{660, 540},
		Kind:     registry.DemoKindMixed,
		Description: "The imzero2 remote-stream codec control (ADR-0088). In production it " +
			"opens from a status-bar indicator when a browser viewer is connected. The dialog " +
			"shows the live stream geometry/fps/cadence and telemetry (bitrate, frames sent / " +
			"coalesced / behind); a codec picker annotated with the host encode backend and the " +
			"browser decode standing (reported separately), the WebCodecs string, and the pixel " +
			"format; and a 'Disabled encoders' table listing each lane the host probed unusable " +
			"with the specific cause — here the VAAPI lanes a Fedora-mesa host opens but cannot " +
			"encode with. Fed a representative model; production fetches it from the live host " +
			"each frame.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			return &videoOutputDemoState{st: videooutput.NewGalleryState(videoOutputDemoModel())}
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			renderVideoOutputDemo(ids, state.(*videoOutputDemoState))
		},
		SourceFunc: renderVideoOutputDemo,
	})
}

func renderVideoOutputDemo(ids *c.WidgetIdStack, st *videoOutputDemoState) {
	videooutput.ShowGallery(ids, st.st)
}

// videoOutputDemoModel is a representative capability model: all three codecs
// encode in software while the host's VAAPI lanes were probed unusable (the
// Fedora-mesa class), and the browser decodes H.264/VP9 but not AV1 — so the
// picker shows an offerable pair, one host-encodable-but-undecodable codec, and
// a full disabled-encoder table.
func videoOutputDemoModel() videopipeline.Model {
	return videopipeline.Model{
		Active: videopipeline.CodecH264,
		Stream: videopipeline.StreamInfo{
			Width: 1280, Height: 800, Fps: 30, Reactive: false,
			BitrateKbps: 4200, FramesSent: 1234, FramesDropped: 7, FramesInFlight: 2,
		},
		Caps: []videopipeline.CodecCaps{
			{Codec: videopipeline.CodecH264, EncodeSoftware: true, EncodeHardwareFail: videopipeline.ProbeEncodeRejected, DecodeSupported: true, DecodeSmooth: true, DecodeHardware: true},
			{Codec: videopipeline.CodecVP9, EncodeSoftware: true, EncodeHardwareFail: videopipeline.ProbeEncodeRejected, DecodeSupported: true, DecodeSmooth: true},
			{Codec: videopipeline.CodecAV1, EncodeSoftware: true, EncodeHardwareFail: videopipeline.ProbeEncodeRejected, DecodeSupported: false},
		},
	}
}
