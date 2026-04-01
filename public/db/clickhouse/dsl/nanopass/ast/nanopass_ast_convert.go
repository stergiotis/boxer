//go:build llm_generated_opus46

package ast

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ConvertCSTToAST converts a parsed CST (from nanopass.Parse) into a canonical AST.
// The CST must have been normalized by preceding passes:
//   - All casts must be in CAST(expr, 'Type') functional form (ColumnExprFunction with CAST identifier)
//   - Array literals must use array() function form
//   - Tuple literals must use tuple() function form
//   - Array access must use arrayElement() function form
//   - Tuple access must use tupleElement() function form
//
// Returns an error if non-canonical CST nodes are encountered (e.g. expr::Type cast,
// [1,2] array literal, t.1 tuple access).
func ConvertCSTToAST(pr *nanopass.ParseResult) (query Query, err error) {
	// Find the queryStmt root
	var queryStmtCtx *grammar.QueryStmtContext
	for i := 0; i < pr.Tree.GetChildCount(); i++ {
		if qs, ok := pr.Tree.GetChild(i).(*grammar.QueryStmtContext); ok {
			queryStmtCtx = qs
			break
		}
	}
	if queryStmtCtx == nil {
		// Try direct query rule
		return convertFromTree(pr, pr.Tree)
	}
	return convertQueryStmt(pr, queryStmtCtx)
}

func convertFromTree(pr *nanopass.ParseResult, tree antlr.Tree) (query Query, err error) {
	for i := 0; i < tree.GetChildCount(); i++ {
		switch ctx := tree.GetChild(i).(type) {
		case *grammar.QueryStmtContext:
			return convertQueryStmt(pr, ctx)
		case *grammar.QueryContext:
			return convertQuery(pr, ctx)
		case *grammar.SelectUnionStmtContext:
			query.Body, err = convertSelectUnion(pr, ctx)
			return
		}
	}
	err = eh.Errorf("ConvertCSTToAST: no query found in tree")
	return
}

func convertQueryStmt(pr *nanopass.ParseResult, ctx *grammar.QueryStmtContext) (query Query, err error) {
	// Find query child
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar.QueryContext:
			query, err = convertQuery(pr, c)
			if err != nil {
				return
			}
		}
	}

	// INTO OUTFILE
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar.ClickHouseLexerOUTFILE {
				// Next non-terminal should be STRING_LITERAL
				if i+1 < ctx.GetChildCount() {
					if lit, ok := ctx.GetChild(i + 1).(*antlr.TerminalNodeImpl); ok {
						query.OutFile = unquoteString(lit.GetText())
					}
				}
			}
		}
	}

	// FORMAT
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar.ClickHouseLexerFORMAT {
				if i+1 < ctx.GetChildCount() {
					if identCtx, ok := ctx.GetChild(i + 1).(*grammar.IdentifierOrNullContext); ok {
						query.Format = normalizeIdentifier(identCtx.GetText())
					}
				}
			}
		}
	}

	return
}

func convertQuery(pr *nanopass.ParseResult, ctx *grammar.QueryContext) (query Query, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar.SetStmtContext:
			settings, setErr := convertSetStmt(pr, c)
			if setErr != nil {
				err = eh.Errorf("convertQuery: %w", setErr)
				return
			}
			query.Settings = append(query.Settings, settings...)

		case *grammar.CtesContext:
			query.CTEs, err = convertCTEs(pr, c)
			if err != nil {
				return
			}

		case *grammar.SelectUnionStmtContext:
			query.Body, err = convertSelectUnion(pr, c)
			if err != nil {
				return
			}
		}
	}
	return
}

// --- SET ---

func convertSetStmt(pr *nanopass.ParseResult, ctx *grammar.SetStmtContext) (settings []SettingPair, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if sel, ok := ctx.GetChild(i).(*grammar.SettingExprListContext); ok {
			settings, err = convertSettingExprList(pr, sel)
			return
		}
	}
	return
}

func convertSettingExprList(pr *nanopass.ParseResult, ctx *grammar.SettingExprListContext) (settings []SettingPair, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if se, ok := ctx.GetChild(i).(*grammar.SettingExprContext); ok {
			pair, pairErr := convertSettingExpr(pr, se)
			if pairErr != nil {
				err = pairErr
				return
			}
			settings = append(settings, pair)
		}
	}
	return
}

func convertSettingExpr(pr *nanopass.ParseResult, ctx *grammar.SettingExprContext) (pair SettingPair, err error) {
	// identifier EQ_SINGLE settingValue
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar.IdentifierContext); ok {
			pair.Key = identText(ident)
		}
	}
	// settingValue is the last child — take its full text
	for i := 0; i < ctx.GetChildCount(); i++ {
		if _, ok := ctx.GetChild(i).(grammar.ISettingValueContext); ok {
			pair.ValueSQL = nanopass.NodeText(pr, ctx.GetChild(i).(antlr.ParserRuleContext))
		}
	}
	return
}

// --- CTEs ---

func convertCTEs(pr *nanopass.ParseResult, ctx *grammar.CtesContext) (ctes []CTE, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if nq, ok := ctx.GetChild(i).(*grammar.NamedQueryContext); ok {
			cte, cteErr := convertNamedQuery(pr, nq)
			if cteErr != nil {
				err = cteErr
				return
			}
			ctes = append(ctes, cte)
		}
	}
	return
}

func convertNamedQuery(pr *nanopass.ParseResult, ctx *grammar.NamedQueryContext) (cte CTE, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar.IdentifierContext:
			if cte.Name == "" {
				cte.Name = identText(c)
			}
		case *grammar.ColumnAliasesContext:
			cte.ColumnAliases = convertColumnAliases(c)
		case *grammar.QueryContext:
			cte.Body, err = convertQuery(pr, c)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertColumnAliases(ctx *grammar.ColumnAliasesContext) (aliases []string) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar.IdentifierContext); ok {
			aliases = append(aliases, identText(ident))
		}
	}
	return
}

// --- SELECT UNION ---

func convertSelectUnion(pr *nanopass.ParseResult, ctx *grammar.SelectUnionStmtContext) (su SelectUnion, err error) {
	first := true
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar.SelectStmtWithParensContext:
			if first {
				su.Head, err = convertSelectStmtWithParens(pr, c)
				if err != nil {
					return
				}
				first = false
			}
		case *grammar.SelectUnionStmtItemContext:
			item, itemErr := convertSelectUnionItem(pr, c)
			if itemErr != nil {
				err = itemErr
				return
			}
			su.Items = append(su.Items, item)
		}
	}
	return
}

func convertSelectUnionItem(pr *nanopass.ParseResult, ctx *grammar.SelectUnionStmtItemContext) (item SelectUnionItem, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar.ClickHouseLexerUNION:
				item.Op = "UNION"
			case grammar.ClickHouseLexerEXCEPT:
				item.Op = "EXCEPT"
			case grammar.ClickHouseLexerINTERSECT:
				item.Op = "INTERSECT"
			case grammar.ClickHouseLexerALL:
				item.Modifier = "ALL"
			case grammar.ClickHouseLexerDISTINCT:
				item.Modifier = "DISTINCT"
			}
		}
		if swp, ok := child.(*grammar.SelectStmtWithParensContext); ok {
			item.Select, err = convertSelectStmtWithParens(pr, swp)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertSelectStmtWithParens(pr *nanopass.ParseResult, ctx *grammar.SelectStmtWithParensContext) (sel Select, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar.SelectStmtContext:
			return convertSelectStmt(pr, c)
		case *grammar.SelectUnionStmtContext:
			// Parenthesized union — wrap in a subquery-like structure
			// For simplicity, if it's a single select, unwrap
			su, suErr := convertSelectUnion(pr, c)
			if suErr != nil {
				err = suErr
				return
			}
			if len(su.Items) == 0 {
				sel = su.Head
				return
			}
			// Multiple selects in parens — this is a union, treat the first as head
			sel = su.Head
			// TODO: preserve union structure in parenthesized form
			return
		}
	}
	err = eh.Errorf("convertSelectStmtWithParens: empty")
	return
}

// --- SELECT ---

func convertSelectStmt(pr *nanopass.ParseResult, ctx *grammar.SelectStmtContext) (sel Select, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		switch c := child.(type) {
		case *grammar.ProjectionClauseContext:
			err = convertProjectionClause(pr, c, &sel)
		case *grammar.WithClauseContext:
			sel.With, err = convertWithClause(pr, c)
		case *grammar.FromClauseContext:
			var je JoinExpr
			je, err = convertFromClause(pr, c)
			sel.From = &je
		case *grammar.ArrayJoinClauseContext:
			var aj ArrayJoinClause
			aj, err = convertArrayJoinClause(pr, c)
			sel.ArrayJoin = &aj
		case *grammar.WindowClauseContext:
			var wd WindowDefClause
			wd, err = convertWindowClause(pr, c)
			sel.WindowDef = &wd
		case *grammar.QualifyClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Qualify = &expr
		case *grammar.PrewhereClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Prewhere = &expr
		case *grammar.WhereClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Where = &expr
		case *grammar.GroupByClauseContext:
			var gb GroupByClause
			gb, err = convertGroupByClause(pr, c)
			sel.GroupBy = &gb
		case *grammar.HavingClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Having = &expr
		case *grammar.OrderByClauseContext:
			var ob OrderByClause
			ob, err = convertOrderByClause(pr, c)
			sel.OrderBy = &ob
		case *grammar.LimitByClauseContext:
			var lb LimitByClause
			lb, err = convertLimitByClause(pr, c)
			sel.LimitBy = &lb
		case *grammar.LimitClauseContext:
			var lc LimitClause
			lc, err = convertLimitClause(pr, c)
			sel.Limit = &lc
		case *grammar.SettingsClauseContext:
			sel.Settings, err = convertSettingsClause(pr, c)
		}
		if err != nil {
			return
		}
	}
	return
}

func convertProjectionClause(pr *nanopass.ParseResult, ctx *grammar.ProjectionClauseContext, sel *Select) (err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar.ClickHouseLexerDISTINCT {
				sel.Distinct = true
			}
		}
		if tc, ok := child.(*grammar.TopClauseContext); ok {
			top, topErr := convertTopClause(tc)
			if topErr != nil {
				err = topErr
				return
			}
			sel.Top = &top
		}
		if cel, ok := child.(*grammar.ColumnExprListContext); ok {
			sel.Projection, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
		if pec, ok := child.(*grammar.ProjectionExceptClauseContext); ok {
			exc, excErr := convertProjectionExceptClause(pr, pec)
			if excErr != nil {
				err = excErr
				return
			}
			sel.ExceptColumns = &exc
		}
	}
	return
}

func convertTopClause(ctx *grammar.TopClauseContext) (top TopClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar.ClickHouseLexerDECIMAL_LITERAL:
				top.N, err = strconv.ParseUint(term.GetText(), 10, 64)
				if err != nil {
					err = eh.Errorf("convertTopClause: %w", err)
					return
				}
			case grammar.ClickHouseLexerTIES:
				top.WithTies = true
			}
		}
	}
	return
}

func convertProjectionExceptClause(pr *nanopass.ParseResult, ctx *grammar.ProjectionExceptClauseContext) (exc ExceptClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		switch c := child.(type) {
		case *grammar.StaticColumnListContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if ident, ok := c.GetChild(j).(*grammar.IdentifierContext); ok {
					exc.Static = append(exc.Static, identText(ident))
				}
			}
		case *grammar.DynamicColumnListContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if dcs, ok := c.GetChild(j).(*grammar.DynamicColumnSelectionContext); ok {
					exc.Dynamic = extractDynamicPattern(dcs)
				}
			}
		}
	}
	return
}

func convertWithClause(pr *nanopass.ParseResult, ctx *grammar.WithClauseContext) (exprs []Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if cel, ok := ctx.GetChild(i).(*grammar.ColumnExprListContext); ok {
			exprs, err = convertColumnExprList(pr, cel)
			return
		}
	}
	return
}

func convertSingleExprClause(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (expr Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar.IColumnExprContext); ok {
			return convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
		}
	}
	err = eh.Errorf("convertSingleExprClause: no column expression found in %T", ctx)
	return
}

// --- GROUP BY ---

func convertGroupByClause(pr *nanopass.ParseResult, ctx *grammar.GroupByClauseContext) (gb GroupByClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar.ClickHouseLexerCUBE:
				gb.Modifier = "CUBE"
			case grammar.ClickHouseLexerROLLUP:
				gb.Modifier = "ROLLUP"
			case grammar.ClickHouseLexerTOTALS:
				gb.WithTotals = true
			}
		}
		if cel, ok := child.(*grammar.ColumnExprListContext); ok {
			gb.Exprs, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

// --- ORDER BY ---

func convertOrderByClause(pr *nanopass.ParseResult, ctx *grammar.OrderByClauseContext) (ob OrderByClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if oel, ok := child.(*grammar.OrderExprListContext); ok {
			ob.Items, err = convertOrderExprList(pr, oel)
			if err != nil {
				return
			}
		}
		// TODO: WITH FILL parsing
	}
	return
}

func convertOrderExpr(pr *nanopass.ParseResult, ctx *grammar.OrderExprContext) (item OrderItem, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar.IColumnExprContext); ok {
			item.Expr, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
			item.Expr = unwrapOrderExprAlias(item.Expr, &item)
			continue
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			tt := term.GetSymbol().GetTokenType()
			switch tt {
			case grammar.ClickHouseLexerASCENDING:
				item.Dir = "ASC"
			case grammar.ClickHouseLexerDESCENDING:
				item.Dir = "DESC"
			case grammar.ClickHouseLexerDESC:
				item.Dir = "DESC"
			case grammar.ClickHouseLexerNULLS:
				// skip
			case grammar.ClickHouseLexerFIRST:
				item.Nulls = "FIRST"
			case grammar.ClickHouseLexerLAST:
				item.Nulls = "LAST"
			case grammar.ClickHouseLexerSTRING_LITERAL:
				item.Collate = unquoteString(term.GetText())
			}
		}
	}
	return
}

// The grammar may parse "a DESC NULLS LAST" as nested aliases:
// ColumnExprAlias(ColumnExprAlias(a, "DESC"), "NULLS")
// with LAST as a terminal of orderExpr.
// Or even: ColumnExprAlias(ColumnExprAlias(ColumnExprAlias(a, "DESC"), "NULLS"), "LAST")
// We need to iteratively unwrap.
func unwrapOrderExprAlias(expr Expr, item *OrderItem) Expr {
	for expr.Kind == KindAlias && expr.Alias != nil {
		upper := strings.ToUpper(expr.Alias.Name)
		switch upper {
		case "ASC", "ASCENDING":
			item.Dir = "ASC"
			expr = expr.Alias.Expr
		case "DESC", "DESCENDING":
			item.Dir = "DESC"
			expr = expr.Alias.Expr
		case "NULLS":
			// NULLS by itself means nothing — FIRST/LAST follows
			expr = expr.Alias.Expr
		case "FIRST":
			item.Nulls = "FIRST"
			expr = expr.Alias.Expr
		case "LAST":
			item.Nulls = "LAST"
			expr = expr.Alias.Expr
		default:
			// Real alias — stop unwrapping
			return expr
		}
	}
	return expr
}

func convertOrderExprList(pr *nanopass.ParseResult, ctx *grammar.OrderExprListContext) (items []OrderItem, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if oe, ok := ctx.GetChild(i).(*grammar.OrderExprContext); ok {
			item, itemErr := convertOrderExpr(pr, oe)
			if itemErr != nil {
				err = itemErr
				return
			}
			items = append(items, item)
		}
	}
	return
}

// --- LIMIT ---

func convertLimitClause(pr *nanopass.ParseResult, ctx *grammar.LimitClauseContext) (lc LimitClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if le, ok := child.(*grammar.LimitExprContext); ok {
			lc.Limit, err = convertLimitExpr(pr, le)
			if err != nil {
				return
			}
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar.ClickHouseLexerTIES {
				lc.WithTies = true
			}
		}
	}
	return
}

func convertLimitByClause(pr *nanopass.ParseResult, ctx *grammar.LimitByClauseContext) (lb LimitByClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if le, ok := child.(*grammar.LimitExprContext); ok {
			lb.Limit, err = convertLimitExpr(pr, le)
			if err != nil {
				return
			}
		}
		if cel, ok := child.(*grammar.ColumnExprListContext); ok {
			lb.Columns, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertLimitExpr(pr *nanopass.ParseResult, ctx *grammar.LimitExprContext) (ls LimitSpec, err error) {
	exprs := make([]Expr, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar.IColumnExprContext); ok {
			expr, exprErr := convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if exprErr != nil {
				err = exprErr
				return
			}
			exprs = append(exprs, expr)
		}
	}
	if len(exprs) >= 1 {
		ls.Limit = exprs[0]
	}
	if len(exprs) >= 2 {
		ls.Offset = &exprs[1]
	}
	return
}

// --- SETTINGS ---

func convertSettingsClause(pr *nanopass.ParseResult, ctx *grammar.SettingsClauseContext) (settings []SettingPair, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if sel, ok := ctx.GetChild(i).(*grammar.SettingExprListContext); ok {
			return convertSettingExprList(pr, sel)
		}
	}
	return
}

// --- ARRAY JOIN ---

func convertArrayJoinClause(pr *nanopass.ParseResult, ctx *grammar.ArrayJoinClauseContext) (aj ArrayJoinClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar.ClickHouseLexerLEFT:
				aj.Kind = "LEFT"
			case grammar.ClickHouseLexerINNER:
				aj.Kind = "INNER"
			}
		}
		if cel, ok := child.(*grammar.ColumnExprListContext); ok {
			aj.Exprs, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

// --- WINDOW ---

func convertWindowClause(pr *nanopass.ParseResult, ctx *grammar.WindowClauseContext) (wd WindowDefClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ident, ok := child.(*grammar.IdentifierContext); ok {
			wd.Name = identText(ident)
		}
		if we, ok := child.(*grammar.WindowExprContext); ok {
			wd.Window, err = convertWindowExpr(pr, we)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertWindowExpr(pr *nanopass.ParseResult, ctx *grammar.WindowExprContext) (ws WindowSpec, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		switch c := child.(type) {
		case *grammar.WinPartitionByClauseContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if cel, ok := c.GetChild(j).(*grammar.ColumnExprListContext); ok {
					ws.PartitionBy, err = convertColumnExprList(pr, cel)
					if err != nil {
						return
					}
				}
			}
		case *grammar.WinOrderByClauseContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if oel, ok := c.GetChild(j).(*grammar.OrderExprListContext); ok {
					ws.OrderBy, err = convertOrderExprList(pr, oel)
					if err != nil {
						return
					}
				}
			}
		case *grammar.WinFrameClauseContext:
			frame, frameErr := convertWinFrameClause(pr, c)
			if frameErr != nil {
				err = frameErr
				return
			}
			ws.Frame = &frame
		}
	}
	return
}

func convertWinFrameClause(pr *nanopass.ParseResult, ctx *grammar.WinFrameClauseContext) (wf WindowFrame, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar.ClickHouseLexerROWS:
				wf.Unit = "ROWS"
			case grammar.ClickHouseLexerRANGE:
				wf.Unit = "RANGE"
			}
		}
		switch c := child.(type) {
		case *grammar.FrameStartContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if wfb, ok := c.GetChild(j).(*grammar.WinFrameBoundContext); ok {
					wf.Start = convertFrameBound(pr, wfb)
				}
			}
		case *grammar.FrameBetweenContext:
			bounds := make([]FrameBound, 0, 2)
			for j := 0; j < c.GetChildCount(); j++ {
				if wfb, ok := c.GetChild(j).(*grammar.WinFrameBoundContext); ok {
					bounds = append(bounds, convertFrameBound(pr, wfb))
				}
			}
			if len(bounds) >= 1 {
				wf.Start = bounds[0]
			}
			if len(bounds) >= 2 {
				wf.End = &bounds[1]
			}
		}
	}
	return
}
func convertWinFrameExtend(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext, wf *WindowFrame) (err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		switch c := child.(type) {
		case *grammar.FrameStartContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if wfb, ok := c.GetChild(j).(*grammar.WinFrameBoundContext); ok {
					wf.Start = convertFrameBound(pr, wfb)
				}
			}
		case *grammar.FrameBetweenContext:
			bounds := make([]FrameBound, 0, 2)
			for j := 0; j < c.GetChildCount(); j++ {
				if wfb, ok := c.GetChild(j).(*grammar.WinFrameBoundContext); ok {
					bounds = append(bounds, convertFrameBound(pr, wfb))
				}
			}
			if len(bounds) >= 1 {
				wf.Start = bounds[0]
			}
			if len(bounds) >= 2 {
				wf.End = &bounds[1]
			}
		default:
			if prc, ok := child.(antlr.ParserRuleContext); ok {
				err = convertWinFrameExtend(pr, prc, wf)
				if err != nil {
					return
				}
			}
		}
	}
	return
}

func convertFrameBound(pr *nanopass.ParseResult, ctx *grammar.WinFrameBoundContext) (fb FrameBound) {
	hasCurrent := false
	hasUnbounded := false
	hasPreceding := false
	hasFollowing := false
	numText := ""

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar.ClickHouseLexerCURRENT:
				hasCurrent = true
			case grammar.ClickHouseLexerUNBOUNDED:
				hasUnbounded = true
			case grammar.ClickHouseLexerPRECEDING:
				hasPreceding = true
			case grammar.ClickHouseLexerFOLLOWING:
				hasFollowing = true
			}
		}
		if nl, ok := child.(*grammar.NumberLiteralContext); ok {
			numText = nanopass.NodeText(pr, nl)
		}
	}

	switch {
	case hasCurrent:
		fb.Kind = "CURRENT_ROW"
	case hasUnbounded && hasPreceding:
		fb.Kind = "UNBOUNDED_PRECEDING"
	case hasUnbounded && hasFollowing:
		fb.Kind = "UNBOUNDED_FOLLOWING"
	case hasPreceding:
		fb.Kind = "N_PRECEDING"
		fb.N = numText
	case hasFollowing:
		fb.Kind = "N_FOLLOWING"
		fb.N = numText
	}
	return
}

// --- FROM / JOIN ---

func convertFromClause(pr *nanopass.ParseResult, ctx *grammar.FromClauseContext) (je JoinExpr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if jec, ok := ctx.GetChild(i).(grammar.IJoinExprContext); ok {
			return convertJoinExpr(pr, jec.(antlr.ParserRuleContext))
		}
	}
	err = eh.Errorf("convertFromClause: no joinExpr found")
	return
}

func convertJoinExpr(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (je JoinExpr, err error) {
	switch c := ctx.(type) {
	case *grammar.JoinExprTableContext:
		return convertJoinExprTable(pr, c)
	case *grammar.JoinExprOpContext:
		return convertJoinExprOp(pr, c)
	case *grammar.JoinExprCrossOpContext:
		return convertJoinExprCrossOp(pr, c)
	case *grammar.JoinExprParensContext:
		// Unwrap parenthesized joinExpr
		for i := 0; i < c.GetChildCount(); i++ {
			if inner, ok := c.GetChild(i).(grammar.IJoinExprContext); ok {
				return convertJoinExpr(pr, inner.(antlr.ParserRuleContext))
			}
		}
		err = eh.Errorf("convertJoinExpr: empty JoinExprParens")
	default:
		err = eh.Errorf("convertJoinExpr: unsupported type %T", ctx)
	}
	return
}

func convertJoinExprTable(pr *nanopass.ParseResult, ctx *grammar.JoinExprTableContext) (je JoinExpr, err error) {
	je.Kind = JoinExprTable
	td := &JoinTableData{}

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if te, ok := child.(grammar.ITableExprContext); ok {
			err = convertTableExpr(pr, te.(antlr.ParserRuleContext), td)
			if err != nil {
				return
			}
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar.ClickHouseLexerFINAL {
				td.Final = true
			}
		}
		if sc, ok := child.(*grammar.SampleClauseContext); ok {
			sample, sErr := convertSampleClause(pr, sc)
			if sErr != nil {
				err = sErr
				return
			}
			td.Sample = &sample
		}
	}

	je.Table = td
	return
}

func convertTableExpr(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext, td *JoinTableData) (err error) {
	switch c := ctx.(type) {
	case *grammar.TableExprIdentifierContext:
		td.TableKind = "ref"
		for i := 0; i < c.GetChildCount(); i++ {
			if ti, ok := c.GetChild(i).(*grammar.TableIdentifierContext); ok {
				td.Database, td.Table = extractTableIdentifier(ti)
			}
		}
	case *grammar.TableExprFunctionContext:
		td.TableKind = "func"
		for i := 0; i < c.GetChildCount(); i++ {
			if tfe, ok := c.GetChild(i).(*grammar.TableFunctionExprContext); ok {
				td.FuncName, td.FuncArgs, err = convertTableFunctionExpr(pr, tfe)
				if err != nil {
					return
				}
			}
		}
	case *grammar.TableExprSubqueryContext:
		td.TableKind = "subquery"
		for i := 0; i < c.GetChildCount(); i++ {
			if sus, ok := c.GetChild(i).(*grammar.SelectUnionStmtContext); ok {
				su, suErr := convertSelectUnion(pr, sus)
				if suErr != nil {
					err = suErr
					return
				}
				td.Subquery = &su
			}
		}
	case *grammar.TableExprAliasContext:
		// Recurse into inner tableExpr, then extract alias
		for i := 0; i < c.GetChildCount(); i++ {
			child := c.GetChild(i)
			if innerTE, ok := child.(grammar.ITableExprContext); ok {
				err = convertTableExpr(pr, innerTE.(antlr.ParserRuleContext), td)
				if err != nil {
					return
				}
			}
			if ident, ok := child.(*grammar.IdentifierContext); ok {
				td.Alias = identText(ident)
			}
			if al, ok := child.(*grammar.AliasContext); ok {
				td.Alias = aliasText(al)
			}
		}
	default:
		err = eh.Errorf("convertTableExpr: unsupported type %T", ctx)
	}
	return
}

func convertTableFunctionExpr(pr *nanopass.ParseResult, ctx *grammar.TableFunctionExprContext) (name string, args []Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ident, ok := child.(*grammar.IdentifierContext); ok {
			name = identText(ident)
		}
		if tal, ok := child.(*grammar.TableArgListContext); ok {
			for j := 0; j < tal.GetChildCount(); j++ {
				if tae, ok := tal.GetChild(j).(*grammar.TableArgExprContext); ok {
					// Table args can be identifiers, functions, or literals
					arg := Expr{Kind: KindLiteral, Literal: &LiteralData{SQL: nanopass.NodeText(pr, tae)}}
					args = append(args, arg)
				}
			}
		}
	}
	return
}

func convertJoinExprOp(pr *nanopass.ParseResult, ctx *grammar.JoinExprOpContext) (je JoinExpr, err error) {
	je.Kind = JoinExprOp
	op := &JoinOpData{}

	joinExprs := make([]antlr.ParserRuleContext, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if jec, ok := child.(grammar.IJoinExprContext); ok {
			joinExprs = append(joinExprs, jec.(antlr.ParserRuleContext))
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar.ClickHouseLexerGLOBAL:
				op.Global = true
			case grammar.ClickHouseLexerLOCAL:
				op.Local = true
			}
		}
		if jo, ok := child.(grammar.IJoinOpContext); ok {
			op.Kind, op.Strictness = extractJoinOp(jo.(antlr.ParserRuleContext))
		}
		if jcc, ok := child.(*grammar.JoinConstraintClauseContext); ok {
			op.Constraint, err = convertJoinConstraint(pr, jcc)
			if err != nil {
				return
			}
		}
	}

	if len(joinExprs) >= 1 {
		op.Left, err = convertJoinExpr(pr, joinExprs[0])
		if err != nil {
			return
		}
	}
	if len(joinExprs) >= 2 {
		op.Right, err = convertJoinExpr(pr, joinExprs[1])
		if err != nil {
			return
		}
	}

	// Default to INNER if no explicit join kind
	if op.Kind == "" {
		op.Kind = "INNER"
	}

	je.Op = op
	return
}

func convertJoinExprCrossOp(pr *nanopass.ParseResult, ctx *grammar.JoinExprCrossOpContext) (je JoinExpr, err error) {
	je.Kind = JoinExprCross
	cross := &JoinCrossData{}

	joinExprs := make([]antlr.ParserRuleContext, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if jec, ok := child.(grammar.IJoinExprContext); ok {
			joinExprs = append(joinExprs, jec.(antlr.ParserRuleContext))
		}
		if joc, ok := child.(*grammar.JoinOpCrossContext); ok {
			for j := 0; j < joc.GetChildCount(); j++ {
				if term, ok := joc.GetChild(j).(*antlr.TerminalNodeImpl); ok {
					switch term.GetSymbol().GetTokenType() {
					case grammar.ClickHouseLexerGLOBAL:
						cross.Global = true
					case grammar.ClickHouseLexerLOCAL:
						cross.Local = true
					}
				}
			}
		}
	}

	if len(joinExprs) >= 1 {
		cross.Left, err = convertJoinExpr(pr, joinExprs[0])
		if err != nil {
			return
		}
	}
	if len(joinExprs) >= 2 {
		cross.Right, err = convertJoinExpr(pr, joinExprs[1])
		if err != nil {
			return
		}
	}

	je.Cross = cross
	return
}

func extractJoinOp(ctx antlr.ParserRuleContext) (kind, strictness string) {
	kind = "INNER" // default
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar.ClickHouseLexerINNER:
				kind = "INNER"
			case grammar.ClickHouseLexerLEFT:
				kind = "LEFT"
			case grammar.ClickHouseLexerRIGHT:
				kind = "RIGHT"
			case grammar.ClickHouseLexerFULL:
				kind = "FULL"
			case grammar.ClickHouseLexerALL:
				strictness = "ALL"
			case grammar.ClickHouseLexerANY:
				strictness = "ANY"
			case grammar.ClickHouseLexerSEMI:
				strictness = "SEMI"
			case grammar.ClickHouseLexerANTI:
				strictness = "ANTI"
			case grammar.ClickHouseLexerASOF:
				strictness = "ASOF"
			}
		}
	}
	return
}

func convertJoinConstraint(pr *nanopass.ParseResult, ctx *grammar.JoinConstraintClauseContext) (jc JoinConstraint, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar.ClickHouseLexerON:
				jc.Kind = "ON"
			case grammar.ClickHouseLexerUSING:
				jc.Kind = "USING"
			}
		}
		if cel, ok := child.(*grammar.ColumnExprListContext); ok {
			jc.Exprs, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertSampleClause(pr *nanopass.ParseResult, ctx *grammar.SampleClauseContext) (sc SampleClause, err error) {
	ratios := make([]RatioExpr, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if re, ok := ctx.GetChild(i).(*grammar.RatioExprContext); ok {
			ratios = append(ratios, convertRatioExpr(pr, re))
		}
	}
	if len(ratios) >= 1 {
		sc.Ratio = ratios[0]
	}
	if len(ratios) >= 2 {
		sc.Offset = &ratios[1]
	}
	return
}

func convertRatioExpr(pr *nanopass.ParseResult, ctx *grammar.RatioExprContext) (re RatioExpr) {
	nums := make([]string, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if nl, ok := ctx.GetChild(i).(*grammar.NumberLiteralContext); ok {
			nums = append(nums, nanopass.NodeText(pr, nl))
		}
	}
	if len(nums) >= 1 {
		re.Numerator = nums[0]
	}
	if len(nums) >= 2 {
		re.Denominator = nums[1]
	}
	return
}

// --- Column expression list ---

func convertColumnExprList(pr *nanopass.ParseResult, ctx *grammar.ColumnExprListContext) (exprs []Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		switch c := child.(type) {
		case *grammar.ColumnsExprColumnContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if ce, ok := c.GetChild(j).(grammar.IColumnExprContext); ok {
					expr, exprErr := convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
					if exprErr != nil {
						err = exprErr
						return
					}
					exprs = append(exprs, expr)
				}
			}
		case *grammar.ColumnsExprAsteriskContext:
			exprs = append(exprs, Expr{
				Kind:     KindAsterisk,
				Asterisk: &AsteriskData{Table: extractAsteriskTable(c)},
			})
		case *grammar.ColumnsExprSubqueryContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if sus, ok := c.GetChild(j).(*grammar.SelectUnionStmtContext); ok {
					su, suErr := convertSelectUnion(pr, sus)
					if suErr != nil {
						err = suErr
						return
					}
					exprs = append(exprs, Expr{
						Kind:     KindSubquery,
						Subquery: &SubqueryData{Query: su},
					})
				}
			}
		}
	}
	return
}

// --- Helpers ---

func identText(ctx *grammar.IdentifierContext) string {
	return normalizeIdentifier(ctx.GetText())
}

func aliasText(ctx *grammar.AliasContext) string {
	return normalizeIdentifier(ctx.GetText())
}

func identTextFromNode(ctx antlr.ParserRuleContext) string {
	return normalizeIdentifier(ctx.GetText())
}

func normalizeIdentifier(s string) string {
	// Strip existing quotes (backtick or double-quote)
	if len(s) >= 2 {
		if (s[0] == '`' && s[len(s)-1] == '`') || (s[0] == '"' && s[len(s)-1] == '"') {
			s = s[1 : len(s)-1]
		}
	}
	return s
}

func unquoteString(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}

func extractTableIdentifier(ctx *grammar.TableIdentifierContext) (database, table string) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if db, ok := child.(*grammar.DatabaseIdentifierContext); ok {
			database = identTextFromNode(db)
		}
		if ident, ok := child.(*grammar.IdentifierContext); ok {
			table = identText(ident)
		}
	}
	return
}

func extractAsteriskTable(ctx *grammar.ColumnsExprAsteriskContext) string {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ti, ok := ctx.GetChild(i).(*grammar.TableIdentifierContext); ok {
			_, table := extractTableIdentifier(ti)
			return table
		}
	}
	return ""
}

func extractDynamicPattern(ctx *grammar.DynamicColumnSelectionContext) string {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar.ClickHouseLexerSTRING_LITERAL {
				return unquoteString(term.GetText())
			}
		}
	}
	return ""
}

// Ensure imports are used
var _ = fmt.Sprintf
var _ = strings.ToLower
var _ = strconv.Itoa
