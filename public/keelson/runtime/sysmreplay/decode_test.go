package sysmreplay

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// msTime dates a row the way the store does — UTC, millisecond resolution.
func msTime(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// topoKindOf names a kind by its numeric value, so the parse test walks the
// enum rather than a hand-copied list of labels.
func topoKindOf(v uint8) sysmsnap.TopoKindE { return sysmsnap.TopoKindE(v) }

// These cover rows the tee cannot produce — misaligned arrays and malformed
// adjacency lists. The happy path is exercised against the writer itself, in
// package sysmtee's round-trip test; what is left is what a corrupted row, a
// partially-filtered projection, or a future writer bug would look like, and
// the point of each case is that it fails loudly rather than reading back
// plausibly.

func TestNetFrom_RefusesMisalignedArrays(t *testing.T) {
	row := sysmfacts.SysNet{
		Name:          []string{"eth0", "lo"},
		Index:         []int32{2, 1},
		HardwareAddr:  []string{"aa:bb:cc:dd:ee:ff", ""},
		Up:            []uint8{1, 1},
		Running:       []uint8{1},
		RxBytes:       []uint64{1, 2},
		TxBytes:       []uint64{3, 4},
		RxBytesPerSec: []uint64{5, 6},
		TxBytesPerSec: []uint64{7, 8},
	}
	snap, err := NetFrom(row)
	require.Error(t, err, "a short array must not be silently padded or truncated")
	assert.Nil(t, snap)
	assert.Contains(t, arrayNames(t, err), "Running")
	assert.Contains(t, err.Error(), "misaligned")
}

func TestGPUFrom_RefusesMisalignedArrays(t *testing.T) {
	row := sysmfacts.SysGpu{
		Vendor: []string{"amd"}, Index: []int32{0}, Name: []string{"780M"}, PCIID: []string{"0x15bf"},
		BusyPercent:     []uint8{33},
		MemoryUsedBytes: []uint64{1, 2}, MemoryTotalBytes: []uint64{8},
		PowerWatts: []float32{15}, TempC: []float32{61}, FreqMHz: []uint32{2700},
	}
	_, err := GPUFrom(row)
	require.Error(t, err)
	assert.Contains(t, arrayNames(t, err), "MemoryUsedBytes")
}

// TestBatteryFrom_GroupsAreCheckedSeparately pins that the two power-supply
// groups have independent lengths: a machine with two batteries and one adapter
// is ordinary, not misaligned.
func TestBatteryFrom_GroupsAreCheckedSeparately(t *testing.T) {
	row := sysmfacts.SysBattery{
		Name: []string{"BAT0", "BAT1"}, Type: []string{"Battery", "Battery"},
		Percent: []uint8{80, 90}, State: []uint8{2, 3},
		PowerWatts: []float32{1, 2}, SecondsToFull: []int64{-1, -1}, SecondsToEmpty: []int64{100, 200},
		AcName: []string{"AC"}, AcOnline: []uint8{1},
	}
	snap, err := BatteryFrom(row)
	require.NoError(t, err)
	assert.Len(t, snap.Batteries, 2)
	assert.Len(t, snap.ACAdapters, 1)

	row.AcOnline = []uint8{1, 0}
	_, err = BatteryFrom(row)
	require.Error(t, err, "the adapter group is still checked within itself")
	assert.Contains(t, arrayNames(t, err), "AcOnline")
}

func TestProcsFrom_RefusesMisalignedCmdRow(t *testing.T) {
	row := sysmfacts.SysProc{
		Pid: []uint32{1}, Ppid: []uint32{0}, Name: []string{"init"}, State: []string{"S"},
		CPUPercent: []float32{0}, RSSBytes: []uint64{0}, VMSizeBytes: []uint64{0},
		NumThreads: []int32{1}, Nice: []int32{0}, Priority: []int32{20},
		KernelThread: []uint8{0}, StartedAtUnixMs: []int64{0},
		Component: []string{""}, CgroupUnit: []string{""},
	}
	cmd := &sysmfacts.SysProcCmd{
		Pid: []uint32{1}, Cmd: []string{"/sbin/init"}, User: []string{"root"},
		Uid: []uint32{0}, Gid: []uint32{0, 0},
	}
	_, err := ProcsFrom(row, cmd)
	require.Error(t, err)
	assert.Equal(t, "sysProcCmd", ebtest.Fields(t, err)["kind"])
}

// TestProcsFrom_JoinsOnPid pins that the command line follows its own pid
// rather than its position, so a cmd row written in a different order still
// lands on the right process.
func TestProcsFrom_JoinsOnPid(t *testing.T) {
	row := sysmfacts.SysProc{
		Pid: []uint32{1, 4242}, Ppid: []uint32{0, 1},
		Name: []string{"init", "boxer"}, State: []string{"S", "R"},
		CPUPercent: []float32{0, 1}, RSSBytes: []uint64{0, 0}, VMSizeBytes: []uint64{0, 0},
		NumThreads: []int32{1, 1}, Nice: []int32{0, 0}, Priority: []int32{20, 20},
		KernelThread: []uint8{0, 0}, StartedAtUnixMs: []int64{0, 0},
		Component: []string{"", ""}, CgroupUnit: []string{"", ""},
	}
	cmd := &sysmfacts.SysProcCmd{
		Pid: []uint32{4242, 1}, Cmd: []string{"boxer play", "/sbin/init"},
		User: []string{"user", "root"}, Uid: []uint32{1000, 0}, Gid: []uint32{1000, 0},
	}
	procs, err := ProcsFrom(row, cmd)
	require.NoError(t, err)
	require.Len(t, procs, 2)
	assert.Equal(t, "/sbin/init", procs[0].Cmd)
	assert.Equal(t, "root", procs[0].User)
	assert.Equal(t, "boxer play", procs[1].Cmd)
	assert.Equal(t, uint32(1000), procs[1].UID)
}

// topoRow builds a well-formed three-node adjacency list the structural cases
// then damage one field at a time.
func topoRow() sysmfacts.SysTopology {
	return sysmfacts.SysTopology{
		Node:           []uint32{0, 1, 2},
		Parent:         []int32{-1, 0, 1},
		NodeKind:       []string{"Machine", "Package", "Core"},
		OSIndex:        []int32{-1, 0, 0},
		CacheLevel:     []uint8{0, 0, 0},
		CacheType:      []string{"", "", ""},
		CacheSizeBytes: []uint64{0, 0, 0},
		MemBytes:       []uint64{0, 0, 0},
		FreqPresent:    []uint8{0, 0, 0},
		FreqMinMHz:     []uint32{0, 0, 0},
		FreqMaxMHz:     []uint32{0, 0, 0},
		FreqGovernor:   []string{"", "", ""},
		FreqDriver:     []string{"", "", ""},
		LogicalCount:   1,
	}
}

func TestTopologyFrom_WellFormed(t *testing.T) {
	topo, err := TopologyFrom(topoRow())
	require.NoError(t, err)
	require.NotNil(t, topo.Root)
	require.Len(t, topo.Root.Children, 1)
	require.Len(t, topo.Root.Children[0].Children, 1)
	assert.Equal(t, int32(1), topo.LogicalCount)
}

func TestTopologyFrom_RefusesEmptyRow(t *testing.T) {
	_, err := TopologyFrom(sysmfacts.SysTopology{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no nodes")
}

func TestTopologyFrom_RefusesDanglingParent(t *testing.T) {
	row := topoRow()
	row.Parent[2] = 99
	_, err := TopologyFrom(row)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in the row")
}

func TestTopologyFrom_RefusesTwoRoots(t *testing.T) {
	row := topoRow()
	row.Parent[1] = -1
	_, err := TopologyFrom(row)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than one root")
}

func TestTopologyFrom_RefusesNoRoot(t *testing.T) {
	row := topoRow()
	row.Parent[0] = 2
	_, err := TopologyFrom(row)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no root")
}

// TestTopologyFrom_RefusesDetachedCycle is the case a per-node parent check
// cannot catch: every node has one parent and there is exactly one root, yet
// two nodes point at each other and hang off nothing.
func TestTopologyFrom_RefusesDetachedCycle(t *testing.T) {
	row := topoRow()
	row.Node = append(row.Node, 3, 4)
	row.Parent = append(row.Parent, 4, 3)
	row.NodeKind = append(row.NodeKind, "Core", "Core")
	row.OSIndex = append(row.OSIndex, 1, 2)
	row.CacheLevel = append(row.CacheLevel, 0, 0)
	row.CacheType = append(row.CacheType, "", "")
	row.CacheSizeBytes = append(row.CacheSizeBytes, 0, 0)
	row.MemBytes = append(row.MemBytes, 0, 0)
	row.FreqPresent = append(row.FreqPresent, 0, 0)
	row.FreqMinMHz = append(row.FreqMinMHz, 0, 0)
	row.FreqMaxMHz = append(row.FreqMaxMHz, 0, 0)
	row.FreqGovernor = append(row.FreqGovernor, "", "")
	row.FreqDriver = append(row.FreqDriver, "", "")

	_, err := TopologyFrom(row)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reachable from the root")
}

func TestTopologyFrom_RefusesSelfParent(t *testing.T) {
	row := topoRow()
	row.Parent[2] = 2
	_, err := TopologyFrom(row)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "its own parent")
}

func TestTopologyFrom_RefusesDuplicateNodeNumbers(t *testing.T) {
	row := topoRow()
	row.Node[2] = 1
	_, err := TopologyFrom(row)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "appears twice")
	f := ebtest.Fields(t, err)
	assert.NotNil(t, f["firstPosition"])
	assert.NotNil(t, f["secondPosition"])
}

// TestTopologyFrom_HonoursStoredNodeNumbers pins why the writer stores the node
// index explicitly: a row whose arrays were filtered no longer has position ==
// node number, and parent references must still resolve.
func TestTopologyFrom_HonoursStoredNodeNumbers(t *testing.T) {
	row := topoRow()
	row.Node = []uint32{100, 101, 102}
	row.Parent = []int32{-1, 100, 101}
	topo, err := TopologyFrom(row)
	require.NoError(t, err)
	require.Len(t, topo.Root.Children, 1)
	assert.Len(t, topo.Root.Children[0].Children, 1)
}

func TestTopologyFrom_RefusesMisalignedArrays(t *testing.T) {
	row := topoRow()
	row.FreqGovernor = []string{"", ""}
	_, err := TopologyFrom(row)
	require.Error(t, err)
	assert.Contains(t, arrayNames(t, err), "FreqGovernor")
}

// TestParseTopoKind_RoundTripsEveryKind pins the label inverse against the
// enum's own String, so a new kind cannot be added on one side only.
func TestParseTopoKind_RoundTripsEveryKind(t *testing.T) {
	for _, k := range []uint8{0, 1, 2, 3, 4, 5} {
		kind := topoKindOf(k)
		assert.Equal(t, kind, parseTopoKind(kind.String()), "kind %s", kind)
	}
}

func TestCarry_HoldsUntilTheNextRow(t *testing.T) {
	mk := func(ms int64) *sysmfacts.SysmetricsEntity {
		return &sysmfacts.SysmetricsEntity{Ts: msTime(ms)}
	}
	c := &carry{rows: []*sysmfacts.SysmetricsEntity{mk(100), mk(300)}, i: -1}

	assert.Nil(t, c.at(msTime(50)), "nothing has been reached yet")
	assert.Equal(t, msTime(100), c.at(msTime(100)).Ts, "a row at the tick counts")
	assert.Equal(t, msTime(100), c.at(msTime(299)).Ts, "it is held across ticks that carry none")
	assert.Equal(t, msTime(300), c.at(msTime(300)).Ts)
	assert.Equal(t, msTime(300), c.at(msTime(9999)).Ts, "the last row holds to the end")
}

// arrayNames returns the pair of array names a misalignment error carries, so a
// test can name the array it expects without matching prose.
func arrayNames(t *testing.T, err error) (names []any) {
	t.Helper()
	f := ebtest.Fields(t, err)
	return []any{f["array"], f["otherArray"]}
}
