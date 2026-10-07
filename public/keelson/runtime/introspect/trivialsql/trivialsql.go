// Package trivialsql answers the SQL that needs no engine: a keelson table
// read whole, with or without named arguments (ADR-0290 §SD3). It is what a
// host without ClickHouse — a browser tab, a small headless binary, a test —
// runs behind the same ClickHouse HTTP dialect a server would speak.
//
// It accepts, by shape and nothing else:
//
//	[SET param_<name> = <value>; …]
//	[WITH a AS (SELECT * FROM keelson(…)) [, …]]
//	SELECT * FROM keelson(…) | a [AS x]
//	[LIMIT n [OFFSET m]] [SETTINGS …]
//	[FORMAT ArrowStream | TabSeparated | JSONEachRow]
//
// and evaluates nothing: it resolves the call's arguments, snapshots the
// provider and encodes the record. Every other statement is refused with
// [ErrNeedsClickHouse], naming what the shape did not allow; that is a
// statement for a real server, not a missing feature here.
package trivialsql

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrNeedsClickHouse is the refusal of a statement outside the shape.
var ErrNeedsClickHouse = eh.Errorf("trivialsql: this endpoint answers only SELECT * FROM keelson(…); the statement needs ClickHouse")

// Formats are the output formats Run encodes; TabSeparated is ClickHouse's
// default when a statement names none.
const (
	FormatArrowStream  = "ArrowStream"
	FormatTabSeparated = "TabSeparated"
	FormatJSONEachRow  = "JSONEachRow"
)

// Run answers sql against reg, binding `{slot:Type}` placeholders from
// params by bare name (ADR-0133 §SD2), and returns the result encoded in the
// statement's FORMAT.
func Run(_ context.Context, reg *introspect.Registry, sql string, params map[string]string) (body []byte, err error) {
	batch, format, err := Read(reg, sql, params)
	if err != nil {
		return nil, err
	}
	defer batch.Release()
	return Encode(batch, format)
}

// Read is Run without the encoding: the record the statement reads, which
// the caller releases, and the format it asked for.
func Read(reg *introspect.Registry, sql string, params map[string]string) (batch arrow.RecordBatch, format string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return nil, "", eb.Build().Errorf("%w: it does not parse: %w", ErrNeedsClickHouse, err)
	}
	qs, ok := pr.Tree.(*grammar1.QueryStmtContext)
	if !ok || qs.Query() == nil {
		return nil, "", refuse("not a SELECT")
	}
	format = FormatTabSeparated
	if qs.FORMAT() != nil && qs.IdentifierOrNull() != nil {
		format = qs.IdentifierOrNull().GetText()
	}
	if !knownFormat(format) {
		return nil, "", refuse("FORMAT " + format)
	}
	q := qs.Query().(*grammar1.QueryContext)
	params, err = bindSets(q, params)
	if err != nil {
		return nil, "", err
	}
	su := q.SelectUnionStmt().(*grammar1.SelectUnionStmtContext)
	ctes, err := readCtes(su)
	if err != nil {
		return nil, "", err
	}
	sel, err := single(su)
	if err != nil {
		return nil, "", err
	}
	fn, err := source(sel, ctes)
	if err != nil {
		return nil, "", err
	}
	limit, offset, err := limits(sel)
	if err != nil {
		return nil, "", err
	}
	p, raw, err := keelsonsql.ResolveCall(reg, fn, params)
	if err != nil {
		return nil, "", err
	}
	batch, err = introspect.SnapshotCall(p, introspect.AllColumns(), raw)
	if err != nil {
		return nil, "", err
	}
	if offset > 0 || limit >= 0 {
		n := batch.NumRows()
		from := min(offset, n)
		to := n
		if limit >= 0 {
			to = min(from+limit, n)
		}
		sliced := batch.NewSlice(from, to)
		batch.Release()
		batch = sliced
	}
	return batch, format, nil
}

func refuse(what string) error { return &RefusalError{Found: what} }

// RefusalError is a refusal of a statement outside the shape, naming what the
// shape did not allow. It is [ErrNeedsClickHouse] to errors.Is.
type RefusalError struct {
	Found string
}

func (inst *RefusalError) Error() string {
	return ErrNeedsClickHouse.Error() + ": found " + inst.Found
}

func (inst *RefusalError) Is(target error) bool { return target == ErrNeedsClickHouse }

func knownFormat(f string) bool {
	switch f {
	case FormatArrowStream, FormatTabSeparated, FormatJSONEachRow:
		return true
	}
	return false
}

// bindSets folds `SET param_<name> = <value>;` preludes into params, as
// ClickHouse binds them; any other SET would change how a server runs the
// statement, which nothing here models, so it is refused.
func bindSets(q *grammar1.QueryContext, params map[string]string) (out map[string]string, err error) {
	sets := q.AllSetStmt()
	if len(sets) == 0 {
		return params, nil
	}
	out = make(map[string]string, len(params)+len(sets))
	for k, v := range params {
		out[k] = v
	}
	for _, st := range sets {
		for _, se := range st.SettingExprList().AllSettingExpr() {
			name := nanopass.DecodeIdentifier(se.Identifier().GetText())
			slot, isParam := strings.CutPrefix(name, "param_")
			if !isParam {
				return nil, refuse("SET " + name)
			}
			v := se.SettingValue().GetText()
			if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
				v = strings.NewReplacer(`\'`, `'`, `\\`, `\`, `''`, `'`).Replace(v[1 : len(v)-1])
			}
			out[slot] = v
		}
	}
	return
}

// readCtes maps each WITH alias to the keelson call it names. An item that
// is anything but `alias AS (SELECT * FROM keelson(…))` is refused.
func readCtes(su *grammar1.SelectUnionStmtContext) (ctes map[string]*grammar1.TableFunctionExprContext, err error) {
	c := su.Ctes()
	if c == nil {
		return nil, nil
	}
	cc := c.(*grammar1.CtesContext)
	if cc.RECURSIVE() != nil {
		return nil, refuse("WITH RECURSIVE")
	}
	ctes = make(map[string]*grammar1.TableFunctionExprContext, len(cc.AllWithItem()))
	for _, wi := range cc.AllWithItem() {
		nq, ok := wi.(*grammar1.WithItemNamedQueryContext)
		if !ok {
			return nil, refuse("a WITH expression")
		}
		named := nq.NamedQuery().(*grammar1.NamedQueryContext)
		if named.ColumnAliases() != nil {
			return nil, refuse("WITH column aliases")
		}
		inner := named.Query().(*grammar1.QueryContext)
		if len(inner.AllSetStmt()) > 0 {
			return nil, refuse("SET inside WITH")
		}
		isu := inner.SelectUnionStmt().(*grammar1.SelectUnionStmtContext)
		if isu.Ctes() != nil {
			return nil, refuse("a nested WITH")
		}
		isel, sErr := single(isu)
		if sErr != nil {
			return nil, sErr
		}
		fn, sErr := source(isel, nil)
		if sErr != nil {
			return nil, sErr
		}
		if isel.LimitClause() != nil || isel.SettingsClause() != nil {
			return nil, refuse("LIMIT or SETTINGS inside WITH")
		}
		ctes[nanopass.DecodeIdentifier(named.Identifier().GetText())] = fn
	}
	return
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

// source checks that sel is `SELECT * FROM <one table>` with no clause that
// filters, groups, orders or joins, and returns the keelson call the table
// is — directly, or through a WITH alias.
func source(sel *grammar1.SelectStmtContext, ctes map[string]*grammar1.TableFunctionExprContext) (fn *grammar1.TableFunctionExprContext, err error) {
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
	proj := sel.ProjectionClause().(*grammar1.ProjectionClauseContext)
	if proj.DISTINCT() != nil || proj.TopClause() != nil || proj.ProjectionExceptClause() != nil {
		return nil, refuse("DISTINCT, TOP or EXCEPT")
	}
	cols := proj.ColumnExprList().(*grammar1.ColumnExprListContext).AllColumnsExpr()
	if len(cols) != 1 {
		return nil, refuse("a column list")
	}
	star, ok := cols[0].(*grammar1.ColumnsExprAsteriskContext)
	if !ok || star.TableIdentifier() != nil {
		return nil, refuse("a column list")
	}
	if sel.FromClause() == nil {
		return nil, refuse("a SELECT without FROM")
	}
	je, ok := sel.FromClause().(*grammar1.FromClauseContext).JoinExpr().(*grammar1.JoinExprTableContext)
	if !ok {
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
		if !keelsonsql.IsCall(f) {
			return nil, refuse("a table function other than keelson()")
		}
		return f, nil
	case *grammar1.TableExprIdentifierContext:
		ti := t.TableIdentifier().(*grammar1.TableIdentifierContext)
		if ti.DatabaseIdentifier() == nil {
			if f, isCte := ctes[nanopass.DecodeIdentifier(ti.GetText())]; isCte {
				return f, nil
			}
		}
		return nil, refuse("a table other than keelson() (" + ti.GetText() + ")")
	default:
		return nil, refuse("a subquery")
	}
}

// limits reads `LIMIT n`, `LIMIT n OFFSET m` and `LIMIT m, n`; limit is -1
// without a LIMIT clause.
func limits(sel *grammar1.SelectStmtContext) (limit int64, offset int64, err error) {
	limit = -1
	lc, ok := sel.LimitClause().(*grammar1.LimitClauseContext)
	if !ok || lc == nil {
		return
	}
	if lc.TIES() != nil {
		return 0, 0, refuse("WITH TIES")
	}
	le := lc.LimitExpr().(*grammar1.LimitExprContext)
	vals := le.AllColumnExpr()
	nums := make([]int64, len(vals))
	for i, v := range vals {
		lit, isLit := v.(*grammar1.ColumnExprLiteralContext)
		if !isLit {
			return 0, 0, refuse("a LIMIT that is not a number")
		}
		n, pErr := strconv.ParseInt(lit.GetText(), 10, 64)
		if pErr != nil || n < 0 {
			return 0, 0, refuse("a LIMIT that is not a number")
		}
		nums[i] = n
	}
	switch {
	case len(nums) == 1:
		limit = nums[0]
	case le.COMMA() != nil:
		offset, limit = nums[0], nums[1]
	default:
		limit, offset = nums[0], nums[1]
	}
	return
}

// Encode writes batch in format.
func Encode(batch arrow.RecordBatch, format string) (body []byte, err error) {
	switch format {
	case FormatArrowStream:
		return introspect.EncodeStream(batch)
	case FormatJSONEachRow:
		var buf bytes.Buffer
		if err = array.RecordToJSON(batch, &buf); err != nil {
			return nil, eh.Errorf("trivialsql: encode JSONEachRow: %w", err)
		}
		return buf.Bytes(), nil
	case FormatTabSeparated:
		return encodeTSV(batch), nil
	default:
		return nil, refuse("FORMAT " + format)
	}
}

// encodeTSV writes ClickHouse's TabSeparated: one line per row, NULL as \N,
// and tab, newline and backslash escaped.
func encodeTSV(batch arrow.RecordBatch) []byte {
	esc := strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`)
	var buf bytes.Buffer
	cols := batch.Columns()
	for r := 0; r < int(batch.NumRows()); r++ {
		for c, col := range cols {
			if c > 0 {
				buf.WriteByte('\t')
			}
			if col.IsNull(r) {
				buf.WriteString(`\N`)
				continue
			}
			buf.WriteString(esc.Replace(col.ValueStr(r)))
		}
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}
