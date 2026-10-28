package mdedit

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/lading/ladingsftp/sftptree"
	"github.com/stergiotis/boxer/public/identity/identifier"
)

// testRef is the snapshot the tests' documents are read from.
var testRef = snapshotRef{mount: identifier.TaggedId(0x84000003A3EC7AEA), snap: time.Unix(0, 1761574725307515194)}

// drainLoadUntil pumps drainSnapshotLoad the way frames would, until the
// pending load lands or the deadline passes.
func drainLoadUntil(t *testing.T, inst *App) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		inst.drainSnapshotLoad()
		if inst.files.loadKey == "" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("snapshot load did not land in time")
}

// TestSnapshotLoad_ReplacesTheBufferLikeAnOpen: the loaded document lands the
// Open trio (rebind, checkpoint, autosave carry) plus the snapshot identity.
func TestSnapshotLoad_ReplacesTheBufferLikeAnOpen(t *testing.T) {
	inst := &App{}
	fsys := fstest.MapFS{"notes/readme.md": {Data: []byte("# From the snapshot\n")}}

	inst.requestSnapshotLoad(fsys, "docs", testRef, "notes/readme.md")
	require.NotEmpty(t, inst.files.loadKey, "a clean buffer loads on the first activation")
	drainLoadUntil(t, inst)

	assert.Equal(t, "# From the snapshot\n", inst.src)
	assert.False(t, inst.dirty())
	assert.True(t, inst.rebindSrc, "a loaded buffer is a rebind the editor must be told about")
	assert.True(t, inst.readFromSnapshot)
	assert.Equal(t, "docs:notes/readme.md @ latest", inst.readName)
	assert.False(t, inst.fileBound(), "a snapshot source is not somewhere to save")

	label, tip, show := inst.fileBadge()
	require.True(t, show)
	assert.Equal(t, "docs:notes/readme.md @ latest", label)
	assert.Equal(t, tipFileSnapshot, tip, "the badge states the snapshot contract, not the Powerbox one")
}

// A document read from a snapshot offers the command that mounts that
// snapshot, and no other document does.
func TestASnapshotDocumentOffersItsMountCommand(t *testing.T) {
	inst := &App{}
	_, ok := inst.mountCommand()
	assert.False(t, ok)
	inst.requestSnapshotLoad(fstest.MapFS{"a.md": {Data: []byte("# a\n")}}, "docs", testRef, "a.md")
	drainLoadUntil(t, inst)
	cmd, ok := inst.mountCommand()
	require.True(t, ok)
	assert.Equal(t, sftptree.RcloneMount(testRef.mount, testRef.snap, mountCommandDir), cmd)
	assert.Contains(t, cmd, "/84000003a3ec7aea/"+sftptree.SnapshotName(testRef.snap), "the snapshot read, not latest")

	inst.adoptDocument("# other\n", "other")
	_, ok = inst.mountCommand()
	assert.False(t, ok, "another document has no snapshot behind it")
}

// TestSnapshotLoad_SharesTheOpenGuard: a dirty buffer refuses the first
// activation and arms; the second means it.
func TestSnapshotLoad_SharesTheOpenGuard(t *testing.T) {
	inst := &App{src: "typed, unsaved"}
	fsys := fstest.MapFS{"a.md": {Data: []byte("# a\n")}}

	inst.requestSnapshotLoad(fsys, "docs", testRef, "a.md")
	assert.Empty(t, inst.files.loadKey, "the first activation on a dirty buffer refuses")
	assert.True(t, inst.confirmDiscard)
	assert.Contains(t, inst.status, "unsaved changes")

	// Typing disarms — the standing clearDiscardConfirm rule.
	inst.src = "typed more"
	inst.clearDiscardConfirm()
	assert.False(t, inst.confirmDiscard)

	// Armed and repeated straight after: proceed.
	inst.requestSnapshotLoad(fsys, "docs", testRef, "a.md")
	require.True(t, inst.confirmDiscard)
	inst.requestSnapshotLoad(fsys, "docs", testRef, "a.md")
	require.NotEmpty(t, inst.files.loadKey)
	drainLoadUntil(t, inst)
	assert.Equal(t, "# a\n", inst.src)
}

// TestSnapshotLoad_RefusesAnOversizeFile: the size gate is a refusal with the
// size in it, never a partial load.
func TestSnapshotLoad_RefusesAnOversizeFile(t *testing.T) {
	inst := &App{}
	big := strings.Repeat("x", int(maxSnapshotDocBytes)+1)
	fsys := fstest.MapFS{"big.md": {Data: []byte(big)}}

	inst.requestSnapshotLoad(fsys, "docs", testRef, "big.md")
	require.NotEmpty(t, inst.files.loadKey)
	drainLoadUntil(t, inst)

	assert.Empty(t, inst.src, "no partial load")
	assert.Contains(t, inst.status, "load failed")
}

// TestSnapshotLoad_EndsAnActiveFollow: the loaded document is a different
// one; following the previous file would reload the wrong text over it.
func TestSnapshotLoad_EndsAnActiveFollow(t *testing.T) {
	inst := &App{followActive: true, diskChanged: true}
	inst.logger = inst.logger.Level(0)
	fsys := fstest.MapFS{"b.md": {Data: []byte("# b\n")}}

	inst.requestSnapshotLoad(fsys, "docs", testRef, "b.md")
	drainLoadUntil(t, inst)

	assert.False(t, inst.followActive)
	assert.False(t, inst.diskChanged)
	assert.Equal(t, "# b\n", inst.src)
}

// A handover's read of its snapshot replaces the text the launch carried,
// but not edits made while it ran, nor a document opened meanwhile.
func TestAHandedSnapshotReplacesOnlyTheTextItCameWith(t *testing.T) {
	fsys := fstest.MapFS{"artefact.md": {Data: []byte("# From the snapshot\n")}}
	start := func() *App {
		inst := &App{}
		inst.adoptDocument("# handed\n", "handed")
		inst.startHandedLoad(fsys, &handedSnapshot{mount: testRef.mount, snap: testRef.snap, name: "handed", path: "artefact.md", text: "# handed\n"})
		return inst
	}
	inst := start()
	drainLoadUntil(t, inst)
	assert.Equal(t, "# From the snapshot\n", inst.src)
	assert.NotNil(t, inst.snapImages)

	inst = start()
	inst.src = "# handed, and typed into\n"
	drainLoadUntil(t, inst)
	assert.Equal(t, "# handed, and typed into\n", inst.src)
	assert.NotNil(t, inst.snapImages, "the images still come from the snapshot")
	_, ok := inst.mountCommand()
	assert.True(t, ok, "and the snapshot can still be mounted")
	assert.Contains(t, inst.status, "kept your edits")

	inst = start()
	inst.adoptDocument("# another\n", "another")
	assert.Empty(t, inst.files.loadKey, "the other document cancelled the read")
	assert.Nil(t, inst.handedSnap)
	inst.drainSnapshotLoad()
	assert.Equal(t, "# another\n", inst.src)
}
