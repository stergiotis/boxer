package opengine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/entrypoint"
)

// witness is a test app whose one command records the entry frame it ran
// under (ADR-0344 §SD1).
type witness struct {
	text string
	seen []entrypoint.KindE
}

type witnessSnap struct{ text string }

func witnessSet() *appops.Set[*witness, witnessSnap] {
	s := appops.NewSet(func(w *witness) witnessSnap { return witnessSnap{text: w.text} })
	s.Resource("text", "the text", func(w *witness) any { return w.text })
	appops.Command(s, app.OperationSpec{Name: "set_text", Version: 1, Summary: "replace the text",
		Effect: app.OperationEffectDocument, Writes: []string{"text"}, Agents: true},
		func(w *witness, call app.OperationCall, in setTextArgs) (appops.None, error) {
			w.seen = append(w.seen, entrypoint.Current())
			w.text = in.Text
			return appops.None{}, nil
		})
	appops.Query(s, app.OperationSpec{Name: "get_text", Version: 1, Summary: "read the text", Reads: []string{"text"}, Agents: true},
		func(sn witnessSnap, in appops.None) (textResult, error) { return textResult{Text: sn.text}, nil })
	return s
}

func newWitness(t *testing.T) (w *witness, eng *Engine) {
	set := witnessSet()
	w = &witness{text: "start"}
	eng = New(set.Catalog(), set.Bind(w))
	eng.SetAttached(true)
	runFrame(eng)
	return
}

func runFrame(eng *Engine) {
	eng.BeginFrame()
	eng.ApplyQueued()
	eng.TakeSnapshot()
	eng.EndFrame()
}

func submit(t *testing.T, eng *Engine, id string, writer string, obo *app.OnBehalfOf) {
	raw, err := buscodec.Encode(setTextArgs{Text: id})
	require.NoError(t, err)
	out := eng.Submit("get_text", opwire.CallRequest{V: opwire.WireVersion, CallId: id + "-read", Writer: writer})
	require.Equal(t, opwire.PhaseCompleted, out.Phase)
	out = eng.Submit("set_text", opwire.CallRequest{V: opwire.WireVersion, CallId: id, Args: raw,
		Expects: map[string]uint64{"text": out.Revisions["text"]}, Writer: writer, OnBehalfOf: obo})
	require.Equal(t, opwire.PhaseAccepted, out.Phase)
}

// A task's queued command runs its handler under the agentic entry frame,
// whether the dispatcher stamped an on-behalf-of context or only named the
// task as writer.
func TestQueuedTaskCommandRunsUnderTheAgenticFrame(t *testing.T) {
	w, eng := newWitness(t)
	assert.Equal(t, entrypoint.KindNone, entrypoint.Current(), "the test itself is under no frame")
	submit(t, eng, "a", opwire.WriterTask("t1"), &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "a"})
	runFrame(eng)
	submit(t, eng, "b", opwire.WriterTask("t1"), nil)
	runFrame(eng)
	require.Len(t, w.seen, 2)
	assert.Equal(t, entrypoint.KindAgentic, w.seen[0])
	assert.Equal(t, entrypoint.KindAgentic, w.seen[1])
	assert.Equal(t, "b", w.text)
}

// A queued command the person wrote — a catalog-routed write-back — and a
// gesture through the app's own UI run under the human frame, on the same
// engine path.
func TestPersonCommandsRunUnderTheHumanFrame(t *testing.T) {
	w, eng := newWitness(t)
	submit(t, eng, "p", opwire.WriterPerson, nil)
	runFrame(eng)
	raw, err := buscodec.Encode(setTextArgs{Text: "g"})
	require.NoError(t, err)
	_, err = eng.Gesture("set_text", raw)
	require.NoError(t, err)
	require.Len(t, w.seen, 2)
	assert.Equal(t, entrypoint.KindHuman, w.seen[0])
	assert.Equal(t, entrypoint.KindHuman, w.seen[1])
	assert.Equal(t, "g", w.text)
}

// The bracket (ADR-0343 §SD5) encloses the frame, not the other way round:
// Begin and End run under no frame, the handler inside runs under one.
func TestBracketEnclosesTheEntryFrame(t *testing.T) {
	w, eng := newWitness(t)
	b := &frameBracket{}
	eng.SetApplyBracket(b)
	submit(t, eng, "a", opwire.WriterTask("t1"), nil)
	runFrame(eng)
	require.Len(t, w.seen, 1)
	assert.Equal(t, entrypoint.KindAgentic, w.seen[0])
	assert.Equal(t, []entrypoint.KindE{entrypoint.KindNone, entrypoint.KindNone}, b.seen)
}

type frameBracket struct{ seen []entrypoint.KindE }

func (b *frameBracket) Begin(uint64) { b.seen = append(b.seen, entrypoint.Current()) }
func (b *frameBracket) End(Applied)  { b.seen = append(b.seen, entrypoint.Current()) }
