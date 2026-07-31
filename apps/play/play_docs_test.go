package play

// The Docs pane's pure halves: the candidate walk, the link pre-pass, the row
// decode, and the cache/debounce behaviour that keeps the caret from becoming
// a query generator.

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stretchr/testify/require"
)

func TestDocsCandidatesOrder(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		off  int
		want []string
	}{
		{"name then enclosing", "SELECT concat(toString(x))", 22, []string{"toString", "concat"}},
		{"enclosing only", "SELECT concat(x, )", 17, []string{"concat"}},
		{"name only", "SELECT toHour(x)", 9, []string{"toHour"}},
		// A call inside itself must not be asked about twice.
		{"deduped", "SELECT f(f(|))", 11, []string{"f"}},
		{"nothing", "SELECT 123", 8, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, ok := highlight.EntityAtIn(tc.sql, tc.off)
			require.Equal(t, tc.want, docsCandidates(e, ok))
		})
	}
}

// The corpus links to ClickHouse's own site with root-relative targets. Left
// alone they render as hyperlinks that resolve to nothing.
func TestAbsolutiseDocLinks(t *testing.T) {
	cases := []struct{ in, want string }{
		{"see [DateTime](/sql-reference/data-types/datetime)",
			"see [DateTime](" + docsSiteBase + "/sql-reference/data-types/datetime)"},
		// Already absolute: untouched.
		{"[x](https://example.org/a)", "[x](https://example.org/a)"},
		// Protocol-relative is already absolute and must not gain a prefix.
		{"[x](//cdn.example.org/a)", "[x](//cdn.example.org/a)"},
		// An in-document anchor resolves within the page.
		{"[x](#syntax)", "[x](#syntax)"},
		// Several in one document, and a buffer with none at all.
		{"[a](/one) and [b](/two)",
			"[a](" + docsSiteBase + "/one) and [b](" + docsSiteBase + "/two)"},
		{"no links here", "no links here"},
		{"", ""},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, absolutiseDocLinks(tc.in), "input %q", tc.in)
	}
}

// docRecord builds a record shaped like the lookup's result set.
func docRecord(t *testing.T, names, kinds, bodies, sources []string) arrow.RecordBatch {
	t.Helper()
	alloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "type", Type: arrow.BinaryTypes.String},
		{Name: "description", Type: arrow.BinaryTypes.String},
		{Name: "source", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(alloc, schema)
	defer b.Release()
	for i, col := range [][]string{names, kinds, bodies, sources} {
		b.Field(i).(*array.StringBuilder).AppendValues(col, nil)
	}
	return b.NewRecordBatch()
}

func TestDecodeDocRows(t *testing.T) {
	rec := docRecord(t,
		[]string{"Array", "Array"},
		[]string{"Data Type", "Aggregate Function Combinator"},
		[]string{"the type", "the combinator"},
		[]string{"src/DataTypes/DataTypeArray.cpp", ""})
	defer rec.Release()

	got := decodeDocRows(rec)
	require.Len(t, got, 2)
	require.Equal(t, "Array", got[0].Name)
	require.Equal(t, "Data Type", got[0].Kind)
	require.Equal(t, "the type", got[0].Body)
	require.Equal(t, "src/DataTypes/DataTypeArray.cpp", got[0].Source)
	require.Empty(t, got[1].Source, "an unknown source is empty, not missing")

	require.Empty(t, decodeDocRows(nil), "no record decodes to nothing")
}

// A column the server did not send reads as empty rather than panicking: the
// pane must degrade to a blank field, not take the tab down.
func TestDecodeDocRowsToleratesAMissingColumn(t *testing.T) {
	alloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(alloc, schema)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"toHour"}, nil)
	rec := b.NewRecordBatch()
	defer rec.Release()

	got := decodeDocRows(rec)
	require.Len(t, got, 1)
	require.Equal(t, "toHour", got[0].Name)
	require.Empty(t, got[0].Body)
}

// --- cache and debounce ---

type docsClock struct{ t time.Time }

func (inst *docsClock) now() time.Time          { return inst.t }
func (inst *docsClock) advance(d time.Duration) { inst.t = inst.t.Add(d) }

// newDocsDriver with no client leaves the lane nil, which is the shape a
// client-less session has; the cache half still works, which is what these
// exercise.
func testDocsDriver() (*docsDriver, *docsClock) {
	clock := &docsClock{t: time.Unix(1000, 0)}
	d := newDocsDriver(nil)
	d.now = clock.now
	return d, clock
}

func TestDocsCacheServesWithoutTheLane(t *testing.T) {
	d, _ := testDocsDriver()
	want := &docsResult{entries: []docEntry{{Name: "toHour", Kind: "Function"}}}
	d.store("tohour", want)

	// Case-insensitively: ClickHouse's own naming is inconsistent, so the
	// cache key is the lowered name.
	got, loading := d.lookup("toHour")
	require.Same(t, want, got)
	require.False(t, loading)
	got, _ = d.lookup("TOHOUR")
	require.Same(t, want, got)
}

// The cache is bounded, and eviction is by least-recent USE rather than by
// insertion: a name the reader keeps coming back to must not be evicted by a
// run of one-off lookups.
func TestDocsCacheEvictsLeastRecentlyUsed(t *testing.T) {
	d, _ := testDocsDriver()
	for i := 0; i < docsCacheMax; i++ {
		d.store(string(rune('a'+i)), &docsResult{})
	}
	require.Len(t, d.cache, docsCacheMax)

	// Touch the oldest, then overflow by one: the SECOND oldest goes.
	require.NotNil(t, d.cached("a"))
	d.store("overflow", &docsResult{})
	require.Len(t, d.cache, docsCacheMax)
	require.NotNil(t, d.cached("a"), "a recently used entry survives")
	require.Nil(t, d.cached("b"), "the least recently used one is evicted")
}

// cached must never arm the debounce: the candidate walk screens with it, and
// screening several names per frame would otherwise restart the timer of the
// one actually being pursued.
func TestDocsCachedDoesNotArmTheDebounce(t *testing.T) {
	d, _ := testDocsDriver()
	d.armed, d.armedAt = "toHour", time.Unix(500, 0)
	require.Nil(t, d.cached("somethingElse"))
	require.Equal(t, "toHour", d.armed, "screening must not disturb the pursued name")
	require.Equal(t, time.Unix(500, 0), d.armedAt)
}

// --- the pane's resolution walk ---

// docsApp is a PlayApp with a pre-populated documentation cache and no lane,
// so the walk can be exercised without a server.
func docsApp(entries map[string]*docsResult) *PlayApp {
	app := &PlayApp{docs: newDocsDriver(nil), docsPane: newDocsPaneState()}
	for k, v := range entries {
		app.docs.store(k, v)
	}
	return app
}

func withDoc(name, kind string) *docsResult {
	return &docsResult{entries: []docEntry{{Name: name, Kind: kind, Body: "body"}}}
}

// The walk prefers the name under the caret and falls through to the calls
// enclosing it, which is what makes the pane answer mid-expression.
func TestResolveDocsFallsThroughToTheEnclosingCall(t *testing.T) {
	app := docsApp(map[string]*docsResult{
		"mycol":  {}, // a column: cached, no documentation
		"tohour": withDoc("toHour", "Function"),
	})
	res := app.resolveDocs(candsAt("SELECT toHour(myCol)", len("SELECT toHour(myC")))
	require.NotNil(t, res)
	require.Equal(t, "toHour", app.docsPane.shown,
		"the column has no page, so the call it belongs to answers")
	require.Equal(t, "toHour", res.entries[0].Name)
}

// A caret crossing something undocumented must not blank the pane — that
// flicker on every keyword between two names is what the held state prevents.
func TestResolveDocsKeepsThePageOnAMiss(t *testing.T) {
	app := docsApp(map[string]*docsResult{
		"tohour": withDoc("toHour", "Function"),
		"select": {}, // a keyword: cached, no documentation
	})
	require.NotNil(t, app.resolveDocs(candsAt("SELECT toHour(x)", 9)))
	require.Equal(t, "toHour", app.docsPane.shown)

	// Caret onto the keyword: still showing toHour, and saying why.
	res := app.resolveDocs(candsAt("SELECT toHour(x)", 2))
	require.NotNil(t, res)
	require.Equal(t, "toHour", app.docsPane.shown, "the page stays up")
	require.Equal(t, "SELECT", app.docsPane.lastMiss, "…and names what came up empty")
}

// The manual box is an explicit act and must not be fought by the caret moving
// underneath it.
func TestResolveDocsManualOverridesTheCaret(t *testing.T) {
	app := docsApp(map[string]*docsResult{
		"tohour":    withDoc("toHour", "Function"),
		"mergetree": withDoc("MergeTree", "Table Engine"),
	})
	app.docsPane.manual = "MergeTree"
	res := app.resolveDocs(candsAt("SELECT toHour(x)", 9))
	require.NotNil(t, res)
	require.Equal(t, "MergeTree", app.docsPane.shown)
	require.Equal(t, "MergeTree", res.entries[0].Name)
}

// With Follow off the pane is pinned: the caret may roam without changing it.
func TestResolveDocsFollowOffPinsThePage(t *testing.T) {
	app := docsApp(map[string]*docsResult{
		"tohour":    withDoc("toHour", "Function"),
		"mergetree": withDoc("MergeTree", "Table Engine"),
	})
	app.docsPane.follow = false
	app.docsPane.shown = "MergeTree"
	res := app.resolveDocs(candsAt("SELECT toHour(x)", 9))
	require.NotNil(t, res)
	require.Equal(t, "MergeTree", app.docsPane.shown)
}

// A lookup that failed is a result, not a miss: the pane reports the failure
// rather than walking outward as if the name were undocumented.
func TestResolveDocsSurfacesAFailure(t *testing.T) {
	app := docsApp(map[string]*docsResult{
		"tohour": {err: eh.Errorf("clickhouse http 400: UNKNOWN_TABLE")},
		"concat": withDoc("concat", "Function"),
	})
	res := app.resolveDocs(candsAt("SELECT concat(toHour(x))", 17))
	require.NotNil(t, res)
	require.Error(t, res.err)
	require.Equal(t, "toHour", app.docsPane.shown,
		"a failed lookup is reported, not stepped over")
}

// candsAt is what the editor would publish for a caret at `off` in `sql`,
// which is exactly what renderDocsTab hands the walk.
func candsAt(sql string, off int) []string {
	return docsCandidates(highlight.EntityAtIn(sql, off))
}
