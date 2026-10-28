package entrypoint

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The frame is on the stack for the whole call and names the kind.
func TestCurrentSeesTheEnclosingFrame(t *testing.T) {
	assert.Equal(t, KindNone, Current(), "a test has no entry frame")
	var inAgent, inHuman KindE
	Agentic(func() { inAgent = Current() })
	Human(func() { inHuman = Current() })
	assert.Equal(t, KindAgentic, inAgent)
	assert.Equal(t, KindHuman, inHuman)
	assert.Equal(t, KindNone, Current(), "nothing remains after the call")
}

// A frame deeper in the stack is still found, and the innermost wins when
// both are present.
func TestInnermostFrameWins(t *testing.T) {
	var deep, nested KindE
	Agentic(func() { deep = viaHelper() })
	Human(func() { Agentic(func() { nested = Current() }) })
	assert.Equal(t, KindAgentic, deep)
	assert.Equal(t, KindAgentic, nested)
}

func viaHelper() KindE { return Current() }

// A goroutine the handler starts does not inherit the frame: the mark is
// per goroutine, as the package says.
func TestAGoroutineDoesNotInherit(t *testing.T) {
	got := make(chan KindE, 1)
	Agentic(func() {
		go func() { got <- Current() }()
	})
	assert.Equal(t, KindNone, <-got)
}

// A formatted trace classifies by the frame's printed name, so a panic
// message or a goroutine dump reads the same way Current does.
func TestOfTraceReadsAFormattedStack(t *testing.T) {
	var agentic, human, none []byte
	Agentic(func() { agentic = debug.Stack() })
	Human(func() { human = debug.Stack() })
	none = debug.Stack()
	assert.Equal(t, KindAgentic, OfTrace(agentic))
	assert.Equal(t, KindHuman, OfTrace(human))
	assert.Equal(t, KindNone, OfTrace(none))
	assert.Contains(t, string(agentic), FuncAgentic, "the documented name is the one a trace prints")
}

// For hands back the frame a boolean asks for.
func TestForPicksTheFrame(t *testing.T) {
	var a, h KindE
	For(true)(func() { a = Current() })
	For(false)(func() { h = Current() })
	assert.Equal(t, KindAgentic, a)
	assert.Equal(t, KindHuman, h)
	assert.Equal(t, "agentic", KindAgentic.String())
	assert.Equal(t, "human", KindHuman.String())
	assert.Equal(t, "none", KindNone.String())
}
