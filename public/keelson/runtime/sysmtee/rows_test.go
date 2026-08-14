package sysmtee

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testTs = time.Unix(1_700_000_000, 0).UTC()

func TestEntityKey_IsStablePerSeriesAndDistinctAcross(t *testing.T) {
	// Stability is the contract: the key is what makes a host's samples one
	// series, so it must not depend on anything but the pair.
	assert.Equal(t, entityKey("box1", "cpu"), entityKey("box1", "cpu"))
	assert.NotEqual(t, entityKey("box1", "cpu"), entityKey("box1", "mem"))
	assert.NotEqual(t, entityKey("box1", "cpu"), entityKey("box2", "cpu"))
	// The separator is what stops a pair colliding with another by
	// concatenation — without it ("box", "1cpu") and ("box1", "cpu") would be
	// one series.
	assert.NotEqual(t, entityKey("box", "1cpu"), entityKey("box1", "cpu"))
}

func TestEntityNaturalKey_RecordsThePairTheKeyDigests(t *testing.T) {
	nk, err := entityNaturalKey("box1", "cpu")
	require.NoError(t, err)
	assert.Contains(t, string(nk), "box1")
	assert.Contains(t, string(nk), "cpu")
}

func TestCpuRow_CarriesTheSampleThrough(t *testing.T) {
	snap := &sysmsnap.CPUSnapshot{
		TotalPercent:   42,
		PerCorePercent: []uint8{10, 20, 30, 40},
		PerCoreFreqMHz: []uint32{3200, 3300, 3100, 3400},
		LoadAvg1:       1.5,
		LoadAvg5:       2.5,
		LoadAvg15:      3.5,
		ActiveCPUs:     []int32{0, 1, 2, 3},
		ModelName:      "Test CPU",
		LogicalCores:   4,
	}
	row, err := cpuRow("box1", snap, testTs)
	require.NoError(t, err)

	assert.Equal(t, entityKey("box1", "cpu"), row.Id)
	assert.Equal(t, testTs, row.Ts)
	assert.Equal(t, "box1", row.Host)
	assert.Equal(t, kindCpu, row.Kind)
	assert.EqualValues(t, 42, row.TotalPercent)
	assert.Equal(t, []uint8{10, 20, 30, 40}, row.PerCorePercent)
	assert.Equal(t, []uint32{3200, 3300, 3100, 3400}, row.PerCoreFreqMHz)
	assert.EqualValues(t, 3.5, row.LoadAvg15)
	assert.Equal(t, []int32{0, 1, 2, 3}, row.ActiveCPUs)
}

// The point of the option: a host without RAPL must store no watts, not zero
// watts. Zero is a legitimate reading and the two must stay distinguishable.
func TestCpuRow_UsageWattsAbsentWhenUnavailable(t *testing.T) {
	unavailable, err := cpuRow("box1", &sysmsnap.CPUSnapshot{UsageWatts: 0, UsageWattsAvailable: false}, testTs)
	require.NoError(t, err)
	assert.False(t, unavailable.UsageWatts.Has, "no RAPL must store no value")

	zero, err := cpuRow("box1", &sysmsnap.CPUSnapshot{UsageWatts: 0, UsageWattsAvailable: true}, testTs)
	require.NoError(t, err)
	require.True(t, zero.UsageWatts.Has, "a genuine zero reading must be stored")
	assert.EqualValues(t, 0, zero.UsageWatts.Val)
}

// The descriptor is a different series from the sample: they share a host and a
// domain but must not share a key, or one would overwrite the other's place in
// the (key, order) ordering and Replay of the sample series would interleave
// descriptors.
func TestCpuInfoRow_IsADistinctSeriesFromTheSample(t *testing.T) {
	snap := &sysmsnap.CPUSnapshot{ModelName: "Test CPU", LogicalCores: 8}
	info, err := cpuInfoRow("box1", snap, testTs)
	require.NoError(t, err)
	sample, err := cpuRow("box1", snap, testTs)
	require.NoError(t, err)

	assert.NotEqual(t, sample.Id, info.Id)
	assert.Equal(t, "Test CPU", info.ModelName)
	assert.EqualValues(t, 8, info.LogicalCores)
	assert.Equal(t, kindCpuInfo, info.Kind)
}

func TestMemRow_CarriesTheSampleThrough(t *testing.T) {
	row, err := memRow("box1", &sysmsnap.MemSnapshot{
		TotalBytes:     16 << 30,
		FreeBytes:      4 << 30,
		AvailableBytes: 8 << 30,
		UsedBytes:      8 << 30,
		SwapTotalBytes: 2 << 30,
		SwapFreeBytes:  2 << 30,
	}, testTs)
	require.NoError(t, err)
	assert.Equal(t, entityKey("box1", "mem"), row.Id)
	assert.Equal(t, kindMem, row.Kind)
	assert.EqualValues(t, 16<<30, row.TotalBytes)
	assert.EqualValues(t, 8<<30, row.AvailableBytes)
	assert.EqualValues(t, 0, row.SwapUsedBytes)
}

func TestSampleTime_PrefersTheBundleStamp(t *testing.T) {
	stamped := sampleTime(&sysmsnap.BundleSnapshot{SampledAtUnixMs: testTs.UnixMilli()})
	assert.Equal(t, testTs, stamped)

	// An unstamped bundle still has to land somewhere on the Order lane.
	before := time.Now().UTC().Add(-time.Second)
	assert.True(t, sampleTime(&sysmsnap.BundleSnapshot{}).After(before))
}
