package runtime

import (
	"bytes"
	"fmt"
	goruntime "runtime"
	"strconv"
)

// The channel is one ordered byte stream with no locking: every message a
// frame sends, every capture scope it opens, and every reply it reads must
// come from the goroutine running the render loop (ADR-0261). A call from any
// other goroutine interleaves into the frame the loop is writing, and the
// peer reads a stream neither side wrote. Nothing in the type system stops
// it, and the race detector only sees it when a test happens to run both.
//
// BindToCurrentGoroutine turns that rule into a check. Go exposes no
// goroutine id, so the check compares goroutineToken, which on amd64 and
// arm64 reads the runtime's descriptor pointer in about a nanosecond. Other
// architectures fall back to parsing the id out of a formatted stack trace,
// which runtime.Stack symbolizes in full however small the buffer: several
// microseconds per message, growing with stack depth.

// BindToCurrentGoroutine binds the channel to the calling goroutine. From
// then on a send, a capture scope or a receive from any other goroutine
// panics on that goroutine, so the panic's stack names the caller that broke
// the rule.
func (inst *Fffi2[U]) BindToCurrentGoroutine() {
	inst.ownerId.Store(currentGoroutineId())
	inst.owner.Store(goroutineToken())
}

// Messages is the number of messages SendIntermediate has taken since the
// channel was made, captured into a deferred block or sent. The counter is
// the render goroutine's own; read it there, as a difference across a span
// of the frame, which is how the window host attributes messages to an
// app's Frame (ADR-0261).
func (inst *Fffi2[U]) Messages() uint64 {
	return inst.msgs
}

func (inst *Fffi2[U]) checkOwner() {
	owner := inst.owner.Load()
	if owner == 0 {
		return
	}
	if g := goroutineToken(); g != 0 && g != owner {
		// The ids are formatted only here, off the per-message path.
		panic(fmt.Sprintf("fffi2: channel used from goroutine %d; it is bound to the render goroutine %d (ADR-0261)", currentGoroutineId(), inst.ownerId.Load()))
	}
}

// currentGoroutineId parses the calling goroutine's id from the header
// runtime.Stack writes ("goroutine 18 [running]:"), or 0 when the header
// does not parse. It names goroutines in the panic message, and is the
// token where goroutineToken has no assembly; there, binding to 0 leaves the
// channel unbound, and checkOwner skips a caller whose token reads as 0: a
// check that cannot read the id stays silent rather than panicking on a
// correct caller.
func currentGoroutineId() uint64 {
	var buf [64]byte
	b := buf[:goruntime.Stack(buf[:], false)]
	b, ok := bytes.CutPrefix(b, []byte("goroutine "))
	if !ok {
		return 0
	}
	if i := bytes.IndexByte(b, ' '); i >= 0 {
		b = b[:i]
	}
	id, err := strconv.ParseUint(string(b), 10, 64)
	if err != nil {
		return 0
	}
	return id
}
