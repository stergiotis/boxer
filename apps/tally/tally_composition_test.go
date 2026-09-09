package tally

import (
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/tally/launchcfg"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/identity/identifier"
)

// The composition surface of ADR-0222: what a caller can ask a tally window
// to look at, and what happens to the query it may pass.

func TestLaunchCarriesSelectionTabAndQuery(t *testing.T) {
	inst := newApp()
	inst.applyLaunch(launchcfg.TallyLaunch{
		MountA: "3bfe363bcf148002",
		SelA:   "doc/adr/0222-tally-composition-surface.md",
		MountB: "3bfe363bcf148003", DirB: "public",
		SelB:   "public/README.md",
		Target: "A",
	})
	// A selection implies its directory when the config named none, and does
	// not override one it did name.
	assert.Equal(t, "doc/adr", inst.panes[paneIDA].st.Dir())
	assert.Equal(t, "doc/adr/0222-tally-composition-surface.md", inst.panes[paneIDA].pendingSel)
	assert.Equal(t, "public", inst.panes[paneIDB].st.Dir())
	assert.Equal(t, "public/README.md", inst.panes[paneIDB].pendingSel)
}

func TestLaunchWithAQueryOpensTheResultsTab(t *testing.T) {
	inst := newApp()
	inst.applyLaunch(launchcfg.TallyLaunch{
		MountA: "3bfe363bcf148002",
		Sql:    "  SELECT path FROM fs(1, 2)  ",
		Target: "A",
	})
	assert.Equal(t, "SELECT path FROM fs(1, 2)", inst.querySql, "the buffer is trimmed")
	assert.Equal(t, dockTabResults, inst.pendingDockActivate,
		"a config carrying a query and no tab opens on what it returned")
	assert.Equal(t, paneIDR, inst.focus)
	assert.Equal(t, paneIDA, inst.target, "the Mounts pane still addresses a browse pane")

	cfg := inst.composeLaunch()
	assert.Equal(t, launchcfg.TabResults, cfg.Tab)
	assert.Equal(t, "SELECT path FROM fs(1, 2)", cfg.Sql)
}

// The store is part of the launch (ADR-0222 Updates 2026-09-09): a config
// naming a database opens the window over the layout in it and reports the
// same database back, and one naming none reads the default store — which is
// what every config written before the field existed says.
func TestLaunchDatabaseSelectsTheStore(t *testing.T) {
	inst := newApp()
	inst.applyLaunch(launchcfg.TallyLaunch{Database: " shadowboxer ", Target: "A"})
	assert.Equal(t, "shadowboxer", inst.layout.Database)
	assert.Equal(t, "shadowboxer.fsmeta", inst.layout.MetaTable())
	assert.Equal(t, "shadowboxer", inst.composeLaunch().Database)
	assert.Equal(t, "shadowboxer", sqlConfig(inst.layout).Database)

	inst = newApp()
	inst.applyLaunch(launchcfg.TallyLaunch{Target: "A"})
	assert.Empty(t, inst.layout.Database)
	assert.Equal(t, ladingschema.DatabaseName+".fsmeta", inst.layout.MetaTable())
	assert.Empty(t, inst.composeLaunch().Database)
}

// Over another store the buffer handed to play has the macro expanded
// against the layout's tables, so play — whose expansion is bound to the
// default store — runs it as it stands.
func TestOpenInPlayBufferNamesTheLayoutTables(t *testing.T) {
	layout := ladingschema.Layout{Database: "shadowboxer"}
	loc := location{mount: identifier.TaggedId(0x3bfe363bcf148002), snap: time.Unix(0, 1_700_000_000_000_000_000).UTC()}
	expanded, err := ladingsql.Expand(sqlConfig(layout), openInPlaySQL(loc, "music"))
	require.NoError(t, err)
	assert.Contains(t, expanded, "FROM shadowboxer.fsmeta")
	assert.NotContains(t, expanded, "FROM "+ladingschema.DatabaseName+".fsmeta")
	assert.NotContains(t, expanded, "fs(")
	assert.True(t, strings.HasPrefix(expanded, "-- tally:"), "the header comment survives the expansion")
	assert.Equal(t, analysis.KindReadOnly, analysis.ClassifyStatementKind(expanded))
}

func TestLaunchTabWithoutAQueryIgnoresResults(t *testing.T) {
	inst := newApp()
	inst.applyLaunch(launchcfg.TallyLaunch{Tab: launchcfg.TabResults, Target: "A"})
	assert.Zero(t, inst.pendingDockActivate, "there is no Results tab without a query")
	assert.Equal(t, paneIDA, inst.focus)

	inst = newApp()
	inst.applyLaunch(launchcfg.TallyLaunch{Tab: launchcfg.TabHistory, Target: "A"})
	assert.Equal(t, dockTabHistory, inst.pendingDockActivate)
	assert.Equal(t, launchcfg.TabHistory, inst.composeLaunch().Tab)
}

func TestEveryDeclaredTabSlugResolves(t *testing.T) {
	for _, slug := range launchcfg.TabIds() {
		id := dockTabForSlug(slug)
		assert.NotZero(t, id, "slug %q has no dock tab", slug)
		assert.Equal(t, slug, tabSlug(id), "slug %q does not round-trip", slug)
	}
	assert.Zero(t, dockTabForSlug("no-such-tab"))
	assert.Empty(t, tabSlug(0), "a tab this window never raised reports nothing")
}

// A query that is not provably read-only is refused before it reaches the
// executor. tally holds the operator's credentials and never writes; a launch
// config is an argument from another app, so the property is checked.
func TestPathSetRefusesAStatementThatIsNotReadOnly(t *testing.T) {
	for _, sql := range []string{
		"ALTER TABLE boxer.facts DELETE WHERE 1",
		"INSERT INTO boxer.facts SELECT * FROM fs(1, 2)",
		"DROP TABLE boxer.facts",
	} {
		_, err := runPathSet(t.Context(), nil, sqlConfig(ladingschema.Layout{}), sql, location{}, nil)
		require.Error(t, err, "accepted %q", sql)
		assert.Contains(t, err.Error(), "not provably read-only")
	}
}

func TestPathSetAnchorsRowsThatNameNoLocation(t *testing.T) {
	anchor := location{mount: identifier.TaggedId(0xF5F5000000000001), snap: time.Unix(0, 1_700_000_000_000_000_000)}
	acc := newPathSetAccumulator(normalizeLocation(anchor), nil)
	rec := pathOnlyBatch(t, "README.md", "src/main.go", "src/deep/leaf.txt")
	defer rec.Release()
	require.NoError(t, acc.add(rec))

	rs := acc.result()
	assert.Equal(t, 3, rs.rows)
	require.Len(t, rs.locs, 1)
	// One location means bare paths: no prefix segment anywhere.
	assert.Contains(t, rs.leaves, "src/main.go")
	leaf := rs.leaves["src/main.go"]
	assert.Equal(t, anchor.mount, leaf.loc.mount)
	assert.Equal(t, "src/main.go", leaf.real)
}

func TestPathSetPrefixesRowsFromSeveralSnapshots(t *testing.T) {
	m1 := identifier.TaggedId(0xF5F5000000000001)
	m2 := identifier.TaggedId(0xF5F5000000000002)
	ts := int64(1_700_000_000_000_000_000)
	acc := newPathSetAccumulator(location{}, func(id identifier.TaggedId) string {
		if id == m1 {
			return "corpus"
		}
		return "fleet"
	})
	rec := locatedBatch(t,
		[]string{"a.md", "b.md"},
		[]uint64{m1.Value(), m2.Value()},
		[]int64{ts, ts},
	)
	defer rec.Release()
	require.NoError(t, acc.add(rec))

	rs := acc.result()
	require.Len(t, rs.locs, 2)
	assert.Equal(t, 2, rs.rows)
	// Every leaf is under a location segment once the result spans more than
	// one snapshot — including the row that arrived before that was known.
	for virt, leaf := range rs.leaves {
		assert.Contains(t, virt, "@", "leaf %q carries no location segment", virt)
		assert.NotEqual(t, virt, leaf.real)
	}
	assert.Contains(t, rs.leaves, "corpus@2023-11-14T22:13:20Z/a.md")
	assert.Contains(t, rs.leaves, "fleet@2023-11-14T22:13:20Z/b.md")
}

func TestPathSetDropsRowsWithNoResolvableLocation(t *testing.T) {
	// No anchor and no mount column: the rows name files in no snapshot, so
	// there is nothing to open. They are dropped, not guessed at.
	acc := newPathSetAccumulator(location{}, nil)
	rec := pathOnlyBatch(t, "a.md", "b.md")
	defer rec.Release()
	require.NoError(t, acc.add(rec))
	rs := acc.result()
	assert.Zero(t, rs.rows)
	assert.Equal(t, 2, rs.dropped, "the pane says what it could not place")
	assert.Contains(t, rs.summary(""), "name no snapshot")
}

func TestPathSetRefusesABatchWithoutAPathColumn(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "size", Type: arrow.PrimitiveTypes.Uint64},
	}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Uint64Builder).Append(1)
	rec := rb.NewRecordBatch()
	defer rec.Release()

	acc := newPathSetAccumulator(location{}, nil)
	err := acc.add(rec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no `path` column")
}

func TestPathSetStopsAtTheCap(t *testing.T) {
	anchor := location{mount: identifier.TaggedId(0xF5F5000000000001), snap: time.Unix(0, 1)}
	acc := newPathSetAccumulator(normalizeLocation(anchor), nil)
	paths := make([]string, resultRowCap+10)
	for i := range paths {
		paths[i] = "f/" + itoaTest(i) + ".txt"
	}
	rec := pathOnlyBatch(t, paths...)
	defer rec.Release()
	require.NoError(t, acc.add(rec))
	assert.True(t, acc.full())

	rs := acc.result()
	assert.Equal(t, resultRowCap, rs.rows)
	assert.True(t, rs.truncated)
	assert.Contains(t, rs.summary("flagged"), "stopped at")
	assert.Contains(t, rs.summary(""), "query result")
}

// pathOnlyBatch builds a batch with just the required column.
func pathOnlyBatch(t *testing.T, paths ...string) arrow.RecordBatch {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "path", Type: arrow.BinaryTypes.String},
	}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	b := rb.Field(0).(*array.StringBuilder)
	for _, p := range paths {
		b.Append(p)
	}
	return rb.NewRecordBatch()
}

// locatedBatch builds a batch shaped like a query over `fs('*')`.
func locatedBatch(t *testing.T, paths []string, mounts []uint64, snaps []int64) arrow.RecordBatch {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "path", Type: arrow.BinaryTypes.String},
		{Name: "mount", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "snap", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	for i := range paths {
		rb.Field(0).(*array.StringBuilder).Append(paths[i])
		rb.Field(1).(*array.Uint64Builder).Append(mounts[i])
		rb.Field(2).(*array.Int64Builder).Append(snaps[i])
	}
	return rb.NewRecordBatch()
}

func itoaTest(v int) (s string) {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func TestNormalizeLocationMakesInstantsComparable(t *testing.T) {
	// The anchor comes from the mount list and a row's instant from Arrow;
	// without normalisation the two spellings of one instant are unequal map
	// keys and a single snapshot looks like two.
	ns := int64(1_700_000_000_123_456_789)
	fromList := location{mount: 7, snap: time.Unix(0, ns).In(time.FixedZone("CET", 3600))}
	fromArrow := location{mount: 7, snap: time.Unix(0, ns).UTC()}
	assert.NotEqual(t, fromList, fromArrow)
	assert.Equal(t, normalizeLocation(fromList), normalizeLocation(fromArrow))
	assert.True(t, normalizeLocation(location{mount: 7}).snap.IsZero(),
		"the zero instant means no snapshot and must not become the epoch")
}
