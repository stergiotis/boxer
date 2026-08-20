// Package ladingadapter turns one snapshot of one mount into an `io/fs.FS`
// (ADR-0198 §SD8).
//
// The view is pinned: [Open] fixes the mount and the snapshot, and every query
// the adapter issues carries both. Nothing it returns can change afterwards —
// a walk running against the same mount writes a different `ts` and is
// invisible here until someone opens it. That is the property the caches below
// rest on: an immutable view needs no invalidation, so there is none.
//
// # What it costs
//
// Every miss is a query, so this is a batch and templating surface, not a hot
// serving path — millisecond latency per call, and never a write. What makes
// it affordable is the key: `(mount, snapshot, path)` makes a Stat a point
// lookup, a subtree a `startsWith` range, and a directory a bloom-filtered
// equality on the materialised `dir`.
//
// # Symlinks
//
// The store records a link, never what it points at — that is the walker's
// contract, and [FS.Lstat] and [FS.ReadLink] serve it verbatim. Reading is the
// other half: `io/fs` says an FS that implements ReadLinkFS resolves links on
// Open and Stat, so this one does, within the snapshot and with a depth limit.
// The snapshot holds the graph; the adapter interprets it the way `io/fs`
// callers expect.
package ladingadapter

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Errors a caller can match on. Everything else arrives as an *fs.PathError
// wrapping one of these or a store failure.
var (
	// ErrNoContent is what reading an entry the snapshot stats but does not
	// hold returns — a directory, a symlink, or a file under a metadata-only
	// policy. The entry lists and stats; it has no bytes.
	ErrNoContent = errors.New("lading: entry carries no stored content")
	// ErrReferenced is what reading a `ref` entry returns without a
	// [SourceFetcherI]. The file was too large for the mount's inline
	// threshold, so the snapshot kept its size, its mtime and its hash but
	// not its bytes.
	ErrReferenced = errors.New("lading: entry content is referenced, not stored; no source fetcher configured")
	// ErrTooManyLinks is a symlink cycle, or a chain deeper than the limit.
	ErrTooManyLinks = errors.New("lading: too many levels of symbolic links")
)

// maxLinkDepth bounds symlink resolution. The number is a fs convention, not a
// property of the store; a snapshot can hold a cycle because the source did.
const maxLinkDepth = 32

// SourceFetcherI serves the bytes of an entry the snapshot only references.
//
// A `ref` entry is one the mount's policy decided not to store: the snapshot
// has everything about it except its content. Where that content is still
// reachable — the live tree, an object store, whatever the mount was of — a
// fetcher hands it back, and the hash on the entry is what a caller checks it
// against.
//
// The default is no fetcher, and then reading a `ref` entry fails with
// [ErrReferenced] rather than pretending the file is empty.
type SourceFetcherI interface {
	FetchContent(ctx context.Context, mount identifier.TaggedId, snap time.Time, name string, contentHash []byte) ([]byte, error)
}

// Option configures an [FS].
type Option func(*FS)

// WithSourceFetcher serves `ref` entries from a live source.
func WithSourceFetcher(f SourceFetcherI) Option {
	return func(inst *FS) { inst.fetcher = f }
}

// WithContext binds the context the adapter's queries run under.
//
// `io/fs` has no context in its signatures, so it has to be held rather than
// passed. One per FS, fixed at Open: a cancelled context makes every later
// call fail, which is what a caller cancelling a batch wants.
func WithContext(ctx context.Context) Option {
	return func(inst *FS) { inst.ctx = ctx }
}

// FS is one snapshot of one mount as a read-only file system.
//
// It implements [fs.StatFS], [fs.ReadDirFS], [fs.ReadFileFS], [fs.GlobFS],
// [fs.ReadLinkFS] and [fs.SubFS]. It is not safe for concurrent use: the
// generated stores it reads through are single-goroutine, so an FS is one
// goroutine's handle on a snapshot.
type FS struct {
	st      lading.Stores
	mount   identifier.TaggedId
	snap    time.Time
	ctx     context.Context
	fetcher SourceFetcherI

	// prefix is non-empty for an FS returned by Sub: every name is resolved
	// under it, and every name handed back is relative to it.
	prefix string

	// entries and dirs cache what has been read. No invalidation, no TTL and
	// no size bound: the view is immutable, so a hit is always right, and the
	// working set is the caller's own access pattern.
	entries map[string]*entry
	dirs    map[string][]*entry
}

var (
	_ fs.StatFS     = (*FS)(nil)
	_ fs.ReadDirFS  = (*FS)(nil)
	_ fs.ReadFileFS = (*FS)(nil)
	_ fs.GlobFS     = (*FS)(nil)
	_ fs.ReadLinkFS = (*FS)(nil)
	_ fs.SubFS      = (*FS)(nil)
)

// entry is one decoded row, with the path it was read under.
type entry struct {
	name string
	row  ladingmeta.LadingEntry
}

// Open pins one snapshot of one mount and returns it as a file system.
//
// snap is the snapshot's instant — a value from [Snapshots] or [Latest], or
// the one a walk returned. Nothing is read here: the first query is the first
// call on the result.
func Open(st lading.Stores, mount identifier.TaggedId, snap time.Time, opts ...Option) (inst *FS, err error) {
	if st.Meta == nil {
		err = eh.Errorf("ladingadapter: no entry store")
		return
	}
	if !mount.IsValid() {
		err = eh.Errorf("ladingadapter: mount id is not a valid tagged id")
		return
	}
	inst = &FS{
		st: st, mount: mount, snap: snap.UTC(), ctx: context.Background(),
		entries: map[string]*entry{},
		dirs:    map[string][]*entry{},
	}
	for _, o := range opts {
		o(inst)
	}
	return
}

// Snap is the instant this view is pinned to.
func (inst *FS) Snap() time.Time { return inst.snap }

// Mount is the mount this view is of.
func (inst *FS) Mount() identifier.TaggedId { return inst.mount }

// --- the io/fs surface.

// Open opens a file for reading, following symlinks.
func (inst *FS) Open(name string) (fs.File, error) {
	e, err := inst.resolve(name)
	if err != nil {
		return nil, pathErr("open", name, err)
	}
	return inst.openEntry(name, e)
}

// Stat follows symlinks; [FS.Lstat] does not.
func (inst *FS) Stat(name string) (fs.FileInfo, error) {
	e, err := inst.resolve(name)
	if err != nil {
		return nil, pathErr("stat", name, err)
	}
	return infoOf(e), nil
}

// Lstat describes the named node without following it — a symlink stats as a
// symlink.
func (inst *FS) Lstat(name string) (fs.FileInfo, error) {
	e, err := inst.lookup(name)
	if err != nil {
		return nil, pathErr("lstat", name, err)
	}
	return infoOf(e), nil
}

// ReadLink returns a symlink's target exactly as the source held it —
// unresolved, and relative if it was relative.
func (inst *FS) ReadLink(name string) (string, error) {
	e, err := inst.lookup(name)
	if err != nil {
		return "", pathErr("readlink", name, err)
	}
	if !isSymlink(e) {
		return "", pathErr("readlink", name, fs.ErrInvalid)
	}
	return e.row.LinkTarget, nil
}

// ReadDir lists a directory, sorted by filename as [fs.ReadDirFS] requires.
//
// One query: `dir = <name>` over the materialised tree column, which is what
// the bloom filter on it exists for. The sort is in memory — the store's key
// orders by the whole path, and a directory's own order is a different
// question.
func (inst *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	e, err := inst.resolve(name)
	if err != nil {
		return nil, pathErr("readdir", name, err)
	}
	if !modeOf(e).IsDir() {
		return nil, pathErr("readdir", name, errors.New("not a directory"))
	}
	kids, err := inst.children(e.name)
	if err != nil {
		return nil, pathErr("readdir", name, err)
	}
	out := make([]fs.DirEntry, 0, len(kids))
	for _, k := range kids {
		out = append(out, fs.FileInfoToDirEntry(infoOf(k)))
	}
	return out, nil
}

// ReadFile returns a file's whole content.
func (inst *FS) ReadFile(name string) ([]byte, error) {
	e, err := inst.resolve(name)
	if err != nil {
		return nil, pathErr("readfile", name, err)
	}
	data, err := inst.content(e)
	if err != nil {
		return nil, pathErr("readfile", name, err)
	}
	return data, nil
}

// Glob matches against the tree, with [path.Match]'s semantics.
//
// A pattern with no meta characters is one Stat. Anything else walks, because
// matching in SQL would mean re-implementing [path.Match] in RE2 and having
// the edge cases agree — a divergence a caller would not see and a test would
// find only by luck. The SQL surface offers `match()` directly for callers who
// want a regular expression rather than a glob.
func (inst *FS) Glob(pattern string) ([]string, error) {
	if _, err := path.Match(pattern, ""); err != nil {
		return nil, err
	}
	if !strings.ContainsAny(pattern, "*?[\\") {
		if _, err := inst.Stat(pattern); err != nil {
			return nil, nil
		}
		return []string{pattern}, nil
	}
	return fs.Glob(struct{ fs.FS }{inst}, pattern)
}

// Sub returns the subtree rooted at dir as a file system of its own. It shares
// this one's caches and its pinning: a Sub of a snapshot is the same snapshot.
func (inst *FS) Sub(dir string) (fs.FS, error) {
	if !fs.ValidPath(dir) {
		return nil, pathErr("sub", dir, fs.ErrInvalid)
	}
	if dir == "." {
		return inst, nil
	}
	e, err := inst.resolve(dir)
	if err != nil {
		return nil, pathErr("sub", dir, err)
	}
	if !modeOf(e).IsDir() {
		return nil, pathErr("sub", dir, errors.New("not a directory"))
	}
	sub := *inst
	sub.prefix = e.name
	return &sub, nil
}

// --- resolution.

// full maps a name in this FS's namespace to the snapshot's.
func (inst *FS) full(name string) string {
	if inst.prefix == "" {
		return name
	}
	if name == "." {
		return inst.prefix
	}
	return inst.prefix + "/" + name
}

// lookup is one entry by name, without following symlinks.
func (inst *FS) lookup(name string) (*entry, error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	return inst.byPath(inst.full(name))
}

// resolve is [FS.lookup] with symlinks followed, the way io/fs expects of a
// ReadLinkFS. A target is interpreted relative to the link's own directory,
// and an absolute target is taken as rooted at the snapshot — a snapshot has
// no other root to offer.
func (inst *FS) resolve(name string) (*entry, error) {
	e, err := inst.lookup(name)
	if err != nil {
		return nil, err
	}
	for depth := 0; isSymlink(e); depth++ {
		if depth >= maxLinkDepth {
			return nil, ErrTooManyLinks
		}
		target := e.row.LinkTarget
		if target == "" {
			return nil, fs.ErrInvalid
		}
		var next string
		if strings.HasPrefix(target, "/") {
			next = path.Clean(strings.TrimPrefix(target, "/"))
		} else {
			next = path.Join(path.Dir(e.name), target)
		}
		if next == "" || !fs.ValidPath(next) {
			return nil, fs.ErrInvalid
		}
		e, err = inst.byPath(next)
		if err != nil {
			return nil, err
		}
	}
	return e, nil
}

// byPath reads one entry of the snapshot, by its full path.
func (inst *FS) byPath(full string) (*entry, error) {
	if e, hit := inst.entries[full]; hit {
		if e == nil {
			return nil, fs.ErrNotExist
		}
		return e, nil
	}
	rows, err := inst.scan(fmt.Sprintf("%s = %s", inst.col("naturalKey"), quote(full)), 1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		// Absence is cacheable for the same reason presence is: the view
		// cannot gain a row.
		inst.entries[full] = nil
		return nil, fs.ErrNotExist
	}
	inst.entries[full] = rows[0]
	return rows[0], nil
}

// children lists one directory, from the materialised `dir` column.
func (inst *FS) children(full string) ([]*entry, error) {
	if kids, hit := inst.dirs[full]; hit {
		return kids, nil
	}
	// The root's children carry dir = '.', every other directory's carry the
	// directory's own path — which is what the materialised column computes.
	rows, err := inst.scan(fmt.Sprintf("dir = %s", quote(full)), 0)
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	for _, r := range rows {
		if _, hit := inst.entries[r.name]; !hit {
			inst.entries[r.name] = r
		}
	}
	inst.dirs[full] = rows
	return rows, nil
}

// scan runs one query over this snapshot's entry rows.
//
// Every predicate is ANDed onto the pinning — mount and snapshot — so a
// caller of this package cannot reach another mount's rows or another
// snapshot's, whatever it asks for.
func (inst *FS) scan(pred string, limit int) (out []*entry, err error) {
	where := fmt.Sprintf("%s = %d AND %s = %s AND (%s)",
		inst.col("id"), inst.mount.Value(),
		inst.col("ts"), tsLiteral(inst.snap),
		pred)
	for ent, serr := range inst.st.Meta.ScanLadingEntry(inst.ctx, recordstore.ScanOpts{
		ExtraPredicate: where, Limit: limit,
	}) {
		if serr != nil {
			return nil, serr
		}
		if !ent.LadingEntry.Has {
			continue
		}
		out = append(out, &entry{name: string(ent.NaturalKey), row: ent.LadingEntry.Val})
	}
	return
}

// col is a backbone plain's physical column name, resolved once per call from
// the descriptor rather than written out.
func (inst *FS) col(plain string) string {
	q, err := ladingschema.PhysicalPlainName(plain)
	if err != nil {
		// Only reachable for a facts schema that has lost a backbone plain,
		// which every store in the tree would already have failed on.
		panic(err)
	}
	return q
}

// --- small helpers.

// tsLiteral renders an instant as ClickHouse expects it.
//
// fromUnixTimestamp64Nano, never toDateTime64: a plain number handed to
// toDateTime64 is read as *seconds* whatever the scale says, so a nanosecond
// value saturates to the year 2262 and the predicate matches nothing — with no
// error anywhere (verified on 26.7.3).
func tsLiteral(t time.Time) string {
	return fmt.Sprintf("fromUnixTimestamp64Nano(toInt64(%d), 'UTC')", t.UTC().UnixNano())
}

// quote renders a SQL string literal.
//
// Paths reach here from a caller and the ScanOpts predicate is spliced
// verbatim, so they are escaped rather than trusted. NUL is escaped as well as
// the obvious two, and not only for tidiness: a block's natural key carries a
// literal NUL between the path and the ordinal, and an executor that hands the
// statement to a process as an argument cannot carry a raw NUL at all — the
// exec fails with EINVAL before ClickHouse sees anything. `\0` in the literal
// travels as two ordinary characters and arrives as the byte.
func quote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\x00", `\0`).Replace(s) + "'"
}

func isSymlink(e *entry) bool { return modeOf(e)&fs.ModeSymlink != 0 }

func pathErr(op, name string, err error) error {
	return &fs.PathError{Op: op, Path: name, Err: err}
}
