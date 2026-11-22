package agent

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

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

// A window the task opened is known to the task as it opened: a first write
// goes through without a read before it, and a change by the person since
// still conflicts.
func TestAWindowTheTaskOpenedIsReadAsItOpened(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	// The window host draws every window while the launch waits for it.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
			}
			r.host.mu.Lock()
			keys := slices.Collect(maps.Keys(r.host.engines))
			r.host.mu.Unlock()
			for _, k := range keys {
				r.host.frame(k)
			}
		}
	}()
	g, err := r.cli.Request(ctx, GrantRequest{Plan: "write a doc", Launches: []GrantLaunch{{App: "doc", Mode: ModeAct, Count: 2}}})
	require.NoError(t, err)
	got, err := r.cli.Launch(ctx, g.Handle, "open", "doc", "", nil)
	require.NoError(t, err)
	out, err := r.cli.Call(ctx, CallRequest{Handle: g.Handle, Instance: got.Instance, Operation: "set_text", Args: `{"text":"agent"}`, Key: "w"})
	require.NoError(t, err)
	assert.Contains(t, []string{"accepted", "applied"}, out.Phase, out.Reason)
	docText := func(k uint64) func() bool {
		return func() bool {
			// A frame writes the doc under frameMu.
			r.host.frameMu.Lock()
			defer r.host.frameMu.Unlock()
			r.host.mu.Lock()
			defer r.host.mu.Unlock()
			return r.host.docs[k].text == "agent"
		}
	}
	require.Eventually(t, docText(got.Instance), 2*time.Second, 5*time.Millisecond, "the first write went through")

	other, err := r.cli.Launch(ctx, g.Handle, "open2", "doc", "", nil)
	require.NoError(t, err)
	r.host.person(other.Instance, func(d *doc) { d.text = "person" })
	r.host.frame(other.Instance)
	_, err = r.cli.Call(ctx, CallRequest{Handle: g.Handle, Instance: other.Instance, Operation: "set_text", Args: `{"text":"agent"}`, Key: "w2"})
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	r.host.frameMu.Lock()
	defer r.host.frameMu.Unlock()
	r.host.mu.Lock()
	defer r.host.mu.Unlock()
	assert.Equal(t, "person", r.host.docs[other.Instance].text, "the person changed it after it opened: the write conflicts")
	log := r.host.engines[other.Instance].LogSince(0)
	require.NotEmpty(t, log)
	assert.Equal(t, "conflict", log[len(log)-1].Phase.String())
	assert.Contains(t, log[len(log)-1].Reason, "moved since it was read")
}
