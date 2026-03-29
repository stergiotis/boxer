//go:build llm_generated_opus46

package marshalling

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// UnmarshalCompositeLiteral parses a SQL literal string into a TypedLiteral,
// preserving CAST information. Automatically detects homogeneous arrays and
// stores them in SoA layout.
//
// mapClickHouseTypeToCanonical maps ClickHouse type names (e.g. "UInt64") to
// canonical type strings (e.g. "u64"). If nil, cast types are not preserved.
func UnmarshalCompositeLiteral(sql string, mapClickHouseTypeToCanonical func(string) (string, error)) (result TypedLiteral, err error) {
	sql = strings.TrimSpace(sql)
	if len(sql) == 0 {
		err = eh.Errorf("UnmarshalCompositeLiteral: empty input")
		return
	}

	wrappedSQL := "SELECT " + sql
	pr, parseErr := nanopass.Parse(wrappedSQL)
	if parseErr != nil {
		err = eh.Errorf("UnmarshalCompositeLiteral: %w", parseErr)
		return
	}

	var exprNode antlr.ParserRuleContext
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if _, ok := ctx.(*grammar.ProjectionClauseContext); ok {
			return true
		}
		if _, ok := ctx.(*grammar.ColumnExprListContext); ok {
			return true
		}
		if _, ok := ctx.(*grammar.ColumnsExprColumnContext); ok {
			return true
		}
		switch ctx.(type) {
		case *grammar.ColumnExprLiteralContext,
			*grammar.ColumnExprFunctionContext,
			*grammar.ColumnExprArrayContext,
			*grammar.ColumnExprIdentifierContext,
			*grammar.ColumnExprCastContext,
			*grammar.ColumnExprTupleContext:
			if exprNode == nil {
				exprNode = ctx
			}
			return false
		}
		return true
	})

	if exprNode == nil {
		err = eh.Errorf("UnmarshalCompositeLiteral: no expression found in %q", sql)
		return
	}

	result, err = UnmarshalCSTToTypedLiteral(pr, exprNode, mapClickHouseTypeToCanonical)
	return
}

// UnmarshalCSTToTypedLiteral extracts a TypedLiteral from a CST node.
// Automatically detects homogeneous arrays and stores them in SoA layout.
//
// mapClickHouseTypeToCanonical maps ClickHouse type names to canonical type strings.
// If nil, cast types are not preserved.
func UnmarshalCSTToTypedLiteral(pr *nanopass.ParseResult, node antlr.ParserRuleContext, mapType func(string) (string, error)) (result TypedLiteral, err error) {
	switch ctx := node.(type) {
	case *grammar.ColumnExprLiteralContext:
		return unmarshalScalarCST(pr, ctx)
	case *grammar.ColumnExprIdentifierContext:
		return unmarshalIdentifierCST(pr, ctx)
	case *grammar.ColumnExprFunctionContext:
		return unmarshalFunctionCST(pr, ctx, mapType)
	case *grammar.ColumnExprArrayContext:
		return unmarshalArrayCST(pr, ctx, mapType)
	case *grammar.ColumnExprCastContext:
		return unmarshalCastExprCST(pr, ctx, mapType)
	case *grammar.ColumnExprTupleContext:
		return unmarshalTupleExprCST(pr, ctx, mapType)
	case *grammar.ColumnArgExprContext:
		if ctx.GetChildCount() > 0 {
			if innerCtx, ok := ctx.GetChild(0).(antlr.ParserRuleContext); ok {
				return UnmarshalCSTToTypedLiteral(pr, innerCtx, mapType)
			}
		}
		err = eh.Errorf("UnmarshalCSTToTypedLiteral: empty ColumnArgExprContext")
		return
	default:
		err = eh.Errorf("UnmarshalCSTToTypedLiteral: unsupported node type %T", node)
		return
	}
}

// --- Scalar ---

func unmarshalScalarCST(pr *nanopass.ParseResult, ctx *grammar.ColumnExprLiteralContext) (result TypedLiteral, err error) {
	text := nanopass.NodeText(pr, ctx)
	result, err = UnmarshalScalarLiteral(text)
	if err != nil {
		err = eh.Errorf("unmarshalScalarCST: %w", err)
	}
	return
}

// --- Identifier (true/false/null) ---

func unmarshalIdentifierCST(pr *nanopass.ParseResult, ctx *grammar.ColumnExprIdentifierContext) (result TypedLiteral, err error) {
	text := nanopass.NodeText(pr, ctx)
	text = strings.TrimSpace(text)
	switch strings.ToLower(text) {
	case "true", "false":
		result, err = UnmarshalScalarLiteral(text)
		if err != nil {
			err = eh.Errorf("unmarshalIdentifierCST: %w", err)
		}
	case "null":
		result = NewScalarNull()
	default:
		err = eh.Errorf("unmarshalIdentifierCST: identifier %q is not a literal value", text)
	}
	return
}

// --- Function: CAST(expr, 'Type') or tuple(...) ---

func unmarshalFunctionCST(pr *nanopass.ParseResult, ctx *grammar.ColumnExprFunctionContext, mapType func(string) (string, error)) (result TypedLiteral, err error) {
	ident := ctx.Identifier()
	if ident == nil {
		err = eh.Errorf("unmarshalFunctionCST: no identifier")
		return
	}
	funcName := strings.ToLower(ident.GetText())
	switch funcName {
	case "cast":
		return unmarshalCastFunctionCST(pr, ctx, mapType)
	case "tuple":
		return unmarshalTupleFunctionCST(pr, ctx, mapType)
	default:
		err = eh.Errorf("unmarshalFunctionCST: unsupported function %q (expected CAST or tuple)", funcName)
		return
	}
}

func unmarshalCastFunctionCST(pr *nanopass.ParseResult, ctx *grammar.ColumnExprFunctionContext, mapType func(string) (string, error)) (result TypedLiteral, err error) {
	var argList *grammar.ColumnArgListContext
	for i := 0; i < ctx.GetChildCount(); i++ {
		if al, ok := ctx.GetChild(i).(*grammar.ColumnArgListContext); ok {
			argList = al
			break
		}
	}
	if argList == nil {
		err = eh.Errorf("unmarshalCastFunctionCST: no arg list")
		return
	}

	var args []*grammar.ColumnArgExprContext
	for i := 0; i < argList.GetChildCount(); i++ {
		if arg, ok := argList.GetChild(i).(*grammar.ColumnArgExprContext); ok {
			args = append(args, arg)
		}
	}
	if len(args) != 2 {
		err = eh.Errorf("unmarshalCastFunctionCST: expected 2 args, got %d", len(args))
		return
	}

	typeText := nanopass.NodeText(pr, args[1])
	typeText = strings.TrimSpace(typeText)
	if len(typeText) < 2 || typeText[0] != '\'' || typeText[len(typeText)-1] != '\'' {
		err = eh.Errorf("unmarshalCastFunctionCST: second arg %q is not a quoted type", typeText)
		return
	}
	chType := typeText[1 : len(typeText)-1]

	if args[0].GetChildCount() == 0 {
		err = eh.Errorf("unmarshalCastFunctionCST: empty first arg")
		return
	}
	innerNode, ok := args[0].GetChild(0).(antlr.ParserRuleContext)
	if !ok {
		err = eh.Errorf("unmarshalCastFunctionCST: first arg child is not a rule context")
		return
	}

	result, err = UnmarshalCSTToTypedLiteral(pr, innerNode, mapType)
	if err != nil {
		err = eh.Errorf("unmarshalCastFunctionCST: inner expression: %w", err)
		return
	}
	result.CastTypeCanonical = mapChTypeToCanonical(chType, mapType)
	return
}

func unmarshalTupleFunctionCST(pr *nanopass.ParseResult, ctx *grammar.ColumnExprFunctionContext, mapType func(string) (string, error)) (result TypedLiteral, err error) {
	var argList *grammar.ColumnArgListContext
	for i := 0; i < ctx.GetChildCount(); i++ {
		if al, ok := ctx.GetChild(i).(*grammar.ColumnArgListContext); ok {
			argList = al
			break
		}
	}
	result.Kind = KindTuple
	result.Elements = make([]TypedLiteral, 0)
	if argList == nil {
		return
	}
	for i := 0; i < argList.GetChildCount(); i++ {
		arg, ok := argList.GetChild(i).(*grammar.ColumnArgExprContext)
		if !ok {
			continue
		}
		elem, elemErr := UnmarshalCSTToTypedLiteral(pr, arg, mapType)
		if elemErr != nil {
			err = eh.Errorf("unmarshalTupleFunctionCST: element %d: %w", len(result.Elements), elemErr)
			return
		}
		result.Elements = append(result.Elements, elem)
	}
	return
}

// --- Array: [elem1, elem2, ...] ---

func unmarshalArrayCST(pr *nanopass.ParseResult, ctx *grammar.ColumnExprArrayContext, mapType func(string) (string, error)) (result TypedLiteral, err error) {
	var exprList *grammar.ColumnExprListContext
	for i := 0; i < ctx.GetChildCount(); i++ {
		if el, ok := ctx.GetChild(i).(*grammar.ColumnExprListContext); ok {
			exprList = el
			break
		}
	}

	// Collect elements as heterogeneous first
	elems := make([]TypedLiteral, 0)
	if exprList != nil {
		for i := 0; i < exprList.GetChildCount(); i++ {
			colsExpr, ok := exprList.GetChild(i).(*grammar.ColumnsExprColumnContext)
			if !ok {
				continue
			}
			if colsExpr.GetChildCount() == 0 {
				continue
			}
			innerNode, ok := colsExpr.GetChild(0).(antlr.ParserRuleContext)
			if !ok {
				continue
			}
			elem, elemErr := UnmarshalCSTToTypedLiteral(pr, innerNode, mapType)
			if elemErr != nil {
				err = eh.Errorf("unmarshalArrayCST: element %d: %w", len(elems), elemErr)
				return
			}
			elems = append(elems, elem)
		}
	}

	// Try to promote to homogeneous
	hetResult := NewHeterogeneousArray(elems...)
	if homResult, ok := hetResult.TryHomogeneous(); ok {
		result = homResult
	} else {
		result = hetResult
	}
	return
}

// --- Cast expr: expr::Type or CAST(expr AS Type) ---

func unmarshalCastExprCST(pr *nanopass.ParseResult, ctx *grammar.ColumnExprCastContext, mapType func(string) (string, error)) (result TypedLiteral, err error) {
	var chType string
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		switch c := child.(type) {
		case *grammar.ColumnTypeExprSimpleContext:
			chType = c.GetText()
		case *grammar.ColumnTypeExprComplexContext:
			chType = c.GetText()
		}
	}

	var exprNode antlr.ParserRuleContext
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ruleCtx, ok := child.(antlr.ParserRuleContext); ok {
			_, isSimple := child.(*grammar.ColumnTypeExprSimpleContext)
			_, isComplex := child.(*grammar.ColumnTypeExprComplexContext)
			if !isSimple && !isComplex {
				exprNode = ruleCtx
				break
			}
		}
	}

	if exprNode == nil {
		err = eh.Errorf("unmarshalCastExprCST: no expression child found")
		return
	}

	result, err = UnmarshalCSTToTypedLiteral(pr, exprNode, mapType)
	if err != nil {
		err = eh.Errorf("unmarshalCastExprCST: inner expression: %w", err)
		return
	}
	result.CastTypeCanonical = mapChTypeToCanonical(chType, mapType)
	return
}

// --- Tuple expr: (elem1, elem2, ...) ---

func unmarshalTupleExprCST(pr *nanopass.ParseResult, ctx *grammar.ColumnExprTupleContext, mapType func(string) (string, error)) (result TypedLiteral, err error) {
	result.Kind = KindTuple
	result.Elements = make([]TypedLiteral, 0)

	var exprList *grammar.ColumnExprListContext
	for i := 0; i < ctx.GetChildCount(); i++ {
		if el, ok := ctx.GetChild(i).(*grammar.ColumnExprListContext); ok {
			exprList = el
			break
		}
	}
	if exprList == nil {
		return
	}

	for i := 0; i < exprList.GetChildCount(); i++ {
		colsExpr, ok := exprList.GetChild(i).(*grammar.ColumnsExprColumnContext)
		if !ok {
			continue
		}
		if colsExpr.GetChildCount() == 0 {
			continue
		}
		innerNode, ok := colsExpr.GetChild(0).(antlr.ParserRuleContext)
		if !ok {
			continue
		}
		elem, elemErr := UnmarshalCSTToTypedLiteral(pr, innerNode, mapType)
		if elemErr != nil {
			err = eh.Errorf("unmarshalTupleExprCST: element %d: %w", len(result.Elements), elemErr)
			return
		}
		result.Elements = append(result.Elements, elem)
	}
	return
}

// --- Helpers ---

func mapChTypeToCanonical(chType string, mapFunc func(string) (string, error)) string {
	if mapFunc == nil || chType == "" {
		return ""
	}
	canonical, err := mapFunc(chType)
	if err != nil {
		return ""
	}
	return canonical
}
