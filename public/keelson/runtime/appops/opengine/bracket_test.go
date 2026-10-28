package opengine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// recordingBracket notes every Begin and End it sees.
type recordingBracket struct {
	begins []uint64
	ends   []Applied
}

func (b *recordingBracket) Begin(frame uint64) { b.begins = append(b.begins, frame) }
func (b *recordingBracket) End(a Applied)      { b.ends = append(b.ends, a) }

// The bracket surrounds the handler call of a command that runs, with what
// the command was (ADR-0343 §SD5).
func TestBracketSurroundsTheHandlerCall(t *testing.T) {
	h := newHarness(t)
	b := &recordingBracket{}
	h.eng.SetApplyBracket(b)
	_, rev := h.read()
	h.call("set_text", setTextArgs{Text: "agent"}, map[string]uint64{"text": rev})
	h.frame(nil)
	require.Len(t, b.begins, 1)
	require.Len(t, b.ends, 1)
	assert.Equal(t, b.begins[0], b.ends[0].Frame)
	assert.Equal(t, "set_text", b.ends[0].Operation)
	assert.Equal(t, opwire.PhaseApplied, b.ends[0].Phase)
	assert.Equal(t, opwire.WriterTask("t1"), b.ends[0].Writer)
	assert.Equal(t, string(rune('a'+h.n)), b.ends[0].CallId)
	assert.Equal(t, "agent", h.ed.text)
}

// A command whose apply ends before the handler runs — a conflict, an
// unavailable operation — is not bracketed: no app code ran.
func TestBracketIsSilentWhenTheHandlerDoesNotRun(t *testing.T) {
	h := newHarness(t)
	b := &recordingBracket{}
	h.eng.SetApplyBracket(b)
	_, rev := h.read()
	h.call("set_text", setTextArgs{Text: "stale"}, map[string]uint64{"text": rev + 1})
	h.frame(nil)
	assert.Empty(t, b.begins, "a conflict runs no handler")

	h.ed.disabled = true
	h.frame(nil)
	_, rev = h.read()
	h.call("set_text", setTextArgs{Text: "refused"}, map[string]uint64{"text": rev})
	h.frame(nil)
	assert.Empty(t, b.begins, "an unavailable operation runs no handler")
	assert.Equal(t, "start", h.ed.text)
}

// Two commands in one frame are two brackets, in order.
func TestEachCommandOfAFrameHasItsOwnBracket(t *testing.T) {
	h := newHarness(t)
	b := &recordingBracket{}
	h.eng.SetApplyBracket(b)
	_, rev := h.read()
	h.call("set_text", setTextArgs{Text: "one"}, map[string]uint64{"text": rev})
	h.call("set_text", setTextArgs{Text: "two"}, map[string]uint64{"text": rev + 1})
	h.frame(nil)
	require.Len(t, b.ends, 2)
	assert.Equal(t, b.ends[0].Frame, b.ends[1].Frame)
	assert.NotEqual(t, b.ends[0].CallId, b.ends[1].CallId)
	assert.Equal(t, "two", h.ed.text)
}
