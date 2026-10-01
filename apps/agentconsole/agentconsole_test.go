package agentconsole

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestTheConsoleRegisters(t *testing.T) {
	m, ok := app.LookupManifest(AppId)
	require.True(t, ok)
	assert.Nil(t, m.Operations, "a coordinator offers no operations of its own")
}
