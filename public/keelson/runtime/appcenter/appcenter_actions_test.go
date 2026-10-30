package appcenter

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// fakeHost records what the Running page asked of the window host.
type fakeHost struct {
	raised   []windowhost.WindowKeyT
	closed   []windowhost.WindowKeyT
	reasons  []string
	raiseErr error
}

func (inst *fakeHost) Raise(key windowhost.WindowKeyT) error {
	inst.raised = append(inst.raised, key)
	return inst.raiseErr
}

func (inst *fakeHost) Close(key windowhost.WindowKeyT, reason string) {
	inst.closed = append(inst.closed, key)
	inst.reasons = append(inst.reasons, reason)
}

var _ HostI = (*windowhost.Inst)(nil)

func TestSetHost(t *testing.T) {
	prev := hostOf()
	t.Cleanup(func() { SetHost(prev) })
	h := &fakeHost{}
	SetHost(h)
	assert.Same(t, h, hostOf())
	SetHost(nil)
	assert.Nil(t, hostOf(), "without a host the page shows no actions")
}

func TestRaiseSaysWhyItWasRefused(t *testing.T) {
	inst := newApp()
	h := &fakeHost{}
	inst.raiseWindow(h, 4)
	assert.Equal(t, []windowhost.WindowKeyT{4}, h.raised)
	assert.Empty(t, inst.actionNote)
	h.raiseErr = errors.New("windowhost: no open window with this key")
	inst.raiseWindow(h, 9)
	assert.Contains(t, inst.actionNote, "no open window")
	h.raiseErr = nil
	inst.raiseWindow(h, 4)
	assert.Empty(t, inst.actionNote, "the next action clears the note")
}

// Close asks first, closes the window it was armed for, and the row says
// "closing" until a read no longer lists the window.
func TestCloseIsArmedByKeyAndClosesThatWindow(t *testing.T) {
	inst := newApp()
	h := &fakeHost{}
	w, f, tk := runningFixture()
	v := assembleRunning(&w, &f, &tk)

	inst.armClose(1)
	assert.Empty(t, h.closed, "arming closes nothing")
	// A re-read reorders the rows; the confirmation stays with window 1.
	v.rows[0], v.rows[1] = v.rows[1], v.rows[0]
	inst.pruneClosing(&v)
	assert.EqualValues(t, 1, inst.closeArmed)

	inst.closeWindow(h, inst.closeArmed)
	require.Equal(t, []windowhost.WindowKeyT{1}, h.closed)
	assert.Equal(t, []string{closeReason}, h.reasons)
	assert.Zero(t, inst.closeArmed)
	assert.True(t, inst.closing[1])

	inst.pruneClosing(&v)
	assert.True(t, inst.closing[1], "still listed: still closing")
	w.Key, w.AppId, w.Display, w.Title, w.Kind, w.Active, w.AgentTasks = w.Key[1:], w.AppId[1:], w.Display[1:], w.Title[1:], w.Kind[1:], w.Active[1:], w.AgentTasks[1:]
	v = assembleRunning(&w, &f, &tk)
	inst.pruneClosing(&v)
	assert.False(t, inst.closing[1], "gone from the read: forgotten")

	inst.armClose(1)
	inst.pruneClosing(&v)
	assert.Zero(t, inst.closeArmed, "a confirmation for a window that closed meanwhile is dropped")
}
