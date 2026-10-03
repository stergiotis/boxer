package breadcrumbs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

func steps() *Model {
	return &Model{
		Labels: []string{"Connect", "Databases", "Structure", "Sync"},
		Done:   []bool{true, true, false, false},
		Locked: []string{"", "", "", "compare the structure first"},
	}
}

// scene renders a trail with no host behind it, one scripted frame at a time.
type scene struct {
	sm    *c.StateManager
	ids   *c.WidgetIdStack
	model *Model
	state *State
}

const sceneKey = "scene"

func newScene(t *testing.T, m *Model) (s *scene) {
	t.Cleanup(scenetest.Install())
	s = &scene{sm: c.CurrentApplicationState.StateManager, ids: c.NewWidgetIdStack(), model: m, state: &State{}}
	return
}

// handle derives item i's button handle the way Render does.
func (s *scene) handle(i int) (h widgethandle.WidgetHandle) {
	for range c.IdScope(s.ids.PrepareStr(sceneKey)) {
		for range c.IdScope(s.ids.PrepareSeq(uint64(i))) {
			h = widgethandle.Make(s.ids.PrepareStr("item").Derive())
		}
	}
	return
}

// frame plays one frame in which the items in clicked were clicked.
func (s *scene) frame(clicked ...int) Result {
	s.sm.ScriptReset()
	for _, i := range clicked {
		s.sm.ScriptResponse(s.handle(i), c.PrimaryClickedResponseFlags)
	}
	return Render(Input{Ids: s.ids, ScopeKey: sceneKey, Model: s.model, State: s.state})
}

func TestAZeroStateDrawsTheLastItemCurrent(t *testing.T) {
	s := newScene(t, &Model{Labels: []string{"/", "home", "docs"}})
	res := s.frame()
	require.NoError(t, res.Err)
	assert.EqualValues(t, -1, res.Clicked)
	assert.EqualValues(t, -1, s.state.Current())
	assert.Equal(t, 2, s.state.resolve(3))
}

func TestAClickMovesTheCurrentItem(t *testing.T) {
	s := newScene(t, steps())
	s.state.SetCurrent(2)
	res := s.frame(0)
	assert.EqualValues(t, 0, res.Clicked)
	assert.EqualValues(t, 0, s.state.Current())
}

func TestTheCurrentAndLockedItemsAreNotReported(t *testing.T) {
	s := newScene(t, steps())
	s.state.SetCurrent(2)
	assert.EqualValues(t, -1, s.frame(2).Clicked, "the current item")
	assert.EqualValues(t, -1, s.frame(3).Clicked, "a locked item")
	assert.EqualValues(t, 2, s.state.Current())
}

func TestABrokenInputIsReported(t *testing.T) {
	s := newScene(t, &Model{Labels: []string{"a", "b"}, Done: []bool{true}})
	assert.Error(t, s.frame().Err)
	s.model, s.state = steps(), nil
	assert.Error(t, s.frame().Err)
}

func TestSetCurrentOutOfRangeFallsBackToTheLast(t *testing.T) {
	var st State
	st.SetCurrent(9)
	assert.Equal(t, 3, st.resolve(4))
	st.SetCurrent(-5)
	assert.EqualValues(t, -1, st.Current())
}
