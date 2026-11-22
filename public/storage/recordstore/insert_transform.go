package recordstore

import (
	"context"
	"errors"
	"iter"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// InsertTransformI rewrites one insert into another table's schema (ADR-0402
// §SD1): a deployment that keeps a table's rows in a table of its own
// receives the batches a store built for the table, and returns the table to
// insert into and the batches in that table's schema.
//
// TransformInsert must be a pure function of its input: the same batches give
// the same target and the same output, so a re-sent batch is the same block
// under the same deduplication token. It must not retain or release the
// batches it receives. The batches it returns are released by the caller
// once the insert returned, so one it passes through unchanged is Retained
// first. One insert maps to one insert into one table: there is no fan-out.
type InsertTransformI interface {
	TransformInsert(table string, records []arrow.RecordBatch) (target string, out []arrow.RecordBatch, err error)
}

// ErrTransformedTable is what a statement that would change, empty or
// create a table by its name returns when an insert transform is registered
// for that table (ADR-0402 §SD5): the rows are stored in a deployment's own
// table, which the statement would not reach, so it is refused rather than
// run against a relation that holds none of them. Match with errors.Is.
var ErrTransformedTable = errors.New("the table's inserts are transformed into a deployment's own table, which this statement does not reach")

// InPlaceStatement is one statement that changes stored rows of a table by
// the table's name: the kind of statement an insert transform leaves
// pointing at a relation that holds none of the rows (ADR-0402 §SD5).
type InPlaceStatement struct {
	// Op names what the statement does, for a transform to dispatch on. It
	// is a constant the statement's owner exports.
	Op string
	// SQL is the statement as its owner composed it against the table. A
	// transform reads or rewrites it; it is not what runs.
	SQL string
	// Args are the values SQL was composed from, by the names Op's owner
	// documents, so that a transform need not parse them back out.
	Args map[string]string
}

// InPlaceTransformI is what an [InsertTransformI] also implements when its
// deployment can say what an in-place statement is in its own storage.
// TransformInPlace returns the statement to run instead of stmt.SQL, against
// the deployment's table. A transform that has no answer for stmt.Op
// returns an error wrapping [ErrTransformedTable], and the statement is
// refused as it is for a transform that does not implement this at all.
type InPlaceTransformI interface {
	TransformInPlace(table string, stmt InPlaceStatement) (sql string, err error)
}

// InPlaceStatementFor is the statement to run for stmt against table: stmt.SQL
// when no insert transform is registered for table; what the registered
// transform makes of it when that transform is an [InPlaceTransformI]; and
// otherwise an error wrapping [ErrTransformedTable], since the statement
// would reach a relation that holds none of the rows.
func InPlaceStatementFor(table string, stmt InPlaceStatement) (sql string, err error) {
	t, ok := RegisteredInsertTransform(table)
	if !ok {
		return stmt.SQL, nil
	}
	ip, ok := t.(InPlaceTransformI)
	if !ok {
		err = eb.Build().Str("table", table).Str("op", stmt.Op).Errorf("recordstore: %w", ErrTransformedTable)
		return
	}
	sql, err = ip.TransformInPlace(table, stmt)
	if err != nil {
		err = eb.Build().Str("table", table).Str("op", stmt.Op).Errorf("recordstore: in-place statement: %w", err)
		return
	}
	if strings.TrimSpace(sql) == "" {
		err = eb.Build().Str("table", table).Str("op", stmt.Op).Errorf("recordstore: the transform returned no statement: %w", ErrTransformedTable)
	}
	return
}

// InsertTransformFunc adapts a function as an InsertTransformI.
type InsertTransformFunc func(table string, records []arrow.RecordBatch) (target string, out []arrow.RecordBatch, err error)

var _ InsertTransformI = InsertTransformFunc(nil)

func (inst InsertTransformFunc) TransformInsert(table string, records []arrow.RecordBatch) (target string, out []arrow.RecordBatch, err error) {
	return inst(table, records)
}

// ErrUnqualifiedTransformedTable is what an insert returns when it names a
// table without its database while an insert transform is registered for a
// table of that name in some database (ADR-0402 §SD2). Which database a bare
// name resolves in is the server session's, which the registry does not
// know: the insert might be one the transform must rewrite, so it is refused
// rather than sent untransformed. Match with errors.Is.
var ErrUnqualifiedTransformedTable = errors.New("the insert names a table without its database, and an insert transform is registered for a table of that name; name the database")

// bareIdentRe is one unquoted identifier.
var bareIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// insertTransforms is the process-wide registry: canonical table reference
// ([CanonicalTableRef]) → transform.
var insertTransforms struct {
	mu sync.RWMutex
	m  map[string]*registeredTransform
}

// registeredTransform is one registration; its address tells it from a later
// one for the same table, which a func-typed transform could not.
type registeredTransform struct{ t InsertTransformI }

// CanonicalTableRef is the one spelling a table reference is registered and
// looked up under (ADR-0402 §SD2): each part without the backticks or double
// quotes ClickHouse allows around an identifier, the parts joined by a dot.
// So boxer.facts, `boxer`.`facts` and "boxer"."facts" are one table. Case is
// kept — ClickHouse identifiers are case-sensitive.
//
// qualified reports whether the reference names its database. ok is false
// for text that is not one or two identifiers: an empty part, an unclosed
// quote, a quoted part holding a quote or a backslash, a third part. Such a
// reference matches no registration, and the server judges it.
func CanonicalTableRef(ref string) (canon string, qualified bool, ok bool) {
	rest := strings.TrimSpace(ref)
	var parts [2]string
	n := 0
	for {
		if n == len(parts) || rest == "" {
			return "", false, false
		}
		var part string
		if q := rest[0]; q == '`' || q == '"' {
			end := strings.IndexByte(rest[1:], q)
			if end < 0 {
				return "", false, false
			}
			part, rest = rest[1:1+end], rest[2+end:]
			if strings.ContainsAny(part, "`\"\\") {
				return "", false, false
			}
		} else {
			end := strings.IndexByte(rest, '.')
			if end < 0 {
				end = len(rest)
			}
			part, rest = rest[:end], rest[end:]
			if !bareIdentRe.MatchString(part) {
				return "", false, false
			}
		}
		if part == "" {
			return "", false, false
		}
		parts[n] = part
		n++
		if rest == "" {
			break
		}
		if rest[0] != '.' {
			return "", false, false
		}
		rest = rest[1:]
	}
	if n == 1 {
		return parts[0], false, true
	}
	return parts[0] + "." + parts[1], true, true
}

// RegisterInsertTransform registers t for every insert into table, for the
// whole process (ADR-0402 §SD2). A binary registers before it builds any
// store: the executors boxer constructs consult the registry on each insert,
// and a writer that ran before the registration wrote the untransformed
// shape. table names its database — "boxer.facts" for the facts table — and
// an insert matches it under any spelling [CanonicalTableRef] makes equal.
//
// It panics on a malformed or unqualified reference, a nil t, or a second
// registration for the same table: a registration is wiring, and two
// transforms for one table would make which one ran depend on the order of
// init. unregister removes the registration; a binary does not call it, a
// test does.
func RegisterInsertTransform(table string, t InsertTransformI) (unregister func()) {
	if err := CheckTableRef(table); err != nil {
		panic("recordstore.RegisterInsertTransform: " + err.Error())
	}
	key, qualified, _ := CanonicalTableRef(table)
	if !qualified {
		panic("recordstore.RegisterInsertTransform: " + table + " does not name its database; a bare name resolves in whatever database a session starts in")
	}
	if t == nil {
		panic("recordstore.RegisterInsertTransform: nil transform")
	}
	insertTransforms.mu.Lock()
	defer insertTransforms.mu.Unlock()
	if _, dup := insertTransforms.m[key]; dup {
		panic("recordstore.RegisterInsertTransform: a transform is already registered for " + key)
	}
	if insertTransforms.m == nil {
		insertTransforms.m = make(map[string]*registeredTransform, 1)
	}
	reg := &registeredTransform{t: t}
	insertTransforms.m[key] = reg
	unregister = func() {
		insertTransforms.mu.Lock()
		defer insertTransforms.mu.Unlock()
		if insertTransforms.m[key] == reg {
			delete(insertTransforms.m, key)
		}
	}
	return
}

// RegisteredInsertTransform is the transform registered for table, if any,
// whatever spelling of the reference table is ([CanonicalTableRef]). A
// reference that does not name its database has none: see
// [ErrUnqualifiedTransformedTable] for what an insert naming one gets.
func RegisteredInsertTransform(table string) (t InsertTransformI, ok bool) {
	t, _ = lookupRegistered(table)
	return t, t != nil
}

// RegisteredInsertTransformTables lists the tables a transform is registered
// for, in their canonical spelling, sorted.
func RegisteredInsertTransformTables() (tables []string) {
	insertTransforms.mu.RLock()
	defer insertTransforms.mu.RUnlock()
	tables = make([]string, 0, len(insertTransforms.m))
	for k := range insertTransforms.m {
		tables = append(tables, k)
	}
	slices.Sort(tables)
	return
}

// lookupRegistered resolves an insert's table against the registry: the
// transform, nil when the table has none, or an error for a bare name that
// a registration's table part matches.
func lookupRegistered(table string) (t InsertTransformI, err error) {
	insertTransforms.mu.RLock()
	defer insertTransforms.mu.RUnlock()
	if len(insertTransforms.m) == 0 {
		return
	}
	canon, qualified, ok := CanonicalTableRef(table)
	if !ok {
		return
	}
	if qualified {
		if reg := insertTransforms.m[canon]; reg != nil {
			t = reg.t
		}
		return
	}
	for key := range insertTransforms.m {
		if _, name, _ := strings.Cut(key, "."); name == canon {
			err = eb.Build().Str("table", table).Str("registered", key).Errorf("recordstore: %w", ErrUnqualifiedTransformedTable)
			return
		}
	}
	return
}

// lookupOne is lookupRegistered for the one table of [TransformExecutor].
func lookupOne(key string, t InsertTransformI) func(table string) (InsertTransformI, error) {
	_, name, _ := strings.Cut(key, ".")
	return func(table string) (InsertTransformI, error) {
		canon, qualified, ok := CanonicalTableRef(table)
		switch {
		case !ok:
			return nil, nil
		case qualified && canon == key:
			return t, nil
		case !qualified && canon == name:
			return nil, eb.Build().Str("table", table).Str("registered", key).Errorf("recordstore: %w", ErrUnqualifiedTransformedTable)
		}
		return nil, nil
	}
}

// transformCtxKey carries, on the context of one insert, that a transform
// already rewrote it and where an observer wants the rewrite reported.
type transformCtxKey struct{}

type transformCtx struct {
	// applied: the insert under this context is a transform's output.
	applied bool
	// route, when not nil, receives the table a transform sent the insert to.
	route *insertRoute
}

// insertRoute is what [ObservingExecutor] learns about one insert from the
// executor below it that applied a transform.
type insertRoute struct{ target string }

func transformCtxFrom(ctx context.Context) (tc transformCtx) {
	tc, _ = ctx.Value(transformCtxKey{}).(transformCtx)
	return
}

// WithTransformApplied marks ctx as carrying an insert a transform already
// rewrote, so that no executor below applies the registry to it again. A
// transform applies once per insert: the wrapping executor sets this before
// it calls its inner one, and a caller that applies a transform by hand
// ([TransformInsert]) and sends the result through an executor that
// consults the registry does the same. Without it, a transform whose target
// is itself a registered table would be rewritten twice.
func WithTransformApplied(ctx context.Context) context.Context {
	tc := transformCtxFrom(ctx)
	if tc.applied {
		return ctx
	}
	tc.applied = true
	return context.WithValue(ctx, transformCtxKey{}, tc)
}

// TransformApplied reports whether ctx carries [WithTransformApplied].
func TransformApplied(ctx context.Context) bool {
	return transformCtxFrom(ctx).applied
}

// withInsertRoute asks the executors below to report where a transform sent
// the insert made under the returned context.
func withInsertRoute(ctx context.Context) (out context.Context, route *insertRoute) {
	tc := transformCtxFrom(ctx)
	route = &insertRoute{}
	tc.route = route
	out = context.WithValue(ctx, transformCtxKey{}, tc)
	return
}

// reportRoute tells the observer of ctx's insert, if there is one, that a
// transform sent it to target.
func reportRoute(ctx context.Context, target string) {
	if r := transformCtxFrom(ctx).route; r != nil {
		r.target = target
	}
}

// TransformInsert applies t to one insert into table. release must be
// called once the transformed insert returned; it releases the batches t
// returned. A target that is not a table reference, or an error from t, fails
// the insert before anything is sent. An empty records is passed through
// untransformed: it sends nothing.
func TransformInsert(t InsertTransformI, table string, records []arrow.RecordBatch) (target string, out []arrow.RecordBatch, release func(), err error) {
	release = func() {}
	if t == nil || len(records) == 0 {
		return table, records, release, nil
	}
	target, out, err = t.TransformInsert(table, records)
	releaseOut := func() {
		for _, r := range out {
			if r != nil {
				r.Release()
			}
		}
	}
	if err != nil {
		releaseOut()
		err = eb.Build().Str("table", table).Errorf("recordstore: insert transform: %w", err)
		return
	}
	if cerr := CheckTableRef(target); cerr != nil {
		releaseOut()
		err = eb.Build().Str("table", table).Str("target", target).Errorf("recordstore: insert transform returned no usable target: %w", cerr)
		return
	}
	release = releaseOut
	return
}

// ApplyRegisteredTransform is [TransformInsert] with the transform registered
// for table, and a pass-through when there is none or when ctx says a
// transform already rewrote this insert ([WithTransformApplied]). It is what
// an executor that consults the registry calls on each insert, with the
// insert's context. A bare table name that a registration's table part
// matches fails with [ErrUnqualifiedTransformedTable] before anything is
// sent. The target is reported to an observing executor above.
func ApplyRegisteredTransform(ctx context.Context, table string, records []arrow.RecordBatch) (target string, out []arrow.RecordBatch, release func(), err error) {
	release = func() {}
	if TransformApplied(ctx) {
		return table, records, release, nil
	}
	t, err := lookupRegistered(table)
	if err != nil || t == nil {
		return table, records, release, err
	}
	target, out, release, err = TransformInsert(t, table, records)
	if err == nil {
		reportRoute(ctx, target)
	}
	return
}

// TransformingExecutor is the decorator [TransformExecutor] and
// [RegisteredTransforms] return: inserts into the table it transforms go
// through the transform, and every other call — statements, queries, inserts
// into other tables — reaches the inner executor untouched. The settings of
// an insert, its deduplication token among them, pass through to the
// transformed insert. The inner insert runs under [WithTransformApplied], so
// an inner executor that consults the registry itself does not rewrite the
// insert a second time. It is safe for concurrent use when its inner
// executor and its transform are.
type TransformingExecutor struct {
	inner  ExecutorI
	lookup func(table string) (InsertTransformI, error)
}

var _ ExecutorI = (*TransformingExecutor)(nil)
var _ InsertSettingsI = (*TransformingExecutor)(nil)

// TransformExecutor decorates inner so that inserts into table go through t.
// table names its database and is matched as the registry matches
// ([CanonicalTableRef]); it panics on a reference that is not one.
func TransformExecutor(inner ExecutorI, table string, t InsertTransformI) (inst *TransformingExecutor) {
	if inner == nil || t == nil {
		panic("recordstore.TransformExecutor: nil inner executor or transform")
	}
	key, qualified, ok := CanonicalTableRef(table)
	if !ok || !qualified {
		panic("recordstore.TransformExecutor: " + table + " is not a database-qualified table reference")
	}
	return &TransformingExecutor{inner: inner, lookup: lookupOne(key, t)}
}

// RegisteredTransforms decorates inner so that each insert goes through the
// transform registered for its table at the time of the insert
// ([RegisterInsertTransform]). It is for an executor that does not consult
// the registry itself.
func RegisteredTransforms(inner ExecutorI) (inst *TransformingExecutor) {
	if inner == nil {
		panic("recordstore.RegisteredTransforms: nil inner executor")
	}
	return &TransformingExecutor{inner: inner, lookup: lookupRegistered}
}

func (inst *TransformingExecutor) Exec(ctx context.Context, sql string) error {
	return inst.inner.Exec(ctx, sql)
}

func (inst *TransformingExecutor) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	return inst.inner.QueryArrow(ctx, sql)
}

func (inst *TransformingExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	_, err = inst.insert(ctx, table, records, InsertSettings{})
	return
}

// InsertArrowWith transforms as InsertArrow does and forwards the settings;
// applied is the inner executor's answer.
func (inst *TransformingExecutor) InsertArrowWith(ctx context.Context, table string, records []arrow.RecordBatch, settings InsertSettings) (applied bool, err error) {
	return inst.insert(ctx, table, records, settings)
}

func (inst *TransformingExecutor) insert(ctx context.Context, table string, records []arrow.RecordBatch, settings InsertSettings) (applied bool, err error) {
	if TransformApplied(ctx) {
		return InsertArrowWith(ctx, inst.inner, table, records, settings)
	}
	t, err := inst.lookup(table)
	if err != nil {
		return
	}
	if t == nil {
		return InsertArrowWith(ctx, inst.inner, table, records, settings)
	}
	target, out, release, err := TransformInsert(t, table, records)
	if err != nil {
		return
	}
	defer release()
	reportRoute(ctx, target)
	applied, err = InsertArrowWith(WithTransformApplied(ctx), inst.inner, target, out, settings)
	if err != nil {
		err = eh.Errorf("recordstore: transformed insert: %w", err)
	}
	return
}
