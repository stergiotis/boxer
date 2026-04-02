//go:build llm_generated_opus46

package ast

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
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
	var queryStmtCtx *grammar1.QueryStmtContext
	for i := 0; i < pr.Tree.GetChildCount(); i++ {
		if qs, ok := pr.Tree.GetChild(i).(*grammar1.QueryStmtContext); ok {
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
		case *grammar1.QueryStmtContext:
			return convertQueryStmt(pr, ctx)
		case *grammar1.QueryContext:
			return convertQuery(pr, ctx)
		case *grammar1.SelectUnionStmtContext:
			query.Body, err = convertSelectUnion(pr, ctx)
			return
		}
	}
	err = eh.Errorf("ConvertCSTToAST: no query found in tree")
	return
}

func convertQueryStmt(pr *nanopass.ParseResult, ctx *grammar1.QueryStmtContext) (query Query, err error) {
	// Find query child
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar1.QueryContext:
			query, err = convertQuery(pr, c)
			if err != nil {
				return
			}
		}
	}

	// INTO OUTFILE
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerOUTFILE {
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
			if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerFORMAT {
				if i+1 < ctx.GetChildCount() {
					if identCtx, ok := ctx.GetChild(i + 1).(*grammar1.IdentifierOrNullContext); ok {
						query.Format = normalizeIdentifier(identCtx.GetText())
					}
				}
			}
		}
	}

	return
}

func convertQuery(pr *nanopass.ParseResult, ctx *grammar1.QueryContext) (query Query, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar1.SetStmtContext:
			settings, setErr := convertSetStmt(pr, c)
			if setErr != nil {
				err = eh.Errorf("convertQuery: %w", setErr)
				return
			}
			query.Settings = append(query.Settings, settings...)

		case *grammar1.CtesContext:
			query.CTEs, err = convertCTEs(pr, c)
			if err != nil {
				return
			}

		case *grammar1.SelectUnionStmtContext:
			query.Body, err = convertSelectUnion(pr, c)
			if err != nil {
				return
			}
		}
	}
	return
}

// --- SET ---

func convertSetStmt(pr *nanopass.ParseResult, ctx *grammar1.SetStmtContext) (settings []SettingPair, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if sel, ok := ctx.GetChild(i).(*grammar1.SettingExprListContext); ok {
			settings, err = convertSettingExprList(pr, sel)
			return
		}
	}
	return
}

func convertSettingExprList(pr *nanopass.ParseResult, ctx *grammar1.SettingExprListContext) (settings []SettingPair, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if se, ok := ctx.GetChild(i).(*grammar1.SettingExprContext); ok {
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

func convertSettingExpr(pr *nanopass.ParseResult, ctx *grammar1.SettingExprContext) (pair SettingPair, err error) {
	// identifier EQ_SINGLE settingValue
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar1.IdentifierContext); ok {
			pair.Key = identText(ident)
		}
	}
	// settingValue is the last child — take its full text
	for i := 0; i < ctx.GetChildCount(); i++ {
		if _, ok := ctx.GetChild(i).(grammar1.ISettingValueContext); ok {
			pair.ValueSQL = nanopass.NodeText(pr, ctx.GetChild(i).(antlr.ParserRuleContext))
		}
	}
	return
}

// --- CTEs ---

func convertCTEs(pr *nanopass.ParseResult, ctx *grammar1.CtesContext) (ctes []CTE, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if nq, ok := ctx.GetChild(i).(*grammar1.NamedQueryContext); ok {
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

func convertNamedQuery(pr *nanopass.ParseResult, ctx *grammar1.NamedQueryContext) (cte CTE, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar1.IdentifierContext:
			if cte.Name == "" {
				cte.Name = identText(c)
			}
		case *grammar1.ColumnAliasesContext:
			cte.ColumnAliases = convertColumnAliases(c)
		case *grammar1.QueryContext:
			cte.Body, err = convertQuery(pr, c)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertColumnAliases(ctx *grammar1.ColumnAliasesContext) (aliases []string) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar1.IdentifierContext); ok {
			aliases = append(aliases, identText(ident))
		}
	}
	return
}

// --- SELECT UNION ---

func convertSelectUnion(pr *nanopass.ParseResult, ctx *grammar1.SelectUnionStmtContext) (su SelectUnion, err error) {
	first := true
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar1.SelectStmtWithParensContext:
			if first {
				su.Head, err = convertSelectStmtWithParens(pr, c)
				if err != nil {
					return
				}
				first = false
			}
		case *grammar1.SelectUnionStmtItemContext:
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

func convertSelectUnionItem(pr *nanopass.ParseResult, ctx *grammar1.SelectUnionStmtItemContext) (item SelectUnionItem, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar1.ClickHouseLexerUNION:
				item.Op = "UNION"
			case grammar1.ClickHouseLexerEXCEPT:
				item.Op = "EXCEPT"
			case grammar1.ClickHouseLexerINTERSECT:
				item.Op = "INTERSECT"
			case grammar1.ClickHouseLexerALL:
				item.Modifier = "ALL"
			case grammar1.ClickHouseLexerDISTINCT:
				item.Modifier = "DISTINCT"
			}
		}
		if swp, ok := child.(*grammar1.SelectStmtWithParensContext); ok {
			item.Select, err = convertSelectStmtWithParens(pr, swp)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertSelectStmtWithParens(pr *nanopass.ParseResult, ctx *grammar1.SelectStmtWithParensContext) (sel Select, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar1.SelectStmtContext:
			return convertSelectStmt(pr, c)
		case *grammar1.SelectUnionStmtContext:
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

func convertSelectStmt(pr *nanopass.ParseResult, ctx *grammar1.SelectStmtContext) (sel Select, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		switch c := child.(type) {
		case *grammar1.ProjectionClauseContext:
			err = convertProjectionClause(pr, c, &sel)
		case *grammar1.WithClauseContext:
			sel.With, err = convertWithClause(pr, c)
		case *grammar1.FromClauseContext:
			var je JoinExpr
			je, err = convertFromClause(pr, c)
			sel.From = &je
		case *grammar1.ArrayJoinClauseContext:
			var aj ArrayJoinClause
			aj, err = convertArrayJoinClause(pr, c)
			sel.ArrayJoin = &aj
		case *grammar1.WindowClauseContext:
			var wd WindowDefClause
			wd, err = convertWindowClause(pr, c)
			sel.WindowDef = &wd
		case *grammar1.QualifyClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Qualify = &expr
		case *grammar1.PrewhereClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Prewhere = &expr
		case *grammar1.WhereClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Where = &expr
		case *grammar1.GroupByClauseContext:
			var gb GroupByClause
			gb, err = convertGroupByClause(pr, c)
			sel.GroupBy = &gb
		case *grammar1.HavingClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Having = &expr
		case *grammar1.OrderByClauseContext:
			var ob OrderByClause
			ob, err = convertOrderByClause(pr, c)
			sel.OrderBy = &ob
		case *grammar1.LimitByClauseContext:
			var lb LimitByClause
			lb, err = convertLimitByClause(pr, c)
			sel.LimitBy = &lb
		case *grammar1.LimitClauseContext:
			var lc LimitClause
			lc, err = convertLimitClause(pr, c)
			sel.Limit = &lc
		case *grammar1.SettingsClauseContext:
			sel.Settings, err = convertSettingsClause(pr, c)
		}
		if err != nil {
			return
		}
	}
	return
}

func convertProjectionClause(pr *nanopass.ParseResult, ctx *grammar1.ProjectionClauseContext, sel *Select) (err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerDISTINCT {
				sel.Distinct = true
			}
		}
		if tc, ok := child.(*grammar1.TopClauseContext); ok {
			top, topErr := convertTopClause(tc)
			if topErr != nil {
				err = topErr
				return
			}
			sel.Top = &top
		}
		if cel, ok := child.(*grammar1.ColumnExprListContext); ok {
			sel.Projection, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
		if pec, ok := child.(*grammar1.ProjectionExceptClauseContext); ok {
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

func convertTopClause(ctx *grammar1.TopClauseContext) (top TopClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar1.ClickHouseLexerDECIMAL_LITERAL:
				top.N, err = strconv.ParseUint(term.GetText(), 10, 64)
				if err != nil {
					err = eh.Errorf("convertTopClause: %w", err)
					return
				}
			case grammar1.ClickHouseLexerTIES:
				top.WithTies = true
			}
		}
	}
	return
}

func convertProjectionExceptClause(pr *nanopass.ParseResult, ctx *grammar1.ProjectionExceptClauseContext) (exc ExceptClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		switch c := child.(type) {
		case *grammar1.StaticColumnListContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if ident, ok := c.GetChild(j).(*grammar1.IdentifierContext); ok {
					exc.Static = append(exc.Static, identText(ident))
				}
			}
		case *grammar1.DynamicColumnListContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if dcs, ok := c.GetChild(j).(*grammar1.DynamicColumnSelectionContext); ok {
					exc.Dynamic = extractDynamicPattern(dcs)
				}
			}
		}
	}
	return
}

func convertWithClause(pr *nanopass.ParseResult, ctx *grammar1.WithClauseContext) (exprs []Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if cel, ok := ctx.GetChild(i).(*grammar1.ColumnExprListContext); ok {
			exprs, err = convertColumnExprList(pr, cel)
			return
		}
	}
	return
}

func convertSingleExprClause(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (expr Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar1.IColumnExprContext); ok {
			return convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
		}
	}
	err = eh.Errorf("convertSingleExprClause: no column expression found in %T", ctx)
	return
}

// --- GROUP BY ---

func convertGroupByClause(pr *nanopass.ParseResult, ctx *grammar1.GroupByClauseContext) (gb GroupByClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar1.ClickHouseLexerCUBE:
				gb.Modifier = "CUBE"
			case grammar1.ClickHouseLexerROLLUP:
				gb.Modifier = "ROLLUP"
			case grammar1.ClickHouseLexerTOTALS:
				gb.WithTotals = true
			}
		}
		if cel, ok := child.(*grammar1.ColumnExprListContext); ok {
			gb.Exprs, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

// --- ORDER BY ---

func convertOrderByClause(pr *nanopass.ParseResult, ctx *grammar1.OrderByClauseContext) (ob OrderByClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if oel, ok := child.(*grammar1.OrderExprListContext); ok {
			ob.Items, err = convertOrderExprList(pr, oel)
			if err != nil {
				return
			}
		}
		// TODO: WITH FILL parsing
	}
	return
}

func convertOrderExpr(pr *nanopass.ParseResult, ctx *grammar1.OrderExprContext) (item OrderItem, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar1.IColumnExprContext); ok {
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
			case grammar1.ClickHouseLexerASCENDING:
				item.Dir = "ASC"
			case grammar1.ClickHouseLexerDESCENDING:
				item.Dir = "DESC"
			case grammar1.ClickHouseLexerDESC:
				item.Dir = "DESC"
			case grammar1.ClickHouseLexerNULLS:
				// skip
			case grammar1.ClickHouseLexerFIRST:
				item.Nulls = "FIRST"
			case grammar1.ClickHouseLexerLAST:
				item.Nulls = "LAST"
			case grammar1.ClickHouseLexerSTRING_LITERAL:
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

func convertOrderExprList(pr *nanopass.ParseResult, ctx *grammar1.OrderExprListContext) (items []OrderItem, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if oe, ok := ctx.GetChild(i).(*grammar1.OrderExprContext); ok {
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

func convertLimitClause(pr *nanopass.ParseResult, ctx *grammar1.LimitClauseContext) (lc LimitClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if le, ok := child.(*grammar1.LimitExprContext); ok {
			lc.Limit, err = convertLimitExpr(pr, le)
			if err != nil {
				return
			}
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerTIES {
				lc.WithTies = true
			}
		}
	}
	return
}

func convertLimitByClause(pr *nanopass.ParseResult, ctx *grammar1.LimitByClauseContext) (lb LimitByClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if le, ok := child.(*grammar1.LimitExprContext); ok {
			lb.Limit, err = convertLimitExpr(pr, le)
			if err != nil {
				return
			}
		}
		if cel, ok := child.(*grammar1.ColumnExprListContext); ok {
			lb.Columns, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertLimitExpr(pr *nanopass.ParseResult, ctx *grammar1.LimitExprContext) (ls LimitSpec, err error) {
	exprs := make([]Expr, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar1.IColumnExprContext); ok {
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

func convertSettingsClause(pr *nanopass.ParseResult, ctx *grammar1.SettingsClauseContext) (settings []SettingPair, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if sel, ok := ctx.GetChild(i).(*grammar1.SettingExprListContext); ok {
			return convertSettingExprList(pr, sel)
		}
	}
	return
}

// --- ARRAY JOIN ---

func convertArrayJoinClause(pr *nanopass.ParseResult, ctx *grammar1.ArrayJoinClauseContext) (aj ArrayJoinClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar1.ClickHouseLexerLEFT:
				aj.Kind = "LEFT"
			case grammar1.ClickHouseLexerINNER:
				aj.Kind = "INNER"
			}
		}
		if cel, ok := child.(*grammar1.ColumnExprListContext); ok {
			aj.Exprs, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

// --- WINDOW ---

func convertWindowClause(pr *nanopass.ParseResult, ctx *grammar1.WindowClauseContext) (wd WindowDefClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ident, ok := child.(*grammar1.IdentifierContext); ok {
			wd.Name = identText(ident)
		}
		if we, ok := child.(*grammar1.WindowExprContext); ok {
			wd.Window, err = convertWindowExpr(pr, we)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertWindowExpr(pr *nanopass.ParseResult, ctx *grammar1.WindowExprContext) (ws WindowSpec, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		switch c := child.(type) {
		case *grammar1.WinPartitionByClauseContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if cel, ok := c.GetChild(j).(*grammar1.ColumnExprListContext); ok {
					ws.PartitionBy, err = convertColumnExprList(pr, cel)
					if err != nil {
						return
					}
				}
			}
		case *grammar1.WinOrderByClauseContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if oel, ok := c.GetChild(j).(*grammar1.OrderExprListContext); ok {
					ws.OrderBy, err = convertOrderExprList(pr, oel)
					if err != nil {
						return
					}
				}
			}
		case *grammar1.WinFrameClauseContext:
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

func convertWinFrameClause(pr *nanopass.ParseResult, ctx *grammar1.WinFrameClauseContext) (wf WindowFrame, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar1.ClickHouseLexerROWS:
				wf.Unit = "ROWS"
			case grammar1.ClickHouseLexerRANGE:
				wf.Unit = "RANGE"
			}
		}
		switch c := child.(type) {
		case *grammar1.FrameStartContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if wfb, ok := c.GetChild(j).(*grammar1.WinFrameBoundContext); ok {
					wf.Start = convertFrameBound(pr, wfb)
				}
			}
		case *grammar1.FrameBetweenContext:
			bounds := make([]FrameBound, 0, 2)
			for j := 0; j < c.GetChildCount(); j++ {
				if wfb, ok := c.GetChild(j).(*grammar1.WinFrameBoundContext); ok {
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
		case *grammar1.FrameStartContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if wfb, ok := c.GetChild(j).(*grammar1.WinFrameBoundContext); ok {
					wf.Start = convertFrameBound(pr, wfb)
				}
			}
		case *grammar1.FrameBetweenContext:
			bounds := make([]FrameBound, 0, 2)
			for j := 0; j < c.GetChildCount(); j++ {
				if wfb, ok := c.GetChild(j).(*grammar1.WinFrameBoundContext); ok {
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

func convertFrameBound(pr *nanopass.ParseResult, ctx *grammar1.WinFrameBoundContext) (fb FrameBound) {
	hasCurrent := false
	hasUnbounded := false
	hasPreceding := false
	hasFollowing := false
	numText := ""

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar1.ClickHouseLexerCURRENT:
				hasCurrent = true
			case grammar1.ClickHouseLexerUNBOUNDED:
				hasUnbounded = true
			case grammar1.ClickHouseLexerPRECEDING:
				hasPreceding = true
			case grammar1.ClickHouseLexerFOLLOWING:
				hasFollowing = true
			}
		}
		if nl, ok := child.(*grammar1.NumberLiteralContext); ok {
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

func convertFromClause(pr *nanopass.ParseResult, ctx *grammar1.FromClauseContext) (je JoinExpr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if jec, ok := ctx.GetChild(i).(grammar1.IJoinExprContext); ok {
			return convertJoinExpr(pr, jec.(antlr.ParserRuleContext))
		}
	}
	err = eh.Errorf("convertFromClause: no joinExpr found")
	return
}

func convertJoinExpr(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (je JoinExpr, err error) {
	switch c := ctx.(type) {
	case *grammar1.JoinExprTableContext:
		return convertJoinExprTable(pr, c)
	case *grammar1.JoinExprOpContext:
		return convertJoinExprOp(pr, c)
	case *grammar1.JoinExprCrossOpContext:
		return convertJoinExprCrossOp(pr, c)
	case *grammar1.JoinExprParensContext:
		// Unwrap parenthesized joinExpr
		for i := 0; i < c.GetChildCount(); i++ {
			if inner, ok := c.GetChild(i).(grammar1.IJoinExprContext); ok {
				return convertJoinExpr(pr, inner.(antlr.ParserRuleContext))
			}
		}
		err = eh.Errorf("convertJoinExpr: empty JoinExprParens")
	default:
		err = eh.Errorf("convertJoinExpr: unsupported type %T", ctx)
	}
	return
}

func convertJoinExprTable(pr *nanopass.ParseResult, ctx *grammar1.JoinExprTableContext) (je JoinExpr, err error) {
	je.Kind = JoinExprTable
	td := &JoinTableData{}

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if te, ok := child.(grammar1.ITableExprContext); ok {
			err = convertTableExpr(pr, te.(antlr.ParserRuleContext), td)
			if err != nil {
				return
			}
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerFINAL {
				td.Final = true
			}
		}
		if sc, ok := child.(*grammar1.SampleClauseContext); ok {
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
	case *grammar1.TableExprIdentifierContext:
		td.TableKind = "ref"
		for i := 0; i < c.GetChildCount(); i++ {
			if ti, ok := c.GetChild(i).(*grammar1.TableIdentifierContext); ok {
				td.Database, td.Table = extractTableIdentifier(ti)
			}
		}
	case *grammar1.TableExprFunctionContext:
		td.TableKind = "func"
		for i := 0; i < c.GetChildCount(); i++ {
			if tfe, ok := c.GetChild(i).(*grammar1.TableFunctionExprContext); ok {
				td.FuncName, td.FuncArgs, err = convertTableFunctionExpr(pr, tfe)
				if err != nil {
					return
				}
			}
		}
	case *grammar1.TableExprSubqueryContext:
		td.TableKind = "subquery"
		for i := 0; i < c.GetChildCount(); i++ {
			if sus, ok := c.GetChild(i).(*grammar1.SelectUnionStmtContext); ok {
				su, suErr := convertSelectUnion(pr, sus)
				if suErr != nil {
					err = suErr
					return
				}
				td.Subquery = &su
			}
		}
	case *grammar1.TableExprAliasContext:
		// Recurse into inner tableExpr, then extract alias
		for i := 0; i < c.GetChildCount(); i++ {
			child := c.GetChild(i)
			if innerTE, ok := child.(grammar1.ITableExprContext); ok {
				err = convertTableExpr(pr, innerTE.(antlr.ParserRuleContext), td)
				if err != nil {
					return
				}
			}
			if ident, ok := child.(*grammar1.IdentifierContext); ok {
				td.Alias = identText(ident)
			}
			if al, ok := child.(*grammar1.AliasContext); ok {
				td.Alias = aliasText(al)
			}
		}
	default:
		err = eh.Errorf("convertTableExpr: unsupported type %T", ctx)
	}
	return
}

func convertTableFunctionExpr(pr *nanopass.ParseResult, ctx *grammar1.TableFunctionExprContext) (name string, args []Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ident, ok := child.(*grammar1.IdentifierContext); ok {
			name = identText(ident)
		}
		if tal, ok := child.(*grammar1.TableArgListContext); ok {
			for j := 0; j < tal.GetChildCount(); j++ {
				if tae, ok := tal.GetChild(j).(*grammar1.TableArgExprContext); ok {
					// Table args can be identifiers, functions, or literals
					arg := Expr{Kind: KindLiteral, Literal: &LiteralData{SQL: nanopass.NodeText(pr, tae)}}
					args = append(args, arg)
				}
			}
		}
	}
	return
}

func convertJoinExprOp(pr *nanopass.ParseResult, ctx *grammar1.JoinExprOpContext) (je JoinExpr, err error) {
	je.Kind = JoinExprOp
	op := &JoinOpData{}

	joinExprs := make([]antlr.ParserRuleContext, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if jec, ok := child.(grammar1.IJoinExprContext); ok {
			joinExprs = append(joinExprs, jec.(antlr.ParserRuleContext))
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar1.ClickHouseLexerGLOBAL:
				op.Global = true
			case grammar1.ClickHouseLexerLOCAL:
				op.Local = true
			}
		}
		if jo, ok := child.(grammar1.IJoinOpContext); ok {
			op.Kind, op.Strictness = extractJoinOp(jo.(antlr.ParserRuleContext))
		}
		if jcc, ok := child.(*grammar1.JoinConstraintClauseContext); ok {
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

func convertJoinExprCrossOp(pr *nanopass.ParseResult, ctx *grammar1.JoinExprCrossOpContext) (je JoinExpr, err error) {
	je.Kind = JoinExprCross
	cross := &JoinCrossData{}

	joinExprs := make([]antlr.ParserRuleContext, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if jec, ok := child.(grammar1.IJoinExprContext); ok {
			joinExprs = append(joinExprs, jec.(antlr.ParserRuleContext))
		}
		if joc, ok := child.(*grammar1.JoinOpCrossContext); ok {
			for j := 0; j < joc.GetChildCount(); j++ {
				if term, ok := joc.GetChild(j).(*antlr.TerminalNodeImpl); ok {
					switch term.GetSymbol().GetTokenType() {
					case grammar1.ClickHouseLexerGLOBAL:
						cross.Global = true
					case grammar1.ClickHouseLexerLOCAL:
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
			case grammar1.ClickHouseLexerINNER:
				kind = "INNER"
			case grammar1.ClickHouseLexerLEFT:
				kind = "LEFT"
			case grammar1.ClickHouseLexerRIGHT:
				kind = "RIGHT"
			case grammar1.ClickHouseLexerFULL:
				kind = "FULL"
			case grammar1.ClickHouseLexerALL:
				strictness = "ALL"
			case grammar1.ClickHouseLexerANY:
				strictness = "ANY"
			case grammar1.ClickHouseLexerSEMI:
				strictness = "SEMI"
			case grammar1.ClickHouseLexerANTI:
				strictness = "ANTI"
			case grammar1.ClickHouseLexerASOF:
				strictness = "ASOF"
			}
		}
	}
	return
}

func convertJoinConstraint(pr *nanopass.ParseResult, ctx *grammar1.JoinConstraintClauseContext) (jc JoinConstraint, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar1.ClickHouseLexerON:
				jc.Kind = "ON"
			case grammar1.ClickHouseLexerUSING:
				jc.Kind = "USING"
			}
		}
		if cel, ok := child.(*grammar1.ColumnExprListContext); ok {
			jc.Exprs, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertSampleClause(pr *nanopass.ParseResult, ctx *grammar1.SampleClauseContext) (sc SampleClause, err error) {
	ratios := make([]RatioExpr, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if re, ok := ctx.GetChild(i).(*grammar1.RatioExprContext); ok {
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

func convertRatioExpr(pr *nanopass.ParseResult, ctx *grammar1.RatioExprContext) (re RatioExpr) {
	nums := make([]string, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if nl, ok := ctx.GetChild(i).(*grammar1.NumberLiteralContext); ok {
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

func convertColumnExprList(pr *nanopass.ParseResult, ctx *grammar1.ColumnExprListContext) (exprs []Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		switch c := child.(type) {
		case *grammar1.ColumnsExprColumnContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if ce, ok := c.GetChild(j).(grammar1.IColumnExprContext); ok {
					expr, exprErr := convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
					if exprErr != nil {
						err = exprErr
						return
					}
					exprs = append(exprs, expr)
				}
			}
		case *grammar1.ColumnsExprAsteriskContext:
			exprs = append(exprs, Expr{
				Kind:     KindAsterisk,
				Asterisk: &AsteriskData{Table: extractAsteriskTable(c)},
			})
		case *grammar1.ColumnsExprSubqueryContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if sus, ok := c.GetChild(j).(*grammar1.SelectUnionStmtContext); ok {
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

func identText(ctx *grammar1.IdentifierContext) string {
	return normalizeIdentifier(ctx.GetText())
}

func aliasText(ctx *grammar1.AliasContext) string {
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

func extractTableIdentifier(ctx *grammar1.TableIdentifierContext) (database, table string) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if db, ok := child.(*grammar1.DatabaseIdentifierContext); ok {
			database = identTextFromNode(db)
		}
		if ident, ok := child.(*grammar1.IdentifierContext); ok {
			table = identText(ident)
		}
	}
	return
}

func extractAsteriskTable(ctx *grammar1.ColumnsExprAsteriskContext) string {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ti, ok := ctx.GetChild(i).(*grammar1.TableIdentifierContext); ok {
			_, table := extractTableIdentifier(ti)
			return table
		}
	}
	return ""
}

func extractDynamicPattern(ctx *grammar1.DynamicColumnSelectionContext) string {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerSTRING_LITERAL {
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
