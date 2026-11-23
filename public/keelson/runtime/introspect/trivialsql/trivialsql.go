// Package trivialsql answers the SQL that needs no engine: a keelson table
// read whole, with or without named arguments, and inline constants
// (ADR-0290 §SD3). It is what a host without ClickHouse — a browser tab, a
// small headless binary, a test — runs behind the same ClickHouse HTTP
// dialect a server would speak.
//
// It accepts, by shape and nothing else:
//
//	[SET param_<name> = <value>; …]
//	[WITH <constant> AS c | q AS (SELECT <list> FROM <source>) [, …]]
//	SELECT <list> [FROM <source> [AS x]]
//	[LIMIT n [OFFSET m]] [SETTINGS …]
//	[FORMAT ArrowStream | TabSeparated[WithNames] | CSV[WithNames] | JSONEachRow]
//
// where a source is keelson(…), values(…) over constants, an earlier WITH
// query or a table the caller bound by name ([ReadTables]); a list is *,
// bare columns of the source and constants, each with an optional alias; and
// a constant is a literal, true, false, a {slot:Type} parameter or a WITH
// constant (keelsonsql.EvalConstant). It evaluates nothing per row: it
// resolves a call's arguments, snapshots the provider, hands on the
// source's columns — some of them, under other names, sharing their buffers
// (ADR-0290, update of 2026-11-13) — types the constants as ClickHouse types
// them and encodes the record. A setting — a SET, a
// SETTINGS clause or a request's query-string key — is accepted only when
// it changes neither the rows nor their encoding (ignorableSetting). Every
// other statement is refused with [ErrNeedsClickHouse], naming what the
// shape did not allow; that is a statement for a real server, not a missing
// feature here.
package trivialsql

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrNeedsClickHouse is the refusal of a statement outside the shape.
var ErrNeedsClickHouse = errors.New("trivialsql: this endpoint answers only SELECT * FROM keelson(…); the statement needs ClickHouse")

// Run answers sql against reg, binding `{slot:Type}` placeholders from
// params by bare name (ADR-0133 §SD2), and returns the result encoded in the
// statement's FORMAT, TabSeparated when it names none.
func Run(ctx context.Context, reg *introspect.Registry, sql string, params map[string]string) (body []byte, err error) {
	body, _, err = answer(ctx, reg, sql, params, nil)
	return
}

// Read is Run without the encoding: the record the statement reads, which
// the caller releases, and the format it asked for.
func Read(ctx context.Context, reg *introspect.Registry, sql string, params map[string]string) (batch arrow.RecordBatch, format string, err error) {
	batch, format, _, err = read(ctx, reg, sql, params, nil, nil)
	return
}

// Named is a relation under the name a statement gave it.
type Named struct {
	Name  string
	Batch arrow.RecordBatch
}

// Answer is what [ReadTables] returns; the caller releases it.
type Answer struct {
	// Main is the record the statement reads.
	Main arrow.RecordBatch
	// With is every WITH query of the statement, in order, read whole,
	// whether or not the statement's own SELECT names it.
	With []Named
	// Tables names the bound tables the statement read, in the order it
	// first read them.
	Tables []string
}

// Release releases the answer's records.
func (inst *Answer) Release() {
	if inst == nil {
		return
	}
	if inst.Main != nil {
		inst.Main.Release()
		inst.Main = nil
	}
	for i := range inst.With {
		inst.With[i].Batch.Release()
	}
	inst.With = nil
}

// ReadTables is Read over tables the caller binds by name. A statement
// reads a bound table as it reads an earlier WITH query, `FROM name`, and
// is answered as ClickHouse would answer it with each table before it as a
// WITH query of the same rows. So a WITH query may not take a bound table's
// name. The tables are borrowed: what the answer holds of them it retains.
//
// It is the evaluator of a statement whose data is already at hand — a
// stored result, say — and it returns the statement's WITH queries beside
// its own relation, for a caller that reads more than one relation of a
// statement.
func ReadTables(ctx context.Context, reg *introspect.Registry, sql string, params map[string]string, tables map[string]arrow.RecordBatch) (ans *Answer, err error) {
	if tables == nil {
		tables = map[string]arrow.RecordBatch{}
	}
	main, _, ev, err := read(ctx, reg, sql, params, nil, tables)
	if err != nil {
		return nil, err
	}
	ans = &Answer{Main: main}
	for i, cte := range ev.ctes {
		var batch arrow.RecordBatch
		batch, err = ev.evalSelect(ctx, cte.sel, i)
		if err != nil {
			ans.Release()
			return nil, err
		}
		ans.With = append(ans.With, Named{Name: cte.name, Batch: batch})
	}
	ans.Tables = ev.reads
	return ans, nil
}

// Runner serves the evaluator as an introspection endpoint's runner
// (introspecthttp.Config.Runner, ADR-0290 §SD4). It resolves keelson()
// itself, and it is handed the request's query-string settings: it honours
// default_format and refuses a setting that is not ignorable.
type Runner struct {
	Registry *introspect.Registry
}

// RunSQL answers sql as Run does.
func (inst Runner) RunSQL(ctx context.Context, sql string, params map[string]string) (body []byte, err error) {
	body, _, err = answer(ctx, inst.Registry, sql, params, nil)
	return
}

// RunSQLSettings answers sql as Run does, with the request's settings, and
// returns the format it encoded the body in.
func (inst Runner) RunSQLSettings(ctx context.Context, sql string, params map[string]string, settings map[string]string) (body []byte, format string, err error) {
	return answer(ctx, inst.Registry, sql, params, settings)
}

// ResolvesMacros marks Runner as resolving keelson() calls itself.
func (Runner) ResolvesMacros() {}

func answer(ctx context.Context, reg *introspect.Registry, sql string, params map[string]string, settings map[string]string) (body []byte, format string, err error) {
	if err = ctx.Err(); err != nil {
		return nil, "", err
	}
	batch, format, _, err := read(ctx, reg, sql, params, settings, nil)
	if err != nil {
		return nil, "", err
	}
	defer batch.Release()
	body, err = Encode(batch, format)
	return body, format, err
}

// defaultFormatSetting names the format of a statement without a FORMAT
// clause, as ClickHouse's HTTP interface reads it from the query string.
const defaultFormatSetting = "default_format"

// read answers sql. tables is nil for a statement that may name none.
func read(ctx context.Context, reg *introspect.Registry, sql string, params map[string]string, settings map[string]string, tables map[string]arrow.RecordBatch) (batch arrow.RecordBatch, format string, ev *evaluator, err error) {
	for name := range settings {
		if name != defaultFormatSetting && !ignorableSetting(name) {
			return nil, "", nil, refuse("the setting " + name)
		}
	}
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return nil, "", nil, eb.Build().Errorf("%w: it does not parse: %w", ErrNeedsClickHouse, err)
	}
	qs, ok := pr.Tree.(*grammar1.QueryStmtContext)
	if !ok || qs.Query() == nil {
		return nil, "", nil, refuse("not a SELECT")
	}
	format = FormatTabSeparated
	if df := settings[defaultFormatSetting]; df != "" {
		format = df
	}
	if qs.FORMAT() != nil && qs.IdentifierOrNull() != nil {
		format = qs.IdentifierOrNull().GetText()
	}
	canonical, known := canonicalFormat(format)
	if !known {
		return nil, "", nil, refuse("FORMAT " + format)
	}
	format = canonical
	q := qs.Query().(*grammar1.QueryContext)
	sets := q.AllSetStmt()
	for _, st := range sets {
		if err = checkSettings(st.SettingExprList(), "SET ", true); err != nil {
			return nil, "", nil, err
		}
	}
	// The scope binds the SET param_ prelude itself.
	scope, err := keelsonsql.NewConstScope(pr, params)
	if err != nil {
		return nil, "", nil, err
	}
	ev = &evaluator{reg: reg, scope: scope, tables: tables}
	su := q.SelectUnionStmt().(*grammar1.SelectUnionStmtContext)
	if err = ev.readCtes(ctx, su); err != nil {
		return nil, "", nil, err
	}
	sel, err := single(su)
	if err != nil {
		return nil, "", nil, err
	}
	if sc, isClause := sel.SettingsClause().(*grammar1.SettingsClauseContext); isClause && sc != nil {
		if err = checkSettings(sc.SettingExprList(), "SETTINGS ", false); err != nil {
			return nil, "", nil, err
		}
	}
	limit, offset, err := limits(sel)
	if err != nil {
		return nil, "", nil, err
	}
	batch, err = ev.evalSelect(ctx, sel, len(ev.ctes))
	if err != nil {
		return nil, "", nil, err
	}
	if offset > 0 || limit >= 0 {
		n := batch.NumRows()
		from := min(offset, n)
		to := n
		if limit >= 0 && limit < n-from {
			to = from + limit
		}
		sliced := batch.NewSlice(from, to)
		batch.Release()
		batch = sliced
	}
	return batch, format, ev, nil
}

// checkSettings refuses a setting in l that is not ignorable; a SET may also
// bind a query parameter.
func checkSettings(l grammar1.ISettingExprListContext, spelled string, paramsAllowed bool) (err error) {
	for _, se := range l.AllSettingExpr() {
		name := nanopass.DecodeIdentifier(se.Identifier().GetText())
		if paramsAllowed && strings.HasPrefix(name, "param_") {
			continue
		}
		if !ignorableSetting(name) {
			return refuse(spelled + name)
		}
	}
	return nil
}

// ignorableSetting reports whether a setting changes neither the rows a
// statement reads nor how they are encoded, so that answering without it is
// answering as ClickHouse would. They are what play and the chhttp tolerance
// list send (ADR-0133 §SD1), plus the resource limits a whole-table read
// cannot reach in a way that changes its result; user and password are the
// query string's credentials, which an in-process endpoint has no use for.
// Anything else — limit, offset, max_result_rows, an output_format_* — is
// refused, since ignoring it could hand back other rows or other bytes.
func ignorableSetting(name string) bool {
	switch name {
	case "log_comment", "readonly", "send_progress_in_http_headers", "wait_end_of_query",
		"replace_running_query", "use_query_cache", "enable_reads_from_query_cache", "enable_writes_to_query_cache",
		"max_threads", "max_execution_time", "max_memory_usage",
		"user", "password":
		return true
	}
	return false
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
