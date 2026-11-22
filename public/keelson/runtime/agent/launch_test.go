package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// A launch names its app the way a model writes it — the id, the alias, or
// the display name in any case — and the window opens.
func TestALaunchNameIsResolvedLeniently(t *testing.T) {
	for _, name := range []string{string(docAppId), "doc", "Doc", " DOC "} {
		r := newRig(t, true)
		g, err := r.cli.Request(context.Background(), GrantRequest{Launches: []GrantLaunch{{App: name, Mode: ModeAct, Count: 1}}})
		require.NoError(t, err, name)
		got, err := r.cli.Launch(context.Background(), g.Handle, "l1", name, "", nil)
		require.NoError(t, err, name)
		assert.NotZero(t, got.Instance, name)
	}
}

// A launch no app answers to refuses the request, saying where ids are
// listed — a grant never comes back without a launch it was asked for.
// The same holds without test grants, where the person would see it.
func TestALaunchNoAppAnswersToRefusesTheRequest(t *testing.T) {
	for _, testGrants := range []bool{true, false} {
		r := newRigWith(t, func(cfg *Config) { cfg.TestGrants = testGrants; cfg.Coordinators = []string{"test.coordinator"} })
		_, err := r.cli.Request(context.Background(), GrantRequest{Launches: []GrantLaunch{{App: "SQL Playground", Mode: ModeAct, Count: 1}}})
		var refused *RefusedError
		require.True(t, errors.As(err, &refused), "%v", err)
		assert.Contains(t, refused.Reason, `no app named "SQL Playground"`)
		assert.Contains(t, refused.Reason, "describe_app")
	}
}

// open_window for a name no app answers to says so, by name.
func TestLaunchingAnUnknownAppNamesIt(t *testing.T) {
	r := newRig(t, true)
	g, err := r.cli.Request(context.Background(), GrantRequest{Launches: []GrantLaunch{{App: "doc", Mode: ModeAct, Count: 1}}})
	require.NoError(t, err)
	_, err = r.cli.Launch(context.Background(), g.Handle, "l1", "notepad", "", nil)
	var refused *RefusedError
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, `no app named "notepad"`)
}

// A request that opens an app asks for the destinations the app declares a
// window of it needs, beside those it names, once each and only within the
// ceiling; so the request shows them and is decided as a whole.
func TestARequestThatOpensAnAppAsksForItsLaunchDestinations(t *testing.T) {
	r := newRig(t, false)
	const id = app.AppIdT("github.com/x/apps/sql")
	require.NoError(t, r.svc.cfg.Registry.RegisterFactory(app.Manifest{Id: id, Display: "Sql", Summary: "run sql",
		Surface: app.SurfaceWindowed, Topics: []app.TopicT{app.Topics()[0]},
		LaunchDestinations: func() []string { return []string{"clickhouse:127.0.0.1:8143"} }},
		func() (app.AppI, error) { return nil, nil }))
	launches := []wireLaunchEntry{{App: string(id), Count: 1}}
	got := r.svc.withLaunchDestinations([]string{"publish:wolfram"}, launches, nil)
	assert.Equal(t, []string{"publish:wolfram", "clickhouse:127.0.0.1:8143"}, got)
	assert.Equal(t, []string{"clickhouse:127.0.0.1:8143"}, r.svc.withLaunchDestinations([]string{"clickhouse:127.0.0.1:8143"}, launches, nil), "named once")
	hostOnly := Ceiling{Mode: ModeAct, Effect: app.OperationEffectRun, Reach: ReachHost}
	assert.Empty(t, r.svc.withLaunchDestinations(nil, launches, &hostOnly), "a destination the ceiling refuses is not asked for")
	assert.Empty(t, r.svc.withLaunchDestinations(nil, []wireLaunchEntry{{App: string(docAppId), Count: 1}}, nil), "an app that declares none adds none")
}
