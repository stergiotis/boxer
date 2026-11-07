package writingstylescope

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

// sweptApp is a window whose panes have been swept inline, as the tour
// stage and a host without tasks sweep them.
func sweptApp(t *testing.T) (inst *App) {
	t.Helper()
	inst = phaseApp()
	inst.analyze()
	require.NotNil(t, inst.res, "the example pair sweeps")
	return
}

// The phase is what the runner and the cache say: nothing, a result, a
// result the text has moved on from, a refusal.
func TestThePhaseFollowsTheSweepAndTheText(t *testing.T) {
	for _, tc := range []struct {
		name string
		prep func(t *testing.T) *App
		want phaseE
	}{
		{"nothing swept", func(*testing.T) *App { return phaseApp() }, phaseNoResult},
		{"a result", sweptApp, phaseResult},
		{"an edit since", func(t *testing.T) *App { inst := sweptApp(t); inst.docB += "\n\nmore"; return inst }, phaseStale},
		{"the floor moved", func(t *testing.T) *App { inst := sweptApp(t); inst.minSectionBytes = 50; return inst }, phaseStale},
		{"the edit undone", func(t *testing.T) *App {
			inst := sweptApp(t)
			was := inst.docB
			inst.docB += "x"
			inst.docB = was
			return inst
		}, phaseResult},
		{"a refusal", func(*testing.T) *App {
			inst := phaseApp()
			inst.applySweep(inst.inputs(), nil, errors.New("too few sections"))
			return inst
		}, phaseFailed},
	} {
		inst := tc.prep(t)
		assert.Equal(t, tc.want, inst.observePhase(), tc.name)
	}
}

// Every phase is reachable from no result along the declared rules, so an
// observation can only skip states, never contradict the graph; and the
// mirror follows what the frame observes.
func TestEveryPhaseIsReachable(t *testing.T) {
	m := newPhaseMachine()
	for _, ph := range allPhases[1:] {
		assert.True(t, m.CanReach(phaseNoResult, ph), ph.String())
	}
	inst := sweptApp(t)
	inst.mirrorPhase()
	assert.Equal(t, phaseResult, inst.phaseMachine.Current(), "an inline sweep skips the sweeping frame nobody sampled")
	inst.docA = "edited"
	inst.mirrorPhase()
	assert.Equal(t, phaseStale, inst.phaseMachine.Current())
}

// The machine is mounted: analysis_machine is the chip's graph,
// analysis_state the current phase and what moves it.
func TestTheAnalysisMachineIsMounted(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))

	h := phaseApp().Operations()
	raw, err := h.Snapshot().Query("analysis_machine", nil)
	require.NoError(t, err)
	mach, err := buscodec.Decode[opfsm.Machine](raw)
	require.NoError(t, err)
	assert.Len(t, mach.States, len(allPhases))
	raw, err = h.Snapshot().Query("analysis_state", nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[opfsm.State](raw)
	require.NoError(t, err)
	assert.Equal(t, "no result", st.Current)
	assert.Contains(t, st.Next, opfsm.Edge{From: "no result", To: "sweeping", Label: "Analyze"})
}
