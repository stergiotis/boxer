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
// multi-statement SET requires multi_statements=1 which
// [play.Client.ExecuteArrowStream] does not currently plumb.
//
// play.Client.ExecuteArrowStream handles the FORMAT ArrowStream rewrite via
// its nanopass pipeline; callers of these builders must not append FORMAT
// themselves.

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
// of matching patterns. Uses the VectorScan / hyperscan backend.
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
