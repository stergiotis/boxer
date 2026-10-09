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

// chFnE names one ClickHouse function the Functions tab evaluates. The
// order is the order of the tab's rows and of the result columns.
type chFnE uint8

const (
	fnMatch chFnE = iota
	fnCountMatches
	fnExtract
	fnRegexpExtract
	fnExtractAll
	// fnExtractAllGroups is evaluated only when the pattern captures:
	// ClickHouse rejects it otherwise ("There are no groups in regexp").
	fnExtractAllGroups
	fnReplaceRegexpOne
	fnReplaceRegexpAll
)

// patternFns are the functions of the pattern alone, evaluated in one
// query; replaceFns also read the replacement and run in their own, so a
// replacement ClickHouse rejects (`\9` with one group) fails only them.
var (
	patternFns = []chFnE{fnMatch, fnCountMatches, fnExtract, fnRegexpExtract, fnExtractAll, fnExtractAllGroups}
	replaceFns = []chFnE{fnReplaceRegexpOne, fnReplaceRegexpAll}
)

// name returns the function's name as ClickHouse spells it, with the
// argument a row adds to it.
func (inst chFnE) name() (name string) {
	switch inst {
	case fnMatch:
		name = "match"
	case fnCountMatches:
		name = "countMatches"
	case fnExtract:
		name = "extract"
	case fnRegexpExtract:
		name = "regexpExtract(…, 0)"
	case fnExtractAll:
		name = "extractAll"
	case fnExtractAllGroups:
		name = "extractAllGroups"
	case fnReplaceRegexpOne:
		name = "replaceRegexpOne"
	case fnReplaceRegexpAll:
		name = "replaceRegexpAll"
	}
	return
}

// expr returns the function's SQL over a column named haystack, with the
// pattern (and replacement) as literals. It is both what runs — the
// queries bind the haystack under that name — and what the tab's copy
// action hands the user, so the two cannot drift apart.
func (inst chFnE) expr(pattern string, replacement string) (sql string) {
	p := marshalling.EscapeString(pattern)
	switch inst {
	case fnMatch:
		sql = "match(haystack, " + p + ")"
	case fnCountMatches:
		sql = "countMatches(haystack, " + p + ")"
	case fnExtract:
		sql = "extract(haystack, " + p + ")"
	case fnRegexpExtract:
		// Index 0, the whole match: valid for every pattern, where the
		// default index 1 is out of range for one without a group.
		sql = "regexpExtract(haystack, " + p + ", 0)"
	case fnExtractAll:
		sql = "extractAll(haystack, " + p + ")"
	case fnExtractAllGroups:
		sql = "extractAllGroups(haystack, " + p + ")"
	case fnReplaceRegexpOne:
		sql = "replaceRegexpOne(haystack, " + p + ", " + marshalling.EscapeString(replacement) + ")"
	case fnReplaceRegexpAll:
		sql = "replaceRegexpAll(haystack, " + p + ", " + marshalling.EscapeString(replacement) + ")"
	}
	return
}

// buildFnsSQL returns one SELECT evaluating fns over haystack, one column
// per function in order. The haystack is bound once, as a WITH alias;
// ClickHouse folds it, so the regex functions still see constant
// arguments where they require them.
func buildFnsSQL(haystack string, pattern string, replacement string, fns []chFnE) (sql string) {
	var b strings.Builder
	b.WriteString("WITH ")
	b.WriteString(marshalling.EscapeString(haystack))
	b.WriteString(" AS haystack SELECT ")
	for i, f := range fns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(f.expr(pattern, replacement))
	}
	sql = b.String()
	return
}

// buildExtractAllSQL returns a SELECT querying ClickHouse's
// extractAll(haystack, pattern) — Array(String). The SD1 tripwire's query.
func buildExtractAllSQL(haystack string, pattern string) (sql string) {
	sql = "SELECT extractAll(" + marshalling.EscapeString(haystack) + ", " + marshalling.EscapeString(pattern) + ")"
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
