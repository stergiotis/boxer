package opengine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// linkCall submits a call an authorless link made: the person's click, a
// link origin, no task and no expected revision (ADR-0409 §SD5).
func (inst *harness) linkCall(op string, args any) {
	inst.n++
	raw, err := buscodec.Encode(args)
	require.NoError(inst.t, err)
	inst.eng.Submit(inst.t.Context(), op, opwire.CallRequest{V: opwire.WireVersion, CallId: string(rune('a' + inst.n)), Args: raw,
		Writer: opwire.WriterPerson, Link: &app.LinkOrigin{Level: 3, Surface: "chat"}})
}

func TestAnAuthorlessLinkCallExpectsNoRevision(t *testing.T) {
	h := newHarness(t)
	h.linkCall("set_text", setTextArgs{Text: "from a link"})
	h.frame(nil)
	st, _ := h.eng.Status(string(rune('a' + h.n)))
	assert.NotEqual(t, opwire.PhaseConflict, st.Phase, st.Reason)
	assert.Equal(t, "from a link", h.ed.text)
}

func TestAnAuthorlessLinkCallStillYieldsToThePersonEditing(t *testing.T) {
	h := newHarness(t)
	h.ed.focused = true
	h.linkCall("set_text", setTextArgs{Text: "from a link"})
	h.frame(nil)
	st, _ := h.eng.Status(string(rune('a' + h.n)))
	assert.Equal(t, opwire.PhaseConflict, st.Phase)
	assert.Contains(t, st.Reason, "editing")
	assert.Equal(t, "start", h.ed.text)
}
