package httpegress

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

type fakeDelegation struct{ allow map[string]bool }

func (inst *fakeDelegation) AllowDestination(task string, epoch uint64, destination string) (bool, string) {
	if inst.allow[task+"|"+destination] {
		return true, ""
	}
	return false, "not in the grant"
}

// ADR-0269 §SD6: a fetch that is an agent's work reaches only a destination
// the task's grant lists, and is refused while no dispatcher checks it.
func TestAgentCausedFetchNeedsTheGrant(t *testing.T) {
	srv, _ := origin(t)
	cli, svc, _ := serve(t, []DestinationSpec{spec("tiles", Destination{Prefixes: []string{srv.URL + "/tiles/"}})}, "tiles")
	ctx := context.Background()
	obo := &app.OnBehalfOf{Task: "task-1", Epoch: 1}
	var refused *RefusedError

	_, err := cli.Fetch(ctx, "tiles", Request{URL: srv.URL + "/tiles/a", OnBehalfOf: obo})
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "no dispatcher")

	svc.SetDelegation(&fakeDelegation{allow: map[string]bool{"task-1|" + DelegationDestination("tiles"): true}})
	res, err := cli.Fetch(ctx, "tiles", Request{URL: srv.URL + "/tiles/a", OnBehalfOf: obo})
	require.NoError(t, err)
	assert.Equal(t, "hello", string(res.Body))

	_, err = cli.Fetch(ctx, "tiles", Request{URL: srv.URL + "/tiles/a", OnBehalfOf: &app.OnBehalfOf{Task: "task-2", Epoch: 1}})
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "not in the grant")

	_, err = cli.Fetch(ctx, "tiles", Request{URL: srv.URL + "/tiles/a"})
	require.NoError(t, err, "the app's own fetch is not an agent's")
	calls := svc.Calls()
	require.NotEmpty(t, calls)
	assert.Equal(t, "task-2", calls[len(calls)-2].Task)
}
