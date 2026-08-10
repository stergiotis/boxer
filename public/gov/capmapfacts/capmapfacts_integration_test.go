//go:build integration

package capmapfacts_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapfacts"
	"github.com/stergiotis/boxer/public/gov/capmapvocab"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
)

// Physical column names for the sections this encoding writes. They are
// spelled out because the read path is SQL: a query has to name the leeway
// columns, and that coupling is the thing this test exists to pin.
const (
	colId       = "`id:id:u64:47::0:`"
	colSymValue = "`tv:symbol:value:val:s:124::I:0::data`"
	colSymLr    = "`tv:symbol:lr:lr:u64:1247:::0::data`"
	colTxtLmr   = "`tv:textArray:lmr:lmr:u64:1247:::0::data`"
	colTxtMrhp  = "`tv:textArray:mrhp:mrhp:y:4:::0::data`"
	colFkValue  = "`tv:foreignKey:value:val:u64:4:M::0::foreignKey`"
	colFkLr     = "`tv:foreignKey:lr:lr:u64:1247:M::0::foreignKey`"
)

const integrationDatabase = "capmap_facts_integration_test"

// newLiveTable creates a scratch facts table and returns a client plus its
// qualified name. It never touches boxer.facts: a test that dropped the
// runtime's own table would be destroying real state to check an encoding.
func newLiveTable(t *testing.T) (cli *chclient.Client, qualified string, cleanup func()) {
	t.Helper()
	cfg := chstore.Defaults()
	cfg.Database = integrationDatabase
	ctx := context.Background()
	store, err := chstore.New(cfg)
	require.NoError(t, err)
	if pErr := store.Ping(ctx); pErr != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, pErr)
	}
	require.NoError(t, store.DropTable(ctx))
	require.NoError(t, store.SetupTable(ctx, ""))
	cli = chclient.New(chclient.Config{URL: cfg.URL, User: cfg.User, Password: cfg.Password}, nil)
	return cli, cfg.Database + "." + cfg.Table, func() { _ = store.DropTable(context.Background()) }
}

// queryText runs sql and returns its TabSeparated body. The format is appended
// here rather than passed as an argument because the client takes none.
func queryText(t *testing.T, cli *chclient.Client, ctx context.Context, sql string) (out string) {
	t.Helper()
	body, err := cli.Query(ctx, sql+" FORMAT TabSeparated")
	require.NoError(t, err, sql)
	defer func() { _ = body.Close() }()
	raw, rErr := io.ReadAll(body)
	require.NoError(t, rErr)
	return string(raw)
}

// TestIngestRoundTripsThroughClickHouse is the end-to-end gate ADR-0168's
// verification plan names: the encoding is only correct if the rows it writes
// can be read back as the corpus that produced them.
//
// It asserts the three shapes unit tests cannot reach — a scalar attribute
// decoded by membership id, a section heading carried as a mixed membership's
// high-card parameter (§SD5), and a relation joined to its endpoints through
// the foreignKey section (§SD2).
func TestIngestRoundTripsThroughClickHouse(t *testing.T) {
	cli, qualified, cleanup := newLiveTable(t)
	defer cleanup()

	corpus := sampleCorpus()
	ctx := context.Background()
	stats, err := capmapfacts.Ingest(ctx, corpus, cli, qualified, fixedNow)
	require.NoError(t, err)
	require.Equal(t, 6, stats.Rows)

	query := func(sql string) (out string) { return queryText(t, cli, ctx, sql) }

	kindCap := capmapvocab.MembKindCompetence.GetId().Value()
	kindRel := capmapvocab.MembKindRelation.GetId().Value()
	slugMemb := capmapvocab.MembCompSlug.GetId().Value()
	secMemb := capmapvocab.MembCompSection.GetId().Value()
	srcMemb := capmapvocab.MembRelSource.GetId().Value()
	tgtMemb := capmapvocab.MembRelTarget.GetId().Value()

	t.Run("every row landed", func(t *testing.T) {
		assert.Equal(t, "6\n", query(fmt.Sprintf("SELECT count() FROM %s", qualified)))
		assert.Equal(t, "2\n", query(fmt.Sprintf(
			"SELECT count() FROM %s WHERE has(%s, %d)", qualified, colSymLr, kindCap)))
		assert.Equal(t, "4\n", query(fmt.Sprintf(
			"SELECT count() FROM %s WHERE has(%s, %d)", qualified, colSymLr, kindRel)))
	})

	t.Run("a scalar attribute decodes by membership id", func(t *testing.T) {
		got := query(fmt.Sprintf(
			"SELECT arrayElement(%s, indexOf(%s, %d)) AS slug FROM %s WHERE has(%s, %d) ORDER BY slug",
			colSymValue, colSymLr, slugMemb, qualified, colSymLr, kindCap))
		assert.Equal(t, "analytics\nrobustness\n", got)
	})

	t.Run("section headings ride the mixed membership parameter", func(t *testing.T) {
		got := query(fmt.Sprintf(
			"SELECT arrayStringConcat(arrayFilter((p, m) -> m = %d, %s, %s), '|') AS headings "+
				"FROM %s WHERE has(%s, %d) AND arrayElement(%s, indexOf(%s, %d)) = 'robustness'",
			secMemb, colTxtMrhp, colTxtLmr, qualified, colSymLr, kindCap, colSymValue, colSymLr, slugMemb))
		assert.Equal(t, "Standards\n", got)
	})

	t.Run("relations join to their endpoints through foreignKey", func(t *testing.T) {
		got := query(fmt.Sprintf(`
WITH
  caps AS (SELECT %s AS cid, arrayElement(%s, indexOf(%s, %d)) AS slug
           FROM %s WHERE has(%s, %d)),
  rels AS (SELECT arrayElement(%s, indexOf(%s, %d)) AS src,
                  arrayElement(%s, indexOf(%s, %d)) AS tgt
           FROM %s WHERE has(%s, %d) AND has(%s, %d))
SELECT concat(s.slug, '->', t.slug) AS edge
FROM rels r INNER JOIN caps s ON s.cid = r.src INNER JOIN caps t ON t.cid = r.tgt
ORDER BY edge`,
			colId, colSymValue, colSymLr, slugMemb,
			qualified, colSymLr, kindCap,
			colFkValue, colFkLr, srcMemb,
			colFkValue, colFkLr, tgtMemb,
			qualified, colSymLr, kindRel, colFkLr, tgtMemb))
		// The parent and the similarity edge both resolve; the unresolved link
		// and the citation carry no target foreign key and so do not appear.
		assert.Equal(t, "robustness->analytics\nrobustness->analytics\n", got)
	})
}

// A re-ingest of an unchanged corpus must reuse ids rather than mint a second
// set, which is what makes the vault the source of truth in practice: the
// table can always be rebuilt from it without accumulating duplicates under
// new keys.
func TestReIngestReusesIds(t *testing.T) {
	cli, qualified, cleanup := newLiveTable(t)
	defer cleanup()

	ctx := context.Background()
	_, err := capmapfacts.Ingest(ctx, sampleCorpus(), cli, qualified, fixedNow)
	require.NoError(t, err)
	_, err = capmapfacts.Ingest(ctx, sampleCorpus(), cli, qualified, fixedNow.Add(time.Hour))
	require.NoError(t, err)

	got := queryText(t, cli, ctx, fmt.Sprintf("SELECT count(), uniqExact(%s) FROM %s", colId, qualified))
	assert.Equal(t, "12\t6\n", got,
		"twelve rows written, but only six distinct ids — the second pass re-stated the same entities")
}
