package trivialsql

import (
	"bytes"
	"errors"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// evaluator answers one statement: its registry, the constants and
// parameters its expressions may name, and its WITH queries in order.
type evaluator struct {
	reg   *introspect.Registry
	scope *keelsonsql.ConstScope
	ctes  []namedSelect
	// tables are the relations the caller bound by name ([ReadTables]);
	// reads names the ones the statement read, in order of first read.
	tables map[string]arrow.RecordBatch
	reads  []string
}

// namedSelect is a WITH query, `name AS (SELECT …)`; it may name the WITH
// queries before it.
type namedSelect struct {
	name string
	sel  *grammar1.SelectStmtContext
}

// readCtes takes the WITH queries of su; a WITH constant is the scope's.
// Any other WITH item is refused even when nothing names it: ClickHouse
// evaluates it, and may fail on it, which is not modelled here.
func (inst *evaluator) readCtes(su *grammar1.SelectUnionStmtContext) (err error) {
	c := su.Ctes()
	if c == nil {
		return nil
	}
	cc := c.(*grammar1.CtesContext)
	if cc.RECURSIVE() != nil {
		return refuse("WITH RECURSIVE")
	}
	if others := inst.scope.NotConstants(); len(others) > 0 {
		return refuse("the WITH item " + others[0] + ", which is not a constant")
	}
	for _, wi := range cc.AllWithItem() {
		nq, ok := wi.(*grammar1.WithItemNamedQueryContext)
		if !ok {
			if col, isCol := wi.(*grammar1.WithItemColumnsExprContext).ColumnsExpr().(*grammar1.ColumnsExprColumnContext); !isCol || !isAliased(col.ColumnExpr()) {
				return refuse("a WITH item without AS (" + wi.GetText() + ")")
			}
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
		name := nanopass.DecodeIdentifier(named.Identifier().GetText())
		for _, prev := range inst.ctes {
			if prev.name == name {
				// ClickHouse: CTE with name … already exists.
				return eb.Build().Str("with", name).Errorf("trivialsql: WITH names a query twice")
			}
		}
		if _, bound := inst.tables[name]; bound {
			// A bound table stands before the statement as a WITH query,
			// so this is the same name twice.
			return eb.Build().Str("with", name).Errorf("trivialsql: WITH names a query as a bound table is named")
		}
		if inst.scope.IsWithName(name) {
			return refuse("WITH naming a query and an expression alike (" + name + ")")
		}
		inst.ctes = append(inst.ctes, namedSelect{name: name, sel: isel})
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

// projItem is one item of a SELECT list: *, a column of the source under
// its name or an alias, or a named constant.
type projItem struct {
	star bool
	// column is the source column a bare identifier names, when isColumn.
	column   string
	isColumn bool
	name     string
	aliased  bool
	value    keelsonsql.Constant
}

// evalSelect answers sel, which may name the first visible WITH queries:
// its source read whole — a keelson() call, values(), a WITH query, a bound
// table, or one row when there is no FROM — then its SELECT list of *,
// columns and constants. A clause that filters, groups, orders or joins is
// refused.
func (inst *evaluator) evalSelect(sel *grammar1.SelectStmtContext, visible int) (batch arrow.RecordBatch, err error) {
	switch {
	case len(sel.AllArrayJoinClause()) > 0:
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
	return project(src, items, sel.FromClause() != nil, visible == len(inst.ctes))
}

// projection reads the SELECT list: *, bare columns of the source and
// constants, a column named by its alias or its own name, a constant by its
// alias or by ClickHouse's default name for it.
//
// ClickHouse resolves a name in the list to an alias of the list before a
// column of the source, through chains and around cycles (`a AS b, b AS a`
// answers a as a and b as b). That resolution is not modelled: a column
// whose name another item has for its alias is refused.
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
			if keelsonsql.IsBareAlias(col.ColumnExpr()) {
				return nil, refuse("an alias without AS (" + col.GetText() + ")")
			}
			expr, name, aliased := keelsonsql.Aliased(col.ColumnExpr())
			if aliased && inst.scope.IsWithName(name) {
				// ClickHouse: MULTIPLE_EXPRESSIONS_FOR_ALIAS, unless the two
				// expressions are the same.
				return nil, refuse("an alias a WITH expression also has (" + name + ")")
			}
			if column, isColumn := bareColumn(expr); isColumn && sel.FromClause() != nil && !inst.scope.IsWithName(column) {
				if !aliased {
					name = column
				}
				items = append(items, projItem{column: column, isColumn: true, name: name, aliased: aliased})
				continue
			}
			if !aliased && isParam(expr) {
				return nil, refuse("a parameter without AS, which ClickHouse names _CAST(…)")
			}
			var v keelsonsql.Constant
			v, err = keelsonsql.EvalConstant(expr, inst.scope)
			if err != nil {
				var nc *keelsonsql.NotConstantError
				if errors.As(err, &nc) {
					return nil, refuse("an expression in the SELECT list (" + nc.Found + ")")
				}
				return nil, err
			}
			if v.Null {
				return nil, refuse("NULL as a column, whose type is Nullable(Nothing)")
			}
			if !aliased {
				name = defaultName(v)
			}
			items = append(items, projItem{name: name, aliased: aliased, value: v})
		default:
			return nil, refuse("a subquery in the SELECT list")
		}
	}
	for i, it := range items {
		if !it.isColumn {
			continue
		}
		for j, other := range items {
			if j != i && other.aliased && other.name == it.column {
				return nil, refuse("the name " + it.column + ", which is a column and an alias in the SELECT list")
			}
		}
	}
	return
}

// bareColumn is the name of e when e is one unqualified identifier that is
// not true or false: `a`, not `t.a`, `(a)` or `a.b`. A quoted true or false
// is a name, as in ClickHouse.
func bareColumn(e grammar1.IColumnExprContext) (name string, ok bool) {
	id, isIdent := e.(*grammar1.ColumnExprIdentifierContext)
	if !isIdent {
		return "", false
	}
	ci := id.ColumnIdentifier().(*grammar1.ColumnIdentifierContext)
	ni := ci.NestedIdentifier().(*grammar1.NestedIdentifierContext)
	if ci.ColumnQualifier() != nil || len(ni.AllIdentifier()) != 1 {
		return "", false
	}
	text := ni.GetText()
	if strings.EqualFold(text, "true") || strings.EqualFold(text, "false") {
		return "", false
	}
	return nanopass.DecodeIdentifier(text), true
}

func isAliased(e grammar1.IColumnExprContext) bool {
	_, _, aliased := keelsonsql.Aliased(e)
	return aliased
}

// isParam reports a {slot:Type} parameter, parenthesised or not.
func isParam(e grammar1.IColumnExprContext) bool {
	for {
		switch v := e.(type) {
		case *grammar1.ColumnExprParensContext:
			e = v.ColumnExpr()
		case *grammar1.ColumnExprParamSlotContext:
			return true
		default:
			return false
		}
	}
}

// constantErr is a refusal for an expression that is not a constant, and
// err itself otherwise.
func constantErr(err error) error {
	var nc *keelsonsql.NotConstantError
	if errors.As(err, &nc) {
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
		buf.WriteString(keelsonsql.FloatFieldText(v.Float))
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
				return nil, constantErr(rErr)
			}
			batch, err = introspect.SnapshotCall(p, introspect.AllColumns(), raw)
			if err != nil {
				return nil, err
			}
			return inst.unshadowed(batch, "keelson() column")
		case f.Identifier() != nil && strings.EqualFold(f.Identifier().GetText(), "values"):
			return inst.values(f)
		}
		return nil, refuse("a table function other than keelson() and values()")
	case *grammar1.TableExprIdentifierContext:
		ti := t.TableIdentifier().(*grammar1.TableIdentifierContext)
		if ti.DatabaseIdentifier() == nil {
			name := nanopass.DecodeIdentifier(ti.GetText())
			for i := visible - 1; i >= 0; i-- {
				if inst.ctes[i].name != name {
					continue
				}
				batch, err = inst.evalSelect(inst.ctes[i].sel, i)
				if err != nil {
					return nil, err
				}
				return inst.unshadowed(batch, "WITH query's column")
			}
			if bound, found := inst.tables[name]; found {
				if !slices.Contains(inst.reads, name) {
					inst.reads = append(inst.reads, name)
				}
				bound.Retain()
				return inst.unshadowed(bound, "bound table's column")
			}
			for j := visible + 1; j < len(inst.ctes); j++ {
				if inst.ctes[j].name == name {
					// ClickHouse answers a WITH query that reads one written
					// after it; here a query is read only once it is written.
					return nil, refuse("a WITH query read before it is written (" + name + ")")
				}
			}
			if inst.tables != nil {
				if visible < len(inst.ctes) {
					// ClickHouse resolves a WITH query only where the
					// statement reads it, and answers the statement
					// otherwise. Every WITH query is answered here, so this
					// one is a refusal, as a column it lacks is (project).
					return nil, refuse("a WITH query reading a table that is not bound (" + name + ")")
				}
				// ClickHouse: UNKNOWN_TABLE.
				return nil, eb.Build().Str("table", name).Errorf("trivialsql: no table of that name is bound")
			}
		}
		if inst.tables != nil {
			return nil, refuse("a table of a database (" + ti.GetText() + "), where only the bound tables can be read")
		}
		return nil, refuse("a table other than keelson() and values() (" + ti.GetText() + ")")
	}
	return nil, refuse("a subquery")
}

// unshadowed refuses a table whose column a WITH expression's alias
// shadows: ClickHouse then qualifies the column under * (q.a, and a
// keelson() call's column under its TEMPORARY table's name), which is not
// modelled. values() keeps the bare name, as clickhouse-local does.
func (inst *evaluator) unshadowed(batch arrow.RecordBatch, what string) (out arrow.RecordBatch, err error) {
	for _, f := range batch.Schema().Fields() {
		if inst.scope.IsWithName(f.Name) {
			batch.Release()
			return nil, refuse("a " + what + " " + f.Name + ", which a WITH expression's alias shadows")
		}
	}
	return batch, nil
}

// project builds the SELECT list over src: * is src's columns, a column is
// src's array under the item's name, a constant a column of its value on
// every row. A name given twice, or a WITH constant named like a column of
// src, is refused: which one ClickHouse would pick is not modelled here. A
// column src does not have is an error in the outermost SELECT, as in
// ClickHouse, and refused in a WITH query, where ClickHouse may find the
// name among the outer SELECT's aliases.
func project(src arrow.RecordBatch, items []projItem, hasFrom bool, outermost bool) (batch arrow.RecordBatch, err error) {
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
		if it.isColumn {
			idx := src.Schema().FieldIndices(it.column)
			switch {
			case len(idx) == 0 && outermost:
				// ClickHouse: UNKNOWN_IDENTIFIER.
				return nil, eb.Build().Str("column", it.column).Errorf("trivialsql: the source has no such column")
			case len(idx) != 1:
				return nil, refuse("the column " + it.column + ", which the source does not have once")
			}
			f := src.Schema().Field(idx[0])
			f.Name = it.name
			c := src.Column(idx[0])
			c.Retain()
			if err = add(f, c); err != nil {
				return nil, err
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
