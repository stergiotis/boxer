package decision

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

func TestKindFollowsOriginRoleDestroysAndCaution(t *testing.T) {
	type row struct {
		origin    OriginE
		opt       Option
		cautioned bool
		want      c.ButtonKindE
	}
	for _, r := range []row{
		// A request: the affirmative leads, the decline is a plain button.
		{OriginRequest, Option{Role: RoleAffirm}, false, c.ButtonKindPrimary},
		{OriginRequest, Option{Role: RoleDecline}, false, c.ButtonKindSecondary},
		{OriginRequest, Option{Role: RoleOther}, false, c.ButtonKindGhost},
		// A request under a note of caution: nothing is primary.
		{OriginRequest, Option{Role: RoleAffirm}, true, c.ButtonKindSecondary},
		{OriginRequest, Option{Role: RoleDecline}, true, c.ButtonKindSecondary},
		// A confirmation: the affirmative leads, cautioned or not.
		{OriginConfirm, Option{Role: RoleAffirm}, false, c.ButtonKindPrimary},
		{OriginConfirm, Option{Role: RoleAffirm}, true, c.ButtonKindPrimary},
		{OriginConfirm, Option{Role: RoleDecline}, false, c.ButtonKindSecondary},
		{OriginConfirm, Option{Role: RoleOther}, false, c.ButtonKindGhost},
		// What destroys is Danger in either origin.
		{OriginRequest, Option{Role: RoleAffirm, Destroys: true}, false, c.ButtonKindDanger},
		{OriginRequest, Option{Role: RoleAffirm, Destroys: true}, true, c.ButtonKindDanger},
		{OriginConfirm, Option{Role: RoleAffirm, Destroys: true}, false, c.ButtonKindDanger},
		{OriginConfirm, Option{Role: RoleDecline, Destroys: true}, false, c.ButtonKindDanger},
		{OriginRequest, Option{Role: RoleOther, Destroys: true}, false, c.ButtonKindDangerGhost},
		// Disabled changes nothing about the kind.
		{OriginConfirm, Option{Role: RoleAffirm, Disabled: true, Why: "x"}, false, c.ButtonKindPrimary},
	} {
		assert.Equal(t, r.want, kindOf(r.origin, r.opt, r.cautioned), "origin %d role %d destroys %v cautioned %v", r.origin, r.opt.Role, r.opt.Destroys, r.cautioned)
	}
}

func TestOnlyAWarningOrAnErrorNoteCautions(t *testing.T) {
	for tone, want := range map[styletokens.Tone]bool{
		styletokens.ToneNeutral: false, styletokens.ToneInfo: false, styletokens.ToneSuccess: false,
		styletokens.ToneWarning: true, styletokens.ToneError: true,
	} {
		in := Input{Notes: []Note{{Text: "n", Tone: tone}}}
		assert.Equal(t, want, in.cautioned(), "tone %d", tone)
	}
	assert.False(t, Input{}.cautioned())
}

func TestADeclineIsNeverWeakerThanAPlainButton(t *testing.T) {
	for _, origin := range []OriginE{OriginRequest, OriginConfirm} {
		for _, destroys := range []bool{false, true} {
			k := kindOf(origin, Option{Role: RoleDecline, Destroys: destroys}, false)
			assert.NotEqual(t, c.ButtonKindGhost, k)
			assert.NotEqual(t, c.ButtonKindDangerGhost, k)
		}
	}
}

func valid() Input {
	return Input{
		Ids:   c.NewWidgetIdStack(),
		Title: "Export the artefact?",
		Options: []Option{
			{Key: "yes", Label: "Export", Role: RoleAffirm},
			{Key: "no", Label: "Cancel", Role: RoleDecline},
		},
	}
}

func TestValidate(t *testing.T) {
	require.NoError(t, valid().Validate())

	broken := map[string]func(in *Input){
		"no ids":          func(in *Input) { in.Ids = nil },
		"empty title":     func(in *Input) { in.Title = "  " },
		"no options":      func(in *Input) { in.Options = nil },
		"unknown origin":  func(in *Input) { in.Origin = 7 },
		"empty key":       func(in *Input) { in.Options[1].Key = "" },
		"duplicate key":   func(in *Input) { in.Options[1].Key = "yes" },
		"empty label":     func(in *Input) { in.Options[0].Label = "" },
		"unknown role":    func(in *Input) { in.Options[1].Role = 9 },
		"two affirmative": func(in *Input) { in.Options[1].Role = RoleAffirm },
		"disabled no why": func(in *Input) { in.Options[0].Disabled = true },
	}
	for name, breakIt := range broken {
		in := valid()
		in.Options = append([]Option(nil), in.Options...)
		breakIt(&in)
		assert.Error(t, in.Validate(), name)
	}

	in := valid()
	in.Options[0].Disabled, in.Options[0].Why = true, "The artefact moved since you asked."
	in.Options = append(in.Options, Option{Key: "later", Label: "Later", Role: RoleOther})
	assert.NoError(t, in.Validate(), "a disabled option with its reason, and a third option")
}

// scene renders a decision with no host behind it, one scripted frame at a
// time.
type scene struct {
	sm *c.StateManager
	in Input
}

const sceneKey = "scene"

func newScene(t *testing.T) (s *scene) {
	t.Cleanup(egui2test.Install())
	s = &scene{sm: c.CurrentApplicationState.StateManager, in: valid()}
	s.in.ScopeKey = sceneKey
	return
}

// handle derives option key's button handle the way Render does: the frame
// pushes its id, so an unframed decision's buttons have other ids.
func (s *scene) handle(key string) (h widgethandle.WidgetHandle) {
	ids := s.in.Ids
	derive := func() {
		for range c.IdScope(ids.PrepareStr("options")) {
			h = widgethandle.Make(ids.PrepareStr(key).Derive())
		}
	}
	for range c.IdScope(ids.PrepareStr(sceneKey)) {
		if s.in.Unframed {
			derive()
			continue
		}
		for range c.IdScope(ids.PrepareStr("frame")) {
			derive()
		}
	}
	return
}

// frame plays one frame in which the options in clicked were clicked.
func (s *scene) frame(clicked ...string) Result {
	s.sm.ScriptReset()
	for _, k := range clicked {
		s.sm.ScriptResponse(s.handle(k), c.PrimaryClickedResponseFlags)
	}
	return Render(s.in)
}

func TestAFrameWithNoClickChoosesNothing(t *testing.T) {
	s := newScene(t)
	res := s.frame()
	require.NoError(t, res.Err)
	assert.False(t, res.Has)
	assert.Empty(t, res.Chosen)
}

func TestAClickIsReportedByKey(t *testing.T) {
	s := newScene(t)
	res := s.frame("no")
	require.NoError(t, res.Err)
	assert.True(t, res.Has)
	assert.Equal(t, "no", res.Chosen)
}

func TestADisabledOptionIsNotChosen(t *testing.T) {
	s := newScene(t)
	s.in.Options[0].Disabled, s.in.Options[0].Why = true, "The artefact moved since you asked."
	res := s.frame("yes")
	require.NoError(t, res.Err)
	assert.False(t, res.Has)
}

func TestEverySlotDrawsAndTheSlotsRunInOrder(t *testing.T) {
	s := newScene(t)
	var order []string
	s.in.Subject = "revision 3"
	s.in.Follows = []string{"Anyone who can read the store can read it."}
	s.in.Notes = []Note{{Text: "The artefact moved since you asked.", Tone: styletokens.ToneWarning}}
	s.in.Form = func() { order = append(order, "form") }
	s.in.Detail = func() { order = append(order, "detail") }
	s.in.Options[0].Tooltip = "writes one snapshot"
	s.in.Options[0].Icon = "*"
	res := s.frame("yes")
	require.NoError(t, res.Err)
	assert.Equal(t, "yes", res.Chosen)
	assert.Equal(t, []string{"form", "detail"}, order)

	s.in.Unframed = true
	order = order[:0]
	assert.Equal(t, "yes", s.frame("yes").Chosen, "the same ids without the frame")
}

func TestABrokenInputIsReportedAndDrawsNoSlot(t *testing.T) {
	s := newScene(t)
	called := false
	s.in.Form = func() { called = true }
	s.in.Options = nil
	assert.Error(t, s.frame().Err)
	assert.False(t, called)

	s.in = valid()
	s.in.Ids = nil
	assert.Error(t, s.frame().Err, "no stack still draws the message")
}
