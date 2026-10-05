package jackstay

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreflight(t *testing.T) {
	rep := DiskReport{
		Disks: []DiskInfo{
			{Name: "default", FreeSpace: 100, TotalSpace: 1000},
			{Name: "fast", FreeSpace: 50, TotalSpace: 100, KeepFreeSpace: 10},
		},
		Tables: []TableFootprint{
			{Ref: ref("d", "a"), Policy: "default", Disks: []string{"default"}},
			{Ref: ref("d", "b"), Policy: "hot", Disks: []string{"fast"}},
		},
	}
	tables := []*PlanTable{
		{Target: ref("d", "a"), Bytes: 40, Sync: &TableSync{Mode: SyncModeFull}},
		{Target: ref("d", "c"), Bytes: 100, Sync: &TableSync{Mode: SyncModeSample, SampleNum: 1, SampleDen: 4}},
		{Target: ref("d", "b"), Bytes: 30, Sync: &TableSync{Mode: SyncModeFull}},
	}
	out := Preflight(tables, &rep, 1.5)
	require.Len(t, out, 2)
	assert.Equal(t, []string{"default"}, out[0].Disks, "a table not on the target yet lands on default")
	assert.Equal(t, uint64(65), out[0].Need)
	assert.Equal(t, uint64(97), out[0].Headroom)
	assert.True(t, out[0].OK)
	assert.Equal(t, uint64(40), out[1].Free, "keep_free_space is not free")
	assert.Equal(t, uint64(45), out[1].Headroom)
	assert.False(t, out[1].OK)

	rep.Tables[0].BytesOnDisk = 20
	tables[0].Sync.Existing = ExistingPolicyReplace
	out = Preflight(tables, &rep, 1)
	assert.Equal(t, uint64(20), out[0].Held, "replace holds the target's bytes until merges")
	assert.Equal(t, uint64(85), out[0].Headroom)

	// Repair: the copy is the differing leaves' share of the source rows, and
	// the cleared share of the target's bytes is held until merges.
	d := &TableDiff{DstRows: 10, Differing: []ChunkDiff{{Leaves: []LeafDiff{{SrcRows: 4, DstRows: 5}}}}}
	repair := []*PlanTable{{Target: ref("d", "a"), Bytes: 40, Rows: 8, Diff: d, Sync: &TableSync{Mode: SyncModeRepair}}}
	out = Preflight(repair, &rep, 1)
	require.Len(t, out, 1)
	assert.Equal(t, uint64(20), out[0].Need, "4 of 8 source rows")
	assert.Equal(t, uint64(10), out[0].Held, "5 of 10 target rows of 20 bytes")
	assert.Equal(t, uint64(30), out[0].Headroom)
	assert.True(t, out[0].OK)
}

func TestPreflight_SharedDisk(t *testing.T) {
	rep := DiskReport{
		Disks: []DiskInfo{
			{Name: "default", FreeSpace: 100, TotalSpace: 1000},
			{Name: "fast", FreeSpace: 100, TotalSpace: 1000},
		},
		Tables: []TableFootprint{
			{Ref: ref("d", "a"), Policy: "default", Disks: []string{"default"}},
			{Ref: ref("d", "b"), Policy: "tiered", Disks: []string{"default", "fast"}},
		},
	}
	tables := []*PlanTable{
		{Target: ref("d", "a"), Bytes: 60, Sync: &TableSync{Mode: SyncModeFull}},
		{Target: ref("d", "b"), Bytes: 60, Sync: &TableSync{Mode: SyncModeFull}},
	}
	out := Preflight(tables, &rep, 1)
	require.Len(t, out, 2)
	assert.Equal(t, uint64(100), out[0].Free)
	assert.Equal(t, uint64(200), out[1].Free)
	assert.LessOrEqual(t, out[0].Headroom, out[0].Free, "each group fits on its own")
	assert.LessOrEqual(t, out[1].Headroom, out[1].Free)
	assert.False(t, out[0].OK, "together they exceed the shared disk")
	assert.False(t, out[1].OK)

	tables[1].Bytes = 40
	out = Preflight(tables, &rep, 1)
	assert.True(t, out[0].OK, "60 + 40 fit on default")
	assert.True(t, out[1].OK)
}

func TestPreflight_MultiDiskGroup(t *testing.T) {
	// One group over two disks no other group uses: its headroom is set
	// against their free space together, not charged whole to each.
	rep := DiskReport{
		Disks: []DiskInfo{
			{Name: "d1", FreeSpace: 100, TotalSpace: 1000},
			{Name: "d2", FreeSpace: 100, TotalSpace: 1000},
		},
		Tables: []TableFootprint{{Ref: ref("d", "a"), Policy: "jbod", Disks: []string{"d1", "d2"}}},
	}
	tables := []*PlanTable{{Target: ref("d", "a"), Bytes: 150, Sync: &TableSync{Mode: SyncModeFull}}}
	out := Preflight(tables, &rep, 1)
	require.Len(t, out, 1)
	assert.Equal(t, uint64(200), out[0].Free)
	assert.Equal(t, uint64(150), out[0].Headroom)
	assert.True(t, out[0].OK)

	tables[0].Bytes = 250
	out = Preflight(tables, &rep, 1)
	assert.False(t, out[0].OK)
}

func TestPreflight_Filter(t *testing.T) {
	rep := DiskReport{
		Disks:  []DiskInfo{{Name: "default", FreeSpace: 1000, TotalSpace: 1000}},
		Tables: []TableFootprint{{Ref: ref("d", "a"), Disks: []string{"default"}, BytesOnDisk: 100, Rows: 50}},
	}
	// The slice is 25 of the source's 100 rows, 10 of the target's 50.
	diff := &TableDiff{SrcRows: 25, DstRows: 10, Differing: []ChunkDiff{{Leaves: []LeafDiff{{SrcRows: 5, DstRows: 4}}}}}
	pt := &PlanTable{Target: ref("d", "a"), Bytes: 400, Rows: 100, Filter: "x > 0", Diff: diff,
		Sync: &TableSync{Mode: SyncModeFull, Existing: ExistingPolicyReplace}}
	out := Preflight([]*PlanTable{pt}, &rep, 1)
	require.Len(t, out, 1)
	assert.Equal(t, uint64(100), out[0].Need, "a quarter of the source")
	assert.Equal(t, uint64(20), out[0].Held, "the slice's 10 of 50 target rows")
	assert.Empty(t, out[0].Overstated)

	// Repair: bytes per row come from the target's own rows, not the slice's.
	pt.Sync = &TableSync{Mode: SyncModeRepair}
	out = Preflight([]*PlanTable{pt}, &rep, 1)
	assert.Equal(t, uint64(20), out[0].Need, "5 of 100 source rows")
	assert.Equal(t, uint64(8), out[0].Held, "4 of 50 target rows")
	assert.Empty(t, out[0].Overstated)

	// Without the target's row count, the slice's stands in and overstates.
	rep.Tables[0].Rows = 0
	out = Preflight([]*PlanTable{pt}, &rep, 1)
	assert.Equal(t, uint64(40), out[0].Held, "4 of the slice's 10 rows")
	assert.Equal(t, []datacatalog.TableRef{ref("d", "a")}, out[0].Overstated)

	// No diff has counted the slice: the whole table, said to overstate.
	pt.Diff = nil
	pt.Sync = &TableSync{Mode: SyncModeSample, SampleNum: 1, SampleDen: 2, Existing: ExistingPolicyReplace}
	out = Preflight([]*PlanTable{pt}, &rep, 1)
	assert.Equal(t, uint64(200), out[0].Need)
	assert.Equal(t, uint64(100), out[0].Held)
	assert.Equal(t, []datacatalog.TableRef{ref("d", "a")}, out[0].Overstated)

	// Unfiltered tables are never overstated.
	pt.Filter = ""
	out = Preflight([]*PlanTable{pt}, &rep, 1)
	assert.Empty(t, out[0].Overstated)
}

func TestExpectedCopy_Filter(t *testing.T) {
	diff := &TableDiff{SrcRows: 25, Differing: []ChunkDiff{{AbsentOnTarget: true, SrcRows: 5}}}
	pt := &PlanTable{Rows: 100, Filter: "x > 0", Diff: diff, Sync: &TableSync{Mode: SyncModeFull}}
	assert.InDelta(t, 0.25, ExpectedCopyFraction(pt), 1e-9, "the slice")
	pt.Sync = &TableSync{Mode: SyncModeSample, SampleNum: 1, SampleDen: 5}
	assert.InDelta(t, 0.05, ExpectedCopyFraction(pt), 1e-9, "a fifth of the slice")
	pt.Sync = &TableSync{Mode: SyncModeRepair}
	assert.InDelta(t, 0.05, ExpectedCopyFraction(pt), 1e-9, "repair counts rows, not shares")
	pt.Sync = nil
	assert.InDelta(t, 0.25, ExpectedCopyFraction(pt), 1e-9)
	assert.Equal(t, int64(25), ExpectedRows([]*PlanTable{pt}), "progress counts towards the slice")

	pt.Diff = nil
	f, known := expectedCopy(pt)
	assert.InDelta(t, 1, f, 1e-9)
	assert.False(t, known, "no diff has counted the slice")
	pt.Filter = ""
	_, known = expectedCopy(pt)
	assert.True(t, known)
}

func TestFreeFloor(t *testing.T) {
	f := FreeFloor{MinFreeBytes: 10, MinFreeFraction: 0.1}
	rep := DiskReport{Disks: []DiskInfo{
		{Name: "a", FreeSpace: 50, TotalSpace: 1000},
		{Name: "b", FreeSpace: 150, TotalSpace: 1000},
		{Name: "c", FreeSpace: 15, TotalSpace: 50, KeepFreeSpace: 8},
	}}
	low := f.Low(&rep, []string{"a", "b", "c", "missing"})
	require.Len(t, low, 2)
	assert.Equal(t, "a", low[0].Name, "below 10% of its size")
	assert.Equal(t, "c", low[1].Name, "the floor is on top of keep_free_space")
}

// fakeDiskQuery answers the four ReadDisks queries with canned bodies, or
// fails every query with err.
type fakeDiskQuery struct {
	free  uint64
	err   error
	calls int
}

func (inst *fakeDiskQuery) Query(_ context.Context, sql string) (body io.ReadCloser, err error) {
	inst.calls++
	if inst.err != nil {
		return nil, inst.err
	}
	var rows string
	switch {
	case strings.Contains(sql, "system.disks"):
		rows = `{"name":"default","path":"/","free_space":` + strconv.FormatUint(inst.free, 10) + `,"total_space":1000,"keep_free_space":0}` + "\n"
	case strings.Contains(sql, "system.storage_policies"):
		rows = `{"policy_name":"default","disks":["default"]}` + "\n"
	case strings.Contains(sql, "system.tables"):
		rows = `{"database":"d","name":"t","storage_policy":"default"}` + "\n"
	case strings.Contains(sql, "system.parts"):
		rows = `{"database":"d","table":"t","bytes":7,"parts":1}` + "\n"
	}
	return io.NopCloser(strings.NewReader(rows)), nil
}

func TestWaitForFree_Canned(t *testing.T) {
	floor := FreeFloor{MinFreeBytes: 10, Poll: time.Second}

	// Not low: returns at once without notifying.
	q := &fakeDiskQuery{free: 500}
	notified := 0
	err := floor.WaitForFree(context.Background(), q, ref("d", "t"), func([]DiskInfo) { notified++ })
	require.NoError(t, err)
	assert.Zero(t, notified)
	assert.Equal(t, 4, q.calls, "one readout")

	// A failed readout does not end the wait: notify sees no disks, and only
	// the context does.
	q = &fakeDiskQuery{err: errors.New("connection refused")}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var seen [][]DiskInfo
	err = floor.WaitForFree(ctx, q, ref("d", "t"), func(low []DiskInfo) { seen = append(seen, low) })
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	require.Len(t, seen, 1)
	assert.Empty(t, seen[0])
}

func TestExpectedCopy(t *testing.T) {
	d := &TableDiff{Differing: []ChunkDiff{
		{AbsentOnTarget: true, SrcRows: 7},
		{AbsentOnSource: true, DstRows: 3},
		{Leaves: []LeafDiff{{SrcRows: 2, DstRows: 5}, {SrcRows: 1, DstRows: 0}}},
	}}
	assert.Equal(t, uint64(10), RepairCopyRows(d))
	assert.Equal(t, uint64(8), RepairClearRows(d))
	assert.InDelta(t, 0.1, ExpectedCopyFraction(&PlanTable{Rows: 100, Diff: d, Sync: &TableSync{Mode: SyncModeRepair}}), 1e-9)
	assert.InDelta(t, 0.25, ExpectedCopyFraction(&PlanTable{Sync: &TableSync{Mode: SyncModeSample, SampleNum: 1, SampleDen: 4}}), 1e-9)
	assert.InDelta(t, 1, ExpectedCopyFraction(&PlanTable{Sync: &TableSync{Mode: SyncModeSample, SampleNum: 1, SampleDen: 0}}), 1e-9, "a zero denominator is the whole table, not a division by zero")
	assert.InDelta(t, 1, ExpectedCopyFraction(&PlanTable{}), 1e-9)
}
