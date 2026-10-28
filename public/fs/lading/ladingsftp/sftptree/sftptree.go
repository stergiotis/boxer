// Package sftptree names the tree the lading SFTP head serves (ADR-0198
// §SD9): a mount's directory, a snapshot's directory, and the rclone remote
// that reaches one.
//
// It is apart from ladingsftp so a client that only spells the names — an
// app offering the command that mounts what it shows — does not carry the
// SFTP server with it.
package sftptree

import (
	"fmt"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/identity/identifier"
)

// Latest is the mutable name a mount always carries, pointing at its newest
// complete snapshot.
const Latest = "latest"

// snapshotLayout is how an instant is spelled as a directory name.
//
// Compact, sortable and safe in every filesystem rclone might sync into: no
// colons, no spaces, and lexicographic order is chronological order. It keeps
// nanoseconds because a snapshot is named by its instant and two walks of a
// small mount can share a second.
const snapshotLayout = "20060102T150405.000000000Z"

// MountName is a mount's directory name: its id in lower-case hex, zero-padded
// to the full width.
//
// Hex rather than decimal because these ids are read as tagged — the tag is the
// high bits, and hex is where that is visible. Zero-padded so the names sort
// the way the ids do. A human-readable alias would have to come from the mount
// policy record, which is a lookup the head deliberately does not make: the
// tree has to be listable without reading `boxer.facts`.
func MountName(mount identifier.TaggedId) string {
	return fmt.Sprintf("%016x", mount.Value())
}

// ParseMountName is the inverse.
func ParseMountName(name string) (mount identifier.TaggedId, ok bool) {
	if len(name) != 16 {
		return 0, false
	}
	v, err := strconv.ParseUint(name, 16, 64)
	if err != nil {
		return 0, false
	}
	mount = identifier.TaggedId(v)
	return mount, mount.IsValid()
}

// SnapshotName spells an instant as a directory name.
func SnapshotName(snap time.Time) string {
	return snap.UTC().Format(snapshotLayout)
}

// ParseSnapshotName is the inverse. It is exact — the layout keeps every digit
// of the instant — so a name that round-trips addresses the snapshot it came
// from and nothing near it.
func ParseSnapshotName(name string) (snap time.Time, ok bool) {
	t, err := time.ParseInLocation(snapshotLayout, name, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// SnapshotPath is a snapshot's directory in the tree, rooted.
func SnapshotPath(mount identifier.TaggedId, snap time.Time) string {
	return "/" + MountName(mount) + "/" + SnapshotName(snap)
}

// RcloneRemote is the rclone connection string for one snapshot, over a head
// that `boxer fs sftp-stdio` runs in place of ssh and that may read this
// mount only. The command is spelled as the reader's shell would find it; the
// head connects with the reader's own `CLICKHOUSE_*` settings.
func RcloneRemote(mount identifier.TaggedId, snap time.Time) string {
	return fmt.Sprintf(`:sftp,ssh="boxer fs sftp-stdio --mount 0x%016x",shell_type=unix:%s`,
		mount.Value(), SnapshotPath(mount, snap))
}

// RcloneMount is the command that mounts one snapshot read-only at dir.
func RcloneMount(mount identifier.TaggedId, snap time.Time, dir string) string {
	return "rclone mount --read-only '" + RcloneRemote(mount, snap) + "' " + dir
}
