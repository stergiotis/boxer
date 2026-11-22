package chat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// The changes note folds a repeated change into one line with its count,
// and the app's own changes into one line per window.
func TestChangeLinesFoldRepeatsAndTheAppsOwnWork(t *testing.T) {
	var changes []agent.Change
	for i := range 300 {
		changes = append(changes, agent.Change{Instance: 2, Seq: uint64(i), Writer: "app", Resources: []string{"query_state"}})
	}
	changes = append(changes,
		agent.Change{Instance: 2, Writer: "app", Resources: []string{"result"}},
		agent.Change{Instance: 2, Writer: "person", Op: "set_sql", Resources: []string{"sql"}},
		agent.Change{Instance: 2, Writer: "person", Op: "set_sql", Resources: []string{"sql"}},
		agent.Change{Instance: 3, Writer: "another task", Op: "evaluate", Resources: []string{"session"}})
	lines := changeLines(changes)
	require.Len(t, lines, 3)
	assert.Equal(t, "the app's own work updated query_state, result in window 2 (301 changes; it follows runs, yours included)", lines[0])
	assert.Equal(t, "person changed sql in window 2 (set_sql), 2 times", lines[1])
	assert.Equal(t, "another task changed session in window 3 (evaluate)", lines[2])

	var many []agent.Change
	for i := range 40 {
		many = append(many, agent.Change{Instance: uint64(i), Writer: "person", Resources: []string{"sql"}})
	}
	lines = changeLines(many)
	require.Len(t, lines, changesMaxLines+1)
	assert.True(t, strings.HasPrefix(lines[changesMaxLines], "and 28 more"), lines[changesMaxLines])
}
