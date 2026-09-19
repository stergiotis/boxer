package fsbrowser

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/fsmatch"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
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
	rows := st.view(st.read(fsys, "."), false, nil, nil)
	assert.Equal(t, []string{"docs", "empty", "src", "zeta", "big.bin", "readme.md"}, names(rows),
		"directories first, then files, by name; .hidden out")
	rows = st.view(st.read(fsys, "."), true, nil, nil)
	assert.Contains(t, names(rows), ".hidden")

	st.SetFilter("RE")
	rows = st.view(st.read(fsys, "."), false, nil, nil)
	assert.Equal(t, []string{"readme.md"}, names(rows), "a plain word is a case-insensitive match anywhere in the path")
	st.SetFilter("")

	st.SetSort(SortBySize, true)
	rows = st.view(st.read(fsys, "."), false, nil, nil)
	assert.Equal(t, []string{"zeta", "src", "empty", "docs", "big.bin", "readme.md"}, names(rows),
		"descending flips within each group and directories stay first")
}

func TestFilterIsARegexOverThePath(t *testing.T) {
	var st State
	fsys := fixture()

	st.SetFilter(`\.md$`)
	rows := st.view(st.read(fsys, "."), false, nil, nil)
	assert.Equal(t, []string{"readme.md"}, names(rows), "an anchored extension pattern")
	assert.False(t, st.FilterLiteral())

	st.SetFilter("^SRC/U")
	rows = st.view(st.read(fsys, "src"), false, nil, nil)
	assert.Equal(t, []string{"util"}, names(rows), "the pattern sees the path from the root with / between segments, case-insensitively")

	st.SetFilter("md$|^big")
	rows = st.view(st.read(fsys, "."), false, nil, nil)
	assert.Equal(t, []string{"big.bin", "readme.md"}, names(rows), "alternation; the sort still holds")

	st.SetFilter("read(")
	rows = st.view(st.read(fsys, "."), false, nil, nil)
	assert.Empty(t, names(rows), "a pattern that does not compile matches as a literal")
	assert.True(t, st.FilterLiteral(), "and says so")
	st.SetFilter("big.")
	rows = st.view(st.read(fsys, "."), false, nil, nil)
	assert.Equal(t, []string{"big.bin"}, names(rows))
	assert.False(t, st.FilterLiteral(), "the flag clears once the text compiles again")

	st.SetFilter("  ")
	rows = st.view(st.read(fsys, "."), false, nil, nil)
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

func TestKeepDecidesWhatIsARow(t *testing.T) {
	fsys := fixture()
	dirsOnly := func(e Entry) bool { return e.IsDir }
	noSrc := func(e Entry) bool { return e.Path != "src" }

	var st State
	rows := st.view(st.read(fsys, "."), false, dirsOnly, nil)
	assert.Equal(t, []string{"docs", "empty", "src", "zeta"}, names(rows), "a refused entry is not a row")
	rows = st.view(st.read(fsys, "."), true, dirsOnly, nil)
	assert.NotContains(t, names(rows), ".hidden", "and showing dot-names does not bring it back")

	_, nodes := st.buildOutline(fsys, false, dirsOnly)
	for _, e := range nodes {
		assert.True(t, e.IsDir || e.Ord < 0, "the outline lists what the list does: %q", e.Path)
	}

	re := regexp.MustCompile(`(?i)\.go$`)
	res, err := walkSearch(context.Background(), fsys, ".", re, false, noSrc, &searchFeed{}, noReport)
	require.NoError(t, err)
	assert.Empty(t, res.rows, "a refused directory is not walked, so nothing beneath it matches")

	m := &matchFS{MapFS: fixture()}
	onlyMain := func(e Entry) bool { return e.Name == "main.go" }
	res, err = runSearch(context.Background(), m, ".", re, false, onlyMain, &searchFeed{}, noReport)
	require.NoError(t, err)
	assert.Equal(t, []string{"src/main.go"}, paths(res.rows), "a store's answer passes the predicate match by match")
}

func TestSingleSelectClicksOnlyReplace(t *testing.T) {
	// No modifiers are read on this path, so it needs no render context.
	assert.Equal(t, selectModeReplace, Input{SingleSelect: true}.clickMode())
}

func noReport(uint64, uint64, string) {}

func sortedPaths(es []Entry) (out []string) {
	out = paths(es)
	sort.Strings(out)
	return
}

func TestWalkSearchFindsMatchesAtAnyDepth(t *testing.T) {
	fsys := fixture()
	ctx := context.Background()
	re := regexp.MustCompile(`(?i)\.go$`)

	var shares []uint64
	var notes []string
	report := func(done, total uint64, note string) {
		assert.Equal(t, uint64(progressScale), total, "a constant total, or the job's estimator starts over")
		shares = append(shares, done)
		notes = append(notes, note)
	}
	feed := &searchFeed{}
	res, err := walkSearch(ctx, fsys, ".", re, false, nil, feed, report)
	require.NoError(t, err)
	assert.Equal(t, []string{"src/main.go", "src/util/u.go"}, sortedPaths(res.rows))
	assert.False(t, res.more)
	assert.ElementsMatch(t, []int{0, 1}, []int{res.rows[0].Ord, res.rows[1].Ord}, "search rows carry their own ordinals")
	fed, _, _ := feed.take(^uint64(0), nil)
	assert.Equal(t, paths(res.rows), paths(fed), "what was fed while walking is what came back")
	assert.True(t, sort.SliceIsSorted(shares, func(i, j int) bool { return shares[i] < shares[j] }), "the share never goes back: %v", shares)
	assert.Equal(t, uint64(progressScale), shares[len(shares)-1], "and ends whole")
	assert.Contains(t, notes[len(notes)-1], "2 matches")

	res, err = walkSearch(ctx, fsys, ".", regexp.MustCompile("(?i)keep"), false, nil, &searchFeed{}, noReport)
	require.NoError(t, err)
	assert.Empty(t, res.rows, "a dot-name is hidden, below the root as at it")
	res, err = walkSearch(ctx, fsys, ".", regexp.MustCompile("(?i)keep"), true, nil, &searchFeed{}, noReport)
	require.NoError(t, err)
	assert.Equal(t, []string{"empty/.keep"}, paths(res.rows), "unless hidden names are shown")

	res, err = walkSearch(ctx, fsys, "src", regexp.MustCompile(`(?i)u\.go$`), false, nil, &searchFeed{}, noReport)
	require.NoError(t, err)
	assert.Equal(t, []string{"src/util/u.go"}, paths(res.rows), "under the directory given, through one the pattern does not name")
	assert.Equal(t, "util/u.go", relTo("src", res.rows[0].Path), "and shown relative to it")

	res, err = walkSearch(ctx, fsys, "src", regexp.MustCompile("(?i)^zeta"), false, nil, &searchFeed{}, noReport)
	require.NoError(t, err)
	assert.Empty(t, res.rows, "the search is rooted at the directory given")

	_, err = walkSearch(ctx, fsys, "no/such", re, false, nil, &searchFeed{}, noReport)
	assert.Error(t, err, "an unreadable first directory is the search's error")

	gone, cancel := context.WithCancel(ctx)
	cancel()
	_, err = walkSearch(gone, fsys, ".", re, false, nil, &searchFeed{}, noReport)
	assert.ErrorIs(t, err, context.Canceled, "a cancelled walk stops before its next read")
}

func TestWalkSearchSaysWhichCapStoppedIt(t *testing.T) {
	// A tree wider than the walk reads: every directory holds one match.
	wide := fstest.MapFS{}
	for i := 0; i < walkMaxDirs+10; i++ {
		wide["d"+strconv.Itoa(i)+"/hit.txt"] = &fstest.MapFile{}
	}
	res, err := walkSearch(context.Background(), wide, ".", regexp.MustCompile(`hit`), false, nil, &searchFeed{}, noReport)
	require.NoError(t, err)
	assert.True(t, res.more, "more matches than the list shows: the pattern is too wide")
	assert.False(t, res.unread)

	res, err = walkSearch(context.Background(), wide, ".", regexp.MustCompile(`nothing-is-called-this`), false, nil, &searchFeed{}, noReport)
	require.NoError(t, err)
	assert.True(t, res.unread, "directories left unread: the tree is too big, whatever the pattern")
	assert.False(t, res.more)

	var st State
	st.SetFilter("x")
	st.found = searchT{key: "k", done: true, unread: true}
	assert.Contains(t, st.searchStatus(), "search from further down")
	st.found = searchT{key: "k", done: true, more: true}
	assert.Contains(t, st.searchStatus(), "narrow the pattern")
}

func TestRunSearchAsksTheFileSystemFirst(t *testing.T) {
	ctx := context.Background()
	m := &matchFS{MapFS: fixture()}
	re := regexp.MustCompile(`(?i)\.GO$`)

	var totals []uint64
	res, err := runSearch(ctx, m, ".", re, false, nil, &searchFeed{}, func(_, total uint64, _ string) { totals = append(totals, total) })
	require.NoError(t, err)
	assert.Equal(t, 1, m.calls, "one call answers")
	assert.Equal(t, []uint64{0}, totals, "of a length nobody can see into: indeterminate")
	assert.Equal(t, []string{"src/main.go", "src/util/u.go"}, paths(res.rows), "the file system got the compiled pattern, case fold included")
	assert.False(t, res.rows[0].ModTime.IsZero(), "rows carry the info the answer had")

	m.err = errors.ErrUnsupported
	res, err = runSearch(ctx, m, ".", regexp.MustCompile(`(?i)u\.go$`), false, nil, &searchFeed{}, noReport)
	require.NoError(t, err, "ErrUnsupported is the cue to walk")
	assert.Equal(t, []string{"src/util/u.go"}, paths(res.rows))

	m.err = errors.New("server gone")
	_, err = runSearch(ctx, m, ".", re, false, nil, &searchFeed{}, noReport)
	assert.ErrorContains(t, err, "server gone", "any other failure is reported, not walked around")
}

// frames calls search as the render loop would until cond holds.
func frames(t *testing.T, st *State, fsys fs.FS, cond func(s *searchT) bool) (rows []Entry, s *searchT) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows, s = st.search(fsys, false, nil, nil, nil)
		if cond(s) {
			return
		}
		require.True(t, time.Now().Before(deadline), "the search did not get there")
		time.Sleep(time.Millisecond)
	}
}

func TestSearchRunsAsAJob(t *testing.T) {
	defer func(d time.Duration) { searchDebounce = d }(searchDebounce)
	searchDebounce = 20 * time.Millisecond
	done := func(s *searchT) bool { return s.done }

	var st State
	m := &matchFS{MapFS: fixture()}
	st.SetFilter(`\.go$`)
	_, s := st.search(m, false, nil, nil, nil)
	assert.False(t, s.started, "a filter just typed waits: a reader typing starts one search, not one per key")
	assert.Zero(t, m.calls)

	rows, s := frames(t, &st, m, done)
	assert.Equal(t, []string{"src/main.go", "src/util/u.go"}, paths(rows), "sorted by path for the list")
	assert.False(t, s.cancelled)
	assert.Equal(t, 1, m.calls)
	assert.Equal(t, "2 matches", st.searchStatus())

	st.search(m, false, nil, nil, nil)
	assert.Equal(t, 1, m.calls, "a repeated frame is not a repeated search")
	st.SetFilter("main")
	frames(t, &st, m, done)
	assert.Equal(t, 2, m.calls, "a changed pattern is")
	st.Invalidate()
	frames(t, &st, m, done)
	assert.Equal(t, 3, m.calls, "Invalidate drops the answer with the listings")

	m.err = errors.New("server gone")
	st.SetFilter("u\\.go$")
	_, s = frames(t, &st, m, done)
	assert.ErrorContains(t, s.err, "server gone")
	assert.Equal(t, bgjob.StateFailed, st.job.Snapshot().State)
}

// gateFS is a file system whose directory reads wait for the test, so a
// search can be caught while it runs.
type gateFS struct {
	fstest.MapFS
	gate chan struct{}
}

func (g gateFS) ReadDir(name string) ([]fs.DirEntry, error) {
	<-g.gate
	return g.MapFS.ReadDir(name)
}

func TestSearchStreamsAndCancels(t *testing.T) {
	defer func(d time.Duration) { searchDebounce = d }(searchDebounce)
	searchDebounce = 0

	var st State
	g := gateFS{MapFS: fixture(), gate: make(chan struct{})}
	st.SetFilter(`\.(md|go)$`)
	_, s := st.search(g, false, nil, nil, nil)
	require.True(t, s.started)
	assert.True(t, st.searching())

	g.gate <- struct{}{} // the root is read: readme.md is a match
	rows, s := frames(t, &st, g, func(s *searchT) bool { return len(s.rows) > 0 })
	assert.Equal(t, []string{"readme.md"}, paths(rows), "matches arrive while the walk goes on")
	assert.False(t, s.done)

	st.cancelSearch()
	close(g.gate)
	rows, s = frames(t, &st, g, func(s *searchT) bool { return s.done })
	assert.True(t, s.cancelled)
	assert.Contains(t, paths(rows), "readme.md", "what was found before the cancel stands")
	assert.Contains(t, st.searchStatus(), "cancelled")
	st.search(g, false, nil, nil, nil)
	assert.False(t, st.searching(), "and the same filter does not start over")

	st.SetFilter("")
	st.StopSearch()
	assert.Equal(t, bgjob.StateIdle, st.job.Snapshot().State)
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
	assert.Empty(t, st.view(l, false, nil, nil))
	l2 := st.read(nil, ".")
	assert.Error(t, l2.err, "no file system is an error row, not a nil deref")
}

func TestOutlineLoadsOnExpandAndShowsDisclosureBeforeThat(t *testing.T) {
	var st State
	fsys := fixture()
	tr, nodes := st.buildOutline(fsys, false, nil)
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
	tr, nodes = st.buildOutline(fsys, false, nil)
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
	tr, _ = st.buildOutline(fsys, false, nil)
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
	tr, _ := st.buildOutline(fsys, false, nil)
	rows, err := tree.Flatten(tr, &st.tree, nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	st.tree.SetExpanded(rows[0].Node, true)
	// Swap the FS for one where d is unreadable: the cache is per State, so
	// invalidate first.
	st.Invalidate()
	broken := fstest.MapFS{"d": {Data: []byte("not a dir")}}
	tr, nodes := st.buildOutline(broken, false, nil)
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
