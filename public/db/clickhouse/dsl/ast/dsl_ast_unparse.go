//go:build llm_generated_opus46

package ast

import (
	"fmt"
	"strings"
)

// ToSQL converts a Query AST back to a valid SQL string.
// The output is valid ClickHouse SQL but not necessarily canonicalized —
// identifiers are emitted bare (unquoted) when they don't require quoting.
func (inst Query) ToSQL() string {
	var b strings.Builder
	for _, sp := range inst.Settings {
		b.WriteString("SET ")
		b.WriteString(sp.Key)
		b.WriteString(" = ")
		b.WriteString(sp.ValueSQL)
		b.WriteString(";\n")
	}
	if len(inst.CTEs) > 0 {
		b.WriteString("WITH ")
		for i, cte := range inst.CTEs {
			if i > 0 {
				b.WriteString(", ")
			}
			writeCTE(&b, cte)
		}
		b.WriteByte(' ')
	}
	writeSelectUnion(&b, inst.Body)
	if inst.Format != "" {
		b.WriteString(" FORMAT ")
		b.WriteString(inst.Format)
	}
	return b.String()
}

func writeCTE(b *strings.Builder, cte CTE) {
	b.WriteString(cte.Name)
	if len(cte.ColumnAliases) > 0 {
		b.WriteByte('(')
		for i, a := range cte.ColumnAliases {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(a)
		}
		b.WriteByte(')')
	}
	b.WriteString(" AS (")
	b.WriteString(cte.Body.ToSQL())
	b.WriteByte(')')
}

func writeSelectUnion(b *strings.Builder, su SelectUnion) {
	writeSelect(b, su.Head)
	for _, item := range su.Items {
		b.WriteByte(' ')
		switch item.Op {
		case UnionOpUnion:
			b.WriteString("UNION")
		case UnionOpExcept:
			b.WriteString("EXCEPT")
		case UnionOpIntersect:
			b.WriteString("INTERSECT")
		}
		switch item.Modifier {
		case UnionModAll:
			b.WriteString(" ALL")
		case UnionModDistinct:
			b.WriteString(" DISTINCT")
		}
		b.WriteByte(' ')
		if len(item.Body.Items) > 0 {
			b.WriteByte('(')
			writeSelectUnion(b, item.Body)
			b.WriteByte(')')
		} else {
			writeSelect(b, item.Body.Head)
		}
	}
}

func writeSelect(b *strings.Builder, sel Select) {
	if len(sel.With) > 0 {
		b.WriteString("WITH ")
		writeExprList(b, sel.With)
		b.WriteByte(' ')
	}
	b.WriteString("SELECT ")
	if sel.Distinct {
		b.WriteString("DISTINCT ")
	}
	if sel.Top != nil {
		fmt.Fprintf(b, "TOP %d ", sel.Top.N)
		if sel.Top.WithTies {
			b.WriteString("WITH TIES ")
		}
	}
	writeExprList(b, sel.Projection)
	if sel.ExceptColumns != nil {
		b.WriteString(" EXCEPT ")
		if sel.ExceptColumns.Dynamic != "" {
			b.WriteString("COLUMNS('")
			b.WriteString(sel.ExceptColumns.Dynamic)
			b.WriteString("')")
		} else {
			b.WriteByte('(')
			b.WriteString(strings.Join(sel.ExceptColumns.Static, ", "))
			b.WriteByte(')')
		}
	}
	if sel.From != nil {
		b.WriteString(" FROM ")
		writeJoinExpr(b, *sel.From)
	}
	if sel.ArrayJoin != nil {
		b.WriteByte(' ')
		switch sel.ArrayJoin.Kind {
		case ArrayJoinLeft:
			b.WriteString("LEFT ")
		case ArrayJoinInner:
			b.WriteString("INNER ")
		}
		b.WriteString("ARRAY JOIN ")
		writeExprList(b, sel.ArrayJoin.Exprs)
	}
	if sel.WindowDef != nil {
		b.WriteString(" WINDOW ")
		b.WriteString(sel.WindowDef.Name)
		b.WriteString(" AS (")
		writeWindowSpec(b, sel.WindowDef.Window)
		b.WriteByte(')')
	}
	if sel.Qualify != nil {
		b.WriteString(" QUALIFY ")
		writeExpr(b, *sel.Qualify)
	}
	if sel.Prewhere != nil {
		b.WriteString(" PREWHERE ")
		writeExpr(b, *sel.Prewhere)
	}
	if sel.Where != nil {
		b.WriteString(" WHERE ")
		writeExpr(b, *sel.Where)
	}
	if sel.GroupBy != nil {
		b.WriteString(" GROUP BY ")
		if sel.GroupBy.Modifier == GroupByModCube {
			b.WriteString("CUBE(")
			writeExprList(b, sel.GroupBy.Exprs)
			b.WriteByte(')')
		} else if sel.GroupBy.Modifier == GroupByModRollup {
			b.WriteString("ROLLUP(")
			writeExprList(b, sel.GroupBy.Exprs)
			b.WriteByte(')')
		} else {
			writeExprList(b, sel.GroupBy.Exprs)
		}
		if sel.GroupBy.WithTotals {
			b.WriteString(" WITH TOTALS")
		}
	}
	if sel.Having != nil {
		b.WriteString(" HAVING ")
		writeExpr(b, *sel.Having)
	}
	if sel.OrderBy != nil {
		b.WriteString(" ORDER BY ")
		for i, item := range sel.OrderBy.Items {
			if i > 0 {
				b.WriteString(", ")
			}
			writeOrderItem(b, item)
		}
	}
	if sel.LimitBy != nil {
		b.WriteString(" LIMIT ")
		writeLimitSpec(b, sel.LimitBy.Limit)
		b.WriteString(" BY ")
		writeExprList(b, sel.LimitBy.Columns)
	}
	if sel.Limit != nil {
		b.WriteString(" LIMIT ")
		writeLimitSpec(b, sel.Limit.Limit)
		if sel.Limit.WithTies {
			b.WriteString(" WITH TIES")
		}
	}
	if len(sel.Settings) > 0 {
		b.WriteString(" SETTINGS ")
		for i, sp := range sel.Settings {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(sp.Key)
			b.WriteString(" = ")
			b.WriteString(sp.ValueSQL)
		}
	}
}

func writeLimitSpec(b *strings.Builder, ls LimitSpec) {
	writeExpr(b, ls.Limit)
	if ls.Offset != nil {
		b.WriteString(" OFFSET ")
		writeExpr(b, *ls.Offset)
	}
}

func writeOrderItem(b *strings.Builder, item OrderItem) {
	writeExpr(b, item.Expr)
	switch item.Dir {
	case OrderDirAsc:
		b.WriteString(" ASC")
	case OrderDirDesc:
		b.WriteString(" DESC")
	}
	switch item.Nulls {
	case OrderNullsFirst:
		b.WriteString(" NULLS FIRST")
	case OrderNullsLast:
		b.WriteString(" NULLS LAST")
	}
	if item.Collate != "" {
		b.WriteString(" COLLATE '")
		b.WriteString(item.Collate)
		b.WriteByte('\'')
	}
}

// --- JOIN ---

func writeJoinExpr(b *strings.Builder, je JoinExpr) {
	switch je.Kind {
	case JoinExprTable:
		writeJoinTable(b, je.Table)
	case JoinExprOp:
		writeJoinOp(b, je.Op)
	case JoinExprCross:
		writeJoinCross(b, je.Cross)
	}
}

func writeJoinTable(b *strings.Builder, td *JoinTableData) {
	switch td.TableKind {
	case TableKindRef:
		if td.Database != "" {
			b.WriteString(td.Database)
			b.WriteByte('.')
		}
		b.WriteString(td.Table)
	case TableKindFunc:
		b.WriteString(td.FuncName)
		b.WriteByte('(')
		writeExprList(b, td.FuncArgs)
		b.WriteByte(')')
	case TableKindSubquery:
		b.WriteByte('(')
		writeSelectUnion(b, *td.Subquery)
		b.WriteByte(')')
	}
	if td.Final {
		b.WriteString(" FINAL")
	}
	if td.Sample != nil {
		b.WriteString(" SAMPLE ")
		writeRatio(b, td.Sample.Ratio)
		if td.Sample.Offset != nil {
			b.WriteString(" OFFSET ")
			writeRatio(b, *td.Sample.Offset)
		}
	}
	if td.Alias != "" {
		b.WriteString(" AS ")
		b.WriteString(td.Alias)
	}
}

func writeRatio(b *strings.Builder, r RatioExpr) {
	b.WriteString(r.Numerator)
	if r.Denominator != "" {
		b.WriteByte('/')
		b.WriteString(r.Denominator)
	}
}

func writeJoinOp(b *strings.Builder, op *JoinOpData) {
	writeJoinExpr(b, op.Left)
	b.WriteByte(' ')
	if op.Global {
		b.WriteString("GLOBAL ")
	}
	if op.Local {
		b.WriteString("LOCAL ")
	}
	switch op.Strictness {
	case JoinStrictnessAll:
		b.WriteString("ALL ")
	case JoinStrictnessAny:
		b.WriteString("ANY ")
	case JoinStrictnessSemi:
		b.WriteString("SEMI ")
	case JoinStrictnessAnti:
		b.WriteString("ANTI ")
	case JoinStrictnessAsof:
		b.WriteString("ASOF ")
	}
	switch op.Kind {
	case JoinKindInner:
		b.WriteString("JOIN ")
	case JoinKindLeft:
		b.WriteString("LEFT JOIN ")
	case JoinKindRight:
		b.WriteString("RIGHT JOIN ")
	case JoinKindFull:
		b.WriteString("FULL JOIN ")
	}
	writeJoinExpr(b, op.Right)
	switch op.Constraint.Kind {
	case JoinConstraintOn:
		b.WriteString(" ON ")
		writeExprList(b, op.Constraint.Exprs)
	case JoinConstraintUsing:
		b.WriteString(" USING (")
		writeExprList(b, op.Constraint.Exprs)
		b.WriteByte(')')
	}
}

func writeJoinCross(b *strings.Builder, cross *JoinCrossData) {
	writeJoinExpr(b, cross.Left)
	b.WriteByte(' ')
	if cross.Global {
		b.WriteString("GLOBAL ")
	}
	if cross.Local {
		b.WriteString("LOCAL ")
	}
	b.WriteString("CROSS JOIN ")
	writeJoinExpr(b, cross.Right)
}

// --- Window ---

func writeWindowSpec(b *strings.Builder, ws WindowSpec) {
	needSpace := false
	if len(ws.PartitionBy) > 0 {
		b.WriteString("PARTITION BY ")
		writeExprList(b, ws.PartitionBy)
		needSpace = true
	}
	if len(ws.OrderBy) > 0 {
		if needSpace {
			b.WriteByte(' ')
		}
		b.WriteString("ORDER BY ")
		for i, item := range ws.OrderBy {
			if i > 0 {
				b.WriteString(", ")
			}
			writeOrderItem(b, item)
		}
		needSpace = true
	}
	if ws.Frame != nil {
		if needSpace {
			b.WriteByte(' ')
		}
		writeWindowFrame(b, ws.Frame)
	}
}

func writeWindowFrame(b *strings.Builder, f *WindowFrame) {
	switch f.Unit {
	case FrameUnitRows:
		b.WriteString("ROWS ")
	case FrameUnitRange:
		b.WriteString("RANGE ")
	}
	if f.End != nil {
		b.WriteString("BETWEEN ")
		writeFrameBound(b, f.Start)
		b.WriteString(" AND ")
		writeFrameBound(b, *f.End)
	} else {
		writeFrameBound(b, f.Start)
	}
}

func writeFrameBound(b *strings.Builder, fb FrameBound) {
	switch fb.Kind {
	case FrameBoundCurrentRow:
		b.WriteString("CURRENT ROW")
	case FrameBoundUnboundedPreceding:
		b.WriteString("UNBOUNDED PRECEDING")
	case FrameBoundUnboundedFollowing:
		b.WriteString("UNBOUNDED FOLLOWING")
	case FrameBoundNPreceding:
		b.WriteString(fb.N)
		b.WriteString(" PRECEDING")
	case FrameBoundNFollowing:
		b.WriteString(fb.N)
		b.WriteString(" FOLLOWING")
	}
}

// --- Expressions ---

func writeExprList(b *strings.Builder, exprs []Expr) {
	for i, e := range exprs {
		if i > 0 {
			b.WriteString(", ")
		}
		writeExpr(b, e)
	}
}

func writeExpr(b *strings.Builder, e Expr) {
	switch e.Kind {
	case KindLiteral:
		b.WriteString(e.Literal.SQL)
	case KindParamSlot:
		b.WriteByte('{')
		b.WriteString(e.Param.Name)
		b.WriteString(": ")
		b.WriteString(e.Param.Type)
		b.WriteByte('}')
	case KindColumnRef:
		writeColumnRef(b, e.ColRef)
	case KindFunctionCall:
		writeFuncCall(b, e.Func)
	case KindWindowFunc:
		writeWindowFunc(b, e.WinFunc)
	case KindBinary:
		writeBinary(b, e.Binary)
	case KindUnary:
		writeUnary(b, e.Unary)
	case KindBetween:
		writeBetween(b, e.Between)
	case KindIsNull:
		writeIsNull(b, e.IsNull)
	case KindInterval:
		b.WriteString("INTERVAL ")
		writeExpr(b, e.Interval.Value)
		b.WriteByte(' ')
		b.WriteString(intervalUnitSQL(e.Interval.Unit))
	case KindLambda:
		writeLambda(b, e.Lambda)
	case KindAlias:
		writeExpr(b, e.Alias.Expr)
		b.WriteString(" AS ")
		b.WriteString(e.Alias.Name)
	case KindSubquery:
		b.WriteByte('(')
		writeSelectUnion(b, e.Subquery.Query)
		b.WriteByte(')')
	case KindAsterisk:
		if e.Asterisk.Table != "" {
			b.WriteString(e.Asterisk.Table)
			b.WriteByte('.')
		}
		b.WriteByte('*')
	case KindDynColumn:
		b.WriteString("COLUMNS('")
		b.WriteString(e.DynCol.Pattern)
		b.WriteString("')")
	}
}

func writeColumnRef(b *strings.Builder, ref *ColumnRefData) {
	if ref.Database != "" {
		b.WriteString(ref.Database)
		b.WriteByte('.')
	}
	if ref.Table != "" {
		b.WriteString(ref.Table)
		b.WriteByte('.')
	}
	b.WriteString(ref.Column)
	if ref.Nested != "" {
		b.WriteByte('.')
		b.WriteString(ref.Nested)
	}
}

func writeFuncCall(b *strings.Builder, fn *FuncCallData) {
	b.WriteString(fn.Name)
	if len(fn.Params) > 0 {
		b.WriteByte('(')
		writeExprList(b, fn.Params)
		b.WriteByte(')')
	}
	b.WriteByte('(')
	if fn.Distinct {
		b.WriteString("DISTINCT ")
	}
	writeExprList(b, fn.Args)
	b.WriteByte(')')
}

func writeWindowFunc(b *strings.Builder, wfn *WindowFuncData) {
	b.WriteString(wfn.Name)
	if len(wfn.Params) > 0 {
		b.WriteByte('(')
		writeExprList(b, wfn.Params)
		b.WriteByte(')')
	}
	b.WriteByte('(')
	writeExprList(b, wfn.Args)
	b.WriteByte(')')
	b.WriteString(" OVER ")
	if wfn.WindowRef != "" {
		b.WriteString(wfn.WindowRef)
	} else if wfn.Window != nil {
		b.WriteByte('(')
		writeWindowSpec(b, *wfn.Window)
		b.WriteByte(')')
	}
}

// binaryOpNeedsParens returns true if the binary operator has lower precedence
// than common contexts where it appears, warranting parentheses on operands.
// We parenthesize AND/OR operands conservatively to avoid ambiguity.
func binaryNeedsParens(child Expr, parentOp BinaryOpE) bool {
	if child.Kind != KindBinary {
		return false
	}
	childPrec := binOpPrecedence(child.Binary.Op)
	parentPrec := binOpPrecedence(parentOp)
	return childPrec < parentPrec
}

func binOpPrecedence(op BinaryOpE) int {
	switch op {
	case BinOpOr:
		return 1
	case BinOpAnd:
		return 2
	case BinOpEq, BinOpNotEq, BinOpLt, BinOpGt, BinOpLe, BinOpGe,
		BinOpIn, BinOpNotIn, BinOpGlobalIn, BinOpGlobalNotIn,
		BinOpLike, BinOpNotLike, BinOpILike, BinOpNotILike:
		return 3
	case BinOpPlus, BinOpMinus, BinOpConcat:
		return 4
	case BinOpMultiply, BinOpDivide, BinOpModulo:
		return 5
	default:
		return 0
	}
}

func writeBinary(b *strings.Builder, bin *BinaryData) {
	if binaryNeedsParens(bin.Left, bin.Op) {
		b.WriteByte('(')
		writeExpr(b, bin.Left)
		b.WriteByte(')')
	} else {
		writeExpr(b, bin.Left)
	}
	b.WriteByte(' ')
	b.WriteString(binaryOpSQL(bin.Op))
	b.WriteByte(' ')
	if binaryNeedsParens(bin.Right, bin.Op) {
		b.WriteByte('(')
		writeExpr(b, bin.Right)
		b.WriteByte(')')
	} else {
		writeExpr(b, bin.Right)
	}
}

func writeUnary(b *strings.Builder, un *UnaryData) {
	switch un.Op {
	case UnaryOpNot:
		b.WriteString("NOT ")
		writeExpr(b, un.Expr)
	case UnaryOpNegate:
		b.WriteByte('-')
		writeExpr(b, un.Expr)
	}
}

func writeBetween(b *strings.Builder, btw *BetweenData) {
	writeExpr(b, btw.Expr)
	if btw.Negate {
		b.WriteString(" NOT BETWEEN ")
	} else {
		b.WriteString(" BETWEEN ")
	}
	writeExpr(b, btw.Low)
	b.WriteString(" AND ")
	writeExpr(b, btw.High)
}

func writeIsNull(b *strings.Builder, isn *IsNullData) {
	writeExpr(b, isn.Expr)
	if isn.Negate {
		b.WriteString(" IS NOT NULL")
	} else {
		b.WriteString(" IS NULL")
	}
}

func writeLambda(b *strings.Builder, lam *LambdaData) {
	if len(lam.Params) == 1 {
		b.WriteString(lam.Params[0])
	} else {
		b.WriteByte('(')
		b.WriteString(strings.Join(lam.Params, ", "))
		b.WriteByte(')')
	}
	b.WriteString(" -> ")
	writeExpr(b, lam.Body)
}

// --- SQL text for enums ---

func binaryOpSQL(op BinaryOpE) string {
	switch op {
	case BinOpAnd:
		return "AND"
	case BinOpOr:
		return "OR"
	case BinOpPlus:
		return "+"
	case BinOpMinus:
		return "-"
	case BinOpMultiply:
		return "*"
	case BinOpDivide:
		return "/"
	case BinOpModulo:
		return "%"
	case BinOpConcat:
		return "||"
	case BinOpEq:
		return "="
	case BinOpNotEq:
		return "!="
	case BinOpLt:
		return "<"
	case BinOpGt:
		return ">"
	case BinOpLe:
		return "<="
	case BinOpGe:
		return ">="
	case BinOpIn:
		return "IN"
	case BinOpNotIn:
		return "NOT IN"
	case BinOpGlobalIn:
		return "GLOBAL IN"
	case BinOpGlobalNotIn:
		return "GLOBAL NOT IN"
	case BinOpLike:
		return "LIKE"
	case BinOpNotLike:
		return "NOT LIKE"
	case BinOpILike:
		return "ILIKE"
	case BinOpNotILike:
		return "NOT ILIKE"
	default:
		return "?"
	}
}

func intervalUnitSQL(u IntervalUnitE) string {
	switch u {
	case IntervalSecond:
		return "SECOND"
	case IntervalMinute:
		return "MINUTE"
	case IntervalHour:
		return "HOUR"
	case IntervalDay:
		return "DAY"
	case IntervalWeek:
		return "WEEK"
	case IntervalMonth:
		return "MONTH"
	case IntervalQuarter:
		return "QUARTER"
	case IntervalYear:
		return "YEAR"
	default:
		return "?"
	}
}
