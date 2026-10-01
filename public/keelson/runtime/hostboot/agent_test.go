package hostboot

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ADR-0269 §SD6 and M6: test grants skip the person, a scripted model's
// answers are not a model's, and the action file is for trials, so the
// desktop host, where the person's windows are, refuses all three.
func TestDesktopHostRefusesTheTestLaneKnobs(t *testing.T) {
	assert.False(t, headlessOnly(true, false))
	assert.True(t, headlessOnly(true, true))
	assert.False(t, headlessOnly(false, true))
}
