package tally

import (
	"io"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identifier"
)

// The Results pane's tree (ADR-0222 §SD4). The leaves are what a query
// returned; the directories above them are synthesised, because a query
// returns files and a browser needs somewhere to put them.

func testLocation(n uint64) (loc location) {
	return location{mount: identifier.TaggedId(0xF5F5000000000000 + n), snap: time.Unix(0, int64(n)).UTC()}
}

// oneSnapshot is the tree the leaves point into.
func oneSnapshot() (fsys fstest.MapFS) {
	return fstest.MapFS{
		"README.md":       &fstest.MapFile{Data: []byte("# readme\n")},
		"src/main.go":     &fstest.MapFile{Data: []byte("package main\n")},
		"src/deep/a.txt":  &fstest.MapFile{Data: []byte("aaa")},
		"unlisted/b.json": &fstest.MapFile{Data: []byte("{}")},
	}
}

func buildResultFS(t *testing.T, leaves map[string]resultLeaf, views map[location]fs.FS) *resultFS {
	t.Helper()
	return buildResultFSWithDirs(t, leaves, nil, views)
}

func buildResultFSWithDirs(t *testing.T, leaves map[string]resultLeaf, dirs map[string]struct{}, views map[location]fs.FS) *resultFS {
	t.Helper()
	return newResultFS(func(loc location) (fs.FS, error) {
		v, ok := views[loc]
		require.True(t, ok, "no view for location %v", loc)
		return v, nil
	}, leaves, dirs)
}

func TestResultFSSynthesisesTheDirectoriesAboveItsLeaves(t *testing.T) {
	loc := testLocation(1)
	rfs := buildResultFS(t, map[string]resultLeaf{
		"README.md":      {loc: loc, real: "README.md"},
		"src/main.go":    {loc: loc, real: "src/main.go"},
		"src/deep/a.txt": {loc: loc, real: "src/deep/a.txt"},
	}, map[location]fs.FS{loc: oneSnapshot()})

	root, err := rfs.ReadDir(".")
	require.NoError(t, err)
	require.Len(t, root, 2)
	// Sorted by name, so the file comes before the directory here — the
	// widget applies its own ordering on top.
	assert.Equal(t, "README.md", root[0].Name())
	assert.False(t, root[0].IsDir())
	assert.Equal(t, "src", root[1].Name())
	assert.True(t, root[1].IsDir())

	src, err := rfs.ReadDir("src")
	require.NoError(t, err)
	require.Len(t, src, 2)
	assert.Equal(t, "deep", src[0].Name())
	assert.True(t, src[0].IsDir())
	assert.Equal(t, "main.go", src[1].Name())

	// A path the result did not name is not in the tree, even though it is
	// in the snapshot behind it: the pane shows the query's answer.
	_, err = rfs.ReadDir("unlisted")
	assert.Error(t, err)
}

func TestResultFSOpensLeavesThroughTheirOwnSnapshot(t *testing.T) {
	a, b := testLocation(1), testLocation(2)
	other := fstest.MapFS{"src/main.go": &fstest.MapFile{Data: []byte("// the other snapshot\n")}}
	rfs := buildResultFS(t, map[string]resultLeaf{
		"one/src/main.go": {loc: a, real: "src/main.go"},
		"two/src/main.go": {loc: b, real: "src/main.go"},
	}, map[location]fs.FS{a: oneSnapshot(), b: other})

	read := func(p string) string {
		f, err := rfs.Open(p)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		data, err := io.ReadAll(f)
		require.NoError(t, err)
		return string(data)
	}
	assert.Equal(t, "package main\n", read("one/src/main.go"))
	assert.Equal(t, "// the other snapshot\n", read("two/src/main.go"))

	leaf, ok := rfs.Leaf("two/src/main.go")
	require.True(t, ok)
	assert.Equal(t, b, leaf.loc)
	assert.Equal(t, "src/main.go", leaf.real)
	_, ok = rfs.Leaf("two/src")
	assert.False(t, ok, "a directory resolves to no row")
}

func TestResultFSStatsLeavesThroughTheSnapshotAndDirectoriesFromNothing(t *testing.T) {
	loc := testLocation(1)
	rfs := buildResultFS(t, map[string]resultLeaf{
		"src/main.go": {loc: loc, real: "src/main.go"},
	}, map[location]fs.FS{loc: oneSnapshot()})

	entries, err := rfs.ReadDir("src")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	info, err := entries[0].Info()
	require.NoError(t, err)
	assert.Equal(t, "main.go", info.Name())
	assert.EqualValues(t, len("package main\n"), info.Size())

	root, err := rfs.ReadDir(".")
	require.NoError(t, err)
	require.Len(t, root, 1)
	dirInfo, err := root[0].Info()
	require.NoError(t, err)
	assert.True(t, dirInfo.IsDir())
	assert.Zero(t, dirInfo.Size(), "a synthesised directory records no size")
	assert.True(t, dirInfo.ModTime().IsZero(), "and no time; nothing wrote one")
}

// A leaf that is also an ancestor of another leaf is a directory, which is
// what it is in the snapshot too.
func TestResultFSTreatsALeafThatIsAlsoAnAncestorAsADirectory(t *testing.T) {
	loc := testLocation(1)
	rfs := buildResultFS(t, map[string]resultLeaf{
		"src":         {loc: loc, real: "src"},
		"src/main.go": {loc: loc, real: "src/main.go"},
	}, map[location]fs.FS{loc: oneSnapshot()})

	assert.True(t, rfs.isDir("src"))
	_, ok := rfs.Leaf("src")
	assert.False(t, ok)
	entries, err := rfs.ReadDir("src")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "main.go", entries[0].Name())
}

func TestResultFSOpenedDirectoryListsAndRefusesBytes(t *testing.T) {
	loc := testLocation(1)
	rfs := buildResultFS(t, map[string]resultLeaf{
		"src/main.go":    {loc: loc, real: "src/main.go"},
		"src/deep/a.txt": {loc: loc, real: "src/deep/a.txt"},
	}, map[location]fs.FS{loc: oneSnapshot()})

	f, err := rfs.Open("src")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	require.NoError(t, err)
	assert.True(t, st.IsDir())
	_, err = f.Read(make([]byte, 1))
	assert.Error(t, err, "reading bytes from a directory is an error, here as anywhere")

	rd, ok := f.(fs.ReadDirFile)
	require.True(t, ok)
	first, err := rd.ReadDir(1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	rest, err := rd.ReadDir(-1)
	require.NoError(t, err)
	require.Len(t, rest, 1)
	_, err = rd.ReadDir(1)
	assert.ErrorIs(t, err, io.EOF)
}

func TestResultFSRejectsPathsIoFsDoesNotAccept(t *testing.T) {
	rfs := buildResultFS(t, map[string]resultLeaf{}, nil)
	_, err := rfs.Open("/absolute")
	assert.ErrorIs(t, err, fs.ErrInvalid)
	_, err = rfs.ReadDir("../escape")
	assert.ErrorIs(t, err, fs.ErrInvalid)
	_, err = rfs.Open("missing.txt")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestVirtualPathAndSegmentSanitising(t *testing.T) {
	assert.Equal(t, "a/b.txt", virtualPath("", "a/b.txt"))
	assert.Equal(t, "a/b.txt", virtualPath("", "./a/b.txt"))
	assert.Equal(t, "m@t/a/b.txt", virtualPath("m@t", "a/b.txt"))
	assert.Equal(t, "m@t", virtualPath("m@t", "."))

	assert.Equal(t, "a_b", sanitizeSegment("a/b"), "a separator would split the segment in two")
	assert.Equal(t, "_", sanitizeSegment(""))
	assert.Equal(t, "_", sanitizeSegment(".."))
	assert.Equal(t, "corpus", sanitizeSegment("corpus"))
}

// A query that selected `is_dir` says which of its rows are directories, so a
// directory row is not a leaf pretending to be a file.
func TestResultFSHonoursDeclaredDirectories(t *testing.T) {
	loc := testLocation(1)
	rfs := buildResultFSWithDirs(t,
		map[string]resultLeaf{"src/main.go": {loc: loc, real: "src/main.go"}},
		map[string]struct{}{"empty/dir": {}},
		map[location]fs.FS{loc: oneSnapshot()},
	)
	assert.True(t, rfs.isDir("empty/dir"))
	assert.True(t, rfs.isDir("empty"), "a declared directory brings its ancestors")

	root, err := rfs.ReadDir(".")
	require.NoError(t, err)
	require.Len(t, root, 2)
	assert.Equal(t, "empty", root[0].Name())
	assert.True(t, root[0].IsDir())
	assert.Equal(t, "src", root[1].Name())

	// It lists, and it lists nothing — which is what an empty directory is.
	entries, err := rfs.ReadDir("empty/dir")
	require.NoError(t, err)
	assert.Empty(t, entries)
	_, ok := rfs.Leaf("empty/dir")
	assert.False(t, ok)
}
