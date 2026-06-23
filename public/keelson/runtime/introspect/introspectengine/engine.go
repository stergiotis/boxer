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
	"io"
	"sort"

	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// DefaultPoolName is the chlocal pool the engine targets.
const DefaultPoolName = "introspect"

// Engine analyses and runs introspection queries.
type Engine struct {
	reg      *introspect.Registry
	bus      app.BusI
	poolName string
	log      zerolog.Logger
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
	return &Engine{reg: reg, bus: cfg.Bus, poolName: pool, log: log}, nil
}

// Query runs sql and returns the result body in the given ClickHouse
// FORMAT (e.g. "ArrowStream", "JSONEachRow", "PrettyCompact"; empty for
// the clickhouse-local default).
func (e *Engine) Query(ctx context.Context, sql, format string) (body []byte, contentType string, err error) {
	p := e.plan(sql)
	body, contentType, err = e.exec(ctx, sql, format, p.tables, p.proj)
	if err != nil && p.pruned {
		// Conservative fallback (ADR-0094 §SD4): a pruned column set may
		// have dropped a column the analyser missed. Retry once with all
		// columns before surfacing the error.
		e.log.Debug().Err(err).Str("sql", sql).Msg("introspectengine: pruned query failed; retrying with all columns")
		body, contentType, err = e.exec(ctx, sql, format, p.tables, allColumns(p.tables))
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
	for _, tr := range analysis.ExtractTables(pr) {
		if _, ok := e.reg.Lookup(tr.Table); ok {
			refd[tr.Table] = struct{}{}
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

// exec snapshots the referenced tables under proj and runs sql via the
// chlocal broker.
func (e *Engine) exec(ctx context.Context, sql, format string, tables []string, proj map[string]introspect.Projection) (body []byte, contentType string, err error) {
	var inputs map[string][]byte
	if len(tables) > 0 {
		inputs = make(map[string][]byte, len(tables))
		for _, t := range tables {
			prov, ok := e.reg.Lookup(t)
			if !ok {
				continue
			}
			pj, ok := proj[t]
			if !ok {
				pj = introspect.AllColumns()
			}
			b, snapErr := introspect.SnapshotFile(prov, pj)
			if snapErr != nil {
				return nil, "", eh.Errorf("introspectengine: snapshot %q: %w", t, snapErr)
			}
			inputs[t] = b
		}
	}

	rep, reqErr := chlocalbroker.ExecOnPool(ctx, e.bus, e.poolName, chlocalbroker.ExecRequest{
		SQL:         sql,
		Format:      format,
		InputTables: inputs,
	})
	if reqErr != nil {
		return nil, "", reqErr
	}
	defer func() { _ = rep.Close() }()
	if repErr := rep.Err(); repErr != nil {
		return nil, "", repErr
	}
	body, err = io.ReadAll(rep)
	if err != nil {
		return nil, "", eh.Errorf("introspectengine: read reply: %w", err)
	}
	contentType = rep.ContentType
	return
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
