//go:build integration

package stage2

import (
	"context"
	_ "embed"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
)

//go:embed drone_ddl_clickhouse.out.sql
var droneDDL string

// TestUploadToClickHouseServer uploads the marshalled drone rows to a real
// ClickHouse server — localhost:8123 unless the CLICKHOUSE_* entries say
// otherwise — skipped if unreachable (like anchor's integration tests). It
// creates the bespoke drone.facts table from the
// generated DDL, INSERTs the Arrow batch the marshallgen codec produces, and
// reads count + id range back to confirm the rows landed. (Value fidelity is
// covered by the clickhouse-local roundtrip; this proves a server INSERT works.)
//
// The plumbing is keelson's chclient — the runtime ClickHouse client — rather
// than a test-local HTTP shim; the demo consumes the same data plane the
// runtime does.
func TestUploadToClickHouseServer(t *testing.T) {
	ctx := context.Background()
	ch := chclient.New(chclient.ConfigFromEnv(), nil)

	if err := ch.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not available on %s, skipping: %v", chclient.ConfigFromEnv().URL, err)
	}

	require.NoError(t, ch.Exec(ctx, "CREATE DATABASE IF NOT EXISTS drone;"))
	require.NoError(t, ch.Exec(ctx, droneDDL)) // CREATE OR REPLACE TABLE drone.facts (...)

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

	require.NoError(t, ch.InsertArrow(ctx, "drone.facts", recs))

	// the id plain column lands under its generated physical name.
	body, err := ch.Query(ctx, `SELECT count(), min("id:id:u64:47::0:"), max("id:id:u64:47::0:") FROM drone.facts FORMAT TabSeparated`)
	require.NoError(t, err)
	defer body.Close()
	out, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "3\t1001\t1003\n", string(out))
}
