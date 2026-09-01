package distsql

import (
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ExpandDescriptiveStatistics is the ADR-0161 §SD3 macro pass: it rewrites
//
//	SELECT descriptiveStatistics(['<estimator>',] col1 [, col2 …])
//	FROM …  [WHERE …]  [GROUP BY k1 …]  [SETTINGS …]
//
// into one UNION ALL branch per argument column over the original
// FROM/WHERE/GROUP BY, each branch emitting the §SD1 result contract
// (series/n/ps/qs + moments + estimator provenance) over the fixed §SD2
// probability grid. The call must be the sole select item; misuse errors
// at expansion time so a malformed macro never reaches the server (the
// LW_ID precedent). A statement without the macro passes through
// unchanged, which is also what makes the pass idempotent: expansion
// output contains no macro call.
//
// Deliberately not carried into v1 branches, each a loud error instead:
// ORDER BY / LIMIT / LIMIT BY / HAVING / QUALIFY (no honest home across a
// UNION ALL of per-column branches), positional or CUBE/ROLLUP GROUP BY
// (positions refer to the pre-expansion select list). Histogram columns
// are M2. The expansion output is machine-generated and deliberately not
// re-canonicalised (the existing macro-family consequence).
var ExpandDescriptiveStatistics = nanopass.LiftBodyPass("ExpandDescriptiveStatistics",
	func(sql string) (result string, err error) {
		return expandDescriptiveStatistics(sql, GridLevels())
	},
	nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	})

// FuncName is the macro's canonical spelling — what a user types and what a
// vocabulary listing shows (ADR-0174 §SD3). Matching is case- and
// quoting-insensitive, so this is the preferred spelling, not the only
// accepted one. Named to match docsearchsql.FuncName and keelsonsql.FuncName.
const FuncName = "descriptiveStatistics"

// macroName is the registry key: FuncName normalised, which is what
// NormalizeCallName produces for every accepted spelling.
var macroName = nanopass.NormalizeCallName(FuncName)

// estimator maps the optional leading string token to the quantiles call
// and the provenance token the panel displays.
type estimatorSpec struct {
	call  string // e.g. "quantilesTDigest(" — levels (and any accuracy arg) follow
	extra string // leading accuracy argument incl. trailing comma, or ""
	token string // §SD1 estimator provenance value
}

var estimators = map[string]estimatorSpec{
	"tdigest": {call: "quantilesTDigest", token: "tdigest"},
	"exact":   {call: "quantilesExactInclusive", token: "exact-hf7"},
	"gk":      {call: "quantilesGK", extra: "1000,", token: "gk:1000"},
	"dd":      {call: "quantilesDD", extra: "0.01,", token: "dd:0.01"},
}

const defaultEstimator = "tdigest"

// seriesNullGlyph stands in for a NULL group-key value in the folded
// series label.
const seriesNullGlyph = "∅"

func expandDescriptiveStatistics(sql string, levels []float64) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eb.Build().Errorf("ExpandDescriptiveStatistics: %w", err)
		return
	}

	// Collect every occurrence first: zero → fast pass-through (byte-identical),
	// more than one → reject before any placement question arises.
	var calls []*grammar1.ColumnExprFunctionContext
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		// Keep descending inside a match: a nested occurrence in the
		// argument list must be seen to be rejected.
		if fn, ok := ctx.(*grammar1.ColumnExprFunctionContext); ok {
			if ident := fn.Identifier(); ident != nil && nanopass.NormalizeCallName(ident.GetText()) == macroName {
				calls = append(calls, fn)
			}
		}
		return true
	})
	if len(calls) == 0 {
		result = sql
		return
	}
	if len(calls) > 1 {
		err = eb.Build().Int("occurrences", len(calls)).Errorf("descriptiveStatistics: exactly one call per statement (nesting it in its own arguments has no meaning)")
		return
	}
	call := calls[0]
	if len(call.AllLPAREN()) > 1 {
		err = eb.Build().Errorf("descriptiveStatistics: the parametric spelling f('…')(…) is not supported — pass the estimator as the first argument")
		return
	}

	stmt, err := soleTopLevelSelect(pr, call)
	if err != nil {
		return
	}
	err = rejectUnsupportedClauses(pr, stmt)
	if err != nil {
		return
	}
	keys, err := groupByKeyTexts(pr, stmt)
	if err != nil {
		return
	}
	est, cols, err := macroArgs(pr, call)
	if err != nil {
		return
	}

	callRange := pr.SourceRangeOf(call)
	prefix := sql[:callRange.Start]
	tail := sql[callRange.End:]
	branches := make([]string, 0, len(cols))
	for _, col := range cols {
		branches = append(branches, prefix+branchColumns(col, keys, est, levels)+tail)
	}
	result = strings.Join(branches, " UNION ALL ")
	return
}

// soleTopLevelSelect checks the call sits as the sole, unaliased select item
// of the statement's single top-level SELECT and returns that statement.
func soleTopLevelSelect(pr *nanopass.ParseResult, call *grammar1.ColumnExprFunctionContext) (stmt *grammar1.SelectStmtContext, err error) {
	roots, err := nanopass.BuildScopes(pr, "")
	if err != nil {
		err = eb.Build().Errorf("descriptiveStatistics: %w", err)
		return
	}
	if len(roots) != 1 {
		err = eb.Build().Int("members", len(roots)).Errorf("descriptiveStatistics: not inside a UNION — expansion produces its own UNION ALL")
		return
	}
	stmt = roots[0].Node

	// The nearest enclosing SELECT of the call must be the top-level one.
	for p := antlr.Tree(call).GetParent(); p != nil; p = p.GetParent() {
		enclosing, isSelect := p.(*grammar1.SelectStmtContext)
		if !isSelect {
			continue
		}
		if enclosing != stmt {
			err = eb.Build().Errorf("descriptiveStatistics: must be the top-level select item, not inside a subquery or CTE body")
		}
		break
	}
	if err != nil {
		return
	}

	pc, ok := stmt.ProjectionClause().(*grammar1.ProjectionClauseContext)
	if !ok || pc == nil {
		err = eb.Build().Errorf("descriptiveStatistics: statement has no projection clause")
		return
	}
	cel, ok := pc.ColumnExprList().(*grammar1.ColumnExprListContext)
	if !ok || cel == nil {
		err = eb.Build().Errorf("descriptiveStatistics: statement has no select list")
		return
	}
	items := cel.AllColumnsExpr()
	if len(items) != 1 {
		err = eb.Build().Int("items", len(items)).Errorf("descriptiveStatistics: must be the sole select item — a merged output shape with other expressions does not exist (ADR-0161 §SD3)")
		return
	}
	itemText := strings.TrimSpace(nanopass.NodeText(pr, items[0].(antlr.ParserRuleContext)))
	callText := strings.TrimSpace(nanopass.NodeText(pr, call))
	if itemText != callText {
		err = eb.Build().Str("item", itemText).Errorf("descriptiveStatistics: must stand alone — not aliased, negated, or wrapped in another expression")
		return
	}
	return
}

// rejectUnsupportedClauses names each clause v1 refuses to carry into the
// per-column UNION ALL branches.
func rejectUnsupportedClauses(pr *nanopass.ParseResult, stmt *grammar1.SelectStmtContext) (err error) {
	for _, c := range []struct {
		name string
		node antlr.ParserRuleContext
	}{
		{"ORDER BY", asRuleCtx(stmt.OrderByClause())},
		{"LIMIT", asRuleCtx(stmt.LimitClause())},
		{"LIMIT BY", asRuleCtx(stmt.LimitByClause())},
		{"HAVING", asRuleCtx(stmt.HavingClause())},
		{"QUALIFY", asRuleCtx(stmt.QualifyClause())},
	} {
		if c.node != nil {
			err = eb.Build().Str("clause", c.name).Errorf("descriptiveStatistics: this clause has no honest home across the expansion's UNION ALL branches — drop it (v1)")
			return
		}
	}
	_ = pr
	return
}

// asRuleCtx normalises a possibly-nil clause interface to a nillable
// ParserRuleContext (a typed-nil interface value must read as absent).
func asRuleCtx(v any) antlr.ParserRuleContext {
	if v == nil {
		return nil
	}
	ctx, ok := v.(antlr.ParserRuleContext)
	if !ok {
		return nil
	}
	return ctx
}

// groupByKeyTexts returns the GROUP BY expressions' verbatim source texts.
// Positional keys and CUBE/ROLLUP/GROUPING SETS are rejected: positions
// refer to the pre-expansion select list, and the special forms produce
// rows the series folding cannot label.
func groupByKeyTexts(pr *nanopass.ParseResult, stmt *grammar1.SelectStmtContext) (keys []string, err error) {
	gbAny := stmt.GroupByClause()
	if gbAny == nil {
		return
	}
	gb, ok := gbAny.(*grammar1.GroupByClauseContext)
	if !ok || gb == nil {
		return
	}
	upper := strings.ToUpper(nanopass.NodeText(pr, gb))
	for _, special := range []string{"CUBE", "ROLLUP", "GROUPING"} {
		if strings.Contains(upper, special) {
			err = eb.Build().Str("form", special).Errorf("descriptiveStatistics: GROUP BY is not supported (v1) — the series label cannot fold its subtotal rows")
			return
		}
	}
	cel, ok := gb.ColumnExprList().(*grammar1.ColumnExprListContext)
	if !ok || cel == nil {
		return
	}
	items := cel.AllColumnsExpr()
	keys = make([]string, 0, len(items))
	for _, it := range items {
		text := strings.TrimSpace(nanopass.NodeText(pr, it.(antlr.ParserRuleContext)))
		if _, intErr := strconv.Atoi(text); intErr == nil {
			err = eb.Build().Str("key", text).Errorf("descriptiveStatistics: positional GROUP BY refers to the pre-expansion select list — name the expression instead")
			return
		}
		keys = append(keys, text)
	}
	return
}

// macroArgs splits the call's arguments into the estimator spec (optional
// leading string literal, default tdigest) and ≥1 column expression texts.
func macroArgs(pr *nanopass.ParseResult, call *grammar1.ColumnExprFunctionContext) (est estimatorSpec, cols []string, err error) {
	argsAny := call.ColumnArgList()
	var items []grammar1.IColumnArgExprContext
	if argsAny != nil {
		if al, ok := argsAny.(*grammar1.ColumnArgListContext); ok {
			items = al.AllColumnArgExpr()
		}
	}
	est = estimators[defaultEstimator]
	texts := make([]string, 0, len(items))
	for _, it := range items {
		texts = append(texts, strings.TrimSpace(nanopass.NodeText(pr, it.(antlr.ParserRuleContext))))
	}
	if len(texts) > 0 && strings.HasPrefix(texts[0], "'") {
		name := strings.Trim(texts[0], "'")
		spec, known := estimators[name]
		if !known {
			err = eb.Build().Str("estimator", name).Errorf("descriptiveStatistics: unknown estimator — one of 'tdigest', 'exact', 'gk', 'dd'")
			return
		}
		est = spec
		texts = texts[1:]
	}
	if len(texts) == 0 {
		err = eb.Build().Errorf("descriptiveStatistics: at least one column argument is required")
		return
	}
	cols = texts
	return
}

// branchColumns renders one branch's select list over column expression
// col: the §SD1 contract columns in table order.
func branchColumns(col string, keys []string, est estimatorSpec, levels []float64) string {
	lv := levelsSQL(levels)
	var b strings.Builder
	b.Grow(512 + 2*len(lv))
	b.WriteString(seriesExpr(col, keys))
	b.WriteString(" AS series, count(")
	b.WriteString(col)
	b.WriteString(") AS n, toUInt64(count() - count(")
	b.WriteString(col)
	b.WriteString(")) AS n_null, min(")
	b.WriteString(col)
	b.WriteString(") AS x_min, max(")
	b.WriteString(col)
	b.WriteString(") AS x_max, avg(")
	b.WriteString(col)
	b.WriteString(") AS mean, stddevSamp(")
	b.WriteString(col)
	b.WriteString(") AS sd, skewSamp(")
	b.WriteString(col)
	b.WriteString(") AS skew, kurtSamp(")
	b.WriteString(col)
	b.WriteString(") AS kurt, [")
	b.WriteString(lv)
	b.WriteString("] AS ps, ")
	b.WriteString(est.call)
	b.WriteString("(")
	b.WriteString(est.extra)
	b.WriteString(lv)
	b.WriteString(")(")
	b.WriteString(col)
	b.WriteString(") AS qs, '")
	b.WriteString(est.token)
	b.WriteString("' AS estimator")
	return b.String()
}

// seriesExpr builds the series label: the argument's source text as a
// literal, with GROUP BY key values folded in via toString (NULL keys
// render the placeholder glyph).
func seriesExpr(col string, keys []string) string {
	lit := "'" + escapeSQLString(col) + "'"
	if len(keys) == 0 {
		return lit
	}
	var b strings.Builder
	b.WriteString("concat(")
	b.WriteString(lit)
	for _, k := range keys {
		b.WriteString(", ' · ', ifNull(toString(")
		b.WriteString(k)
		b.WriteString("), '")
		b.WriteString(seriesNullGlyph)
		b.WriteString("')")
	}
	b.WriteString(")")
	return b.String()
}

// levelsSQL renders the probability grid as a comma-joined literal list;
// FormatFloat 'g'/-1 is the shortest representation that round-trips to
// the identical float64, so ps and the quantiles levels stay bit-equal.
func levelsSQL(levels []float64) string {
	parts := make([]string, 0, len(levels))
	for _, p := range levels {
		parts = append(parts, strconv.FormatFloat(p, 'g', -1, 64))
	}
	return strings.Join(parts, ",")
}

// escapeSQLString escapes a string for a single-quoted ClickHouse literal.
func escapeSQLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}
