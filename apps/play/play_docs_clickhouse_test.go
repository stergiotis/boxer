package play

// ClickHouseDocsSource's own halves: the link pre-pass and the row decode —
// the ClickHouse-specific concerns DocsSourceI abstracts over. The pane-state
// machine that sits above any installed source (candidate walk, cache,
// debounce, followDocsLink) is exercised source-agnostically in
// play_docs_test.go.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stretchr/testify/require"
)

// testClickHouseDocsSource is a lane-less ClickHouseDocsSource — enough to
// exercise the link/decode methods below, none of which touch the lane.
func testClickHouseDocsSource() *ClickHouseDocsSource {
	return &ClickHouseDocsSource{SiteBase: defaultDocsSiteBase}
}

// The corpus links to ClickHouse's own site with root-relative targets. Left
// alone they render as hyperlinks that resolve to nothing.
func TestAbsolutiseDocLinks(t *testing.T) {
	src := testClickHouseDocsSource()
	cases := []struct{ in, want string }{
		{"see [DateTime](/sql-reference/data-types/datetime)",
			"see [DateTime](" + defaultDocsSiteBase + "/sql-reference/data-types/datetime)"},
		// Already absolute: untouched.
		{"[x](https://example.org/a)", "[x](https://example.org/a)"},
		// Protocol-relative is already absolute and must not gain a prefix.
		{"[x](//cdn.example.org/a)", "[x](//cdn.example.org/a)"},
		// An in-document anchor resolves within the page.
		{"[x](#syntax)", "[x](#syntax)"},
		// Several in one document, and a buffer with none at all.
		{"[a](/one) and [b](/two)",
			"[a](" + defaultDocsSiteBase + "/one) and [b](" + defaultDocsSiteBase + "/two)"},
		{"no links here", "no links here"},
		{"", ""},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, src.AbsolutiseLinks(tc.in), "input %q", tc.in)
	}
}

// An empty SiteBase disables the rewrite rather than prepending nothing
// usefully — a re-user whose table has no linked site should not see its
// bodies mangled.
func TestAbsolutiseDocLinksNoSiteBase(t *testing.T) {
	src := &ClickHouseDocsSource{}
	require.Equal(t, "see [DateTime](/sql-reference/data-types/datetime)",
		src.AbsolutiseLinks("see [DateTime](/sql-reference/data-types/datetime)"))
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

// Claiming is a syntactic test that runs per link per frame, so it must not
// depend on what happens to be cached — a link that changed shape as the
// reader scrolled would read as a glitch.
func TestDocsLinkClaimed(t *testing.T) {
	src := testClickHouseDocsSource()
	claimed := []string{
		"/sql-reference/data-types/datetime",
		"../data-types/int-uint.md",
		"./functions/date-time-functions",
		defaultDocsSiteBase + "/sql-reference/functions/date-time-functions#tohour",
	}
	for _, u := range claimed {
		require.True(t, src.LinkClaimed(u), "should stay in the pane: %s", u)
	}
	notClaimed := []string{
		"",
		"#syntax",                         // addresses this same page
		"https://github.com/ClickHouse/x", // genuinely elsewhere
		"http://example.org/",
		"mailto:a@b.c",
	}
	for _, u := range notClaimed {
		require.False(t, src.LinkClaimed(u), "should leave for a browser: %s", u)
	}
}

// The label leads, because it is what the author wrote to name the thing: a
// page covering a whole family is addressed by one URL and only the label
// says which member was meant.
func TestDocsLinkCandidatesPreferTheLabel(t *testing.T) {
	src := testClickHouseDocsSource()
	got := src.LinkCandidates("UInt8", "/sql-reference/data-types/int-uint")
	require.Equal(t, []string{"UInt8", "int-uint"}, got,
		"the label answers where the page name cannot")

	// A fragment names a section, which is usually the entity.
	got = src.LinkCandidates("date and time functions",
		"/sql-reference/functions/date-time-functions#tohour")
	require.Equal(t, []string{"tohour", "date-time-functions"}, got,
		"a multi-word label is not an entity name and is dropped")

	// Backticks are markup, not part of the name; `.md` is not either. The
	// segment then collapses into the label, because the dedup is
	// case-insensitive — which it must be, since the lookup is.
	require.Equal(t, []string{"DateTime"},
		src.LinkCandidates("`DateTime`", "../data-types/datetime.md"))

	// Label and fragment collapse when they agree, and the page they live on
	// stays as the last resort — `date-time` is not an entity, but a corpus
	// where it became one should still be reachable.
	require.Equal(t, []string{"toHour", "date-time"},
		src.LinkCandidates("toHour", "/sql-reference/functions/date-time#toHour"))
}

func TestDocsAbsoluteURL(t *testing.T) {
	src := testClickHouseDocsSource()
	require.Equal(t, defaultDocsSiteBase+"/sql-reference/x", src.AbsoluteURL("/sql-reference/x"))
	require.Equal(t, "https://example.org/a", src.AbsoluteURL("https://example.org/a"))
	require.Equal(t, defaultDocsSiteBase+"/data-types/x.md", src.AbsoluteURL("../data-types/x.md"))
	require.Equal(t, defaultDocsSiteBase+"/functions/y", src.AbsoluteURL("./functions/y"))
}

// ExplainError's specific "needs a newer server" copy is gated on Query
// actually naming system.documentation — a re-user who repurposed Query for
// their own table must not have THEIR unknown-table errors misreported as a
// ClickHouse version problem.
func TestExplainErrorScopesToOwnQuery(t *testing.T) {
	stock := &ClickHouseDocsSource{Query: defaultDocsQuery}
	stockErr := eh.Errorf("clickhouse http 404: Table system.documentation doesn't exist (UNKNOWN_TABLE)")
	require.Contains(t, stock.ExplainError(stockErr), "ClickHouse 26.x")

	repurposed := &ClickHouseDocsSource{Query: "SELECT term AS name, description FROM dspl.ontology_terms WHERE lower(term) = lower({n:String})"}
	repurposedErr := eh.Errorf("clickhouse http 404: Table dspl.ontology_terms doesn't exist (UNKNOWN_TABLE)")
	got := repurposed.ExplainError(repurposedErr)
	require.NotContains(t, got, "system.documentation")
	require.NotContains(t, got, "ClickHouse 26.x")
}
