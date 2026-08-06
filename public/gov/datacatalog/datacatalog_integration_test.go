//go:build integration

package datacatalog_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// The scratch database holds both the fixtures and the catalog this run
// writes. The catalog's target is deliberately not `boxer`: a run reads the
// whole server, so an integration test that also *wrote* where the real catalog
// lives would rebuild production state as a side effect.
const integrationDatabase = "datacatalog_integration_test"

func newLiveClient(t *testing.T) (client *chclient.Client) {
	t.Helper()
	client = chclient.New(chclient.ConfigFromEnv(), nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable: %v", err)
	}
	return
}

// setupFixtures creates a scratch database with three tables: two leeway ones
// in a containment relation and one opaque, series-shaped one. The leeway
// tables are created from their physical column names — which is all
// system.columns reports, and all the classifier reads.
func setupFixtures(t *testing.T, client *chclient.Client) (cleanup func()) {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string) {
		t.Helper()
		require.NoErrorf(t, client.Exec(ctx, sql), "sql: %s", sql)
	}
	exec("DROP DATABASE IF EXISTS " + integrationDatabase)
	exec("CREATE DATABASE " + integrationDatabase)

	small := buildOneSectionTable(t, "metric", "value")
	large := buildTwoSectionTable(t)
	exec(createFromLeeway(t, "lw_small", &small, "_"))
	exec(createFromLeeway(t, "lw_large", &large, ":"))
	exec("CREATE TABLE " + integrationDatabase + ".readings (" +
		"ts DateTime64(3), reading Float64) ENGINE = MergeTree ORDER BY ts")

	return func() {
		_ = client.Exec(ctx, "DROP DATABASE IF EXISTS "+integrationDatabase)
	}
}

// createFromLeeway renders a CREATE TABLE whose columns are the fixture's
// physical leeway names. Every column is a String — the naming grammar reads
// names, not ClickHouse types, so the physical type is irrelevant to what this
// test checks and a uniform one keeps the DDL short.
func createFromLeeway(t *testing.T, name string, tbl *common.TableDesc, sep string) (sql string) {
	t.Helper()
	names := physicalNames(t, tbl, sep)
	cols := make([]string, 0, len(names))
	for _, n := range names {
		// Backticks: a leeway physical name carries `:` and other characters
		// ClickHouse will not accept bare.
		cols = append(cols, "`"+strings.ReplaceAll(n, "`", "\\`")+"` String")
	}
	return "CREATE TABLE " + integrationDatabase + "." + name + " (" +
		strings.Join(cols, ", ") + ") ENGINE = MergeTree ORDER BY tuple()"
}

// scalar runs a one-value query and returns the answer as text.
func scalar(t *testing.T, client *chclient.Client, sql string) (value string) {
	t.Helper()
	body, err := client.Query(context.Background(), sql)
	require.NoErrorf(t, err, "sql: %s", sql)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	return strings.TrimSpace(string(raw))
}

// TestRefresh_LiveServer is the ADR-0170 §Verification integration case: a
// scratch database with two related leeway tables and an opaque series-shaped
// one, one `refresh`, then the kinds, the containment pair, and the shape row
// read back out of the catalog.
func TestRefresh_LiveServer(t *testing.T) {
	client := newLiveClient(t)
	cleanup := setupFixtures(t, client)
	defer cleanup()

	target := datacatalog.TargetDatabase(integrationDatabase)
	res, err := datacatalog.Run(context.Background(), datacatalog.NewChFetcher(client), client,
		target, false, zerolog.Nop())
	require.NoError(t, err)
	require.NotEmpty(t, res.RunId)

	q := func(sql string) string { return scalar(t, client, sql) }
	scope := " WHERE database = '" + integrationDatabase + "'"

	// The catalog discovered its own database, including the four tables it
	// just wrote there — the catalog lists itself, as the ADR intends.
	assert.Equal(t, "leeway", q("SELECT kind FROM "+target.Qualified(datacatalog.TableCatalog)+
		scope+" AND name = 'lw_small'"))
	assert.Equal(t, "leeway", q("SELECT kind FROM "+target.Qualified(datacatalog.TableCatalog)+
		scope+" AND name = 'lw_large'"))
	assert.Equal(t, "opaque", q("SELECT kind FROM "+target.Qualified(datacatalog.TableCatalog)+
		scope+" AND name = 'readings'"))

	// The restoration payload landed for the leeway ones and nothing else in
	// this database.
	assert.Equal(t, "2", q("SELECT count() FROM "+target.Qualified(datacatalog.TableLeeway)+scope))

	// lw_large strictly contains lw_small, so the pair reads superset (the
	// rows are ordered so A precedes B, and 'lw_large' < 'lw_small').
	assert.Equal(t, "superset", q("SELECT relation FROM "+target.Qualified(datacatalog.TableCompatibility)+
		" WHERE database_a = '"+integrationDatabase+"' AND name_a = 'lw_large'"+
		" AND database_b = '"+integrationDatabase+"' AND name_b = 'lw_small'"))
	// And the pair's shape id is the contained side's schema hash — the
	// invariant the Sankey chapter draws.
	assert.Equal(t, "1", q("SELECT count() FROM "+target.Qualified(datacatalog.TableCompatibility)+" AS p"+
		" INNER JOIN "+target.Qualified(datacatalog.TableLeeway)+" AS l"+
		" ON l.database = p.database_b AND l.name = p.name_b"+
		" WHERE p.database_a = '"+integrationDatabase+"' AND p.name_a = 'lw_large'"+
		" AND p.name_b = 'lw_small' AND p.shape_id = l.schema_hash"))

	// The opaque table satisfies exactly the series shape.
	assert.Equal(t, "series", q("SELECT shape FROM "+target.Qualified(datacatalog.TableOpaqueShapes)+
		scope+" AND name = 'readings'"))

	// Every row of every table carries this run's stamp, which is how a reader
	// tells a join that spans a rebuild from one that does not.
	assert.Equal(t, "1", q("SELECT count(DISTINCT run_id) FROM "+target.Qualified(datacatalog.TableCatalog)))
	assert.Equal(t, res.RunId, q("SELECT any(run_id) FROM "+target.Qualified(datacatalog.TableCatalog)))
}

// A second refresh replaces rather than appends: CREATE OR REPLACE is what
// makes the catalog a snapshot instead of a growing log.
func TestRefresh_ReplacesWholeCatalog(t *testing.T) {
	client := newLiveClient(t)
	cleanup := setupFixtures(t, client)
	defer cleanup()

	target := datacatalog.TargetDatabase(integrationDatabase)
	fetcher := datacatalog.NewChFetcher(client)
	_, err := datacatalog.Run(context.Background(), fetcher, client, target, false, zerolog.Nop())
	require.NoError(t, err)
	second, err := datacatalog.Run(context.Background(), fetcher, client, target, false, zerolog.Nop())
	require.NoError(t, err)

	assert.Equal(t, "1", scalar(t, client, "SELECT count(DISTINCT run_id) FROM "+
		target.Qualified(datacatalog.TableCatalog)))
	assert.Equal(t, second.RunId, scalar(t, client, "SELECT any(run_id) FROM "+
		target.Qualified(datacatalog.TableCatalog)))
}

// A dry run touches nothing: the catalog tables do not even come into
// existence.
func TestRefresh_DryRunCreatesNothing(t *testing.T) {
	client := newLiveClient(t)
	cleanup := setupFixtures(t, client)
	defer cleanup()

	target := datacatalog.TargetDatabase(integrationDatabase)
	res, err := datacatalog.Run(context.Background(), datacatalog.NewChFetcher(client), client,
		target, true, zerolog.Nop())
	require.NoError(t, err)
	assert.NotEmpty(t, res.Catalog)
	assert.Equal(t, "0", scalar(t, client, "SELECT count() FROM system.tables WHERE database = '"+
		integrationDatabase+"' AND name = '"+datacatalog.TableCatalog+"'"))
}
