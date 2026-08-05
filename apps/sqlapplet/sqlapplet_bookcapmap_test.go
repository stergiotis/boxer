package sqlapplet

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

// capmapDefsBySlug parses the embedded competence book and indexes it by slug.
func capmapDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("capmap", help.MustSub(bookcapmapFS, "bookcapmap"))
	require.Empty(t, errs)
	require.Len(t, defs, 4)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, 4)
	return bySlug
}

// TestCapmapBookCorpus is the ADR-0132 §SD6 gate over the competence book.
func TestCapmapBookCorpus(t *testing.T) {
	bySlug := capmapDefsBySlug(t)
	for slug, d := range bySlug {
		assert.Equal(t, EndpointIntrospection, d.Endpoint, slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: keelson('…') classifies as a local read", slug)
		assert.NotEmpty(t, d.Icon, slug)
	}

	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["comp-overview"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}, {ID: "detail"}, {ID: "network"}}, bySlug["comp-browser"].Tabs)
	assert.Equal(t, []TabSel{{ID: "treemap"}, {ID: "table"}}, bySlug["comp-map"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}, {ID: "detail"}}, bySlug["comp-lint"].Tabs)

	// The browser is the one click-driven lens: `selection_key` stays UNBOUND,
	// which is what makes it a signal a table row or graph vertex publishes
	// rather than a fixed value, and what puts the applet in Live mode.
	assert.True(t, bySlug["comp-browser"].HasUnboundSlots, "comp-browser: selection_key is a signal, not a prelude value")
	assert.Contains(t, bySlug["comp-browser"].SQL, "{selection_key:String}")
	assert.NotContains(t, bySlug["comp-browser"].SQL, "SET param_selection_key",
		"binding it in the prelude would turn the signal into a fixed value")

	// The other three draw whole: every knob is a widget, so they run on mount.
	for _, slug := range []string{"comp-overview", "comp-map", "comp-lint"} {
		assert.False(t, bySlug[slug].HasUnboundSlots, "%s: every knob is prelude-bound", slug)
	}

	// The map declares the ADR-0166 nodes contract, not the folded one: a
	// competence's own prose has to be able to carry a rectangle, and only the
	// nodes arm lets an interior node hold a value.
	mapSQL := bySlug["comp-map"].SQL
	for _, col := range []string{" AS id", " AS parent", " AS value", " AS label", " AS color"} {
		assert.Contains(t, mapSQL, col, "comp-map: the nodes contract needs%s", col)
	}
	assert.NotContains(t, mapSQL, " AS stack", "comp-map: the folded arm would win over the nodes arm")

	// The browser's body column declares its media type, which is what makes
	// the Detail tab render markdown rather than escaped text (ADR-0123 §SD2).
	assert.Contains(t, bySlug["comp-browser"].SQL, "body@text/markdown")
}

// TestMintCapmapBook mints the book beside its siblings, guarding slug
// collisions across all four embedded corpora.
func TestMintCapmapBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "sqlapplet", fsys: help.MustSub(bookFS, "book"), topics: []app.TopicT{app.TopicRuntime}},
		{id: "topology", fsys: help.MustSub(booktopoFS, "booktopo"), topics: []app.TopicT{app.TopicTopology}},
		{id: "godep", fsys: help.MustSub(bookgodepFS, "bookgodep"), topics: []app.TopicT{app.TopicCode}},
		{id: "pprof", fsys: help.MustSub(bookpprofFS, "bookpprof"), topics: []app.TopicT{app.TopicObservability}},
		{id: "capmap", fsys: help.MustSub(bookcapmapFS, "bookcapmap"), topics: []app.TopicT{app.TopicCode}},
	})
	require.Empty(t, errs)
	assert.Equal(t, 22, minted)
	m, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + "comp-map"))
	require.True(t, ok)
	assert.Equal(t, "Competence map", m.Display)
	assert.Equal(t, []app.TopicT{app.TopicCode}, m.Topics)
}

// capmapTestVault materialises a small vault that exercises every shape the
// book's four buffers read, and points the corpus at it for the test.
//
// A fixture rather than this repository's own doc/competences, because that
// directory is git-ignored (ADR-0168 §SD7): CI has no corpus, and a test that
// only passed on a populated working tree would be green for the wrong reason.
// The shapes it has to carry are the ones the buffers reason about — a
// directory-backed competence (a `capability.md`, the vault's own spelling),
// a multi-parent level-4 leaf, an interior node
// with prose of its own, a citation, a dirref and a genuinely dangling link.
func capmapTestVault(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"toolbelt/capability.md": "---\nname: Toolbelt\nlevel: 1\ndomain: boxer-toolbelt\ncatalog: boxer\n---\n\n" +
			"# Vision and Scope\n\nThe root has prose of its own, which is what earns it a rectangle.\n",
		"toolbelt/observability/capability.md": "---\nname: Observability\nabbrev: Obs\nlevel: 2\n" +
			"domain: boxer-toolbelt\ncatalog: boxer\nowner: Platform Lead\nmaturity: 3\npain: 1\n" +
			"parent_ids:\n  - \"[[toolbelt/capability]]\"\n---\n\n" +
			"# Vision and Scope\n\nLogging, tracing, profiling.\n\n" +
			"# Standards\n\nCites [[Jouppi-1990]] and [[open-telemetry]].\n",
		"toolbelt/observability/obs-logging.md": "---\nname: Logging\nlevel: 3\n" +
			"domain: boxer-toolbelt\ncatalog: boxer\nparent_ids:\n  - \"[[observability/capability]]\"\n---\n\n" +
			"# Vision and Scope\n\nStructured logging.\n\n" +
			"# Activities\n\nSee [[observability]] for the parent.\n",
		"toolbelt/numerical/capability.md": "---\nname: Numerical\nlevel: 2\n" +
			"domain: boxer-numerics\ncatalog: boxer\nparent_ids:\n  - \"[[toolbelt/capability]]\"\n---\n\n" +
			"# Vision and Scope\n\nAxis layout and entropy.\n",
		"toolbelt/numerical/num-shared-block.md": "---\nname: Shared Block\nlevel: 4\n" +
			"domain: boxer-numerics\ncatalog: boxer\n" +
			"parent_ids:\n  - \"[[numerical/capability]]\"\n  - \"[[observability/capability]]\"\n---\n\n" +
			"# Vision and Scope\n\nA building block two competences both use.\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	capmapcorpus.SetVaultForTest(t, root)
}

// overSink swaps a buffer's final statement for another one over the same
// CTEs, so a lane the tabs read privately — a treemap's node set, a lint's
// tally — can be asserted without restating the prelude.
//
// It cuts at the last top-level SELECT rather than string-replacing the
// authored one, because the authored text is prose under maintenance and a
// replacement that silently missed would leave the test asserting the sink it
// was trying to bypass.
func overSink(sql string, sink string) (out string) {
	i := strings.LastIndex(sql, "\nSELECT ")
	if i < 0 {
		return sql
	}
	return sql[:i+1] + sink
}

// TestCapmapBookQueriesExecute runs every buffer verbatim — SET prelude
// included — through the introspect engine over the live competence tables. It
// is the half of the corpus gate a parse cannot reach: a buffer that parses,
// classifies and mints can still name a column that does not exist.
func TestCapmapBookQueriesExecute(t *testing.T) {
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed: %v", err)
	}
	capmapTestVault(t)

	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.WarnLevel)
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(120 * time.Second)
	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})

	reg := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(reg))

	caller := bus.NewClient("test.bookcapmap.engine", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	e, err := introspectengine.New(introspectengine.Config{Registry: reg, Bus: caller}, logger)
	require.NoError(t, err)

	query := func(sql string) (rows []string, err error) {
		body, _, qErr := e.Query(context.Background(), sql, "TabSeparated")
		if qErr != nil {
			return nil, qErr
		}
		out := strings.TrimSpace(string(body))
		if out == "" {
			return nil, nil
		}
		return strings.Split(out, "\n"), nil
	}

	bySlug := capmapDefsBySlug(t)

	// asAuthored is the buffer as play executes it on mount: an unbound String
	// signal ships as the empty value, which is what lets a click-driven applet
	// run before the first click.
	asAuthored := func(d *AppletDef) string {
		if !d.HasUnboundSlots {
			return d.SQL
		}
		return "SET param_selection_key = '';\n" + d.SQL
	}
	for slug, d := range bySlug {
		rows, qErr := query(asAuthored(d))
		require.NoError(t, qErr, "%s: buffer failed", slug)
		assert.NotEmpty(t, rows, "%s: the sink returned no rows", slug)
	}

	// The overview reports the fixture it is looking at, including the reading
	// that motivates the row: how much of the corpus carries a judgement.
	rows, err := query(bySlug["comp-overview"].SQL)
	require.NoError(t, err)
	joined := strings.Join(rows, "\n")
	assert.Contains(t, joined, "competences\t5")
	assert.Contains(t, joined, "assessed (maturity)\t1")
	assert.Contains(t, joined, "domain · boxer-toolbelt\t3")

	// The browser: unfocused it lists the corpus, and the note it reassembles
	// is the markdown the Detail tab renders.
	bodySQL := strings.Replace("SET param_selection_key = '';\n"+bySlug["comp-browser"].SQL,
		"SELECT * FROM comps",
		"SELECT slug, `body@text/markdown` FROM comps WHERE slug = 'observability'", 1)
	rows, err = query(bodySQL)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Contains(t, rows[0], "# Vision and Scope", "the body is the note's own markdown")
	assert.Contains(t, rows[0], "# Standards")

	// …and its graph lanes answer for a focus, the way a click drives them.
	focused := "SET param_selection_key = 'observability';\n" + bySlug["comp-browser"].SQL
	vertSQL := strings.Replace(focused, "SELECT * FROM comps", "SELECT id, tone FROM vertices ORDER BY id", 1)
	rows, err = query(vertSQL)
	require.NoError(t, err)
	// The focus, its parent, its two children — and `numerical`, which is in
	// the picture only because it co-parents one of those children. The last
	// row loses its trailing empty `tone` field to the harness's TrimSpace,
	// which is a TabSeparated reading artifact and not a missing column.
	assert.Equal(t, []string{
		"num-shared-block\t", "numerical\t", "obs-logging\t", "observability\taccent", "toolbelt",
	}, rows)

	edgeSQL := strings.Replace(focused, "SELECT * FROM comps", "SELECT source, target FROM edges ORDER BY source, target", 1)
	rows, err = query(edgeSQL)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"numerical\tnum-shared-block", "observability\tnum-shared-block",
		"observability\tobs-logging", "toolbelt\tnumerical", "toolbelt\tobservability",
	}, rows, "a shared block keeps both of its parent edges")
	// `toolbelt -> numerical` is drawn because both ends are in the picture,
	// and it is what stops the co-parent from floating unattached.

	// The map: one row per competence, an interior node carrying its own bytes,
	// and every node reachable from a root.
	rows, err = query(bySlug["comp-map"].SQL)
	require.NoError(t, err)
	assert.Len(t, rows, 5, "one row per competence")

	rows, err = query(overSink(bySlug["comp-map"].SQL, "SELECT id, parent, color FROM nodes WHERE parent = ''"))
	require.NoError(t, err)
	require.Len(t, rows, 1, "exactly one root, or the treemap draws a forest it was not asked for")
	assert.Equal(t, "toolbelt\t\ttoolbelt", rows[0],
		"a root with no level-2 ancestor colours by its topmost ancestor, which is itself")

	// A competence with two parents is drawn under exactly one of them.
	rows, err = query(overSink(bySlug["comp-map"].SQL, "SELECT parent FROM nodes WHERE id = 'num-shared-block'"))
	require.NoError(t, err)
	assert.Equal(t, []string{"numerical"}, rows, "the alphabetically first parent wins the partition")

	// The interior root has prose of its own, which is the ADR-0166 §SD3 self
	// cell: a value on an interior node, not silently redistributed downward.
	rows, err = query(overSink(bySlug["comp-map"].SQL, "SELECT value > 0 FROM nodes WHERE id = 'toolbelt'"))
	require.NoError(t, err)
	assert.Equal(t, []string{"1"}, rows)

	// The lint: only the genuinely-missing slug is unresolved. The citation is
	// external, and the bare link to a directory-backed competence is a dirref
	// — the whole reason `resolution` has four states.
	allSQL := strings.Replace(bySlug["comp-lint"].SQL, "SET param_show = 'unresolved';", "SET param_show = 'all';", 1)
	rows, err = query(overSink(allSQL, "SELECT resolution, count() FROM rels GROUP BY resolution ORDER BY resolution"))
	require.NoError(t, err)
	assert.Equal(t, []string{"direct\t5", "dirref\t1", "external\t1", "unresolved\t1"}, rows)

	rows, err = query(bySlug["comp-lint"].SQL)
	require.NoError(t, err)
	require.Len(t, rows, 1, "one genuinely dangling link")
	assert.Contains(t, rows[0], "open-telemetry")
	assert.Contains(t, rows[0], "Standards", "a body link carries the heading it was found under")
}
