package sqlapplet

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/code/analysis/golang/codevol"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

func codevolDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("codevol", help.MustSub(bookcodevolFS, "bookcodevol"))
	require.Empty(t, errs)
	require.Len(t, defs, 4)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, 4)
	return bySlug
}

// TestCodevolBookCorpus is the ADR-0132 §SD6 gate over the code-volume book.
func TestCodevolBookCorpus(t *testing.T) {
	bySlug := codevolDefsBySlug(t)
	for slug, d := range bySlug {
		assert.Equal(t, EndpointIntrospection, d.Endpoint, slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: keelson('…') classifies as a local read", slug)
		assert.NotEmpty(t, d.Icon, slug)
		assert.False(t, d.HasUnboundSlots, "%s: every knob is prelude-bound", slug)
	}

	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["vol-overview"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["vol-modules"].Tabs)
	assert.Equal(t, []TabSel{{ID: "treemap"}, {ID: "table"}}, bySlug["vol-map"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["vol-lenses"].Tabs)

	// The map declares the ADR-0166 nodes contract; without these the panel
	// falls back to a different arm and renders nothing useful.
	mapSQL := bySlug["vol-map"].SQL
	for _, col := range []string{" AS id", " AS parent", " AS value", " AS label", " AS color"} {
		assert.Contains(t, mapSQL, col, "vol-map: the nodes contract needs%s", col)
	}
	assert.NotContains(t, mapSQL, " AS stack", "vol-map: the folded arm would win over the nodes arm")
}

// splitPrelude separates an applet buffer's SET statements from its query
// body, so a test can wrap the body in an aggregate while still binding the
// parameters the body reads. A buffer is `SET …;` lines followed by one
// statement (ADR-0132), and a nested statement cannot carry its own SET.
func splitPrelude(sql string) (prelude, body string) {
	lines := strings.Split(sql, "\n")
	i := 0
	for ; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		if strings.HasPrefix(t, "SET ") && strings.HasSuffix(t, ";") {
			continue
		}
		break
	}
	return strings.Join(lines[:i], "\n"), strings.Join(lines[i:], "\n")
}

// codevolGateSource is the fixture the engine gate serves go_modules and
// go_symbols from.
//
// A fixture rather than the running test binary, because `go test` links the
// binary it runs *without* a symbol table (`go test -c` does not), so a
// self-read would yield zero symbol rows and the buffers would be asserted
// against nothing. The shapes here are the ones the SQL reasons about: a main
// module, two dependencies (one of them replaced), stdlib packages owned by no
// module, and a module that shipped no code at all.
type codevolGateSource struct{}

func (codevolGateSource) Get() ([]codevol.ModuleInfo, codevol.SymbolReport) {
	mods := []codevol.ModuleInfo{
		{Path: "example.com/main", Version: "(devel)", IsMain: true, Party: codevol.PartyFirst},
		{Path: "example.com/dep", Version: "v1.2.3", Sum: "h1:abc=", Party: codevol.PartyThird},
		{Path: "example.com/forked", Version: "v0.1.0", ReplacedBy: "example.com/fork@v0.2.0", Party: codevol.PartyThird},
		// Declared but contributing no symbols — the "zero bytes is not
		// unused" row the modules applet documents.
		{Path: "example.com/typesonly", Version: "v0.0.1", Sum: "h1:def=", Party: codevol.PartyThird},
	}
	rep := codevol.SymbolReport{
		ModuleExact: true,
		TotalText:   1000 + 400 + 250 + 300 + 120,
		TotalData:   50 + 20 + 10 + 5 + 2,
		Packages: []codevol.PackageSymbols{
			{PkgPath: "example.com/main/app", ModulePath: "example.com/main", Party: codevol.PartyFirst,
				NumSymbols: 10, TextBytes: 1000, DataBytes: 50},
			{PkgPath: "example.com/dep/x", ModulePath: "example.com/dep", Party: codevol.PartyThird,
				NumSymbols: 4, TextBytes: 400, DataBytes: 20},
			{PkgPath: "example.com/forked/y", ModulePath: "example.com/forked", Party: codevol.PartyThird,
				NumSymbols: 3, TextBytes: 250, DataBytes: 10},
			{PkgPath: "net/http", ModulePath: "std", Party: codevol.PartyStdlib,
				NumSymbols: 5, TextBytes: 300, DataBytes: 5},
			{PkgPath: "runtime", ModulePath: "std", Party: codevol.PartyStdlib,
				NumSymbols: 2, TextBytes: 120, DataBytes: 2},
		},
	}
	return mods, rep
}

// TestCodevolBookQueries runs every buffer verbatim through the introspect
// engine over the two tables the book reads.
//
// Coverage is registered with a nil source, so the coverage tables are empty
// and the shipped-vs-executed buffer exercises its uninstrumented path — the
// shape every appliance build has.
func TestCodevolBookQueries(t *testing.T) {
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(30 * time.Second)
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
	require.NoError(t, providers.RegisterCodevol(reg, codevolGateSource{}))
	// nil source: the coverage tables exist and are empty, which is the
	// uninstrumented-build shape vol-lenses must survive.
	require.NoError(t, providers.RegisterCoverage(reg, nil))

	caller := bus.NewClient("test.codevol.engine", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	e, err := introspectengine.New(introspectengine.Config{Registry: reg, Bus: caller}, logger)
	require.NoError(t, err)

	query := func(sql string) string {
		t.Helper()
		body, _, qErr := e.Query(context.Background(), sql, "TabSeparated")
		require.NoError(t, qErr, "query failed:\n%s", sql)
		return strings.TrimSpace(string(body))
	}

	bySlug := codevolDefsBySlug(t)
	for _, slug := range []string{"vol-overview", "vol-modules", "vol-map", "vol-lenses"} {
		t.Run(slug, func(t *testing.T) {
			out := query(bySlug[slug].SQL)
			// Every buffer must parse, execute and produce rows against the
			// real tables. An empty result would mean the tables did not
			// answer, which is the regression this gate exists to catch.
			assert.NotEmpty(t, out, "%s produced no rows", slug)
		})
	}

	// The overview's headline must be arithmetic, not decoration: the three
	// party byte totals partition the whole.
	assert.Equal(t, "1", query(`
SELECT sumIf(text_bytes, party = 'first') + sumIf(text_bytes, party = 'third')
     + sumIf(text_bytes, party = 'stdlib') = sum(text_bytes)
FROM keelson('go_symbols')`), "party split must partition the text bytes")

	// Every module a symbol was attributed to is one go_modules declares, or
	// the stdlib sentinel. Attribution must never invent a module.
	assert.Equal(t, "0", query(`
SELECT count()
FROM keelson('go_symbols')
WHERE module_path != 'std'
  AND module_path NOT IN (SELECT path FROM keelson('go_modules'))`),
		"go_symbols attributed code to an undeclared module")

	// A module that shipped no symbols still gets a row, with zeroes — the
	// left join in vol-modules is what makes "declared but contributed
	// nothing" visible rather than absent.
	modPre, modBody := splitPrelude(bySlug["vol-modules"].SQL)
	assert.Equal(t, "example.com/typesonly\t0\t0",
		query(modPre+"\nSELECT module, packages, text_bytes FROM ("+modBody+
			") WHERE module = 'example.com/typesonly'"))

	// A replaced module must surface its replacement, because path@version
	// does not describe the code that shipped.
	assert.Equal(t, "example.com/fork@v0.2.0",
		query(modPre+"\nSELECT replaced_by FROM ("+modBody+
			") WHERE module = 'example.com/forked'"))

	// The treemap arm must be a well-formed tree: exactly one root, and every
	// non-root parent must exist as a node.
	mapPre, mapBody := splitPrelude(bySlug["vol-map"].SQL)
	assert.Equal(t, "1", query(mapPre+"\nSELECT count() FROM ("+mapBody+") WHERE parent = ''"),
		"vol-map must have exactly one root")
	assert.Equal(t, "0", query(mapPre+"\nSELECT count() FROM ("+mapBody+") AS n WHERE n.parent != ''"+
		" AND n.parent NOT IN (SELECT id FROM ("+mapBody+"))"),
		"vol-map has nodes whose parent is not a node")
	// Leaf area must sum to the whole 2070 bytes; interior nodes carry no
	// area of their own, which is what keeps a treemap from double-counting.
	assert.Equal(t, "2070\t0", query(mapPre+"\nSELECT toUInt64(sum(value)),"+
		" toUInt64(sumIf(value, id IN ('binary', 'first', 'third', 'stdlib'))) FROM ("+mapBody+")"))
	// The standard library has no modules, so it is broken down by top-level
	// directory instead.
	assert.Equal(t, "net,runtime", query(mapPre+
		"\nSELECT arrayStringConcat(arraySort(groupArray(id)), ',')"+
		" FROM ("+mapBody+") WHERE parent = 'stdlib'"))

	// go_modules answers even where go_symbols cannot — it is the floor of
	// the tiering.
	n, err := strconv.Atoi(query("SELECT count() FROM keelson('go_modules')"))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 1, "go_modules must always carry at least the main module")
}
