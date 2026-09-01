package windowhost

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// fakeLauncher records which surface the host asked for.
type fakeLauncher struct {
	renders int
	menus   int
}

func (inst *fakeLauncher) Render(ids *c.WidgetIdStack)     { inst.renders++ }
func (inst *fakeLauncher) RenderMenu(ids *c.WidgetIdStack) { inst.menus++ }

// TestSetLauncher_EmptyStateDelegates is ADR-0214 §SD2's wiring: the pane the
// host draws when nothing is open is the launcher component, not a second
// browse surface. The two used to be separate render paths kept in step by a
// shared predicate function, and they drifted — this test is what makes the
// delegation checkable rather than a comment.
func TestSetLauncher_EmptyStateDelegates(t *testing.T) {
	l := &fakeLauncher{}
	inst := NewInst(app.NewRegistry(), zerolog.Nop())
	inst.SetLauncher(l)

	ids := c.NewWidgetIdStack()
	inst.renderEmptyState(ids)
	assert.Equal(t, 1, l.renders)
	assert.Equal(t, 0, l.menus, "the pane is not the menu")

	inst.RenderAppsMenu(ids)
	assert.Equal(t, 1, l.menus)
}

// The nil-launcher fallback — the branch that renders "launcher unavailable"
// rather than dereferencing — is deliberately not unit-tested here. Both
// branches emit egui ops, and there is no widget-emitting harness in this
// package; a test would assert against the FFFI builder rather than against
// the guard. What covers it is the screenshot-tour boot, which constructs a
// host with no launcher, and the type system, which makes the field nilable
// exactly so that path exists.

// TestOpenAppIds_ReportsWhatTheLauncherBadgesOn covers the host half of the
// launcher's HostI (§SD3): the set behind the row's "open" badge, and the
// reason the default row action can raise rather than open a duplicate.
func TestOpenAppIds_ReportsWhatTheLauncherBadgesOn(t *testing.T) {
	reg := app.NewRegistry()
	m := app.Manifest{
		Id: "test.badged", Display: "Badged", Summary: "fixture summary",
		Surface: app.SurfaceWindowed, Topics: []app.TopicT{app.TopicRuntime},
	}
	require.NoError(t, reg.RegisterFactory(m, func() (a app.AppI, err error) {
		a = &noopApp{m: m}
		return
	}))
	inst := NewInst(reg, zerolog.Nop())
	assert.Empty(t, inst.OpenAppIds(), "nothing open yet")

	_, err := inst.Open("test.badged")
	require.NoError(t, err)
	assert.Equal(t, []app.AppIdT{"test.badged"}, inst.OpenAppIds())

	// OpenOrRaiseApp on an app that already has a window must not add one —
	// the silent double-open §SD10 names as a bug.
	require.NoError(t, inst.OpenOrRaiseApp("test.badged"))
	assert.Len(t, inst.OpenAppIds(), 1, "raise, not a second window")
}

type noopApp struct{ m app.Manifest }

func (inst *noopApp) Manifest() (m app.Manifest)                { m = inst.m; return }
func (inst *noopApp) Mount(ctx app.MountContextI) (err error)   { return }
func (inst *noopApp) Frame(ctx app.FrameContextI) (err error)   { return }
func (inst *noopApp) Unmount(ctx app.MountContextI) (err error) { return }
