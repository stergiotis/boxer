package play

// The Docs pane's pure halves: the candidate walk, the link pre-pass, the row
// decode, and the cache/debounce behaviour that keeps the caret from becoming
// a query generator.

import (
	"testing"
	"time"

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

// --- cache and debounce ---

type docsClock struct{ t time.Time }

func (inst *docsClock) now() time.Time          { return inst.t }
func (inst *docsClock) advance(d time.Duration) { inst.t = inst.t.Add(d) }

// newDocsDriver with no source leaves lookups unavailable, which is the shape
// a client-less session has; the cache half still works, which is what these
// exercise.
func testDocsDriver() (*docsDriver, *docsClock) {
	clock := &docsClock{t: time.Unix(1000, 0)}
	d := newDocsDriver(nil)
	d.now = clock.now
	return d, clock
}

func TestDocsCacheServesWithoutTheLane(t *testing.T) {
	d, _ := testDocsDriver()
	want := &docsResult{entries: []docEntry{{DocsEntry: DocsEntry{Name: "toHour", Kind: "Function"}}}}
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

// docsApp is a PlayApp with a pre-populated documentation cache and a
// lane-less ClickHouseDocsSource, so the walk can be exercised without a
// server. The lane-less source is safe here: every candidate these tests walk
// is pre-seeded via store, so lookup always hits the cache and never reaches
// DocsSourceI.Lookup — it is wired only so followDocsLink has a real
// LinkCandidates to call.
func docsApp(entries map[string]*docsResult) *PlayApp {
	app := &PlayApp{docs: newDocsDriver(&ClickHouseDocsSource{SiteBase: defaultDocsSiteBase}), docsPane: newDocsPaneState()}
	for k, v := range entries {
		app.docs.store(k, v)
	}
	return app
}

func withDoc(name, kind string) *docsResult {
	return &docsResult{entries: []docEntry{{DocsEntry: DocsEntry{Name: name, Kind: kind, Body: "body"}}}}
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

// --- link routing (ADR-0147's caret seam's sibling: a link is the other way
// a reader says "tell me about this") ---
//
// The claim/candidate/absolute-URL heuristics themselves are
// ClickHouseDocsSource's own concern — see play_docs_clickhouse_test.go —
// these exercise the pane-state machine (followDocsLink/resolveDocs) that
// sits above whatever DocsSourceI is installed.

// Following a link pins the pane: the caret has not moved, so leaving Follow
// on would snap it back on the next frame and the click would look inert.
func TestFollowDocsLinkPinsAndRecordsHistory(t *testing.T) {
	app := docsApp(map[string]*docsResult{
		"tohour": withDoc("toHour", "Function"),
		"uint8":  withDoc("UInt8", "Data Type"),
	})
	app.docsPane.shown = "toHour"

	app.followDocsLink("UInt8", "/sql-reference/data-types/int-uint")
	require.False(t, app.docsPane.follow, "a followed link pins the pane")
	require.Equal(t, []string{"toHour"}, app.docsPane.back, "…and is undoable")
	require.Equal(t, []string{"UInt8", "int-uint"}, app.docsPane.nav)

	res := app.resolveDocs(nil)
	require.NotNil(t, res)
	require.Equal(t, "UInt8", app.docsPane.shown)
	require.Empty(t, app.docsPane.nav, "a resolved navigation is consumed")
	require.Empty(t, app.docsPane.navURL, "…and needs no browser fallback")
}

// A link into something this server does not document keeps its URL, so the
// page it named is still reachable rather than a dead end.
func TestFollowDocsLinkKeepsTheURLOnAMiss(t *testing.T) {
	app := docsApp(map[string]*docsResult{
		"nosuchthing": {}, // cached, no documentation
		"weird-page":  {},
	})
	app.followDocsLink("nosuchthing", "/operations/weird-page")
	res := app.resolveDocs(nil)
	require.NotNil(t, res)
	require.Empty(t, res.entries)
	require.Equal(t, "/operations/weird-page", app.docsPane.navURL,
		"the browser escape hatch survives a miss")
}

// The lookup box outranks everything, so following a link has to clear it —
// otherwise the click resolves and is undone on the same frame by the stale
// name still sitting in the box.
func TestFollowDocsLinkClearsTheLookupBox(t *testing.T) {
	app := docsApp(map[string]*docsResult{
		"tohour": withDoc("toHour", "Function"),
		"uint8":  withDoc("UInt8", "Data Type"),
	})
	app.docsPane.manual = "toHour"
	app.docsPane.shown = "toHour"

	app.followDocsLink("UInt8", "/sql-reference/data-types/int-uint")
	require.Empty(t, app.docsPane.manual, "the box no longer describes what is shown")

	res := app.resolveDocs(nil)
	require.NotNil(t, res)
	require.Equal(t, "UInt8", app.docsPane.shown, "the navigation survives the frame")
}
