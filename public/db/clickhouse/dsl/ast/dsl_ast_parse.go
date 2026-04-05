//go:build llm_generated_opus46

package ast

import (
	"strconv"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ConvertCSTToAST converts a Grammar2 CST (from nanopass.ParseCanonical) to an AST.
func ConvertCSTToAST(pr *nanopass.ParseResult) (query Query, err error) {
	root, ok := pr.Tree.(*grammar2.QueryStmtContext)
	if !ok {
		err = eb.Build().Type("ctxType", pr.Tree).Errorf("expected *grammar2.QueryStmtContext")
		return
	}
	return convertQueryStmt(pr, root)
}

func convertQueryStmt(pr *nanopass.ParseResult, ctx *grammar2.QueryStmtContext) (query Query, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if c, ok := ctx.GetChild(i).(*grammar2.QueryContext); ok {
			query, err = convertQuery(pr, c)
			if err != nil {
				return
			}
		}
	}
	// FORMAT
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerFORMAT {
				if i+1 < ctx.GetChildCount() {
					if ident, ok := ctx.GetChild(i + 1).(*antlr.TerminalNodeImpl); ok {
						if ident.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
							query.Format = stripQuotes(ident.GetText())
						}
					}
				}
			}
		}
	}
	return
}

func convertQuery(pr *nanopass.ParseResult, ctx *grammar2.QueryContext) (query Query, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar2.SetStmtContext:
			var settings []SettingPair
			settings, err = convertSetStmt(pr, c)
			if err != nil {
				return
			}
			query.Settings = append(query.Settings, settings...)
		case *grammar2.CtesContext:
			query.CTEs, err = convertCTEs(pr, c)
			if err != nil {
				return
			}
		case *grammar2.SelectUnionStmtContext:
			query.Body, err = convertSelectUnion(pr, c)
			if err != nil {
				return
			}
		}
	}
	return
}

// --- SET ---

func convertSetStmt(pr *nanopass.ParseResult, ctx *grammar2.SetStmtContext) (settings []SettingPair, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if sel, ok := ctx.GetChild(i).(*grammar2.SettingExprListContext); ok {
			return convertSettingExprList(pr, sel)
		}
	}
	return
}

func convertSettingExprList(pr *nanopass.ParseResult, ctx *grammar2.SettingExprListContext) (settings []SettingPair, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if se, ok := ctx.GetChild(i).(*grammar2.SettingExprContext); ok {
			pair := convertSettingExpr(pr, se)
			settings = append(settings, pair)
		}
	}
	return
}

func convertSettingExpr(pr *nanopass.ParseResult, ctx *grammar2.SettingExprContext) (pair SettingPair) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
				pair.Key = stripQuotes(term.GetText())
			}
		}
		if sv, ok := ctx.GetChild(i).(grammar2.ISettingValueContext); ok {
			pair.ValueSQL = nanopass.NodeText(pr, sv.(antlr.ParserRuleContext))
		}
	}
	return
}

// --- CTEs ---

func convertCTEs(pr *nanopass.ParseResult, ctx *grammar2.CtesContext) (ctes []CTE, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if nq, ok := ctx.GetChild(i).(*grammar2.NamedQueryContext); ok {
			var cte CTE
			cte, err = convertNamedQuery(pr, nq)
			if err != nil {
				return
			}
			ctes = append(ctes, cte)
		}
	}
	return
}

func convertNamedQuery(pr *nanopass.ParseResult, ctx *grammar2.NamedQueryContext) (cte CTE, err error) {
	nameFound := false
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER && !nameFound {
				cte.Name = stripQuotes(term.GetText())
				nameFound = true
			}
		}
		if ca, ok := child.(*grammar2.ColumnAliasesContext); ok {
			cte.ColumnAliases = convertColumnAliases(ca)
		}
		if q, ok := child.(*grammar2.QueryContext); ok {
			cte.Body, err = convertQuery(pr, q)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertColumnAliases(ctx *grammar2.ColumnAliasesContext) (aliases []string) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
				aliases = append(aliases, stripQuotes(term.GetText()))
			}
		}
	}
	return
}

// --- SELECT UNION ---

func convertSelectUnion(pr *nanopass.ParseResult, ctx *grammar2.SelectUnionStmtContext) (su SelectUnion, err error) {
	first := true
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar2.SelectStmtWithParensContext:
			if first {
				var headUnion SelectUnion
				headUnion, err = convertSelectStmtWithParens(pr, c)
				if err != nil {
					return
				}
				su.Head = headUnion.Head
				// If the head itself is a compound union, flatten its items
				su.Items = append(su.Items, headUnion.Items...)
				first = false
			}
		case *grammar2.SelectUnionStmtItemContext:
			var item SelectUnionItem
			item, err = convertSelectUnionItem(pr, c)
			if err != nil {
				return
			}
			su.Items = append(su.Items, item)
		}
	}
	return
}

func convertSelectUnionItem(pr *nanopass.ParseResult, ctx *grammar2.SelectUnionStmtItemContext) (item SelectUnionItem, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerUNION:
				item.Op = UnionOpUnion
			case grammar2.ClickHouseLexerEXCEPT:
				item.Op = UnionOpExcept
			case grammar2.ClickHouseLexerINTERSECT:
				item.Op = UnionOpIntersect
			case grammar2.ClickHouseLexerALL:
				item.Modifier = UnionModAll
			case grammar2.ClickHouseLexerDISTINCT:
				item.Modifier = UnionModDistinct
			}
		}
		if swp, ok := child.(*grammar2.SelectStmtWithParensContext); ok {
			item.Body, err = convertSelectStmtWithParens(pr, swp)
			if err != nil {
				return
			}
		}
	}
	return
}

// convertSelectStmtWithParens returns a SelectUnion. For a bare selectStmt,
// the result has a single Head and no Items. For a parenthesized selectUnionStmt,
// the result is the full union chain.
func convertSelectStmtWithParens(pr *nanopass.ParseResult, ctx *grammar2.SelectStmtWithParensContext) (su SelectUnion, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar2.SelectStmtContext:
			su.Head, err = convertSelectStmt(pr, c)
			return
		case *grammar2.SelectUnionStmtContext:
			return convertSelectUnion(pr, c)
		}
	}
	err = eh.Errorf("convertSelectStmtWithParens: empty")
	return
}

// --- SELECT ---

func convertSelectStmt(pr *nanopass.ParseResult, ctx *grammar2.SelectStmtContext) (sel Select, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		switch c := child.(type) {
		case *grammar2.ProjectionClauseContext:
			err = convertProjectionClause(pr, c, &sel)
		case *grammar2.WithClauseContext:
			sel.With, err = convertWithClause(pr, c)
		case *grammar2.FromClauseContext:
			var je JoinExpr
			je, err = convertFromClause(pr, c)
			sel.From = &je
		case *grammar2.ArrayJoinClauseContext:
			var aj ArrayJoinClause
			aj, err = convertArrayJoinClause(pr, c)
			sel.ArrayJoin = &aj
		case *grammar2.WindowClauseContext:
			var wd WindowDefClause
			wd, err = convertWindowClause(pr, c)
			sel.WindowDef = &wd
		case *grammar2.QualifyClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Qualify = &expr
		case *grammar2.PrewhereClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Prewhere = &expr
		case *grammar2.WhereClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Where = &expr
		case *grammar2.GroupByClauseContext:
			var gb GroupByClause
			gb, err = convertGroupByClause(pr, c)
			sel.GroupBy = &gb
		case *grammar2.HavingClauseContext:
			var expr Expr
			expr, err = convertSingleExprClause(pr, c)
			sel.Having = &expr
		case *grammar2.OrderByClauseContext:
			var ob OrderByClause
			ob, err = convertOrderByClause(pr, c)
			sel.OrderBy = &ob
		case *grammar2.LimitByClauseContext:
			var lb LimitByClause
			lb, err = convertLimitByClause(pr, c)
			sel.LimitBy = &lb
		case *grammar2.LimitClauseContext:
			var lc LimitClause
			lc, err = convertLimitClause(pr, c)
			sel.Limit = &lc
		case *grammar2.SettingsClauseContext:
			sel.Settings, err = convertSettingsClause(pr, c)
		}
		if err != nil {
			return
		}
	}
	return
}

func convertProjectionClause(pr *nanopass.ParseResult, ctx *grammar2.ProjectionClauseContext, sel *Select) (err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerDISTINCT {
				sel.Distinct = true
			}
		}
		if tc, ok := child.(*grammar2.TopClauseContext); ok {
			var top TopClause
			top, err = convertTopClause(tc)
			if err != nil {
				return
			}
			sel.Top = &top
		}
		if cel, ok := child.(*grammar2.ColumnExprListContext); ok {
			sel.Projection, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
		if pec, ok := child.(*grammar2.ProjectionExceptClauseContext); ok {
			exc := convertProjectionExceptClause(pec)
			sel.ExceptColumns = &exc
		}
	}
	return
}

func convertTopClause(ctx *grammar2.TopClauseContext) (top TopClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerDECIMAL_LITERAL:
				top.N, err = strconv.ParseUint(term.GetText(), 10, 64)
				if err != nil {
					return
				}
			case grammar2.ClickHouseLexerTIES:
				top.WithTies = true
			}
		}
	}
	return
}

func convertProjectionExceptClause(ctx *grammar2.ProjectionExceptClauseContext) (exc ExceptClause) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar2.StaticColumnListContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if term, ok := c.GetChild(j).(*antlr.TerminalNodeImpl); ok {
					if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
						exc.Static = append(exc.Static, stripQuotes(term.GetText()))
					}
				}
			}
		case *grammar2.DynamicColumnListContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if dcs, ok := c.GetChild(j).(*grammar2.DynamicColumnSelectionContext); ok {
					exc.Dynamic = extractDynamicPattern(dcs)
				}
			}
		}
	}
	return
}

func convertWithClause(pr *nanopass.ParseResult, ctx *grammar2.WithClauseContext) (exprs []Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if cel, ok := ctx.GetChild(i).(*grammar2.ColumnExprListContext); ok {
			return convertColumnExprList(pr, cel)
		}
	}
	return
}

func convertSingleExprClause(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (expr Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar2.IColumnExprContext); ok {
			return convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
		}
	}
	err = eb.Build().Type("ctxType", ctx).Errorf("no column expression found")
	return
}

// --- GROUP BY ---

func convertGroupByClause(pr *nanopass.ParseResult, ctx *grammar2.GroupByClauseContext) (gb GroupByClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerCUBE:
				gb.Modifier = GroupByModCube
			case grammar2.ClickHouseLexerROLLUP:
				gb.Modifier = GroupByModRollup
			case grammar2.ClickHouseLexerTOTALS:
				gb.WithTotals = true
			}
		}
		if cel, ok := child.(*grammar2.ColumnExprListContext); ok {
			gb.Exprs, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

// --- ORDER BY ---

func convertOrderByClause(pr *nanopass.ParseResult, ctx *grammar2.OrderByClauseContext) (ob OrderByClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if oel, ok := ctx.GetChild(i).(*grammar2.OrderExprListContext); ok {
			ob.Items, err = convertOrderExprList(pr, oel)
			return
		}
	}
	return
}

func convertOrderExprList(pr *nanopass.ParseResult, ctx *grammar2.OrderExprListContext) (items []OrderItem, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if oe, ok := ctx.GetChild(i).(*grammar2.OrderExprContext); ok {
			var item OrderItem
			item, err = convertOrderExpr(pr, oe)
			if err != nil {
				return
			}
			items = append(items, item)
		}
	}
	return
}

func convertOrderExpr(pr *nanopass.ParseResult, ctx *grammar2.OrderExprContext) (item OrderItem, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar2.IColumnExprContext); ok {
			item.Expr, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerASCENDING:
				item.Dir = OrderDirAsc
			case grammar2.ClickHouseLexerDESCENDING, grammar2.ClickHouseLexerDESC:
				item.Dir = OrderDirDesc
			case grammar2.ClickHouseLexerFIRST:
				item.Nulls = OrderNullsFirst
			case grammar2.ClickHouseLexerLAST:
				item.Nulls = OrderNullsLast
			case grammar2.ClickHouseLexerSTRING_LITERAL:
				item.Collate = stripStringQuotes(term.GetText())
			}
		}
	}
	return
}

// --- LIMIT ---

func convertLimitClause(pr *nanopass.ParseResult, ctx *grammar2.LimitClauseContext) (lc LimitClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if le, ok := child.(*grammar2.LimitExprContext); ok {
			lc.Limit, err = convertLimitExpr(pr, le)
			if err != nil {
				return
			}
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerTIES {
				lc.WithTies = true
			}
		}
	}
	return
}

func convertLimitByClause(pr *nanopass.ParseResult, ctx *grammar2.LimitByClauseContext) (lb LimitByClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if le, ok := child.(*grammar2.LimitExprContext); ok {
			lb.Limit, err = convertLimitExpr(pr, le)
			if err != nil {
				return
			}
		}
		if cel, ok := child.(*grammar2.ColumnExprListContext); ok {
			lb.Columns, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertLimitExpr(pr *nanopass.ParseResult, ctx *grammar2.LimitExprContext) (ls LimitSpec, err error) {
	exprs := make([]Expr, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar2.IColumnExprContext); ok {
			var expr Expr
			expr, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
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

func convertSettingsClause(pr *nanopass.ParseResult, ctx *grammar2.SettingsClauseContext) (settings []SettingPair, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if sel, ok := ctx.GetChild(i).(*grammar2.SettingExprListContext); ok {
			return convertSettingExprList(pr, sel)
		}
	}
	return
}

// --- ARRAY JOIN ---

func convertArrayJoinClause(pr *nanopass.ParseResult, ctx *grammar2.ArrayJoinClauseContext) (aj ArrayJoinClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerLEFT:
				aj.Kind = ArrayJoinLeft
			case grammar2.ClickHouseLexerINNER:
				aj.Kind = ArrayJoinInner
			}
		}
		if cel, ok := child.(*grammar2.ColumnExprListContext); ok {
			aj.Exprs, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

// --- WINDOW ---

func convertWindowClause(pr *nanopass.ParseResult, ctx *grammar2.WindowClauseContext) (wd WindowDefClause, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
				wd.Name = stripQuotes(term.GetText())
			}
		}
		if we, ok := child.(*grammar2.WindowExprContext); ok {
			wd.Window, err = convertWindowExpr(pr, we)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertWindowExpr(pr *nanopass.ParseResult, ctx *grammar2.WindowExprContext) (ws WindowSpec, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar2.WinPartitionByClauseContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if cel, ok := c.GetChild(j).(*grammar2.ColumnExprListContext); ok {
					ws.PartitionBy, err = convertColumnExprList(pr, cel)
					if err != nil {
						return
					}
				}
			}
		case *grammar2.WinOrderByClauseContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if oel, ok := c.GetChild(j).(*grammar2.OrderExprListContext); ok {
					ws.OrderBy, err = convertOrderExprList(pr, oel)
					if err != nil {
						return
					}
				}
			}
		case *grammar2.WinFrameClauseContext:
			frame := convertWinFrameClause(pr, c)
			ws.Frame = &frame
		}
	}
	return
}

func convertWinFrameClause(pr *nanopass.ParseResult, ctx *grammar2.WinFrameClauseContext) (wf WindowFrame) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerROWS:
				wf.Unit = FrameUnitRows
			case grammar2.ClickHouseLexerRANGE:
				wf.Unit = FrameUnitRange
			}
		}
		switch c := child.(type) {
		case *grammar2.FrameStartContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if wfb, ok := c.GetChild(j).(*grammar2.WinFrameBoundContext); ok {
					wf.Start = convertFrameBound(pr, wfb)
				}
			}
		case *grammar2.FrameBetweenContext:
			bounds := make([]FrameBound, 0, 2)
			for j := 0; j < c.GetChildCount(); j++ {
				if wfb, ok := c.GetChild(j).(*grammar2.WinFrameBoundContext); ok {
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

func convertFrameBound(pr *nanopass.ParseResult, ctx *grammar2.WinFrameBoundContext) (fb FrameBound) {
	hasCurrent, hasUnbounded, hasPreceding, hasFollowing := false, false, false, false
	numText := ""
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerCURRENT:
				hasCurrent = true
			case grammar2.ClickHouseLexerUNBOUNDED:
				hasUnbounded = true
			case grammar2.ClickHouseLexerPRECEDING:
				hasPreceding = true
			case grammar2.ClickHouseLexerFOLLOWING:
				hasFollowing = true
			}
		}
		if nl, ok := child.(*grammar2.NumberLiteralContext); ok {
			numText = nanopass.NodeText(pr, nl)
		}
	}
	switch {
	case hasCurrent:
		fb.Kind = FrameBoundCurrentRow
	case hasUnbounded && hasPreceding:
		fb.Kind = FrameBoundUnboundedPreceding
	case hasUnbounded && hasFollowing:
		fb.Kind = FrameBoundUnboundedFollowing
	case hasPreceding:
		fb.Kind = FrameBoundNPreceding
		fb.N = numText
	case hasFollowing:
		fb.Kind = FrameBoundNFollowing
		fb.N = numText
	}
	return
}

// --- FROM / JOIN ---

func convertFromClause(pr *nanopass.ParseResult, ctx *grammar2.FromClauseContext) (je JoinExpr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if jec, ok := ctx.GetChild(i).(grammar2.IJoinExprContext); ok {
			return convertJoinExpr(pr, jec.(antlr.ParserRuleContext))
		}
	}
	err = eh.Errorf("no joinExpr in FROM")
	return
}

func convertJoinExpr(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (je JoinExpr, err error) {
	switch c := ctx.(type) {
	case *grammar2.JoinExprTableContext:
		return convertJoinExprTable(pr, c)
	case *grammar2.JoinExprOpContext:
		return convertJoinExprOp(pr, c)
	case *grammar2.JoinExprCrossOpContext:
		return convertJoinExprCrossOp(pr, c)
	case *grammar2.JoinExprParensContext:
		for i := 0; i < c.GetChildCount(); i++ {
			if inner, ok := c.GetChild(i).(grammar2.IJoinExprContext); ok {
				return convertJoinExpr(pr, inner.(antlr.ParserRuleContext))
			}
		}
		err = eh.Errorf("empty JoinExprParens")
	default:
		err = eb.Build().Type("ctxType", ctx).Errorf("unsupported join expression type")
	}
	return
}

func convertJoinExprTable(pr *nanopass.ParseResult, ctx *grammar2.JoinExprTableContext) (je JoinExpr, err error) {
	je.Kind = JoinExprTable
	td := &JoinTableData{}
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if te, ok := child.(grammar2.ITableExprContext); ok {
			err = convertTableExpr(pr, te.(antlr.ParserRuleContext), td)
			if err != nil {
				return
			}
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerFINAL {
				td.Final = true
			}
		}
		if sc, ok := child.(*grammar2.SampleClauseContext); ok {
			sample := convertSampleClause(pr, sc)
			td.Sample = &sample
		}
	}
	je.Table = td
	return
}

func convertTableExpr(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext, td *JoinTableData) (err error) {
	switch c := ctx.(type) {
	case *grammar2.TableExprIdentifierContext:
		td.TableKind = TableKindRef
		for i := 0; i < c.GetChildCount(); i++ {
			if ti, ok := c.GetChild(i).(*grammar2.TableIdentifierContext); ok {
				td.Database, td.Table = extractTableIdentifier(ti)
			}
		}
	case *grammar2.TableExprFunctionContext:
		td.TableKind = TableKindFunc
		for i := 0; i < c.GetChildCount(); i++ {
			if tfe, ok := c.GetChild(i).(*grammar2.TableFunctionExprContext); ok {
				td.FuncName, td.FuncArgs = convertTableFunctionExpr(pr, tfe)
			}
		}
	case *grammar2.TableExprSubqueryContext:
		td.TableKind = TableKindSubquery
		for i := 0; i < c.GetChildCount(); i++ {
			if sus, ok := c.GetChild(i).(*grammar2.SelectUnionStmtContext); ok {
				var su SelectUnion
				su, err = convertSelectUnion(pr, sus)
				if err != nil {
					return
				}
				td.Subquery = &su
			}
		}
	case *grammar2.TableExprAliasContext:
		for i := 0; i < c.GetChildCount(); i++ {
			child := c.GetChild(i)
			if innerTE, ok := child.(grammar2.ITableExprContext); ok {
				err = convertTableExpr(pr, innerTE.(antlr.ParserRuleContext), td)
				if err != nil {
					return
				}
			}
			if term, ok := child.(*antlr.TerminalNodeImpl); ok {
				if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
					td.Alias = stripQuotes(term.GetText())
				}
			}
			if a, ok := child.(*grammar2.AliasContext); ok {
				for j := 0; j < a.GetChildCount(); j++ {
					if term, ok := a.GetChild(j).(*antlr.TerminalNodeImpl); ok {
						if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
							td.Alias = stripQuotes(term.GetText())
						}
					}
				}
			}
		}
	default:
		err = eb.Build().Type("ctxType", ctx).Errorf("unsupported table expression type")
	}
	return
}

func convertTableFunctionExpr(pr *nanopass.ParseResult, ctx *grammar2.TableFunctionExprContext) (name string, args []Expr) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
				name = stripQuotes(term.GetText())
			}
		}
		if tal, ok := child.(*grammar2.TableArgListContext); ok {
			for j := 0; j < tal.GetChildCount(); j++ {
				if tae, ok := tal.GetChild(j).(*grammar2.TableArgExprContext); ok {
					args = append(args, Expr{Kind: KindLiteral, Literal: &LiteralData{SQL: nanopass.NodeText(pr, tae)}})
				}
			}
		}
	}
	return
}

func convertJoinExprOp(pr *nanopass.ParseResult, ctx *grammar2.JoinExprOpContext) (je JoinExpr, err error) {
	je.Kind = JoinExprOp
	op := &JoinOpData{}
	joinExprs := make([]antlr.ParserRuleContext, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if jec, ok := child.(grammar2.IJoinExprContext); ok {
			joinExprs = append(joinExprs, jec.(antlr.ParserRuleContext))
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerGLOBAL:
				op.Global = true
			case grammar2.ClickHouseLexerLOCAL:
				op.Local = true
			}
		}
		if jo, ok := child.(grammar2.IJoinOpContext); ok {
			op.Kind, op.Strictness = extractJoinOp(jo.(antlr.ParserRuleContext))
		}
		if jcc, ok := child.(*grammar2.JoinConstraintClauseContext); ok {
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
	je.Op = op
	return
}

func convertJoinExprCrossOp(pr *nanopass.ParseResult, ctx *grammar2.JoinExprCrossOpContext) (je JoinExpr, err error) {
	je.Kind = JoinExprCross
	cross := &JoinCrossData{}
	joinExprs := make([]antlr.ParserRuleContext, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if jec, ok := child.(grammar2.IJoinExprContext); ok {
			joinExprs = append(joinExprs, jec.(antlr.ParserRuleContext))
		}
		if joc, ok := child.(*grammar2.JoinOpCrossContext); ok {
			for j := 0; j < joc.GetChildCount(); j++ {
				if term, ok := joc.GetChild(j).(*antlr.TerminalNodeImpl); ok {
					switch term.GetSymbol().GetTokenType() {
					case grammar2.ClickHouseLexerGLOBAL:
						cross.Global = true
					case grammar2.ClickHouseLexerLOCAL:
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

func extractJoinOp(ctx antlr.ParserRuleContext) (kind JoinKindE, strictness JoinStrictnessE) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerINNER:
				kind = JoinKindInner
			case grammar2.ClickHouseLexerLEFT:
				kind = JoinKindLeft
			case grammar2.ClickHouseLexerRIGHT:
				kind = JoinKindRight
			case grammar2.ClickHouseLexerFULL:
				kind = JoinKindFull
			case grammar2.ClickHouseLexerALL:
				strictness = JoinStrictnessAll
			case grammar2.ClickHouseLexerANY:
				strictness = JoinStrictnessAny
			case grammar2.ClickHouseLexerSEMI:
				strictness = JoinStrictnessSemi
			case grammar2.ClickHouseLexerANTI:
				strictness = JoinStrictnessAnti
			case grammar2.ClickHouseLexerASOF:
				strictness = JoinStrictnessAsof
			}
		}
	}
	return
}

func convertJoinConstraint(pr *nanopass.ParseResult, ctx *grammar2.JoinConstraintClauseContext) (jc JoinConstraint, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerON:
				jc.Kind = JoinConstraintOn
			case grammar2.ClickHouseLexerUSING:
				jc.Kind = JoinConstraintUsing
			}
		}
		if cel, ok := child.(*grammar2.ColumnExprListContext); ok {
			jc.Exprs, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertSampleClause(pr *nanopass.ParseResult, ctx *grammar2.SampleClauseContext) (sc SampleClause) {
	ratios := make([]RatioExpr, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if re, ok := ctx.GetChild(i).(*grammar2.RatioExprContext); ok {
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

func convertRatioExpr(pr *nanopass.ParseResult, ctx *grammar2.RatioExprContext) (re RatioExpr) {
	nums := make([]string, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if nl, ok := ctx.GetChild(i).(*grammar2.NumberLiteralContext); ok {
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

func convertColumnExprList(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprListContext) (exprs []Expr, err error) {
	n := ctx.GetChildCount()
	exprs = make([]Expr, 0, (n+1)/2)
	for i := 0; i < n; i++ {
		switch c := ctx.GetChild(i).(type) {
		case *grammar2.ColumnsExprColumnContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if ce, ok := c.GetChild(j).(grammar2.IColumnExprContext); ok {
					var expr Expr
					expr, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
					if err != nil {
						return
					}
					exprs = append(exprs, expr)
				}
			}
		case *grammar2.ColumnsExprAsteriskContext:
			exprs = append(exprs, Expr{Kind: KindAsterisk, Asterisk: &AsteriskData{Table: extractAsteriskTable(c)}})
		case *grammar2.ColumnsExprSubqueryContext:
			for j := 0; j < c.GetChildCount(); j++ {
				if sus, ok := c.GetChild(j).(*grammar2.SelectUnionStmtContext); ok {
					var su SelectUnion
					su, err = convertSelectUnion(pr, sus)
					if err != nil {
						return
					}
					exprs = append(exprs, Expr{Kind: KindSubquery, Subquery: &SubqueryData{Query: su}})
				}
			}
		}
	}
	return
}

// --- Helpers ---

func stripQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func stripStringQuotes(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}

func extractTableIdentifier(ctx *grammar2.TableIdentifierContext) (database, table string) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if db, ok := child.(*grammar2.DatabaseIdentifierContext); ok {
			for j := 0; j < db.GetChildCount(); j++ {
				if term, ok := db.GetChild(j).(*antlr.TerminalNodeImpl); ok {
					if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
						database = stripQuotes(term.GetText())
					}
				}
			}
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
				table = stripQuotes(term.GetText())
			}
		}
	}
	return
}

func extractAsteriskTable(ctx *grammar2.ColumnsExprAsteriskContext) string {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ti, ok := ctx.GetChild(i).(*grammar2.TableIdentifierContext); ok {
			_, table := extractTableIdentifier(ti)
			return table
		}
	}
	return ""
}

func extractDynamicPattern(ctx *grammar2.DynamicColumnSelectionContext) string {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerSTRING_LITERAL {
				return stripStringQuotes(term.GetText())
			}
		}
	}
	return ""
}
