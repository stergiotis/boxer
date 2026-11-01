package appcenter

import (
	"strings"
	"testing"
	"time"

	"github.com/google/pprof/profile"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appcenter/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// cpuProfile is a CPU profile of samples, each a window label ("" for none)
// and its CPU time.
func cpuProfile(samples map[string]int64) (p *profile.Profile) {
	p = &profile.Profile{SampleType: []*profile.ValueType{{Type: "samples", Unit: "count"}, {Type: "cpu", Unit: "nanoseconds"}}}
	for window, ns := range samples {
		s := &profile.Sample{Value: []int64{1, ns}}
		if window != "" {
			s.Label = map[string][]string{task.LabelWindow: {window}, task.LabelApp: {"a"}}
		}
		p.Sample = append(p.Sample, s)
	}
	return
}

func TestSplitCpuByWindowLabel(t *testing.T) {
	m := splitCpu(cpuProfile(map[string]int64{"1": 300, "2": 100, "": 50, "not-a-key": 25}))
	assert.Equal(t, map[uint64]time.Duration{1: 300, 2: 100}, m.byWindow)
	assert.Equal(t, time.Duration(75), m.host, "unlabelled and unreadable labels are the host's")
	assert.Equal(t, time.Duration(475), m.total)
}

// A window is measured only when it was open as the measurement started; a
// measured window no longer listed counts as closed.
func TestCpuSharesAndClosedWindows(t *testing.T) {
	m := &cpuMeasurement{length: time.Second, open: map[uint64]bool{1: true, 2: true, 7: true},
		byWindow: map[uint64]time.Duration{1: 250 * time.Millisecond, 7: 100 * time.Millisecond}}
	pct, ok := m.windowShare(1)
	assert.True(t, ok)
	assert.InDelta(t, 25.0, pct, 1e-9)
	pct, ok = m.windowShare(2)
	assert.True(t, ok, "open and idle is measured")
	assert.Zero(t, pct)
	_, ok = m.windowShare(3)
	assert.False(t, ok, "opened since")
	_, ok = (*cpuMeasurement)(nil).windowShare(1)
	assert.False(t, ok, "before any measurement")

	w, f, tk := runningFixture()
	v := assembleRunning(&w, &f, &tk)
	assert.Equal(t, 100*time.Millisecond, m.closed(&v), "window 7 is not listed")
}

func TestListRunningCarriesTheMeasurement(t *testing.T) {
	inst := opsFixture(t)
	w, f, tk := runningFixture()
	inst.mu.Lock()
	inst.snap.running = running{view: assembleRunning(&w, &f, &tk), read: time.Now()}
	inst.mu.Unlock()
	inst.cpu.last = &cpuMeasurement{at: time.Now(), length: 2 * time.Second, open: map[uint64]bool{1: true, 2: true},
		byWindow: map[uint64]time.Duration{1: time.Second, 9: 200 * time.Millisecond}, host: 100 * time.Millisecond,
		total: 1300 * time.Millisecond, published: true}

	out, err := query[struct{}, Running](t, inst, opListRunning, struct{}{})
	require.NoError(t, err)
	pcts := map[uint64]float64{}
	for _, r := range out.Windows {
		pcts[r.Key] = r.CpuPct
	}
	assert.Equal(t, map[uint64]float64{1: 50, 2: 0, 3: -1}, pcts)
	assert.InDelta(t, 65.0, out.Cpu.AllPct, 1e-9)
	assert.InDelta(t, 5.0, out.Cpu.HostPct, 1e-9)
	assert.InDelta(t, 10.0, out.Cpu.ClosedPct, 1e-9)
	assert.Equal(t, cpuDatasetAlias, out.Cpu.ProfileAlias)
	assert.InDelta(t, 2.0, out.Cpu.Seconds, 1e-9)
}

func TestMeasureCpuRefusesALengthOutOfRange(t *testing.T) {
	inst := opsFixture(t)
	err := command(t, inst, opMeasureCpu, MeasureCpuArgs{Seconds: cpuMaxSeconds + 1})
	require.Error(t, err)
	assert.NotNil(t, refusal(err))
	assert.False(t, inst.cpu.job.Running())
}

// A measurement runs as a job and lands on the next frame's sync; a second
// request while it runs is refused.
func TestMeasureCpuTakesAMeasurement(t *testing.T) {
	inst := opsFixture(t)
	require.NoError(t, command(t, inst, opMeasureCpu, MeasureCpuArgs{Seconds: 1}))
	err := command(t, inst, opMeasureCpu, MeasureCpuArgs{Seconds: 1})
	require.Error(t, err)
	assert.NotNil(t, refusal(err), "one at a time")
	require.Eventually(t, func() bool {
		inst.syncCpu()
		return inst.cpu.last != nil
	}, 10*time.Second, 50*time.Millisecond)
	m := inst.cpu.last
	assert.InDelta(t, 1.0, m.length.Seconds(), 0.3)
	assert.False(t, m.published, "no bus to publish on")
	assert.NotEmpty(t, m.publishErr)
}

func TestCpuSeedSqlNarrowsToOneWindow(t *testing.T) {
	assert.NotContains(t, cpuSeedSql(0), "WHERE")
	sql := cpuSeedSql(12)
	assert.True(t, strings.Contains(sql, "WHERE labels['boxer.window'] = '12'"), sql)
	assert.Contains(t, sql, "keelson('"+cpuDatasetAlias+"')")
}

// A launch config naming a filter, the slow-window notice's (ADR-0351
// §SD6), opens the Running tab under it; one naming an app opens its page.
func TestLaunchConfigFilterOpensTheRunningTab(t *testing.T) {
	mount := func(launch launchcfg.AppCenterLaunch) (inst *App) {
		raw, err := buscodec.Encode(launch)
		require.NoError(t, err)
		ctx := app.NewStaticMountContext(AppId, zerolog.Nop(), nil, nil, nil)
		ctx.SetLaunchConfig(raw)
		inst = newApp()
		inst.reader = &fixtureReader{}
		require.NoError(t, inst.Mount(ctx))
		t.Cleanup(func() { _ = inst.Unmount(ctx) })
		return
	}
	inst := mount(launchcfg.AppCenterLaunch{At: time.Now(), Filter: "SQL Playground"})
	assert.Equal(t, viewRunning, inst.view)
	assert.Equal(t, "SQL Playground", inst.runFilter)

	inst = mount(launchcfg.AppCenterLaunch{At: time.Now(), AppId: "a/play"})
	assert.Equal(t, viewApps, inst.view)
	assert.Empty(t, inst.runFilter)
}
