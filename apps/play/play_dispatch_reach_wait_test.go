package play

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

const waitSealedSQL = "SELECT * FROM keelson('adhoc_secret')"

// reachWaitClient is a client pinned off the plane, with a demonstration
// for its endpoint under way that the test ends; done ends it.
func reachWaitClient(t *testing.T) (c *Client, done func()) {
	t.Helper()
	withSealedPlane(t, "http://127.0.0.1:9/query", "adhoc_secret")
	withProbe(t, &fakeProbe{})
	c = NewClient(ClientConfig{URL: "http://sibling.invalid"}, nil)
	done, ok := c.reach.begin("http://sibling.invalid")
	require.True(t, ok)
	return
}

func (inst *Client) decideSealed() dispatchDecision { return inst.Dispatch(waitSealedSQL, "") }

// TestARunWaitsForADemonstrationThatSucceeds: the first join of sealed data
// with a server endpoint no longer fails while the endpoint's demonstration
// is under way; the run waits and is placed once it succeeds.
func TestARunWaitsForADemonstrationThatSucceeds(t *testing.T) {
	c, done := reachWaitClient(t)
	frame := c.decideSealed()
	require.Equal(t, dispatchClassRefused, frame.class)
	require.NotNil(t, frame.pending, "the refusal names the placement it waits for")
	assert.Contains(t, frame.reason, "a demonstration has started: a run of the statement waits for it up to 5s")
	go func() {
		time.Sleep(30 * time.Millisecond)
		c.reach.record("http://sibling.invalid")
		done()
	}()
	start := time.Now()
	dec := c.dispatchAwaitingReach(t.Context(), c.decideSealed)
	assert.NotEqual(t, dispatchClassRefused, dec.class, dec.reason)
	assert.True(t, dec.Confined())
	assert.Less(t, time.Since(start), 2*time.Second)
}

// TestARunRefusedWhenTheDemonstrationDoesNotSucceedInTime: past the bound the
// run is refused, and the reason says it waited.
func TestARunRefusedWhenTheDemonstrationDoesNotSucceedInTime(t *testing.T) {
	c, done := reachWaitClient(t)
	defer done()
	c.reach.runWait = 40 * time.Millisecond
	dec := c.dispatchAwaitingReach(t.Context(), c.decideSealed)
	require.Equal(t, dispatchClassRefused, dec.class)
	assert.Nil(t, dec.pending)
	assert.Contains(t, dec.reason, "the run waited up to 5s for the demonstration under way, which did not succeed")
	assert.NotContains(t, dec.reason, "is accepted once it succeeds")
}

// TestARunStopsWaitingWhenItsContextEnds: a cancelled run does not wait out
// the bound.
func TestARunStopsWaitingWhenItsContextEnds(t *testing.T) {
	c, done := reachWaitClient(t)
	defer done()
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	dec := c.dispatchAwaitingReach(ctx, c.decideSealed)
	assert.Less(t, time.Since(start), 2*time.Second, "the run's context ends the wait, not the 5s bound")
	assert.Equal(t, dispatchClassRefused, dec.class)
}

// TestNoWaitWhenNoDemonstrationCanBeMade: without a plane to demonstrate
// reaching, the statement is refused at once and says no demonstration can
// be made.
func TestNoWaitWhenNoDemonstrationCanBeMade(t *testing.T) {
	withSealedPlane(t, "http://127.0.0.1:9/query", "adhoc_secret")
	withProbe(t, nil)
	c := NewClient(ClientConfig{URL: "http://sibling.invalid"}, nil)
	start := time.Now()
	dec := c.dispatchAwaitingReach(t.Context(), c.decideSealed)
	assert.Less(t, time.Since(start), time.Second)
	require.Equal(t, dispatchClassRefused, dec.class)
	assert.Nil(t, dec.pending)
	assert.Contains(t, dec.reason, "no demonstration can be made")
	assert.NotContains(t, dec.reason, "has started")
}

// TestTheCallLeavesAWaitingStatementToTheRun: the check at a call that starts
// a run judges the placement the run waits for; a check with no run after it
// refuses and says a run would wait.
func TestTheCallLeavesAWaitingStatementToTheRun(t *testing.T) {
	c, done := reachWaitClient(t)
	defer done()
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1",
		Destinations: []string{"keelson:adhoc_secret", DestinationClickHouse(endpointHost("http://sibling.invalid"))}}
	assert.NoError(t, refuseAgentRun(c, obo, waitSealedSQL), "the run decides once the demonstration is over")
	err := refuseAgentStatement(c, obo, waitSealedSQL)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Reason, "a run of the statement waits for it up to 5s")
	// The grant is still judged at the call: the placement waited for needs
	// the endpoint in the grant.
	bare := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1", Destinations: []string{"keelson:adhoc_secret"}}
	err = refuseAgentRun(c, bare, waitSealedSQL)
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, []string{DestinationClickHouse(endpointHost("http://sibling.invalid"))}, refusal.Destinations)
}
