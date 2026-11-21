package notebook

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/notebook/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

func TestThePhaseFollowsWhatTheWindowHolds(t *testing.T) {
	nb, err := mdnotebook.Read([]byte("```sql\nSELECT 1 AS one\n```\n^one\n"))
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		set  func(inst *App)
		want phaseE
	}{
		{"nothing", func(*App) {}, phaseEmpty},
		{"a snapshot being opened", func(inst *App) { inst.opening = &snapshotRef{path: "a.md"} }, phaseOpening},
		{"a text that is not a notebook", func(inst *App) { inst.doc = &document{} }, phaseUnreadable},
		{"not read yet", func(inst *App) { inst.doc, inst.nb = &document{}, nb }, phaseReading},
		{"sealed", func(inst *App) { inst.doc, inst.nb, inst.checked = &document{}, nb, true }, phaseSealed},
		{"stale", func(inst *App) {
			inst.doc, inst.nb, inst.checked = &document{}, nb, true
			inst.check = notebookview.Check{Stale: []string{"one"}}
		}, phaseStale},
		{"a problem outranks the age of what is stored", func(inst *App) {
			inst.doc, inst.nb, inst.checked = &document{}, nb, true
			inst.check = notebookview.Check{Stale: []string{"one"}, Problems: []string{"a blob is missing"}}
		}, phaseUnsealed},
		{"an open outranks what is shown", func(inst *App) {
			inst.doc, inst.nb, inst.checked = &document{}, nb, true
			inst.opening = &snapshotRef{path: "a.md"}
		}, phaseOpening},
	} {
		inst := newApp()
		tc.set(inst)
		assert.Equal(t, tc.want, inst.observePhase(), tc.name)
	}
}

func TestEveryPhaseIsReachable(t *testing.T) {
	m := newPhaseMachine()
	for _, ph := range allPhases[1:] {
		assert.True(t, m.CanReach(phaseEmpty, ph), ph.String())
	}
	t.Cleanup(egui2test.Install())
	inst := newApp()
	require.NoError(t, inst.openSample("minimal"))
	inst.mirrorPhase()
	assert.Equal(t, phaseReading, inst.phaseMachine.Current(), "opening a sample mirrors its phase")
	read(t, inst)
	inst.mirrorPhase()
	assert.Equal(t, phaseSealed, inst.phaseMachine.Current())
	require.NoError(t, inst.Unmount(nil))
}

func TestTheNotebookMachineIsMounted(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	h := newApp().Operations()
	raw, err := h.Snapshot().Query("notebook_machine", nil)
	require.NoError(t, err)
	mach, err := buscodec.Decode[opfsm.Machine](raw)
	require.NoError(t, err)
	assert.Len(t, mach.States, len(allPhases))
	raw, err = h.Snapshot().Query("notebook_state", nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[opfsm.State](raw)
	require.NoError(t, err)
	assert.Equal(t, phaseEmpty.String(), st.Current)
}

// The workingset is the launch config that opens the window's notebook
// again, and only when the person did something in the window.
func TestTheWorkingsetOpensTheNotebookAgain(t *testing.T) {
	t.Cleanup(egui2test.Install())
	inst := newApp()
	_, dirty, err := inst.ComposeWorkingset()
	require.NoError(t, err)
	assert.False(t, dirty, "an empty window keeps nothing")
	require.NoError(t, inst.openSample("minimal"))
	_, dirty, _ = inst.ComposeWorkingset()
	assert.False(t, dirty, "a sample as it is: an empty window offers it")

	// An edited sample travels as its text.
	sql, err := mdnotebook.Buffer(inst.nb)
	require.NoError(t, err)
	_, err = inst.setRecords(sql+" LIMIT 1", "")
	require.NoError(t, err)
	cfg, dirty, err := inst.ComposeWorkingset()
	require.NoError(t, err)
	require.True(t, dirty)
	l, err := buscodec.Decode[launchcfg.NotebookLaunch](cfg)
	require.NoError(t, err)
	assert.Equal(t, "minimal.md", l.Name)
	assert.Contains(t, l.Text, "LIMIT 1")
	assert.Zero(t, l.Mount)

	// A notebook of a snapshot is named by it, without its text.
	ref := snapshotRef{mount: 0x2a, path: "report.md"}
	inst.setDocument(&document{name: "report.md", origin: "snapshot " + ref.String(), text: []byte("x"), fsys: emptyFS{}, ref: &ref})
	cfg, dirty, err = inst.ComposeWorkingset()
	require.NoError(t, err)
	require.True(t, dirty)
	l, err = buscodec.Decode[launchcfg.NotebookLaunch](cfg)
	require.NoError(t, err)
	assert.EqualValues(t, 0x2a, l.Mount)
	assert.Equal(t, "report.md", l.Path)
	assert.Empty(t, l.Text)
	require.NoError(t, inst.Unmount(nil))
}

// The tour entry mounts a window with no host and draws it on the sample.
func TestTheTourEntryDrawsTheSample(t *testing.T) {
	_, zero, reset := egui2test.InstallCounting()
	t.Cleanup(reset)
	zero()
	state := tourInit(c.NewWidgetIdStack())
	inst, ok := state.(*App)
	require.True(t, ok)
	require.NotNil(t, inst.doc)
	assert.Equal(t, "sample report.md", inst.doc.origin)
	read(t, inst)
	assert.NotPanics(t, func() {
		defer c.CurrentApplicationState.StateManager.ScriptReset()
		tourRender(inst.ids, inst)
	})
	assert.Equal(t, phaseSealed, inst.phaseMachine.Current())
	require.NoError(t, inst.Unmount(nil))
}
