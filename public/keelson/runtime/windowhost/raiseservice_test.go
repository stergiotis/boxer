package windowhost

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// An app raises its own window over RaiseSubject; another app's window, or
// one not open, is refused with a reason.
func TestRaiseService_OwnWindowsOnly(t *testing.T) {
	reg, _ := mkRegistryWithSingleton(t, testCallerAppId, "test.other")
	fx := mkOpenServiceFixture(t, reg)
	fx.caller = fx.bus.NewClient(testCallerAppId, []app.SubjectFilter{
		{Pattern: RaiseSubject, Direction: app.CapDirectionPub},
	})
	own, err := fx.host.Open(testCallerAppId)
	require.NoError(t, err)
	other, err := fx.host.Open("test.other")
	require.NoError(t, err)

	require.NoError(t, RequestRaise(fx.caller, uint64(own)))
	assert.Equal(t, own, fx.host.pendingRaise)

	fx.host.pendingRaise = 0
	err = RequestRaise(fx.caller, uint64(other))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Zero(t, fx.host.pendingRaise)

	assert.Error(t, RequestRaise(fx.caller, 99))
}
