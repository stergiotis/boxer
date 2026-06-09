package stage2

import (
	"bytes"
	"context"
	_ "embed"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

//go:embed drone_ddl_clickhouse.out.sql
var droneDDL string

// chHTTP is a tiny dependency-free ClickHouse HTTP client (no SQL driver),
// modeled on anchor's integration test, used to upload the drone rows to a real
// server. Distinct from the clickhouse-local path the roundtrip test uses.
type chHTTP struct {
	endpoint string
	cli      *http.Client
}

func (c *chHTTP) send(ctx context.Context, method, u string, body io.Reader) (out []byte, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, 0, eh.Errorf("build request: %w", err)
	}
	req.Header.Set("X-ClickHouse-User", "default")
	req.Header.Set("X-ClickHouse-Key", "")
	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, 0, eh.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	out, _ = io.ReadAll(resp.Body)
	return out, resp.StatusCode, nil
}

func (c *chHTTP) exec(ctx context.Context, sql string) error {
	b, status, err := c.send(ctx, http.MethodPost, c.endpoint+"/", strings.NewReader(sql))
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return eb.Build().Int("status", status).Str("body", string(b)).Errorf("clickhouse exec failed")
	}
	return nil
}

func (c *chHTTP) query(ctx context.Context, sql string) (string, error) {
	b, status, err := c.send(ctx, http.MethodPost, c.endpoint+"/", strings.NewReader(sql))
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", eb.Build().Int("status", status).Str("body", string(b)).Errorf("clickhouse query failed")
	}
	return string(b), nil
}

func (c *chHTTP) insertArrow(ctx context.Context, table string, recs []arrow.RecordBatch) error {
	var buf bytes.Buffer
	w, err := ipc.NewFileWriter(&buf, ipc.WithSchema(recs[0].Schema()))
	if err != nil {
		return eh.Errorf("arrow writer: %w", err)
	}
	for _, rec := range recs {
		if err := w.Write(rec); err != nil {
			return eh.Errorf("arrow write: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return eh.Errorf("arrow close: %w", err)
	}
	u := c.endpoint + "/?query=" + url.QueryEscape("INSERT INTO "+table+" FORMAT Arrow")
	b, status, err := c.send(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return eb.Build().Int("status", status).Str("body", string(b)).Errorf("clickhouse insert failed")
	}
	return nil
}

// TestUploadToClickHouseServer uploads the marshalled drone rows to a real
// ClickHouse server on localhost:8123, skipped if unreachable (like anchor's
// integration tests). It creates the bespoke drone.facts table from the
// generated DDL, INSERTs the Arrow batch the marshallgen codec produces, and
// reads count + id range back to confirm the rows landed. (Value fidelity is
// covered by the clickhouse-local roundtrip; this proves a server INSERT works.)
func TestUploadToClickHouseServer(t *testing.T) {
	ctx := context.Background()
	ch := &chHTTP{endpoint: "http://localhost:8123", cli: &http.Client{Timeout: 10 * time.Second}}

	if _, status, err := ch.send(ctx, http.MethodGet, ch.endpoint+"/ping", nil); err != nil || status != http.StatusOK {
		t.Skipf("ClickHouse not available on localhost:8123, skipping: err=%v status=%d", err, status)
	}

	require.NoError(t, ch.exec(ctx, "CREATE DATABASE IF NOT EXISTS drone;"))
	require.NoError(t, ch.exec(ctx, droneDDL)) // CREATE OR REPLACE TABLE drone.facts (...)

	t0 := time.Unix(1_600_000_000, 0).UTC()
	original := []DroneEntity{
		{ID: 1001, Status: "IDLE", Battery: 9000, Tags: []string{"survey"}, Lat: 47.5, Lng: 8.5, Cell: 12345, WindowBegin: t0, WindowEnd: t0.Add(time.Hour)},
		{ID: 1002, Status: "IN_TRANSIT", Battery: 8000, Tags: []string{"survey", "urgent"}, Lat: 40.25, Lng: 12.5, Cell: 67890, WindowBegin: t0, WindowEnd: t0.Add(time.Hour)},
		{ID: 1003, Status: "CHARGING", Battery: 150, Tags: []string{"idle"}, Lat: 51.5, Lng: 0.5, Cell: 11111, WindowBegin: t0, WindowEnd: t0.Add(time.Hour)},
	}
	cols := &DroneEntityColumns{}
	for _, r := range original {
		cols.Append(r)
	}
	table := NewInEntityDroneTable(memory.NewGoAllocator(), cols.Len())
	require.NoError(t, DroneEntityBuildEntities(table, cols))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	require.NotEmpty(t, recs)

	require.NoError(t, ch.insertArrow(ctx, "drone.facts", recs))

	// the id plain column lands under its generated physical name.
	out, err := ch.query(ctx, `SELECT count(), min("id:id:u64:2k:0:0:"), max("id:id:u64:2k:0:0:") FROM drone.facts FORMAT TabSeparated`)
	require.NoError(t, err)
	require.Equal(t, "3\t1001\t1003\n", out)
}
