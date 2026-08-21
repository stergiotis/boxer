package tally

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/tally/launchcfg"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

func TestManifestIsRegistered(t *testing.T) {
	m, ok := app.LookupManifest(ManifestId)
	require.True(t, ok, "init must register the manifest")
	assert.Equal(t, app.SurfaceWindowed, m.Surface)
	assert.NotNil(t, m.Help, "the help book ships with the app")
	assert.NotEmpty(t, m.Caps)
	for _, cap := range m.Caps {
		assert.False(t, strings.HasPrefix(cap.Pattern, "fs.handle."), "never a standing handle grant (ADR-0026 §SD7)")
	}
}

func TestClassifyByName(t *testing.T) {
	assert.Equal(t, previewKindMarkdown, classifyByName("doc/README.md"))
	assert.Equal(t, previewKindJSON, classifyByName("x.JSON"))
	assert.Equal(t, previewKindSQL, classifyByName("q.sql"))
	assert.Equal(t, previewKindGo, classifyByName("main.go"))
	assert.Equal(t, previewKindImage, classifyByName("a/b.PNG"))
	assert.Equal(t, previewKindNone, classifyByName("Makefile"), "no extension: decide from the bytes")
	assert.Equal(t, previewKindNone, classifyByName("data.bin"))
}

func TestLooksText(t *testing.T) {
	assert.True(t, looksText([]byte("hello\nworld\n")))
	assert.True(t, looksText([]byte("héllo wörld — ünïcödé")))
	assert.False(t, looksText([]byte{0x89, 'P', 'N', 'G', 0, 0}), "a NUL is the binary verdict")
	assert.False(t, looksText([]byte{0xff, 0xfe, 0xfd}), "invalid UTF-8 is binary")
	assert.True(t, looksText(nil), "empty is text, vacuously")
}

func TestHexDump(t *testing.T) {
	out := hexDump([]byte("ABCDEFGHIJKLMNOPQ\x00\x01"))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 2)
	assert.True(t, strings.HasPrefix(lines[0], "00000000  41 42 43 44 45 46 47 48  49 4a 4b 4c 4d 4e 4f 50  |ABCDEFGHIJKLMNOP|"), lines[0])
	assert.True(t, strings.HasPrefix(lines[1], "00000010  51 00 01"), lines[1])
	assert.True(t, strings.HasSuffix(lines[1], "|Q..|"), lines[1])
}

func TestInfoSQLExpands(t *testing.T) {
	mount := identifier.TaggedId(4322952322827452417)
	snap := time.Unix(0, 1755723885967744578)
	sql := infoSQL(mount, snap, "a/it's.txt")
	assert.Contains(t, sql, "fs(4322952322827452417, 1755723885967744578)")
	assert.Contains(t, sql, `WHERE path = 'a/it\'s.txt'`, "the path is a quoted literal")
	out, err := ladingsql.Expand(ladingsql.Config{Visibility: ladingsql.VisibleAll{}}, sql)
	require.NoError(t, err)
	assert.Contains(t, out, "fromUnixTimestamp64Nano", "the snapshot is pinned by nanoseconds, never toDateTime64 on a number")
	assert.Contains(t, out, "boxer.fsmeta")
}

func TestLaneRunsOncePerKeyAndSupersedes(t *testing.T) {
	var l lane[int]
	var runs atomic.Int32
	slow := func(ctx context.Context) (int, error) {
		runs.Add(1)
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(20 * time.Millisecond):
			return 7, nil
		}
	}
	eventually := func(cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for !cond() {
			require.True(t, time.Now().Before(deadline), "condition not reached")
			time.Sleep(2 * time.Millisecond)
		}
	}
	_, done, _, busy := l.demand("a", slow)
	assert.False(t, done)
	assert.True(t, busy)
	_, _, _, busy = l.demand("a", slow)
	assert.True(t, busy, "the same key does not start a second run")
	eventually(func() bool { return runs.Load() == 1 })
	// A new key supersedes: the old result never lands.
	_, _, _, _ = l.demand("b", slow)
	eventually(func() bool { return runs.Load() == 2 })
	deadline := time.Now().Add(2 * time.Second)
	for {
		v, d, err, b := l.demand("b", slow)
		if d && !b {
			require.NoError(t, err)
			assert.Equal(t, 7, v)
			break
		}
		require.True(t, time.Now().Before(deadline), "lane did not complete")
		time.Sleep(5 * time.Millisecond)
	}
	assert.Equal(t, int32(2), runs.Load(), "a done key is a cache hit")
	l.invalidate()
	_, _, _, busy = l.demand("b", slow)
	assert.True(t, busy, "invalidate re-runs the key")
	l.close()
}

func TestHumanSize(t *testing.T) {
	assert.Equal(t, "512 B", humanSize(512))
	assert.Equal(t, "2.0 KiB", humanSize(2048))
	assert.Equal(t, "6.1 GiB", humanSize(6582979993))
}

func TestPrettyValue(t *testing.T) {
	assert.Equal(t, "-rw-r--r--  (420)", prettyValue("mode", "420"))
	assert.Equal(t, "drwxr-xr-x  (2147484141)", prettyValue("mode", "2147484141"))
	assert.Equal(t, "72.1 KiB  (73822)", prettyValue("size", "73822"))
	assert.Equal(t, "12", prettyValue("size", "12"), "small sizes stay as they are")
	assert.Equal(t, "blocks", prettyValue("content", "blocks"))
}

func TestQueryBuildersExpand(t *testing.T) {
	open := ladingsql.Config{Visibility: ladingsql.VisibleAll{}}
	a := location{mount: identifier.TaggedId(4322952322827452417), snap: time.Unix(0, 1755723885967744578)}
	b := location{mount: identifier.TaggedId(4322952322827452418), snap: time.Unix(0, 1755723894483286742)}
	for name, sql := range map[string]string{
		"diff whole":   diffSQL(b, a, "."),
		"diff subtree": diffSQL(b, a, "doc/adr"),
		"history":      historySQL(a.mount, "adr/0198-fs-snapshot-store.md"),
		"open in play": openInPlaySQL(a, "doc"),
	} {
		out, err := ladingsql.Expand(open, sql)
		require.NoErrorf(t, err, "%s must expand: %s", name, sql)
		assert.Containsf(t, out, "boxer.fsmeta", "%s reads the store", name)
	}
	assert.Contains(t, diffSQL(b, a, "doc/adr"), "startsWith(path, 'doc/adr/')")
	assert.Contains(t, diffSQL(b, a, "."), "WHERE 1)", "the root scope is no predicate")
	assert.Contains(t, historySQL(a.mount, "x"), "fs(4322952322827452417, '*')")
	assert.Contains(t, openInPlaySQL(a, "."), "fs(0x3BFE363BCF148001, 1755723885967744578)")
	refs := ladingsql.References(openInPlaySQL(a, "."))
	require.Len(t, refs, 1)
	assert.Equal(t, a.mount, refs[0].Mount)
}

func TestRcloneMountCommandAndSftpSpelling(t *testing.T) {
	loc := location{mount: identifier.TaggedId(4322952322827452417), snap: time.Date(2026, 8, 20, 21, 4, 45, 967744578, time.UTC)}
	assert.Equal(t, `rclone mount --read-only ':sftp,ssh="boxer fs sftp-stdio --mount 0x3BFE363BCF148001",shell_type=unix:/3bfe363bcf148001/latest' /mnt/tally`,
		rcloneMountCommand(loc, true))
	assert.Contains(t, rcloneMountCommand(loc, false), "/3bfe363bcf148001/20260820T210445.967744578Z'")
}

func TestSmallHelpers(t *testing.T) {
	assert.Equal(t, "1", scopePredicate("."))
	assert.Equal(t, "1", scopePredicate(""))
	assert.Equal(t, "startsWith(path, 'a/b/')", scopePredicate("a/b"))
	assert.Equal(t, 1, columnIndex([]string{"path", "Change"}, "change"))
	assert.Equal(t, -1, columnIndex([]string{"path"}, "size"))
	assert.Equal(t, "A", paneIDA.String())
	assert.Equal(t, "B", paneIDB.String())
	for _, s := range []string{"2026-08-20 21:04:45.967744578", "2026-08-20T21:04:45.967744578Z", "2026-08-20 21:04:45"} {
		_, ok := parseSnapText(s)
		assert.True(t, ok, s)
	}
	_, ok := parseSnapText("yesterday")
	assert.False(t, ok)
}

func TestM4BuildersExpand(t *testing.T) {
	open := ladingsql.Config{Visibility: ladingsql.VisibleAll{}}
	loc := location{mount: identifier.TaggedId(4322952322827452417), snap: time.Unix(0, 1755723885967744578)}
	for name, sql := range map[string]string{
		"find dir":     findSQL(loc, findScopeDir, "doc/adr", `\.md$`, ".md", 1024),
		"find mount":   findSQL(loc, findScopeMount, "doc", "", "", 0),
		"find all":     findSQL(loc, findScopeAll, "doc", "x", "", 0),
		"grep dir":     grepSQL(loc, findScopeDir, "doc", "TODO"),
		"grep all":     grepSQL(loc, findScopeAll, "doc", "it's"),
		"du root":      duSQL(loc, "."),
		"du subtree":   duSQL(loc, "doc/adr"),
		"du files":     duFilesSQL(loc, "doc"),
		"problems":     problemsSQL(loc),
		"audit":        auditSQL(loc),
		"history root": historySQL(loc.mount, "."),
	} {
		out, err := ladingsql.Expand(open, sql)
		require.NoErrorf(t, err, "%s must expand: %s", name, sql)
		assert.Containsf(t, out, "boxer.fs", "%s reads the store", name)
	}
	assert.Contains(t, findSQL(loc, findScopeAll, "d", "", "", 0), "fs('*')", "all mounts is the wildcard")
	assert.Contains(t, grepSQL(loc, findScopeDir, "doc", "TODO"), "startsWith(path, 'doc/')")
	assert.Contains(t, grepSQL(loc, findScopeAll, "doc", "x"), "fsdata('*')")
	assert.Contains(t, duSQL(loc, "doc/adr"), "HAVING anc = 'doc/adr' OR startsWith(anc, 'doc/adr/')")
	assert.Contains(t, duSQL(loc, "."), "HAVING 1")
}

func TestBuildDuTree(t *testing.T) {
	res := tableResult{headers: []string{"path", "size"}, rows: [][]string{
		{"a/b/c.txt", "10"}, {"a/b/d.txt", "5"}, {"a/e.txt", "7"}, {"f.bin", "100"}, {"bad", "x"},
	}}
	root, files := buildDuTree(".", res)
	assert.Equal(t, 4, files, "the unparsable size is skipped")
	require.Len(t, root.Children, 2, "a/ and f.bin")
	var a *layout.Node
	for _, ch := range root.Children {
		if ch.Name == "a" {
			a = ch
		}
	}
	require.NotNil(t, a)
	require.Len(t, a.Children, 2, "b/ and e.txt")
	for _, ch := range a.Children {
		if ch.Name == "b" {
			assert.Len(t, ch.Children, 2)
			assert.Equal(t, float64(0), ch.Size, "an interior node carries no own size")
		}
		if ch.Name == "e.txt" {
			assert.Equal(t, float64(7), ch.Size)
		}
	}
}

func TestFindScopeStrings(t *testing.T) {
	assert.Equal(t, "this directory", findScopeDir.String())
	assert.Equal(t, "this snapshot", findScopeMount.String())
	assert.Equal(t, "all mounts", findScopeAll.String())
	f := findState{needle: " TODO "}
	assert.Contains(t, f.armedSQL(location{mount: 4322952322827452417, snap: time.Unix(0, 1)}, "."), "match(data, 'TODO')", "a needle makes it a content search, trimmed")
	f = findState{pattern: "x", minSize: "12"}
	assert.Contains(t, f.armedSQL(location{mount: 4322952322827452417, snap: time.Unix(0, 1)}, "."), "size >= 12")
}

func TestComponentProbes(t *testing.T) {
	reg := componentsql.NewRegistry()
	require.NoError(t, reg.Register(componentsql.Set{Store: "X", Table: "boxer.facts", Kinds: map[string]componentsql.Artefacts{
		"Thing": {Presence: "has(lr, 42)", Filter: "has(lr, 42) AND countEqual(lr, 42) = 1", Projection: "tuple(1)"},
	}}))
	mount := identifier.TaggedId(4322952322827452417)
	snap := time.Unix(0, 1755723885967744578)
	probes := componentProbes(reg, mount, snap, "a/b.txt")
	kinds := make([]string, 0, len(probes))
	for _, p := range probes {
		kinds = append(kinds, p.kind)
		assert.Contains(t, p.sql, "fromUnixTimestamp64Nano(toInt64(1755723885967744578), 'UTC')", "the snapshot pins by nanoseconds")
		assert.Contains(t, p.sql, "'a/b.txt'")
		assert.Contains(t, p.sql, "SELECT count() FROM ")
	}
	assert.Contains(t, kinds, "LadingEntry", "the store's own entry kind is probed first")
	assert.Contains(t, kinds, "LadingSnapshot")
	assert.Contains(t, kinds, "Thing", "and every registered kind after it")
	assert.Equal(t, "boxer.fsmeta", probes[0].table)
	for _, p := range probes {
		if p.kind == "Thing" {
			assert.Equal(t, "boxer.facts", p.table)
			assert.Contains(t, p.sql, "(has(lr, 42)) AND")
		}
	}
}

func TestLaunchConfigRoundTripThroughTheApp(t *testing.T) {
	inst := newApp()
	inst.applyLaunch(launchcfg.TallyLaunch{
		MountA: "3bfe363bcf148002", SnapA: "", DirA: "doc/adr",
		MountB: "0x3BFE363BCF148003", SnapB: "2026-08-20T21:04:54.483286742Z", DirB: "public",
		Sync: true, Target: "B",
	})
	assert.Equal(t, identifier.TaggedId(4322952322827452418), inst.panes[paneIDA].mount)
	assert.True(t, inst.panes[paneIDA].followLatest)
	assert.Equal(t, "doc/adr", inst.panes[paneIDA].st.Dir())
	assert.Equal(t, identifier.TaggedId(4322952322827452419), inst.panes[paneIDB].mount)
	assert.False(t, inst.panes[paneIDB].followLatest)
	assert.Equal(t, "public", inst.panes[paneIDB].st.Dir())
	assert.True(t, inst.syncBrowse)
	assert.Equal(t, paneIDB, inst.target)

	cfg := inst.composeLaunch()
	assert.Equal(t, "3bfe363bcf148002", cfg.MountA)
	assert.Equal(t, "", cfg.SnapA, "following latest records no snapshot")
	assert.Equal(t, "2026-08-20T21:04:54.483286742Z", cfg.SnapB)
	assert.Equal(t, "B", cfg.Target)
	assert.True(t, cfg.Sync)

	// Nothing done since mount: not dirty, nothing to write.
	inst.mountsSeen = true
	inst.syncWorkingsetDirty()
	b, dirty, err := inst.ComposeWorkingset()
	require.NoError(t, err)
	assert.False(t, dirty)
	assert.Nil(t, b)
	// The reader moves: dirty, and the bytes decode to what was composed.
	inst.panes[paneIDA].st.SetDir("doc")
	inst.syncWorkingsetDirty()
	b, dirty, err = inst.ComposeWorkingset()
	require.NoError(t, err)
	assert.True(t, dirty)
	out, err := buscodec.Decode[launchcfg.TallyLaunch](b)
	require.NoError(t, err)
	assert.Equal(t, "doc", out.DirA)
}

func TestParseMountText(t *testing.T) {
	id, ok := parseMountText("0x3BFE363BCF148001")
	assert.True(t, ok)
	assert.Equal(t, identifier.TaggedId(4322952322827452417), id)
	_, ok = parseMountText("")
	assert.False(t, ok)
	_, ok = parseMountText("zz")
	assert.False(t, ok)
}

func TestStringTableWidthColumns(t *testing.T) {
	tb := stringTable{scopeKey: "diff-table", headers: []string{"path", "change"}, widths: []float32{360}}
	cols := tb.widthColumns()
	assert.Equal(t, []colwidth.Column{{Name: "path", Type: "text;view=diff-table"}, {Name: "change", Type: "text;view=diff-table"}}, cols)
	assert.Equal(t, []float64{360, float64(tableDefaultWidth)}, tb.widthDefaults())
	other := stringTable{scopeKey: "find-table", headers: []string{"path"}}
	assert.NotEqual(t, cols[0], other.widthColumns()[0], "the same header in another table is another column")
	assert.NotEqual(t, widthSignature(cols), widthSignature(cols[:1]))
}

func TestTablesAreWiredToTheResolver(t *testing.T) {
	inst := newApp()
	assert.Len(t, inst.tables(), 5)
	for _, tb := range inst.tables() {
		assert.Nil(t, tb.res, "no host store: no resolver")
	}
}
