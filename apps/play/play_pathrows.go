package play

import (
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/hmi/gloss"
)

// play_pathrows.go is the PATH contract and the read-only file system a result
// is interned into (ADR-0200 Update 2026-08-21). It stands to the Files tab as
// play_hierarchy.go stands to Icicle and Treemap: a column contract lives beside
// the panels that read it rather than inside one of them.
//
// ONE REQUIRED COLUMN. `path` — an io/fs-style '/'-separated path — is the whole
// claim (§SD1's named-columns doctrine: a column called `path` is a statement of
// intent, and nothing else in a result is). Five more are read by name when they
// are there: `is_dir`, `size`, `mtime`, `link_target` and `is_symlink`. Every
// REMAINING column becomes a browser column beside name, size and modified,
// formatted through the same gloss-aware formatter the Table grid uses
// (ADR-0186). That is what makes `SELECT * FROM fs('<mount>')` a browser rather
// than a grid: the store's projection already carries hash, text guarantee,
// content policy, error and expiry, and they arrive as columns without this
// file naming any of them.
//
// A RESULT IS NOT A FILE SYSTEM, and the interning says only what it can. Rows
// are the leaves; the directories between them are SYNTHESISED, and a
// synthesised directory carries no size, no time and no row, because the query
// made no claim about one. A row that other rows nest under is a directory
// whatever its `is_dir` said — a node with children cannot be a file. And no
// entry carries bytes: a row is metadata, so the browser's activation reports a
// path and play's Detail tab is what shows the row behind it.

const (
	// The contract. `path` is required; the rest are optional and read by
	// name, matching the `fs()` projection's own spellings (ADR-0198 §7) so
	// the store's macro needs no aliasing to browse.
	pathPathCol    = "path"
	pathIsDirCol   = "is_dir"
	pathSizeCol    = "size"
	pathMtimeCol   = "mtime"
	pathLinkCol    = "link_target"
	pathSymlinkCol = "is_symlink"

	// pathMaxRows bounds the interning. Unlike the board's and the tree's
	// caps this one is not about what gets DRAWN — the browser reads one
	// directory at a time and the table culls to its visible range — but
	// about the one pass that builds the tree and the map it fills, both paid
	// on the render thread when a result arrives. The excess is dropped and
	// counted in the status line rather than rejected: a bounded look at a
	// big mount is a reasonable thing to want on the way to a WHERE.
	pathMaxRows = 100000
)

// pathClaim is the resolved contract: the column indices, -1 for each optional
// column the result did not carry, plus the row the selection signal points at.
type pathClaim struct {
	pathCol  int
	dirCol   int
	sizeCol  int
	mtimeCol int
	linkCol  int
	symCol   int
	// hostCols is every column the contract did not claim, in schema order.
	// They become the browser's host columns, so what a query projects is
	// what the browser shows.
	hostCols []int
	selRow   int64
}

// resolvePathRows applies the contract to a schema. Pure and schema-only: it
// runs once per registered tab per frame (the dock strip carries the verdict),
// so it asks nothing of the data.
//
// `path` carries no type requirement, for the reason kanban's `lane` and
// `title` carry none: it is read through formatCell, which is total over Arrow
// types, so a path column that arrives as a dictionary, a fixed-size binary or
// even a number still interns. The optional columns are read by type and are
// simply absent when the type does not answer.
func resolvePathRows(schema *arrow.Schema) (k pathClaim, reason string) {
	k = pathClaim{pathCol: -1, dirCol: -1, sizeCol: -1, mtimeCol: -1, linkCol: -1, symCol: -1, selRow: -1}
	if schema == nil {
		reason = pathContractHint
		return
	}
	for ci, f := range schema.Fields() {
		switch pathColumnLabel(f.Name) {
		case pathPathCol:
			k.pathCol = ci
		case pathIsDirCol:
			k.dirCol = ci
		case pathSizeCol:
			k.sizeCol = ci
		case pathMtimeCol:
			k.mtimeCol = ci
		case pathLinkCol:
			k.linkCol = ci
		case pathSymlinkCol:
			k.symCol = ci
		default:
			k.hostCols = append(k.hostCols, ci)
		}
	}
	if k.pathCol < 0 {
		reason = pathContractHint
	}
	return
}

// pathColumnLabel is the name the contract matches on. A column that declares
// a gloss carries the declaration in its NAME — `size@gloss/bytes` (ADR-0186
// §SD7) — and the contract is a question about what a column IS, not about how
// it renders, so a listing that glosses its sizes and mount ids (the lading
// book's browse chapter does both) still resolves.
//
// The '@' test is what keeps this off the hot path: acceptance runs once per
// registered tab per frame, and a name without an '@' — nearly all of them —
// costs one byte scan rather than a media-type parse.
func pathColumnLabel(name string) string {
	if !strings.ContainsRune(name, '@') {
		return name
	}
	if d, declared := gloss.Default().ParseColumn(name); declared && d.Label != "" {
		return d.Label
	}
	return name
}

// pathContractHint is the empty state, and it names the shortest query that
// satisfies the contract rather than describing it in the abstract.
const pathContractHint = "Run a query with a `path` column — `SELECT * FROM fs('<mount>')`, or any result naming one — to browse it as files."

// rowNode is one node of the interned tree. row is the result row it came from
// and -1 when the node was synthesised to hold children.
type rowNode struct {
	name     string
	fullPath string
	isDir    bool
	symlink  bool
	size     int64
	mtime    time.Time
	row      int64
	children map[string]*rowNode
	// sorted is the children in name order — io/fs requires ReadDir to be
	// sorted, and the browser keys its widget ids on the listing position, so
	// the order must also be stable for as long as the tree is.
	sorted []*rowNode
}

func (inst *rowNode) sortedChildren() []*rowNode {
	if inst.sorted != nil || len(inst.children) == 0 {
		return inst.sorted
	}
	out := make([]*rowNode, 0, len(inst.children))
	for _, kid := range inst.children {
		out = append(out, kid)
	}
	slices.SortFunc(out, func(a, b *rowNode) int { return strings.Compare(a.name, b.name) })
	inst.sorted = out
	return out
}

// rowFS is a result read as a read-only [io/fs.FS]: ReadDirFS and StatFS over
// the interned tree, and an Open that yields a file with no bytes. Nothing in
// it can change, so a browser over it never needs to re-read.
type rowFS struct {
	root   *rowNode
	byPath map[string]*rowNode

	// Counted for the status line: what was interned, what the cap dropped,
	// and what could not be read as a path at all.
	interned int64
	dropped  int64
	skipped  int64
	dirs     int64
	files    int64
}

// buildPathFS interns a result into a tree. It is the one data pass the panel
// makes; everything after it is a map lookup.
func buildPathFS(rec arrow.RecordBatch, k pathClaim) (out *rowFS) {
	out = &rowFS{root: &rowNode{name: ".", fullPath: ".", isDir: true, row: -1}}
	out.byPath = map[string]*rowNode{".": out.root}
	if rec == nil || k.pathCol < 0 {
		return
	}
	rows := rec.NumRows()
	if rows > pathMaxRows {
		out.dropped = rows - pathMaxRows
		rows = pathMaxRows
	}
	for row := range rows {
		p, ok := cleanRowPath(formatCell(rec, k.pathCol, row))
		if !ok {
			out.skipped++
			continue
		}
		e := rowEntryOf(rec, k, row)
		if p == "." {
			// The store's own root row (ADR-0198: `.` is the commit) is an
			// entry like any other, but it is not a child of anything. Its
			// attributes land on the root so the columns have a row to read,
			// and it lists nowhere.
			out.root.row, out.root.mtime, out.root.size = row, e.mtime, e.size
			out.interned++
			continue
		}
		out.intern(p, row, e)
		out.interned++
	}
	out.count(out.root)
	return
}

// rowEntry is one row's contract-carried attributes.
type rowEntry struct {
	isDir   bool
	symlink bool
	size    int64
	mtime   time.Time
}

func rowEntryOf(rec arrow.RecordBatch, k pathClaim, row int64) (e rowEntry) {
	if k.dirCol >= 0 {
		e.isDir, _ = pathBoolCell(rec.Column(k.dirCol), row)
	}
	if k.symCol >= 0 {
		e.symlink, _ = pathBoolCell(rec.Column(k.symCol), row)
	}
	// A link target is the other way of saying it, and the one `fs()` carries
	// per entry; either spelling gives the browser its symlink glyph.
	if !e.symlink && k.linkCol >= 0 {
		e.symlink = formatCell(rec, k.linkCol, row) != ""
	}
	if k.sizeCol >= 0 {
		if v, ok := numericCellValue(rec.Column(k.sizeCol), row); ok {
			e.size = int64(v)
		}
	}
	if k.mtimeCol >= 0 {
		// leewayTemporal: a lading `mtime` is a DateTime('UTC') value column,
		// which reaches Arrow as an integer second count rather than as a
		// timestamp (ADR-0163's reader has the same problem and the same fix).
		if ms, ok := temporalCellMS(rec.Column(k.mtimeCol), int(row), true); ok {
			e.mtime = time.UnixMilli(ms).UTC()
		}
	}
	return
}

// intern walks p into the tree, synthesising the directories on the way.
func (inst *rowFS) intern(p string, row int64, e rowEntry) {
	cur := inst.root
	rest := p
	for off := 0; ; {
		seg := rest
		leaf := true
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			seg, rest, leaf = rest[:i], rest[i+1:], false
		}
		full := p[:off+len(seg)]
		off += len(seg) + 1
		kid, seen := cur.children[seg]
		if !seen {
			kid = &rowNode{name: seg, fullPath: full, row: -1}
			if cur.children == nil {
				cur.children = make(map[string]*rowNode, 4)
			}
			cur.children[seg] = kid
			cur.sorted = nil
			inst.byPath[full] = kid
		}
		if leaf {
			// A repeated path (two snapshots of one mount in one result) keeps
			// the FIRST row: the browser shows one tree, and picking the later
			// row would make which snapshot it describes depend on the ORDER BY.
			if kid.row < 0 {
				kid.row, kid.size, kid.mtime, kid.symlink = row, e.size, e.mtime, e.symlink
			}
			kid.isDir = kid.isDir || e.isDir
			return
		}
		// An interior node holds children, so it is a directory whatever the
		// row that named it claimed.
		kid.isDir = true
		cur = kid
	}
}

func (inst *rowFS) count(n *rowNode) {
	for _, kid := range n.children {
		if kid.isDir {
			inst.dirs++
			inst.count(kid)
			continue
		}
		inst.files++
	}
}

// rowOf reports the result row an interned path came from, -1 for a synthesised
// directory or a path the tree does not hold.
func (inst *rowFS) rowOf(p string) int64 {
	if inst == nil {
		return -1
	}
	n, ok := inst.byPath[p]
	if !ok {
		return -1
	}
	return n.row
}

func (inst *rowFS) node(op string, name string) (n *rowNode, err error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	n, ok := inst.byPath[name]
	if !ok {
		return nil, &fs.PathError{Op: op, Path: name, Err: fs.ErrNotExist}
	}
	return n, nil
}

func (inst *rowFS) Open(name string) (fs.File, error) {
	n, err := inst.node("open", name)
	if err != nil {
		return nil, err
	}
	return &rowFile{node: n}, nil
}

func (inst *rowFS) Stat(name string) (fs.FileInfo, error) {
	n, err := inst.node("stat", name)
	if err != nil {
		return nil, err
	}
	return rowInfo{node: n}, nil
}

func (inst *rowFS) ReadDir(name string) (out []fs.DirEntry, err error) {
	n, err := inst.node("readdir", name)
	if err != nil {
		return nil, err
	}
	if !n.isDir {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: errors.New("not a directory")}
	}
	kids := n.sortedChildren()
	out = make([]fs.DirEntry, len(kids))
	for i, kid := range kids {
		out[i] = rowInfo{node: kid}
	}
	return out, nil
}

// rowInfo is one node as both a [io/fs.DirEntry] and a [io/fs.FileInfo].
type rowInfo struct{ node *rowNode }

func (inst rowInfo) Name() string       { return inst.node.name }
func (inst rowInfo) Size() int64        { return inst.node.size }
func (inst rowInfo) ModTime() time.Time { return inst.node.mtime }
func (inst rowInfo) IsDir() bool        { return inst.node.isDir }
func (inst rowInfo) Sys() any           { return nil }

// Mode reports the kind and read-only permissions. The result carries no mode
// bits the browser reads — `fs()` has a `mode` column, but it rides as a host
// column where it is a datum rather than a permission this file system claims
// to enforce — so what is encoded here is the KIND and nothing else.
func (inst rowInfo) Mode() (m fs.FileMode) {
	switch {
	case inst.node.symlink:
		m = fs.ModeSymlink | 0o444
	case inst.node.isDir:
		m = fs.ModeDir | 0o555
	default:
		m = 0o444
	}
	return
}

func (inst rowInfo) Type() fs.FileMode          { return inst.Mode().Type() }
func (inst rowInfo) Info() (fs.FileInfo, error) { return inst, nil }

// rowFile is an open node. A file has no bytes — the row is metadata — and a
// directory reads its children, so [io/fs.WalkDir] works over the tree even
// though nothing but the browser walks it today.
type rowFile struct {
	node *rowNode
	off  int
}

func (inst *rowFile) Stat() (fs.FileInfo, error) { return rowInfo{node: inst.node}, nil }
func (inst *rowFile) Close() error               { return nil }

func (inst *rowFile) Read(p []byte) (int, error) {
	if inst.node.isDir {
		return 0, &fs.PathError{Op: "read", Path: inst.node.fullPath, Err: errors.New("is a directory")}
	}
	return 0, io.EOF
}

func (inst *rowFile) ReadDir(n int) (out []fs.DirEntry, err error) {
	if !inst.node.isDir {
		return nil, &fs.PathError{Op: "readdir", Path: inst.node.fullPath, Err: errors.New("not a directory")}
	}
	kids := inst.node.sortedChildren()
	if inst.off >= len(kids) {
		if n <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}
	end := len(kids)
	if n > 0 && inst.off+n < end {
		end = inst.off + n
	}
	out = make([]fs.DirEntry, 0, end-inst.off)
	for _, kid := range kids[inst.off:end] {
		out = append(out, rowInfo{node: kid})
	}
	inst.off = end
	return out, nil
}

// cleanRowPath maps a cell to an io/fs path: leading and trailing slashes go
// and the rest is cleaned. Whitespace is NOT trimmed — a trailing space is a
// legal character in a name, and trimming it would browse a tree the store does
// not hold. A cell that cleans to something io/fs will not accept (a `..` that
// escapes, a NUL) is refused rather than clamped into the root, so a malformed
// path is counted as skipped instead of appearing as a phantom entry.
func cleanRowPath(s string) (p string, ok bool) {
	p = strings.Trim(s, "/")
	if p == "" {
		return ".", true
	}
	p = path.Clean(p)
	if p == "." {
		return ".", true
	}
	if !fs.ValidPath(p) || strings.ContainsRune(p, 0) {
		return "", false
	}
	return p, true
}

// pathBoolCell reads a truth value. ClickHouse `Bool` arrives as an Arrow
// boolean and a comparison like `NodeKind = 'dir'` as an unsigned byte, so both
// spellings of the same question have to answer.
func pathBoolCell(arr arrow.Array, row int64) (v bool, ok bool) {
	if arr == nil || row < 0 || int(row) >= arr.Len() || arr.IsNull(int(row)) {
		return false, false
	}
	if b, isBool := arr.(*array.Boolean); isBool {
		return b.Value(int(row)), true
	}
	if d, isDict := arr.(*array.Dictionary); isDict {
		return pathBoolCell(d.Dictionary(), int64(d.GetValueIndex(int(row))))
	}
	n, numeric := numericCellValue(arr, row)
	if !numeric {
		return false, false
	}
	return n != 0, true
}
