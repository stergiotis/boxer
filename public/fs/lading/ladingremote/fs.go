// Package ladingremote presents an SFTP connection as an `io/fs.FS`, and
// spawns `rclone serve sftp --stdio <remote>` to get one (ADR-0198 §SD9).
//
// It is the ingress half of the rclone story: the head in `ladingsftp` lets
// rclone read the store, and this lets the store read anything rclone can
// reach — S3, Azure, Google Drive, WebDAV, a `crypt` remote, forty-odd others
// — without the walker learning a single one of them.
//
//	src, err := ladingremote.Serve(ctx, "s3:bucket/prefix")
//	defer src.Close()
//	res, err := ladingingest.Snapshot(ctx, src, mount, policy, stores)
//
// # Why this is not in the walker
//
// The plan sketched it inside `ladingingest`. It is a package of its own
// because the walker's input is an `fs.FS` and nothing else — that is what
// makes one walker serve a grant, an embed, a zip and a remote alike. Folding
// a transport into it would make every consumer of the walker link `pkg/sftp`
// and, through it, `golang.org/x/crypto/ssh`: about 1.55 MB for a dependency
// most of them have no use for.
//
// # What a remote cannot carry
//
// Two things the acceptance for this milestone names, and neither is fixable
// here. **Modification times arrive at whole-second resolution** — that is
// SFTP's attribute width, so a snapshot of a remote records seconds where a
// snapshot of a local tree records nanoseconds. And **symlinks generally do
// not survive**: `rclone serve sftp` only exposes them with `--links`, and
// then as `.rclonelink` regular files rather than as links, so a snapshot
// through this path records what rclone showed it.
package ladingremote

import (
	"io"
	"io/fs"
	"sort"
	"strings"

	"github.com/pkg/sftp"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// FS is an `io/fs.FS` over an SFTP connection, rooted at one remote path.
//
// It implements [fs.StatFS], [fs.ReadDirFS], [fs.ReadFileFS] and
// [fs.ReadLinkFS] — the four the walker consults. It does not cache: a remote
// is live, and a walk reads each node once.
type FS struct {
	client *sftp.Client
	// root is the remote path the FS is rooted at, always absolute-ish in the
	// server's own terms and never ending in a separator.
	root string
}

var (
	_ fs.FS         = (*FS)(nil)
	_ fs.StatFS     = (*FS)(nil)
	_ fs.ReadDirFS  = (*FS)(nil)
	_ fs.ReadFileFS = (*FS)(nil)
	_ fs.ReadLinkFS = (*FS)(nil)
)

// NewFS roots an FS at root on an existing client.
//
// The client stays the caller's to close; [Serve] is the constructor that owns
// one.
func NewFS(client *sftp.Client, root string) (inst *FS, err error) {
	if client == nil {
		err = eh.Errorf("ladingremote: nil sftp client")
		return
	}
	if root == "" {
		root = "/"
	}
	inst = &FS{client: client, root: strings.TrimRight(root, "/")}
	if inst.root == "" {
		inst.root = "/"
	}
	return
}

// resolve maps an io/fs name onto the remote's own path space.
func (inst *FS) resolve(name string) (remote string, err error) {
	if !fs.ValidPath(name) {
		err = &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
		return
	}
	if name == "." {
		return inst.root, nil
	}
	if inst.root == "/" {
		return "/" + name, nil
	}
	return inst.root + "/" + name, nil
}

// Open opens a file for reading.
func (inst *FS) Open(name string) (f fs.File, err error) {
	remote, err := inst.resolve(name)
	if err != nil {
		return
	}
	rf, err := inst.client.Open(remote)
	if err != nil {
		return nil, wrap("open", name, err)
	}
	return rf, nil
}

// Stat follows symlinks, as [fs.StatFS] requires.
func (inst *FS) Stat(name string) (info fs.FileInfo, err error) {
	remote, err := inst.resolve(name)
	if err != nil {
		return
	}
	info, err = inst.client.Stat(remote)
	if err != nil {
		return nil, wrap("stat", name, err)
	}
	return
}

// Lstat describes a node without following it.
func (inst *FS) Lstat(name string) (info fs.FileInfo, err error) {
	remote, err := inst.resolve(name)
	if err != nil {
		return
	}
	info, err = inst.client.Lstat(remote)
	if err != nil {
		return nil, wrap("lstat", name, err)
	}
	return
}

// ReadLink returns a symlink's target verbatim.
//
// Whether a remote has any is the remote's business: `rclone serve sftp`
// exposes them only under `--links`, and then as `.rclonelink` regular files,
// so this usually reports "not a link" rather than a target.
func (inst *FS) ReadLink(name string) (target string, err error) {
	remote, err := inst.resolve(name)
	if err != nil {
		return
	}
	target, err = inst.client.ReadLink(remote)
	if err != nil {
		return "", wrap("readlink", name, err)
	}
	return
}

// ReadDir lists a directory, sorted by filename as [fs.ReadDirFS] requires.
//
// The entries carry the attributes the server sent with the listing, which for
// SFTP are lstat-shaped: a symlink lists as a symlink rather than as whatever
// it points at. That matters because [fs.WalkDir] takes a node's type from the
// listing, and the walker takes its `Stat` from the same place.
func (inst *FS) ReadDir(name string) (entries []fs.DirEntry, err error) {
	remote, err := inst.resolve(name)
	if err != nil {
		return
	}
	infos, err := inst.client.ReadDir(remote)
	if err != nil {
		return nil, wrap("readdir", name, err)
	}
	entries = make([]fs.DirEntry, 0, len(infos))
	for _, i := range infos {
		entries = append(entries, fs.FileInfoToDirEntry(i))
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].Name() < entries[b].Name() })
	return
}

// ReadFile reads a whole file.
//
// Streamed rather than sized-then-read: a remote can change under a reader,
// and a short read against a stale size would be a silent truncation.
func (inst *FS) ReadFile(name string) (data []byte, err error) {
	f, err := inst.Open(name)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	data, err = io.ReadAll(f)
	if err != nil {
		return nil, wrap("readfile", name, err)
	}
	return
}

// Sub roots a new FS at a subdirectory.
func (inst *FS) Sub(dir string) (sub fs.FS, err error) {
	remote, err := inst.resolve(dir)
	if err != nil {
		return
	}
	return NewFS(inst.client, remote)
}

// wrap turns an SFTP error into an fs-shaped one, so a caller matching
// [fs.ErrNotExist] works the same over a remote as over a local tree.
func wrap(op, name string, err error) error {
	if err == nil {
		return nil
	}
	// pkg/sftp already maps the protocol's NO_SUCH_FILE onto os.ErrNotExist
	// and PERMISSION_DENIED onto os.ErrPermission; what it does not do is name
	// the io/fs path, which is the half a walk's error message needs.
	return &fs.PathError{Op: op, Path: name, Err: err}
}

// Root is the remote path this FS is rooted at.
func (inst *FS) Root() string { return inst.root }
