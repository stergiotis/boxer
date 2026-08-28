package widgets

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/track"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/waveform"
)

// =============================================================================
// waveform player demo — the audio waveform player of ADR-0208 over a
// synthetic stereo track (ADR-0208 M2). The left channel is a 440 Hz tone
// under a gate, the shape a voice recording has at a glance; the right is a
// chirp from 100 Hz to 4 kHz. The sink is the device-less Null clock, so
// pressing play moves the playhead without a sound device. The readout lines
// under the canvas are what a headless scene asserts on.
// =============================================================================

const (
	waveformDemoSeconds = 24
	waveformDemoHeight  = 220
	waveformDemoWidth   = 1000
)

type waveformDemoState struct {
	tr        *track.Track
	player    *waveform.Player
	err       error
	lastClick int64
	hasClick  bool
}

func init() {
	registry.Register(registry.Demo{
		Name:     "waveform",
		Category: "Charts & plots",
		Title:    icons.PhWaveform + " waveform player",
		Stage:    [2]float32{1100, 420},
		Kind:     registry.DemoKindUX,
		Description: "Audio waveform player (ADR-0208): the track's min/max peaks " +
			"drawn one rect batch per channel, a playhead, click-to-seek, drag-to-pan, " +
			"wheel to scroll and Ctrl+wheel or pinch to zoom about the pointer, a " +
			"duration ruler and a hover readout. Space plays and pauses, the arrows " +
			"seek (Shift for a longer step), Home/End jump, PageUp/PageDown zoom. " +
			"Zoom in far enough and the columns give way to the samples themselves. " +
			"The track here is synthetic — a gated tone on the left, a chirp on the " +
			"right — and the sink is the device-less clock, so play moves the playhead " +
			"without audio hardware.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			return newWaveformDemoState(ids)
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoWaveform(ids, state.(*waveformDemoState))
		},
		SourceFunc: demoWaveform,
	})
}

func newWaveformDemoState(ids *c.WidgetIdStack) (st *waveformDemoState) {
	st = &waveformDemoState{}
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	frames := format.DurationToFrames(waveformDemoSeconds * time.Second)
	// Bursts of 0.35 s tone every 0.9 s, over a slow amplitude ramp so the
	// whole-track view has visible structure; a chirp on the right.
	rate := int64(format.SampleRate)
	left := pcm.Gate(pcm.Sine(format, 440, 0.8), rate*35/100, rate*55/100)
	ramp := func(frame int64, ch int) float32 {
		s := left(frame, ch)
		phase := float32(frame%(rate*8)) / float32(rate*8)
		return s * (0.35 + 0.65*phase)
	}
	right := pcm.Chirp(format, frames, 100, 4000, 0.5)
	src, err := pcm.NewSynthSourceE(format, frames, pcm.PerChannel(ramp, right))
	if err != nil {
		st.err = err
		return st
	}
	tr, err := track.OpenE(context.Background(), src, track.Options{
		NewSink: func(s pcm.SourceI) sink.SinkI { return sink.NewNull(s, nil) },
	})
	if err != nil {
		st.err = err
		log.Error().Err(err).Msg("waveform demo: unable to open the synthetic track")
		return st
	}
	st.tr = tr
	st.player = waveform.New(ids, tr, waveform.Options{ScopeKey: "waveform-demo"})
	return st
}

func demoWaveform(ids *c.WidgetIdStack, st *waveformDemoState) {
	if st.err != nil || st.player == nil {
		c.Label(fmt.Sprintf("waveform demo unavailable: %v", st.err)).Send()
		return
	}
	p := st.player
	c.Label("Click to seek, drag to pan, wheel to scroll, Ctrl+wheel to zoom; Space plays.").Send()
	c.Separator().Horizontal().Send()

	for range c.HorizontalTop().KeepIter() {
		playLabel := icons.PhPlay + " play"
		if p.IsPlaying() {
			playLabel = icons.PhPause + " pause"
		}
		if c.Button(ids.PrepareStr("wf-play"), c.Atoms().Text(playLabel).Keep()).SendResp().HasPrimaryClicked() {
			p.TogglePlay()
		}
		if c.Button(ids.PrepareStr("wf-rewind"), c.Atoms().Text(icons.PhSkipBack+" start").Keep()).SendResp().HasPrimaryClicked() {
			p.SeekTo(0)
		}
		if c.Button(ids.PrepareStr("wf-fit"), c.Atoms().Text("fit").Keep()).SendResp().HasPrimaryClicked() {
			p.FitAll()
		}
		if c.Button(ids.PrepareStr("wf-zoom-in"), c.Atoms().Text(icons.PhMagnifyingGlassPlus+" zoom in").Keep()).SendResp().HasPrimaryClicked() {
			p.ZoomBy(4)
		}
		if c.Button(ids.PrepareStr("wf-zoom-out"), c.Atoms().Text(icons.PhMagnifyingGlassMinus+" zoom out").Keep()).SendResp().HasPrimaryClicked() {
			p.ZoomBy(0.25)
		}
		// A fixed zoom, so a drag of N pixels is a known offset: at 48 kHz,
		// 480 frames per pixel is 10 ms per pixel.
		if c.Button(ids.PrepareStr("wf-zoom-10ms"), c.Atoms().Text("10 ms/px").Keep()).SendResp().HasPrimaryClicked() {
			v := p.View()
			v.FramesPerPx = 480
			p.SetView(v)
		}
	}
	c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))

	p.RenderFillWidth(waveformDemoHeight, waveformDemoWidth)

	c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
	state := "paused"
	if p.IsPlaying() {
		state = "playing"
	} else if st.tr.Sink().Ended() {
		state = "ended"
	}
	v := p.View()
	w, _ := p.Size()
	rate := float64(st.tr.Format().SampleRate)
	c.Label(fmt.Sprintf("position: %s · %s", p.FormatOffset(p.Position()), state)).Selectable(false).Send()
	c.Label(fmt.Sprintf("view: %s – %s · %.3f ms/px",
		p.FormatOffset(int64(v.FromFrame)), p.FormatOffset(int64(v.ToFrame(w))),
		v.FramesPerPx/rate*1000)).Selectable(false).Send()
	hover := "hover: —"
	if f, ok := p.Hover(); ok {
		hover = "hover: " + p.FormatOffset(f)
	}
	c.Label(hover).Selectable(false).Send()
	if f, ok := p.Clicked(); ok {
		st.lastClick = f
		st.hasClick = true
	}
	click := "clicked: —"
	if st.hasClick {
		click = "clicked: " + p.FormatOffset(st.lastClick)
	}
	c.Label(click).Selectable(false).Send()
}
