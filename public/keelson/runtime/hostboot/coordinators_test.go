package hostboot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestComposeCoordinators(t *testing.T) {
	assert.Equal(t, "launchable AND (kind = 'chatlet' OR x)", composeCoordinators("kind = 'chatlet' OR x"))
}

// Set without clickhouse-local, the host does not boot.
func TestBoot_CoordinatorsPredicateWithoutChlocalFails(t *testing.T) {
	agent.CoordinatorsWhereEnv.Override("true")
	t.Cleanup(agent.CoordinatorsWhereEnv.ClearOverride)
	reg, _ := registryWithService(t)
	opts := minimalOptions(t, reg)
	opts.Services.Agent = true
	opts.NoWindowHost = true
	rt, err := Boot(context.Background(), opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clickhouse-local")
	if rt != nil {
		rt.Close()
	}
}

// The predicate selects among launchable apps, beside the named list.
func TestBoot_CoordinatorsPredicateSelectsBesideTheList(t *testing.T) {
	agent.CoordinatorsEnv.SetForTest(t, "named")
	agent.CoordinatorsWhereEnv.Override("id = '" + string(headlessId) + "'")
	t.Cleanup(agent.CoordinatorsWhereEnv.ClearOverride)
	reg, _ := registryWithService(t)
	opts := chlocalOptions(t, reg)
	opts.Services.Agent = true
	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	defer rt.Close()
	assert.True(t, reg.Coordinator(headlessId))
	assert.True(t, reg.Coordinator(app.AppIdT("named")))
	assert.False(t, reg.Coordinator("other"))
}
