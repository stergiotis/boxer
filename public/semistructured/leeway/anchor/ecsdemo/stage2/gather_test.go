package stage2

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// TestExtractComponentsFromRow marshals fat DroneEntity rows to a single Arrow
// batch, then extracts each typed component (Identity, Battery, Located, Tasked)
// out of that same row via Extract[T], asserting every component recovers its
// fields and shares the row's entity id. This is the stage-2 mirror of stage-1's
// World.Gather: one fat row, four typed component views.
func TestExtractComponentsFromRow(t *testing.T) {
	t0 := time.Unix(1_600_000_000, 0).UTC()
	original := []DroneEntity{
		{ID: 1001, Status: "IDLE", Battery: 9000, Tags: []string{"survey"},
			Lat: 47.5, Lng: 8.5, Cell: 12345, WindowBegin: t0, WindowEnd: t0.Add(time.Hour)},
		{ID: 1002, Status: "BUSY", Battery: 8000, Tags: []string{"urgent", "night"},
			Lat: 40.25, Lng: 12.5, Cell: 67890, WindowBegin: t0.Add(time.Hour), WindowEnd: t0.Add(2 * time.Hour)},
	}

	// marshal the fat entity via the generated codec
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

	// extract every component from the same row
	row, err := NewFatRow(recs[0])
	require.NoError(t, err)
	defer row.Release()
	require.Equal(t, len(original), row.NumRows())

	ids, err := Extract[Identity](row)
	require.NoError(t, err)
	bats, err := Extract[Battery](row)
	require.NoError(t, err)
	locs, err := Extract[Located](row)
	require.NoError(t, err)
	tasks, err := Extract[Tasked](row)
	require.NoError(t, err)

	require.Len(t, ids, len(original))
	require.Len(t, bats, len(original))
	require.Len(t, locs, len(original))
	require.Len(t, tasks, len(original))

	for i, o := range original {
		// every component carries the same entity id — they came from one row
		require.Equalf(t, o.ID, ids[i].ID, "row %d Identity.ID", i)
		require.Equalf(t, o.ID, bats[i].ID, "row %d Battery.ID", i)
		require.Equalf(t, o.ID, locs[i].ID, "row %d Located.ID", i)
		require.Equalf(t, o.ID, tasks[i].ID, "row %d Tasked.ID", i)

		require.Equalf(t, o.Status, ids[i].Status, "row %d Identity.Status", i)
		require.Equalf(t, o.Battery, bats[i].Charge, "row %d Battery.Charge", i)
		require.InDeltaf(t, float64(o.Lat), float64(locs[i].Lat), 1e-6, "row %d Located.Lat", i)
		require.InDeltaf(t, float64(o.Lng), float64(locs[i].Lng), 1e-6, "row %d Located.Lng", i)
		require.Equalf(t, o.Cell, locs[i].Cell, "row %d Located.Cell", i)
		require.Truef(t, o.WindowBegin.Equal(tasks[i].WindowBegin), "row %d Tasked.WindowBegin", i)
		require.Truef(t, o.WindowEnd.Equal(tasks[i].WindowEnd), "row %d Tasked.WindowEnd", i)
		require.Equalf(t, o.Tags, tasks[i].Tags, "row %d Tasked.Tags", i)
	}
}
