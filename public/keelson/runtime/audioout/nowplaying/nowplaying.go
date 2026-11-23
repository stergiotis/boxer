// Package nowplaying is the one recording a window plays: a WAVE file's
// bytes, loaded and opened off the frame, played through the output the
// host gives (ADR-0372), with the state a face reads once per frame.
//
// It is the weight of a recording shown among other things — a cell of a
// notebook, a row's attachment: play, pause, seek and a playhead. Zoom,
// regions and lanes are the waveform player's (ADR-0208), which needs a
// track. Starting a recording stops the one that was playing.
package nowplaying

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audioout"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
)

// State is a recording's state as a face reads it.
type State struct {
	// Active says the recording is the player's: it was started and not
	// replaced or stopped.
	Active bool
	// Opening says its bytes are being read or the output opened.
	Opening bool
	Playing bool
	// Fraction is the playhead in [0, 1] and Position the same as a time.
	Fraction float64
	Position time.Duration
	// Note says why nothing is heard, or why the recording could not be
	// played; "" when it plays and is heard.
	Note string
}

// Loader reads a recording's bytes. It runs off the render goroutine.
type Loader func(ctx context.Context) (data []byte, err error)

// Player holds at most one recording. Its methods are the render
// goroutine's; the reading and the opening run where spawn puts them.
type Player struct {
	bus   app.BusI
	spawn func(f func(ctx context.Context)) (started bool)
	cur   *session
}

// New is a player over the window's bus. spawn runs work off the render
// goroutine as the window's — a lifetime scope's Go; nil runs it bare. A
// nil bus plays on a silent clock, and the state says so.
func New(bus app.BusI, spawn func(f func(ctx context.Context)) (started bool)) (inst *Player) {
	if spawn == nil {
		spawn = func(f func(ctx context.Context)) bool {
			go f(context.Background()) //boxer:lint disable=CS020 reason="documented root: a nil spawn runs the work bare, under no lifetime"
			return true
		}
	}
	return &Player{bus: bus, spawn: spawn}
}

type session struct {
	key string

	mu      sync.Mutex
	sink    sink.SinkI
	file    *wavfile.File
	note    string
	opening bool
	closed  bool
}

// State reports the recording key names.
func (inst *Player) State(key string) (st State) {
	s := inst.cur
	if s == nil || s.key != key {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st.Active, st.Opening, st.Note = true, s.opening, s.note
	if s.sink == nil {
		return
	}
	if st.Note == "" {
		// An output a viewer's page plays may have nobody listening at
		// this moment (ADR-0372 §SD9); the playhead moves all the same.
		if yes, reason := audioout.Audible(s.sink); !yes {
			st.Note = "silent: " + reason
		}
	}
	st.Playing = s.sink.State() == sink.StatePlaying
	if frames := s.sink.Frames(); frames > 0 {
		pos := s.sink.Position()
		st.Fraction = min(float64(pos)/float64(frames), 1)
		st.Position = s.sink.Format().FramesToDuration(pos)
	}
	return
}

// Toggle plays or pauses the recording key names. For a recording that is
// not the player's it stops the one that is and starts this one: load is
// called once, off the render goroutine, and playback starts when the
// output is open.
func (inst *Player) Toggle(key string, load Loader) {
	if s := inst.cur; s != nil && s.key == key {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.sink == nil {
			// Still opening, or it could not be played.
			return
		}
		switch {
		case s.sink.State() == sink.StatePlaying:
			s.sink.Pause()
		case s.sink.Ended():
			_ = s.sink.SeekFrame(0)
			s.sink.Play()
		default:
			s.sink.Play()
		}
		return
	}
	inst.Stop()
	s := &session{key: key, opening: true}
	inst.cur = s
	bus := inst.bus
	if !inst.spawn(func(ctx context.Context) { s.open(ctx, bus, load) }) {
		// The window is leaving: nothing starts.
		s.mu.Lock()
		s.opening, s.closed = false, true
		s.mu.Unlock()
		inst.cur = nil
	}
}

// open loads the recording and opens the output the host gives on bus. A
// host that gives none gets the device-less clock and a reason, not a
// failed open: the playhead still moves (ADR-0208 §SD6).
func (inst *session) open(ctx context.Context, bus app.BusI, load Loader) {
	// Nothing here holds the lock while it reads or talks to the sound
	// server: a face reads the state every frame.
	var out sink.SinkI
	var f *wavfile.File
	note := ""
	data, err := load(ctx)
	if err == nil {
		f, err = wavfile.NewReader(bytes.NewReader(data), int64(len(data)))
	}
	switch {
	case err != nil:
		note = "not played: " + err.Error()
		f = nil
	case bus == nil:
		note = "no output device: this host gives the window no bus"
	default:
		dev, st := audioout.Open(ctx, bus, f)
		out = dev
		if !st.Audible() {
			note = "no output device: " + st.Reason
		}
	}
	if out == nil && f != nil {
		out = sink.NewNull(f, nil)
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.opening = false
	if inst.closed {
		// Stopped while opening: the session is gone, so is what it opened.
		if out != nil {
			_ = out.Close()
		}
		if f != nil {
			_ = f.Close()
		}
		return
	}
	inst.file, inst.sink, inst.note = f, out, note
	if out != nil {
		out.Play()
	}
}

// Seek moves the playhead of the recording key names, when it is the
// player's.
func (inst *Player) Seek(key string, fraction float64) {
	s := inst.cur
	if s == nil || s.key != key {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sink != nil {
		_ = s.sink.SeekFrame(int64(fraction * float64(s.sink.Frames())))
	}
}

// Stop ends the recording the player holds, if any.
func (inst *Player) Stop() {
	s := inst.cur
	if s == nil {
		return
	}
	inst.cur = nil
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.sink != nil {
		_ = s.sink.Close()
		s.sink = nil
	}
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
}
