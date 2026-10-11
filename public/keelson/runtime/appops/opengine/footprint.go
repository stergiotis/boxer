package opengine

import (
	"slices"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// The footprint check (ADR-0315 §SD2, law L9; ADR-0310 §SD11 step 9): a command's
// observed resource changes are among its declared writes. The engine
// already observes, after the app's frame, every resource that changed
// other than through a declared write and logs it as the app's own doing;
// read between one applied command's entry and the next command's, those
// changes are the command's, declared or not. A conformance test runs the
// commands it cares about one at a time, lets the app's frames settle, and
// reads the log.

// UndeclaredWrite is one resource a command changed without declaring it.
type UndeclaredWrite struct {
	Op       string
	CallId   string
	Writer   string
	Resource string
	// Frame is the frame the change was observed in; a later frame than
	// the command's is work the command started.
	Frame uint64
	// Observed is the writer the engine recorded for the change: the app,
	// for a change its frame made; the person, for a change that landed
	// between frames, which the engine cannot tell from the person's own
	// edit. A headless conformance run has no person, so its between-frame
	// changes are the command's asynchronous work (§SD6).
	Observed string
}

// UndeclaredWrites reads the log after seq: every observed change after an
// applied command's entry and before the next command's, of a resource the
// command did not declare. Changes before the first command, or after a
// command that was refused, conflicted or found stale, are nobody's: a
// command that did not apply started nothing.
func (inst *Engine) UndeclaredWrites(seq uint64) (out []UndeclaredWrite) {
	return undeclaredWrites(inst.LogSince(seq))
}

func undeclaredWrites(entries []LogEntry) (out []UndeclaredWrite) {
	var cmd *LogEntry
	for i := range entries {
		e := &entries[i]
		switch {
		case e.Op != "":
			cmd = nil
			if e.Phase == opwire.PhaseApplied {
				cmd = e
			}
		case cmd == nil:
		case e.Writer == opwire.WriterApp, e.Writer == opwire.WriterPerson:
			for _, r := range e.Resources {
				if !slices.Contains(cmd.Resources, r) {
					out = append(out, UndeclaredWrite{Op: cmd.Op, CallId: cmd.CallId, Writer: cmd.Writer, Resource: r, Frame: e.Frame, Observed: e.Writer})
				}
			}
		}
	}
	return
}
