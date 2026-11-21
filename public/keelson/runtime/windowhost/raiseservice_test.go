package windowhost

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowopen"
)

// An app raises its own window over windowopen.RaiseSubject; another app's window, or
// one not open, is refused with a reason.
func TestRaiseService_OwnWindowsOnly(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, testCallerAppId, "test.other")
	fx := mkOpenServiceFixture(t, reg)
	fx.caller = fx.bus.NewClient(testCallerAppId, []app.SubjectFilter{
		{Pattern: windowopen.RaiseSubject, Direction: app.CapDirectionPub},
	})
	own, err := fx.host.Open(testCallerAppId)
	require.NoError(t, err)
	other, err := fx.host.Open("test.other")
	require.NoError(t, err)

	require.NoError(t, windowopen.RequestRaise(t.Context(), fx.caller, uint64(own)))
	assert.Equal(t, own, fx.host.pendingRaise)

	fx.host.pendingRaise = 0
	err = windowopen.RequestRaise(t.Context(), fx.caller, uint64(other))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Zero(t, fx.host.pendingRaise)

	assert.Error(t, windowopen.RequestRaise(t.Context(), fx.caller, 99))
}
