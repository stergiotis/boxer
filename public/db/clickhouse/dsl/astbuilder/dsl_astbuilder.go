// Package chsql provides a fluent builder for constructing ClickHouse DQL
// queries as ast.Query values.
//
// Errors are deferred: if Lit() receives an unsupported type, the error
// propagates through the chain and surfaces at Build() or ToSQL():
//
//	q, err := Select(Col("a")).Where(Col("a").Eq(Lit(badValue))).Build()
//	// err: "Lit: unsupported type ..."
//
//go:builder llm_generated_opus64
package astbuilder

import (
	"strconv"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ============================================================================
// E — expression with deferred error
// ============================================================================

// E is a fluent expression wrapper. It carries an optional error from
// constructors like Lit(). Errors propagate through operator methods and
// surface when the expression is consumed by SelectBuilder.Build().
type E struct {
	ast.Expr
	err error
}

// Err returns the deferred error, if any.
func (inst E) Err() error { return inst.err }

func errE(err error) E { return E{err: err} }

func mergeErr(a, b E) error {
	if a.err != nil {
		return a.err
	}
	return b.err
}

func mergeErrSlice(es []E) error {
	for _, e := range es {
		if e.err != nil {
			return e.err
		}
	}
	return nil
}

// --- Constructors ---

// Col creates a column reference. Parts: column, or table+column, or db+table+column.
func Col(parts ...string) E {
	ref := &ast.ColumnRefData{}
	switch len(parts) {
	case 1:
		ref.Column = parts[0]
	case 2:
		ref.Table = parts[0]
		ref.Column = parts[1]
	case 3:
		ref.Database = parts[0]
		ref.Table = parts[1]
		ref.Column = parts[2]
	}
	return E{Expr: ast.Expr{Kind: ast.KindColumnRef, ColRef: ref}}
}

var marshallingOptions = marshalling.MarshalOptions{
	PreserveCasts:            false,
	MapCanonicalToClickHouse: marshalling.MapCanonicalToClickHouseTypeStr,
}
var marshallingOptionsCast = marshalling.MarshalOptions{
	PreserveCasts:            true,
	MapCanonicalToClickHouse: marshalling.MapCanonicalToClickHouseTypeStr,
}

func preprocessLiteralValue(v any) any {
	switch vt := v.(type) {
	case int:
		v = int64(vt)
	case uint:
		v = uint64(vt)
	}
	return v
}

// Lit creates a literal expression from a Go value. Uses marshalling.MarshalGoValueToSQL.
// If the value type is unsupported, the error is deferred to Build().
func Lit(v interface{}) E {
	v = preprocessLiteralValue(v)
	sql, err := marshalling.MarshalGoValueToSQLWithOptions(v, marshallingOptions)
	if err != nil {
		return errE(eh.Errorf("Lit: %w", err))
	}
	return E{Expr: ast.Expr{Kind: ast.KindLiteral, Literal: &ast.LiteralData{SQL: sql}}}
}
func LitCast(v interface{}) E {
	v = preprocessLiteralValue(v)
	sql, typeName, err := marshalling.MarshalGoValueToSQLWithOptionsCast(v, marshallingOptionsCast)
	if err != nil {
		return errE(eh.Errorf("LitCast: %w", err))
	}
	return E{Expr: ast.Expr{Kind: ast.KindFunctionCall, Func: &ast.FuncCallData{
		Name: "CAST",
		Args: []ast.Expr{
			{Kind: ast.KindLiteral, Literal: &ast.LiteralData{SQL: sql}},
			{Kind: ast.KindLiteral, Literal: &ast.LiteralData{SQL: "'" + typeName + "'"}},
		},
	}}}
}

// Func creates a function call expression.
func Func(name string, args ...E) E {
	err := mergeErrSlice(args)
	a := make([]ast.Expr, len(args))
	for i, arg := range args {
		a[i] = arg.Expr
	}
	return E{Expr: ast.Expr{Kind: ast.KindFunctionCall, Func: &ast.FuncCallData{Name: name, Args: a}}, err: err}
}

// FuncDistinct creates a function call with DISTINCT.
func FuncDistinct(name string, args ...E) E {
	err := mergeErrSlice(args)
	a := make([]ast.Expr, len(args))
	for i, arg := range args {
		a[i] = arg.Expr
	}
	return E{Expr: ast.Expr{Kind: ast.KindFunctionCall, Func: &ast.FuncCallData{Name: name, Args: a, Distinct: true}}, err: err}
}

// Star creates a * or table.* expression.
func Star(table ...string) E {
	tbl := ""
	if len(table) > 0 {
		tbl = table[0]
	}
	return E{Expr: ast.Expr{Kind: ast.KindAsterisk, Asterisk: &ast.AsteriskData{Table: tbl}}}
}

// Sub creates a subquery expression from a SelectBuilder.
// Propagates any error from the sub-builder.
func Sub(sb SelectBuilder) E {
	if sb.err != nil {
		return errE(sb.err)
	}
	q := sb.buildUnion()
	return E{Expr: ast.Expr{Kind: ast.KindSubquery, Subquery: &ast.SubqueryData{Query: q}}}
}

// Interval creates an INTERVAL expression.
func Interval(val E, unit ast.IntervalUnitE) E {
	return E{Expr: ast.Expr{Kind: ast.KindInterval, Interval: &ast.IntervalData{Value: val.Expr, Unit: unit}}, err: val.err}
}

// Raw creates a literal expression from raw SQL text (escape hatch, never errors).
func Raw(sql string) E {
	return E{Expr: ast.Expr{Kind: ast.KindLiteral, Literal: &ast.LiteralData{SQL: sql}}}
}

// --- Operators ---

func (inst E) As(name string) E {
	return E{Expr: ast.Expr{Kind: ast.KindAlias, Alias: &ast.AliasData{Expr: inst.Expr, Name: name}}, err: inst.err}
}

func (inst E) Eq(other E) E     { return inst.binOp(ast.BinOpEq, other) }
func (inst E) NotEq(other E) E  { return inst.binOp(ast.BinOpNotEq, other) }
func (inst E) Gt(other E) E     { return inst.binOp(ast.BinOpGt, other) }
func (inst E) Lt(other E) E     { return inst.binOp(ast.BinOpLt, other) }
func (inst E) Ge(other E) E     { return inst.binOp(ast.BinOpGe, other) }
func (inst E) Le(other E) E     { return inst.binOp(ast.BinOpLe, other) }
func (inst E) And(other E) E    { return inst.binOp(ast.BinOpAnd, other) }
func (inst E) Or(other E) E     { return inst.binOp(ast.BinOpOr, other) }
func (inst E) Plus(other E) E   { return inst.binOp(ast.BinOpPlus, other) }
func (inst E) Minus(other E) E  { return inst.binOp(ast.BinOpMinus, other) }
func (inst E) Mul(other E) E    { return inst.binOp(ast.BinOpMultiply, other) }
func (inst E) Div(other E) E    { return inst.binOp(ast.BinOpDivide, other) }
func (inst E) Mod(other E) E    { return inst.binOp(ast.BinOpModulo, other) }
func (inst E) Concat(other E) E { return inst.binOp(ast.BinOpConcat, other) }
func (inst E) Like(other E) E   { return inst.binOp(ast.BinOpLike, other) }
func (inst E) ILike(other E) E  { return inst.binOp(ast.BinOpILike, other) }
func (inst E) In(other E) E     { return inst.binOp(ast.BinOpIn, other) }
func (inst E) NotIn(other E) E  { return inst.binOp(ast.BinOpNotIn, other) }

func (inst E) Not() E {
	return E{Expr: ast.Expr{Kind: ast.KindUnary, Unary: &ast.UnaryData{Op: ast.UnaryOpNot, Expr: inst.Expr}}, err: inst.err}
}

func (inst E) Neg() E {
	return E{Expr: ast.Expr{Kind: ast.KindUnary, Unary: &ast.UnaryData{Op: ast.UnaryOpNegate, Expr: inst.Expr}}, err: inst.err}
}

func (inst E) IsNull() E {
	return E{Expr: ast.Expr{Kind: ast.KindIsNull, IsNull: &ast.IsNullData{Expr: inst.Expr}}, err: inst.err}
}

func (inst E) IsNotNull() E {
	return E{Expr: ast.Expr{Kind: ast.KindIsNull, IsNull: &ast.IsNullData{Expr: inst.Expr, Negate: true}}, err: inst.err}
}

func (inst E) Between(low, high E) E {
	err := inst.err
	if err == nil {
		err = mergeErr(low, high)
	}
	return E{Expr: ast.Expr{Kind: ast.KindBetween, Between: &ast.BetweenData{
		Expr: inst.Expr, Low: low.Expr, High: high.Expr,
	}}, err: err}
}

func (inst E) NotBetween(low, high E) E {
	err := inst.err
	if err == nil {
		err = mergeErr(low, high)
	}
	return E{Expr: ast.Expr{Kind: ast.KindBetween, Between: &ast.BetweenData{
		Expr: inst.Expr, Low: low.Expr, High: high.Expr, Negate: true,
	}}, err: err}
}

func (inst E) binOp(op ast.BinaryOpE, other E) E {
	return E{Expr: ast.Expr{Kind: ast.KindBinary, Binary: &ast.BinaryData{
		Op: op, Left: inst.Expr, Right: other.Expr,
	}}, err: mergeErr(inst, other)}
}

// --- ORDER BY helpers ---

func (inst E) Asc() ast.OrderItem  { return ast.OrderItem{Expr: inst.Expr, Dir: ast.OrderDirAsc} }
func (inst E) Desc() ast.OrderItem { return ast.OrderItem{Expr: inst.Expr, Dir: ast.OrderDirDesc} }
func (inst E) Order() ast.OrderItem {
	return ast.OrderItem{Expr: inst.Expr}
}

// ============================================================================
// OrderItem helpers
// ============================================================================

func NullsFirst(o ast.OrderItem) ast.OrderItem { o.Nulls = ast.OrderNullsFirst; return o }
func NullsLast(o ast.OrderItem) ast.OrderItem  { o.Nulls = ast.OrderNullsLast; return o }

// ============================================================================
// SelectBuilder
// ============================================================================

// SelectBuilder accumulates SELECT clauses. Immutable — each method returns
// a value copy with pointer fields cloned before mutation.
//
// Errors from E values or nested SelectBuilders are captured and deferred
// to Build() / ToSQL().
type SelectBuilder struct {
	sel   ast.Select
	ctes  []ast.CTE
	items []ast.SelectUnionItem
	fmt   string
	err   error
}

func (inst SelectBuilder) withErr(err error) SelectBuilder {
	if inst.err == nil {
		inst.err = err
	}
	return inst
}

func (inst SelectBuilder) absorbE(es ...E) SelectBuilder {
	if inst.err != nil {
		return inst
	}
	for _, e := range es {
		if e.err != nil {
			inst.err = e.err
			return inst
		}
	}
	return inst
}

func (inst SelectBuilder) absorbSB(sb SelectBuilder) SelectBuilder {
	if inst.err == nil && sb.err != nil {
		inst.err = sb.err
	}
	return inst
}

// Select starts a new query with the given projection expressions.
func Select(cols ...E) SelectBuilder {
	proj := make([]ast.Expr, len(cols))
	for i, c := range cols {
		proj[i] = c.Expr
	}
	sb := SelectBuilder{sel: ast.Select{Projection: proj}}
	sb.err = mergeErrSlice(cols)
	return sb
}

func (inst SelectBuilder) Distinct() SelectBuilder {
	inst.sel.Distinct = true
	return inst
}

// From sets the FROM clause to a simple table reference. Parts: table or db+table.
func (inst SelectBuilder) From(parts ...string) SelectBuilder {
	td := &ast.JoinTableData{TableKind: ast.TableKindRef}
	switch len(parts) {
	case 1:
		td.Table = parts[0]
	case 2:
		td.Database = parts[0]
		td.Table = parts[1]
	}
	inst.sel.From = &ast.JoinExpr{Kind: ast.JoinExprTable, Table: td}
	return inst
}

// FromAlias sets the FROM clause to a table with an alias.
func (inst SelectBuilder) FromAlias(table, alias string) SelectBuilder {
	td := &ast.JoinTableData{TableKind: ast.TableKindRef, Table: table, Alias: alias}
	inst.sel.From = &ast.JoinExpr{Kind: ast.JoinExprTable, Table: td}
	return inst
}

// FromSubquery sets the FROM clause to a subquery.
func (inst SelectBuilder) FromSubquery(sub SelectBuilder, alias string) SelectBuilder {
	inst = inst.absorbSB(sub)
	su := sub.buildUnion()
	td := &ast.JoinTableData{TableKind: ast.TableKindSubquery, Subquery: &su, Alias: alias}
	inst.sel.From = &ast.JoinExpr{Kind: ast.JoinExprTable, Table: td}
	return inst
}

// FromExpr sets the FROM clause to an arbitrary ast.JoinExpr (for complex JOINs).
func (inst SelectBuilder) FromExpr(je ast.JoinExpr) SelectBuilder {
	inst.sel.From = &je
	return inst
}

// Final adds the FINAL modifier to the FROM table.
func (inst SelectBuilder) Final() SelectBuilder {
	if inst.sel.From != nil && inst.sel.From.Table != nil {
		fromCopy := *inst.sel.From
		tdCopy := *fromCopy.Table
		tdCopy.Final = true
		fromCopy.Table = &tdCopy
		inst.sel.From = &fromCopy
	}
	return inst
}

func (inst SelectBuilder) Prewhere(cond E) SelectBuilder {
	inst = inst.absorbE(cond)
	inst.sel.Prewhere = &cond.Expr
	return inst
}

func (inst SelectBuilder) Where(cond E) SelectBuilder {
	inst = inst.absorbE(cond)
	if inst.sel.Where != nil {
		combined := E{Expr: *inst.sel.Where}.And(cond)
		inst.sel.Where = &combined.Expr
	} else {
		inst.sel.Where = &cond.Expr
	}
	return inst
}

func (inst SelectBuilder) Qualify(cond E) SelectBuilder {
	inst = inst.absorbE(cond)
	inst.sel.Qualify = &cond.Expr
	return inst
}

func (inst SelectBuilder) GroupBy(exprs ...E) SelectBuilder {
	inst = inst.absorbE(exprs...)
	inst.sel.GroupBy = &ast.GroupByClause{Exprs: unwrap(exprs)}
	return inst
}

func (inst SelectBuilder) WithTotals() SelectBuilder {
	if inst.sel.GroupBy != nil {
		gbCopy := *inst.sel.GroupBy
		gbCopy.WithTotals = true
		inst.sel.GroupBy = &gbCopy
	}
	return inst
}

func (inst SelectBuilder) Having(cond E) SelectBuilder {
	inst = inst.absorbE(cond)
	inst.sel.Having = &cond.Expr
	return inst
}

func (inst SelectBuilder) OrderBy(items ...ast.OrderItem) SelectBuilder {
	inst.sel.OrderBy = &ast.OrderByClause{Items: items}
	return inst
}

func (inst SelectBuilder) Limit(n int) SelectBuilder {
	ls := ast.LimitSpec{Limit: litInt(n)}
	if inst.sel.Limit != nil {
		ls.Offset = inst.sel.Limit.Limit.Offset
	}
	inst.sel.Limit = &ast.LimitClause{Limit: ls}
	return inst
}

func (inst SelectBuilder) Offset(n int) SelectBuilder {
	off := litInt(n)
	if inst.sel.Limit != nil {
		lcCopy := *inst.sel.Limit
		lcCopy.Limit.Offset = &off
		inst.sel.Limit = &lcCopy
	} else {
		inst.sel.Limit = &ast.LimitClause{Limit: ast.LimitSpec{Offset: &off}}
	}
	return inst
}

func (inst SelectBuilder) LimitBy(n int, cols ...E) SelectBuilder {
	inst = inst.absorbE(cols...)
	inst.sel.LimitBy = &ast.LimitByClause{
		Limit:   ast.LimitSpec{Limit: litInt(n)},
		Columns: unwrap(cols),
	}
	return inst
}

func (inst SelectBuilder) Settings(key, val string) SelectBuilder {
	inst.sel.Settings = append(copySettings(inst.sel.Settings), ast.SettingPair{Key: key, ValueSQL: val})
	return inst
}

func (inst SelectBuilder) ArrayJoin(exprs ...E) SelectBuilder {
	inst = inst.absorbE(exprs...)
	inst.sel.ArrayJoin = &ast.ArrayJoinClause{Exprs: unwrap(exprs)}
	return inst
}

func (inst SelectBuilder) LeftArrayJoin(exprs ...E) SelectBuilder {
	inst = inst.absorbE(exprs...)
	inst.sel.ArrayJoin = &ast.ArrayJoinClause{Kind: ast.ArrayJoinLeft, Exprs: unwrap(exprs)}
	return inst
}

func (inst SelectBuilder) Format(f string) SelectBuilder {
	inst.fmt = f
	return inst
}

// --- CTE ---

func (inst SelectBuilder) With(name string, body SelectBuilder) SelectBuilder {
	inst = inst.absorbSB(body)
	bq := body.buildQuery()
	inst.ctes = append(copyCTEs(inst.ctes), ast.CTE{Name: name, Body: bq})
	return inst
}

// --- UNION ---

func (inst SelectBuilder) UnionAll(other SelectBuilder) SelectBuilder {
	inst = inst.absorbSB(other)
	inst.items = append(copyUnionItems(inst.items), ast.SelectUnionItem{
		Op: ast.UnionOpUnion, Modifier: ast.UnionModAll, Body: other.buildUnion(),
	})
	return inst
}

func (inst SelectBuilder) UnionDistinct(other SelectBuilder) SelectBuilder {
	inst = inst.absorbSB(other)
	inst.items = append(copyUnionItems(inst.items), ast.SelectUnionItem{
		Op: ast.UnionOpUnion, Modifier: ast.UnionModDistinct, Body: other.buildUnion(),
	})
	return inst
}

func (inst SelectBuilder) ExceptAll(other SelectBuilder) SelectBuilder {
	inst = inst.absorbSB(other)
	inst.items = append(copyUnionItems(inst.items), ast.SelectUnionItem{
		Op: ast.UnionOpExcept, Modifier: ast.UnionModAll, Body: other.buildUnion(),
	})
	return inst
}

// --- Build ---

func (inst SelectBuilder) buildUnion() ast.SelectUnion {
	return ast.SelectUnion{Head: inst.sel, Items: inst.items}
}

func (inst SelectBuilder) buildQuery() ast.Query {
	return ast.Query{
		CTEs:   inst.ctes,
		Body:   inst.buildUnion(),
		Format: inst.fmt,
	}
}

// Build constructs the final ast.Query. Returns the deferred error if any
// expression or nested builder in the chain failed.
func (inst SelectBuilder) Build() (query ast.Query, err error) {
	if inst.err != nil {
		err = inst.err
		return
	}
	query = inst.buildQuery()
	return
}

// ToSQL is a convenience shortcut for Build() + ToSQL().
// Returns the deferred error if any.
func (inst SelectBuilder) ToSQL() (result string, err error) {
	var query ast.Query
	query, err = inst.Build()
	if err != nil {
		return
	}
	result = query.ToSQL()
	return
}

// ============================================================================
// Helpers
// ============================================================================

func unwrap(es []E) []ast.Expr {
	out := make([]ast.Expr, len(es))
	for i, e := range es {
		out[i] = e.Expr
	}
	return out
}

func litInt(n int) ast.Expr {
	return ast.Expr{Kind: ast.KindLiteral, Literal: &ast.LiteralData{SQL: strconv.Itoa(n)}}
}

func copySettings(s []ast.SettingPair) []ast.SettingPair {
	out := make([]ast.SettingPair, len(s))
	copy(out, s)
	return out
}

func copyCTEs(c []ast.CTE) []ast.CTE {
	out := make([]ast.CTE, len(c))
	copy(out, c)
	return out
}

func copyUnionItems(items []ast.SelectUnionItem) []ast.SelectUnionItem {
	out := make([]ast.SelectUnionItem, len(items))
	copy(out, items)
	return out
}
