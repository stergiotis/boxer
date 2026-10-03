package jackstay

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

func phaseApp() (inst *App) {
	inst = newApp()
	inst.logger = zerolog.Nop()
	return
}

// The phase follows the plan's sections: the DDL, then the latest of a
// comparison and a sync.
func TestThePhaseFollowsThePlan(t *testing.T) {
	t0 := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	identical := &jk.TableDiff{ComputedAt: t0}
	differing := &jk.TableDiff{ComputedAt: t0, Differing: []jk.ChunkDiff{{}}}
	clean := &jk.TableSyncReport{FinishedAt: t0.Add(time.Minute), Copied: 1}
	failed := &jk.TableSyncReport{FinishedAt: t0.Add(time.Minute), Copied: 1, Failed: 1}
	for _, tc := range []struct {
		name   string
		tables []jk.PlanTable
		want   phaseE
	}{
		{"ddl", []jk.PlanTable{{TableVerdict: jk.TableVerdict{DDL: []string{"CREATE TABLE t"}}}}, phaseDDLPending},
		{"in line", []jk.PlanTable{{}}, phasePlanned},
		{"differs", []jk.PlanTable{{Diff: identical}, {Diff: differing}}, phaseDiffers},
		{"identical", []jk.PlanTable{{Diff: identical}, {}}, phaseIdentical},
		{"synced", []jk.PlanTable{{SyncReport: clean}}, phaseSynced},
		{"chunks left", []jk.PlanTable{{SyncReport: failed}}, phaseIncomplete},
		{"a sync after a comparison", []jk.PlanTable{{Diff: differing}, {SyncReport: clean}}, phaseSynced},
		{"a comparison after a sync", []jk.PlanTable{{Diff: &jk.TableDiff{ComputedAt: t0.Add(time.Hour)}, SyncReport: clean}}, phaseIdentical},
	} {
		inst := phaseApp()
		inst.plan = &jk.Plan{Tables: tc.tables}
		assert.Equal(t, tc.want, inst.observePhase(), tc.name)
	}
}

func TestThePhaseBeforeAPlan(t *testing.T) {
	inst := phaseApp()
	assert.Equal(t, phaseIdle, inst.observePhase())
	inst.disc = &discovered{}
	assert.Equal(t, phaseDiscovered, inst.observePhase())
	inst.plan = &jk.Plan{}
	inst.stale = []string{"default.t: the sorting key changed"}
	assert.Equal(t, phaseStale, inst.observePhase(), "a plan the servers moved under is stale whatever it holds")
}

// Every phase is reachable from idle along the declared rules, so an
// observation can only skip states, never contradict the graph.
func TestEveryPhaseIsReachable(t *testing.T) {
	m := newPhaseMachine()
	for _, ph := range allPhases[1:] {
		assert.True(t, m.CanReach(phaseIdle, ph), ph.String())
	}
	inst := phaseApp()
	inst.plan = &jk.Plan{Tables: []jk.PlanTable{{}}}
	inst.mirrorPhase()
	assert.Equal(t, phasePlanned, inst.phaseMachine.Current(), "opening a plan mirrors its phase")
}

// The phase is mounted: plan_machine is the chip's graph, plan_state the
// current phase and what moves it.
func TestThePlanMachineIsMounted(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(AppId)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(AppId))

	h := phaseApp().Operations()
	raw, err := h.Snapshot().Query("plan_machine", nil)
	require.NoError(t, err)
	mach, err := buscodec.Decode[opfsm.Machine](raw)
	require.NoError(t, err)
	assert.Len(t, mach.States, len(allPhases))
	raw, err = h.Snapshot().Query("plan_state", nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[opfsm.State](raw)
	require.NoError(t, err)
	assert.Equal(t, "idle", st.Current)
	assert.Contains(t, st.Next, opfsm.Edge{From: "idle", To: "discovering", Label: "Discover"})
}
