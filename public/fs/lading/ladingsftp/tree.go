// Package ladingsftp serves a lading store over SFTP on a pipe (ADR-0198
// §SD9).
//
// It is the store's only native transport beyond SQL, and it is deliberately
// the smallest one that could work: rclone's `sftp` backend runs a command in
// place of ssh and speaks SFTP over its pipes, so
//
//	rclone mount ":sftp,ssh='boxer fs sftp-stdio':/<mount>/latest" /mnt/x
//
// needs no socket, no port and no credential — possession of the pipe is the
// authorisation. Everything else a caller might want of a file server is
// rclone's: VFS caching, checksums, `union`, and `serve s3/webdav/nfs/…` with
// rclone's own users, keys and TLS in front.
//
// # The tree
//
//	/                              every mount this head may serve
//	/<mount>/                      every complete snapshot of it, and `latest`
//	/<mount>/latest                a symlink to the newest snapshot
//	/<mount>/<snapshot>/<path>     the snapshot itself
//
// Time travel is `cd` into another snapshot. `latest` is the only mutable name
// in the tree, which is what makes an rclone VFS cache safe to keep for a very
// long time everywhere else.
//
// # Read-only, and not by convention
//
// Every write verb — `Filewrite`, and every `Filecmd` — returns permission
// denied. The store has no update path at all (§SD1), so a head that accepted
// a write would have to invent one.
package ladingsftp

import (
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/identity/identifier"
)

// latestName is the mutable name a mount always carries, pointing at its
// newest complete snapshot.
const latestName = "latest"

// snapshotLayout is how an instant is spelled as a directory name.
//
// Compact, sortable and safe in every filesystem rclone might sync into: no
// colons, no spaces, and lexicographic order is chronological order. It keeps
// nanoseconds because a snapshot is named by its instant and two walks of a
// small mount can share a second.
const snapshotLayout = "20060102T150405.000000000Z"

// mountName is a mount's directory name: its id in lower-case hex, zero-padded
// to the full width.
//
// Hex rather than decimal because these ids are read as tagged — the tag is the
// high bits, and hex is where that is visible. Zero-padded so the names sort
// the way the ids do. A human-readable alias would have to come from the mount
// policy record, which is a lookup this head deliberately does not make: the
// tree has to be listable without reading `boxer.facts`.
func mountName(mount identifier.TaggedId) string {
	return fmt.Sprintf("%016x", mount.Value())
}

// parseMountName is the inverse.
func parseMountName(name string) (mount identifier.TaggedId, ok bool) {
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

// snapshotName spells an instant as a directory name.
func snapshotName(snap time.Time) string {
	return snap.UTC().Format(snapshotLayout)
}

// parseSnapshotName is the inverse. It is exact — the layout keeps every digit
// of the instant — so a name that round-trips addresses the snapshot it came
// from and nothing near it.
func parseSnapshotName(name string) (snap time.Time, ok bool) {
	t, err := time.ParseInLocation(snapshotLayout, name, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// levelE is how deep into the tree a request reached.
type levelE uint8

const (
	// levelRoot is `/` — the list of mounts.
	levelRoot levelE = iota
	// levelMount is `/<mount>` — the list of snapshots, plus `latest`.
	levelMount
	// levelLatest is `/<mount>/latest` — the symlink.
	levelLatest
	// levelSnapshot is `/<mount>/<snapshot>` and everything under it.
	levelSnapshot
)

// loc is one resolved request path.
type loc struct {
	level levelE
	mount identifier.TaggedId
	snap  time.Time
	// useLatest says the snapshot was addressed through the `latest` link
	// rather than by name, so which one it is has to be looked up.
	useLatest bool
	// name is the path inside the snapshot, in io/fs form: unrooted,
	// `/`-separated, "." for the snapshot's own root.
	name string
}

// resolvePath maps an SFTP path onto the tree.
//
// SFTP paths are absolute and client-normalised, but "client-normalised" is
// the client's word for it — so this cleans the path itself and refuses
// anything that escapes the root rather than trusting the caller. `..` above
// the root is not a lookup that fails, it is a request that must not be
// answered.
func resolvePath(p string) (l loc, err error) {
	clean := path.Clean("/" + strings.TrimPrefix(p, "/"))
	if clean == "/" {
		l.level = levelRoot
		return
	}
	segs := strings.Split(strings.TrimPrefix(clean, "/"), "/")

	mount, ok := parseMountName(segs[0])
	if !ok {
		err = fs.ErrNotExist
		return
	}
	l.mount = mount
	if len(segs) == 1 {
		l.level = levelMount
		return
	}

	if segs[1] == latestName {
		if len(segs) == 2 {
			// The link itself: Lstat and Readlink report it as one.
			l.level = levelLatest
			return
		}
		// A path *through* the link. Resolving it is the server's job, not the
		// client's — every filesystem walks a symlink for a path operation,
		// and rclone lists `latest` and then addresses its children through
		// that name rather than re-issuing against the target.
		l.level, l.useLatest = levelSnapshot, true
		l.name = strings.Join(segs[2:], "/")
		if !fs.ValidPath(l.name) {
			err = fs.ErrInvalid
		}
		return
	}

	snap, ok := parseSnapshotName(segs[1])
	if !ok {
		err = fs.ErrNotExist
		return
	}
	l.level, l.snap = levelSnapshot, snap
	if len(segs) == 2 {
		l.name = "."
		return
	}
	l.name = strings.Join(segs[2:], "/")
	if !fs.ValidPath(l.name) {
		err = fs.ErrInvalid
		return
	}
	return
}

// dirInfo is a synthetic directory of the virtual tree — a mount or a snapshot.
// It has no source of its own; it exists because the tree has levels the store
// does not.
type dirInfo struct {
	name  string
	mtime time.Time
}

func (inst dirInfo) Name() string       { return inst.name }
func (inst dirInfo) Size() int64        { return 0 }
func (inst dirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (inst dirInfo) ModTime() time.Time { return unknownTimeAsEpoch(inst.mtime) }
func (inst dirInfo) IsDir() bool        { return true }
func (inst dirInfo) Sys() any           { return nil }

// linkInfo is the `latest` symlink.
type linkInfo struct {
	name   string
	mtime  time.Time
	target string
}

func (inst linkInfo) Name() string       { return inst.name }
func (inst linkInfo) Size() int64        { return int64(len(inst.target)) }
func (inst linkInfo) Mode() fs.FileMode  { return fs.ModeSymlink | 0o777 }
func (inst linkInfo) ModTime() time.Time { return unknownTimeAsEpoch(inst.mtime) }

// unknownTimeAsEpoch keeps a level with no time of its own — the tree root,
// and a mount, neither of which the store dates — from reporting one twenty
// years in the future.
//
// pkg/sftp marshals mtime as uint32(fi.ModTime().Unix()), and the zero
// time.Time is -62135596800, whose low 32 bits are 2042-07-14. Anything
// comparing directory times over SFTP (rclone --update, lsl, a VFS dir cache)
// then acts on that date. The epoch is the honest answer — the conventional
// "not known" — and it does not wrap.
func unknownTimeAsEpoch(t time.Time) time.Time {
	if t.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return t
}
func (inst linkInfo) IsDir() bool { return false }
func (inst linkInfo) Sys() any    { return nil }
