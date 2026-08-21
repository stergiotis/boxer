package play

import (
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// play_pathrows_test.go covers the path contract and the interning: which
// schemas claim, what the tree makes of the rows, and that what comes out is a
// file system io/fs itself accepts — fstest.TestFS is the oracle there, because
// the browser reaches this type only through fs.ReadDir and fs.Stat.

// pathTestCol is one fixture column; exactly one of the value slices is set.
type pathTestCol struct {
	name string
	str  []string
	b    []bool
	i    []int64
	// sec is an epoch-second count, the shape a leeway DateTime('UTC') column
	// takes on the Arrow wire (uint32 rather than a timestamp).
	sec []uint32
}

func pathTestRec(t *testing.T, cols ...pathTestCol) arrow.RecordBatch {
	t.Helper()
	mem := memory.NewGoAllocator()
	fields := make([]arrow.Field, 0, len(cols))
	arrs := make([]arrow.Array, 0, len(cols))
	rows := 0
	for _, col := range cols {
		var arr arrow.Array
		switch {
		case col.str != nil:
			b := array.NewStringBuilder(mem)
			b.AppendValues(col.str, nil)
			arr, rows = b.NewArray(), len(col.str)
			b.Release()
		case col.b != nil:
			b := array.NewBooleanBuilder(mem)
			b.AppendValues(col.b, nil)
			arr, rows = b.NewArray(), len(col.b)
			b.Release()
		case col.sec != nil:
			b := array.NewUint32Builder(mem)
			b.AppendValues(col.sec, nil)
			arr, rows = b.NewArray(), len(col.sec)
			b.Release()
		default:
			b := array.NewInt64Builder(mem)
			b.AppendValues(col.i, nil)
			arr, rows = b.NewArray(), len(col.i)
			b.Release()
		}
		fields = append(fields, arrow.Field{Name: col.name, Type: arr.DataType()})
		arrs = append(arrs, arr)
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, int64(rows))
	for _, a := range arrs {
		a.Release()
	}
	t.Cleanup(rec.Release)
	return rec
}

// The contract: `path` is the claim, five columns are read by name, and
// everything else becomes a browser column in the query's own order.
func TestResolvePathRowsContract(t *testing.T) {
	k, reason := resolvePathRows(schemaWith(
		strField("path"),
		arrow.Field{Name: "is_dir", Type: arrow.FixedWidthTypes.Boolean},
		arrow.Field{Name: "size", Type: arrow.PrimitiveTypes.Int64},
		tsField("mtime"),
		strField("content_hash"),
		strField("link_target"),
		strField("ext"),
	))
	require.Empty(t, reason)
	assert.Equal(t, 0, k.pathCol)
	assert.Equal(t, 1, k.dirCol)
	assert.Equal(t, 2, k.sizeCol)
	assert.Equal(t, 3, k.mtimeCol)
	assert.Equal(t, 5, k.linkCol)
	assert.Equal(t, -1, k.symCol, "a column the result did not carry is absent, not zero")
	assert.Equal(t, []int{4, 6}, k.hostCols,
		"every unclaimed column becomes a browser column, in schema order")
}

// No `path`, no browser — and the reason names the shortest query that would
// work rather than describing the contract.
func TestResolvePathRowsRejectsWithoutPath(t *testing.T) {
	_, reason := resolvePathRows(schemaWith(strField("name"), strField("dir")))
	require.NotEmpty(t, reason)
	assert.Contains(t, reason, "`path`")

	_, reason = resolvePathRows(nil)
	assert.NotEmpty(t, reason, "no schema is the same empty state")
}

// Rows are the leaves; the directories between them are synthesised, carry no
// row, and list in name order.
func TestBuildPathFSSynthesisesDirectories(t *testing.T) {
	rec := pathTestRec(t, pathTestCol{name: "path", str: []string{
		"a/b/c.txt", "a/b/d.txt", "e.txt",
	}}, pathTestCol{name: "size", i: []int64{10, 20, 30}})
	k, reason := resolvePathRows(rec.Schema())
	require.Empty(t, reason)

	fsys := buildPathFS(rec, k)
	assert.Equal(t, int64(3), fsys.interned)
	assert.Equal(t, int64(2), fsys.dirs, "a and a/b, neither of them a row")
	assert.Equal(t, int64(3), fsys.files)

	des, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	require.Len(t, des, 2)
	assert.Equal(t, "a", des[0].Name(), "io/fs requires a sorted listing")
	assert.True(t, des[0].IsDir())
	assert.Equal(t, "e.txt", des[1].Name())

	assert.Equal(t, int64(-1), fsys.rowOf("a/b"), "a synthesised directory has no row")
	assert.Equal(t, int64(2), fsys.rowOf("e.txt"))
	assert.Equal(t, int64(0), fsys.rowOf("a/b/c.txt"))

	info, err := fs.Stat(fsys, "a/b/c.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(10), info.Size())
	assert.False(t, info.IsDir())
}

// A node with children is a directory whatever the row that named it said —
// otherwise the browser would offer to open a file that holds entries.
func TestBuildPathFSInteriorWinsOverIsDir(t *testing.T) {
	rec := pathTestRec(t,
		pathTestCol{name: "path", str: []string{"a", "a/b"}},
		pathTestCol{name: "is_dir", b: []bool{false, false}},
	)
	k, reason := resolvePathRows(rec.Schema())
	require.Empty(t, reason)

	fsys := buildPathFS(rec, k)
	info, err := fs.Stat(fsys, "a")
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, int64(0), fsys.rowOf("a"), "it is still the row that named it")
}

// The store's own root row (`.` is the commit, ADR-0198) is an entry that is a
// child of nothing: its attributes land on the root and it lists nowhere.
func TestBuildPathFSRootRowLandsOnTheRoot(t *testing.T) {
	rec := pathTestRec(t,
		pathTestCol{name: "path", str: []string{".", "a.txt"}},
		pathTestCol{name: "mtime", sec: []uint32{1_700_000_000, 1_700_000_001}},
	)
	k, reason := resolvePathRows(rec.Schema())
	require.Empty(t, reason)

	fsys := buildPathFS(rec, k)
	assert.Equal(t, int64(0), fsys.rowOf("."))
	assert.Equal(t, time.Unix(1_700_000_000, 0).UTC(), fsys.root.mtime,
		"an epoch-second column is read as one (leewayTemporal)")

	des, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	require.Len(t, des, 1)
	assert.Equal(t, "a.txt", des[0].Name())
}

// A path io/fs will not accept is counted, not clamped into the root: a
// phantom entry would read as data the query never returned.
func TestBuildPathFSSkipsUnusablePaths(t *testing.T) {
	rec := pathTestRec(t, pathTestCol{name: "path", str: []string{
		"../escape", "ok.txt", "also/../ok2.txt", "nul\x00name",
	}})
	k, reason := resolvePathRows(rec.Schema())
	require.Empty(t, reason)

	fsys := buildPathFS(rec, k)
	assert.Equal(t, int64(2), fsys.skipped)
	assert.Equal(t, int64(2), fsys.interned)
	assert.Equal(t, int64(2), fsys.rowOf("ok2.txt"), "a cleanable path is cleaned, not refused")
}

// The cap drops the tail and counts it, so the status line can say so.
func TestBuildPathFSCapsInterning(t *testing.T) {
	paths := make([]string, pathMaxRows+3)
	for i := range paths {
		paths[i] = fmt.Sprintf("f%06d.txt", i)
	}
	rec := pathTestRec(t, pathTestCol{name: "path", str: paths})
	k, reason := resolvePathRows(rec.Schema())
	require.Empty(t, reason)

	fsys := buildPathFS(rec, k)
	assert.Equal(t, int64(3), fsys.dropped)
	assert.Equal(t, int64(pathMaxRows), fsys.interned)
	assert.Equal(t, int64(-1), fsys.rowOf(paths[pathMaxRows]), "the tail is not in the tree")
}

// The browser reaches this type only through io/fs, so io/fs's own conformance
// check is the oracle: listings, Stat, Open, ReadDirFile paging and WalkDir.
func TestRowFSIsAWellBehavedFS(t *testing.T) {
	rec := pathTestRec(t,
		pathTestCol{name: "path", str: []string{"a/b/c.txt", "a/d.txt", "e.txt", "a"}},
		pathTestCol{name: "is_dir", b: []bool{false, false, false, true}},
		pathTestCol{name: "size", i: []int64{1, 2, 3, 0}},
	)
	k, reason := resolvePathRows(rec.Schema())
	require.Empty(t, reason)

	require.NoError(t, fstest.TestFS(buildPathFS(rec, k), "a", "a/b", "a/b/c.txt", "a/d.txt", "e.txt"))
}

// A file carries no bytes — the row is metadata — and a directory refuses a
// read rather than pretending to be empty.
func TestRowFSFilesHaveNoBytes(t *testing.T) {
	rec := pathTestRec(t, pathTestCol{name: "path", str: []string{"a/b.txt"}})
	k, _ := resolvePathRows(rec.Schema())
	fsys := buildPathFS(rec, k)

	body, err := fs.ReadFile(fsys, "a/b.txt")
	require.NoError(t, err)
	assert.Empty(t, body)

	f, err := fsys.Open("a")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	_, err = f.Read(make([]byte, 1))
	assert.Error(t, err, "a directory is not readable as bytes")
}

func TestCleanRowPath(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{in: "a/b", want: "a/b", ok: true},
		{in: "/a/b/", want: "a/b", ok: true},
		{in: "", want: ".", ok: true},
		{in: "/", want: ".", ok: true},
		{in: ".", want: ".", ok: true},
		{in: "./a", want: "a", ok: true},
		{in: "a//b", want: "a/b", ok: true},
		{in: "a/../b", want: "b", ok: true},
		{in: "../b", ok: false},
		{in: "a\x00b", ok: false},
		// A trailing space is a legal character in a name; trimming it would
		// browse a tree the store does not hold.
		{in: "a ", want: "a ", ok: true},
	} {
		got, ok := cleanRowPath(tc.in)
		assert.Equal(t, tc.ok, ok, "cleanRowPath(%q)", tc.in)
		if tc.ok {
			assert.Equal(t, tc.want, got, "cleanRowPath(%q)", tc.in)
		}
	}
}

// Both spellings of a truth value answer: an Arrow boolean, and the unsigned
// byte a `NodeKind = 'dir'` comparison arrives as.
func TestPathBoolCellReadsBothSpellings(t *testing.T) {
	rec := pathTestRec(t,
		pathTestCol{name: "b", b: []bool{true, false}},
		pathTestCol{name: "n", i: []int64{1, 0}},
	)
	for _, col := range []int{0, 1} {
		v, ok := pathBoolCell(rec.Column(col), 0)
		assert.True(t, ok)
		assert.True(t, v)
		v, ok = pathBoolCell(rec.Column(col), 1)
		assert.True(t, ok)
		assert.False(t, v)
	}
	_, ok := pathBoolCell(rec.Column(0), 99)
	assert.False(t, ok, "out of range answers nothing")
}
