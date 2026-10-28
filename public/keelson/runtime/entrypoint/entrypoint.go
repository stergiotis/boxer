// Package entrypoint names, in the call stack itself, whether the code
// below it runs because an agent asked or because a person (or the app
// on its own) did (ADR-0344 §SD1).
//
// # Contract
//
// Every place the runtime hands control to app code on a caller's behalf
// goes through exactly one of two functions, and never calls the handler
// directly:
//
//   - [Agentic] — the work is agent-caused: an agent's command the
//     dispatcher queued (ADR-0269 §SD6), a watchbill job enqueued under an
//     attested on-behalf-of context, and whatever either of those starts.
//   - [Human] — the work is not agent-caused: the person's gesture
//     through the app's own catalog, a watchbill job with no delegation,
//     an operator's shell verb.
//
// Both are kept out of line and do nothing but call what they were given,
// so their fully qualified names,
//
//	github.com/stergiotis/boxer/public/keelson/runtime/entrypoint.Agentic
//	github.com/stergiotis/boxer/public/keelson/runtime/entrypoint.Human
//
// appear as a frame in every stack captured under them: a panic trace, a
// goroutine dump, a profiler sample, a debugger's backtrace. A reader
// greps for the frame; a program calls [Of], [Current] or [OfTrace].
//
// What this is not: the frame marks the goroutine it is on. A goroutine
// the handler starts does not inherit it, and a frame says nothing about
// what the code did — coverage (ADR-0343) and the trail say that. The
// innermost entry frame wins when both are present, which happens only
// when a handler calls back into the runtime's own apply path.
package entrypoint

import (
	"bytes"
	"runtime"
	"strings"
)

// KindE is what an entry frame says about the stack it is on.
type KindE uint8

const (
	// KindNone is a stack with no entry frame: runtime code, a goroutine
	// a handler started, a test.
	KindNone KindE = iota
	// KindAgentic is a stack under [Agentic].
	KindAgentic
	// KindHuman is a stack under [Human].
	KindHuman
)

// String names the kind the way the trail spells writers.
func (inst KindE) String() (s string) {
	switch inst {
	case KindAgentic:
		return "agentic"
	case KindHuman:
		return "human"
	default:
		return "none"
	}
}

// The fully qualified function names of the two frames, as a stack trace
// prints them; the contract every reader of a trace may rely on.
const (
	FuncAgentic = "github.com/stergiotis/boxer/public/keelson/runtime/entrypoint.Agentic"
	FuncHuman   = "github.com/stergiotis/boxer/public/keelson/runtime/entrypoint.Human"
)

// Agentic runs fn as agent-caused work. The frame stays on the stack for
// the whole call.
//
//go:noinline
func Agentic(fn func()) {
	fn()
}

// Human runs fn as work no agent caused. The frame stays on the stack for
// the whole call.
//
//go:noinline
func Human(fn func()) {
	fn()
}

// For picks the frame for agentCaused, so a caller with a boolean does not
// branch twice.
func For(agentCaused bool) (enter func(fn func())) {
	if agentCaused {
		return Agentic
	}
	return Human
}

// maxDepth bounds the frames Current walks; a handler's stack under the
// engine or the worker is far shallower.
const maxDepth = 256

// Current classifies the calling goroutine's stack: the innermost entry
// frame decides, KindNone when there is none.
func Current() (kind KindE) {
	var pcs [maxDepth]uintptr
	n := runtime.Callers(2, pcs[:])
	return Of(pcs[:n])
}

// Of classifies program counters captured by runtime.Callers, innermost
// first, as Current does.
func Of(pcs []uintptr) (kind KindE) {
	frames := runtime.CallersFrames(pcs)
	for {
		fr, more := frames.Next()
		switch fr.Function {
		case FuncAgentic:
			return KindAgentic
		case FuncHuman:
			return KindHuman
		}
		if !more {
			return KindNone
		}
	}
}

// OfTrace classifies a formatted trace — debug.Stack, a panic message,
// one goroutine of a dump — by the first entry frame it prints, which is
// the innermost. A trace covering several goroutines classifies by the
// first goroutine that carries a frame; split the dump first to classify
// each.
func OfTrace(trace []byte) (kind KindE) {
	for len(trace) > 0 {
		line := trace
		if i := bytes.IndexByte(trace, '\n'); i >= 0 {
			line, trace = trace[:i], trace[i+1:]
		} else {
			trace = nil
		}
		s := strings.TrimSpace(string(line))
		switch {
		case strings.HasPrefix(s, FuncAgentic+"("), s == FuncAgentic:
			return KindAgentic
		case strings.HasPrefix(s, FuncHuman+"("), s == FuncHuman:
			return KindHuman
		}
	}
	return KindNone
}
