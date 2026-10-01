package hostboot

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ADR-0269 §SD6: test grants skip the person, so the desktop host, where
// the person's windows are, refuses the flag.
func TestDesktopHostRefusesTestGrants(t *testing.T) {
	assert.False(t, agentTestGrants(true, false))
	assert.True(t, agentTestGrants(true, true))
	assert.False(t, agentTestGrants(false, true))
}

// ADR-0269 M6: the scripted model is for scenes; the desktop host refuses it.
func TestDesktopHostRefusesTheScriptedModel(t *testing.T) {
	assert.False(t, scriptedModel(true, false))
	assert.True(t, scriptedModel(true, true))
}
