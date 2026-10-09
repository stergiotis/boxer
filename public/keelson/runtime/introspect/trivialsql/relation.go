package trivialsql

import (
	"bytes"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
)

// evaluator answers one statement: its registry, the constants and
// parameters its expressions may name, and its WITH queries in order.
type evaluator struct {
	reg   *introspect.Registry
	scope *keelsonsql.ConstScope
	ctes  []namedSelect
}

// namedSelect is a WITH query, `name AS (SELECT …)`; it may name the WITH
// queries before it.
type namedSelect struct {
	name string
	sel  *grammar1.SelectStmtContext
}

// readCtes takes the WITH queries of su; a WITH constant is the scope's,
// and any other WITH item is left alone unless something names it.
func (inst *evaluator) readCtes(su *grammar1.SelectUnionStmtContext) (err error) {
	c := su.Ctes()
	if c == nil {
		return nil
	}
	cc := c.(*grammar1.CtesContext)
	if cc.RECURSIVE() != nil {
		return refuse("WITH RECURSIVE")
	}
	for _, wi := range cc.AllWithItem() {
		nq, ok := wi.(*grammar1.WithItemNamedQueryContext)
		if !ok {
			continue
		}
		named := nq.NamedQuery().(*grammar1.NamedQueryContext)
		if named.ColumnAliases() != nil {
			return refuse("WITH column aliases")
		}
		inner := named.Query().(*grammar1.QueryContext)
		if len(inner.AllSetStmt()) > 0 {
			return refuse("SET inside WITH")
		}
		isu := inner.SelectUnionStmt().(*grammar1.SelectUnionStmtContext)
		if isu.Ctes() != nil {
			return refuse("a nested WITH")
		}
		isel, sErr := single(isu)
		if sErr != nil {
			return sErr
		}
		if isel.LimitClause() != nil || isel.SettingsClause() != nil {
			return refuse("LIMIT or SETTINGS inside WITH")
		}
		inst.ctes = append(inst.ctes, namedSelect{name: nanopass.DecodeIdentifier(named.Identifier().GetText()), sel: isel})
	}
	return nil
}

// single is the one plain SELECT of a union with no further arms.
func single(su *grammar1.SelectUnionStmtContext) (sel *grammar1.SelectStmtContext, err error) {
	if len(su.AllSelectUnionStmtItem()) > 0 {
		return nil, refuse("UNION, EXCEPT or INTERSECT")
	}
	sp := su.SelectStmtWithParens().(*grammar1.SelectStmtWithParensContext)
	s, ok := sp.SelectStmt().(*grammar1.SelectStmtContext)
	if !ok || s == nil {
		return nil, refuse("a parenthesised SELECT")
	}
	return s, nil
}

// projItem is one item of a SELECT list: * or a named constant.
type projItem struct {
	star  bool
	name  string
	value keelsonsql.Constant
}

// evalSelect answers sel, which may name the first visible WITH queries:
// its source read whole — a keelson() call, values(), a WITH query, or one
// row when there is no FROM — then its SELECT list of * and constants. A
// clause that filters, groups, orders or joins is refused.
func (inst *evaluator) evalSelect(sel *grammar1.SelectStmtContext, visible int) (batch arrow.RecordBatch, err error) {
	switch {
	case sel.ArrayJoinClause() != nil:
		return nil, refuse("ARRAY JOIN")
	case sel.WindowClause() != nil, sel.QualifyClause() != nil:
		return nil, refuse("WINDOW or QUALIFY")
	case sel.PrewhereClause() != nil, sel.WhereClause() != nil:
		return nil, refuse("WHERE")
	case sel.GroupByClause() != nil, sel.HavingClause() != nil:
		return nil, refuse("GROUP BY")
	case sel.OrderByClause() != nil:
		return nil, refuse("ORDER BY")
	case sel.LimitByClause() != nil:
		return nil, refuse("LIMIT BY")
	}
	items, err := inst.projection(sel)
	if err != nil {
		return nil, err
	}
	var src arrow.RecordBatch
	if sel.FromClause() == nil {
		src = array.NewRecordBatch(arrow.NewSchema(nil, nil), nil, 1)
	} else {
		src, err = inst.from(sel.FromClause().(*grammar1.FromClauseContext), visible)
		if err != nil {
			return nil, err
		}
	}
	defer src.Release()
	return project(src, items, sel.FromClause() != nil)
}

// projection reads the SELECT list: * and constants, each constant named
// by its alias or by ClickHouse's default name for it.
func (inst *evaluator) projection(sel *grammar1.SelectStmtContext) (items []projItem, err error) {
	proj := sel.ProjectionClause().(*grammar1.ProjectionClauseContext)
	if proj.DISTINCT() != nil || proj.TopClause() != nil || proj.ProjectionExceptClause() != nil {
		return nil, refuse("DISTINCT, TOP or EXCEPT")
	}
	for _, ce := range proj.ColumnExprList().(*grammar1.ColumnExprListContext).AllColumnsExpr() {
		switch col := ce.(type) {
		case *grammar1.ColumnsExprAsteriskContext:
			if col.TableIdentifier() != nil {
				return nil, refuse("a qualified * (" + col.GetText() + ")")
			}
			items = append(items, projItem{star: true})
		case *grammar1.ColumnsExprColumnContext:
			expr, name, aliased := keelsonsql.Aliased(col.ColumnExpr())
			if _, isParam := expr.(*grammar1.ColumnExprParamSlotContext); isParam && !aliased {
				return nil, refuse("a parameter without AS, which ClickHouse names _CAST(…)")
			}
			var v keelsonsql.Constant
			v, err = keelsonsql.EvalConstant(expr, inst.scope)
			if err != nil {
				return nil, constantErr(err)
			}
			if v.Null {
				return nil, refuse("NULL as a column, whose type is Nullable(Nothing)")
			}
			if !aliased {
				name = defaultName(v)
			}
			items = append(items, projItem{name: name, value: v})
		default:
			return nil, refuse("a subquery in the SELECT list")
		}
	}
	return
}

// constantErr is a refusal for an expression that is not a constant, and
// err itself otherwise.
func constantErr(err error) error {
	if nc, ok := err.(*keelsonsql.NotConstantError); ok {
		return refuse(nc.Found)
	}
	return err
}

// defaultName is ClickHouse's name for an unaliased constant: a WITH
// constant's alias, a literal's value — a float always with a point or an
// exponent, a string quoted and escaped. A parameter, which ClickHouse
// names _CAST(…), is refused without an alias before it gets here.
func defaultName(v keelsonsql.Constant) (name string) {
	if v.Origin == keelsonsql.ConstOriginWith {
		return v.Ref
	}
	var buf bytes.Buffer
	switch {
	case v.Type.IsFloat():
		writeFloat(&buf, v.Float, 64, styleTSV)
		if strings.Trim(buf.String(), "-0123456789") == "" {
			buf.WriteByte('.')
		}
	case v.Type == keelsonsql.ScalarTypeString:
		writeBytes(&buf, []byte(v.Str), styleQuoted)
	default:
		writeConstant(&buf, v)
	}
	return buf.String()
}

// from reads the FROM clause: one table, not joined.
func (inst *evaluator) from(fc *grammar1.FromClauseContext, visible int) (batch arrow.RecordBatch, err error) {
	var je *grammar1.JoinExprTableContext
	switch j := fc.JoinExpr().(type) {
	case *grammar1.JoinExprTableContext:
		je = j
	case *grammar1.JoinExprParensContext:
		return nil, refuse("a parenthesised FROM")
	default:
		return nil, refuse("JOIN")
	}
	if je.FINAL() != nil || je.SampleClause() != nil {
		return nil, refuse("FINAL or SAMPLE")
	}
	te := je.TableExpr()
	if al, isAlias := te.(*grammar1.TableExprAliasContext); isAlias {
		te = al.TableExpr()
	}
	switch t := te.(type) {
	case *grammar1.TableExprFunctionContext:
		f := t.TableFunctionExpr().(*grammar1.TableFunctionExprContext)
		switch {
		case keelsonsql.IsCall(f):
			p, raw, rErr := keelsonsql.ResolveCall(inst.reg, f, inst.scope)
			if rErr != nil {
				return nil, rErr
			}
			return introspect.SnapshotCall(p, introspect.AllColumns(), raw)
		case f.Identifier() != nil && strings.EqualFold(f.Identifier().GetText(), "values"):
			return inst.values(f)
		}
		return nil, refuse("a table function other than keelson() and values()")
	case *grammar1.TableExprIdentifierContext:
		ti := t.TableIdentifier().(*grammar1.TableIdentifierContext)
		if ti.DatabaseIdentifier() == nil {
			name := nanopass.DecodeIdentifier(ti.GetText())
			for i := visible - 1; i >= 0; i-- {
				if inst.ctes[i].name == name {
					return inst.evalSelect(inst.ctes[i].sel, i)
				}
			}
		}
		return nil, refuse("a table other than keelson() and values() (" + ti.GetText() + ")")
	}
	return nil, refuse("a subquery")
}

// project builds the SELECT list over src: * is src's columns, a constant a
// column of its value on every row. A name given twice, or a WITH constant
// named like a column of src, is refused: which one ClickHouse would pick
// is not modelled here.
func project(src arrow.RecordBatch, items []projItem, hasFrom bool) (batch arrow.RecordBatch, err error) {
	n := src.NumRows()
	var fields []arrow.Field
	var cols []arrow.Array
	defer func() {
		if err != nil {
			for _, c := range cols {
				c.Release()
			}
		}
	}()
	seen := make(map[string]struct{})
	add := func(f arrow.Field, col arrow.Array) error {
		if _, dup := seen[f.Name]; dup {
			col.Release()
			return refuse("two columns named " + f.Name)
		}
		seen[f.Name] = struct{}{}
		fields = append(fields, f)
		cols = append(cols, col)
		return nil
	}
	for _, it := range items {
		if it.star {
			if !hasFrom {
				return nil, refuse("SELECT * without FROM")
			}
			for i, f := range src.Schema().Fields() {
				c := src.Column(i)
				c.Retain()
				if err = add(f, c); err != nil {
					return nil, err
				}
			}
			continue
		}
		if it.value.Origin == keelsonsql.ConstOriginWith && src.Schema().HasField(it.value.Ref) {
			return nil, refuse("the WITH constant " + it.value.Ref + ", which is also a column")
		}
		if err = add(arrow.Field{Name: it.name, Type: it.value.Type.Arrow()}, constColumn(it.value, int(n))); err != nil {
			return nil, err
		}
	}
	batch = array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, n)
	for _, c := range cols {
		c.Release()
	}
	cols = nil
	return batch, nil
}

// constColumn is n copies of v.
func constColumn(v keelsonsql.Constant, n int) arrow.Array {
	b := array.NewBuilder(memory.DefaultAllocator, v.Type.Arrow())
	defer b.Release()
	for range n {
		appendConstant(b, v)
	}
	return b.NewArray()
}

// appendConstant appends v to b, a builder of v's Arrow type.
func appendConstant(b array.Builder, v keelsonsql.Constant) {
	if v.Null {
		b.AppendNull()
		return
	}
	switch bb := b.(type) {
	case *array.Uint8Builder:
		bb.Append(uint8(v.Uint))
	case *array.Uint16Builder:
		bb.Append(uint16(v.Uint))
	case *array.Uint32Builder:
		bb.Append(uint32(v.Uint))
	case *array.Uint64Builder:
		bb.Append(v.Uint)
	case *array.Int8Builder:
		bb.Append(int8(v.Int))
	case *array.Int16Builder:
		bb.Append(int16(v.Int))
	case *array.Int32Builder:
		bb.Append(int32(v.Int))
	case *array.Int64Builder:
		bb.Append(v.Int)
	case *array.Float32Builder:
		bb.Append(float32(v.Float))
	case *array.Float64Builder:
		bb.Append(v.Float)
	case *array.StringBuilder:
		bb.Append(v.Str)
	case *array.BooleanBuilder:
		bb.Append(v.Bool)
	}
}

// writeConstant writes an integer or Bool constant as a text format does.
func writeConstant(buf *bytes.Buffer, v keelsonsql.Constant) {
	b := array.NewBuilder(memory.DefaultAllocator, v.Type.Arrow())
	defer b.Release()
	appendConstant(b, v)
	a := b.NewArray()
	defer a.Release()
	writeValue(buf, a, 0, styleTSV)
}
