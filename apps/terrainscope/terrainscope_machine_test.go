package terrainscope

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

func phaseApp() (inst *App) {
	inst = newApp()
	inst.logger = zerolog.Nop()
	return
}

// The phase follows the selection, then what the last sweep left.
func TestThePhaseFollowsTheSelectionAndTheSweep(t *testing.T) {
	for _, tc := range []struct {
		name string
		prep func(inst *App)
		want phaseE
	}{
		{"nothing placed", func(*App) {}, phaseIdle},
		{"the observer placed", func(inst *App) { inst.stage = selectionStagePt1 }, phaseOnePoint},
		{"both placed, nothing launched", func(inst *App) { inst.stage = selectionStagePt2 }, phaseTwoPoints},
		{"a result", func(inst *App) { inst.stage = selectionStagePt2; inst.result = synthSweepResult() }, phaseResult},
		{"a failure", func(inst *App) { inst.stage = selectionStagePt2; inst.sweepErr = errors.New("tiles") }, phaseFailed},
		{"a cancel", func(inst *App) { inst.stage = selectionStagePt2; inst.sweepNote = "sweep cancelled" }, phaseCancelled},
		{"a reset after a result", func(inst *App) {
			inst.stage = selectionStagePt2
			inst.result = synthSweepResult()
			inst.resetSelection()
		}, phaseIdle},
	} {
		inst := phaseApp()
		tc.prep(inst)
		assert.Equal(t, tc.want, inst.observePhase(), tc.name)
	}
}

// Every phase is reachable from idle along the declared rules, so an
// observation can only skip states, never contradict the graph; and the
// mirror follows what the frame observes.
func TestEveryPhaseIsReachable(t *testing.T) {
	m := newPhaseMachine()
	for _, ph := range allPhases[1:] {
		assert.True(t, m.CanReach(phaseIdle, ph), ph.String())
	}
	inst := phaseApp()
	inst.setPoints(SetPointsArgs{ObserverLat: 46.9, ObserverLon: 8.2, TargetLat: 46.95, TargetLon: 8.3})
	inst.mirrorPhase()
	assert.Equal(t, phaseTwoPoints, inst.phaseMachine.Current())
	inst.result = synthSweepResult()
	inst.mirrorPhase()
	assert.Equal(t, phaseResult, inst.phaseMachine.Current(), "a landing skips the sweeping frame nobody sampled")
}

// The machine is mounted: sweep_machine is the chip's graph, sweep_state
// the current phase and what moves it.
func TestTheSweepMachineIsMounted(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))

	h := phaseApp().Operations()
	raw, err := h.Snapshot().Query("sweep_machine", nil)
	require.NoError(t, err)
	mach, err := buscodec.Decode[opfsm.Machine](raw)
	require.NoError(t, err)
	assert.Len(t, mach.States, len(allPhases))
	raw, err = h.Snapshot().Query("sweep_state", nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[opfsm.State](raw)
	require.NoError(t, err)
	assert.Equal(t, "idle", st.Current)
	assert.Contains(t, st.Next, opfsm.Edge{From: "idle", To: "one point", Label: "click the observer"})
}
