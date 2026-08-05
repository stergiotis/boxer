package sqlapplet

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providersgodep"
)

// godepDefsBySlug parses the embedded godep book and indexes it by slug.
func godepDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("godep", help.MustSub(bookgodepFS, "bookgodep"))
	require.Empty(t, errs)
	require.Len(t, defs, 4)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, 4)
	return bySlug
}

// TestGodepBookCorpus is the ADR-0132 §SD6 gate over the Go dependency book.
func TestGodepBookCorpus(t *testing.T) {
	bySlug := godepDefsBySlug(t)
	for slug, d := range bySlug {
		assert.Equal(t, EndpointIntrospection, d.Endpoint, slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: keelson('…') classifies as a local read", slug)
		assert.NotEmpty(t, d.Icon, slug)
	}

	masterDetail := []TabSel{{ID: "table"}, {ID: "network"}, {ID: "detail"}}
	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["go-overview"].Tabs)
	assert.Equal(t, masterDetail, bySlug["go-packages"].Tabs)
	assert.Equal(t, masterDetail, bySlug["go-architecture"].Tabs)
	assert.Equal(t, masterDetail, bySlug["go-modules"].Tabs)

	// The two *navigable* lenses deliberately leave ONE slot unbound —
	// `selection_key`, the signal a table row or graph vertex click
	// publishes. That is what makes them Live and click-driven, and it is why
	// this book does not share the topology book's "params are prelude-bound"
	// assertion. Every other knob IS prelude-bound (a widget, not a signal),
	// so the buffer runs on mount.
	//
	// The overview has nothing to focus, and the architecture quotient draws
	// whole — its detail comes from the clicked row's own `members` /
	// `violation_edges` columns, not from a re-query — so neither takes the
	// signal, and neither is Live.
	for _, slug := range []string{"go-packages", "go-modules"} {
		assert.True(t, bySlug[slug].HasUnboundSlots, "%s: selection_key is a signal, not a prelude value", slug)
		assert.Contains(t, bySlug[slug].SQL, "{selection_key:String}", slug)
		assert.NotContains(t, bySlug[slug].SQL, "SET param_selection_key",
			"%s: binding it in the prelude would turn the signal into a fixed value", slug)
	}
	for _, slug := range []string{"go-overview", "go-architecture"} {
		assert.False(t, bySlug[slug].HasUnboundSlots, "%s: every knob is prelude-bound", slug)
	}
	// The architecture lens carries its detail as data instead.
	assert.Contains(t, bySlug["go-architecture"].SQL, "AS members")
	assert.Contains(t, bySlug["go-architecture"].SQL, "AS violation_edges")
}

// TestMintGodepBook mints the book beside its siblings, guarding slug
// collisions across all three embedded corpora.
func TestMintGodepBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "sqlapplet", fsys: help.MustSub(bookFS, "book"), topics: []app.TopicT{app.TopicRuntime}},
		{id: "topology", fsys: help.MustSub(booktopoFS, "booktopo"), topics: []app.TopicT{app.TopicTopology}},
		{id: "godep", fsys: help.MustSub(bookgodepFS, "bookgodep"), topics: []app.TopicT{app.TopicCode}},
	})
	require.Empty(t, errs)
	assert.Equal(t, 14, minted)
	m, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + "go-packages"))
	require.True(t, ok)
	assert.Equal(t, "Go packages", m.Display)
}

// TestGodepBookQueriesExecute runs every buffer verbatim — SET prelude
// included — through the introspect engine over the *live* godep tables, so
// the lenses are checked against this repository's real import graph rather
// than a fixture. It is the live half of the corpus gate.
//
// The unbound `selection_key` slot is supplied the way play supplies it: as
// a SET line prepended to the buffer, which is what the param channel does
// with a signal value at execute time.
func TestGodepBookQueriesExecute(t *testing.T) {
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed: %v", err)
	}
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
	require.NoError(t, providersgodep.Register(reg, providersgodep.Config{Log: logger}))

	caller := bus.NewClient("test.bookgodep.engine", []app.SubjectFilter{
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

	// The first query starts collection and waits for it; if the toolchain is
	// unavailable or this is not a checkout, the tables stay empty and there
	// is nothing to assert against.
	status, err := query("SELECT status FROM keelson('go_collection')")
	require.NoError(t, err)
	require.Len(t, status, 1)
	if status[0] != "ready" {
		t.Skipf("godep collection not ready (status %q)", status[0])
	}

	bySlug := godepDefsBySlug(t)

	// asAuthored is the buffer as play executes it on mount: an unbound
	// String signal ships as the empty value (signalDefaultsEmpty), which is
	// what lets a click-driven applet run before the first click. Without it
	// ClickHouse refuses the query outright ("Substitution … is not set").
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

	// The overview reports the collection this process made.
	rows, err := query(bySlug["go-overview"].SQL)
	require.NoError(t, err)
	joined := strings.Join(rows, "\n")
	assert.Contains(t, joined, "github.com/stergiotis/boxer", "the root module is named")
	assert.Contains(t, joined, "class · internal")

	// The packages lens: a focused walk, driven the way a click drives it.
	focused := "SET param_selection_key = 'github.com/stergiotis/boxer/apps/play';\n" + bySlug["go-packages"].SQL
	rows, err = query(focused)
	require.NoError(t, err)
	assert.Greater(t, len(rows), 500, "the closure table is the full package list")

	// …and its graph lanes answer for that focus. The `vertices` CTE is what
	// the Network tab demands on its own lane, so it is checked directly.
	vertSQL := strings.Replace(focused, "SELECT * FROM pkgs", "SELECT id, label, `group`, tone FROM vertices", 1)
	rows, err = query(vertSQL)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	assert.LessOrEqual(t, len(rows), 120, "max_nodes caps the drawn neighbourhood")
	assert.Contains(t, strings.Join(rows, "\n"), "\taccent", "the focus node is toned")

	edgeSQL := strings.Replace(focused, "SELECT * FROM pkgs", "SELECT source, target FROM edges", 1)
	rows, err = query(edgeSQL)
	require.NoError(t, err)
	assert.NotEmpty(t, rows, "a focused package has import edges")

	// The architecture lens: the quotient's own table, plus the two coloured
	// verdicts. apps/play is a group at the default depth.
	rows, err = query(bySlug["go-architecture"].SQL)
	require.NoError(t, err)
	assert.Contains(t, strings.Join(rows, "\n"), "apps/play")

	// Tallying by tone rather than parsing rows: an empty trailing field is
	// invisible in TabSeparated, so the count has to come from SQL.
	archEdges := strings.Replace(bySlug["go-architecture"].SQL,
		"SELECT * FROM groups", "SELECT if(tone = '', 'plain', tone) AS t, count() FROM edges GROUP BY t ORDER BY t", 1)
	rows, err = query(archEdges)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	tones := map[string]bool{}
	for _, r := range rows {
		tones[strings.Split(r, "\t")[0]] = true
	}
	assert.True(t, tones["plain"], "most quotient edges are plain")
	assert.True(t, tones["warning"], "this repository has group cycles, drawn in the warning tone")

	// The modules lens: the rollup, and the witness chain for a module we
	// know is a direct dependency.
	rows, err = query("SET param_selection_key = 'github.com/rs/zerolog';\n" + bySlug["go-modules"].SQL)
	require.NoError(t, err)
	assert.Contains(t, strings.Join(rows, "\n"), "github.com/rs/zerolog")

	witSQL := strings.Replace(
		"SET param_selection_key = 'github.com/rs/zerolog';\n"+bySlug["go-modules"].SQL,
		"SELECT * FROM mods", "SELECT source, target FROM edges", 1)
	rows, err = query(witSQL)
	require.NoError(t, err)
	assert.NotEmpty(t, rows, "a directly-imported module has a witness chain")
}
