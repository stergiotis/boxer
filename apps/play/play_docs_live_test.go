//go:build integration

package play

// Live checks for the Docs pane's lookup against a real server. What these
// pin is the half unit tests cannot: that `system.documentation` answers the
// query the pane ships, through the ordinary lane machinery, with the columns
// the decoder expects.

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// runDocsQuery ships the pane's own query for one name and decodes it exactly
// as the driver would.
func runDocsQuery(t *testing.T, name string) []docEntry {
	t.Helper()
	client := NewClient(ClientConfig{URL: liveClickHouseURL(t)}, nil)
	node := compiledNode{SQL: docsQuery, Params: map[string]string{"n": name}}
	rec, _, _, err := clientExecutor{client: client, opts: newExecOptions("docs")}.
		execute(context.Background(), node, memory.NewGoAllocator())
	require.NoError(t, err, "the documentation lookup must run on this endpoint")
	if rec != nil {
		defer rec.Release()
	}
	return decodeDocRows(rec)
}

// The query the pane ships must run, and its result must decode into the four
// fields the pane renders.
func TestLiveDocsLookupDecodes(t *testing.T) {
	got := runDocsQuery(t, "toHour")
	require.NotEmpty(t, got, "toHour must be documented")
	require.Equal(t, "toHour", got[0].Name)
	require.Equal(t, "Function", got[0].Kind)
	require.NotEmpty(t, got[0].Body, "the description is the whole point")
	require.Contains(t, got[0].Body, "toHour", "the body should mention its own name")
}

// The lookup is case-insensitive with an exact-spelling preference, because
// ClickHouse's own naming is inconsistent about case and a reader who typed
// one casing of a case-insensitive function should still get the page.
func TestLiveDocsLookupIsCaseInsensitive(t *testing.T) {
	got := runDocsQuery(t, "TOHOUR")
	require.NotEmpty(t, got)
	require.Equal(t, "toHour", got[0].Name, "the canonical spelling comes back")
}

// A name carrying several kinds returns all of them, which is what the pane's
// kind selector exists for. `Array` is both a data type and an
// aggregate-function combinator.
func TestLiveDocsLookupReturnsEveryKind(t *testing.T) {
	got := runDocsQuery(t, "Array")
	require.GreaterOrEqual(t, len(got), 2, "Array is documented under several kinds")
	kinds := make(map[string]bool, len(got))
	for _, e := range got {
		kinds[e.Kind] = true
	}
	require.True(t, kinds["Data Type"])
	require.True(t, kinds["Aggregate Function Combinator"])
}

// The pane must reach more than functions: the caret lands on data types,
// table engines, formats and settings too, and the walk offers all of them.
func TestLiveDocsLookupCoversTheOtherKinds(t *testing.T) {
	for name, wantKind := range map[string]string{
		"MergeTree":     "Table Engine",
		"UInt8":         "Data Type",
		"JSONEachRow":   "Format",
		"numbers":       "Table Function",
		"max_threads":   "Setting",
		"documentation": "System Table",
	} {
		t.Run(name, func(t *testing.T) {
			got := runDocsQuery(t, name)
			require.NotEmpty(t, got, "%s must be documented", name)
			var kinds []string
			for _, e := range got {
				kinds = append(kinds, e.Kind)
			}
			require.Contains(t, kinds, wantKind)
		})
	}
}

// A name nobody documents comes back empty rather than failing — the pane
// distinguishes "no page" from "the lookup broke", and the difference has to
// survive the round trip.
func TestLiveDocsLookupUnknownNameIsEmptyNotAnError(t *testing.T) {
	require.Empty(t, runDocsQuery(t, "no_such_entity_anywhere_xyzzy"))
}

// The bodies really are Markdown with fenced SQL examples, which is what the
// pane's Insert buttons hang off.
func TestLiveDocsBodiesCarryFencedSqlExamples(t *testing.T) {
	got := runDocsQuery(t, "toHour")
	require.NotEmpty(t, got)
	doc := got[0].rendered()
	require.NotNil(t, doc, "the body must parse as markdown")
	require.Contains(t, got[0].Body, "```sql", "examples are fenced as sql")
}

// End to end through the driver: the debounce holds the query back until the
// name settles, and the answer lands in the cache.
func TestLiveDocsDriverDebouncesThenCaches(t *testing.T) {
	client := NewClient(ClientConfig{URL: liveClickHouseURL(t)}, nil)
	d := newDocsDriver(client)
	defer d.close()
	clock := &docsClock{t: time.Unix(1000, 0)}
	d.now = clock.now

	// First sight of the name only arms it.
	res, loading := d.lookup("toHour")
	require.Nil(t, res)
	require.False(t, loading, "arming is not loading")

	// Still inside the window: nothing ships.
	clock.advance(docsQuiescence / 2)
	res, _ = d.lookup("toHour")
	require.Nil(t, res)

	// Past it: the query goes out, and the lane answers within the timeout.
	clock.advance(docsQuiescence)
	deadline := time.Now().Add(docsProbeTimeout)
	for res == nil && time.Now().Before(deadline) {
		res, _ = d.lookup("toHour")
		if res == nil {
			time.Sleep(10 * time.Millisecond)
		}
	}
	require.NotNil(t, res, "the lookup must complete")
	require.NoError(t, res.err)
	require.NotEmpty(t, res.entries)

	// And it is cached: a second lookup answers without the lane.
	require.NotNil(t, d.cached("toHour"))
	require.NotNil(t, d.cached("TOHOUR"), "the cache key is case-folded")
}
