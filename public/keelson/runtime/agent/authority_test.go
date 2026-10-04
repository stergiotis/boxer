package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// The most powerful thing allowed picks the rung, whatever else is set.
func TestLevelIsTheMostPowerfulThingAllowed(t *testing.T) {
	for _, tc := range []struct {
		c    Ceiling
		want LevelE
	}{
		{Ceiling{}, LevelTalk},
		{Ceiling{Effect: app.OperationEffectRun, Launch: true}, LevelTalk}, // no window: nothing else applies
		{Ceiling{Mode: ModeObserve, Effect: app.OperationEffectRun}, LevelRead},
		{Ceiling{Mode: ModeSuggest, Effect: app.OperationEffectView}, LevelView},
		{Ceiling{Mode: ModeAct, Effect: app.OperationEffectDocument}, LevelEdit},
		{Ceiling{Mode: ModeAct, Effect: app.OperationEffectRun}, LevelRun},
		{Unlimited(), LevelOutside},
	} {
		assert.Equal(t, tc.want, tc.c.Level(), "%+v", tc.c)
	}
}

// A position stays inside its rung's band, and what else is allowed only
// moves it within: no set of factors lifts a lower rung past a higher one.
func TestScoreStaysInItsBand(t *testing.T) {
	bands := float64(len(AllLevels))
	var prevTop float64
	for _, l := range AllLevels {
		lo := Ceiling{Mode: ModeSuggest, Effect: effectOf(l)}
		hi := Ceiling{Mode: ModeAct, Effect: effectOf(l), Launch: true, Desktop: true, Reach: ReachNetwork}
		if l == LevelTalk {
			lo, hi = Ceiling{}, Ceiling{}
		}
		if l == LevelRead {
			lo.Mode, hi.Mode = ModeObserve, ModeObserve
		}
		a, b := lo.Score(false), hi.Score(true)
		require.Equal(t, l, a.Level)
		require.Equal(t, l, b.Level)
		assert.GreaterOrEqual(t, a.Position, float64(l)/bands)
		assert.Less(t, b.Position, float64(l+1)/bands)
		assert.LessOrEqual(t, a.Position, b.Position)
		assert.Greater(t, a.Position, prevTop, "the least of a rung is above the most of the rung below")
		prevTop = b.Position
	}
	s := Unlimited().Score(true)
	assert.Equal(t, 98, s.Value())
	assert.Contains(t, s.Factors, "changes apply without asking")
	assert.Contains(t, s.Factors, "the model is off this machine")
	assert.Empty(t, Ceiling{}.Score(false).Factors)
}

func effectOf(l LevelE) app.OperationEffectE {
	switch l {
	case LevelView:
		return app.OperationEffectView
	case LevelEdit:
		return app.OperationEffectDocument
	case LevelRun:
		return app.OperationEffectRun
	case LevelOutside:
		return app.OperationEffectConsequential
	}
	return app.OperationEffectNone
}

func TestReachOfDestinations(t *testing.T) {
	assert.Equal(t, ReachHost, ReachOf("keelson:apps"))
	assert.Equal(t, ReachData, ReachOf("clickhouse:localhost:8123"))
	assert.Equal(t, ReachNetwork, ReachOf("http:basemap"))
	assert.Equal(t, ReachNetwork, ReachOf("llm"))
	assert.Equal(t, ReachNetwork, ReachOf("something-new"), "an unknown class is taken as the widest")
	read := &Ceiling{Mode: ModeObserve, Reach: ReachHost}
	assert.Empty(t, read.refuseDestination("keelson:apps"))
	assert.NotEmpty(t, read.refuseDestination("clickhouse:localhost:8123"))
	var none *Ceiling
	assert.Empty(t, none.refuseDestination("http:anything"), "no ceiling forbids nothing")
}

// A request above the ceiling it brings is refused before anyone is asked.
func TestRequestAboveTheCeilingIsRefused(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	read := Ceiling{Mode: ModeObserve}
	_, err := r.cli.Request(ctx, GrantRequest{Plan: "edit", Ceiling: &read,
		Entries: []GrantEntry{{Instance: r.docKey, Mode: ModeAct}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at most observe")

	_, err = r.cli.Request(ctx, GrantRequest{Plan: "open", Ceiling: &read,
		Launches: []GrantLaunch{{App: "doc", Mode: ModeObserve, Count: 1}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open windows")

	talk := Ceiling{}
	_, err = r.cli.Request(ctx, GrantRequest{Plan: "look", Ceiling: &talk,
		Entries: []GrantEntry{{Instance: r.docKey, Mode: ModeObserve}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "work in windows")
}

// The ceiling binds calls whatever the grant holds, and moving it binds at
// once; the dispatcher reports both bounds.
func TestCeilingBindsCallsAndMoves(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	edit := Ceiling{Mode: ModeAct, Effect: app.OperationEffectDocument}
	g, err := r.cli.Request(ctx, GrantRequest{Plan: "edit the doc", Ceiling: &edit,
		Entries: []GrantEntry{{Instance: r.docKey, Mode: ModeAct}}})
	require.NoError(t, err)

	a, err := r.cli.Authority(ctx, g.Handle, nil)
	require.NoError(t, err)
	assert.True(t, a.Limited)
	assert.Equal(t, LevelEdit, a.Ceiling.Level())
	assert.Equal(t, LevelEdit, a.Granted.Level(), "the doc's catalog offers export, which the ceiling caps")

	assert.Equal(t, "refused", r.call(g, "x", "export", "{}").Phase, "act outside is above an edit ceiling")
	out := r.call(g, "w", "set_text", `{"text":"a"}`)
	assert.NotEqual(t, "refused", out.Phase)

	read := Ceiling{Mode: ModeObserve}
	a, err = r.cli.Authority(ctx, g.Handle, &read)
	require.NoError(t, err)
	assert.Equal(t, LevelRead, a.Granted.Level(), "what is granted follows the ceiling down")
	out = r.call(g, "w2", "set_text", `{"text":"b"}`)
	assert.Equal(t, "refused", out.Phase)
	assert.Contains(t, out.Reason, "settings")
	assert.NotEqual(t, "refused", r.call(g, "q", "get_text", "{}").Phase, "reading stays allowed")

}

// Without a ceiling the grant is scored as it stands.
func TestAuthorityWithoutACeiling(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeObserve)
	a, err := r.cli.Authority(context.Background(), g.Handle, nil)
	require.NoError(t, err)
	assert.False(t, a.Limited)
	assert.Equal(t, LevelOutside, a.Ceiling.Level())
	assert.Equal(t, LevelRead, a.Granted.Level())
}

// A paced task's visible changes are spaced by the dispatcher; reads are not,
// and the unpaced grant lifts the spacing (ADR-0280 §SD6).
func TestPaceSpacesVisibleChanges(t *testing.T) {
	const pace = 120 * time.Millisecond
	r := newRigWith(t, func(cfg *Config) { cfg.TestGrants, cfg.Pace = true, pace })
	ctx := context.Background()
	paced := Ceiling{Mode: ModeAct, Effect: app.OperationEffectDocument}
	g, err := r.cli.Request(ctx, GrantRequest{Plan: "edit the doc", Ceiling: &paced,
		Entries: []GrantEntry{{Instance: r.docKey, Mode: ModeAct}}})
	require.NoError(t, err)

	start := time.Now()
	r.call(g, "w1", "set_text", `{"text":"a"}`)
	first := time.Since(start)
	r.call(g, "q", "get_text", "{}")
	r.call(g, "w2", "set_text", `{"text":"b"}`)
	assert.Less(t, first, pace, "the first change does not wait")
	assert.GreaterOrEqual(t, time.Since(start), pace, "the second change waits for its turn; the read between them does not add to it")

	fast := paced
	fast.Unpaced = true
	a, err := r.cli.Authority(ctx, g.Handle, &fast)
	require.NoError(t, err)
	assert.True(t, a.Granted.Unpaced)
	assert.Contains(t, a.Granted.Score(false).Factors, "works faster than you can follow")
	start = time.Now()
	r.call(g, "w3", "set_text", `{"text":"c"}`)
	r.call(g, "w4", "set_text", `{"text":"d"}`)
	assert.Less(t, time.Since(start), pace, "unpaced: the changes land as they are made")
}
