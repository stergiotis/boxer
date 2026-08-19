package sysmtee

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file pins the tee's row builders against their inverses in
// `sysmreplay`, without a database.
//
// It lives here, in package sysmtee, because the row builders are unexported
// and the inverse is what needs checking against them. sysmreplay does not
// import sysmtee — it restates the thirteen domain tokens instead — so this
// test is also where that duplication is held honest: TestRoundTrip_EntityKeys
// compares the keys the reader derives with the ids the writers stamp. Were
// sysmreplay to import sysmtee for those tokens, this test could not exist:
// the import would close a cycle through the test binary.

const rtHost = "test-host"

var rtTs = time.UnixMilli(1_700_000_000_123).UTC()

// TestRoundTrip_EntityKeys pins the reader's key derivation against the ids the
// writers actually stamp. A drift here is the one failure mode of restating the
// domain tokens in two packages, and it would otherwise surface as a replay
// that silently finds no rows.
func TestRoundTrip_EntityKeys(t *testing.T) {
	cpu, err := cpuRow(rtHost, &sysmsnap.CPUSnapshot{}, rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainCPU), cpu.Id, "cpu")

	info, err := cpuInfoRow(rtHost, &sysmsnap.CPUSnapshot{}, rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainCPUInfo), info.Id, "cpuInfo")

	mem, err := memRow(rtHost, &sysmsnap.MemSnapshot{}, rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainMem), mem.Id, "mem")

	psi, err := psiRow(rtHost, &sysmsnap.PSISnapshot{}, rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainPSI), psi.Id, "psi")

	net, err := netRow(rtHost, &sysmsnap.NetSnapshot{}, rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainNet), net.Id, "net")

	mounts, err := diskMountRow(rtHost, &sysmsnap.DiskSnapshot{}, rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainDiskMnt), mounts.Id, "diskMount")

	dio, err := diskIoRow(rtHost, &sysmsnap.DiskSnapshot{}, rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainDiskIO), dio.Id, "diskIo")

	bat, err := batteryRow(rtHost, &sysmsnap.BatterySnapshot{}, rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainBattery), bat.Id, "battery")

	gpu, err := gpuRow(rtHost, &sysmsnap.GPUSnapshot{}, rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainGPU), gpu.Id, "gpu")

	proc, err := procRow(rtHost, nil, rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainProc), proc.Id, "proc")

	pcmd, err := procCmdRow(rtHost, nil, rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainProcCmd), pcmd.Id, "procCmd")

	sock, err := socketRow(rtHost, &sysmsnap.SocketsSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainSockets), sock.Id, "sockets")

	topo, err := topologyRow(rtHost, rtTopology(), rtTs)
	require.NoError(t, err)
	assert.Equal(t, sysmreplay.EntityKey(rtHost, sysmreplay.DomainTopology), topo.Id, "topology")
}

func TestRoundTrip_CPU(t *testing.T) {
	in := &sysmsnap.CPUSnapshot{
		SampledAtUnixMs:     rtTs.UnixMilli(),
		TotalPercent:        42,
		PerCorePercent:      []uint8{10, 20, 30, 40},
		PerCoreFreqMHz:      []uint32{2400, 2500, 0, 3200},
		LoadAvg1:            1.5,
		LoadAvg5:            2.25,
		LoadAvg15:           0.75,
		UsageWatts:          17.5,
		UsageWattsAvailable: true,
		ActiveCPUs:          []int32{0, 1, 2, 3},
		ModelName:           "Test CPU @ 3.2GHz",
		LogicalCores:        4,
	}
	row, err := cpuRow(rtHost, in, rtTs)
	require.NoError(t, err)
	info, err := cpuInfoRow(rtHost, in, rtTs)
	require.NoError(t, err)

	got, err := sysmreplay.CPUFrom(row, &info)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

// TestRoundTrip_CPU_WattsAbsence pins the one field whose absence carries
// meaning: a host without RAPL must come back unavailable, not idle.
func TestRoundTrip_CPU_WattsAbsence(t *testing.T) {
	in := &sysmsnap.CPUSnapshot{SampledAtUnixMs: rtTs.UnixMilli(), UsageWattsAvailable: false, UsageWatts: 0}
	row, err := cpuRow(rtHost, in, rtTs)
	require.NoError(t, err)
	require.False(t, row.UsageWatts.Has, "no watts must be stored when RAPL is unavailable")

	got, err := sysmreplay.CPUFrom(row, nil)
	require.NoError(t, err)
	assert.False(t, got.UsageWattsAvailable)
	assert.Zero(t, got.UsageWatts)
}

// TestRoundTrip_CPU_WithoutDescriptor covers the tick the descriptor kind was
// not written on — the reader carries it forward, so nil here means "caller
// supplied none" and must not invent one.
func TestRoundTrip_CPU_WithoutDescriptor(t *testing.T) {
	in := &sysmsnap.CPUSnapshot{SampledAtUnixMs: rtTs.UnixMilli(), ModelName: "Test CPU", LogicalCores: 8}
	row, err := cpuRow(rtHost, in, rtTs)
	require.NoError(t, err)

	got, err := sysmreplay.CPUFrom(row, nil)
	require.NoError(t, err)
	assert.Empty(t, got.ModelName, "the descriptor lives in its own kind")
	assert.Zero(t, got.LogicalCores)
}

func TestRoundTrip_Mem(t *testing.T) {
	in := &sysmsnap.MemSnapshot{
		SampledAtUnixMs: rtTs.UnixMilli(),
		TotalBytes:      64 << 30, FreeBytes: 8 << 30, AvailableBytes: 40 << 30,
		BuffersBytes: 1 << 30, CachedBytes: 20 << 30,
		SwapTotalBytes: 8 << 30, SwapFreeBytes: 7 << 30,
		UsedBytes: 24 << 30, SwapUsedBytes: 1 << 30,
		ARCSizeBytes: 2 << 30, ARCMinBytes: 1 << 29,
	}
	row, err := memRow(rtHost, in, rtTs)
	require.NoError(t, err)
	got, err := sysmreplay.MemFrom(row)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

func TestRoundTrip_PSI(t *testing.T) {
	press := func(base float32, total uint64) sysmsnap.PSIPressure {
		return sysmsnap.PSIPressure{Avg10: base, Avg60: base * 2, Avg300: base * 3, TotalUs: total}
	}
	in := &sysmsnap.PSISnapshot{
		SampledAtUnixMs: rtTs.UnixMilli(),
		Available:       true,
		CPU:             sysmsnap.PSIResource{Some: press(1.5, 100), Full: press(0.5, 50)},
		Memory:          sysmsnap.PSIResource{Some: press(2.5, 200), Full: press(1.25, 150)},
		IO:              sysmsnap.PSIResource{Some: press(3.5, 300), Full: press(2.75, 250)},
	}
	row, err := psiRow(rtHost, in, rtTs)
	require.NoError(t, err)
	got, err := sysmreplay.PSIFrom(row)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

func TestRoundTrip_Net(t *testing.T) {
	in := &sysmsnap.NetSnapshot{
		SampledAtUnixMs: rtTs.UnixMilli(),
		Interfaces: []sysmsnap.NetInterface{
			{Name: "eth0", Index: 2, HardwareAddr: "aa:bb:cc:dd:ee:ff", Up: true, Running: true,
				RxBytes: 1 << 30, TxBytes: 1 << 29, RxBytesPerSec: 1024, TxBytesPerSec: 512},
			{Name: "lo", Index: 1, HardwareAddr: "", Up: true, Running: false,
				RxBytes: 4096, TxBytes: 4096, RxBytesPerSec: 0, TxBytesPerSec: 0},
		},
	}
	row, err := netRow(rtHost, in, rtTs)
	require.NoError(t, err)
	got, err := sysmreplay.NetFrom(row)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

// TestRoundTrip_Net_IPListsAreNotStored pins one of the ADR-0197 §SD8 gaps: a
// nested list per element does not flatten onto parallel arrays, so the
// addresses are gone and a caller must not read their absence as "no address".
func TestRoundTrip_Net_IPListsAreNotStored(t *testing.T) {
	in := &sysmsnap.NetSnapshot{
		SampledAtUnixMs: rtTs.UnixMilli(),
		Interfaces: []sysmsnap.NetInterface{
			{Name: "eth0", IPv4: []string{"10.0.0.1"}, IPv6: []string{"fe80::1"}},
		},
	}
	row, err := netRow(rtHost, in, rtTs)
	require.NoError(t, err)
	got, err := sysmreplay.NetFrom(row)
	require.NoError(t, err)
	require.Len(t, got.Interfaces, 1)
	assert.Nil(t, got.Interfaces[0].IPv4)
	assert.Nil(t, got.Interfaces[0].IPv6)
}

func TestRoundTrip_Disk(t *testing.T) {
	in := &sysmsnap.DiskSnapshot{
		SampledAtUnixMs: rtTs.UnixMilli(),
		Mounts: []sysmsnap.DiskMount{
			{Device: "/dev/nvme0n1p2", MountPoint: "/", FSType: "ext4", BlockName: "nvme0n1p2", Real: true,
				Capacity: sysmsnap.DiskCapacity{TotalBytes: 500 << 30, FreeBytes: 200 << 30, UsedBytes: 275 << 30, UsedPercent: 55.5}},
			{Device: "tmpfs", MountPoint: "/run", FSType: "tmpfs", BlockName: "", Real: false,
				Capacity: sysmsnap.DiskCapacity{TotalBytes: 8 << 30, FreeBytes: 8 << 30, UsedBytes: 0, UsedPercent: 0}},
		},
		BlockDevices: []sysmsnap.BlockDevice{
			{Name: "nvme0n1", ReadBytesPerSec: 1 << 20, WriteBytesPerSec: 1 << 19, BusyPercent: 12},
		},
	}
	mounts, err := diskMountRow(rtHost, in, rtTs)
	require.NoError(t, err)
	dio, err := diskIoRow(rtHost, in, rtTs)
	require.NoError(t, err)

	got, err := sysmreplay.DiskFrom(&mounts, &dio)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

// TestRoundTrip_Disk_MountOptionsAreNotStored pins the second §SD8 gap.
func TestRoundTrip_Disk_MountOptionsAreNotStored(t *testing.T) {
	in := &sysmsnap.DiskSnapshot{
		SampledAtUnixMs: rtTs.UnixMilli(),
		Mounts:          []sysmsnap.DiskMount{{Device: "/dev/sda1", MountPoint: "/", Options: "rw,relatime"}},
	}
	mounts, err := diskMountRow(rtHost, in, rtTs)
	require.NoError(t, err)
	got, err := sysmreplay.DiskFrom(&mounts, nil)
	require.NoError(t, err)
	require.Len(t, got.Mounts, 1)
	assert.Empty(t, got.Mounts[0].Options)
}

// TestRoundTrip_Disk_IndependentLengths is the reason the tee splits disk into
// two kinds: one snapshot, two item lists that do not align.
func TestRoundTrip_Disk_IndependentLengths(t *testing.T) {
	in := &sysmsnap.DiskSnapshot{
		SampledAtUnixMs: rtTs.UnixMilli(),
		Mounts: []sysmsnap.DiskMount{
			{Device: "a", MountPoint: "/a"}, {Device: "b", MountPoint: "/b"}, {Device: "c", MountPoint: "/c"},
		},
		BlockDevices: []sysmsnap.BlockDevice{{Name: "sda"}},
	}
	mounts, err := diskMountRow(rtHost, in, rtTs)
	require.NoError(t, err)
	dio, err := diskIoRow(rtHost, in, rtTs)
	require.NoError(t, err)
	got, err := sysmreplay.DiskFrom(&mounts, &dio)
	require.NoError(t, err)
	assert.Len(t, got.Mounts, 3)
	assert.Len(t, got.BlockDevices, 1)
}

func TestRoundTrip_Battery(t *testing.T) {
	in := &sysmsnap.BatterySnapshot{
		SampledAtUnixMs: rtTs.UnixMilli(),
		Batteries: []sysmsnap.BatteryStatus{
			{Name: "BAT0", Type: "Battery", Percent: 87, State: sysmsnap.StateDischarging,
				PowerWatts: 12.5, SecondsToFull: -1, SecondsToEmpty: 7200},
			{Name: "BAT1", Type: "UPS", Percent: 100, State: sysmsnap.StateFull,
				PowerWatts: 0, SecondsToFull: -1, SecondsToEmpty: -1},
		},
		ACAdapters: []sysmsnap.ACAdapter{{Name: "AC", Online: false}},
	}
	row, err := batteryRow(rtHost, in, rtTs)
	require.NoError(t, err)
	got, err := sysmreplay.BatteryFrom(row)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

// TestRoundTrip_Battery_EveryState pins the numeric state code against the enum
// — the tee stores the code rather than the label precisely so a rename on the
// Go side cannot silently reinterpret stored rows.
func TestRoundTrip_Battery_EveryState(t *testing.T) {
	for _, st := range sysmsnap.AllStates {
		in := &sysmsnap.BatterySnapshot{
			SampledAtUnixMs: rtTs.UnixMilli(),
			Batteries:       []sysmsnap.BatteryStatus{{Name: "BAT0", State: st, SecondsToFull: -1, SecondsToEmpty: -1}},
			ACAdapters:      []sysmsnap.ACAdapter{},
		}
		row, err := batteryRow(rtHost, in, rtTs)
		require.NoError(t, err)
		got, err := sysmreplay.BatteryFrom(row)
		require.NoError(t, err)
		require.Len(t, got.Batteries, 1)
		assert.Equal(t, st, got.Batteries[0].State, "state %s", st)
	}
}

func TestRoundTrip_GPU(t *testing.T) {
	in := &sysmsnap.GPUSnapshot{
		SampledAtUnixMs: rtTs.UnixMilli(),
		Devices: []sysmsnap.GPUDevice{
			{Vendor: "amd", Index: 0, Name: "Radeon 780M", PCIID: "0x15bf", BusyPercent: 33,
				MemoryUsedBytes: 1 << 30, MemoryTotalBytes: 8 << 30, PowerWatts: 15.25, TempC: 61.5, FreqMHz: 2700},
			{Vendor: "intel", Index: 1, Name: "UHD", PCIID: "0x9a49", BusyPercent: 0},
		},
	}
	row, err := gpuRow(rtHost, in, rtTs)
	require.NoError(t, err)
	got, err := sysmreplay.GPUFrom(row)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

func TestRoundTrip_Procs_WithCmd(t *testing.T) {
	in := []sysmsnap.ProcInfo{
		{PID: 1, PPID: 0, Name: "systemd", Cmd: "/sbin/init splash", State: 'S', UID: 0, GID: 0, User: "root",
			StartedAtUnixMs: 1_699_000_000_000, CPUPercent: 0.5, RSSBytes: 12 << 20, VMSizeBytes: 200 << 20,
			NumThreads: 1, Nice: 0, Priority: 20, KernelThread: false, Component: "init", CgroupUnit: "init.scope"},
		{PID: 2, PPID: 0, Name: "kthreadd", Cmd: "", State: 'S', KernelThread: true},
		{PID: 4242, PPID: 1, Name: "boxer", Cmd: "boxer play --x", State: 'R', UID: 1000, GID: 1000, User: "user",
			CPUPercent: 12.5, RSSBytes: 300 << 20, NumThreads: 12, Nice: -5, Priority: 15},
	}
	row, err := procRow(rtHost, in, rtTs)
	require.NoError(t, err)
	cmd, err := procCmdRow(rtHost, in, rtTs)
	require.NoError(t, err)

	got, err := sysmreplay.ProcsFrom(row, &cmd)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

// TestRoundTrip_Procs_WithoutCmd is the default deployment: --tee-proc-cmd off,
// so the sensitive kind was never written (ADR-0184 §SD8). The rest of the
// table must still come back whole.
func TestRoundTrip_Procs_WithoutCmd(t *testing.T) {
	in := []sysmsnap.ProcInfo{
		{PID: 4242, PPID: 1, Name: "boxer", Cmd: "boxer play --secret", State: 'R',
			UID: 1000, GID: 1000, User: "user", CPUPercent: 12.5},
	}
	row, err := procRow(rtHost, in, rtTs)
	require.NoError(t, err)

	got, err := sysmreplay.ProcsFrom(row, nil)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "boxer", got[0].Name, "the non-sensitive half survives")
	assert.Equal(t, float32(12.5), got[0].CPUPercent)
	assert.Empty(t, got[0].Cmd, "command line is opt-in and was not stored")
	assert.Empty(t, got[0].User)
	assert.Zero(t, got[0].UID)
	assert.Zero(t, got[0].GID)
}

// TestRoundTrip_Procs_ZeroState pins the NUL the tee refuses to store: an
// unreported state is an empty label going in and the zero byte coming back,
// not the character 0.
func TestRoundTrip_Procs_ZeroState(t *testing.T) {
	in := []sysmsnap.ProcInfo{{PID: 7, Name: "x", State: 0}}
	row, err := procRow(rtHost, in, rtTs)
	require.NoError(t, err)
	require.Equal(t, "", row.State[0])
	got, err := sysmreplay.ProcsFrom(row, nil)
	require.NoError(t, err)
	assert.Equal(t, byte(0), got[0].State)
}

func TestRoundTrip_Sockets(t *testing.T) {
	in := &sysmsnap.SocketsSnapshot{
		CollectedAtUnixMs: rtTs.UnixMilli(),
		Sockets: []sysmsnap.SocketInfo{
			{Proto: sysmsnap.SocketProtoTCP, Addr: "0.0.0.0", Port: 8123, Inode: 12345, UID: 0, PID: 900},
			{Proto: sysmsnap.SocketProtoUnix, Addr: "@/tmp/x.sock", Port: 0, Inode: 999, UID: 1000, PID: 0},
			{Proto: sysmsnap.SocketProtoTCP6, Addr: "::", Port: 9000, Inode: 4242, UID: 1000, PID: 4242},
		},
	}
	row, err := socketRow(rtHost, in)
	require.NoError(t, err)
	// The sockets row is dated by the collector's own stamp, not the bundle's
	// — the property the reader relies on to carry it across ticks.
	require.Equal(t, in.CollectedAtUnixMs, row.Ts.UnixMilli())

	got, err := sysmreplay.SocketsFrom(row)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

// rtTopology builds a two-package tree with several children per level, so the
// round trip exercises sibling ordering rather than a single spine.
func rtTopology() *sysmsnap.Topology {
	pu := func(idx int32, gov string) *sysmsnap.TopoObject {
		return &sysmsnap.TopoObject{
			Kind: sysmsnap.TopoKindPU, OSIndex: idx,
			FreqPolicy: &sysmsnap.FreqPolicy{MinMHz: 400, MaxMHz: 5000, Governor: gov, Driver: "amd-pstate"},
		}
	}
	core := func(idx, pu0, pu1 int32) *sysmsnap.TopoObject {
		l1d := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindCache, OSIndex: -1,
			CacheLevel: 1, CacheType: sysmsnap.CacheTypeData, CacheSizeBytes: 32 << 10}
		l1i := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindCache, OSIndex: -1,
			CacheLevel: 1, CacheType: sysmsnap.CacheTypeInstruction, CacheSizeBytes: 32 << 10,
			Children: []*sysmsnap.TopoObject{pu(pu0, "schedutil"), pu(pu1, "performance")}}
		return &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindCore, OSIndex: idx,
			Children: []*sysmsnap.TopoObject{l1d, l1i}}
	}
	l3 := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindCache, OSIndex: -1,
		CacheLevel: 3, CacheType: sysmsnap.CacheTypeUnified, CacheSizeBytes: 32 << 20,
		Children: []*sysmsnap.TopoObject{core(0, 0, 1), core(1, 2, 3)}}
	numa := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindNUMANode, OSIndex: 0, MemBytes: 64 << 30,
		Children: []*sysmsnap.TopoObject{l3}}
	pkg := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindPackage, OSIndex: 0,
		Children: []*sysmsnap.TopoObject{numa}}
	root := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindMachine, OSIndex: -1,
		Children: []*sysmsnap.TopoObject{pkg}}
	return &sysmsnap.Topology{Root: root, LogicalCount: 4}
}

func TestRoundTrip_Topology(t *testing.T) {
	in := rtTopology()
	row, err := topologyRow(rtHost, in, rtTs)
	require.NoError(t, err)

	got, err := sysmreplay.TopologyFrom(row)
	require.NoError(t, err)
	assert.Equal(t, in, got, "the tree must come back node for node, in child order")
}

// TestRoundTrip_Topology_CacheTypeOnlyOnCaches pins the writer's kind gate: the
// enum's zero value is Unified, so a non-cache node storing a label would read
// back as a unified cache. The empty label is what keeps them apart, and it
// must not resurrect as a cache type on a Core or PU.
func TestRoundTrip_Topology_CacheTypeOnlyOnCaches(t *testing.T) {
	in := rtTopology()
	row, err := topologyRow(rtHost, in, rtTs)
	require.NoError(t, err)
	for i, kind := range row.NodeKind {
		if kind == "Cache" {
			assert.NotEmpty(t, row.CacheType[i], "cache node %d must carry a type", i)
			continue
		}
		assert.Empty(t, row.CacheType[i], "non-cache node %d (%s) must carry no type", i, kind)
	}
}
