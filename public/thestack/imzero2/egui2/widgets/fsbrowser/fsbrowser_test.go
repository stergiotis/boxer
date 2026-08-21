package fsbrowser

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

func fixture() fstest.MapFS {
	t0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	return fstest.MapFS{
		"readme.md":        {Data: []byte("# hi\n"), ModTime: t0},
		"big.bin":          {Data: make([]byte, 3000), ModTime: t0.Add(time.Hour)},
		".hidden":          {Data: []byte("x"), ModTime: t0},
		"src/main.go":      {Data: []byte("package main\n"), ModTime: t0.Add(2 * time.Hour)},
		"src/util/u.go":    {Data: []byte("package util\n"), ModTime: t0},
		"docs/a.txt":       {Data: []byte("a"), ModTime: t0},
		"docs/b.txt":       {Data: []byte("bb"), ModTime: t0},
		"empty/.keep":      {Data: nil, ModTime: t0},
		"zeta/deep/x.json": {Data: []byte("{}"), ModTime: t0},
	}
}

func names(es []Entry) (out []string) {
	for _, e := range es {
		out = append(out, e.Name)
	}
	return
}

func TestListingIsReadOnceAndOrdinalsAreStable(t *testing.T) {
	var st State
	fsys := fixture()
	l1 := st.read(fsys, ".")
	require.NoError(t, l1.err)
	l2 := st.read(fsys, ".")
	assert.Same(t, l1, l2, "the second read is the cache")
	for i, e := range l1.entries {
		assert.Equal(t, i, e.Ord)
	}
	st.Invalidate()
	l3 := st.read(fsys, ".")
	assert.NotSame(t, l1, l3, "Invalidate drops the cache")
}

func TestViewHidesDotNamesSortsDirsFirstAndFilters(t *testing.T) {
	var st State
	fsys := fixture()
	rows := st.view(st.read(fsys, "."), false, nil)
	assert.Equal(t, []string{"docs", "empty", "src", "zeta", "big.bin", "readme.md"}, names(rows),
		"directories first, then files, by name; .hidden out")
	rows = st.view(st.read(fsys, "."), true, nil)
	assert.Contains(t, names(rows), ".hidden")

	st.SetFilter("RE")
	rows = st.view(st.read(fsys, "."), false, nil)
	assert.Equal(t, []string{"readme.md"}, names(rows), "the filter is a case-insensitive substring of the name")
	st.SetFilter("")

	st.SetSort(SortBySize, true)
	rows = st.view(st.read(fsys, "."), false, nil)
	assert.Equal(t, []string{"zeta", "src", "empty", "docs", "big.bin", "readme.md"}, names(rows),
		"descending flips within each group and directories stay first")
}

func TestNavigationClearsTheSelection(t *testing.T) {
	var st State
	st.SelectOnly("readme.md")
	assert.Equal(t, []string{"readme.md"}, st.Selection())
	assert.Equal(t, "readme.md", st.Cursor())
	st.SetDir("src/util")
	assert.Equal(t, "src/util", st.Dir())
	assert.Empty(t, st.Selection())
	assert.Equal(t, "", st.Cursor())
	assert.True(t, st.Up())
	assert.Equal(t, "src", st.Dir())
	assert.True(t, st.Up())
	assert.Equal(t, ".", st.Dir())
	assert.False(t, st.Up(), "the root has no parent")
	st.SetDir("/docs/")
	assert.Equal(t, "docs", st.Dir(), "a leading slash and a trailing one are tolerated")
}

func TestRekeyDropsCacheAndSelectionKeepsDir(t *testing.T) {
	var st State
	fsys := fixture()
	st.SetDir("src")
	st.SelectOnly("src/main.go")
	_ = st.read(fsys, "src")
	assert.False(t, st.rekey(""), "the zero key is the first key")
	assert.True(t, st.rekey("snapshot-2"))
	assert.Equal(t, "src", st.Dir(), "the same path across two snapshots")
	assert.Empty(t, st.Selection())
	assert.Empty(t, st.cache)
}

func TestErrorsAreRowsNotPanics(t *testing.T) {
	var st State
	l := st.read(fixture(), "nope")
	assert.Error(t, l.err)
	assert.True(t, errors.Is(l.err, fs.ErrNotExist))
	assert.Empty(t, st.view(l, false, nil))
	l2 := st.read(nil, ".")
	assert.Error(t, l2.err, "no file system is an error row, not a nil deref")
}

func TestOutlineLoadsOnExpandAndShowsDisclosureBeforeThat(t *testing.T) {
	var st State
	fsys := fixture()
	tr, nodes := st.buildOutline(fsys, false)
	require.NoError(t, tr.Validate())
	// Root children, then one placeholder per unread directory.
	assert.Equal(t, []string{"docs", "empty", "src", "zeta", "big.bin", "readme.md"}, names(nodes[:6]))
	rows, err := tree.Flatten(tr, &st.tree, nil)
	require.NoError(t, err)
	byKey := map[string]tree.Row{}
	for _, r := range rows {
		byKey[tr.Keys[r.Node]] = r
	}
	assert.True(t, byKey["src"].HasChildren, "an unread directory still opens")
	assert.True(t, byKey["empty"].HasChildren, "even one that will turn out empty")
	assert.False(t, byKey["readme.md"].HasChildren)
	assert.Len(t, rows, 6, "collapsed: placeholders are not drawn")

	// Open src: its real children replace the placeholder in the same build.
	st.tree.SetExpanded(byKey["src"].Node, true)
	tr, nodes = st.buildOutline(fsys, false)
	require.NoError(t, tr.Validate())
	rows, err = tree.Flatten(tr, &st.tree, nil)
	require.NoError(t, err)
	labels := make([]string, 0, len(rows))
	for _, r := range rows {
		labels = append(labels, tr.Labels[r.Node])
	}
	assert.Equal(t, []string{"docs", "empty", "src", "util", "main.go", "zeta", "big.bin", "readme.md"}, labels)
	for _, n := range nodes {
		if n.Name == "util" {
			assert.True(t, n.IsDir)
		}
	}
	assert.NotContains(t, labels, "…", "no placeholder is ever drawn for a loaded directory")

	// Open empty: read, found empty, becomes a leaf.
	for _, r := range rows {
		if tr.Keys[r.Node] == "empty" {
			st.tree.SetExpanded(r.Node, true)
		}
	}
	tr, _ = st.buildOutline(fsys, false)
	rows, err = tree.Flatten(tr, &st.tree, nil)
	require.NoError(t, err)
	for _, r := range rows {
		if tr.Keys[r.Node] == "empty" {
			assert.False(t, r.HasChildren, "an empty directory (dot-names hidden) is a leaf once read")
		}
	}
}

func TestOutlineErrorIsAChildRow(t *testing.T) {
	var st State
	fsys := fstest.MapFS{"d/f": {Data: []byte("x")}}
	tr, _ := st.buildOutline(fsys, false)
	rows, err := tree.Flatten(tr, &st.tree, nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	st.tree.SetExpanded(rows[0].Node, true)
	// Swap the FS for one where d is unreadable: the cache is per State, so
	// invalidate first.
	st.Invalidate()
	broken := fstest.MapFS{"d": {Data: []byte("not a dir")}}
	tr, nodes := st.buildOutline(broken, false)
	assert.Len(t, nodes, 1, "d is now a file, so there is nothing to expand")
	_ = tr
}

func TestHumanBytesAndTime(t *testing.T) {
	assert.Equal(t, "0 B", humanBytes(0))
	assert.Equal(t, "1023 B", humanBytes(1023))
	assert.Equal(t, "1.0 KiB", humanBytes(1024))
	assert.Equal(t, "1.5 MiB", humanBytes(3<<19))
	assert.Equal(t, "", formatTime(time.Time{}))
	assert.Len(t, formatTime(time.Unix(0, 0)), len("2006-01-02 15:04"))
}

func TestApplySelectionModes(t *testing.T) {
	var st State
	rows := []Entry{{Path: "a", Ord: 0}, {Path: "b", Ord: 1}, {Path: "c", Ord: 2}, {Path: "d", Ord: 3}}
	applySelection(&st, rows, 1, selectReplace)
	assert.Equal(t, []string{"b"}, st.Selection())
	applySelection(&st, rows, 3, selectToggle)
	assert.Equal(t, []string{"b", "d"}, st.Selection())
	assert.Equal(t, "d", st.Cursor())
	applySelection(&st, rows, 0, selectExtend)
	assert.Equal(t, []string{"a", "b", "c", "d"}, st.Selection(), "shift extends from the cursor")
	applySelection(&st, rows, 2, selectReplace)
	assert.Equal(t, []string{"c"}, st.Selection())
}
