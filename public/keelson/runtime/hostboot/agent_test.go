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
