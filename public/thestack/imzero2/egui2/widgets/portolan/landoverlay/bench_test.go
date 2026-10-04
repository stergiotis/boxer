package landoverlay

import (
	"iter"
	"testing"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

// meteringChannel discards what it is sent and counts the messages and their
// bytes: the wire cost of a frame without a host behind it.
type meteringChannel struct{ msgs, bytes *int64 }

var _ runtime.ChannelI[*runtime.Unmarshaller] = meteringChannel{}

func (m meteringChannel) SyncMultiUseMsg(_ uint64, b []byte) { *m.msgs++; *m.bytes += int64(len(b)) }
func (m meteringChannel) SendSingleUseMsg(b []byte)          { *m.msgs++; *m.bytes += int64(len(b)) }
func (m meteringChannel) FlushMessages()                     {}
func (m meteringChannel) ReceiveMsg() iter.Seq[*runtime.Unmarshaller] {
	return func(func(*runtime.Unmarshaller) bool) {}
}

// benchViews are the views the land frame-cost trial measures
// (doc/trials/portolan-land-frame-cost): the whole world at zoom 0, where a
// wide pane shows several copies of it; a continent; a country and its
// neighbours, where clipping does most of the work; a country alone.
var benchViews = []struct {
	name      string
	lat, lng  float64
	zoom      float64
	w, h      float32
	wantsFill bool
}{
	{"world-z0", 20, 0, 0, 960, 600, true},
	{"europe-z2.6", 35, 5, 2.6, 960, 600, true},
	{"alps-z5", 46.5, 9, 5, 960, 600, true},
	{"swiss-z8", 46.8, 8.2, 8, 960, 600, true},
}

// BenchmarkPaint is one frame of the overlay on a NoTiles map: the cull, the
// projection, the clip and the encoding into a channel that discards it. It
// is the Go side of the layer and nothing of the host.
func BenchmarkPaint(b *testing.B) {
	level := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	defer zerolog.SetGlobalLevel(level)
	a, err := worldmap.LoadAtlas()
	if err != nil {
		b.Fatal(err)
	}
	var msgs, bytes int64
	typed.SetCurrentFffiVar(runtime.NewFffi2[*runtime.Unmarshaller](meteringChannel{msgs: &msgs, bytes: &bytes}))
	sm := c.CurrentApplicationState.StateManager
	sm.ScriptReset()
	defer sm.ScriptReset()
	for _, v := range benchViews {
		for _, arm := range []struct {
			name string
			st   Style
		}{{"fill", DefaultStyle()}, {"nofill", Style{NoFill: true}}} {
			b.Run(v.name+"/"+arm.name, func(b *testing.B) {
				m := portolan.New(c.NewWidgetIdStack(), "", portolan.Options{NoTiles: true, Center: portolan.LL(v.lat, v.lng), Zoom: v.zoom})
				layer := &Layer{}
				st := arm.st
				if st.NoFill {
					st.Land, st.Border, st.BorderWidth = DefaultStyle().Land, DefaultStyle().Border, DefaultStyle().BorderWidth
				}
				frame := func() { m.Render(v.w, v.h, func(p portolan.Projector) { layer.Paint(p, a, st) }) }
				for range 5 {
					frame()
				}
				msgs, bytes = 0, 0
				frame()
				frameMsgs, frameBytes := msgs, bytes
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					frame()
				}
				b.ReportMetric(float64(layer.Drawn()), "countries")
				b.ReportMetric(float64(frameMsgs), "msgs/frame")
				b.ReportMetric(float64(frameBytes), "bytes/frame")
			})
		}
	}
}

// BenchmarkPaintEmpty is the same frame with no overlay: what Render costs
// around the layer, to subtract.
func BenchmarkPaintEmpty(b *testing.B) {
	typed.SetCurrentFffiVar(runtime.NewFffi2[*runtime.Unmarshaller](meteringChannel{msgs: new(int64), bytes: new(int64)}))
	sm := c.CurrentApplicationState.StateManager
	sm.ScriptReset()
	defer sm.ScriptReset()
	m := portolan.New(c.NewWidgetIdStack(), "", portolan.Options{NoTiles: true, Center: portolan.LL(20, 0), Zoom: 0})
	b.ReportAllocs()
	for range b.N {
		m.Render(960, 600, func(p portolan.Projector) {})
	}
}
