package play

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ExtractParams parses sql and removes any top-level `SET` statement whose
// every setting name starts with `param_`, returning the residual SQL plus
// the harvested parameter values.
//
// The values are the raw SQL literal texts of the right-hand side, with
// surrounding single quotes stripped from string literals so they can be
// shipped verbatim as ClickHouse HTTP `?param_<name>=<value>` URL fields.
//
// Naming convention: ClickHouse maps URL key `param_<X>` to placeholder
// `{<X>:Type}` — the `param_` prefix is the URL-side marker, not part of
// the placeholder name. This pass passes SET names through verbatim, so
// `SET param_a=1; SELECT {a:UInt64}` is the canonical form. To use the
// placeholder `{param_a:Type}` literally, the SET must be
// `SET param_param_a=1`.
//
// A SET statement that mixes `param_*` settings with non-`param_*` settings
// is rejected: partial deletion of individual settingExprs (with their
// commas) is fiddly and out of scope. SET statements that contain only
// non-`param_*` settings are left intact in the residual.
//
// See ExecuteArrowStream's doc for the URL-length limits that bound how
// large the harvested values can collectively be.
func ExtractParams(sql string) (residual string, params map[string]string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("ExtractParams: %w", err)
		return
	}

	var paramStmts []*grammar1.SetStmtContext
	params, paramStmts, err = collectParamSettings(pr)
	if err != nil {
		err = eh.Errorf("ExtractParams: %w", err)
		return
	}

	rw := nanopass.NewRewriter(pr)
	for _, setStmt := range paramStmts {
		deleteSetStmt(rw, pr, setStmt)
	}
	residual = strings.TrimLeft(nanopass.GetText(rw), " \t\r\n")
	return
}

type paramPair struct {
	name  string
	value string
}

// collectParamSettings walks the first query's top-level SET statements and
// harvests every `param_*` setting into a name→value map, also returning the
// SetStmt nodes that consist *only* of param_* settings so ExtractParams can
// delete them from the residual. This is the single SET-harvest both
// ExtractParams (which deletes) and collectParamValues (which only reads)
// share. A SET that mixes param_* with regular settings is rejected (partial
// deletion is out of scope); a SET with no param_* settings is left untouched.
func collectParamSettings(pr *nanopass.ParseResult) (params map[string]string, paramOnlyStmts []*grammar1.SetStmtContext, err error) {
	params = make(map[string]string)
	queryCtx := findFirstQuery(pr)
	if queryCtx == nil {
		return
	}
	n := queryCtx.GetChildCount()
	for i := 0; i < n; i++ {
		setStmt, ok := queryCtx.GetChild(i).(*grammar1.SetStmtContext)
		if !ok {
			continue
		}
		var pairs []paramPair
		var nonParam uint32
		stmtErr := iterateSettingExprs(setStmt, func(expr *grammar1.SettingExprContext) (stopErr error) {
			name, value, exErr := extractSettingNameValue(pr, expr)
			if exErr != nil {
				stopErr = exErr
				return
			}
			if !strings.HasPrefix(name, "param_") {
				nonParam++
				return
			}
			pairs = append(pairs, paramPair{name: name, value: value})
			return
		})
		if stmtErr != nil {
			err = stmtErr
			return
		}
		if len(pairs) == 0 {
			continue
		}
		if nonParam > 0 {
			err = eb.Build().Errorf("SET statement mixes param_* with non-param settings (not supported)")
			return
		}
		for _, p := range pairs {
			params[p.name] = p.value
		}
		paramOnlyStmts = append(paramOnlyStmts, setStmt)
	}
	return
}

func findFirstQuery(pr *nanopass.ParseResult) (out *grammar1.QueryContext) {
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if q, ok := ctx.(*grammar1.QueryContext); ok {
			out = q
			return false
		}
		return true
	})
	return
}

func iterateSettingExprs(setStmt *grammar1.SetStmtContext, visit func(expr *grammar1.SettingExprContext) error) (err error) {
	n := setStmt.GetChildCount()
	for i := 0; i < n; i++ {
		list, ok := setStmt.GetChild(i).(*grammar1.SettingExprListContext)
		if !ok {
			continue
		}
		m := list.GetChildCount()
		for j := 0; j < m; j++ {
			expr, ok := list.GetChild(j).(*grammar1.SettingExprContext)
			if !ok {
				continue
			}
			err = visit(expr)
			if err != nil {
				return
			}
		}
	}
	return
}

// extractSettingNameValue pulls the identifier name and the unquoted SQL
// value text from a `name = value` settingExpr. The grammar guarantees the
// child layout `identifier EQ_SINGLE settingValue`, so we read by position.
func extractSettingNameValue(pr *nanopass.ParseResult, expr *grammar1.SettingExprContext) (name string, value string, err error) {
	if expr.GetChildCount() < 3 {
		err = eh.Errorf("settingExpr has %d children, expected at least 3", expr.GetChildCount())
		return
	}
	ident, ok := expr.GetChild(0).(*grammar1.IdentifierContext)
	if !ok {
		err = eh.Errorf("settingExpr first child is not an identifier")
		return
	}
	valueCtx, ok := expr.GetChild(2).(antlr.ParserRuleContext)
	if !ok {
		err = eh.Errorf("settingExpr third child is not a parser rule context")
		return
	}
	name = strings.Trim(ident.GetText(), "`")
	value = chParamValue(nanopass.NodeText(pr, valueCtx))
	return
}

// chParamValue converts a SQL literal into the unquoted form expected by
// ClickHouse's HTTP `param_*` channel. Outer single quotes are stripped from
// string literals and the standard backslash escapes plus the doubled-quote
// escape (`''`) are decoded; numbers, arrays, tuples, and other forms are
// passed through verbatim.
func chParamValue(literalSQL string) (out string) {
	s := strings.TrimSpace(literalSQL)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		inner := s[1 : len(s)-1]
		out = sqlStringUnescape.Replace(inner)
		return
	}
	out = s
	return
}

var sqlStringUnescape = strings.NewReplacer(
	`\\`, `\`,
	`\'`, `'`,
	`\"`, `"`,
	`\n`, "\n",
	`\r`, "\r",
	`\t`, "\t",
	`\0`, "\x00",
	`''`, `'`, // CH also accepts SQL-standard quote doubling inside '…'
)

// deleteSetStmt removes the SET statement plus a single preceding whitespace
// token if present, so that consecutive deletions don't leave double blanks.
func deleteSetStmt(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, setStmt *grammar1.SetStmtContext) {
	start := setStmt.GetStart().GetTokenIndex()
	stop := setStmt.GetStop().GetTokenIndex()
	if start > 0 {
		prev := pr.TokenStream.Get(start - 1)
		if prev.GetTokenType() == grammar1.ClickHouseLexerWHITESPACE {
			start = prev.GetTokenIndex()
		}
	}
	rw.DeleteDefault(start, stop)
}
