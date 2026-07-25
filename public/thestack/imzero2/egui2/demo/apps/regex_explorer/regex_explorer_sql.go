package regex_explorer

// SQL construction for the regex explorer.
//
// Parameter binding: user-supplied pattern / haystack / replacement strings
// are inlined into the SQL via boxer's [marshalling.EscapeString], which
// produces a single-quoted ClickHouse literal with ClickHouse-specific
// escaping (single-quote, backslash, \n, \t, \r, \0). This is the
// fallback-chain path codified in ADR-0054 SD2: the originally-proposed
// SETTINGS-clause binding does not work (ClickHouse's SETTINGS is for
// query-level server settings, not parameter substitution), and
// multi-statement SET buys nothing given this app's one-SELECT-per-dispatch
// shape.
//
// The output format is requested out of band — the broker is asked for
// ArrowStream when the query is published (see regex_explorer_chlocal.go),
// so callers of these builders must not append a FORMAT clause themselves.

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
)

// buildMatchSQL returns a SELECT querying ClickHouse's match(haystack, pattern)
// with both arguments escaped as SQL literals. Returns UInt8 (0 or 1).
func buildMatchSQL(haystack string, pattern string) (sql string) {
	sql = "SELECT match(" + marshalling.EscapeString(haystack) + ", " + marshalling.EscapeString(pattern) + ")"
	return
}

// buildExtractAllSQL returns a SELECT querying ClickHouse's
// extractAll(haystack, pattern) with both arguments escaped as SQL
// literals. Returns Array(String) — one match text per element.
func buildExtractAllSQL(haystack string, pattern string) (sql string) {
	sql = "SELECT extractAll(" + marshalling.EscapeString(haystack) + ", " + marshalling.EscapeString(pattern) + ")"
	return
}

// buildExtractAllGroupsSQL returns a SELECT querying ClickHouse's
// extractAllGroups(haystack, pattern). Returns Array(Array(String)) — one
// inner array of capture-group values per match, without the full match.
//
// ClickHouse rejects a pattern with no capture group ("There are no groups
// in regexp", BAD_ARGUMENTS), so callers must confirm the pattern has at
// least one before building this.
func buildExtractAllGroupsSQL(haystack string, pattern string) (sql string) {
	sql = "SELECT extractAllGroups(" + marshalling.EscapeString(haystack) + ", " + marshalling.EscapeString(pattern) + ")"
	return
}

// buildReplaceAllSQL returns a SELECT querying ClickHouse's
// replaceRegexpAll(haystack, pattern, replacement) with all three
// arguments escaped as SQL literals. Returns String — the haystack with
// every match replaced.
func buildReplaceAllSQL(haystack string, pattern string, replacement string) (sql string) {
	sql = "SELECT replaceRegexpAll(" +
		marshalling.EscapeString(haystack) + ", " +
		marshalling.EscapeString(pattern) + ", " +
		marshalling.EscapeString(replacement) + ")"
	return
}

// buildMultiMatchSQL returns a SELECT querying ClickHouse's
// multiMatchAllIndices(haystack, [p1, p2, ...]) with each pattern escaped
// as an individual SQL literal. Returns Array(UInt64) — 1-based indices
// of matching patterns, unsorted. Uses the VectorScan / hyperscan backend.
//
// patterns must be non-empty: `multiMatchAllIndices(h, [])` types the
// array as Array(Nothing) and ClickHouse rejects it with
// ILLEGAL_TYPE_OF_ARGUMENT. Callers filter the empty case out before
// getting here (see [App.reconcileMulti]).
func buildMultiMatchSQL(haystack string, patterns []string) (sql string) {
	var b strings.Builder
	b.WriteString("SELECT multiMatchAllIndices(")
	b.WriteString(marshalling.EscapeString(haystack))
	b.WriteString(", [")
	for i, p := range patterns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(marshalling.EscapeString(p))
	}
	b.WriteString("])")
	sql = b.String()
	return
}
