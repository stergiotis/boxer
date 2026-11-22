package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
)

// Unattended, nobody resumes a conversation after its turn, so a call waits
// for its work as long as a pending call would be followed; attended, it
// waits CallWait and the work is followed past the turn.
func TestSettleWaitIsThePendingLimitUnattended(t *testing.T) {
	c := &coordinator{}
	callWait, pendingMax, _ := c.callLimits()
	assert.Equal(t, callWait, c.settleWait())
	c.grant = agent.Grant{Task: "t", Handle: "h", Unattended: true}
	assert.Equal(t, max(callWait, pendingMax), c.settleWait())
}
