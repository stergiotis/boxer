package widgets

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/sink/pulsesink"
	"github.com/stergiotis/boxer/public/science/audio/track"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/waveform"
)

// =============================================================================
// waveform player demo — the audio waveform player of ADR-0208 over a
// synthetic stereo track. The left channel is a 440 Hz tone under a gate, the
// shape a voice recording has at a glance; the right is a chirp. The sink
// opens as the device-less Null clock, so pressing play moves the playhead
// without a sound device; 'output device' swaps the PulseAudio sink in (M3).
// 'twelve hours' opens a 12 h synthetic track whose peaks build in the
// background while it draws (M4); 'open' loads a file from disk through the
// sniffing decoder — WAV natively, everything else through ffmpeg. The readout
// lines under the canvas are what the headless scene asserts on.
// =============================================================================

const (
	waveformDemoSeconds = 24
	waveformDemoHeight  = 220
	waveformDemoWidth   = 1000
)

// waveformDemoFile names a recording the demo opens at mount instead of the
// synthetic track — the scripted route for a headless scene, since the demo's
// path field is a painter-adjacent TextEdit the accessibility driver cannot
// reach. Empty opens the synthetic track.
var waveformDemoFile = env.NewPath(env.Spec{
	Name:        "BOXER_WAVEFORM_DEMO_FILE",
	Description: "recording the waveform gallery demo opens at mount, for scripted scenes; empty opens the synthetic track",
	Category:    env.CategoryDev,
})

type waveformDemoState struct {
	tr     *track.Track
	player *waveform.Player
	ids    *c.WidgetIdStack
	err    error

	lastClick int64
	hasClick  bool
	// Bound to widgets, so they live on the heap for the frame after the
	// edit (stable-pointer rule).
	device    bool
	deviceErr string
	rate      float64
	volume    float64
	path      string
	openErr   string
	source    string // what the track is: the short synthetic, the 12 h one, or a file
}

func init() {
	registry.Register(registry.Demo{
		Name:     "waveform",
		Category: "Charts & plots",
		Title:    icons.PhWaveform + " waveform player",
		Stage:    [2]float32{1100, 460},
		Kind:     registry.DemoKindUX,
		Description: "Audio waveform player (ADR-0208): the track's min/max peaks " +
			"drawn one rect batch per channel, a playhead, click-to-seek, drag-to-pan, " +
			"wheel to scroll and Ctrl+wheel or pinch to zoom about the pointer, a " +
			"duration ruler and a hover readout. Space plays and pauses, the arrows " +
			"seek (Shift for a longer step), Home/End jump, PageUp/PageDown zoom. " +
			"Zoom in far enough and the columns give way to the samples themselves, " +
			"fetched off the frame thread. The track here is synthetic — a gated tone on " +
			"the left, a chirp on the right. It opens on the device-less clock, so play " +
			"moves the playhead without audio hardware; 'output device' swaps in the " +
			"PulseAudio sink and plays through the speakers, with rate and volume. " +
			"'twelve hours' opens a 12 h synthetic track whose peaks build in the " +
			"background while you scroll it; 'open' loads a recording from disk (WAV " +
			"natively, other formats through ffmpeg), its peaks cached for the next open.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			return newWaveformDemoState(ids)
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoWaveform(ids, state.(*waveformDemoState))
		},
		SourceFunc: demoWaveform,
	})
}

// waveformDemoSignal is the demo's stereo test signal over a track of the
// given length: bursts of tone every 0.9 s under a slow amplitude ramp on the
// left, a chirp across the whole length on the right.
func waveformDemoSignal(format pcm.Format, frames int64) (fn pcm.SampleFunc) {
	rate := int64(format.SampleRate)
	left := pcm.Gate(pcm.Sine(format, 440, 0.8), rate*35/100, rate*55/100)
	ramp := func(frame int64, ch int) float32 {
		s := left(frame, ch)
		phase := float32(frame%(rate*8)) / float32(rate*8)
		return s * (0.35 + 0.65*phase)
	}
	right := pcm.Chirp(format, frames, 100, 4000, 0.5)
	return pcm.PerChannel(ramp, right)
}

func newWaveformDemoState(ids *c.WidgetIdStack) (st *waveformDemoState) {
	st = &waveformDemoState{ids: ids, rate: 1, volume: 1}
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	frames := format.DurationToFrames(waveformDemoSeconds * time.Second)
	src, err := pcm.NewSynthSourceE(format, frames, waveformDemoSignal(format, frames))
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
	st.setTrack(tr, fmt.Sprintf("synthetic %d s", waveformDemoSeconds))
	if path := waveformDemoFile.Get(); path != "" {
		st.path = path
		st.openFile()
	}
	return st
}

// setTrack swaps the track and rebuilds the player over it; the old track is
// closed. The player keeps its scope key, so egui state under it survives.
func (st *waveformDemoState) setTrack(tr *track.Track, source string) {
	if st.tr != nil {
		if err := st.tr.CloseE(); err != nil {
			log.Warn().Err(err).Msg("waveform demo: closing the previous track")
		}
	}
	st.tr = tr
	st.source = source
	st.player = waveform.New(st.ids, tr, waveform.Options{ScopeKey: "waveform-demo"})
	st.hasClick = false
	st.device, st.deviceErr = false, ""
	st.rate, st.volume = 1, 1
	sm := c.CurrentApplicationState.StateManager
	sm.OverrideDatabindingBPtr(&st.device)
	sm.OverrideDatabindingF64Ptr(&st.rate)
	sm.OverrideDatabindingF64Ptr(&st.volume)
}

// openTwelveHours opens a 12 h synthetic stereo track building in the
// background (ADR-0208 SD4); Reopen hands the builder and the window cache
// their own procedural sources, as a file would get its own decoders.
func (st *waveformDemoState) openTwelveHours() {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	frames := format.DurationToFrames(12 * time.Hour)
	fn := waveformDemoSignal(format, frames)
	reopen := func(_ context.Context) (pcm.SourceI, error) { return pcm.NewSynthSourceE(format, frames, fn) }
	src, err := reopen(context.Background())
	if err != nil {
		st.openErr = err.Error()
		return
	}
	tr, err := track.OpenE(context.Background(), src, track.Options{
		Background: true,
		Reopen:     reopen,
		NewSink:    func(s pcm.SourceI) sink.SinkI { return sink.NewNull(s, nil) },
	})
	if err != nil {
		st.openErr = err.Error()
		return
	}
	st.openErr = ""
	st.setTrack(tr, "synthetic 12 h")
}

func (st *waveformDemoState) openFile() {
	if st.path == "" {
		st.openErr = "no path"
		return
	}
	tr, kind, err := track.OpenFileE(context.Background(), st.path, track.Options{
		NewSink: func(s pcm.SourceI) sink.SinkI { return sink.NewNull(s, nil) },
	})
	if err != nil {
		st.openErr = err.Error()
		return
	}
	st.openErr = ""
	st.setTrack(tr, kind.String()+" file")
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
	for range c.HorizontalTop().KeepIter() {
		if c.Checkbox(ids.PrepareStr("wf-device"), st.device, "output device").SendRespVal(&st.device).HasChanged() {
			st.deviceErr = ""
			var err error
			if st.device {
				err = st.tr.ReplaceSinkE(func(src pcm.SourceI) (sink.SinkI, error) {
					return pulsesink.OpenE(src, pulsesink.Options{AppName: "boxer widget gallery"})
				})
			} else {
				err = st.tr.ReplaceSinkE(func(src pcm.SourceI) (sink.SinkI, error) { return sink.NewNull(src, nil), nil })
			}
			if err != nil {
				// The old sink stays; put the checkbox back and say why.
				st.device = false
				st.deviceErr = err.Error()
				c.CurrentApplicationState.StateManager.OverrideDatabindingBPtr(&st.device)
			}
		}
		if c.SliderF64(ids.PrepareStr("wf-rate"), st.rate, 0.5, 2.0).Text("rate").SendRespVal(&st.rate).HasChanged() {
			if err := st.tr.Sink().SetRateE(st.rate); err != nil {
				log.Warn().Err(err).Msg("waveform demo: rate rejected")
			}
		}
		if c.SliderF64(ids.PrepareStr("wf-volume"), st.volume, 0, 1).Text("volume").SendRespVal(&st.volume).HasChanged() {
			if err := st.tr.Sink().SetVolumeE(st.volume); err != nil {
				log.Warn().Err(err).Msg("waveform demo: volume rejected")
			}
		}
	}
	if st.deviceErr != "" {
		c.Label("no output device: " + st.deviceErr).Selectable(false).Send()
	}
	for range c.HorizontalTop().KeepIter() {
		if c.Button(ids.PrepareStr("wf-12h"), c.Atoms().Text(icons.PhClock+" twelve hours").Keep()).SendResp().HasPrimaryClicked() {
			st.openTwelveHours()
		}
		c.TextEdit(ids.PrepareStr("wf-path"), st.path, false).HintText("path to a recording (wav, flac, mp3, opus, …)").DesiredWidth(420).SendRespVal(&st.path)
		if c.Button(ids.PrepareStr("wf-open"), c.Atoms().Text(icons.PhFolderOpen+" open").Keep()).SendResp().HasPrimaryClicked() {
			st.openFile()
		}
	}
	if st.openErr != "" {
		c.Label("open failed: " + st.openErr).Selectable(false).Send()
	}
	c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))

	// A button above may have swapped the track; draw the current player.
	p = st.player
	p.RenderFillWidth(waveformDemoHeight, waveformDemoWidth)

	c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
	bp := st.tr.BuildProgress()
	if !bp.Complete {
		frac := float32(0)
		if bp.TotalFrames > 0 {
			frac = float32(float64(bp.BuiltFrames) / float64(bp.TotalFrames))
		}
		note := "peaks building in the background"
		if bp.Err != nil {
			note = "build failed: " + bp.Err.Error()
		}
		jobprogress.Render(jobprogress.Input{Title: "peaks", Fraction: frac, Note: note})
	}

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
	entries, bytes, hits, misses, fetches := st.tr.WindowCacheStats()
	c.Label(fmt.Sprintf("track: %s · %s · peaks %s · windows %d (%d KiB) hits %d misses %d fetches %d",
		st.source, st.tr.Duration().Round(time.Second), peaksState(bp), entries, bytes/1024, hits, misses, fetches)).Selectable(false).Send()
}

func peaksState(bp track.BuildProgress) (s string) {
	switch {
	case bp.FromCache:
		return "from cache"
	case bp.Complete:
		return "built"
	case bp.Err != nil:
		return "failed"
	default:
		return "building"
	}
}
