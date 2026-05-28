//go:build llm_generated_opus47

package cpu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

const (
	// RAPLEnergyPath is the sysfs-relative path to the Intel RAPL package
	// energy counter. Reading requires root on kernels with
	// CVE-2020-8694 mitigations applied (most modern distros).
	RAPLEnergyPath = "class/powercap/intel-rapl:0/energy_uj"

	// CGroupCPUSetPath is the cgroup v2 effective cpuset under sysfs.
	CGroupCPUSetPath = "fs/cgroup/cpuset.cpus.effective"
)

// Snapshot is a single sample of CPU state.
type Snapshot struct {
	SampledAtUnixMs int64

	// TotalPercent is total CPU busy percentage [0..100], rounded.
	// First Sample after [New] returns 0.
	TotalPercent uint8
	// PerCorePercent is per-logical-CPU busy percentage, in /proc/stat
	// order. First Sample returns all zeros.
	PerCorePercent []uint8

	// PerCoreFreqMHz is the current frequency of each logical CPU in MHz,
	// 0 when cpufreq is not available for that core.
	PerCoreFreqMHz []uint32

	// LoadAvgN are the kernel-reported 1/5/15-minute load averages.
	LoadAvg1, LoadAvg5, LoadAvg15 float32

	// UsageWatts is the average package power over the most recent sample
	// interval, derived from the Intel RAPL energy_uj counter. Valid only
	// when UsageWattsAvailable is true.
	UsageWatts          float32
	UsageWattsAvailable bool

	// ActiveCPUs is the cgroup v2 effective cpuset (cpuset.cpus.effective)
	// or nil when the cgroup file is absent. Indices are logical CPU ids.
	ActiveCPUs []int32

	// ModelName is the /proc/cpuinfo "model name" field, copied at New().
	ModelName string

	// LogicalCores is the number of logical CPUs detected at New().
	LogicalCores int32
}

// CollectorI is the public surface a CPU sampler implements.
type CollectorI interface {
	Sample(ctx context.Context) (snap Snapshot, err error)
}

// Options configures a [Collector]. Disable* flags default to false (the
// feature enabled). The zero Options{} is the recommended default.
//
// CPU temperatures are intentionally NOT included here — wire a separate
// [sensors.Collector] (or use [sysmetrics.Bundle]) when you need them.
type Options struct {
	Proc *procfs.Reader
	Sys  *sysfs.Reader

	// NowFunc, when non-nil, overrides the wall+monotonic clock.
	NowFunc func() time.Time

	// DisableFreq turns off cpufreq sampling.
	DisableFreq bool
	// DisableRAPL turns off Intel RAPL package power sampling.
	DisableRAPL bool
}

// Collector samples CPU state.
type Collector struct {
	proc       *procfs.Reader
	sys        *sysfs.Reader
	nowFn      func() time.Time
	enableFreq bool
	enableRAPL bool

	modelName    string
	logicalCores int32
	cpuFreqPaths []string

	primed           bool
	prevTotal        uint64
	prevIdle         uint64
	prevPerCoreTotal []uint64
	prevPerCoreIdle  []uint64

	raplKnown     bool // determined on first read
	raplAvailable bool
	raplPrevAt    time.Time
	raplPrevUJ    uint64
}

// New returns a CPU Collector. Static metadata (model name, logical core
// count, cpufreq path discovery) is read once here.
func New(opts Options) (inst *Collector, err error) {
	if opts.Proc == nil {
		opts.Proc = procfs.New("")
	}
	if opts.Sys == nil {
		opts.Sys = sysfs.New("")
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}

	inst = &Collector{
		proc:       opts.Proc,
		sys:        opts.Sys,
		nowFn:      opts.NowFunc,
		enableFreq: !opts.DisableFreq,
		enableRAPL: !opts.DisableRAPL,
	}

	err = inst.readStaticMetadata()
	if err != nil {
		err = eh.Errorf("read cpu static metadata: %w", err)
		return
	}
	if inst.enableFreq {
		inst.discoverCPUFreqPaths()
	}
	return
}

var _ CollectorI = (*Collector)(nil)

// Sample reads the live CPU state and returns a Snapshot. The first call
// returns zero percentages and zero watts (no prior tick to delta against).
func (inst *Collector) Sample(ctx context.Context) (snap Snapshot, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}

	now := inst.nowFn()
	snap.SampledAtUnixMs = now.UnixMilli()
	snap.ModelName = inst.modelName
	snap.LogicalCores = inst.logicalCores

	err = inst.sampleStat(&snap)
	if err != nil {
		return
	}
	err = inst.sampleLoadAvg(&snap)
	if err != nil {
		return
	}
	if inst.enableFreq {
		inst.sampleFreq(&snap)
	}
	if inst.enableRAPL {
		inst.sampleRAPL(now, &snap)
	}
	inst.sampleActiveCPUs(&snap)
	return
}

// readStaticMetadata reads /proc/cpuinfo once for model name and the
// logical core count.
func (inst *Collector) readStaticMetadata() (err error) {
	var raw []byte
	raw, err = inst.proc.ReadFile("cpuinfo")
	if err != nil {
		return
	}
	var logical int32
	for k, v := range procfs.IterKV(raw) {
		switch string(k) {
		case "processor":
			logical++
		case "model name":
			if inst.modelName == "" {
				inst.modelName = string(v)
			}
		}
	}
	if logical == 0 {
		err = eb.Build().Errorf("no processor entries in /proc/cpuinfo")
		return
	}
	inst.logicalCores = logical
	return
}

// discoverCPUFreqPaths populates inst.cpuFreqPaths with one entry per
// logical CPU (or "" when policyN/scaling_cur_freq is missing).
func (inst *Collector) discoverCPUFreqPaths() {
	inst.cpuFreqPaths = make([]string, inst.logicalCores)
	for i := int32(0); i < inst.logicalCores; i++ {
		// provenance: btop src/linux/btop_collect.cpp:349
		rel := filepath.Join("devices/system/cpu/cpufreq", fmt.Sprintf("policy%d", i), "scaling_cur_freq")
		if _, statErr := inst.statSysfs(rel); statErr == nil {
			inst.cpuFreqPaths[i] = rel
		}
	}
}

func (inst *Collector) statSysfs(rel string) (name string, err error) {
	_, err = inst.sys.ReadFile(rel)
	if err != nil {
		return
	}
	return rel, nil
}

func (inst *Collector) sampleStat(snap *Snapshot) (err error) {
	var raw []byte
	raw, err = inst.proc.ReadFile("stat")
	if err != nil {
		err = eh.Errorf("read /proc/stat: %w", err)
		return
	}
	total, perCore, parseErr := parseStat(raw)
	if parseErr != nil {
		err = parseErr
		return
	}
	snap.PerCorePercent = make([]uint8, len(perCore))

	totT, totI := total.totalAndIdle()

	if !inst.primed {
		inst.prevTotal = totT
		inst.prevIdle = totI
		inst.prevPerCoreTotal = make([]uint64, len(perCore))
		inst.prevPerCoreIdle = make([]uint64, len(perCore))
		for i, c := range perCore {
			inst.prevPerCoreTotal[i], inst.prevPerCoreIdle[i] = c.totalAndIdle()
		}
		inst.primed = true
		// First Sample yields all-zero percentages by design.
		return
	}

	snap.TotalPercent = pct(totT, totI, inst.prevTotal, inst.prevIdle)
	inst.prevTotal = totT
	inst.prevIdle = totI

	// Resize prior-tick slices if the kernel reports more cores now (rare
	// but possible — CPU hotplug). Provenance: btop:1196-1202.
	if len(perCore) > len(inst.prevPerCoreTotal) {
		inst.prevPerCoreTotal = append(inst.prevPerCoreTotal, make([]uint64, len(perCore)-len(inst.prevPerCoreTotal))...)
		inst.prevPerCoreIdle = append(inst.prevPerCoreIdle, make([]uint64, len(perCore)-len(inst.prevPerCoreIdle))...)
	}
	for i, c := range perCore {
		ct, ci := c.totalAndIdle()
		snap.PerCorePercent[i] = pct(ct, ci, inst.prevPerCoreTotal[i], inst.prevPerCoreIdle[i])
		inst.prevPerCoreTotal[i] = ct
		inst.prevPerCoreIdle[i] = ci
	}
	return
}

func (inst *Collector) sampleLoadAvg(snap *Snapshot) (err error) {
	var raw []byte
	raw, err = inst.proc.ReadFile("loadavg")
	if err != nil {
		// Missing /proc/loadavg is unusual but not fatal — leave loads at zero.
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
		return
	}
	// /proc/loadavg: "0.05 0.10 0.15 1/123 4567\n"
	var idx int
	for f := range procfs.IterFields(bytes.TrimRight(raw, "\n")) {
		v, perr := strconv.ParseFloat(string(f), 32)
		if perr != nil {
			break
		}
		switch idx {
		case 0:
			snap.LoadAvg1 = float32(v)
		case 1:
			snap.LoadAvg5 = float32(v)
		case 2:
			snap.LoadAvg15 = float32(v)
		}
		idx++
		if idx == 3 {
			break
		}
	}
	return
}

func (inst *Collector) sampleFreq(snap *Snapshot) {
	snap.PerCoreFreqMHz = make([]uint32, inst.logicalCores)
	for i, p := range inst.cpuFreqPaths {
		if p == "" {
			continue
		}
		s, err := inst.sys.ReadString(p)
		if err != nil {
			continue
		}
		// scaling_cur_freq is in kHz; convert to MHz.
		v, perr := strconv.ParseUint(s, 10, 64)
		if perr != nil {
			continue
		}
		snap.PerCoreFreqMHz[i] = uint32(v / 1000)
	}
}

// sampleRAPL reads the package energy counter and converts to average
// watts over the elapsed wall-clock window.
//
// provenance: btop src/linux/btop_collect.cpp:1008-1047 (get_cpuConsumptionWatts).
func (inst *Collector) sampleRAPL(now time.Time, snap *Snapshot) {
	s, err := inst.sys.ReadString(RAPLEnergyPath)
	if err != nil {
		// Counter absent or not readable — treat as unavailable forever.
		inst.raplKnown = true
		inst.raplAvailable = false
		return
	}
	uj, perr := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if perr != nil {
		inst.raplKnown = true
		inst.raplAvailable = false
		return
	}
	if !inst.raplKnown {
		inst.raplKnown = true
		inst.raplAvailable = true
		inst.raplPrevAt = now
		inst.raplPrevUJ = uj
		return
	}
	if !inst.raplAvailable {
		return
	}

	elapsed := now.Sub(inst.raplPrevAt)
	if elapsed <= 0 || uj < inst.raplPrevUJ {
		// Counter went backwards (rollover with no rolloverMax hint), or
		// no time has passed. Update state and emit no rate.
		inst.raplPrevAt = now
		inst.raplPrevUJ = uj
		return
	}
	delta := uj - inst.raplPrevUJ
	// uJ over us == W directly.
	watts := float64(delta) / float64(elapsed.Microseconds())
	snap.UsageWatts = float32(watts)
	snap.UsageWattsAvailable = true
	inst.raplPrevAt = now
	inst.raplPrevUJ = uj
}

func (inst *Collector) sampleActiveCPUs(snap *Snapshot) {
	s, err := inst.sys.ReadString(CGroupCPUSetPath)
	if err != nil {
		return
	}
	snap.ActiveCPUs = parseCPUSet(s)
}

// cpuLine holds the up-to-10 numeric fields of a /proc/stat cpu/cpuN line.
type cpuLine struct {
	times      [10]uint64
	fieldCount int
}

// totalAndIdle returns the total/idle numbers for delta computation.
//
// total = sum(fields) - sum(fields[8..]); guest+guest_nice are already
// counted in user, so subtract to avoid double-counting.
//
// idle = fields[3] (idle) + fields[4] (iowait) when present.
//
// provenance: btop src/linux/btop_collect.cpp:1144-1148.
func (cl cpuLine) totalAndIdle() (total, idle uint64) {
	for i := 0; i < cl.fieldCount; i++ {
		total += cl.times[i]
	}
	if cl.fieldCount > 8 {
		total -= cl.times[8]
	}
	if cl.fieldCount > 9 {
		total -= cl.times[9]
	}
	if cl.fieldCount > 3 {
		idle += cl.times[3]
	}
	if cl.fieldCount > 4 {
		idle += cl.times[4]
	}
	return
}

func parseStat(content []byte) (total cpuLine, perCore []cpuLine, err error) {
	first := true
	for line := range procfs.IterLines(content) {
		if !bytes.HasPrefix(line, []byte("cpu")) {
			break
		}
		var name []byte
		var cl cpuLine
		var idx int
		for f := range procfs.IterFields(line) {
			if idx == 0 {
				name = f
				idx++
				continue
			}
			if cl.fieldCount >= 10 {
				break
			}
			v, perr := strconv.ParseUint(string(f), 10, 64)
			if perr != nil {
				break
			}
			cl.times[cl.fieldCount] = v
			cl.fieldCount++
			idx++
		}
		if cl.fieldCount < 4 {
			if first {
				err = eb.Build().Int("fields", cl.fieldCount).Errorf("malformed /proc/stat: too few fields")
				return
			}
			continue
		}
		if bytes.Equal(name, []byte("cpu")) {
			total = cl
		} else {
			// cpuN
			perCore = append(perCore, cl)
		}
		first = false
	}
	if total.fieldCount == 0 {
		err = eb.Build().Errorf("/proc/stat had no aggregate cpu line")
	}
	return
}

// pct computes a clamped uint8 percentage from delta-totals and delta-idles.
//
// provenance: btop src/linux/btop_collect.cpp:1158, 1188.
func pct(curTotal, curIdle, prevTotal, prevIdle uint64) (out uint8) {
	if curTotal <= prevTotal {
		return 0
	}
	dT := curTotal - prevTotal
	var dI uint64
	if curIdle > prevIdle {
		dI = curIdle - prevIdle
	}
	if dI > dT {
		return 0
	}
	v := float64(dT-dI) * 100 / float64(dT)
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return uint8(v + 0.5)
	}
}

// parseCPUSet parses a comma-separated list of single CPU ids and ranges
// (e.g. "0-3,7,9-11") into the explicit logical CPU id list.
//
// provenance: btop src/linux/btop_collect.cpp:1063-1077.
func parseCPUSet(s string) (cpus []int32) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		dash := strings.IndexByte(part, '-')
		if dash < 0 {
			v, err := strconv.ParseInt(part, 10, 32)
			if err != nil {
				continue
			}
			cpus = append(cpus, int32(v))
			continue
		}
		lo, errLo := strconv.ParseInt(part[:dash], 10, 32)
		hi, errHi := strconv.ParseInt(part[dash+1:], 10, 32)
		if errLo != nil || errHi != nil || lo > hi {
			continue
		}
		for v := lo; v <= hi; v++ {
			cpus = append(cpus, int32(v))
		}
	}
	return
}
