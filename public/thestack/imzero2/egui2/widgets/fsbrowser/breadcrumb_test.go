package fsbrowser

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// crumbHandle derives trail item i's button handle the way the breadcrumbs
// widget does under the "crumbs" scope renderBreadcrumb gives it.
func crumbHandle(ids *c.WidgetIdStack, i int) (h widgethandle.WidgetHandle) {
	for range c.IdScope(ids.PrepareStr("crumbs")) {
		for range c.IdScope(ids.PrepareSeq(uint64(i))) {
			h = widgethandle.Make(ids.PrepareStr("item").Derive())
		}
	}
	return
}

// A click on a trail item goes to the directory it names: the root, or the
// path up to that segment.
func TestATrailClickGoesToThatDirectory(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	ids := c.NewWidgetIdStack()
	in := Input{Ids: ids}
	var st State
	st.SetDir("a/b/c")

	click := func(i int) bool {
		sm.ScriptReset()
		sm.ScriptResponse(crumbHandle(ids, i), c.PrimaryClickedResponseFlags)
		return in.renderBreadcrumb(&st, styletokens.DensityStandard)
	}

	assert.False(t, click(3), "the current directory is not a link")
	assert.Equal(t, "a/b/c", st.Dir())
	assert.True(t, click(2))
	assert.Equal(t, "a/b", st.Dir())
	assert.True(t, click(0))
	assert.Equal(t, ".", st.Dir())
}
