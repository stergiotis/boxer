//go:build llm_generated_opus46

package ast

import (
	"fmt"
	"strconv"
	"strings"
)

// ToGoCode emits Go source code that uses the chsql builder to reconstruct
// this query. The output is a Go expression of type chsql.SelectBuilder.
func (inst Query) ToGoCode() string {
	return inst.ToGoCodeWithPrefix("")
}

// ToGoCodeWithPrefix emits Go source code with the given package prefix.
func (inst Query) ToGoCodeWithPrefix(p string) string {
	var b strings.Builder
	g := &goEmitter{b: &b, p: p}

	g.emitSelectUnion(inst.Body)

	for _, cte := range inst.CTEs {
		b.WriteString(".\n\tWith(")
		b.WriteString(goStr(cte.Name))
		b.WriteString(", ")
		g.emitSelectUnion(cte.Body.Body)
		b.WriteByte(')')
	}

	if inst.Format != "" {
		b.WriteString(".\n\tFormat(")
		b.WriteString(goStr(inst.Format))
		b.WriteByte(')')
	}

	return b.String()
}

type goEmitter struct {
	b *strings.Builder
	p string
}

func (inst *goEmitter) w(s string) { inst.b.WriteString(s) }

func (inst *goEmitter) emitSelectUnion(su SelectUnion) {
	inst.emitSelect(su.Head)
	for _, item := range su.Items {
		switch item.Op {
		case UnionOpUnion:
			if item.Modifier == UnionModAll {
				inst.w(".\n\tUnionAll(")
			} else {
				inst.w(".\n\tUnionDistinct(")
			}
		case UnionOpExcept:
			inst.w(".\n\tExceptAll(")
		case UnionOpIntersect:
			inst.w(".\n\tIntersectAll(")
		}
		inst.emitSelectUnion(item.Body)
		inst.w(")")
	}
}

func (inst *goEmitter) emitSelect(sel Select) {
	inst.w(inst.p)
	inst.w("Select(")
	for i, e := range sel.Projection {
		if i > 0 {
			inst.w(", ")
		}
		inst.emitExpr(e)
	}
	inst.w(")")

	if sel.Distinct {
		inst.w(".\n\tDistinct()")
	}

	if sel.From != nil {
		inst.emitFrom(sel.From)
	}

	if sel.From != nil && sel.From.Kind == JoinExprTable && sel.From.Table != nil && sel.From.Table.Final {
		inst.w(".\n\tFinal()")
	}

	if sel.ArrayJoin != nil {
		if sel.ArrayJoin.Kind == ArrayJoinLeft {
			inst.w(".\n\tLeftArrayJoin(")
		} else {
			inst.w(".\n\tArrayJoin(")
		}
		inst.emitExprList(sel.ArrayJoin.Exprs)
		inst.w(")")
	}

	if sel.Prewhere != nil {
		inst.w(".\n\tPrewhere(")
		inst.emitExpr(*sel.Prewhere)
		inst.w(")")
	}

	if sel.Where != nil {
		inst.w(".\n\tWhere(")
		inst.emitExpr(*sel.Where)
		inst.w(")")
	}

	if sel.Qualify != nil {
		inst.w(".\n\tQualify(")
		inst.emitExpr(*sel.Qualify)
		inst.w(")")
	}

	if sel.GroupBy != nil {
		inst.w(".\n\tGroupBy(")
		inst.emitExprList(sel.GroupBy.Exprs)
		inst.w(")")
		if sel.GroupBy.WithTotals {
			inst.w(".WithTotals()")
		}
	}

	if sel.Having != nil {
		inst.w(".\n\tHaving(")
		inst.emitExpr(*sel.Having)
		inst.w(")")
	}

	if sel.OrderBy != nil {
		inst.w(".\n\tOrderBy(")
		for i, item := range sel.OrderBy.Items {
			if i > 0 {
				inst.w(", ")
			}
			inst.emitOrderItem(item)
		}
		inst.w(")")
	}

	if sel.LimitBy != nil {
		inst.w(".\n\tLimitBy(")
		inst.w(extractLitInt(sel.LimitBy.Limit.Limit))
		inst.w(", ")
		inst.emitExprList(sel.LimitBy.Columns)
		inst.w(")")
	}

	if sel.Limit != nil {
		inst.w(".\n\tLimit(")
		inst.w(extractLitInt(sel.Limit.Limit.Limit))
		inst.w(")")
		if sel.Limit.Limit.Offset != nil {
			inst.w(".Offset(")
			inst.w(extractLitInt(*sel.Limit.Limit.Offset))
			inst.w(")")
		}
	}

	for _, sp := range sel.Settings {
		inst.w(".\n\tSettings(")
		inst.w(goStr(sp.Key))
		inst.w(", ")
		inst.w(goStr(sp.ValueSQL))
		inst.w(")")
	}
}

// --- FROM ---

func (inst *goEmitter) emitFrom(je *JoinExpr) {
	switch je.Kind {
	case JoinExprTable:
		td := je.Table
		switch td.TableKind {
		case TableKindRef:
			if td.Alias != "" {
				inst.w(".\n\tFromAlias(")
				if td.Database != "" {
					inst.w(goStr(td.Database + "." + td.Table))
				} else {
					inst.w(goStr(td.Table))
				}
				inst.w(", ")
				inst.w(goStr(td.Alias))
				inst.w(")")
			} else {
				inst.w(".\n\tFrom(")
				if td.Database != "" {
					inst.w(goStr(td.Database))
					inst.w(", ")
				}
				inst.w(goStr(td.Table))
				inst.w(")")
			}
		case TableKindSubquery:
			inst.w(".\n\tFromSubquery(")
			if td.Subquery != nil {
				inst.emitSelectUnion(*td.Subquery)
			}
			inst.w(", ")
			inst.w(goStr(td.Alias))
			inst.w(")")
		case TableKindFunc:
			inst.w(".\n\tFromExpr(")
			inst.emitJoinExprLiteral(je)
			inst.w(")")
		}
	default:
		// Complex JOIN tree — emit as ast struct literal via FromExpr
		inst.w(".\n\tFromExpr(")
		inst.emitJoinExprLiteral(je)
		inst.w(")")
	}
}

// emitJoinExprLiteral emits an ast.JoinExpr as a Go struct literal.
// This handles any JOIN shape the builder can't express natively.
func (inst *goEmitter) emitJoinExprLiteral(je *JoinExpr) {
	inst.w("ast.JoinExpr{")
	switch je.Kind {
	case JoinExprTable:
		inst.w("Kind: ast.JoinExprTable, Table: &ast.JoinTableData{")
		td := je.Table
		inst.emitTableDataFields(td)
		inst.w("}")
	case JoinExprOp:
		inst.w("Kind: ast.JoinExprOp, Op: &ast.JoinOpData{")
		op := je.Op
		inst.w("Left: ")
		inst.emitJoinExprLiteral(&op.Left)
		inst.w(", Right: ")
		inst.emitJoinExprLiteral(&op.Right)
		inst.w(fmt.Sprintf(", Kind: %s", joinKindConst(op.Kind)))
		if op.Strictness != JoinStrictnessNone {
			inst.w(fmt.Sprintf(", Strictness: %s", joinStrictnessConst(op.Strictness)))
		}
		if op.Global {
			inst.w(", Global: true")
		}
		if op.Local {
			inst.w(", Local: true")
		}
		inst.w(", Constraint: ast.JoinConstraint{")
		if op.Constraint.Kind == JoinConstraintOn {
			inst.w("Kind: ast.JoinConstraintOn")
		} else {
			inst.w("Kind: ast.JoinConstraintUsing")
		}
		inst.w(", Exprs: []ast.Expr{")
		for i, e := range op.Constraint.Exprs {
			if i > 0 {
				inst.w(", ")
			}
			inst.emitExprAsLiteral(e)
		}
		inst.w("}}")
		inst.w("}")
	case JoinExprCross:
		inst.w("Kind: ast.JoinExprCross, Cross: &ast.JoinCrossData{")
		cross := je.Cross
		inst.w("Left: ")
		inst.emitJoinExprLiteral(&cross.Left)
		inst.w(", Right: ")
		inst.emitJoinExprLiteral(&cross.Right)
		if cross.Global {
			inst.w(", Global: true")
		}
		if cross.Local {
			inst.w(", Local: true")
		}
		inst.w("}")
	}
	inst.w("}")
}

func (inst *goEmitter) emitTableDataFields(td *JoinTableData) {
	switch td.TableKind {
	case TableKindRef:
		inst.w("TableKind: ast.TableKindRef")
		if td.Database != "" {
			inst.w(", Database: ")
			inst.w(goStr(td.Database))
		}
		inst.w(", Table: ")
		inst.w(goStr(td.Table))
	case TableKindFunc:
		inst.w("TableKind: ast.TableKindFunc, FuncName: ")
		inst.w(goStr(td.FuncName))
		if len(td.FuncArgs) > 0 {
			inst.w(", FuncArgs: []ast.Expr{")
			for i, arg := range td.FuncArgs {
				if i > 0 {
					inst.w(", ")
				}
				inst.emitExprAsLiteral(arg)
			}
			inst.w("}")
		}
	case TableKindSubquery:
		inst.w("TableKind: ast.TableKindSubquery")
		if td.Subquery != nil {
			inst.w(", Subquery: buildUnionPtr(")
			inst.emitSelectUnion(*td.Subquery)
			inst.w(")")
		}
	}
	if td.Alias != "" {
		inst.w(", Alias: ")
		inst.w(goStr(td.Alias))
	}
	if td.Final {
		inst.w(", Final: true")
	}
}

// emitExprAsLiteral emits an ast.Expr as a Go struct literal (not builder call).
// Used inside ast struct literals (e.g. JoinConstraint.Exprs).
func (inst *goEmitter) emitExprAsLiteral(e Expr) {
	// For simplicity, emit as the Expr produced by the builder expression,
	// wrapped in .Expr to extract the ast.Expr from chsql.E.
	inst.emitExpr(e)
	inst.w(".Expr")
}

// --- ORDER BY ---

func (inst *goEmitter) emitOrderItem(item OrderItem) {
	needsWrap := item.Nulls != OrderNullsDefault

	if needsWrap {
		inst.w(inst.p)
		switch item.Nulls {
		case OrderNullsFirst:
			inst.w("NullsFirst(")
		case OrderNullsLast:
			inst.w("NullsLast(")
		}
	}

	inst.emitExpr(item.Expr)
	switch item.Dir {
	case OrderDirAsc:
		inst.w(".Asc()")
	case OrderDirDesc:
		inst.w(".Desc()")
	default:
		inst.w(".Order()")
	}

	if needsWrap {
		inst.w(")")
	}
}

// --- Expressions ---

func (inst *goEmitter) emitExprList(exprs []Expr) {
	for i, e := range exprs {
		if i > 0 {
			inst.w(", ")
		}
		inst.emitExpr(e)
	}
}

func (inst *goEmitter) emitExpr(e Expr) {
	switch e.Kind {
	case KindLiteral:
		inst.w(inst.p)
		inst.w("Raw(")
		inst.w(goStr(e.Literal.SQL))
		inst.w(")")

	case KindColumnRef:
		inst.w(inst.p)
		inst.w("Col(")
		ref := e.ColRef
		parts := make([]string, 0, 3)
		if ref.Database != "" {
			parts = append(parts, ref.Database)
		}
		if ref.Table != "" {
			parts = append(parts, ref.Table)
		}
		parts = append(parts, ref.Column)
		for i, part := range parts {
			if i > 0 {
				inst.w(", ")
			}
			inst.w(goStr(part))
		}
		inst.w(")")
		if ref.Nested != "" {
			inst.w(fmt.Sprintf(" /* .%s */", ref.Nested))
		}

	case KindFunctionCall:
		fn := e.Func
		inst.w(inst.p)
		if fn.Distinct {
			inst.w("FuncDistinct(")
		} else {
			inst.w("Func(")
		}
		inst.w(goStr(fn.Name))
		for _, arg := range fn.Args {
			inst.w(", ")
			inst.emitExpr(arg)
		}
		inst.w(")")

	case KindWindowFunc:
		wfn := e.WinFunc
		inst.w(inst.p)
		inst.w("Func(")
		inst.w(goStr(wfn.Name))
		for _, arg := range wfn.Args {
			inst.w(", ")
			inst.emitExpr(arg)
		}
		inst.w(") /* OVER window */")

	case KindBinary:
		bin := e.Binary
		method := binOpMethod(bin.Op)
		if method != "" {
			inst.emitExpr(bin.Left)
			inst.w(".")
			inst.w(method)
			inst.w("(")
			inst.emitExpr(bin.Right)
			inst.w(")")
		} else {
			// Fallback: emit as Raw() with the SQL text
			inst.w(inst.p)
			inst.w("Raw(")
			tmpQ := Query{Body: SelectUnion{Head: Select{Projection: []Expr{e}}}}
			full := tmpQ.ToSQL()
			if strings.HasPrefix(full, "SELECT ") {
				inst.w(goStr(full[7:]))
			} else {
				inst.w(goStr(full))
			}
			inst.w(")")
		}

	case KindUnary:
		un := e.Unary
		switch un.Op {
		case UnaryOpNot:
			inst.emitExpr(un.Expr)
			inst.w(".Not()")
		case UnaryOpNegate:
			inst.emitExpr(un.Expr)
			inst.w(".Neg()")
		}

	case KindBetween:
		btw := e.Between
		inst.emitExpr(btw.Expr)
		if btw.Negate {
			inst.w(".NotBetween(")
		} else {
			inst.w(".Between(")
		}
		inst.emitExpr(btw.Low)
		inst.w(", ")
		inst.emitExpr(btw.High)
		inst.w(")")

	case KindIsNull:
		isn := e.IsNull
		inst.emitExpr(isn.Expr)
		if isn.Negate {
			inst.w(".IsNotNull()")
		} else {
			inst.w(".IsNull()")
		}

	case KindInterval:
		iv := e.Interval
		inst.w(inst.p)
		inst.w("Interval(")
		inst.emitExpr(iv.Value)
		inst.w(", ast.")
		inst.w(intervalUnitConst(iv.Unit))
		inst.w(")")

	case KindLambda:
		lam := e.Lambda
		inst.w(inst.p)
		inst.w("Raw(")
		inst.w(goStr(lambdaToSQL(lam)))
		inst.w(")")

	case KindAlias:
		als := e.Alias
		inst.emitExpr(als.Expr)
		inst.w(".As(")
		inst.w(goStr(als.Name))
		inst.w(")")

	case KindSubquery:
		inst.w(inst.p)
		inst.w("Sub(")
		inst.emitSelectUnion(e.Subquery.Query)
		inst.w(")")

	case KindAsterisk:
		inst.w(inst.p)
		if e.Asterisk.Table != "" {
			inst.w("Star(")
			inst.w(goStr(e.Asterisk.Table))
			inst.w(")")
		} else {
			inst.w("Star()")
		}

	case KindDynColumn:
		inst.w(inst.p)
		inst.w("Raw(")
		inst.w(goStr("COLUMNS('" + e.DynCol.Pattern + "')"))
		inst.w(")")

	case KindParamSlot:
		inst.w(inst.p)
		inst.w("Raw(")
		inst.w(goStr("{" + e.Param.Name + ": " + e.Param.Type + "}"))
		inst.w(")")
	}
}

// ============================================================================
// Helpers
// ============================================================================

func goStr(s string) string {
	return strconv.Quote(s)
}

func extractLitInt(e Expr) string {
	if e.Kind == KindLiteral && e.Literal != nil {
		return e.Literal.SQL
	}
	return "0"
}

func binOpMethod(op BinaryOpE) string {
	switch op {
	case BinOpEq:
		return "Eq"
	case BinOpNotEq:
		return "NotEq"
	case BinOpGt:
		return "Gt"
	case BinOpLt:
		return "Lt"
	case BinOpGe:
		return "Ge"
	case BinOpLe:
		return "Le"
	case BinOpAnd:
		return "And"
	case BinOpOr:
		return "Or"
	case BinOpPlus:
		return "Plus"
	case BinOpMinus:
		return "Minus"
	case BinOpMultiply:
		return "Mul"
	case BinOpDivide:
		return "Div"
	case BinOpModulo:
		return "Mod"
	case BinOpConcat:
		return "Concat"
	case BinOpLike:
		return "Like"
	case BinOpNotLike:
		return "NotLike"
	case BinOpILike:
		return "ILike"
	case BinOpNotILike:
		return "NotILike"
	case BinOpIn:
		return "In"
	case BinOpNotIn:
		return "NotIn"
	case BinOpGlobalIn:
		return "GlobalIn"
	case BinOpGlobalNotIn:
		return "GlobalNotIn"
	default:
		return ""
	}
}

func joinKindConst(k JoinKindE) string {
	switch k {
	case JoinKindInner:
		return "ast.JoinKindInner"
	case JoinKindLeft:
		return "ast.JoinKindLeft"
	case JoinKindRight:
		return "ast.JoinKindRight"
	case JoinKindFull:
		return "ast.JoinKindFull"
	default:
		return "ast.JoinKindInner"
	}
}

func joinStrictnessConst(s JoinStrictnessE) string {
	switch s {
	case JoinStrictnessAll:
		return "ast.JoinStrictnessAll"
	case JoinStrictnessAny:
		return "ast.JoinStrictnessAny"
	case JoinStrictnessSemi:
		return "ast.JoinStrictnessSemi"
	case JoinStrictnessAnti:
		return "ast.JoinStrictnessAnti"
	case JoinStrictnessAsof:
		return "ast.JoinStrictnessAsof"
	default:
		return "ast.JoinStrictnessNone"
	}
}

func intervalUnitConst(u IntervalUnitE) string {
	switch u {
	case IntervalSecond:
		return "IntervalSecond"
	case IntervalMinute:
		return "IntervalMinute"
	case IntervalHour:
		return "IntervalHour"
	case IntervalDay:
		return "IntervalDay"
	case IntervalWeek:
		return "IntervalWeek"
	case IntervalMonth:
		return "IntervalMonth"
	case IntervalQuarter:
		return "IntervalQuarter"
	case IntervalYear:
		return "IntervalYear"
	default:
		return "IntervalSecond"
	}
}

func lambdaToSQL(lam *LambdaData) string {
	var b strings.Builder
	if len(lam.Params) == 1 {
		b.WriteString(lam.Params[0])
	} else {
		b.WriteByte('(')
		b.WriteString(strings.Join(lam.Params, ", "))
		b.WriteByte(')')
	}
	b.WriteString(" -> ")
	tmpQ := Query{Body: SelectUnion{Head: Select{Projection: []Expr{lam.Body}}}}
	full := tmpQ.ToSQL()
	if strings.HasPrefix(full, "SELECT ") {
		b.WriteString(full[7:])
	} else {
		b.WriteString(full)
	}
	return b.String()
}
