package ladingsftp

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadapter"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Head serves one lading store over SFTP.
//
// # One store, one goroutine
//
// A generated record store is single-goroutine (ADR-0100) and the SFTP request
// server is not: it answers packets concurrently. So every path into the store
// takes the head's lock — including the reads a client makes through a handle
// after the handler that opened it returned. That makes the head effectively
// serial against the store, which is the right trade for a surface the ADR
// already describes as batch-shaped rather than a hot serving path.
//
// # Views are cached, because a snapshot cannot change
//
// One [ladingadapter.FS] per (mount, snapshot), kept for the head's lifetime.
// The adapter's own caches are safe to keep for the same reason: the view is
// pinned, so nothing it has read can go stale. A head that served a hundred
// snapshots would hold a hundred views; that is bounded by what a client
// actually visits, and a fresh head starts empty.
type Head struct {
	mu     sync.Mutex
	exec   recordstore.ExecutorI
	stores lading.Stores
	vis    ladingsql.MountVisibilityI
	ctx    context.Context
	views  map[viewKey]*ladingadapter.FS
	// snaps caches each mount's complete, unexpired snapshots. It is what
	// makes a snapshot addressable by name only if it is one — a walk that
	// died before its root row still has an addressable instant, and the
	// caller who holds the failed Result knows it.
	snaps map[identifier.TaggedId][]ladingadapter.Snapshot
}

type viewKey struct {
	mount identifier.TaggedId
	snap  int64
}

// Config parameterises a head.
type Config struct {
	// Exec reaches the server. Used for the snapshot index — which mounts
	// exist, and which snapshots each has.
	Exec recordstore.ExecutorI
	// Stores is the pair a snapshot is read through.
	Stores lading.Stores
	// Visibility decides which mounts this head serves. Nil serves none:
	// possession of the pipe is the authorisation for the *store* (§SD9), but
	// which mounts inside it are visible is still a decision, and defaulting
	// it to "all" would make it one nobody took.
	Visibility ladingsql.MountVisibilityI
	// Ctx bounds the head's queries. Nil means context.Background.
	Ctx context.Context
}

// New builds a head.
func New(cfg Config) (inst *Head, err error) {
	if cfg.Exec == nil {
		err = eh.Errorf("no executor")
		return
	}
	if cfg.Stores.Meta == nil {
		err = eh.Errorf("no entry store")
		return
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	inst = &Head{
		exec: cfg.Exec, stores: cfg.Stores, vis: cfg.Visibility, ctx: ctx,
		views: map[viewKey]*ladingadapter.FS{},
		snaps: map[identifier.TaggedId][]ladingadapter.Snapshot{},
	}
	return
}

// Serve speaks SFTP over rwc until the peer hangs up.
//
// rwc is a pipe — stdin/stdout under `boxer fs sftp-stdio`, or whatever a test
// hands it. There is no handshake and no authentication here by design: the
// only way to reach this is to already hold the pipe.
func (inst *Head) Serve(rwc io.ReadWriteCloser) (err error) {
	srv := sftp.NewRequestServer(rwc, sftp.Handlers{
		FileGet:  inst,
		FilePut:  inst,
		FileCmd:  inst,
		FileList: inst,
	})
	err = srv.Serve()
	if errors.Is(err, io.EOF) {
		// The peer closed the pipe, which is how every session ends.
		err = nil
	}
	_ = srv.Close()
	return
}

var (
	_ sftp.FileReader         = (*Head)(nil)
	_ sftp.FileWriter         = (*Head)(nil)
	_ sftp.FileCmder          = (*Head)(nil)
	_ sftp.FileLister         = (*Head)(nil)
	_ sftp.LstatFileLister    = (*Head)(nil)
	_ sftp.ReadlinkFileLister = (*Head)(nil)
)

// --- the write half, which does not exist.

// Filewrite refuses. The store is append-only and has no update path at all
// (§SD1), so accepting a write would mean inventing one.
func (inst *Head) Filewrite(*sftp.Request) (io.WriterAt, error) {
	return nil, os.ErrPermission
}

// Filecmd refuses every mutating verb — Setstat, Rename, Rmdir, Mkdir, Link,
// Symlink, Remove — for the same reason.
func (inst *Head) Filecmd(*sftp.Request) error {
	return os.ErrPermission
}

// --- reading.

// Fileread opens a file of a snapshot.
//
// The returned reader is the adapter's own handle, so a client reading in
// 32 KiB chunks pays one block query per block rather than one per chunk: the
// handle caches the blocks it has decoded, and consecutive chunks inside a
// block hit that cache. That is what makes this head usable by rclone at all.
func (inst *Head) Fileread(req *sftp.Request) (ra io.ReaderAt, err error) {
	l, err := resolvePath(req.Filepath)
	if err != nil {
		return nil, err
	}
	if l.level != levelSnapshot {
		// A mount, a snapshot directory or the root — all directories, and
		// `latest` is a symlink the client must read rather than open.
		return nil, os.ErrPermission
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()

	view, err := inst.viewAt(l)
	if err != nil {
		return nil, err
	}
	f, err := view.Open(l.name)
	if err != nil {
		return nil, err
	}
	inner, ok := f.(io.ReaderAt)
	if !ok {
		_ = f.Close()
		return nil, os.ErrInvalid
	}
	return &lockedReaderAt{head: inst, file: f, ra: inner}, nil
}

// lockedReaderAt carries the head's lock into the reads a client makes after
// the handler returned. The request server calls ReadAt concurrently, and what
// it reads through is a store that is not.
type lockedReaderAt struct {
	head *Head
	file fs.File
	ra   io.ReaderAt
}

func (inst *lockedReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	inst.head.mu.Lock()
	defer inst.head.mu.Unlock()
	return inst.ra.ReadAt(p, off)
}

// Close is called by the request server when the handle closes.
func (inst *lockedReaderAt) Close() error {
	inst.head.mu.Lock()
	defer inst.head.mu.Unlock()
	return inst.file.Close()
}

// --- listing, stat and readlink.

// Filelist answers List and Stat. Readlink has its own method below, and Lstat
// likewise, so this one never has to guess which of the three it is serving
// from the path alone.
func (inst *Head) Filelist(req *sftp.Request) (l sftp.ListerAt, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	switch req.Method {
	case "List":
		return inst.list(req.Filepath)
	default:
		return inst.stat(req.Filepath, true)
	}
}

// Lstat describes a node without following it, which is what makes `latest`
// visible as a link rather than as the directory it points at.
func (inst *Head) Lstat(req *sftp.Request) (l sftp.ListerAt, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.stat(req.Filepath, false)
}

// Readlink returns a link's target as a path, which the FileInfo-shaped
// FileLister cannot express (its Name is only a base name).
func (inst *Head) Readlink(p string) (target string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	loc, err := resolvePath(p)
	if err != nil {
		return "", err
	}
	switch loc.level {
	case levelLatest:
		snap, found, lerr := inst.latest(loc.mount)
		if lerr != nil {
			return "", lerr
		}
		if !found {
			return "", fs.ErrNotExist
		}
		// Relative, so the link stays correct however the tree is mounted.
		return snapshotName(snap), nil
	case levelSnapshot:
		view, verr := inst.viewAt(loc)
		if verr != nil {
			return "", verr
		}
		t, verr := view.ReadLink(loc.name)
		if verr != nil {
			return "", verr
		}
		return relativeTarget(loc.name, t), nil
	}
	return "", fs.ErrInvalid
}

// relativeTarget rewrites an absolute link target into one the client will
// resolve where the adapter resolves it.
//
// A snapshot's absolute target is rooted at the *snapshot*, which is the only
// root a snapshot has; a client resolves it against the root of the SFTP tree,
// which is the mount list. So `/a/b.txt` would take the adapter to the
// snapshot's a/b.txt and take rclone to a mount named "a" — ENOENT, and a
// dangling link written by `rclone copy --links` where the adapter would have
// copied the file. Handing back the equivalent relative path makes the two
// agree without the client having to know where the snapshot begins.
//
// A relative target already means the same thing to both and is passed
// through.
func relativeTarget(from, target string) string {
	if !strings.HasPrefix(target, "/") {
		return target
	}
	dst := path.Clean(strings.TrimPrefix(target, "/"))
	if dst == "" || dst == "." {
		dst = "."
	}
	rel, err := filepath.Rel(path.Dir(from), dst)
	if err != nil {
		// Cannot happen for two cleaned, unrooted io/fs paths; if it ever
		// does, the verbatim target is no worse than a wrong relative one.
		return target
	}
	return filepath.ToSlash(rel)
}

// list serves a directory at any level of the tree.
func (inst *Head) list(p string) (sftp.ListerAt, error) {
	l, err := resolvePath(p)
	if err != nil {
		return nil, err
	}
	switch l.level {
	case levelRoot:
		return inst.listMounts()
	case levelMount:
		return inst.listSnapshots(l.mount)
	case levelLatest:
		// Listing the link means listing what it points at, the way opendir on
		// a symlink does everywhere else. Lstat and Readlink are where a client
		// learns it is a link.
		l.level, l.useLatest, l.name = levelSnapshot, true, "."
	}
	view, err := inst.viewAt(l)
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(view, l.name)
	if err != nil {
		return nil, err
	}
	out := make([]os.FileInfo, 0, len(entries))
	for _, e := range entries {
		info, ierr := e.Info()
		if ierr != nil {
			return nil, ierr
		}
		out = append(out, info)
	}
	return listerAt(out), nil
}

// stat describes one node. follow says whether a symlink is resolved, which is
// the whole difference between Stat and Lstat.
func (inst *Head) stat(p string, follow bool) (sftp.ListerAt, error) {
	l, err := resolvePath(p)
	if err != nil {
		return nil, err
	}
	switch l.level {
	case levelRoot:
		return listerAt{dirInfo{name: "/", mtime: time.Time{}}}, nil
	case levelMount:
		if err = inst.checkMount(l.mount); err != nil {
			return nil, err
		}
		return listerAt{dirInfo{name: mountName(l.mount)}}, nil
	case levelLatest:
		snap, found, lerr := inst.latest(l.mount)
		if lerr != nil {
			return nil, lerr
		}
		if !found {
			return nil, fs.ErrNotExist
		}
		if !follow {
			return listerAt{linkInfo{name: latestName, mtime: snap, target: snapshotName(snap)}}, nil
		}
		return listerAt{dirInfo{name: latestName, mtime: snap}}, nil
	}
	view, err := inst.viewAt(l)
	if err != nil {
		return nil, err
	}
	var info os.FileInfo
	if follow {
		info, err = view.Stat(l.name)
	} else {
		info, err = view.Lstat(l.name)
	}
	if err != nil {
		return nil, err
	}
	return listerAt{info}, nil
}

// listMounts is the root: every mount this head may serve that has at least
// one complete snapshot.
//
// "Has a complete snapshot" rather than "exists": a mount whose only walk died
// half way has rows but nothing to show, and a directory that listed it would
// be a directory a client cannot enter.
func (inst *Head) listMounts() (sftp.ListerAt, error) {
	mounts, err := ladingadapter.Mounts(inst.ctx, inst.exec)
	if err != nil {
		return nil, err
	}
	out := make([]os.FileInfo, 0, len(mounts))
	for _, m := range mounts {
		if inst.checkMount(m) != nil {
			continue
		}
		out = append(out, dirInfo{name: mountName(m)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return listerAt(out), nil
}

// listSnapshots is a mount: its complete snapshots, oldest first, plus the
// `latest` link.
func (inst *Head) listSnapshots(mount identifier.TaggedId) (sftp.ListerAt, error) {
	if err := inst.checkMount(mount); err != nil {
		return nil, err
	}
	snaps, err := inst.snapshotsOf(mount)
	if err != nil {
		return nil, err
	}
	if len(snaps) == 0 {
		return nil, fs.ErrNotExist
	}
	out := make([]os.FileInfo, 0, len(snaps)+1)
	for _, s := range snaps {
		out = append(out, dirInfo{name: snapshotName(s.Snap), mtime: s.Snap})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	newest := snaps[0].Snap // Snapshots is newest-first
	out = append(out, linkInfo{name: latestName, mtime: newest, target: snapshotName(newest)})
	return listerAt(out), nil
}

// snapshotsOf is a mount's complete, unexpired snapshots, newest first,
// cached for the life of the head.
//
// Caching is safe for the same reason the views are: a snapshot is immutable
// and a session is short. It can go stale in one direction — a snapshot taken
// or expired during a session — which is the same staleness a client already
// has from its own directory cache.
func (inst *Head) snapshotsOf(mount identifier.TaggedId) ([]ladingadapter.Snapshot, error) {
	if err := inst.checkMount(mount); err != nil {
		return nil, err
	}
	if s, hit := inst.snaps[mount]; hit {
		return s, nil
	}
	s, err := ladingadapter.Snapshots(inst.ctx, inst.exec, mount)
	if err != nil {
		return nil, err
	}
	inst.snaps[mount] = s
	return s, nil
}

// latest is a mount's newest complete snapshot.
func (inst *Head) latest(mount identifier.TaggedId) (snap time.Time, found bool, err error) {
	if err = inst.checkMount(mount); err != nil {
		return
	}
	snaps, err := inst.snapshotsOf(mount)
	if err != nil || len(snaps) == 0 {
		return
	}
	return snaps[0].Snap, true, nil
}

// checkMount refuses a mount this head may not serve — as absent rather than
// as forbidden, because a tree that answered "permission denied" for one name
// and "no such file" for another would let a client enumerate what it cannot
// read.
func (inst *Head) checkMount(mount identifier.TaggedId) error {
	if inst.vis == nil || !inst.vis.VisibleMount(mount) {
		return fs.ErrNotExist
	}
	return nil
}

// viewAt is [Head.view] for a resolved path, following the `latest` link when
// the path came through it.
func (inst *Head) viewAt(l loc) (*ladingadapter.FS, error) {
	snap := l.snap
	if l.useLatest {
		newest, found, err := inst.latest(l.mount)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fs.ErrNotExist
		}
		snap = newest
	}
	return inst.view(l.mount, snap)
}

// view is the cached adapter for one snapshot.
func (inst *Head) view(mount identifier.TaggedId, snap time.Time) (*ladingadapter.FS, error) {
	// Completeness, not just visibility. ladingingest.Snapshot returns its
	// Result even when the walk failed, so a half-written instant is a real
	// address; listSnapshots hides it, but nothing stopped a client from
	// naming it directly and reading a tree with no root row. §SD6 makes "has
	// a root row" the rule, and this is where the head applies it.
	snaps, err := inst.snapshotsOf(mount)
	if err != nil {
		return nil, err
	}
	want := snap.UTC()
	complete := false
	for _, s := range snaps {
		if s.Snap.Equal(want) {
			complete = true
			break
		}
	}
	if !complete {
		return nil, fs.ErrNotExist
	}
	k := viewKey{mount: mount, snap: snap.UTC().UnixNano()}
	if v, hit := inst.views[k]; hit {
		return v, nil
	}
	v, err := ladingadapter.Open(inst.stores, mount, snap, ladingadapter.WithContext(inst.ctx))
	if err != nil {
		return nil, err
	}
	inst.views[k] = v
	return v, nil
}

// listerAt is the []os.FileInfo shape the request server pages through.
type listerAt []os.FileInfo

func (inst listerAt) ListAt(out []os.FileInfo, off int64) (n int, err error) {
	if off >= int64(len(inst)) {
		return 0, io.EOF
	}
	n = copy(out, inst[off:])
	if n < len(out) {
		err = io.EOF
	}
	return
}
