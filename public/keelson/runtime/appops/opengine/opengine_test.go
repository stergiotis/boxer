package opengine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// editor is a test app: one text resource bound to a widget, and a counter
// the app's own frame logic moves.
type editor struct {
	text     string
	focused  bool
	ticks    int
	disabled bool
}

type editorSnap struct {
	text     string
	disabled bool
}

type setTextArgs struct{ Text string }
type textResult struct{ Text string }

func editorSet() *appops.Set[*editor, editorSnap] {
	s := appops.NewSet(func(e *editor) editorSnap { return editorSnap{text: e.text, disabled: e.disabled} })
	s.Resource("text", "the text", func(e *editor) any { return e.text })
	s.Resource("ticks", "a counter the app moves", func(e *editor) any { return e.ticks })
	s.Editing("text", func(e *editor) bool { return e.focused })
	appops.Command(s, app.OperationSpec{Name: "set_text", Version: 1, Summary: "replace the text",
		Effect: app.OperationEffectDocument, Writes: []string{"text"}, Agents: true},
		func(e *editor, call app.OperationCall, in setTextArgs) (appops.None, error) {
			e.text = in.Text
			return appops.None{}, nil
		})
	appops.Query(s, app.OperationSpec{Name: "get_text", Version: 1, Summary: "read the text", Reads: []string{"text"}, Agents: true},
		func(sn editorSnap, in appops.None) (textResult, error) { return textResult{Text: sn.text}, nil })
	s.Available("set_text", func(sn editorSnap) (bool, string) { return !sn.disabled, "the editor is read-only" })
	return s
}

type harness struct {
	t   *testing.T
	ed  *editor
	eng *Engine
	n   int
}

func newHarness(t *testing.T) *harness {
	set := editorSet()
	ed := &editor{text: "start"}
	h := &harness{t: t, ed: ed, eng: New(set.Catalog(), set.Bind(ed))}
	h.frame(nil) // the first frame records the starting values
	return h
}

// frame runs one frame the way the window host does; during runs inside the
// app's Frame, writeBack after it, standing for the person's input that the
// frame's sync writes back.
func (inst *harness) frame(during func()) {
	inst.eng.BeginFrame()
	inst.eng.ApplyQueued()
	inst.eng.TakeSnapshot()
	if during != nil {
		during()
	}
	inst.eng.EndFrame()
}

func (inst *harness) call(op string, args any, expects map[string]uint64) opwire.Outcome {
	inst.n++
	var raw []byte
	if args != nil {
		var err error
		raw, err = buscodec.Encode(args)
		require.NoError(inst.t, err)
	}
	return inst.eng.Submit(op, opwire.CallRequest{V: opwire.WireVersion, CallId: string(rune('a' + inst.n)), Args: raw,
		Expects: expects, Writer: opwire.WriterTask("t1")})
}

func (inst *harness) read() (text string, rev uint64) {
	out := inst.call("get_text", nil, nil)
	require.Equal(inst.t, opwire.PhaseCompleted, out.Phase)
	res, err := buscodec.Decode[textResult](out.Result)
	require.NoError(inst.t, err)
	return res.Text, out.Revisions["text"]
}

func TestCommandAppliesAfterTheFrameAndRendersInTheNext(t *testing.T) {
	h := newHarness(t)
	text, rev := h.read()
	assert.Equal(t, "start", text)
	out := h.call("set_text", setTextArgs{Text: "agent"}, map[string]uint64{"text": rev})
	assert.Equal(t, opwire.PhaseAccepted, out.Phase)
	assert.Equal(t, "start", h.ed.text, "nothing applies off the render goroutine")

	h.frame(nil)
	assert.Equal(t, "agent", h.ed.text)
	st, ok := h.eng.Status(string(rune('a' + h.n)))
	require.True(t, ok)
	assert.Equal(t, opwire.PhaseApplied, st.Phase)
	assert.Equal(t, rev+1, st.Revisions["text"])
	assert.Equal(t, opwire.WriterTask("t1"), h.eng.Writer("text"))

	h.frame(nil)
	st, _ = h.eng.Status(string(rune('a' + h.n)))
	assert.Equal(t, opwire.PhaseRendered, st.Phase)
	text, _ = h.read()
	assert.Equal(t, "agent", text)
}

// The person wins a tie: their change of frame N lands in the write-back,
// before the queued command is checked in frame N+1.
func TestSameFramePersonChangeWinsAndTheCommandConflicts(t *testing.T) {
	h := newHarness(t)
	_, rev := h.read()
	h.call("set_text", setTextArgs{Text: "agent"}, map[string]uint64{"text": rev})
	callId := string(rune('a' + h.n))
	h.ed.text = "person" // the write-back at the end of the frame the command was queued in
	h.frame(nil)
	assert.Equal(t, "person", h.ed.text)
	st, _ := h.eng.Status(callId)
	assert.Equal(t, opwire.PhaseConflict, st.Phase)
	assert.Equal(t, rev+1, st.Revisions["text"], "a conflict carries the current revision")
	assert.Equal(t, opwire.WriterPerson, h.eng.Writer("text"))
}

func TestCommandToAResourceBeingEditedConflicts(t *testing.T) {
	h := newHarness(t)
	_, rev := h.read()
	h.ed.focused = true
	h.call("set_text", setTextArgs{Text: "agent"}, map[string]uint64{"text": rev})
	h.frame(nil)
	st, _ := h.eng.Status(string(rune('a' + h.n)))
	assert.Equal(t, opwire.PhaseConflict, st.Phase)
	assert.Contains(t, st.Reason, "editing")
	assert.Equal(t, "start", h.ed.text)
}

func TestCommandWithoutAnExpectationAsksToReadFirst(t *testing.T) {
	h := newHarness(t)
	h.call("set_text", setTextArgs{Text: "agent"}, nil)
	h.frame(nil)
	st, _ := h.eng.Status(string(rune('a' + h.n)))
	assert.Equal(t, opwire.PhaseConflict, st.Phase)
	assert.Contains(t, st.Reason, "read first")
}

func TestUnavailableCommandIsRefused(t *testing.T) {
	h := newHarness(t)
	_, rev := h.read()
	h.ed.disabled = true
	h.frame(nil) // the snapshot now says read-only
	h.call("set_text", setTextArgs{Text: "agent"}, map[string]uint64{"text": rev})
	h.frame(nil)
	st, _ := h.eng.Status(string(rune('a' + h.n)))
	assert.Equal(t, opwire.PhaseRefused, st.Phase)
	assert.Equal(t, "the editor is read-only", st.Reason)
}

func TestCancelAndExpireOnlyTouchQueuedCommands(t *testing.T) {
	h := newHarness(t)
	_, rev := h.read()
	h.call("set_text", setTextArgs{Text: "x"}, map[string]uint64{"text": rev})
	first := string(rune('a' + h.n))
	h.call("set_text", setTextArgs{Text: "y"}, map[string]uint64{"text": rev})
	second := string(rune('a' + h.n))
	out, ok := h.eng.Cancel(first)
	require.True(t, ok)
	assert.Equal(t, opwire.PhaseCancelled, out.Phase)
	h.eng.Expire(nil, "task stopped")
	st, _ := h.eng.Status(second)
	assert.Equal(t, opwire.PhaseExpired, st.Phase)
	h.frame(nil)
	assert.Equal(t, "start", h.ed.text)
	assert.False(t, h.eng.Busy())
}

func TestTheAppsOwnChangesAndGesturesAreAttributed(t *testing.T) {
	h := newHarness(t)
	h.frame(func() { h.ed.ticks++ })
	assert.Equal(t, opwire.WriterApp, h.eng.Writer("ticks"))
	h.frame(func() {
		args, err := buscodec.Encode(setTextArgs{Text: "by hand"})
		require.NoError(t, err)
		_, err = h.eng.Gesture("set_text", args)
		require.NoError(t, err)
	})
	assert.Equal(t, opwire.WriterPerson, h.eng.Writer("text"))
	log := h.eng.Log()
	require.NotEmpty(t, log)
	last := log[len(log)-1]
	assert.Equal(t, "set_text", last.Op)
	assert.Equal(t, opwire.WriterPerson, last.Writer)
}

func TestQueryBeforeTheFirstSnapshotIsRefused(t *testing.T) {
	set := editorSet()
	eng := New(set.Catalog(), set.Bind(&editor{}))
	out := eng.Submit("get_text", opwire.CallRequest{CallId: "q"})
	assert.Equal(t, opwire.PhaseRefused, out.Phase)
	out = eng.Submit("nope", opwire.CallRequest{CallId: "n"})
	assert.Equal(t, opwire.PhaseRefused, out.Phase)
}

func TestSameComparesUncomparableValuesDeeply(t *testing.T) {
	assert.True(t, same([]int{1}, []int{1}))
	assert.False(t, same([]int{1}, []int{2}))
	assert.True(t, same("a", "a"))
	assert.False(t, same("a", 1))
	assert.True(t, same(nil, nil))
	assert.True(t, same(struct{ A any }{A: []int{1}}, struct{ A any }{A: []int{1}}))
}
