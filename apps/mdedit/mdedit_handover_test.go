package mdedit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsftp/sftptree"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// A handed-over document opens as one of its own: the buffer holds it,
// the window keeps no store, so mdedit's own document is neither restored
// over it nor overwritten by it, and it reads as unsaved.
func TestAHandedOverDocumentIsNotPersisted(t *testing.T) {
	inst := newApp()
	wire, err := buscodec.Encode(launchcfg.MdeditLaunch{Text: "# Chat\n\nhello", Name: "Chat"})
	require.NoError(t, err)
	mustHandOver(t, inst, wire)
	assert.Equal(t, "# Chat\n\nhello", inst.src)
	assert.Nil(t, inst.store)
	assert.NotEqual(t, inst.saved, inst.src, "unsaved until saved or copied")
	assert.Contains(t, inst.status, "not autosaved")

	inst = newApp()
	handed, err := inst.handOver(nil)
	require.NoError(t, err)
	assert.False(t, handed, "a plain window")
}

// A launch config that does not decode fails the mount rather than opening
// a plain window in its place (ADR-0135 §SD4): the host checked the kind,
// so the bytes are a defect to surface, not a state to fall back from.
func TestAnUndecodableLaunchConfigFailsTheMount(t *testing.T) {
	inst := newApp()
	handed, err := inst.handOver([]byte{0xff})
	require.Error(t, err)
	assert.False(t, handed)
	assert.Contains(t, err.Error(), "decode launch config")
	assert.Empty(t, inst.src, "nothing was adopted")
}

// mustHandOver opens inst on wire and fails the test when the handover does
// not take.
func mustHandOver(t *testing.T, inst *App, wire []byte) {
	t.Helper()
	handed, err := inst.handOver(wire)
	require.NoError(t, err)
	require.True(t, handed)
}

// A handover with a snapshot offers its mount command at once, before the
// window has read the snapshot, and whether or not it can; one without
// offers none.
func TestAHandedSnapshotCanBeMountedAtOnce(t *testing.T) {
	inst := newApp()
	wire, err := buscodec.Encode(launchcfg.MdeditLaunch{Text: "# A\n", Name: "A", Mount: testRef.mount.Value(), SnapNanos: testRef.snap.UnixNano(), Path: "artefact.md"})
	require.NoError(t, err)
	mustHandOver(t, inst, wire)
	cmd, ok := inst.mountCommand()
	require.True(t, ok)
	assert.Contains(t, cmd, sftptree.SnapshotPath(testRef.mount, testRef.snap))

	inst = newApp()
	wire, err = buscodec.Encode(launchcfg.MdeditLaunch{Text: "# A\n", Name: "A"})
	require.NoError(t, err)
	mustHandOver(t, inst, wire)
	_, ok = inst.mountCommand()
	assert.False(t, ok)
}
