package sink

import "github.com/stergiotis/boxer/public/science/audio/pcm"

// StateE is a sink's transport state.
type StateE uint8

const (
	// StateStopped is the state before the first [SinkI.Play] and after
	// playback ran into the end of the source.
	StateStopped StateE = iota
	StatePlaying
	StatePaused
)

// AllStates lists every transport state.
var AllStates = []StateE{StateStopped, StatePlaying, StatePaused}

// String returns the state's lowercase name.
func (inst StateE) String() (s string) {
	switch inst {
	case StateStopped:
		return "stopped"
	case StatePlaying:
		return "playing"
	case StatePaused:
		return "paused"
	}
	return "unknown"
}

// Bounds of the playback rate. The lower bound is exclusive: an arbitrarily
// slow rate is a stalled playhead rather than slow playback, and the
// resampler a device-backed sink runs (ADR-0208 §SD6) has a ratio range of
// its own.
const (
	RateMinExcl float64 = 0.25
	RateMaxIncl float64 = 4
)

// Bounds of the output gain.
const (
	VolumeMinIncl float64 = 0
	VolumeMaxIncl float64 = 1
)

// SinkI is a playback transport over one [pcm.SourceI]. Position is a frame
// index into that source, never a byte offset or a duration; the widget's
// time bases (ADR-0208 §SD9) are derived from it.
//
// Every method is safe to call from any goroutine, and none of them may
// block on a device: a device-backed implementation runs a callback
// goroutine of its own while the widget calls the getters once per rendered
// frame.
type SinkI interface {
	// Format returns the source's format.
	Format() (format pcm.Format)
	// Frames returns the source's length; it bounds Position.
	Frames() (frames int64)
	// Play starts or resumes playback and is idempotent. From a position at
	// Frames() — reached by playing to the end, or by a seek — it restarts
	// from frame 0. It clears Ended.
	Play()
	// Pause freezes the position and is idempotent. It does nothing when
	// playback is not running.
	Pause()
	State() (state StateE)
	// Position returns the audible frame, clamped to [0, Frames()]. While
	// State is StatePlaying, consecutive calls advance.
	Position() (frame int64)
	// SeekE moves the audible position and keeps State. A frame outside
	// [0, Frames()] is clamped rather than rejected — only a closed sink is
	// an error. It clears Ended.
	SeekE(frame int64) (err error)
	// Rate returns the playback rate; 1 plays the source at its own rate.
	Rate() (rate float64)
	// SetRateE sets the playback rate, which must lie in (RateMinExcl,
	// RateMaxIncl]. The position does not jump when the rate changes during
	// playback.
	SetRateE(rate float64) (err error)
	// Volume returns the output gain, in [VolumeMinIncl, VolumeMaxIncl].
	Volume() (v float64)
	SetVolumeE(v float64) (err error)
	// Ended reports whether playback ran into the end of the source and
	// stopped there. Play and SeekE clear it.
	Ended() (ended bool)
	// CloseE releases whatever the sink holds and is idempotent.
	CloseE() (err error)
}
