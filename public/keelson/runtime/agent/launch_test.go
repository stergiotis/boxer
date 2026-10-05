package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A launch names its app the way a model writes it — the id, the alias, or
// the display name in any case — and the window opens.
func TestALaunchNameIsResolvedLeniently(t *testing.T) {
	for _, name := range []string{string(docAppId), "doc", "Doc", " DOC "} {
		r := newRig(t, true)
		g, err := r.cli.Request(context.Background(), GrantRequest{Launches: []GrantLaunch{{App: name, Mode: ModeAct, Count: 1}}})
		require.NoError(t, err, name)
		got, err := r.cli.Launch(context.Background(), g.Handle, name, "", nil)
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
	_, err = r.cli.Launch(context.Background(), g.Handle, "notepad", "", nil)
	var refused *RefusedError
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, `no app named "notepad"`)
}
