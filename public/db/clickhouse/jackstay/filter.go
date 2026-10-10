package jackstay

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

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
// time or the block a row is read in, or which multiply rows (arrayJoin).
// Names are lower-cased.
var volatileFunctions = []string{
	"now", "now64", "nowinblock", "today", "yesterday", "current_timestamp", "current_date", "localtimestamp",
	"utctimestamp", "utc_timestamp", "curdate", "sysdate",
	"timezone", "servertimezone", "timezoneof",
	"hostname", "fqdn", "displayname", "serveruuid", "version", "revision", "buildid", "uptime",
	"getserverport", "tcpport", "getoskernelversion", "zookeepersessionuptime", "shardnum", "shardcount",
	"connectionid", "connection_id", "currentdatabase", "currentuser", "currentschemas", "currentroles",
	"enabledroles", "defaultroles", "currentprofiles", "enabledprofiles", "defaultprofiles",
	"queryid", "initialqueryid", "querystarttime", "initialquerystarttime", "getclienthttpheader",
	"transactionid", "transactionlatestsnapshot", "transactionoldestsnapshot",
	"blocknumber", "blocksize", "rownumberinblock", "rownumberinallblocks", "neighbor", "runningaccumulate",
	"runningdifference", "runningdifferencestartingwithfirstvalue", "runningconcurrency",
	"getsetting", "getsettingordefault", "getmacro", "hascolumnintable", "modelevaluate", "catboostevaluate",
	"file", "url", "s3", "hdfs", "remote", "cluster", "input", "throwif", "sleep", "sleepeachrow",
	"arrayjoin",
}

// volatilePrefixes are families of such functions.
var volatilePrefixes = []string{"rand", "generateuuid", "generatesnowflake", "generateulid", "dictget", "dicthas", "dictisin", "joinget", "filesystem"}

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

// zoneArgument gives, for date functions that read a DateTime in the
// server's timezone unless a zone is passed, the position of that optional
// argument (counting from one). Names are lower-cased; the toStartOf… and
// toRelative… families are matched by prefix in [isZoneDependentCall].
var zoneArgument = map[string]int{
	"todate": 2, "todate32": 2, "toyear": 2, "toquarter": 2, "tomonth": 2, "todayofyear": 2, "todayofmonth": 2,
	"tohour": 2, "tominute": 2, "tosecond": 2, "toyyyymm": 2, "toyyyymmdd": 2, "toyyyymmddhhmmss": 2,
	"tomonday": 2, "toisoyear": 2, "toisoweek": 2,
	"todayofweek": 3, "toweek": 3, "toyearweek": 3, "formatdatetime": 3, "datetrunc": 3, "date_trunc": 3,
	"tostartofinterval": 3,
}

// isZoneDependentCall reports whether a call of name with nArgs arguments
// names no timezone, so over a DateTime column its value is the server's.
func isZoneDependentCall(name string, nArgs int) (dependent bool) {
	n := strings.ToLower(name)
	if pos, has := zoneArgument[n]; has {
		return nArgs < pos
	}
	if strings.HasPrefix(n, "tostartof") || strings.HasPrefix(n, "torelative") {
		return nArgs < 2
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
//
// A filter is joined to other predicates as text ([andPredicates]), so it
// must be one expression on its own. The probe puts it in parentheses, and
// those parentheses must enclose exactly the filter: `a) OR (b` parses inside
// them too, and would widen every predicate it is joined to.
func ValidateFilter(filter string, columns []string) (notes []string, err error) {
	if strings.TrimSpace(filter) == "" {
		err = eh.Errorf("filter is empty")
		return
	}
	err = checkFilterText(filter)
	if err != nil {
		err = eb.Build().Str("filter", filter).Errorf("filter must be one expression: %w", err)
		return
	}
	const prefix = "SELECT 1 FROM " + filterProbeTable + " WHERE ("
	probe := prefix + filter + ")"
	var pr *nanopass.ParseResult
	pr, err = nanopass.Parse(probe)
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
	if !enclosesFilter(pr, utf8.RuneCountInString(prefix)-1, utf8.RuneCountInString(probe)-1) {
		err = eb.Build().Str("filter", filter).Errorf("filter must be one expression, not a fragment that closes the parentheses around it")
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
				Errorf("filter calls a function whose value depends on when or where it runs, or that multiplies rows; both servers must select the same rows")
			return
		}
	}
	for _, c := range filterColumns(pr) {
		if c.resolves(columns) {
			continue
		}
		switch c.qualifiers {
		case 0:
			err = eb.Build().Str("filter", filter).Str("column", strings.Join(c.path, ".")).
				Errorf("filter names a column that is not copied; the target could not evaluate it")
		case 1:
			err = eb.Build().Str("filter", filter).Str("qualifier", c.path[0]).
				Errorf("filter must name columns unqualified; a dotted name must start with a copied column")
		default:
			err = eb.Build().Str("filter", filter).Str("qualifier", strings.Join(c.path[:c.qualifiers], ".")).
				Errorf("filter must name columns unqualified")
		}
		return
	}
	if dateTimeLiteral.MatchString(filter) {
		notes = append(notes, "filter compares with a date-time literal, which each server reads in its own timezone unless the expression names one")
	}
	for _, name := range zoneDependentCalls(pr) {
		notes = append(notes, "filter calls "+name+" without a timezone; over a DateTime column each server computes it in its own timezone")
	}
	return
}

// checkFilterText refuses a filter whose parentheses do not balance outside
// string literals and quoted identifiers, and one holding a comment, which
// could hide the parenthesis the probe closes the filter with.
func checkFilterText(filter string) (err error) {
	depth := 0
	var quote byte
	for i := 0; i < len(filter); i++ {
		ch := filter[i]
		if quote != 0 {
			switch ch {
			case '\\':
				i++
			case quote:
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return eh.Errorf("a parenthesis closes one the filter did not open")
			}
		case '#':
			return eh.Errorf("a comment is not allowed")
		case '-', '/':
			if i+1 < len(filter) && ((ch == '-' && filter[i+1] == '-') || (ch == '/' && filter[i+1] == '*')) {
				return eh.Errorf("a comment is not allowed")
			}
		}
	}
	if quote != 0 {
		return eh.Errorf("a quote is not closed")
	}
	if depth != 0 {
		return eh.Errorf("a parenthesis is not closed")
	}
	return
}

// enclosesFilter reports whether the probe's WHERE expression is the pair of
// parentheses the probe put around the filter, opening at open and closing
// at close (rune offsets into the probe).
func enclosesFilter(pr *nanopass.ParseResult, open int, close int) (ok bool) {
	wheres := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, is := ctx.(*grammar1.WhereClauseContext)
		return is
	})
	if len(wheres) != 1 {
		return false
	}
	e, is := wheres[0].(*grammar1.WhereClauseContext).ColumnExpr().(*grammar1.ColumnExprParensContext)
	if !is {
		return false
	}
	return e.GetStart().GetStart() == open && e.GetStop().GetStop() == close
}

// filterColumn is one column reference of a filter.
type filterColumn struct {
	// path is the reference's segments. The parser reads the leading ones
	// as a table qualifier; qualifiers counts them.
	path       []string
	qualifiers int
	// params are the parameters of the lambdas whose body holds the
	// reference.
	params []string
}

// resolves reports whether the reference names a lambda parameter in scope,
// a copied column, or a subcolumn or tuple element of one (`n.x`, `tup.a`).
// A filter cannot name a table, so a single qualifier is read as the head
// of such a path; two are a database and a table.
func (inst filterColumn) resolves(columns []string) (ok bool) {
	if inst.qualifiers > 1 {
		return false
	}
	if slices.Contains(inst.params, inst.path[0]) {
		return true
	}
	for n := len(inst.path); n > 0; n-- {
		if slices.Contains(columns, strings.Join(inst.path[:n], ".")) {
			return true
		}
	}
	return false
}

// packFilterColumns are the columns a pack's filter may name: the copied
// ones, and those of the sorting key, which a sampled export's predicate
// hashes ([DigestSpec.SamplePredicate]). Both sides hold the key's columns,
// stored or MATERIALIZED, since a syncable table shares its sorting key.
func packFilterColumns(copyColumns []string, sortingKey string) (columns []string) {
	columns = slices.Clone(copyColumns)
	if strings.TrimSpace(sortingKey) == "" {
		return
	}
	pr, err := nanopass.Parse("SELECT 1 FROM " + filterProbeTable + " WHERE tuple(" + sortingKey + ")")
	if err != nil {
		return
	}
	for _, c := range filterColumns(pr) {
		if c.qualifiers == 0 && len(c.params) == 0 && !slices.Contains(columns, c.path[0]) {
			columns = append(columns, c.path[0])
		}
	}
	return
}

func filterColumns(pr *nanopass.ParseResult) (refs []filterColumn) {
	for _, n := range nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar1.ColumnIdentifierContext)
		return ok
	}) {
		cid := n.(*grammar1.ColumnIdentifierContext)
		var ref filterColumn
		if ti := cid.ColumnQualifier(); ti != nil {
			if db := ti.DatabaseIdentifier(); db != nil {
				ref.path = append(ref.path, nanopass.DecodeIdentifier(db.GetText()))
			}
			if id := ti.Identifier(); id != nil {
				ref.path = append(ref.path, nanopass.DecodeIdentifier(id.GetText()))
			} else {
				ref.path = append(ref.path, ti.GetText())
			}
			ref.qualifiers = len(ref.path)
		}
		if ni := cid.NestedIdentifier(); ni != nil {
			for _, id := range ni.AllIdentifier() {
				ref.path = append(ref.path, nanopass.DecodeIdentifier(id.GetText()))
			}
		}
		if len(ref.path) == 0 {
			continue
		}
		// A lambda's parameters are in scope in its body only.
		for p := cid.GetParent(); p != nil; p = p.GetParent() {
			if l, is := p.(*grammar1.ColumnLambdaExprContext); is {
				for _, id := range l.AllIdentifier() {
					ref.params = append(ref.params, nanopass.DecodeIdentifier(id.GetText()))
				}
			}
		}
		refs = append(refs, ref)
	}
	return
}

// zoneDependentCalls names the filter's calls that read a DateTime in the
// server's timezone because they name none.
func zoneDependentCalls(pr *nanopass.ParseResult) (names []string) {
	for _, n := range nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar1.ColumnExprFunctionContext)
		return ok
	}) {
		f := n.(*grammar1.ColumnExprFunctionContext)
		name := nanopass.DecodeIdentifier(f.Identifier().GetText())
		nArgs := 0
		if args := f.ColumnArgList(); args != nil {
			nArgs = len(args.AllColumnArgExpr())
		}
		if isZoneDependentCall(name, nArgs) && !slices.Contains(names, name) {
			names = append(names, name)
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
