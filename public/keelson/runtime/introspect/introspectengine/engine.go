// Package introspectengine runs SQL against the keelson introspection
// tables in-process (ADR-0094 §SD4). It analyses the query with nanopass
// to learn which tables and columns it references, snapshots only those
// (projected to the referenced columns), feeds the projected Arrow to
// the chlocal broker as TEMPORARY tables, and returns the result.
//
// Projection is best-effort and never a correctness dependency:
//   - any `*` projection forces all columns (a pruned `SELECT *` would
//     silently drop columns, which clickhouse-local cannot catch);
//   - a parse failure, an unrecognised table, or a join falls back to
//     all columns of the referenced (or all) tables;
//   - if a pruned query still errors, it is retried once with all
//     columns before the error is surfaced.
package introspectengine

import (
	"context"
	"sort"
	"sync/atomic"

	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine/chlocal"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultPoolName is the chlocal pool the engine targets.
const DefaultPoolName = "introspect"

// Engine analyses and runs introspection queries.
type Engine struct {
	reg *introspect.Registry
	// delivery is the chlocal broker in its ADR-0144 delivery role: this
	// engine consumes a result as a frame stream rather than as a byte
	// slice, so a reply that never arrived is distinguishable from one that
	// arrived empty (R9) without this file having to check for it.
	delivery *chlocal.Engine
	poolName string
	log      zerolog.Logger
	// sealedBaseURL is the HTTP table source a sealed dataset is read from
	// when this engine serves a statement naming one (see
	// [Engine.SetSealedBaseURL]); empty refuses such a statement.
	sealedBaseURL atomic.Pointer[string]
}

// Config parameterises an Engine.
type Config struct {
	// Registry of tables to expose; defaults to introspect.Default.
	Registry *introspect.Registry
	// Bus is the caller's bus client, holding a CapDirectionPub (or Both)
	// SubjectFilter for ch.local.exec.<PoolName>. Required.
	Bus app.BusI
	// PoolName is the chlocal pool; defaults to DefaultPoolName.
	PoolName string
}

// New returns an Engine. Bus is required.
func New(cfg Config, log zerolog.Logger) (e *Engine, err error) {
	if cfg.Bus == nil {
		return nil, eh.Errorf("introspectengine: bus is nil")
	}
	reg := cfg.Registry
	if reg == nil {
		reg = introspect.Default
	}
	pool := cfg.PoolName
	if pool == "" {
		pool = DefaultPoolName
	}
	delivery, err := chlocal.New(chlocal.Config{Bus: cfg.Bus, PoolName: pool})
	if err != nil {
		return nil, err
	}
	return &Engine{reg: reg, delivery: delivery, poolName: pool, log: log}, nil
}

// SetSealedBaseURL names the HTTP table source (its BaseURL) a sealed
// dataset is read from: a keelson('<handle>') naming one is rewritten to
// url() against it rather than snapshotted, which would hand back
// ciphertext (ADR-0145 §SD2). The host sets it once the source is
// listening, since the port is only known then; empty (the default) keeps
// the refusal.
func (e *Engine) SetSealedBaseURL(baseURL string) {
	if baseURL == "" {
		e.sealedBaseURL.Store(nil)
		return
	}
	e.sealedBaseURL.Store(&baseURL)
}

// Query runs sql and returns the result body in the given ClickHouse
// FORMAT (e.g. "ArrowStream", "JSONEachRow", "PrettyCompact"; empty for
// the clickhouse-local default).
func (e *Engine) Query(ctx context.Context, sql, format string) (body []byte, contentType string, err error) {
	return e.QueryParams(ctx, sql, format, nil)
}

// QueryParams is Query with `{name:Type}` placeholder bindings by bare
// name (ADR-0133 §SD2): the broker prepends one SET param_<name> per
// entry and folds the pairs into its cache key; typed substitution stays
// the engine's job.
func (e *Engine) QueryParams(ctx context.Context, sql, format string, params map[string]string) (body []byte, contentType string, err error) {
	// Expand keelson('x') table-function macros before analysis (ADR-0094
	// §SD4): an ordinary table to its bare TEMPORARY-table name —
	// keelson('env') and env are interchangeable — and a sealed dataset,
	// where a source is known, to url() against it. A call with named
	// arguments is resolved against params and becomes a TEMPORARY table of
	// its own (ADR-0290 §SD2). An unknown keelson table fails fast here.
	sealedBase := ""
	if base := e.sealedBaseURL.Load(); base != nil {
		sealedBase = *base
	}
	sql, argCalls, err := keelsonsql.ExpandWithArgs(e.reg, sealedBase, sql, params)
	if err != nil {
		return nil, "", err
	}
	p := e.plan(sql)
	body, contentType, err = e.exec(ctx, sql, format, params, p.tables, p.proj, argCalls)
	if err != nil && p.pruned {
		// Conservative fallback (ADR-0094 §SD4): a pruned column set may
		// have dropped a column the analyser missed. Retry once with all
		// columns before surfacing the error.
		e.log.Debug().Err(err).Str("sql", sql).Msg("introspectengine: pruned query failed; retrying with all columns")
		body, contentType, err = e.exec(ctx, sql, format, params, p.tables, allColumns(p.tables), argCalls)
	}
	return
}

// queryPlan is the analysed shape of a query: which registered tables to
// snapshot, the per-table column projection, and whether any table was
// pruned below all-columns.
type queryPlan struct {
	tables []string
	proj   map[string]introspect.Projection
	pruned bool
}

// referencedTables are the table names pr reads from: every table
// identifier, less a name that the scopes resolve only as a reference to a
// WITH query. A WITH query named like a keelson table shadows it, as in
// ClickHouse, and snapshotting the table would be wasted work at best and,
// for a table that needs arguments, a failure. Anything the scopes do not
// settle stays in, the safe superset.
func referencedTables(pr *nanopass.ParseResult) (tables []string) {
	cteOnly := make(map[string]bool)
	if scopes, err := nanopass.BuildScopes(pr, ""); err == nil {
		for _, sc := range nanopass.FlattenScopes(scopes) {
			for _, ts := range sc.Tables {
				switch {
				case ts.IsSubquery || ts.IsFunction:
				case ts.IsCTE:
					if _, seen := cteOnly[ts.Table]; !seen {
						cteOnly[ts.Table] = true
					}
				default:
					cteOnly[ts.Table] = false
				}
			}
		}
	}
	for _, tr := range analysis.ExtractTables(pr) {
		if !cteOnly[tr.Table] {
			tables = append(tables, tr.Table)
		}
	}
	return
}

// plan analyses sql best-effort. On any uncertainty it widens to a safe
// superset rather than risk dropping data.
func (e *Engine) plan(sql string) (p queryPlan) {
	p.proj = make(map[string]introspect.Projection)

	pr, parseErr := nanopass.Parse(sql)
	if parseErr != nil {
		// Cannot analyse → snapshot every registered table, all columns.
		p.tables = e.reg.Names()
		for _, t := range p.tables {
			p.proj[t] = introspect.AllColumns()
		}
		return
	}

	// Referenced tables ∩ registered providers.
	refd := make(map[string]struct{})
	for _, t := range referencedTables(pr) {
		if _, ok := e.reg.Lookup(t); ok {
			refd[t] = struct{}{}
		}
	}
	if len(refd) == 0 {
		// No introspection table referenced (e.g. SELECT 1, or a query
		// over url()/system tables). Snapshot nothing; the broker runs
		// the SQL as-is.
		return
	}
	for t := range refd {
		p.tables = append(p.tables, t)
	}
	sort.Strings(p.tables)

	// A `*` anywhere, or a join, defeats safe column pruning.
	if hasStar(pr) || len(p.tables) > 1 {
		for _, t := range p.tables {
			p.proj[t] = introspect.AllColumns()
		}
		return
	}

	// Single table, no star: attribute every extracted column to it.
	only := p.tables[0]
	var cols []string
	seen := make(map[string]struct{})
	for _, cr := range analysis.ExtractColumns(pr) {
		if cr.Column == "" {
			continue
		}
		if _, dup := seen[cr.Column]; dup {
			continue
		}
		seen[cr.Column] = struct{}{}
		cols = append(cols, cr.Column)
	}
	if len(cols) == 0 {
		// e.g. SELECT count(*) FROM env — no named columns. All columns
		// (cheap) keeps it correct.
		p.proj[only] = introspect.AllColumns()
		return
	}
	p.proj[only] = introspect.Columns(cols...)
	p.pruned = true
	return
}

// exec snapshots the referenced tables under proj, and each call with
// named arguments whole under its TEMPORARY name, and runs sql via the
// chlocal broker.
func (e *Engine) exec(ctx context.Context, sql, format string, params map[string]string, tables []string, proj map[string]introspect.Projection, argCalls []keelsonsql.ArgCall) (body []byte, contentType string, err error) {
	var inputs map[string][]byte
	for _, c := range argCalls {
		prov, ok := e.reg.Lookup(c.Table)
		if !ok {
			return nil, "", eb.Build().Str("table", c.Table).Errorf("introspectengine: the keelson table was unregistered during the run")
		}
		b, snapErr := introspect.SnapshotCallFile(prov, introspect.AllColumns(), c.Raw)
		if snapErr != nil {
			return nil, "", eb.Build().Str("table", c.Table).Errorf("introspectengine: snapshot failed: %w", snapErr)
		}
		if inputs == nil {
			inputs = make(map[string][]byte, len(tables)+len(argCalls))
		}
		inputs[c.Temp] = b
	}
	for _, t := range tables {
		prov, ok := e.reg.Lookup(t)
		if !ok {
			continue
		}
		// A sealed dataset is not snapshotted and is not bound here: it is
		// served by handle over the loopback plane, decrypted in-process at
		// /table (ADR-0145 §SD2 retired the second, pipe-based decrypt
		// path). A query that reaches this engine still naming one has not
		// been through the url() rewrite that resolves it, so refusing is
		// the honest answer — snapshotting it would hand back ciphertext.
		if _, isEnc := prov.(introspect.EncryptedDatasetI); isEnc {
			return nil, "", eb.Build().Str("table", t).
				Errorf("introspectengine: the table is a sealed dataset; query it through the introspection /query endpoint, which resolves it by handle")
		}
		pj, ok := proj[t]
		if !ok {
			pj = introspect.AllColumns()
		}
		b, snapErr := introspect.SnapshotFile(prov, pj)
		if snapErr != nil {
			return nil, "", eb.Build().Str("table", t).Errorf("introspectengine: snapshot failed: %w", snapErr)
		}
		if inputs == nil {
			inputs = make(map[string][]byte, len(tables))
		}
		inputs[t] = b
	}

	st, res, reqErr := e.delivery.Deliver(ctx, queryengine.Request{
		SQL:    sql,
		Format: format,
		Params: params,
		Inputs: inputs,
	})
	if reqErr != nil {
		return nil, "", reqErr
	}
	defer func() { _ = st.Close() }()

	// Collect reports ErrIncomplete when the stream ended without saying how
	// the run finished — the case a byte slice cannot express, and the one a
	// caller would otherwise render as a short answer.
	body, term, err := queryengine.Collect(st)
	if err != nil {
		return nil, "", err
	}
	if term.State == runstream.TerminalFailed {
		return nil, "", term.Err
	}
	contentType = res.ContentType
	return body, contentType, nil
}

// hasStar reports whether the query contains any `*` — a projection star
// (`SELECT *`, `table.*`) or an expression star (`count(*)`). Either one
// suppresses column pruning for safety.
func hasStar(pr *nanopass.ParseResult) bool {
	nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch ctx.(type) {
		case *grammar1.ColumnsExprAsteriskContext, *grammar1.ColumnExprAsteriskContext:
			return true
		default:
			return false
		}
	})
	return len(nodes) > 0
}

func allColumns(tables []string) (m map[string]introspect.Projection) {
	m = make(map[string]introspect.Projection, len(tables))
	for _, t := range tables {
		m[t] = introspect.AllColumns()
	}
	return
}
