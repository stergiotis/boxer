//go:build llm_generated_opus47

package imztop

import (
	"context"
	"math"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/battery"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/container"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/cpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/disk"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/mem"
	netcoll "github.com/stergiotis/boxer/public/observability/sysmetrics/net"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sensors"
)

// procSampleCap bounds the number of processes fully sampled per tick.
// /proc/[pid]/{cmdline,status} are read only for the top-N by CPU% (RSS
// desc tiebreak); a desktop with ~600 PIDs would otherwise burst ~40 MB
// of short-lived bytes.growSlice garbage per second, exceeding the
// GOGC=100 trigger every tick and yanking the render goroutine into
// mark-assist. 256 still exceeds what the proc panel surfaces in
// practice on the screens this widget targets.
const procSampleCap = 256

// PublishedSnapshot is the read-only frame the renderer consumes. Built
// once per Sampler tick and replaced atomically; slices are owned by
// the snapshot and never mutated after publication, so concurrent
// readers see a coherent view.
//
// All byte/sec history fields (disk/net) are stored in MiB/s so the
// renderer can label plot axes "MiB/s" without per-frame scaling.
// Sub-MiB/s rates appear as small fractional values; raw counters at
// the byte level remain available on the Latest* fields.
type PublishedSnapshot struct {
	SampledAtUnixMs int64

	HistoryTimeUnixSec []float64
	HistoryCPUTotal    []float64
	HistoryMemUsed     []float64
	HistoryDiskRead    []float64 // MiB/s (sum across block devices)
	HistoryDiskWrite   []float64 // MiB/s
	HistoryNetRx       []float64 // MiB/s (sum across interfaces)
	HistoryNetTx       []float64 // MiB/s
	HistoryBatteryPct  []float64

	HistoryCPUPerCore     [][]float64   // [core][time] percent
	HistoryGPUBusyPerDev  [][]float64   // [device][time] percent
	HistoryDiskReadByDev  []NamedSeries // MiB/s per block device, ordered by name
	HistoryDiskWriteByDev []NamedSeries // MiB/s per block device
	HistoryNetRxByIface   []NamedSeries // MiB/s per interface
	HistoryNetTxByIface   []NamedSeries // MiB/s per interface

	LatestCPU       *cpu.Snapshot
	LatestMem       *mem.Snapshot
	LatestDisk      *disk.Snapshot
	LatestNet       *netcoll.Snapshot
	LatestBattery   *battery.Snapshot
	LatestGPU       *gpu.Snapshot
	LatestContainer *container.Info
	Sensors         []sensors.TempReading
	Procs           []proc.Info

	// ProcCPUSmoothed is the per-process EWMA-smoothed CPU% (α=
	// procCPUEWMAAlpha), parallel to Procs by index. Drives the
	// process-table sort key and the CPU%-cell background tint;
	// the raw Procs[i].CPUPercent stays untouched so the displayed
	// value still reflects the latest sampler-interval average.
	// Smoothing is per-PID and persists across ticks via the
	// Sampler's procCPUEWMA map (evicted when a PID disappears).
	ProcCPUSmoothed []float32

	Errors map[sysmetrics.Domain]error
}

// SamplerOptions configures a Sampler.
type SamplerOptions struct {
	UpdateInterval time.Duration
	HistoryWindow  time.Duration
}

// SamplerI is the public surface a Sampler implements.
type SamplerI interface {
	Start(ctx context.Context)
	Latest() (snap *PublishedSnapshot)
	Pause(p bool)
	IsPaused() (p bool)
	Close() (err error)
}

// Sampler runs a goroutine that periodically calls Bundle.Sample and
// publishes a PublishedSnapshot via atomic.Pointer.
type Sampler struct {
	bundle *sysmetrics.Bundle

	intervalNs atomic.Int64
	histN      int32

	timeWindow *SlidingWindow[float64]
	cpuTotal   *SlidingWindow[float64]
	memUsed    *SlidingWindow[float64]
	diskRead   *SlidingWindow[float64]
	diskWrite  *SlidingWindow[float64]
	netRx      *SlidingWindow[float64]
	netTx      *SlidingWindow[float64]
	batteryPct *SlidingWindow[float64]

	cpuPerCore *indexedWindowSet
	gpuBusy    *indexedWindowSet
	diskReadBy *namedWindowSet
	diskWriBy  *namedWindowSet
	netRxBy    *namedWindowSet
	netTxBy    *namedWindowSet

	// procCPUEWMA tracks per-process exponentially smoothed CPU% so
	// the process table's sort order and CPU%-cell tint don't
	// ping-pong when a process briefly spikes. Keyed on (PID,
	// StartedAtUnixMs) rather than PID alone — Linux PID space wraps
	// (typically under 2^22) and a freshly-spawned process inheriting
	// a recently-dead PID would otherwise inherit the dead process's
	// smoothed value. Only owned by tick(); the sampler goroutine is
	// the sole writer.
	procCPUEWMA map[procEWMAKey]float32

	latest atomic.Pointer[PublishedSnapshot]
	paused atomic.Bool

	cancel context.CancelFunc
	done   chan struct{}
}

var _ SamplerI = (*Sampler)(nil)

// NewSampler builds a Sampler with the M2 collector set: CPU + Mem +
// Disk + Net + Battery + Sensors plus optional GPU when a vendor build
// tag is enabled (gpu_rocm wires AMD; gpu_intel and gpu_nvml are not
// baked into the default ADR-0020 build).
func NewSampler(opts SamplerOptions) (inst *Sampler, err error) {
	if opts.UpdateInterval <= 0 {
		opts.UpdateInterval = 1 * time.Second
	}
	if opts.HistoryWindow <= 0 {
		opts.HistoryWindow = 10 * time.Minute
	}

	histN := max(int32(opts.HistoryWindow/opts.UpdateInterval), 2)

	bopts := sysmetrics.BundleOptions{}

	cpuC, cerr := cpu.New(cpu.Options{})
	if cerr != nil {
		err = eh.Errorf("imztop: build cpu collector: %w", cerr)
		return
	}
	bopts.CPU = cpuC

	memC, merr := mem.New(mem.Options{})
	if merr != nil {
		err = eh.Errorf("imztop: build mem collector: %w", merr)
		return
	}
	bopts.Mem = memC

	diskC, derr := disk.New(disk.Options{})
	if derr != nil {
		log.Warn().Err(derr).Msg("imztop: build disk collector failed; disk panel disabled")
	} else {
		bopts.Disk = diskC
	}

	netC, nerr := netcoll.New(netcoll.Options{})
	if nerr != nil {
		log.Warn().Err(nerr).Msg("imztop: build net collector failed; net panel disabled")
	} else {
		bopts.Net = netC
	}

	batC, berr := battery.New(battery.Options{})
	if berr != nil {
		log.Warn().Err(berr).Msg("imztop: build battery collector failed; battery panel disabled")
	} else {
		bopts.Battery = batC
	}

	senC, serr := sensors.New(sensors.Options{})
	if serr != nil {
		log.Warn().Err(serr).Msg("imztop: build sensors collector failed; sensors panel disabled")
	} else {
		bopts.Sensors = senC
	}

	procC, perr := proc.New(proc.Options{MaxProcs: procSampleCap})
	if perr != nil {
		log.Warn().Err(perr).Msg("imztop: build proc collector failed; process panel disabled")
	} else {
		bopts.Proc = procC
	}

	cntC, cerr2 := container.New(container.Options{})
	if cerr2 != nil {
		log.Warn().Err(cerr2).Msg("imztop: build container detector failed; container badge disabled")
	} else {
		bopts.Container = cntC
	}

	wireGPUSampler(&bopts)

	bundle, berr2 := sysmetrics.NewBundle(bopts)
	if berr2 != nil {
		err = eh.Errorf("imztop: build bundle: %w", berr2)
		return
	}

	inst = &Sampler{
		bundle:      bundle,
		histN:       histN,
		timeWindow:  NewSlidingWindow[float64](histN),
		cpuTotal:    NewSlidingWindow[float64](histN),
		memUsed:     NewSlidingWindow[float64](histN),
		diskRead:    NewSlidingWindow[float64](histN),
		diskWrite:   NewSlidingWindow[float64](histN),
		netRx:       NewSlidingWindow[float64](histN),
		netTx:       NewSlidingWindow[float64](histN),
		batteryPct:  NewSlidingWindow[float64](histN),
		cpuPerCore:  newIndexedWindowSet(histN),
		gpuBusy:     newIndexedWindowSet(histN),
		diskReadBy:  newNamedWindowSet(histN),
		diskWriBy:   newNamedWindowSet(histN),
		netRxBy:     newNamedWindowSet(histN),
		netTxBy:     newNamedWindowSet(histN),
		procCPUEWMA: make(map[procEWMAKey]float32),
	}
	inst.intervalNs.Store(int64(opts.UpdateInterval))
	return
}

func (inst *Sampler) Start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	inst.cancel = cancel
	inst.done = make(chan struct{})
	go inst.loop(runCtx)
}

func (inst *Sampler) Latest() (snap *PublishedSnapshot) {
	snap = inst.latest.Load()
	return
}

func (inst *Sampler) Pause(p bool) {
	inst.paused.Store(p)
}

func (inst *Sampler) IsPaused() (p bool) {
	p = inst.paused.Load()
	return
}

// IntervalLabel returns the configured tick interval as a short
// human-readable label suitable for the top-bar status row.
func (inst *Sampler) IntervalLabel() (out string) {
	out = time.Duration(inst.intervalNs.Load()).String()
	return
}

// SetInterval changes the sampler tick period. Clamped to the
// [100ms, 60s] range; the next tick after the current ticker fires
// uses the new period.
func (inst *Sampler) SetInterval(d time.Duration) {
	if d < 100*time.Millisecond {
		d = 100 * time.Millisecond
	}
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	inst.intervalNs.Store(int64(d))
}

// Interval returns the current tick period.
func (inst *Sampler) Interval() (d time.Duration) {
	d = time.Duration(inst.intervalNs.Load())
	return
}

func (inst *Sampler) Close() (err error) {
	if inst.cancel != nil {
		inst.cancel()
	}
	if inst.done != nil {
		<-inst.done
	}
	err = inst.bundle.Close()
	return
}

func (inst *Sampler) loop(ctx context.Context) {
	defer close(inst.done)

	inst.tick(ctx)

	cur := time.Duration(inst.intervalNs.Load())
	ticker := time.NewTicker(cur)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			inst.tick(ctx)
			next := time.Duration(inst.intervalNs.Load())
			if next != cur {
				ticker.Reset(next)
				cur = next
			}
		}
	}
}

func (inst *Sampler) tick(ctx context.Context) {
	// Pause must short-circuit before Sample. The collectors walk
	// /proc, /sys, sysfs, etc.; running them under a "paused" sampler
	// burns CPU the user just asked us to stop spending. The freeze
	// semantics in ADR-0020 SD14 are about the published snapshot, but
	// nothing is gained by also continuing to sample.
	if inst.paused.Load() {
		return
	}
	bundleSnap, err := inst.bundle.Sample(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Warn().Err(err).Msg("imztop: bundle sample returned error")
		}
		return
	}

	inst.timeWindow.Push(float64(bundleSnap.SampledAtUnixMs) / 1000.0)
	if bundleSnap.CPU != nil {
		inst.cpuTotal.Push(float64(bundleSnap.CPU.TotalPercent))
		perCore := make([]float64, len(bundleSnap.CPU.PerCorePercent))
		for i, p := range bundleSnap.CPU.PerCorePercent {
			perCore[i] = float64(p)
		}
		inst.cpuPerCore.push(perCore)
	} else {
		inst.cpuPerCore.push(nil)
	}
	if bundleSnap.Mem != nil {
		inst.memUsed.Push(memUsedPercent(bundleSnap.Mem))
	}
	if bundleSnap.Disk != nil {
		readMiB, writeMiB := sumDiskIOMiB(bundleSnap.Disk)
		inst.diskRead.Push(readMiB)
		inst.diskWrite.Push(writeMiB)
		inst.diskReadBy.push(diskRatesByDevice(bundleSnap.Disk, true))
		inst.diskWriBy.push(diskRatesByDevice(bundleSnap.Disk, false))
	} else {
		inst.diskReadBy.push(nil)
		inst.diskWriBy.push(nil)
	}
	if bundleSnap.Net != nil {
		rxMiB, txMiB := sumNetIOMiB(bundleSnap.Net)
		inst.netRx.Push(rxMiB)
		inst.netTx.Push(txMiB)
		inst.netRxBy.push(netRatesByIface(bundleSnap.Net, true))
		inst.netTxBy.push(netRatesByIface(bundleSnap.Net, false))
	} else {
		inst.netRxBy.push(nil)
		inst.netTxBy.push(nil)
	}
	if bundleSnap.Battery != nil && len(bundleSnap.Battery.Batteries) > 0 {
		inst.batteryPct.Push(float64(bundleSnap.Battery.Batteries[0].Percent))
	}
	if bundleSnap.GPU != nil && len(bundleSnap.GPU.Devices) > 0 {
		busy := make([]float64, len(bundleSnap.GPU.Devices))
		for i, d := range bundleSnap.GPU.Devices {
			busy[i] = float64(d.BusyPercent)
		}
		inst.gpuBusy.push(busy)
	} else {
		inst.gpuBusy.push(nil)
	}

	procs := bundleSnap.Procs
	smoothed := inst.updateProcCPUEWMA(procs)
	if len(procs) > 0 {
		procs, smoothed = applyProcView(procs, smoothed, loadProcView())
	}

	pub := &PublishedSnapshot{
		SampledAtUnixMs:       bundleSnap.SampledAtUnixMs,
		HistoryTimeUnixSec:    copyFloats(inst.timeWindow.Values()),
		HistoryCPUTotal:       copyFloats(inst.cpuTotal.Values()),
		HistoryMemUsed:        copyFloats(inst.memUsed.Values()),
		HistoryDiskRead:       copyFloats(inst.diskRead.Values()),
		HistoryDiskWrite:      copyFloats(inst.diskWrite.Values()),
		HistoryNetRx:          copyFloats(inst.netRx.Values()),
		HistoryNetTx:          copyFloats(inst.netTx.Values()),
		HistoryBatteryPct:     copyFloats(inst.batteryPct.Values()),
		HistoryCPUPerCore:     inst.cpuPerCore.snapshot(),
		HistoryGPUBusyPerDev:  inst.gpuBusy.snapshot(),
		HistoryDiskReadByDev:  inst.diskReadBy.snapshot(),
		HistoryDiskWriteByDev: inst.diskWriBy.snapshot(),
		HistoryNetRxByIface:   inst.netRxBy.snapshot(),
		HistoryNetTxByIface:   inst.netTxBy.snapshot(),
		LatestCPU:             bundleSnap.CPU,
		LatestMem:             bundleSnap.Mem,
		LatestDisk:            bundleSnap.Disk,
		LatestNet:             bundleSnap.Net,
		LatestBattery:         bundleSnap.Battery,
		LatestGPU:             bundleSnap.GPU,
		LatestContainer:       bundleSnap.Container,
		Sensors:               bundleSnap.Sensors,
		Procs:                 procs,
		ProcCPUSmoothed:       smoothed,
		Errors:                bundleSnap.Errors,
	}
	inst.latest.Store(pub)
}

// procEWMAKey identifies a process for EWMA bookkeeping. PID alone
// is not enough: Linux's PID space wraps (typically below 2^22) and
// a fresh process landing on a recently-dead PID would otherwise
// inherit the dead one's smoothed value. The kernel-reported
// process start time disambiguates — two processes with the same
// PID across a wrap cannot also share an identical sub-millisecond
// start time. Falls back gracefully when StartedAt is 0 (collector
// failure for that PID): such rows collapse to PID-only keying and
// the EWMA degrades to the pre-fix behaviour for that one entry
// instead of corrupting the rest of the table.
type procEWMAKey struct {
	PID       uint32
	StartedAt int64
}

// procCPUEWMATau is the EWMA *time constant* governing per-process
// CPU% smoothing. Each tick we compute the per-sample weight as
//
//	α = 1 − exp(−Δt / τ)
//
// from the sampler's current interval Δt, so the smoothing's
// real-world responsiveness stays fixed regardless of how often the
// sampler ticks. A step from 0 → 100 % reaches ~63 % of its new
// value after τ wall-clock seconds at any cadence; the previous
// constant-α implementation honoured that only at the default 1 Hz
// rate and turned into a near-no-op under tour mode's 10 Hz cadence.
//
// 1.5 s was picked to reproduce the previously-validated 1-Hz
// behaviour (α≈0.487 at Δt=1 s ≈ the old hard-coded 0.5) — felt
// "right" against real process traces during interactive
// verification; lower τ smooths less / reacts faster, higher τ
// smooths more / lags more.
const procCPUEWMATau = 1500 * time.Millisecond

// updateProcCPUEWMA folds the current sample's per-process CPU% into
// the per-PID EWMA state and returns the smoothed values aligned to
// procs by index. PIDs that disappear between ticks fall out of the
// state map automatically — we rebuild it each tick from the current
// procs slice rather than mark-and-sweep, so the map is bounded by
// the live process count and dead PIDs cannot leak memory.
//
// The per-sample weight α is derived from procCPUEWMATau and the
// sampler's current Interval(); inst.Interval() is the configured
// cadence rather than the wall-clock gap, which is good enough — the
// configured period is what the sampler is *aiming* for, and brief
// scheduling jitter doesn't change the smoothing's intended time
// constant materially.
func (inst *Sampler) updateProcCPUEWMA(procs []proc.Info) (smoothed []float32) {
	n := len(procs)
	smoothed = make([]float32, n)
	next := make(map[procEWMAKey]float32, n)
	dt := inst.Interval()
	alpha := float32(1.0 - math.Exp(-float64(dt)/float64(procCPUEWMATau)))
	for i := range procs {
		key := procEWMAKey{PID: procs[i].PID, StartedAt: procs[i].StartedAtUnixMs}
		raw := procs[i].CPUPercent
		var s float32
		if prev, ok := inst.procCPUEWMA[key]; ok {
			s = alpha*raw + (1-alpha)*prev
		} else {
			// First sighting — seed from the raw value so a freshly-
			// spawned heavy process appears at the top of the sort
			// order immediately rather than ramping in over several
			// ticks from zero. Also handles the PID-reuse path: a
			// reused PID with a different StartedAt is a brand-new
			// key, so the smoothed state cannot leak.
			s = raw
		}
		smoothed[i] = s
		next[key] = s
	}
	inst.procCPUEWMA = next
	return
}

func memUsedPercent(snap *mem.Snapshot) (pct float64) {
	if snap.TotalBytes == 0 {
		return
	}
	pct = float64(snap.UsedBytes) * 100 / float64(snap.TotalBytes)
	return
}

const bytesPerMiB = 1024 * 1024

func sumDiskIOMiB(snap *disk.Snapshot) (readMiB, writeMiB float64) {
	var read, write uint64
	for _, d := range snap.BlockDevices {
		read += d.ReadBytesPerSec
		write += d.WriteBytesPerSec
	}
	readMiB = float64(read) / bytesPerMiB
	writeMiB = float64(write) / bytesPerMiB
	return
}

func sumNetIOMiB(snap *netcoll.Snapshot) (rxMiB, txMiB float64) {
	var rx, tx uint64
	for _, ifc := range snap.Interfaces {
		rx += ifc.RxBytesPerSec
		tx += ifc.TxBytesPerSec
	}
	rxMiB = float64(rx) / bytesPerMiB
	txMiB = float64(tx) / bytesPerMiB
	return
}

// diskRatesByDevice projects per-block-device rates into NamedValue
// pairs for the named ring set. `read` selects the read-rate axis;
// the write-rate axis flows through the same helper.
func diskRatesByDevice(snap *disk.Snapshot, read bool) (out []NamedValue) {
	out = make([]NamedValue, 0, len(snap.BlockDevices))
	for _, d := range snap.BlockDevices {
		var rate uint64
		if read {
			rate = d.ReadBytesPerSec
		} else {
			rate = d.WriteBytesPerSec
		}
		out = append(out, NamedValue{Name: d.Name, Value: float64(rate) / bytesPerMiB})
	}
	return
}

// netRatesByIface mirrors diskRatesByDevice for network interfaces.
func netRatesByIface(snap *netcoll.Snapshot, rx bool) (out []NamedValue) {
	out = make([]NamedValue, 0, len(snap.Interfaces))
	for _, ifc := range snap.Interfaces {
		var rate uint64
		if rx {
			rate = ifc.RxBytesPerSec
		} else {
			rate = ifc.TxBytesPerSec
		}
		out = append(out, NamedValue{Name: ifc.Name, Value: float64(rate) / bytesPerMiB})
	}
	return
}

func copyFloats(src []float64) (out []float64) {
	out = make([]float64, len(src))
	copy(out, src)
	return
}
