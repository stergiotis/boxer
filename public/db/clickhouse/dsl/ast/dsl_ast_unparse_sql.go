package ast

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// --- Identifier emission ---

// identNeedsQuoting reports whether name must be emitted double-quoted: a
// bare emission is safe only when the real lexer reads the name back as
// exactly one IDENTIFIER token covering the whole input. Keywords, spaces,
// operators, and exotic spellings all fail the probe.
//
// Not cached: a per-call lexer spin-up over one short name is microseconds,
// and a package-global cache keyed by arbitrary names is an unbounded leak
// for long-running callers (and OOMs a fuzz worker over millions of names).
func identNeedsQuoting(name string) bool {
	if name == "" {
		return true
	}
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(name))
	lexer.RemoveErrorListeners()
	tok := lexer.NextToken()
	// A name whose own bytes form a quoted spelling ("x", `x`, "" …) lexes
	// as a single IDENTIFIER too, but raw emission would re-lex into the
	// DECODED name — it must be re-quoted like any other exotic spelling.
	return tok.GetTokenType() != grammar1.ClickHouseLexerIDENTIFIER ||
		tok.GetText() != name ||
		name[0] == '"' || name[0] == '`' ||
		lexer.NextToken().GetTokenType() != antlr.TokenEOF
}

// writeIdent emits an identifier, double-quoting (with escape encoding)
// whenever the bare spelling would not lex back to the same name.
func writeIdent(b *strings.Builder, name string) {
	if identNeedsQuoting(name) {
		b.WriteString(nanopass.QuoteIdentifier(name))
		return
	}
	b.WriteString(name)
}

// ToSQL converts a Query AST back to a valid SQL string.
// The output is valid ClickHouse SQL but not necessarily canonicalized —
// identifiers are emitted bare (unquoted) when they don't require quoting.
func (inst Query) ToSQL() string {
	var b strings.Builder
	for _, sp := range inst.Settings {
		b.WriteString("SET ")
		writeIdent(&b, sp.Key)
		b.WriteString(" = ")
		b.WriteString(sp.ValueSQL)
		b.WriteString(";\n")
	}
	if len(inst.CTEs) > 0 || len(inst.With) > 0 {
		b.WriteString("WITH ")
		for i, cte := range inst.CTEs {
			if i > 0 {
				b.WriteString(", ")
			}
			writeCTE(&b, cte)
		}
		if len(inst.With) > 0 {
			if len(inst.CTEs) > 0 {
				b.WriteString(", ")
			}
			writeExprList(&b, inst.With)
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
	writeIdent(b, cte.Name)
	if len(cte.ColumnAliases) > 0 {
		b.WriteByte('(')
		for i, a := range cte.ColumnAliases {
			if i > 0 {
				b.WriteString(", ")
			}
			writeIdent(b, a)
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
	if len(sel.CTEs) > 0 || len(sel.With) > 0 {
		b.WriteString("WITH ")
		for i, cte := range sel.CTEs {
			if i > 0 {
				b.WriteString(", ")
			}
			writeCTE(b, cte)
		}
		if len(sel.With) > 0 {
			if len(sel.CTEs) > 0 {
				b.WriteString(", ")
			}
			writeExprList(b, sel.With)
		}
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
			b.WriteString("COLUMNS(")
			b.WriteString(marshalling.EscapeString(sel.ExceptColumns.Dynamic))
			b.WriteString(")")
		} else {
			b.WriteByte('(')
			for i, c := range sel.ExceptColumns.Static {
				if i > 0 {
					b.WriteString(", ")
				}
				writeIdent(b, c)
			}
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
		writeIdent(b, sel.WindowDef.Name)
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
			writeIdent(b, sp.Key)
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
		b.WriteString(" COLLATE ")
		b.WriteString(marshalling.EscapeString(item.Collate))
	}
}

// --- JOIN ---

func writeJoinExpr(b *strings.Builder, je JoinExpr) {
	switch je.Kind {
	case JoinExprTable:
		if je.Table != nil {
			writeJoinTable(b, je.Table)
		}
	case JoinExprOp:
		if je.Op != nil {
			writeJoinOp(b, je.Op)
		}
	case JoinExprCross:
		if je.Cross != nil {
			writeJoinCross(b, je.Cross)
		}
	}
}

func writeJoinTable(b *strings.Builder, td *JoinTableData) {
	if td == nil {
		return
	}
	switch td.TableKind {
	case TableKindRef:
		if td.Database != "" {
			writeIdent(b, td.Database)
			b.WriteByte('.')
		}
		writeIdent(b, td.Table)
	case TableKindFunc:
		writeIdent(b, td.FuncName)
		b.WriteByte('(')
		writeExprList(b, td.FuncArgs)
		b.WriteByte(')')
	case TableKindSubquery:
		b.WriteByte('(')
		if td.Subquery != nil {
			writeSelectUnion(b, *td.Subquery)
		}
		b.WriteByte(')')
	}
	// The alias binds to the table expression and must precede the
	// FINAL/SAMPLE modifiers (joinExprTable: tableExpr FINAL? sample?).
	if td.Alias != "" {
		b.WriteString(" AS ")
		writeIdent(b, td.Alias)
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
		writeMaybeParens(b, e.Interval.Value, exprPrec(e.Interval.Value) < 9)
		b.WriteByte(' ')
		b.WriteString(intervalUnitSQL(e.Interval.Unit))
	case KindLambda:
		writeLambda(b, e.Lambda)
	case KindAlias:
		writeExpr(b, e.Alias.Expr)
		b.WriteString(" AS ")
		writeIdent(b, e.Alias.Name)
	case KindSubquery:
		b.WriteByte('(')
		writeSelectUnion(b, e.Subquery.Query)
		b.WriteByte(')')
	case KindAsterisk:
		if e.Asterisk.Table != "" {
			writeIdent(b, e.Asterisk.Table)
			b.WriteByte('.')
		}
		b.WriteByte('*')
	case KindDynColumn:
		b.WriteString("COLUMNS(")
		b.WriteString(marshalling.EscapeString(e.DynCol.Pattern))
		b.WriteString(")")
	}
}

func writeColumnRef(b *strings.Builder, ref *ColumnRefData) {
	if ref.Database != "" {
		writeIdent(b, ref.Database)
		b.WriteByte('.')
	}
	if ref.Table != "" {
		writeIdent(b, ref.Table)
		b.WriteByte('.')
	}
	writeIdent(b, ref.Column)
	if ref.Nested != "" {
		b.WriteByte('.')
		writeIdent(b, ref.Nested)
	}
}

func writeFuncCall(b *strings.Builder, fn *FuncCallData) {
	writeIdent(b, fn.Name)
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
	writeIdent(b, wfn.Name)
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
		writeIdent(b, wfn.WindowRef)
	} else if wfn.Window != nil {
		b.WriteByte('(')
		writeWindowSpec(b, *wfn.Window)
		b.WriteByte(')')
	}
}

// exprPrec mirrors Grammar1's columnExpr alternative ladder: an EARLIER
// alternative binds TIGHTER. Verified against parse trees — BETWEEN and the
// alias form are the loosest binders (below OR), unary minus and INTERVAL
// the tightest non-atoms. Atoms (literals, refs, calls, subqueries, …)
// never need parentheses and rank 99.
func exprPrec(e Expr) int {
	switch e.Kind {
	case KindAlias:
		return 0
	case KindBetween:
		return 1
	case KindBinary:
		switch e.Binary.Op {
		case BinOpOr:
			return 2
		case BinOpAnd:
			return 3
		case BinOpEq, BinOpNotEq, BinOpLt, BinOpGt, BinOpLe, BinOpGe,
			BinOpIn, BinOpNotIn, BinOpGlobalIn, BinOpGlobalNotIn,
			BinOpLike, BinOpNotLike, BinOpILike, BinOpNotILike:
			return 6
		case BinOpPlus, BinOpMinus, BinOpConcat:
			return 7
		default: // *, /, %
			return 8
		}
	case KindUnary:
		if e.Unary != nil && e.Unary.Op == UnaryOpNot {
			return 4
		}
		return 9
	case KindIsNull:
		return 5
	case KindInterval:
		return 9
	default:
		return 99
	}
}

func binOpPrec(op BinaryOpE) int {
	return exprPrec(Expr{Kind: KindBinary, Binary: &BinaryData{Op: op}})
}

func writeMaybeParens(b *strings.Builder, e Expr, parens bool) {
	if parens {
		b.WriteByte('(')
		writeExpr(b, e)
		b.WriteByte(')')
		return
	}
	writeExpr(b, e)
}

func writeBinary(b *strings.Builder, bin *BinaryData) {
	p := binOpPrec(bin.Op)
	// Left-associative grammar: a left operand of equal precedence reparses
	// identically bare; a right operand of equal precedence must keep its
	// parentheses (a - (b - c) ≠ a - b - c).
	writeMaybeParens(b, bin.Left, exprPrec(bin.Left) < p)
	b.WriteByte(' ')
	b.WriteString(binaryOpSQL(bin.Op))
	b.WriteByte(' ')
	writeMaybeParens(b, bin.Right, exprPrec(bin.Right) <= p)
}

func writeUnary(b *strings.Builder, un *UnaryData) {
	switch un.Op {
	case UnaryOpNot:
		b.WriteString("NOT ")
		writeMaybeParens(b, un.Expr, exprPrec(un.Expr) < 4)
	case UnaryOpNegate:
		b.WriteByte('-')
		if exprPrec(un.Expr) < 9 {
			b.WriteByte('(')
			writeExpr(b, un.Expr)
			b.WriteByte(')')
			return
		}
		var inner strings.Builder
		writeExpr(&inner, un.Expr)
		t := inner.String()
		if strings.HasPrefix(t, "-") {
			// "--" would open a line comment — parenthesize the operand.
			b.WriteByte('(')
			b.WriteString(t)
			b.WriteByte(')')
		} else {
			b.WriteString(t)
		}
	}
}

func writeBetween(b *strings.Builder, btw *BetweenData) {
	// The subject parses at BETWEEN level or tighter; the low bound must
	// stop before the structural AND, so anything at AND level or looser
	// needs parentheses. The high bound is parsed greedily (a BETWEEN b
	// AND c OR d groups as High = c OR d), so only BETWEEN-or-looser needs
	// them there.
	writeMaybeParens(b, btw.Expr, exprPrec(btw.Expr) <= 1)
	if btw.Negate {
		b.WriteString(" NOT BETWEEN ")
	} else {
		b.WriteString(" BETWEEN ")
	}
	writeMaybeParens(b, btw.Low, exprPrec(btw.Low) <= 3)
	b.WriteString(" AND ")
	writeMaybeParens(b, btw.High, exprPrec(btw.High) <= 1)
}

func writeIsNull(b *strings.Builder, isn *IsNullData) {
	writeMaybeParens(b, isn.Expr, exprPrec(isn.Expr) < 5)
	if isn.Negate {
		b.WriteString(" IS NOT NULL")
	} else {
		b.WriteString(" IS NULL")
	}
}

func writeLambda(b *strings.Builder, lam *LambdaData) {
	if len(lam.Params) == 1 {
		writeIdent(b, lam.Params[0])
	} else {
		b.WriteByte('(')
		for i, p := range lam.Params {
			if i > 0 {
				b.WriteString(", ")
			}
			writeIdent(b, p)
		}
		b.WriteByte(')')
	}
	b.WriteString(" -> ")
	writeExpr(b, lam.Body)
}

// --- SQL text for enums ---

func binaryOpSQL(op BinaryOpE) string {
	if info, ok := binaryOpInfo[op]; ok {
		return info.SQL
	}
	// Preserve the prior switch default for unknown ops.
	return "?"
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
