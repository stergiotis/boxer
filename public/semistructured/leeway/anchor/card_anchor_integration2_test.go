package anchor

import (
	"context"
	"embed"
	_ "embed"
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed *.sql
var sqlFileContent embed.FS

func getSqlContent(path string, t *testing.T) string {
	b, err := sqlFileContent.ReadFile(path)
	require.NoError(t, err, path)
	return unsafeperf.UnsafeBytesToString(b)
}

func TestLeewayClickHouse(t *testing.T) {
	ctx := context.Background()
	ch := newClickHouseClient("http://localhost:8123")

	// 1. Check if ClickHouse is available
	if err := ch.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not available on localhost:8123, skipping test: %v", err)
	}

	// 2. Programmatically generate Schema and create table
	err := setupClickHouseDdl(ctx, ch)
	require.NoError(t, err)

	err = ch.Exec(ctx, getSqlContent("card_anchor_udf_unflatten_leeway_array.sql", t))
	require.NoError(t, err)

	// 4. Generate mock intersecting records
	records, err := GenerateAlpineEvents(nil, 20)
	require.NoError(t, err)
	records, err = GenerateCyberThreatEvents(records)
	require.NoError(t, err)
	records, err = GenerateDroneMissionEvents(records)
	require.NoError(t, err)

	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()

	t.Log("Inserting Arrow records into ClickHouse...")
	err = ch.InsertArrow(ctx, "anchor.facts", records)
	require.NoError(t, err)

	for _, p := range []string{
		"card_anchor_dql_query1.sql",
		"card_anchor_dql_query2.sql",
		"card_anchor_dql_query3.sql",
		"card_anchor_dql_query4.sql",
		"card_anchor_dql_query5.sql",
		"card_anchor_dql_query6.sql",
	} {
		q := getSqlContent(p, t)
		result, err := ch.Query(ctx, q)
		assert.NoError(t, err, q, string(result))
		fmt.Print(result)
	}
}
