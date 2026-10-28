package sftptree

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identifier"
)

func TestTheNamesRoundTrip(t *testing.T) {
	mount := identifier.TaggedId(0x84000003A3EC7AEA)
	assert.Equal(t, "84000003a3ec7aea", MountName(mount))
	back, ok := ParseMountName(MountName(mount))
	require.True(t, ok)
	assert.Equal(t, mount, back)
	_, ok = ParseMountName("84000003a3ec7ae")
	assert.False(t, ok, "short")
	_, ok = ParseMountName("84000003a3ec7aeg")
	assert.False(t, ok, "not hex")

	snap := time.Date(2026, 10, 27, 14, 18, 45, 307515194, time.UTC)
	assert.Equal(t, "20261027T141845.307515194Z", SnapshotName(snap))
	at, ok := ParseSnapshotName(SnapshotName(snap.In(time.FixedZone("x", 3600))))
	require.True(t, ok)
	assert.True(t, at.Equal(snap), "every digit of the instant survives")
	_, ok = ParseSnapshotName(Latest)
	assert.False(t, ok)
}

// The remote is the one the howto documents, aimed at one snapshot.
func TestTheRemoteNamesOneSnapshot(t *testing.T) {
	mount := identifier.TaggedId(0x84000003A3EC7AEA)
	snap := time.Unix(0, 1761574725307515194)
	assert.Equal(t, `:sftp,ssh="boxer fs sftp-stdio --mount 0x84000003a3ec7aea",shell_type=unix:/84000003a3ec7aea/`+SnapshotName(snap),
		RcloneRemote(mount, snap))
	assert.Equal(t, "rclone mount --read-only '"+RcloneRemote(mount, snap)+"' /mnt/x", RcloneMount(mount, snap, "/mnt/x"))
}
