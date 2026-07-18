package highlight

// HighlightLex performs lexical-only highlighting: the phase-1 token
// classification of [Highlight] plus a one-token lookahead that promotes an
// identifier to CatFunctionName when its next significant token is an opening
// parenthesis — the lexer alone cannot tell a function name from a plain
// identifier (the same peek-ahead ClickHouse's play.html applies).
//
// It never parses, so the cost stays per-keystroke affordable (measured
// ~26 µs for a 180 B statement, ~280 µs for 2.5 KB; roughly linear). This is
// the span source for the ADR-0130 editor path. Spans cover the input
// contiguously in source order, whitespace and comments included; semantic
// categories (table/column/alias/CTE names) are out of reach here and remain
// [Highlight]'s job.
func HighlightLex(sql string) (spans []Span) {
	spans = lexHighlight(sql)
	refineFunctionCalls(spans)
	return
}

// refineFunctionCalls promotes each CatIdentifier span whose next
// significant (non-whitespace, non-comment) span is `(` to CatFunctionName.
// Keywords are left alone: `count` lexes as an identifier and is promoted,
// `CAST(` stays a keyword.
func refineFunctionCalls(spans []Span) {
	for i := range spans {
		if spans[i].Category != CatIdentifier {
			continue
		}
		for j := i + 1; j < len(spans); j++ {
			cat := spans[j].Category
			if cat == CatWhitespace || cat == CatComment {
				continue
			}
			if spans[j].Text == "(" {
				spans[i].Category = CatFunctionName
			}
			break
		}
	}
}
