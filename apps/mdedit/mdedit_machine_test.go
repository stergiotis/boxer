package mdedit

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// The phase follows the facts in their order of precedence: work in flight,
// then what the follow found, then the dirty marker, then the follow.
func TestTheDocumentPhaseFollowsTheFacts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts docFacts
		want  phaseE
	}{
		{"nothing", docFacts{}, phaseClean},
		{"restoring", docFacts{restoring: true, dirty: true}, phaseRestoring},
		{"opening", docFacts{fileBusy: true, fileOp: fileOpOpen, dirty: true}, phaseOpening},
		{"saving", docFacts{fileBusy: true, fileOp: fileOpSave, dirty: true}, phaseSaving},
		{"a finished gesture is no gesture", docFacts{fileOp: fileOpSave, dirty: true}, phaseModified},
		{"sending", docFacts{sending: true, dirty: true}, phaseSending},
		{"a gesture outranks a send", docFacts{fileBusy: true, fileOp: fileOpSave, sending: true}, phaseSaving},
		{"typed", docFacts{dirty: true}, phaseModified},
		{"typed under a follow", docFacts{dirty: true, following: true}, phaseModified},
		{"followed", docFacts{following: true}, phaseFollowing},
		{"the disk moved", docFacts{dirty: true, following: true, diskChanged: true}, phaseDiskChanged},
		{"the file went away", docFacts{following: true, diskGone: true}, phaseDiskChanged},
	} {
		assert.Equal(t, tc.want, observeDocument(tc.facts), tc.name)
	}
}

// Every phase is reachable from restoring along the declared rules, so an
// observation can only skip states, never contradict the graph; and the
// window's own facts mirror into it.
func TestEveryDocumentPhaseIsReachable(t *testing.T) {
	m := newDocMachine()
	for _, ph := range allDocPhases[1:] {
		assert.True(t, m.CanReach(phaseRestoring, ph), ph.String())
	}
	inst := newApp()
	inst.logger = zerolog.Nop()
	inst.mirrorPhase()
	assert.Equal(t, phaseClean, inst.docMachine.Current(), "a window with nothing to restore is clean")
	inst.src = "typed"
	inst.mirrorPhase()
	assert.Equal(t, phaseModified, inst.docMachine.Current())
	require.True(t, inst.beginFile(fileOpSave, "saving…"))
	inst.mirrorPhase()
	assert.Equal(t, phaseSaving, inst.docMachine.Current())
	inst.finishFile(fileResult{op: fileOpSave, saved: "typed"})
	inst.applyFileResult(inst.fileRes)
	inst.mirrorPhase()
	assert.Equal(t, phaseClean, inst.docMachine.Current(), "a landed save is the checkpoint")
}

// The phase is mounted: document_machine is the chip's graph, document_state
// the current phase and what moves it.
func TestTheDocumentMachineIsMounted(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))

	inst := newApp()
	inst.logger = zerolog.Nop()
	h := inst.Operations()
	raw, err := h.Snapshot().Query("document_machine", nil)
	require.NoError(t, err)
	mach, err := buscodec.Decode[opfsm.Machine](raw)
	require.NoError(t, err)
	assert.Len(t, mach.States, len(allDocPhases))
	raw, err = h.Snapshot().Query("document_state", nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[opfsm.State](raw)
	require.NoError(t, err)
	assert.Equal(t, "restoring", st.Current)
	assert.Contains(t, st.Next, opfsm.Edge{From: "restoring", To: "clean", Label: "restored, or nothing to restore"})
	inst.mirrorPhase()
	raw, err = h.Snapshot().Query("document_state", nil)
	require.NoError(t, err)
	st, err = buscodec.Decode[opfsm.State](raw)
	require.NoError(t, err)
	assert.Equal(t, "clean", st.Current)
	assert.Contains(t, st.Next, opfsm.Edge{From: "clean", To: "opening", Label: "Open"})
}
