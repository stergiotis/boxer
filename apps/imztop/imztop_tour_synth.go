package imztop

import (
	"context"
	"math"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// tourSampler is the screenshot tour's metric source: it implements
// sysmetricsbus.BundleSampler with SYNTHETIC, live-looking data instead of a
// real /proc scrape. Feeding the consumer this way keeps package imztop free of
// any collector import (ADR-0090 SD6 — fully closed: even the capture harness no
// longer reaches the collectors), while still exercising every panel. The
// bundle varies slightly per tick so the line plots and per-core grid animate.
type tourSampler struct {
	n    int64              // tick counter driving the animated variation
	topo *sysmsnap.Topology // static synthetic topology, built once
}

func newTourSampler() *tourSampler {
	return &tourSampler{topo: synthTopology()}
}

// Close satisfies sysmetricsbus.BundleSampler; the synthetic source owns nothing.
func (s *tourSampler) Close() (err error) { return nil }

// Sample returns the next synthetic bundle. The producer calls it on a single
// goroutine, so the unsynchronised counter is safe.
func (s *tourSampler) Sample(_ context.Context) (snap sysmsnap.BundleSnapshot, err error) {
	s.n++
	snap = synthBundle(s.n)
	snap.Topology = s.topo // same pointer every tick, as a real scraper stamps it
	return
}

const synthCores = 8

// synthTopology builds a static 1-package / 1-NUMA / 4-core / 8-thread tree
// once, mirroring the shape [cpu.ReadTopology] would produce so the topology
// panel's treemap renders a believable hierarchy.
func synthTopology() (topo *sysmsnap.Topology) {
	pu := func(idx int32) *sysmsnap.TopoObject {
		return &sysmsnap.TopoObject{
			Kind: sysmsnap.TopoKindPU, OSIndex: idx,
			FreqPolicy: &sysmsnap.FreqPolicy{MinMHz: 800, MaxMHz: 4800, Governor: "schedutil", Driver: "synthetic"},
		}
	}
	core := func(idx, pu0, pu1 int32) *sysmsnap.TopoObject {
		l2 := &sysmsnap.TopoObject{
			Kind: sysmsnap.TopoKindCache, OSIndex: -1,
			CacheLevel: 2, CacheType: sysmsnap.CacheTypeUnified, CacheSizeBytes: 1 << 20,
			Children: []*sysmsnap.TopoObject{pu(pu0), pu(pu1)},
		}
		return &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindCore, OSIndex: idx, Children: []*sysmsnap.TopoObject{l2}}
	}
	l3 := &sysmsnap.TopoObject{
		Kind: sysmsnap.TopoKindCache, OSIndex: -1,
		CacheLevel: 3, CacheType: sysmsnap.CacheTypeUnified, CacheSizeBytes: 16 << 20,
		Children: []*sysmsnap.TopoObject{core(0, 0, 1), core(1, 2, 3), core(2, 4, 5), core(3, 6, 7)},
	}
	numa := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindNUMANode, OSIndex: 0, MemBytes: 16 << 30, Children: []*sysmsnap.TopoObject{l3}}
	pkg := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindPackage, OSIndex: 0, Children: []*sysmsnap.TopoObject{numa}}
	root := &sysmsnap.TopoObject{Kind: sysmsnap.TopoKindMachine, OSIndex: -1, Children: []*sysmsnap.TopoObject{pkg}}
	return &sysmsnap.Topology{Root: root, LogicalCount: synthCores}
}

// synthProc is a fixed process-table template; only CPU%/RSS vary per tick so
// the table is stable (and the per-PID EWMA keeps its key). Some carry the
// "imzero2" substring so the tour's filtered scene is non-empty.
type synthProc struct {
	pid, ppid  uint32
	name, cmd  string
	user       string
	baseCPU    float32
	baseRSSMiB uint64
}

var synthProcs = []synthProc{
	{1, 0, "systemd", "/sbin/init", "root", 0.1, 14},
	{842, 1, "imzero2", "imzero2 demo --launch widgets --clientType egui", "boxer", 0, 312},
	{843, 842, "main_go", "./main_go imzero2 demo --clientBinary ./imzero2", "boxer", 0, 196},
	{1190, 1, "wezterm-gui", "wezterm-gui", "boxer", 0, 240},
	{1455, 1190, "go", "go test -tags ... ./...", "boxer", 0, 88},
	{1556, 1, "clickhouse-loc", "clickhouse-local --query ...", "boxer", 0, 420},
	{1789, 1, "pipewire", "/usr/bin/pipewire", "boxer", 0.3, 22},
	{2003, 1, "systemd-resolve", "/usr/lib/systemd/systemd-resolved", "systemd-resolve", 0.0, 18},
}

func synthBundle(n int64) (snap sysmsnap.BundleSnapshot) {
	now := time.Now().UnixMilli()
	snap.SampledAtUnixMs = now
	snap.Errors = map[sysmsnap.Domain]error{}

	// CPU — per-core busy oscillates with a per-core phase; freq tracks load.
	perCore := make([]uint8, synthCores)
	freq := make([]uint32, synthCores)
	var sum int
	for i := 0; i < synthCores; i++ {
		busy := clampPct(45 + 38*math.Sin(float64(n)/7+float64(i)*0.8))
		perCore[i] = uint8(busy)
		sum += int(busy)
		freq[i] = uint32(2200 + 24*busy) // ~2.2–4.6 GHz
	}
	snap.CPU = &sysmsnap.CPUSnapshot{
		SampledAtUnixMs: now,
		TotalPercent:    uint8(sum / synthCores),
		PerCorePercent:  perCore,
		PerCoreFreqMHz:  freq,
		LoadAvg1:        2.1, LoadAvg5: 1.7, LoadAvg15: 1.4,
		UsageWatts:          float32(osc(22, 18, n, 8, 0)),
		UsageWattsAvailable: true,
		ModelName:           "Synthetic 8-thread CPU (imztop tour)",
		LogicalCores:        synthCores,
	}

	// Mem — ~9–11 GiB used of 16 GiB.
	const totalMem = uint64(16) << 30
	used := uint64(osc(9<<30, 2<<30, n, 11, 0))
	avail := totalMem - used
	snap.Mem = &sysmsnap.MemSnapshot{
		SampledAtUnixMs: now,
		TotalBytes:      totalMem,
		FreeBytes:       avail / 2,
		AvailableBytes:  avail,
		BuffersBytes:    512 << 20,
		CachedBytes:     3 << 30,
		SwapTotalBytes:  8 << 30,
		SwapFreeBytes:   uint64(7) << 30,
		UsedBytes:       used,
		SwapUsedBytes:   1 << 30,
	}

	// Disk — two block devices with oscillating throughput.
	snap.Disk = &sysmsnap.DiskSnapshot{
		SampledAtUnixMs: now,
		BlockDevices: []sysmsnap.BlockDevice{
			{Name: "nvme0n1", ReadBytesPerSec: uint64(osc(8<<20, 90<<20, n, 6, 0)), WriteBytesPerSec: uint64(osc(4<<20, 50<<20, n, 9, 1)), BusyPercent: uint8(clampPct(osc(10, 70, n, 6, 0)))},
			{Name: "nvme0n1p2", ReadBytesPerSec: uint64(osc(1<<20, 12<<20, n, 7, 2)), WriteBytesPerSec: uint64(osc(2<<20, 20<<20, n, 8, 0)), BusyPercent: uint8(clampPct(osc(5, 35, n, 7, 2)))},
		},
	}

	// Net — loopback excluded, one ethernet with rates + addresses.
	snap.Net = &sysmsnap.NetSnapshot{
		SampledAtUnixMs: now,
		Interfaces: []sysmsnap.NetInterface{
			{
				Name: "enp5s0", Index: 2, HardwareAddr: "de:ad:be:ef:00:21", Up: true, Running: true,
				IPv4: []string{"10.0.0.42"}, IPv6: []string{"fe80::dead:beef:0:21"},
				RxBytes: uint64(n) * (3 << 20), TxBytes: uint64(n) * (1 << 20),
				RxBytesPerSec: uint64(osc(256<<10, 6<<20, n, 5, 0)), TxBytesPerSec: uint64(osc(64<<10, 2<<20, n, 6, 1)),
			},
			{
				Name: "wg0", Index: 4, Up: true, Running: true,
				IPv4:          []string{"10.8.0.3"},
				RxBytesPerSec: uint64(osc(32<<10, 512<<10, n, 9, 2)), TxBytesPerSec: uint64(osc(16<<10, 256<<10, n, 8, 0)),
			},
		},
	}

	// Battery — discharging slowly, with the AC adapter offline.
	pct := uint8(clampPct(72 + 6*math.Sin(float64(n)/40)))
	snap.Battery = &sysmsnap.BatterySnapshot{
		SampledAtUnixMs: now,
		Batteries: []sysmsnap.BatteryStatus{
			{Name: "BAT0", Type: "Battery", Percent: pct, State: sysmsnap.StateDischarging, PowerWatts: float32(osc(8, 6, n, 8, 0)), SecondsToFull: -1, SecondsToEmpty: 9000},
		},
		ACAdapters: []sysmsnap.ACAdapter{{Name: "AC", Online: false}},
	}

	// GPU — one device, busy and power tracking each other.
	gbusy := uint8(clampPct(osc(20, 60, n, 6, 1)))
	snap.GPU = &sysmsnap.GPUSnapshot{
		SampledAtUnixMs: now,
		Devices: []sysmsnap.GPUDevice{
			{
				Vendor: "amd", Index: 0, Name: "Synthetic GPU (tour)", PCIID: "0x744c",
				BusyPercent: gbusy, MemoryUsedBytes: uint64(osc(1<<30, 3<<30, n, 10, 0)), MemoryTotalBytes: 16 << 30,
				PowerWatts: float32(30 + float64(gbusy)), TempC: float32(osc(42, 28, n, 6, 1)), FreqMHz: uint32(800 + 18*float64(gbusy)),
			},
		},
	}

	// PSI — light CPU pressure, near-zero IO/mem.
	snap.PSI = &sysmsnap.PSISnapshot{
		SampledAtUnixMs: now,
		Available:       true,
		CPU:             sysmsnap.PSIResource{Some: sysmsnap.PSIPressure{Avg10: float32(osc(2, 12, n, 6, 0)), Avg60: 3.1, Avg300: 2.4, TotalUs: uint64(n) * 40000}},
		Memory:          sysmsnap.PSIResource{Some: sysmsnap.PSIPressure{Avg10: 0.2, Avg60: 0.1}},
		IO:              sysmsnap.PSIResource{Some: sysmsnap.PSIPressure{Avg10: float32(osc(0.5, 6, n, 5, 2)), Avg60: 1.2}},
	}

	// Container — a bare-metal host (no runtime).
	snap.Container = &sysmsnap.ContainerInfo{Engine: sysmsnap.EngineNone}

	// Sensors — package + per-core temps tracking CPU load.
	pkgTemp := float32(40 + float64(snap.CPU.TotalPercent)*0.35)
	snap.Sensors = []sysmsnap.TempReading{
		{Name: "k10temp/Tctl", Path: "synthetic", TempC: pkgTemp, CriticalC: 95, KindCPUPackage: true},
		{Name: "k10temp/Tccd1", Path: "synthetic", TempC: pkgTemp - 4, CriticalC: 95, KindCPUCore: true},
		{Name: "nvme/Composite", Path: "synthetic", TempC: float32(osc(38, 10, n, 12, 0)), CriticalC: 84},
	}

	// Procs — stable set; CPU%/RSS vary per tick, the busiest tracking core 0.
	procs := make([]sysmsnap.ProcInfo, len(synthProcs))
	for i, p := range synthProcs {
		cpu := p.baseCPU + float32(osc(0, float64(perCore[i%synthCores])*0.6, n, 4, float64(i)))
		procs[i] = sysmsnap.ProcInfo{
			PID: p.pid, PPID: p.ppid, Name: p.name, Cmd: p.cmd, User: p.user, State: 'S',
			StartedAtUnixMs: 1_700_000_000_000 + int64(p.pid)*1000, // fixed per PID (EWMA key stability)
			CPUPercent:      cpu,
			RSSBytes:        (p.baseRSSMiB << 20) + uint64(osc(0, 16<<20, n, 7, float64(i))),
			VMSizeBytes:     (p.baseRSSMiB << 20) * 3,
			NumThreads:      4,
		}
	}
	snap.Procs = procs
	return
}

// clampPct clamps a percentage to [0,100].
func clampPct(v float64) (out float64) {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// osc returns base + amp·(0..1) following a sine of the tick counter, giving
// each synthetic series a smooth, animated wander.
func osc(base, amp float64, n int64, period, phase float64) (out float64) {
	return base + amp*0.5*(1+math.Sin(float64(n)/period+phase))
}
