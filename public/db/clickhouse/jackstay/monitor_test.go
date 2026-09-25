package jackstay

import (
	"testing"

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
	assert.InDelta(t, 1, ExpectedCopyFraction(&PlanTable{}), 1e-9)
}
