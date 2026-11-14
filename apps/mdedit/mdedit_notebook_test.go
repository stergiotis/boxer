package mdedit

import (
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/fixture"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// snapshotOf is the fixture notebook as a snapshot holds it: under a
// directory, its blobs beside it.
func snapshotOf(t *testing.T, dir string) (snap fstest.MapFS, text string) {
	t.Helper()
	snap = fstest.MapFS{}
	require.NoError(t, fs.WalkDir(fixture.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rErr := fs.ReadFile(fixture.FS(), p)
		if rErr != nil {
			return rErr
		}
		snap[dir+"/"+p] = &fstest.MapFile{Data: data}
		if p == fixture.Name {
			text = string(data)
		}
		return nil
	}))
	require.NotEmpty(t, text)
	return
}

func notebookFrame(inst *App) (drawn bool) {
	defer c.CurrentApplicationState.StateManager.ScriptReset()
	return inst.renderNotebook()
}

// A snapshot document that is a notebook is drawn as one in the reading
// mode, once a lane has read its blobs; in the other modes, and for any
// other document, the preview is markdown.
func TestASnapshotNotebookIsDrawnInTheReadingMode(t *testing.T) {
	t.Cleanup(scenetest.Install())
	snap, text := snapshotOf(t, "reports")
	inst := &App{ids: c.NewWidgetIdStack()}
	defer inst.resetNotebook()
	inst.src = text
	inst.viewMode = viewRead

	assert.False(t, notebookFrame(inst), "a document that is not from a snapshot has no blobs to read")

	inst.setSnapshotImages(newSnapshotImages(snap, "reports/"+fixture.Name, "k"))
	deadline := time.Now().Add(5 * time.Second)
	for inst.notebook.given == nil {
		require.True(t, time.Now().Before(deadline), "the notebook was not prepared")
		require.True(t, notebookFrame(inst), "the notebook claims the preview while its blobs are read")
		time.Sleep(time.Millisecond)
	}
	require.True(t, notebookFrame(inst))
	assert.Empty(t, inst.notebook.view.Problems(), "the blobs were found beside the document")
	assert.Len(t, inst.notebook.view.Notebook().Records, 3)

	inst.viewMode = viewSplit
	assert.False(t, notebookFrame(inst), "the split mode shows the text")
	inst.viewMode = viewRead

	// Plain markdown in a snapshot is not a notebook.
	inst.src = "# A note\n\n```sql\nSELECT 1\n```\n"
	assert.False(t, notebookFrame(inst))

	// So is an applet document: its `sql <panel>` fence is spelled like a
	// presentation cell, and it holds no record for one to read.
	inst.src = "# Timeline\n\n```sql bands\nSELECT 1 AS a WHERE 1\n```\n\n![[take.wav]]\n"
	assert.False(t, notebookFrame(inst))

	// Another document: the notebook goes with the one it belonged to.
	inst.src = text
	require.True(t, notebookFrame(inst))
	inst.setSnapshotImages(nil)
	assert.Nil(t, inst.notebook.given)
	assert.False(t, notebookFrame(inst))
}

// A change to the buffer under a drawn notebook — a task box ticked — is
// read on the lane like the first load, and the notebook read last stays
// drawn meanwhile: one line in its place would cost the reader their place
// in the scroll area.
func TestAnEditedNotebookStaysDrawnWhileItIsRead(t *testing.T) {
	messages, zero, reset := scenetest.InstallCounting()
	t.Cleanup(reset)
	snap, text := snapshotOf(t, "reports")
	inst := &App{ids: c.NewWidgetIdStack()}
	defer inst.resetNotebook()
	inst.src = text
	inst.viewMode = viewRead
	inst.setSnapshotImages(newSnapshotImages(snap, "reports/"+fixture.Name, "k"))

	zero()
	require.True(t, notebookFrame(inst))
	waiting := messages()
	require.Nil(t, inst.notebook.given, "the first frame demands the read and has nothing to draw yet")
	deadline := time.Now().Add(5 * time.Second)
	for inst.notebook.given == nil {
		require.True(t, time.Now().Before(deadline), "the notebook was not prepared")
		require.True(t, notebookFrame(inst))
		time.Sleep(time.Millisecond)
	}
	first := inst.notebook.given

	inst.src = text + "\n- [x] read\n"
	zero()
	require.True(t, notebookFrame(inst))
	assert.Same(t, first, inst.notebook.given, "the frame of the edit has only demanded the read")
	assert.Greater(t, messages(), waiting, "the notebook read last is drawn, not the line that says a read is under way")
	for inst.notebook.given == first {
		require.True(t, time.Now().Before(deadline), "the edited notebook was not prepared")
		require.True(t, notebookFrame(inst))
		time.Sleep(time.Millisecond)
	}
	assert.Contains(t, string(inst.notebook.view.Notebook().Src), "- [x] read", "the view was handed the edited text")
}
