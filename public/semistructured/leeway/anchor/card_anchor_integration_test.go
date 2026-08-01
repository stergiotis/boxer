//go:build integration

package anchor

import (
	"context"
	_ "embed"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The ClickHouse plumbing is keelson's chclient (ADR-0026 M2.5b) — the
// runtime client that was originally modelled on this test's hand-rolled
// HTTP code, adopted back so the demo consumes the platform's data plane
// (Ping gating, Exec DDL, Arrow IPC inserts) instead of duplicating it.

// The endpoint comes from the CLICKHOUSE_* registry entries (ADR-0009) rather
// than chclient.Defaults(), which hardcodes localhost:8123 and left a server
// anywhere else unreachable without editing the test.
func newAnchorChClient() *chclient.Client {
	return chclient.New(chclient.ConfigFromEnv(), nil)
}

//go:embed card_anchor_ddl_clickhouse.out.sql
var clickhouseDdlSql string

func setupClickHouseDdl(ctx context.Context, ch *chclient.Client) (err error) {
	err = ch.Exec(ctx, "CREATE DATABASE IF NOT EXISTS anchor;")
	if err != nil {
		return
	}
	err = ch.Exec(ctx, clickhouseDdlSql)
	return
}

func queryToString(ctx context.Context, ch *chclient.Client, sql string) (out string, err error) {
	var body io.ReadCloser
	body, err = ch.Query(ctx, sql)
	if err != nil {
		return
	}
	defer body.Close()
	var b []byte
	b, err = io.ReadAll(body)
	if err != nil {
		err = eh.Errorf("unable to read query response: %w", err)
		return
	}
	out = string(b)
	return
}

// generateIntersectingEvents builds three entities engineered to meet in one
// H3 cell: a drone in transit, a seismic anomaly whose danger zone covers the
// cell, and a DDoS incident mapped to a facility in the same cell.
func generateIntersectingEvents(allocator memory.Allocator) ([]arrow.RecordBatch, error) {
	table := NewInEntityTestTable(allocator, 3)

	targetH3 := uint64(61029384)
	targetTime := time.Unix(1773269000, 0).UTC()

	// drone, IN_TRANSIT, at the target cell
	table.BeginEntity().SetId(1, []byte("TRK-001"))
	table.GetSectionSymbol().BeginAttribute("IN_TRANSIT").EndAttribute().EndSection()
	table.GetSectionTimeRange().BeginAttribute(targetTime.Add(-100*time.Second), targetTime.Add(100*time.Second)).EndAttribute().EndSection()
	table.GetSectionGeoPoint().BeginAttribute(45.99, 7.74, targetH3).EndAttribute().EndSection()
	if err := table.CommitEntity(); err != nil {
		return nil, err
	}

	// seismic anomaly, danger zone covering the cell
	table.BeginEntity().SetId(2, []byte("SENS-002"))
	table.GetSectionSymbol().BeginAttribute("SEISMIC_ANOMALY").EndAttribute().EndSection()
	table.GetSectionTimeRange().BeginAttribute(targetTime.Add(-300*time.Second), targetTime).EndAttribute().EndSection()
	areaAttr := table.GetSectionGeoArea().BeginAttribute()
	areaAttr.AddToCoContainers(45.99, 7.74, targetH3)
	areaAttr.EndAttribute()
	table.GetSectionGeoArea().EndSection()
	if err := table.CommitEntity(); err != nil {
		return nil, err
	}

	// DDoS on a facility in the same cell
	table.BeginEntity().SetId(3, []byte("INC-003"))
	table.GetSectionSymbol().BeginAttribute("DDOS").EndAttribute().EndSection()
	table.GetSectionTimeRange().BeginAttribute(targetTime.Add(-60*time.Second), targetTime.Add(30*time.Second)).EndAttribute().EndSection()
	cyberArea := table.GetSectionGeoArea().BeginAttribute()
	cyberArea.AddToCoContainers(45.98, 7.75, targetH3)
	cyberArea.EndAttribute()
	table.GetSectionGeoArea().EndSection()
	if err := table.CommitEntity(); err != nil {
		return nil, err
	}

	records, err := table.TransferRecords(nil)
	if err != nil {
		return nil, eh.Errorf("failed to transfer arrow records: %w", err)
	}
	return records, nil
}

// crossDomainFriendly is the correlation query in friendly form; the test
// rewrites it through the pre-execute pipeline and appends a FORMAT via
// SetFormat before shipping — the executed SQL never exists as a hand-written
// physical string.
const crossDomainFriendly = "SELECT h3_hex, groupUniqArray(entity_type) AS simultaneous_events, count() AS total_incidents " +
	"FROM (SELECT `symbol:value`[1] AS entity_type, arrayConcat(`geoPoint:h3`, `geoArea:h3`) AS all_h3_indices " +
	"FROM facts WHERE `timeRange:beginIncl`[1] >= toDateTime64('2026-03-11 00:00:00', 9, 'UTC')) " +
	"ARRAY JOIN all_h3_indices AS h3_hex GROUP BY h3_hex " +
	"HAVING has(simultaneous_events, 'IN_TRANSIT') AND (has(simultaneous_events, 'SEISMIC_ANOMALY') OR has(simultaneous_events, 'DDOS')) " +
	"ORDER BY total_incidents DESC"

func TestLeewayCrossDomainQuery(t *testing.T) {
	ctx := context.Background()
	ch := newAnchorChClient()

	if err := ch.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not available on %s, skipping test: %v", chclient.ConfigFromEnv().URL, err)
	}

	err := setupClickHouseDdl(ctx, ch)
	require.NoError(t, err)

	allocator := memory.NewGoAllocator()
	records, err := generateIntersectingEvents(allocator)
	require.NoError(t, err)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()

	t.Log("Inserting Arrow records into ClickHouse...")
	err = ch.InsertArrow(ctx, "anchor.facts", records)
	require.NoError(t, err)

	t.Log("Rewriting the friendly correlation query and executing it...")
	query, err := NewDqlPreExecutePipeline(NewDqlResolver(), nil).Run(crossDomainFriendly)
	require.NoError(t, err)
	query, err = passes.SetFormat("TabSeparatedWithNames").Run(query)
	require.NoError(t, err)

	result, err := queryToString(ctx, ch, query)
	require.NoError(t, err)
	t.Logf("\n=== CLICKHOUSE QUERY RESULTS ===\n%s\n", result)

	// the three engineered incidents intersect in exactly this cell
	if !strings.Contains(result, "61029384") || !strings.Contains(result, "3") {
		t.Errorf("Expected intersection on hex 61029384 with 3 incidents, got:\n%s", result)
	}
}
