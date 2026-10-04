package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
)

// steps are the states the machine went through, oldest first.
func steps(inst *App) (out []turnStateE) {
	for tr := range inst.turnMachine.History() {
		out = append(out, tr.To)
	}
	return
}

// A plain turn: idle, the model, answered — and every move on the declared
// graph.
func TestATurnMovesIdleModelAnswered(t *testing.T) {
	inst := appOn(t, &fakeModel{})
	inst.drain()
	assert.Equal(t, turnIdle, inst.turnMachine.Current())
	require.True(t, inst.startTurn("q1"))
	inst.drain()
	assert.True(t, inst.turnMachine.Current().running())
	drainUntil(t, inst)
	assert.Equal(t, turnAnswered, inst.turnMachine.Current())
	assert.Equal(t, []turnStateE{turnModel, turnAnswered}, steps(inst))
	assert.Zero(t, inst.turnOffGraph)
}

// The state the cancel bug had no name for: a cancelled run resets its job
// to idle with a turn still pending. It lands as cancelled, and the next
// send starts from there.
func TestACancelledTurnIsAStateOfItsOwn(t *testing.T) {
	inst := appOn(t, blockingModel{})
	require.True(t, inst.startTurn("q1"))
	inst.drain()
	require.Equal(t, turnModel, inst.turnMachine.Current())
	inst.turn.Cancel()
	drainUntil(t, inst)
	assert.Equal(t, turnCancelled, inst.turnMachine.Current())
	assert.True(t, inst.turnMachine.CanTransition(turnModel), "the person can send again")
	assert.Zero(t, inst.turnOffGraph)
}

// Edit takes an answered turn back and Cancel edit puts it back; a failed
// turn's message put back shows the turn before it; New conversation is idle
// from anywhere.
func TestEditFailureAndNewConversation(t *testing.T) {
	inst := appOn(t, &fakeModel{})
	require.True(t, inst.startTurn("q1"))
	drainUntil(t, inst)
	inst.edit()
	inst.drain()
	assert.Equal(t, turnEditing, inst.turnMachine.Current())
	inst.cancelEdit()
	inst.drain()
	assert.Equal(t, turnAnswered, inst.turnMachine.Current())

	// A second turn that fails, then its message put back in the composer.
	req := inst.conv.request("q2")
	inst.conv.begin("q2", 3, false)
	inst.conv.land(req, nil, errors.New("boom"), 4)
	inst.drain()
	assert.Equal(t, turnFailed, inst.turnMachine.Current())
	inst.edit()
	inst.drain()
	assert.Equal(t, turnAnswered, inst.turnMachine.Current(), "the turn before the failed one")

	require.True(t, inst.startTurn("q3"))
	inst.drain()
	inst.newConversation()
	inst.drain()
	assert.Equal(t, turnIdle, inst.turnMachine.Current())
	assert.Zero(t, inst.turnOffGraph, "every move was on the declared graph")
}

// How a turn ended is read off the transcript: stopped is a turn whose calls
// happened and which got no answer.
func TestLastOutcomeReadsTheTranscript(t *testing.T) {
	conv := newConversation()
	assert.Equal(t, turnIdle, conv.lastOutcome())

	req := conv.request("q1")
	conv.begin("q1", 1, false)
	conv.landTurn(req, &turnResult{activity: []string{"read a window"}, stopped: "the model kept calling tools"}, nil, 2)
	assert.Equal(t, turnStopped, conv.lastOutcome())

	req = conv.request("q2")
	conv.begin("q2", 3, false)
	conv.land(req, nil, context.Canceled, 4)
	assert.Equal(t, turnCancelled, conv.lastOutcome())

	req = conv.request("q3")
	conv.begin("q3", 5, false)
	conv.land(req, &llm.Response{Content: "a", CallId: "c"}, nil, 6)
	assert.Equal(t, turnAnswered, conv.lastOutcome())
}

// The tool loop says what it waits on: the model, a tool, or the person
// while a call waits on their decision.
func TestTheCoordinatorReportsWhatATurnWaitsOn(t *testing.T) {
	coord := newCoordinator(nil, nil, "c")
	assert.Equal(t, stageModel, coord.stageNow())
	coord.setStage(stageTool)
	assert.Equal(t, stageTool, coord.stageNow())
	done := coord.awaitPerson()
	assert.Equal(t, stagePerson, coord.stageNow(), "a call waiting on the person outranks the stage")
	done()
	assert.Equal(t, stageTool, coord.stageNow())
}

// The machine is declared whole: every state is named and reachable from
// idle, and a turn in flight can always be cancelled or abandoned.
func TestTheTurnMachineIsDeclaredWhole(t *testing.T) {
	m := newTurnMachine()
	for _, s := range allTurnStates {
		assert.NotEqual(t, "?", s.String())
		if s != turnIdle {
			assert.True(t, m.CanReach(turnIdle, s), "%s is reachable", s)
		}
		assert.True(t, m.CanReach(s, turnIdle), "%s can return to idle", s)
	}
	v := m.OpsView(4)
	assert.Equal(t, "idle", v.Current)
	assert.Len(t, v.States, len(allTurnStates))
	assert.NotEmpty(t, v.Edges)
}

// The window offers an agent the turn's state and nothing that changes it.
func TestTheCatalogOffersTheTurnState(t *testing.T) {
	cat := ops.Catalog()
	require.NotNil(t, cat)
	names := map[string]bool{}
	for _, spec := range cat.Operations {
		names[spec.Name] = true
		assert.Equal(t, app.OperationEffectNone, spec.Effect, "%s reads and changes nothing", spec.Name)
	}
	assert.True(t, names["turn_state"] && names["turn_machine"], "%v", names)
}
