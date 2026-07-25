// Package runstream is the result-frame contract of a query run (the E3
// extension point of doc/explanation/query-system-requirements.md).
//
// A run's result is a sequence of typed, sequenced frames: data, progress,
// and exactly one terminal frame saying how it ended — complete, truncated,
// or failed. The point is R9, completeness honesty: a result capped by a
// row limit, a stream that died halfway, and a complete result are three
// different outcomes, and a consumer must not be able to mistake one for
// another. The rule that makes that hold is simple and absolute:
//
//	no terminal frame means incomplete.
//
// Absence is the safe reading, so nothing needs to go right for a partial
// result to be recognised as partial. A stream that stops — the process
// died, the connection dropped, the producer forgot — collects into
// [ErrIncomplete] rather than into a plausible-looking short answer.
//
// A synchronous HTTP response is the degenerate case of this stream, not an
// exception to it: data frames for the batches, then one terminal. Writing
// it that way is what keeps the invariants in one place instead of in every
// panel that renders a result.
//
// [Collector] owns the invariants. Producers build frames; consumers push
// them into a Collector and read the outcome back.
package runstream

import (
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrIncomplete reports a stream that ended without a terminal frame. Test
// for it with errors.Is.
var ErrIncomplete = errors.New("runstream: stream ended without a terminal frame")

// Seq numbers a frame within one run's stream. Sequence numbers start at 1
// and strictly increase, so a consumer can tell a duplicate or a reordered
// delivery from a fresh frame. Zero is not a valid frame number.
type Seq uint64

// KindE says which of a frame's payloads is meaningful.
//
// The zero value is KindUnknown, which [Collector.Push] rejects — a frame
// nobody filled in is a bug, not a data frame.
type KindE uint8

const (
	KindUnknown KindE = iota
	// KindData carries a chunk of the result.
	KindData
	// KindProgress carries an inflight observation. Progress frames are
	// advisory: they may be absent entirely, and their absence says nothing
	// (R8). Never derive state from a missing progress frame.
	KindProgress
	// KindTerminal ends the stream. Exactly one per run.
	KindTerminal
)

var AllKinds = []KindE{KindUnknown, KindData, KindProgress, KindTerminal}

func (inst KindE) String() (name string) {
	switch inst {
	case KindData:
		name = "data"
	case KindProgress:
		name = "progress"
	case KindTerminal:
		name = "terminal"
	default:
		name = "unknown"
	}
	return
}

// TerminalStateE is how a run ended.
//
// The zero value is TerminalUnknown rather than TerminalComplete, and
// [Collector.Push] rejects it. A terminal frame that forgot to say how the
// run ended must not be readable as "it went fine".
//
//codelint:enum-prefix=Terminal
type TerminalStateE uint8

const (
	TerminalUnknown TerminalStateE = iota
	// TerminalComplete means the whole result arrived.
	TerminalComplete
	// TerminalTruncated means the run ended early against a limit and the
	// result is a prefix of the answer. Reason says which limit.
	TerminalTruncated
	// TerminalFailed means the run did not produce a usable result. Err
	// carries the cause.
	TerminalFailed
)

var AllTerminalStates = []TerminalStateE{
	TerminalUnknown, TerminalComplete, TerminalTruncated, TerminalFailed,
}

func (inst TerminalStateE) String() (name string) {
	switch inst {
	case TerminalComplete:
		name = "complete"
	case TerminalTruncated:
		name = "truncated"
	case TerminalFailed:
		name = "failed"
	default:
		name = "unknown"
	}
	return
}

// Progress is an inflight observation of a run. The fields mirror what both
// producers can actually see — ClickHouse's in-band progress headers and a
// system.processes row — so neither has to invent a number. Zero means "not
// reported", never "zero so far".
type Progress struct {
	ReadRows        uint64
	ReadBytes       uint64
	TotalRowsToRead uint64
	ElapsedNs       uint64
	MemoryUsage     uint64
}

// Terminal says how a run ended.
type Terminal struct {
	State TerminalStateE
	// Reason is for a human: which limit truncated the result, or what
	// failed. Required for TerminalTruncated, where a bare "truncated"
	// leaves the reader with nothing to act on.
	Reason string
	// Err is the cause of a TerminalFailed run, and is nil otherwise.
	Err error
}

// Complete builds the terminal of a run that delivered its whole result.
func Complete() (t Terminal) {
	t = Terminal{State: TerminalComplete}
	return
}

// Truncated builds the terminal of a run that hit a limit. The reason names
// the limit.
func Truncated(reason string) (t Terminal) {
	t = Terminal{State: TerminalTruncated, Reason: reason}
	return
}

// Failed builds the terminal of a run that did not produce a usable result.
func Failed(err error) (t Terminal) {
	t = Terminal{State: TerminalFailed, Reason: errText(err), Err: err}
	return
}

func errText(err error) (text string) {
	if err != nil {
		text = err.Error()
	}
	return
}

// Frame is one element of a run's stream. Kind says which payload is
// meaningful; the others are zero.
type Frame[T any] struct {
	Seq      Seq
	Kind     KindE
	Data     T
	Progress Progress
	Terminal Terminal
}

// DataFrame builds a data frame carrying one chunk of the result.
func DataFrame[T any](seq Seq, data T) (f Frame[T]) {
	f = Frame[T]{Seq: seq, Kind: KindData, Data: data}
	return
}

// ProgressFrame builds an advisory progress frame.
func ProgressFrame[T any](seq Seq, p Progress) (f Frame[T]) {
	f = Frame[T]{Seq: seq, Kind: KindProgress, Progress: p}
	return
}

// TerminalFrame builds the one frame that ends a stream.
func TerminalFrame[T any](seq Seq, t Terminal) (f Frame[T]) {
	f = Frame[T]{Seq: seq, Kind: KindTerminal, Terminal: t}
	return
}

// Collector accumulates one run's frames and holds the stream's invariants
// in a single place, so panels render results instead of re-deriving what
// "finished" means.
//
// It rejects, rather than tolerates: a frame out of sequence, a second
// terminal, anything after a terminal, and a frame whose kind or terminal
// state was never set. Each of those is a producer bug that would otherwise
// surface later as a wrong answer.
//
// The zero Collector is ready to use. Not safe for concurrent use — one
// collector belongs to one run, driven by whoever is reading it.
type Collector[T any] struct {
	last     Seq
	data     []T
	progress Progress
	hasProg  bool
	terminal Terminal
	done     bool
}

// Push adds the next frame of the stream.
func (inst *Collector[T]) Push(f Frame[T]) (err error) {
	if inst.done {
		err = eb.Build().Uint64("seq", uint64(f.Seq)).Str("kind", f.Kind.String()).
			Errorf("runstream: frame after the terminal frame")
		return
	}
	if f.Seq <= inst.last {
		err = eb.Build().Uint64("seq", uint64(f.Seq)).Uint64("previousSeq", uint64(inst.last)).
			Errorf("runstream: sequence must strictly increase")
		return
	}
	switch f.Kind {
	case KindData:
		inst.data = append(inst.data, f.Data)
	case KindProgress:
		inst.progress = f.Progress
		inst.hasProg = true
	case KindTerminal:
		if f.Terminal.State == TerminalUnknown {
			err = eh.Errorf("runstream: terminal frame does not say how the run ended")
			return
		}
		inst.terminal = f.Terminal
		inst.done = true
	default:
		err = eb.Build().Uint64("seq", uint64(f.Seq)).Errorf("runstream: frame has no kind")
		return
	}
	inst.last = f.Seq
	return
}

// Data returns the collected data payloads in arrival order. It is valid to
// read them from an incomplete stream — a prefix of a result is still a
// prefix — as long as the caller has consulted [Collector.Terminal] and
// knows that is what it has.
func (inst *Collector[T]) Data() (data []T) {
	data = inst.data
	return
}

// Progress returns the latest inflight observation. ok is false when none
// arrived, which carries no meaning about the run (R8).
func (inst *Collector[T]) Progress() (p Progress, ok bool) {
	p, ok = inst.progress, inst.hasProg
	return
}

// Done reports whether the terminal frame arrived.
func (inst *Collector[T]) Done() (done bool) {
	done = inst.done
	return
}

// Terminal returns how the run ended, or ErrIncomplete if the stream ended
// without saying. A caller that ignores this error is claiming a partial
// result is a whole one.
func (inst *Collector[T]) Terminal() (t Terminal, err error) {
	if !inst.done {
		err = ErrIncomplete
		return
	}
	t = inst.terminal
	return
}
