package appcenter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// fakeHost records what the Running tab asked of the window host.
type fakeHost struct {
	closed  []windowhost.WindowKeyT
	reasons []string
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
	assert.Nil(t, hostOf(), "without a host the tab shows no Close all")
}

// Close all closes the windows shown when it was armed, this window last,
// and the rows say "closing" until a read no longer lists them.
func TestCloseAllClosesWhatWasShownWhenArmed(t *testing.T) {
	inst := newApp()
	inst.selfKey = 3
	h := &fakeHost{}
	w, f, tk := runningFixture()
	v := assembleRunning(&w, &f, &tk)
	all := []int64{0, 1, 2}

	keys := inst.closable(&v, []int64{2, 0, 1})
	assert.Equal(t, []uint64{2, 1, 3}, keys, "the shown windows in the order shown, this window last")
	assert.Equal(t, []uint64{3}, inst.closable(&v, []int64{2}), "this window alone is closable too")
	assert.Equal(t, []uint64{2, 1}, inst.closable(&v, []int64{0, 1}), "a filter that hides this window leaves it open")
	inst.armCloseAll(keys)
	assert.Empty(t, h.closed, "arming closes nothing")
	assert.Equal(t, "Chat, SQL, Idle", armedNames(&v, inst.closeArmed))

	inst.closeAll(h)
	require.Equal(t, []windowhost.WindowKeyT{2, 1, 3}, h.closed)
	assert.Equal(t, []string{closeReason, closeReason, closeReason}, h.reasons)
	assert.Nil(t, inst.closeArmed)
	assert.True(t, inst.closing[1])
	assert.Empty(t, inst.closable(&v, all), "a closing window is not closed twice")

	inst.pruneClosing(&v)
	assert.True(t, inst.closing[1], "still listed: still closing")
	gone := assembleRunning(&windowCols{}, &f, &tk)
	inst.pruneClosing(&gone)
	assert.Empty(t, inst.closing, "gone from the read: forgotten")
}

// A confirmation narrows to the windows still listed, and is dropped once
// the filter moves or nothing it named is left.
func TestCloseAllConfirmationIsHeldByKeyAndFilter(t *testing.T) {
	inst := newApp()
	w, f, tk := runningFixture()
	v := assembleRunning(&w, &f, &tk)

	inst.runFilter = "play"
	inst.armCloseAll([]uint64{1, 2})
	inst.pruneClosing(&v)
	assert.Equal(t, []uint64{1, 2}, inst.closeArmed)

	only1 := assembleRunning(&windowCols{Key: []int64{1}, AppId: []string{"a/play"}, Display: []string{"Play"},
		Title: []string{"SQL"}, Kind: []string{"app"}, Active: []bool{false}, AgentTasks: [][]string{nil}}, &f, &tk)
	inst.pruneClosing(&only1)
	assert.Equal(t, []uint64{1}, inst.closeArmed, "a window that closed meanwhile is dropped from it")
	assert.Equal(t, "SQL", armedNames(&only1, inst.closeArmed))

	inst.runFilter = "pla"
	inst.pruneClosing(&only1)
	assert.Nil(t, inst.closeArmed, "typing in the filter drops the confirmation")

	inst.armCloseAll([]uint64{2})
	inst.pruneClosing(&only1)
	assert.Nil(t, inst.closeArmed, "nothing it named is left")
}

func TestArmedNamesNamesTheFirstFew(t *testing.T) {
	v := runningView{rows: []runningRow{{key: 1, title: "a"}, {key: 2, title: "b"}, {key: 3, title: "c"}, {key: 4, title: "d"}, {key: 5, title: "e"}}}
	assert.Equal(t, "e, d, c and 2 more", armedNames(&v, []uint64{5, 4, 3, 2, 1}))
	assert.Equal(t, "Close 1 window", closeCount(1))
	assert.Equal(t, "Close 4 windows", closeCount(4))
}
