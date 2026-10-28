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
	require.True(t, inst.handOver(wire))
	assert.Equal(t, "# Chat\n\nhello", inst.src)
	assert.Nil(t, inst.store)
	assert.NotEqual(t, inst.saved, inst.src, "unsaved until saved or copied")
	assert.Contains(t, inst.status, "not autosaved")

	inst = newApp()
	assert.False(t, inst.handOver(nil), "a plain window")
	assert.False(t, inst.handOver([]byte{0xff}), "an undecodable config opens as usual")
}

// A handover with a snapshot offers its mount command at once, before the
// window has read the snapshot, and whether or not it can; one without
// offers none.
func TestAHandedSnapshotCanBeMountedAtOnce(t *testing.T) {
	inst := newApp()
	wire, err := buscodec.Encode(launchcfg.MdeditLaunch{Text: "# A\n", Name: "A", Mount: testRef.mount.Value(), SnapNanos: testRef.snap.UnixNano(), Path: "artefact.md"})
	require.NoError(t, err)
	require.True(t, inst.handOver(wire))
	cmd, ok := inst.mountCommand()
	require.True(t, ok)
	assert.Contains(t, cmd, sftptree.SnapshotPath(testRef.mount, testRef.snap))

	inst = newApp()
	wire, err = buscodec.Encode(launchcfg.MdeditLaunch{Text: "# A\n", Name: "A"})
	require.NoError(t, err)
	require.True(t, inst.handOver(wire))
	_, ok = inst.mountCommand()
	assert.False(t, ok)
}
