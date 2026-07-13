package play

import (
	"context"
	"iter"
	"slices"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
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
	// leewayResolveOrder places the resolver after identsql (100) so it rewrites
	// already-macro-expanded SQL.
	leewayResolveOrder = 200
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

// installLeewayNameResolution gives client its own pass registry — the standard
// pre-execute set plus the schema-aware leeway name resolver — and points the
// client at it. The resolver learns each queried table's schema from
// system.columns via the client itself (a cached, lazy probe), so friendly
// handles like `symbol` or `` `geoPoint:lat` `` are rewritten to physical names
// before a query ships. It is wired per-client rather than into passreg.Default
// because the schema provider closes over this client's live endpoint.
// It returns the resolver so the host can also feed it to the Diagnostics
// pane's client-side pre-execution warnings (a nil sink on the execution path
// keeps rewriting silent; the Diagnostics run supplies a collecting sink).
func installLeewayNameResolution(client *Client) *lwsql.Resolver {
	provider := passes.NewCachingSchemaProvider(schemaCacheTTL, &chSchemaProvider{
		fetch: client.fetchColumnNames,
	}, schemaCacheMaxTables)
	resolver := lwsql.NewResolver(provider)

	reg := passreg.NewRegistry()
	if err := passregdefaults.RegisterStandard(reg); err != nil {
		log.Warn().Err(err).Msg("play: standard pass registration failed")
	}
	if err := reg.Register(passreg.Entry{
		Pass:        passes.ResolveColumnNames(resolver, "", nil),
		Stage:       passreg.StagePreExecute,
		Order:       leewayResolveOrder,
		Description: "resolve friendly leeway column handles to physical names",
		Provenance:  "github.com/stergiotis/boxer/public/semistructured/leeway/lwsql",
	}); err != nil {
		log.Warn().Err(err).Msg("play: leeway name-resolution pass registration failed")
	}
	client.passes = reg
	return resolver
}
