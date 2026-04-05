//go:build llm_generated_opus46

package ast

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func convertColumnExpr(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (expr Expr, err error) {
	switch c := ctx.(type) {
	case *grammar2.ColumnExprLiteralContext:
		expr = convertLiteral(pr, c)
	case *grammar2.ColumnExprParamSlotContext:
		expr = convertParamSlot(pr, c)
	case *grammar2.ColumnExprIdentifierContext:
		expr = convertIdentifier(pr, c)
	case *grammar2.ColumnExprFunctionContext:
		expr, err = convertFunction(pr, c)
	case *grammar2.ColumnExprWinFunctionContext:
		expr, err = convertWinFunction(pr, c)
	case *grammar2.ColumnExprWinFunctionTargetContext:
		expr, err = convertWinFunctionTarget(pr, c)
	case *grammar2.ColumnExprIntervalContext:
		expr, err = convertInterval(pr, c)
	case *grammar2.ColumnExprNegateContext:
		expr, err = convertNegate(pr, c)
	case *grammar2.ColumnExprNotContext:
		expr, err = convertNot(pr, c)
	case *grammar2.ColumnExprAndContext:
		expr, err = convertBinaryExpr(pr, c, BinOpAnd)
	case *grammar2.ColumnExprOrContext:
		expr, err = convertBinaryExpr(pr, c, BinOpOr)
	case *grammar2.ColumnExprPrecedence1Context:
		expr, err = convertPrecedence1(pr, c)
	case *grammar2.ColumnExprPrecedence2Context:
		expr, err = convertPrecedence2(pr, c)
	case *grammar2.ColumnExprPrecedence3Context:
		expr, err = convertPrecedence3(pr, c)
	case *grammar2.ColumnExprIsNullContext:
		expr, err = convertIsNull(pr, c)
	case *grammar2.ColumnExprBetweenContext:
		expr, err = convertBetween(pr, c)
	case *grammar2.ColumnExprAliasContext:
		expr, err = convertAlias(pr, c)
	case *grammar2.ColumnExprAsteriskContext:
		expr = convertAsterisk(pr, c)
	case *grammar2.ColumnExprSubqueryContext:
		expr, err = convertSubquery(pr, c)
	case *grammar2.ColumnExprParensContext:
		expr, err = convertParens(pr, c)
	case *grammar2.ColumnExprDynamicContext:
		expr = convertDynamic(pr, c)
	default:
		err = eb.Build().Type("ctxType", ctx).Errorf("unsupported column expression type")
	}
	return
}

func convertLiteral(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprLiteralContext) (expr Expr) {
	expr.Kind = KindLiteral
	expr.Literal = &LiteralData{SQL: nanopass.NodeText(pr, ctx)}
	return
}

func convertParamSlot(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprParamSlotContext) (expr Expr) {
	expr.Kind = KindParamSlot
	ps := &ParamSlotData{}
	for i := 0; i < ctx.GetChildCount(); i++ {
		if psCtx, ok := ctx.GetChild(i).(*grammar2.ParamSlotContext); ok {
			for j := 0; j < psCtx.GetChildCount(); j++ {
				child := psCtx.GetChild(j)
				if term, ok := child.(*antlr.TerminalNodeImpl); ok {
					if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
						ps.Name = stripQuotes(term.GetText())
					}
				}
				if cte, ok := child.(grammar2.IColumnTypeExprContext); ok {
					ps.Type = nanopass.NodeText(pr, cte.(antlr.ParserRuleContext))
				}
			}
		}
	}
	expr.Param = ps
	return
}

func convertIdentifier(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprIdentifierContext) (expr Expr) {
	expr.Kind = KindColumnRef
	ref := &ColumnRefData{}
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ci, ok := ctx.GetChild(i).(*grammar2.ColumnIdentifierContext); ok {
			extractColumnIdentifier(ci, ref)
		}
	}
	expr.ColRef = ref
	return
}

func extractColumnIdentifier(ctx *grammar2.ColumnIdentifierContext, ref *ColumnRefData) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ti, ok := child.(*grammar2.TableIdentifierContext); ok {
			ref.Database, ref.Table = extractTableIdentifier(ti)
		}
		if ni, ok := child.(*grammar2.NestedIdentifierContext); ok {
			extractNestedIdentifier(ni, ref)
		}
	}
}

func extractNestedIdentifier(ctx *grammar2.NestedIdentifierContext, ref *ColumnRefData) {
	idents := make([]string, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
				idents = append(idents, stripQuotes(term.GetText()))
			}
		}
	}
	if len(idents) >= 1 {
		ref.Column = idents[0]
	}
	if len(idents) >= 2 {
		ref.Nested = idents[1]
	}
}

func convertFunction(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprFunctionContext) (expr Expr, err error) {
	expr.Kind = KindFunctionCall
	fn := &FuncCallData{}
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerIDENTIFIER:
				if fn.Name == "" {
					fn.Name = stripQuotes(term.GetText())
				}
			case grammar2.ClickHouseLexerDISTINCT:
				fn.Distinct = true
			}
		}
		if cel, ok := child.(*grammar2.ColumnExprListContext); ok {
			fn.Params, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
		if cal, ok := child.(*grammar2.ColumnArgListContext); ok {
			fn.Args, err = convertColumnArgList(pr, cal)
			if err != nil {
				return
			}
		}
	}
	expr.Func = fn
	return
}

func convertColumnArgList(pr *nanopass.ParseResult, ctx *grammar2.ColumnArgListContext) (args []Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if cae, ok := ctx.GetChild(i).(*grammar2.ColumnArgExprContext); ok {
			var arg Expr
			arg, err = convertColumnArgExpr(pr, cae)
			if err != nil {
				return
			}
			args = append(args, arg)
		}
	}
	return
}

func convertColumnArgExpr(pr *nanopass.ParseResult, ctx *grammar2.ColumnArgExprContext) (expr Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if cle, ok := child.(*grammar2.ColumnLambdaExprContext); ok {
			return convertLambda(pr, cle)
		}
		if ce, ok := child.(grammar2.IColumnExprContext); ok {
			return convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
		}
	}
	err = eh.Errorf("empty ColumnArgExpr")
	return
}

func convertLambda(pr *nanopass.ParseResult, ctx *grammar2.ColumnLambdaExprContext) (expr Expr, err error) {
	expr.Kind = KindLambda
	lam := &LambdaData{}
	seenArrow := false
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			tt := term.GetSymbol().GetTokenType()
			if tt == grammar2.ClickHouseLexerARROW {
				seenArrow = true
			}
			if !seenArrow && tt == grammar2.ClickHouseLexerIDENTIFIER {
				lam.Params = append(lam.Params, stripQuotes(term.GetText()))
			}
		}
		if seenArrow {
			if ce, ok := child.(grammar2.IColumnExprContext); ok {
				lam.Body, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
				if err != nil {
					return
				}
			}
		}
	}
	expr.Lambda = lam
	return
}

func convertWinFunction(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprWinFunctionContext) (expr Expr, err error) {
	expr.Kind = KindWindowFunc
	wfn := &WindowFuncData{}
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER && wfn.Name == "" {
				wfn.Name = stripQuotes(term.GetText())
			}
		}
		if cel, ok := child.(*grammar2.ColumnExprListContext); ok {
			wfn.Args, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
		if we, ok := child.(*grammar2.WindowExprContext); ok {
			var ws WindowSpec
			ws, err = convertWindowExpr(pr, we)
			if err != nil {
				return
			}
			wfn.Window = &ws
		}
	}
	expr.WinFunc = wfn
	return
}

func convertWinFunctionTarget(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprWinFunctionTargetContext) (expr Expr, err error) {
	expr.Kind = KindWindowFunc
	wfn := &WindowFuncData{}
	idents := make([]string, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
				idents = append(idents, stripQuotes(term.GetText()))
			}
		}
		if cel, ok := child.(*grammar2.ColumnExprListContext); ok {
			wfn.Args, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	if len(idents) >= 1 {
		wfn.Name = idents[0]
	}
	if len(idents) >= 2 {
		wfn.WindowRef = idents[1]
	}
	expr.WinFunc = wfn
	return
}

func convertInterval(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprIntervalContext) (expr Expr, err error) {
	expr.Kind = KindInterval
	iv := &IntervalData{}
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar2.IColumnExprContext); ok {
			iv.Value, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
		}
		if intv, ok := child.(*grammar2.IntervalContext); ok {
			iv.Unit, err = ParseIntervalUnit(strings.ToUpper(intv.GetText()))
			if err != nil {
				return
			}
		}
	}
	expr.Interval = iv
	return
}

func convertNegate(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprNegateContext) (expr Expr, err error) {
	expr.Kind = KindUnary
	un := &UnaryData{Op: UnaryOpNegate}
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar2.IColumnExprContext); ok {
			un.Expr, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
		}
	}
	expr.Unary = un
	return
}

func convertNot(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprNotContext) (expr Expr, err error) {
	expr.Kind = KindUnary
	un := &UnaryData{Op: UnaryOpNot}
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar2.IColumnExprContext); ok {
			un.Expr, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
		}
	}
	expr.Unary = un
	return
}

func convertBinaryExpr(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext, op BinaryOpE) (expr Expr, err error) {
	expr.Kind = KindBinary
	bin := &BinaryData{Op: op}
	exprs := make([]Expr, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar2.IColumnExprContext); ok {
			var e Expr
			e, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
			exprs = append(exprs, e)
		}
	}
	if len(exprs) >= 1 {
		bin.Left = exprs[0]
	}
	if len(exprs) >= 2 {
		bin.Right = exprs[1]
	}
	expr.Binary = bin
	return
}

func convertPrecedence1(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprPrecedence1Context) (expr Expr, err error) {
	var op BinaryOpE
	op, err = extractPrecedence1Op(ctx)
	if err != nil {
		return
	}
	return convertBinaryExpr(pr, ctx, op)
}

func extractPrecedence1Op(ctx *grammar2.ColumnExprPrecedence1Context) (op BinaryOpE, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerASTERISK:
				return BinOpMultiply, nil
			case grammar2.ClickHouseLexerSLASH:
				return BinOpDivide, nil
			case grammar2.ClickHouseLexerPERCENT:
				return BinOpModulo, nil
			}
		}
	}
	err = eh.Errorf("no operator in Precedence1")
	return
}

func convertPrecedence2(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprPrecedence2Context) (expr Expr, err error) {
	var op BinaryOpE
	op, err = extractPrecedence2Op(ctx)
	if err != nil {
		return
	}
	return convertBinaryExpr(pr, ctx, op)
}

func extractPrecedence2Op(ctx *grammar2.ColumnExprPrecedence2Context) (op BinaryOpE, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerPLUS:
				return BinOpPlus, nil
			case grammar2.ClickHouseLexerDASH:
				return BinOpMinus, nil
			case grammar2.ClickHouseLexerCONCAT:
				return BinOpConcat, nil
			}
		}
	}
	err = eh.Errorf("no operator in Precedence2")
	return
}

func convertPrecedence3(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprPrecedence3Context) (expr Expr, err error) {
	var op BinaryOpE
	op, err = extractPrecedence3Op(ctx)
	if err != nil {
		return
	}
	return convertBinaryExpr(pr, ctx, op)
}

func extractPrecedence3Op(ctx *grammar2.ColumnExprPrecedence3Context) (op BinaryOpE, err error) {
	hasGlobal := false
	hasNot := false
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar2.ClickHouseLexerEQ_SINGLE:
				return BinOpEq, nil
			case grammar2.ClickHouseLexerNOT_EQ:
				return BinOpNotEq, nil
			case grammar2.ClickHouseLexerLT:
				return BinOpLt, nil
			case grammar2.ClickHouseLexerGT:
				return BinOpGt, nil
			case grammar2.ClickHouseLexerLE:
				return BinOpLe, nil
			case grammar2.ClickHouseLexerGE:
				return BinOpGe, nil
			case grammar2.ClickHouseLexerGLOBAL:
				hasGlobal = true
			case grammar2.ClickHouseLexerNOT:
				hasNot = true
			case grammar2.ClickHouseLexerIN:
				switch {
				case hasGlobal && hasNot:
					return BinOpGlobalNotIn, nil
				case hasGlobal:
					return BinOpGlobalIn, nil
				case hasNot:
					return BinOpNotIn, nil
				default:
					return BinOpIn, nil
				}
			case grammar2.ClickHouseLexerLIKE:
				if hasNot {
					return BinOpNotLike, nil
				}
				return BinOpLike, nil
			case grammar2.ClickHouseLexerILIKE:
				if hasNot {
					return BinOpNotILike, nil
				}
				return BinOpILike, nil
			}
		}
	}
	err = eh.Errorf("no operator in Precedence3")
	return
}

func convertIsNull(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprIsNullContext) (expr Expr, err error) {
	expr.Kind = KindIsNull
	isn := &IsNullData{}
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar2.IColumnExprContext); ok {
			isn.Expr, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerNOT {
				isn.Negate = true
			}
		}
	}
	expr.IsNull = isn
	return
}

func convertBetween(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprBetweenContext) (expr Expr, err error) {
	expr.Kind = KindBetween
	btw := &BetweenData{}
	exprs := make([]Expr, 0, 3)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar2.IColumnExprContext); ok {
			var e Expr
			e, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
			exprs = append(exprs, e)
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerNOT {
				btw.Negate = true
			}
		}
	}
	if len(exprs) >= 1 {
		btw.Expr = exprs[0]
	}
	if len(exprs) >= 2 {
		btw.Low = exprs[1]
	}
	if len(exprs) >= 3 {
		btw.High = exprs[2]
	}
	expr.Between = btw
	return
}

func convertAlias(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprAliasContext) (expr Expr, err error) {
	expr.Kind = KindAlias
	als := &AliasData{}
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar2.IColumnExprContext); ok {
			als.Expr, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
				als.Name = stripQuotes(term.GetText())
			}
		}
		if a, ok := child.(*grammar2.AliasContext); ok {
			for j := 0; j < a.GetChildCount(); j++ {
				if term, ok := a.GetChild(j).(*antlr.TerminalNodeImpl); ok {
					if term.GetSymbol().GetTokenType() == grammar2.ClickHouseLexerIDENTIFIER {
						als.Name = stripQuotes(term.GetText())
					}
				}
			}
		}
	}
	expr.Alias = als
	return
}

func convertAsterisk(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprAsteriskContext) (expr Expr) {
	expr.Kind = KindAsterisk
	star := &AsteriskData{}
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ti, ok := ctx.GetChild(i).(*grammar2.TableIdentifierContext); ok {
			_, star.Table = extractTableIdentifier(ti)
		}
	}
	expr.Asterisk = star
	return
}

func convertSubquery(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprSubqueryContext) (expr Expr, err error) {
	expr.Kind = KindSubquery
	for i := 0; i < ctx.GetChildCount(); i++ {
		if sus, ok := ctx.GetChild(i).(*grammar2.SelectUnionStmtContext); ok {
			var su SelectUnion
			su, err = convertSelectUnion(pr, sus)
			if err != nil {
				return
			}
			expr.Subquery = &SubqueryData{Query: su}
		}
	}
	return
}

func convertParens(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprParensContext) (expr Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar2.IColumnExprContext); ok {
			return convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
		}
	}
	err = eh.Errorf("empty ColumnExprParens")
	return
}

func convertDynamic(pr *nanopass.ParseResult, ctx *grammar2.ColumnExprDynamicContext) (expr Expr) {
	expr.Kind = KindDynColumn
	for i := 0; i < ctx.GetChildCount(); i++ {
		if dcs, ok := ctx.GetChild(i).(*grammar2.DynamicColumnSelectionContext); ok {
			expr.DynCol = &DynColumnData{Pattern: extractDynamicPattern(dcs)}
		}
	}
	return
}
