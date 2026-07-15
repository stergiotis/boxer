package play

import (
	"context"
	"iter"
	"slices"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

const (
	// schemaProbeTimeout bounds one system.columns probe. It runs inline on the
	// execution path (first query per table), so it must not hang a Run.
	schemaProbeTimeout = 5 * time.Second
	// schemaCacheTTL / schemaCacheMaxTables size the physical-column-list cache
	// that fronts the probe. A table is re-probed at most this often.
	schemaCacheTTL       = 5 * time.Minute
	schemaCacheMaxTables = 256
)

// chSchemaProvider implements passes.SchemaProviderI by asking the live
// ClickHouse endpoint for a table's physical column names. Any failure (server
// down, endpoint that has no system.columns, non-existent table) degrades to
// "not found", and the leeway resolver then leaves that table's handles
// unresolved rather than erroring the whole query.
type chSchemaProvider struct {
	fetch func(ctx context.Context, db string, table string) (names []string, err error)
}

func (inst *chSchemaProvider) GetColumns(dbName string, tableName string) (columns iter.Seq[string], nColumns int, found bool) {
	ctx, cancel := context.WithTimeout(context.Background(), schemaProbeTimeout)
	defer cancel()
	names, err := inst.fetch(ctx, dbName, tableName)
	if err != nil {
		log.Debug().Err(err).Str("db", dbName).Str("table", tableName).
			Msg("play: system.columns probe failed — leeway name resolution skipped for this table")
		return nil, 0, false
	}
	if len(names) == 0 {
		return nil, 0, false
	}
	return slices.Values(names), len(names), true
}

// clientPassBinding is what a Client hands to ApplyBestEffortBound: the bundle
// of seams this client's pre-execute factories realise against. A single
// binding reaches every factory's Build (passreg passes the same value to each),
// so it carries all of them and each factory asserts the interface it needs:
//
//   - *lwsql.Resolver         → passes.ColumnResolverI  (ADR-0116, handles)
//     and passes.ConditionNamerI  (ADR-0121, leeway condition columns)
//   - passes.SchemaProviderI  → the raw column list, which ExposeSelectionConditions's
//     collision check needs even for a non-leeway table (ADR-0121 §SD4)
//
// The two contribute disjoint methods, so the embedding is unambiguous.
type clientPassBinding struct {
	*lwsql.Resolver
	passes.SchemaProviderI
}

// installLeewayNameResolution builds this client's leeway column-handle
// resolver and hands it to the client as the binding for the late-bound
// factories in the standard pre-execute set (ADR-0108 §SD7, ADR-0116 §SD6,
// ADR-0121 §SD7). The resolver learns each queried table's schema from
// system.columns via the client itself (a cached, lazy probe), so friendly
// handles like `symbol` or `` `geoPoint:lat` `` are rewritten to physical names
// before a query ships: buildResidual passes the binding to
// ApplyBestEffortBound, which realises the factories against this client's live
// endpoint. They live in passreg.Default (wired once per host by
// RegisterDefaults), so — unlike the retired per-client registry — they also
// show in keelson('sql_passes').
//
// It returns the resolver so the host can also feed it to the Diagnostics
// pane's client-side pre-execution warnings (the execution-path factory uses a
// nil sink and rewrites silently; the Diagnostics run supplies a collecting
// sink).
// It also realises the opt-in selection-condition rewrite (ADR-0121) against the
// same schema probe, so the top-bar toggle has a pass to run. That one is not
// registered — it changes a query's result schema, so it stays this host's
// choice rather than part of the standard set (ADR-0121 §SD7) — and it is built
// here because it needs exactly the seams this function already assembled: the
// schema for its collision check, and the resolver for leeway condition names.
func installLeewayNameResolution(client *Client) *lwsql.Resolver {
	provider := passes.NewCachingSchemaProvider(schemaCacheTTL, &chSchemaProvider{
		fetch: client.fetchColumnNames,
	}, schemaCacheMaxTables)
	resolver := lwsql.NewResolver(provider)
	client.passBinding = &clientPassBinding{Resolver: resolver, SchemaProviderI: provider}
	client.conditionsPass = passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{
		Schema: provider,
		Namer:  resolver,
	})
	return resolver
}
