package jackstay

import (
	"regexp"
	"slices"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// A row filter (ADR-0271 §SD1) is a boolean expression both servers evaluate,
// so it defines the same slice of a table on each side. It must therefore
// read only the columns both sides hold, and nothing whose value depends on
// when or where it is evaluated.

// volatileFunctions are functions whose value depends on the server, the
// time or the block a row is read in. Names are lower-cased.
var volatileFunctions = []string{
	"now", "now64", "nowinblock", "today", "yesterday", "timezone", "servertimezone", "timezoneof",
	"hostname", "fqdn", "serveruuid", "version", "uptime", "currentdatabase", "currentuser", "currentschemas",
	"currentroles", "enabledroles", "defaultroles", "currentprofiles", "queryid", "initialqueryid",
	"blocknumber", "blocksize", "rownumberinblock", "rownumberinallblocks", "neighbor", "runningaccumulate",
	"runningdifference", "runningdifferencestartingwithfirstvalue", "getsetting", "getmacro", "joinget",
	"file", "url", "s3", "hdfs", "remote", "cluster", "input", "throwif", "sleep", "sleepeachrow",
}

// volatilePrefixes are families of such functions.
var volatilePrefixes = []string{"rand", "generateuuid", "generatesnowflake", "generateulid", "dictget", "dicthas", "dictisin"}

func isVolatileFunction(name string) (volatile bool) {
	n := strings.ToLower(name)
	if slices.Contains(volatileFunctions, n) {
		return true
	}
	for _, p := range volatilePrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// dateTimeLiteral matches a string literal that a DateTime column would parse
// in the reading server's timezone.
var dateTimeLiteral = regexp.MustCompile(`'\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}`)

// filterProbeTable is the table the probe statement reads; a filter that
// names another table is refused.
const filterProbeTable = "jackstay_filter_probe"

// ValidateFilter checks a row filter against the columns it may read (the
// copy column list). notes are facts the operator should know but that do not
// stop the filter, such as a timezone-dependent literal.
func ValidateFilter(filter string, columns []string) (notes []string, err error) {
	if strings.TrimSpace(filter) == "" {
		err = eh.Errorf("filter is empty")
		return
	}
	var pr *nanopass.ParseResult
	pr, err = nanopass.Parse("SELECT 1 FROM " + filterProbeTable + " WHERE (" + filter + ")")
	if err != nil {
		err = eb.Build().Str("filter", filter).Errorf("filter does not parse as an expression: %w", err)
		return
	}
	selects := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar1.SelectStmtContext)
		return ok
	})
	if len(selects) != 1 {
		err = eb.Build().Str("filter", filter).Errorf("filter must be one expression, without a subquery")
		return
	}
	for _, t := range analysis.ExtractTables(pr) {
		if t.Database != "" || t.Table != filterProbeTable {
			err = eb.Build().Str("filter", filter).Str("table", t.Table).Errorf("filter must not read a table")
			return
		}
	}
	for _, f := range analysis.ExtractFunctions(pr) {
		if isVolatileFunction(f.Name) {
			err = eb.Build().Str("filter", filter).Str("function", f.Name).
				Errorf("filter calls a function whose value depends on when or where it runs; both servers must select the same rows")
			return
		}
	}
	lambdaParams := lambdaParameters(pr)
	for _, c := range analysis.ExtractColumns(pr) {
		if c.Table != "" {
			err = eb.Build().Str("filter", filter).Str("qualifier", c.Table).Errorf("filter must name columns unqualified")
			return
		}
		if slices.Contains(columns, c.Column) || slices.Contains(lambdaParams, c.Column) {
			continue
		}
		// A nested path (a.b) may name a tuple element of a copied column.
		if head, _, nested := strings.Cut(c.Column, "."); nested && slices.Contains(columns, head) {
			continue
		}
		err = eb.Build().Str("filter", filter).Str("column", c.Column).
			Errorf("filter names a column that is not copied; the target could not evaluate it")
		return
	}
	if dateTimeLiteral.MatchString(filter) {
		notes = append(notes, "filter compares with a date-time literal, which each server reads in its own timezone unless the expression names one")
	}
	return
}

func lambdaParameters(pr *nanopass.ParseResult) (names []string) {
	for _, n := range nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar1.ColumnLambdaExprContext)
		return ok
	}) {
		for _, id := range n.(*grammar1.ColumnLambdaExprContext).AllIdentifier() {
			names = append(names, nanopass.DecodeIdentifier(id.GetText()))
		}
	}
	return
}

// andPredicates joins predicates with AND, each parenthesised, dropping empty
// ones and the constant "1".
func andPredicates(preds ...string) (sql string) {
	kept := make([]string, 0, len(preds))
	for _, p := range preds {
		if p != "" && p != "1" {
			kept = append(kept, p)
		}
	}
	switch len(kept) {
	case 0:
		return ""
	case 1:
		return kept[0]
	}
	return "(" + strings.Join(kept, ") AND (") + ")"
}
