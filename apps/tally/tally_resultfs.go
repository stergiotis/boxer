package tally

import (
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

// tally_resultfs.go is the Results pane's tree (ADR-0222 §SD4): the rows a
// path-set query returned, presented as an `io/fs.FS` so the browser widget,
// the preview and the attribute panes read a query result the way they read a
// snapshot. Nothing here reads the store — every leaf delegates to the
// adapter view over the snapshot the row came from, opened on demand.

// resultLeaf is one row of a path-set result: where the file is, and where it
// sits in the virtual tree.
type resultLeaf struct {
	loc  location
	real string // the path inside that snapshot
}

// resultFS is a read-only tree over a set of leaves. Directories are
// synthesised from the leaf paths — a query returns files, not the
// directories above them — and a leaf that is itself an ancestor of another
// leaf becomes one of those directories.
type resultFS struct {
	// open resolves a location to its adapter view. Called once per
	// location and cached, because opening one is the expensive half.
	open   func(location) (fs.FS, error)
	leaves map[string]resultLeaf
	// kids lists each directory's children by virtual path, sorted by name
	// with directories first — the order fs.ReadDir promises is by name, so
	// the widget sorts anyway; this only keeps the listing stable.
	kids  map[string][]string
	dirs  map[string]struct{}
	views map[location]fs.FS
}

var (
	_ fs.FS        = (*resultFS)(nil)
	_ fs.ReadDirFS = (*resultFS)(nil)
)

// newResultFS builds the tree from leaves keyed by virtual path. dirs are
// paths the result declared to be directories; the directories implied by a
// leaf's own path are found here and need no declaration.
func newResultFS(open func(location) (fs.FS, error), leaves map[string]resultLeaf, dirs map[string]struct{}) (inst *resultFS) {
	inst = &resultFS{
		open:   open,
		leaves: leaves,
		kids:   make(map[string][]string, len(leaves)),
		dirs:   map[string]struct{}{".": {}},
		views:  make(map[location]fs.FS, 4),
	}
	// The directory set, complete before anything is linked: what the result
	// declared, plus every ancestor of a declared directory and of a leaf.
	markAncestors := func(p string) {
		for dir := path.Dir(p); ; dir = path.Dir(dir) {
			inst.dirs[dir] = struct{}{}
			if dir == "." {
				break
			}
		}
	}
	for p := range dirs {
		inst.dirs[p] = struct{}{}
		markAncestors(p)
	}
	for p := range leaves {
		markAncestors(p)
	}
	// Then the links, parent to child. A leaf that is also a directory —
	// because another leaf sits under it — is linked once, as the directory.
	seen := make(map[string]struct{}, len(leaves)+len(inst.dirs))
	add := func(p string) {
		if p == "." {
			return
		}
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		parent := path.Dir(p)
		inst.kids[parent] = append(inst.kids[parent], p)
	}
	for p := range inst.dirs {
		add(p)
	}
	for p := range leaves {
		add(p)
	}
	for dir, children := range inst.kids {
		sort.Slice(children, func(i, j int) bool {
			return path.Base(children[i]) < path.Base(children[j])
		})
		inst.kids[dir] = children
	}
	return
}

// isDir reports whether a virtual path is one of the synthesised
// directories. A leaf that is also an ancestor is a directory and is not
// openable as a file — which is what it is in the snapshot too.
func (inst *resultFS) isDir(name string) (yes bool) {
	_, yes = inst.dirs[name]
	return
}

// Leaf resolves a virtual path to the snapshot and path behind it. The
// Results pane uses it to point Preview, Info and History at the row a
// selection names.
func (inst *resultFS) Leaf(name string) (leaf resultLeaf, ok bool) {
	if inst.isDir(name) {
		return
	}
	leaf, ok = inst.leaves[name]
	return
}

func (inst *resultFS) view(loc location) (fsys fs.FS, err error) {
	if v, ok := inst.views[loc]; ok {
		return v, nil
	}
	fsys, err = inst.open(loc)
	if err != nil {
		return
	}
	inst.views[loc] = fsys
	return
}

func (inst *resultFS) Open(name string) (f fs.File, err error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if inst.isDir(name) {
		return &resultDir{fsys: inst, name: name}, nil
	}
	leaf, ok := inst.leaves[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	under, err := inst.view(leaf.loc)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return under.Open(leaf.real)
}

func (inst *resultFS) ReadDir(name string) (entries []fs.DirEntry, err error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	if !inst.isDir(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	kids := inst.kids[name]
	entries = make([]fs.DirEntry, 0, len(kids))
	for _, k := range kids {
		entries = append(entries, resultEntry{fsys: inst, virt: k})
	}
	return
}

// resultEntry is one row of a synthesised listing. Its Info stats the
// underlying snapshot for a file, so size and mtime are the store's, and
// answers from nothing for a synthesised directory — there is no directory
// row behind it to read.
type resultEntry struct {
	fsys *resultFS
	virt string
}

var _ fs.DirEntry = resultEntry{}

func (e resultEntry) Name() string { return path.Base(e.virt) }

func (e resultEntry) IsDir() bool { return e.fsys.isDir(e.virt) }

func (e resultEntry) Type() (m fs.FileMode) {
	if e.IsDir() {
		m = fs.ModeDir
	}
	return
}

func (e resultEntry) Info() (info fs.FileInfo, err error) {
	if e.IsDir() {
		return resultDirInfo{name: e.Name()}, nil
	}
	leaf, ok := e.fsys.leaves[e.virt]
	if !ok {
		return nil, &fs.PathError{Op: "stat", Path: e.virt, Err: fs.ErrNotExist}
	}
	under, err := e.fsys.view(leaf.loc)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: e.virt, Err: err}
	}
	info, err = fs.Stat(under, leaf.real)
	if err != nil {
		return
	}
	// The name a listing shows is the virtual one: a prefixed row is named
	// by its leaf, not by the file's name inside its own snapshot, and for
	// an unprefixed row the two are the same.
	return resultFileInfo{FileInfo: info, name: e.Name()}, nil
}

// resultDirInfo is a synthesised directory's stat: a name and the directory
// bit. It carries no size and no time because nothing recorded any — a
// directory here is an artefact of the paths in the result.
type resultDirInfo struct{ name string }

var _ fs.FileInfo = resultDirInfo{}

func (i resultDirInfo) Name() string { return i.name }

func (i resultDirInfo) Size() int64 { return 0 }

func (i resultDirInfo) Mode() fs.FileMode { return fs.ModeDir | 0o555 }

func (i resultDirInfo) ModTime() time.Time { return time.Time{} }

func (i resultDirInfo) IsDir() bool { return true }

func (i resultDirInfo) Sys() any { return nil }

// resultFileInfo renames a stat from the underlying snapshot.
type resultFileInfo struct {
	fs.FileInfo
	name string
}

func (i resultFileInfo) Name() string { return i.name }

// resultDir is an opened synthesised directory: it lists and stats, and
// reading bytes from it is the error reading a directory always is.
type resultDir struct {
	fsys *resultFS
	name string
	off  int
}

var (
	_ fs.File        = (*resultDir)(nil)
	_ fs.ReadDirFile = (*resultDir)(nil)
)

func (d *resultDir) Stat() (fs.FileInfo, error) {
	return resultDirInfo{name: path.Base(d.name)}, nil
}

func (d *resultDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}

func (d *resultDir) Close() error { return nil }

func (d *resultDir) ReadDir(n int) (entries []fs.DirEntry, err error) {
	all, err := d.fsys.ReadDir(d.name)
	if err != nil {
		return
	}
	if d.off >= len(all) {
		if n <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}
	rest := all[d.off:]
	if n > 0 && n < len(rest) {
		rest = rest[:n]
	}
	d.off += len(rest)
	return rest, nil
}

// virtualPath places a row in the tree: bare when the result stands on one
// snapshot, under a "<label>@<snapshot>" segment when it spans several, so a
// path is unambiguous about which snapshot it came from.
func virtualPath(prefix, real string) (virt string) {
	real = path.Clean(strings.TrimPrefix(real, "./"))
	if real == "" || real == "/" {
		real = "."
	}
	if prefix == "" {
		return real
	}
	if real == "." {
		return prefix
	}
	return prefix + "/" + real
}

// sanitizeSegment makes a mount label usable as one path segment: the
// separator and the specials io/fs reserves are replaced, never dropped, so
// two labels that differ still differ.
func sanitizeSegment(s string) (out string) {
	out = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0:
			return '_'
		}
		return r
	}, s)
	out = strings.TrimSpace(out)
	if out == "" || out == "." || out == ".." {
		out = "_"
	}
	return
}
