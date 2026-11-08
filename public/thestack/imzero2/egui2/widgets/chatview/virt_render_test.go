package chatview

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// longModel is a dialogue of n short messages, two senders alternating a
// minute apart.
func longModel(n int) *Model {
	base := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC).UnixMilli()
	m := &Model{Participants: []Participant{{Name: "Ada"}, {Name: "Bob"}}}
	for i := range n {
		m.TimeMS = append(m.TimeMS, base+int64(i)*minute)
		m.Sender = append(m.Sender, int32(i%2))
		m.Body = append(m.Body, "message "+strconv.Itoa(i))
		m.ReplyTo = append(m.ReplyTo, -1)
		m.Flags = append(m.Flags, 0)
		m.Status = append(m.Status, 0)
	}
	return m
}

// renderCounting renders one frame and returns the ordinals whose body the
// widget asked the host to draw — the rows it laid out.
func renderCounting(t *testing.T, m *Model, st *State) (drawn []int) {
	t.Helper()
	c.CurrentApplicationState.StateManager.ScriptReset()
	ids := c.NewWidgetIdStack()
	Render(Input{
		Ids: ids, ScopeKey: "t", Model: m, State: st, Viewer: 0, FillHost: true,
		Block: func(ordinal int) (Block, bool) {
			drawn = append(drawn, ordinal)
			return Block{Render: func() {}}, true
		},
	})
	return
}

// The cost M5 removes: a 300-message window following its tail lays out
// only the rows near the tail, not every message of the window.
func TestRenderLaysOutOnlyRowsNearTheViewport(t *testing.T) {
	t.Cleanup(scenetest.Install())
	m := longModel(1000)
	var st State
	drawn := renderCounting(t, m, &st)
	require.NotEmpty(t, drawn)
	assert.Equal(t, m.Len()-1, drawn[len(drawn)-1], "the tail is drawn")
	assert.LessOrEqual(t, len(drawn), 60, "a %d-message window drew %d rows", st.Window(), len(drawn))
	t.Logf("a %d-message window drew %d rows", st.Window(), len(drawn))
}

// A jump to a message far above the tail lays that message out, so the
// scroll can be asked from its position, while the rows between stand in
// as space.
func TestRenderJumpLaysOutItsTarget(t *testing.T) {
	t.Cleanup(scenetest.Install())
	m := longModel(1000)
	var st State
	renderCounting(t, m, &st)
	st.JumpTo(10)
	drawn := renderCounting(t, m, &st)
	assert.Contains(t, drawn, 10, "the jump's target is laid out")
	assert.LessOrEqual(t, len(drawn), 80)
	assert.False(t, st.Follow(), "a jump releases the tail")
	assert.Equal(t, 1000-10, st.Window(), "a jump older than the window widens it")
}
