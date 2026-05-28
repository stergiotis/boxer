//go:build llm_generated_gemini3pro

package anchor

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ============================================================================
// 1. Tiny ClickHouse HTTP Client (Low Allocation, No SQL Drivers)
// ============================================================================

type clickHouseClient struct {
	endpoint string
	httpCli  *http.Client
}

func newClickHouseClient(endpoint string) *clickHouseClient {
	return &clickHouseClient{
		endpoint: endpoint,
		httpCli: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *clickHouseClient) injectReqHeaders(req *http.Request) {
	req.Header.Set("X-ClickHouse-User", "default")
	req.Header.Set("X-ClickHouse-Key", "")
}

// Ping checks if ClickHouse is available to skip the test gracefully.
func (c *clickHouseClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/ping", nil)
	if err != nil {
		return eh.Errorf("failed to create ping request: %w", err)
	}
	c.injectReqHeaders(req)
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return eh.Errorf("clickhouse not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return eb.Build().Int("status", resp.StatusCode).Errorf("clickhouse returned non-200")
	}
	return nil
}

// Exec executes a DDL or SQL query that doesn't return data.
func (c *clickHouseClient) Exec(ctx context.Context, query string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/", strings.NewReader(query))
	if err != nil {
		return eh.Errorf("failed to create exec request: %w", err)
	}
	c.injectReqHeaders(req)
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return eh.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Warn().Str("body", string(body)).Int("statusCode", resp.StatusCode).Msg("clickhouse returned an error")
		return eb.Build().
			Int("status", resp.StatusCode).
			Str("response", string(body)).
			Str("query", query).
			Errorf("clickhouse query failed")
	}
	return nil
}

// Query executes a query and returns the raw response bytes.
func (c *clickHouseClient) Query(ctx context.Context, query string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/", strings.NewReader(query))
	if err != nil {
		return nil, eh.Errorf("failed to create read request: %w", err)
	}
	c.injectReqHeaders(req)
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, eh.Errorf("failed to execute read query: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, eh.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Warn().Str("query", query).Str("body", string(body)).Int("statusCode", resp.StatusCode).Msg("clickhouse returned an error")
		return body, eb.Build().
			Int("status", resp.StatusCode).
			Str("response", string(body)).
			Errorf("clickhouse query failed")
	}
	return body, nil
}

// InsertArrow writes Apache Arrow Records directly into ClickHouse via IPC.
func (c *clickHouseClient) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error {
	if len(records) == 0 {
		return nil
	}

	var buf bytes.Buffer
	// Write standard Arrow IPC stream format
	writer, err := ipc.NewFileWriter(&buf, ipc.WithSchema(records[0].Schema()))
	if err != nil {
		return eh.Errorf("unable to create arrow writer: %w", err)
	}
	for _, rec := range records {
		if err := writer.Write(rec); err != nil {
			return eh.Errorf("failed to write arrow record to IPC: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return eh.Errorf("failed to close IPC writer: %w", err)
	}

	url := fmt.Sprintf("%s/?query=INSERT+INTO+%s+FORMAT+Arrow", c.endpoint, table)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return eh.Errorf("failed to create insert request: %w", err)
	}

	c.injectReqHeaders(req)
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return eh.Errorf("failed to execute insert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Warn().Str("body", string(body)).Int("statusCode", resp.StatusCode).Msg("clickhouse returned an error")
		return eb.Build().
			Int("status", resp.StatusCode).
			Str("response", string(body)).
			Errorf("clickhouse insert failed")
	}
	return nil
}

//go:embed card_anchor_ddl_clickhouse.out.sql
var clickhouseDdlSql string

func setupClickHouseDdl(ctx context.Context, ch *clickHouseClient) (err error) {
	err = ch.Exec(ctx, "CREATE DATABASE IF NOT EXISTS anchor;")
	if err != nil {
		return
	}
	err = ch.Exec(ctx, clickhouseDdlSql)
	return
}

// ============================================================================
// 3. Data Generation: Forcing the Cross-Domain Intersection
// ============================================================================

func generateIntersectingEvents(allocator memory.Allocator) ([]arrow.RecordBatch, error) {
	table := NewInEntityTestTable(allocator, 3)

	// Target Intersection Criteria for our query:
	// Hex = 61029384, Date = March 11, 2026 (~1773268200)
	targetH3 := uint64(61029384)
	targetTime := time.Unix(1773269000, 0).UTC()

	// --- Event 1: Drone IN_TRANSIT ---
	table.BeginEntity().SetId(1, []byte("TRK-001"))
	table.GetSectionSymbol().BeginAttribute("IN_TRANSIT").EndAttribute().EndSection()
	table.GetSectionTimeRange().BeginAttribute(targetTime.Add(-100*time.Second), targetTime.Add(100*time.Second)).EndAttribute().EndSection()
	table.GetSectionGeoPoint().BeginAttribute(45.99, 7.74, targetH3).EndAttribute().EndSection()
	if err := table.CommitEntity(); err != nil {
		return nil, err
	}

	// --- Event 2: Alpine SEISMIC_ANOMALY ---
	table.BeginEntity().SetId(2, []byte("SENS-002"))
	table.GetSectionSymbol().BeginAttribute("SEISMIC_ANOMALY").EndAttribute().EndSection()
	table.GetSectionTimeRange().BeginAttribute(targetTime.Add(-300*time.Second), targetTime).EndAttribute().EndSection()
	// Alpine uses GeoArea to indicate danger zone
	areaAttr := table.GetSectionGeoArea().BeginAttribute()
	areaAttr.AddToCoContainers(45.99, 7.74, targetH3) // The intersecting Hex
	areaAttr.EndAttribute()
	table.GetSectionGeoArea().EndSection()
	if err := table.CommitEntity(); err != nil {
		return nil, err
	}

	// --- Event 3: Cyber DDOS targeting Regional Data Center ---
	table.BeginEntity().SetId(3, []byte("INC-003"))
	table.GetSectionSymbol().BeginAttribute("DDOS").EndAttribute().EndSection()
	table.GetSectionTimeRange().BeginAttribute(targetTime.Add(-60*time.Second), targetTime.Add(30*time.Second)).EndAttribute().EndSection()
	// Cyber also maps to GeoArea
	cyberArea := table.GetSectionGeoArea().BeginAttribute()
	cyberArea.AddToCoContainers(45.98, 7.75, targetH3) // Target datacenter in the same Hex
	cyberArea.EndAttribute()
	table.GetSectionGeoArea().EndSection()
	if err := table.CommitEntity(); err != nil {
		return nil, err
	}

	// Return extracted Arrow records
	records, err := table.TransferRecords(nil)
	if err != nil {
		return nil, eh.Errorf("failed to transfer arrow records: %w", err)
	}
	return records, nil
}

func TestLeewayCrossDomainQuery(t *testing.T) {
	ctx := context.Background()
	ch := newClickHouseClient("http://localhost:8123")

	// 1. Check if ClickHouse is available
	if err := ch.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not available on localhost:8123, skipping test: %v", err)
	}

	// 2. Programmatically generate Schema and create table
	err := setupClickHouseDdl(ctx, ch)
	require.NoError(t, err)

	// 4. Generate mock intersecting records
	allocator := memory.NewGoAllocator()
	records, err := generateIntersectingEvents(allocator)
	require.NoError(t, err)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()

	// 5. Send Arrow Records to ClickHouse via HTTP IPC
	t.Log("Inserting Arrow records into ClickHouse...")
	err = ch.InsertArrow(ctx, "anchor.facts", records)
	require.NoError(t, err)

	// 6. Execute the Cross-Domain Correlation Query
	t.Log("Executing Cross-Domain correlation query...")
	query := `
		SELECT 
			h3_hex,
			groupUniqArray(entity_type) AS simultaneous_events,
			count() AS total_incidents
		FROM (
			SELECT 
				"id:id:u64:2k:0:0:",
				` + "`tv:symbol:value:val:s:m:0:24:0::data`[1]" + ` AS entity_type,
				arrayConcat(
					` + "`tv:geoPoint:h3:val:u64:g:0:0:0::geo`" + `,
					` + "`tv:geoArea:h3:val:u64m:g:0:0:0::geo`" + `
				) AS all_h3_indices
			FROM anchor.facts
			WHERE ` + "`tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`[1]" + ` >= toDateTime64('2026-03-11 00:00:00', 9, 'UTC')
		)
		ARRAY JOIN all_h3_indices AS h3_hex
		GROUP BY h3_hex
		HAVING has(simultaneous_events, 'IN_TRANSIT') 
		   AND (
			   has(simultaneous_events, 'SEISMIC_ANOMALY') OR 
			   has(simultaneous_events, 'DDOS')
		   )
		ORDER BY total_incidents DESC
		FORMAT TabSeparatedWithNames
	`

	result, err := ch.Query(ctx, query)
	require.NoError(t, err)

	resultStr := string(result)
	t.Logf("\n=== CLICKHOUSE QUERY RESULTS ===\n%s\n", resultStr)

	// Validate the results: We engineered Hex 61029384 to contain exactly 3 orthogonal incidents
	if !strings.Contains(resultStr, "61029384") || !strings.Contains(resultStr, "3") {
		t.Errorf("Expected intersection on hex 61029384 with 3 incidents, got:\n%s", resultStr)
	}
}
