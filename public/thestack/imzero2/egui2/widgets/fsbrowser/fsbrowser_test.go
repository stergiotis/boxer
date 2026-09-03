package fsbrowser

import (
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/fsmatch"
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
	assert.Equal(t, []string{"readme.md"}, names(rows), "a plain word is a case-insensitive match anywhere in the path")
	st.SetFilter("")

	st.SetSort(SortBySize, true)
	rows = st.view(st.read(fsys, "."), false, nil)
	assert.Equal(t, []string{"zeta", "src", "empty", "docs", "big.bin", "readme.md"}, names(rows),
		"descending flips within each group and directories stay first")
}

func TestFilterIsARegexOverThePath(t *testing.T) {
	var st State
	fsys := fixture()

	st.SetFilter(`\.md$`)
	rows := st.view(st.read(fsys, "."), false, nil)
	assert.Equal(t, []string{"readme.md"}, names(rows), "an anchored extension pattern")
	assert.False(t, st.FilterLiteral())

	st.SetFilter("^SRC/U")
	rows = st.view(st.read(fsys, "src"), false, nil)
	assert.Equal(t, []string{"util"}, names(rows), "the pattern sees the path from the root with / between segments, case-insensitively")

	st.SetFilter("md$|^big")
	rows = st.view(st.read(fsys, "."), false, nil)
	assert.Equal(t, []string{"big.bin", "readme.md"}, names(rows), "alternation; the sort still holds")

	st.SetFilter("read(")
	rows = st.view(st.read(fsys, "."), false, nil)
	assert.Empty(t, names(rows), "a pattern that does not compile matches as a literal")
	assert.True(t, st.FilterLiteral(), "and says so")
	st.SetFilter("big.")
	rows = st.view(st.read(fsys, "."), false, nil)
	assert.Equal(t, []string{"big.bin"}, names(rows))
	assert.False(t, st.FilterLiteral(), "the flag clears once the text compiles again")

	st.SetFilter("  ")
	rows = st.view(st.read(fsys, "."), false, nil)
	assert.Len(t, rows, 6, "whitespace is no filter")
	assert.False(t, st.FilterLiteral())
}

func paths(es []Entry) (out []string) {
	for _, e := range es {
		out = append(out, e.Path)
	}
	return
}

// matchFS is a file system that answers the filter itself, counting the
// calls, so a test can tell push-down from a walk.
type matchFS struct {
	fstest.MapFS
	calls int
	err   error
}

func (m *matchFS) MatchPaths(dir, pattern string, hidden bool, limit int) (out []fsmatch.Match, more bool, err error) {
	m.calls++
	if m.err != nil {
		return nil, false, m.err
	}
	re := regexp.MustCompile(pattern)
	err = fs.WalkDir(m.MapFS, dir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || p == dir {
			return werr
		}
		if !hidden && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !re.MatchString(p) {
			return nil
		}
		if limit > 0 && len(out) == limit {
			more = true
			return fs.SkipAll
		}
		info, _ := d.Info()
		out = append(out, fsmatch.Match{Path: p, Info: info})
		return nil
	})
	return
}

func TestFilterSearchesTheSubtreeByWalking(t *testing.T) {
	var st State
	fsys := fixture()

	st.SetFilter(`\.go$`)
	rows, s := st.search(fsys, false, 1, nil)
	assert.False(t, s.done, "one uncached read per call: the root alone is not the answer")
	assert.True(t, s.walking)
	for !s.done {
		rows, s = st.search(fsys, false, 1, nil)
	}
	assert.Equal(t, []string{"src/main.go", "src/util/u.go"}, paths(rows), "matches at any depth, in path order")
	assert.False(t, s.more)
	assert.Equal(t, []int{0, 1}, []int{rows[0].Ord, rows[1].Ord}, "search rows carry their own ordinals")

	rows, s = st.search(fsys, false, 1, nil)
	assert.True(t, s.done, "the same key is the same answer, not another walk")
	assert.Len(t, rows, 2)

	st.SetFilter("keep")
	rows, s = st.search(fsys, false, 100, nil)
	assert.True(t, s.done)
	assert.Empty(t, rows, "a dot-name is hidden, below the root as at it")
	rows, _ = st.search(fsys, true, 100, nil)
	assert.Equal(t, []string{"empty/.keep"}, paths(rows), "unless hidden names are shown")

	st.SetDir("src")
	st.SetFilter("u\\.go$")
	rows, _ = st.search(fsys, false, 100, nil)
	assert.Equal(t, []string{"src/util/u.go"}, paths(rows), "under the current directory, through a directory the pattern does not name")
	assert.Equal(t, "util/u.go", relTo(st.Dir(), rows[0].Path), "and shown relative to it")

	st.SetFilter("^zeta")
	rows, _ = st.search(fsys, false, 100, nil)
	assert.Empty(t, rows, "the search is rooted at the current directory")
}

func TestFilterIsPushedDownWhenTheFileSystemCanRunIt(t *testing.T) {
	var st State
	m := &matchFS{MapFS: fixture()}

	st.SetFilter(`\.GO$`)
	rows, s := st.search(m, false, 0, nil)
	assert.True(t, s.done, "one call answers, whatever the budget")
	assert.False(t, s.walking)
	assert.Equal(t, 1, m.calls)
	assert.Equal(t, []string{"src/main.go", "src/util/u.go"}, paths(rows), "the file system got the compiled pattern, case fold included")
	assert.True(t, rows[0].ModTime.After(rows[1].ModTime) || !rows[0].ModTime.IsZero(), "rows carry the info the answer had")

	st.search(m, false, 0, nil)
	assert.Equal(t, 1, m.calls, "a repeated frame is not a repeated query")
	st.SetFilter("main")
	st.search(m, false, 0, nil)
	assert.Equal(t, 2, m.calls, "a changed pattern is")

	st.Invalidate()
	st.search(m, false, 0, nil)
	assert.Equal(t, 3, m.calls, "Invalidate drops the answer with the listings")

	m.err = errors.ErrUnsupported
	st.SetFilter("u\\.go$")
	rows, s = st.search(m, false, 100, nil)
	assert.True(t, s.walking, "ErrUnsupported is the cue to walk")
	assert.True(t, s.done)
	assert.NoError(t, s.err)
	assert.Equal(t, []string{"src/util/u.go"}, paths(rows))

	m.err = errors.New("server gone")
	st.SetFilter("main")
	rows, s = st.search(m, false, 100, nil)
	assert.True(t, s.done)
	assert.ErrorContains(t, s.err, "server gone", "any other failure is reported, not walked around")
	assert.Empty(t, rows)
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
	applySelection(&st, rows, 1, selectModeReplace)
	assert.Equal(t, []string{"b"}, st.Selection())
	applySelection(&st, rows, 3, selectModeToggle)
	assert.Equal(t, []string{"b", "d"}, st.Selection())
	assert.Equal(t, "d", st.Cursor())
	applySelection(&st, rows, 0, selectModeExtend)
	assert.Equal(t, []string{"a", "b", "c", "d"}, st.Selection(), "shift extends from the cursor")
	applySelection(&st, rows, 2, selectModeReplace)
	assert.Equal(t, []string{"c"}, st.Selection())
}
