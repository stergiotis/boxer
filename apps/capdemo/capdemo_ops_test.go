package capdemo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
)

func getView(t *testing.T, inst *App) (v View) {
	t.Helper()
	raw, err := inst.Operations().Snapshot().Query(opGetView, nil)
	require.NoError(t, err)
	v, err = buscodec.Decode[View](raw)
	require.NoError(t, err)
	return
}

// The catalog registers, carries the one query and no command: every verb
// of this window is a dialog the person answers.
func TestOperationsCatalogIsReadOnly(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	spec, found := m.Operations.Lookup(opGetView)
	require.True(t, found)
	assert.Equal(t, app.OperationClassQuery, spec.Class)
	assert.True(t, spec.Agents)
	for _, op := range m.Operations.Operations {
		assert.NotEqual(t, app.OperationClassCommand, op.Class, "%s: no command on this window", op.Name)
	}
	require.Len(t, m.Operations.Resources, 1)
	assert.Equal(t, resSections, m.Operations.Resources[0].Name)
}

// get_view reads every section's state as the verbs land it, and the
// resource moves with it.
func TestGetViewReadsTheSections(t *testing.T) {
	inst := newApp()
	v := getView(t, inst)
	assert.Equal(t, View{}, v, "a bare window: no bus, no storage, nothing held")
	before := inst.Operations().ResourceValue(resSections)

	inst.scratchpad = "hello"
	inst.setPersistStatus("saved 5 bytes at 10:00:00", 0)
	inst.setBusy(true)
	inst.setClipboardNote("copied 12 bytes as plaintext", 0)
	inst.mu.Lock()
	inst.lastHandlePrefix = "fs.handle.abc"
	inst.previewBytes = []byte("abc")
	inst.previewTotal = 3000
	inst.watchHandlePrefix = "fs.handle.def"
	inst.watchBackend = "poller"
	inst.watchActive = true
	inst.watchRecursive = true
	inst.watchEvents = []fsbroker.WatchEvent{{Name: "x"}, {Name: "y"}}
	inst.mu.Unlock()

	v = getView(t, inst)
	assert.Equal(t, FileSection{Handle: "fs.handle.abc", PickOpen: true, PreviewBytes: 3, TotalBytes: 3000}, v.File)
	assert.Equal(t, PersistSection{ScratchpadLength: 5, Status: "saved 5 bytes at 10:00:00"}, v.Persist)
	assert.Equal(t, WatchSection{Handle: "fs.handle.def", Backend: "poller", Active: true, Events: 2, Recursive: true}, v.Watch)
	assert.Equal(t, ClipboardSection{Status: "copied 12 bytes as plaintext"}, v.Clipboard)
	assert.False(t, v.Bus)
	assert.NotEqual(t, before, inst.Operations().ResourceValue(resSections), "the resource moved")

	inst.setFileErr(noBusRemedy)
	v = getView(t, inst)
	assert.Equal(t, noBusRemedy, v.File.Status)
	assert.Zero(t, v.File.PreviewBytes, "an error clears the preview")
}
