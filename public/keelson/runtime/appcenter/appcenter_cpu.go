package appcenter

import (
	"bytes"
	"context"
	"fmt"
	"runtime/pprof"
	"strconv"
	"time"

	"github.com/google/pprof/profile"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/profiling/pprofarrow"
)

// The Running tab's CPU column (ADR-0351 §SD3–§SD5). Measure CPU profiles
// the process for a few seconds and splits the samples by the window label
// every goroutine doing a window's work carries (§SD1). It runs on the
// person's request, never continuously: a process has one CPU profiler.

const (
	cpuDefaultSeconds = 5
	cpuMaxSeconds     = 30
	// cpuDatasetAlias is the measurement's ad-hoc dataset, apart from
	// imzrt's pprof_cpu so neither replaces the other's.
	cpuDatasetAlias = "pprof_running_cpu"
)

// cpuPublisher is shared by every app center window, so the alias follows
// the newest measurement; the dataset outlives the window that took it.
var cpuPublisher = adhocdata.NewPublisher(cpuDatasetAlias, true)

// cpuMeasurement is one measurement's split.
type cpuMeasurement struct {
	// at is when it ended; length how long it sampled.
	at     time.Time
	length time.Duration
	// open holds the windows open when it started; a window opened since
	// was not measured, whatever its share reads.
	open map[uint64]bool
	// byWindow is sampled CPU time per window key; host is what carried no
	// window label; total is every sample.
	byWindow map[uint64]time.Duration
	host     time.Duration
	total    time.Duration
	// published says the dataset holds this measurement; publishErr says
	// why not.
	published  bool
	publishErr string
}

// cpuState is the render goroutine's side of the measurement.
type cpuState struct {
	job  bgjob.Runner[cpuMeasurement]
	last *cpuMeasurement
}

// share is d as a percentage of one core over the measurement.
func (inst *cpuMeasurement) share(d time.Duration) float64 {
	if inst.length <= 0 {
		return 0
	}
	return 100 * float64(d) / float64(inst.length)
}

// windowShare is the window's share, and whether it was measured.
func (inst *cpuMeasurement) windowShare(key uint64) (pct float64, ok bool) {
	if inst == nil || !inst.open[key] {
		return
	}
	return inst.share(inst.byWindow[key]), true
}

// closed is the CPU time of measured windows no longer listed in v.
func (inst *cpuMeasurement) closed(v *runningView) (d time.Duration) {
	listed := make(map[uint64]bool, len(v.rows))
	for i := range v.rows {
		listed[v.rows[i].key] = true
	}
	for key, t := range inst.byWindow {
		if !listed[key] {
			d += t
		}
	}
	return
}

// measureCpu starts a measurement of seconds over the windows open now.
// It refuses while one runs.
func (inst *App) measureCpu(seconds int) (err error) {
	if seconds <= 0 {
		seconds = cpuDefaultSeconds
	}
	if seconds > cpuMaxSeconds {
		err = eh.Errorf("a measurement lasts at most %d seconds", cpuMaxSeconds)
		return
	}
	snap := inst.snapshot()
	open := make(map[uint64]bool, len(snap.running.view.rows))
	for _, r := range snap.running.view.rows {
		open[r.key] = true
	}
	length := time.Duration(seconds) * time.Second
	bus := inst.bus
	started := inst.cpu.job.StartReporting(inst.tasks, bgjob.Spec{
		Kind:  "appcenter-cpu",
		Title: fmt.Sprintf("measure CPU for %d s", seconds),
	}, func(ctx context.Context, report bgjob.Reporter) (m *cpuMeasurement, err error) {
		return takeCpuMeasurement(ctx, report, length, open, bus)
	})
	if !started {
		err = eh.Errorf("a measurement is running")
		return
	}
	inst.markDirty()
	return
}

// syncCpu takes a finished measurement, on the render goroutine.
func (inst *App) syncCpu() {
	if m, _, ok := inst.cpu.job.TakeResult(); ok {
		inst.cpu.last = m
	}
}

// takeCpuMeasurement profiles the process for length, splits the samples by
// window, and publishes the profile as cpuDatasetAlias. A publish that fails
// leaves the split standing.
func takeCpuMeasurement(ctx context.Context, report bgjob.Reporter, length time.Duration, open map[uint64]bool, bus app.BusI) (m *cpuMeasurement, err error) {
	var buf bytes.Buffer
	err = pprof.StartCPUProfile(&buf)
	if err != nil {
		err = eh.Errorf("another CPU profile is running (imzrt's Profiles tab, or --pprofCpuOutputFile): %w", err)
		return
	}
	start := time.Now()
	const steps = 20
	for i := range uint64(steps) {
		report(i, steps, "sampling")
		select {
		case <-ctx.Done():
			pprof.StopCPUProfile()
			err = ctx.Err()
			return
		case <-time.After(length / steps):
		}
	}
	pprof.StopCPUProfile()
	end := time.Now()

	raw := buf.Bytes()
	p, err := profile.ParseData(raw)
	if err != nil {
		err = eh.Errorf("parse the profile: %w", err)
		return
	}
	m = splitCpu(p)
	m.at, m.length, m.open = end, end.Sub(start), open

	report(0, 0, "publishing")
	conv, cErr := pprofarrow.Convert(bytes.NewReader(raw), pprofarrow.WithKindHint("cpu"))
	if cErr == nil && bus != nil {
		_, cErr = cpuPublisher.Publish(bus, conv.IPCStream)
	}
	switch {
	case cErr != nil:
		m.publishErr = cErr.Error()
	case bus == nil:
		m.publishErr = "no bus to publish the profile on"
	default:
		m.published = true
	}
	return
}

// splitCpu sums a CPU profile's samples by their window label.
func splitCpu(p *profile.Profile) (m *cpuMeasurement) {
	m = &cpuMeasurement{byWindow: map[uint64]time.Duration{}}
	idx := len(p.SampleType) - 1
	for i, st := range p.SampleType {
		if st.Type == "cpu" {
			idx = i
		}
	}
	if idx < 0 {
		return
	}
	for _, s := range p.Sample {
		d := time.Duration(s.Value[idx])
		m.total += d
		w := s.Label[task.LabelWindow]
		key, kErr := uint64(0), error(nil)
		if len(w) == 1 {
			key, kErr = strconv.ParseUint(w[0], 10, 64)
		}
		if len(w) != 1 || kErr != nil {
			m.host += d
			continue
		}
		m.byWindow[key] += d
	}
	return
}

// cpuSeedSql is the statement Explore opens play on: the profile in the
// icicle panel's folded contract, as imzrt's Explore has it, narrowed to one
// window when key is set.
func cpuSeedSql(key uint64) string {
	where := ""
	if key != 0 {
		where = fmt.Sprintf(" WHERE labels['%s'] = '%d'", task.LabelWindow, key)
	}
	return fmt.Sprintf(
		"WITH s AS (SELECT stack, value AS v FROM keelson('%s')%s)\n"+
			"SELECT stack, v / 1000000 AS value, 'ms' AS unit\n"+
			"FROM s", cpuDatasetAlias, where)
}

// exploreCpu opens play on the measurement's dataset, narrowed to the one
// window the filter shows, if it shows one.
func (inst *App) exploreCpu(key uint64) {
	inst.exploreInPlay(cpuSeedSql(key), "icicle", cpuDatasetAlias)
}
