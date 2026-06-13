package cpu_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/cpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

const cpuinfoFixture = `processor	: 0
model name	: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
cpu MHz		: 2400.000

processor	: 1
model name	: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
cpu MHz		: 2400.000

processor	: 2
model name	: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
cpu MHz		: 2400.000

processor	: 3
model name	: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
cpu MHz		: 2400.000
`

const statFixtureT0 = `cpu  8000 0 4000 80000 0 0 0 0 0 0
cpu0 2000 0 1000 20000 0 0 0 0 0 0
cpu1 2000 0 1000 20000 0 0 0 0 0 0
cpu2 2000 0 1000 20000 0 0 0 0 0 0
cpu3 2000 0 1000 20000 0 0 0 0 0 0
intr 12345
ctxt 67890
`

const statFixtureT1 = `cpu  8165 0 4000 80235 0 0 0 0 0 0
cpu0 2090 0 1000 20010 0 0 0 0 0 0
cpu1 2050 0 1000 20050 0 0 0 0 0 0
cpu2 2025 0 1000 20075 0 0 0 0 0 0
cpu3 2000 0 1000 20100 0 0 0 0 0 0
intr 12999
ctxt 67999
`

const loadavgFixture = "0.05 0.10 0.15 1/100 9999\n"

func TestNew_StaticMetadata(t *testing.T) {
	root := newFixtureTree(t)
	c := newCollectorWithFixture(t, root, cpu.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz", snap.ModelName)
	assert.Equal(t, int32(4), snap.LogicalCores)
}

func TestNew_MissingProcCpuinfo_Errors(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "proc"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "sys"), 0o755))
	_, err := cpu.New(cpu.Options{
		Proc: procfs.New(filepath.Join(tmp, "proc")),
		Sys:  sysfs.New(filepath.Join(tmp, "sys")),
	})
	require.Error(t, err)
}

func TestSample_FirstCall_ZeroPercent(t *testing.T) {
	root := newFixtureTree(t)
	c := newCollectorWithFixture(t, root, cpu.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint8(0), snap.TotalPercent)
	assert.Equal(t, []uint8{0, 0, 0, 0}, snap.PerCorePercent)
}

func TestSample_SecondCall_DeltaPercent(t *testing.T) {
	root := newFixtureTree(t)
	c := newCollectorWithFixture(t, root, cpu.Options{})

	_, err := c.Sample(context.Background())
	require.NoError(t, err)

	// Advance the fixture: rewrite /proc/stat with the t=1 content.
	require.NoError(t, os.WriteFile(filepath.Join(root, "proc", "stat"), []byte(statFixtureT1), 0o644))

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	// Expected from fixture math:
	// total delta=400, idle delta=235 → 41.25% → uint8(41).
	assert.Equal(t, uint8(41), snap.TotalPercent)
	// per-core: cpu0 90, cpu1 50, cpu2 25, cpu3 0.
	assert.Equal(t, []uint8{90, 50, 25, 0}, snap.PerCorePercent)
}

func TestSample_LoadAvg(t *testing.T) {
	root := newFixtureTree(t)
	c := newCollectorWithFixture(t, root, cpu.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, 0.05, snap.LoadAvg1, 0.001)
	assert.InDelta(t, 0.10, snap.LoadAvg5, 0.001)
	assert.InDelta(t, 0.15, snap.LoadAvg15, 0.001)
}

func TestSample_PerCoreFreq(t *testing.T) {
	root := newFixtureTree(t)
	c := newCollectorWithFixture(t, root, cpu.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []uint32{2400, 2400, 2400, 2400}, snap.PerCoreFreqMHz)
}

func TestSample_PerCoreFreq_PartialMissing(t *testing.T) {
	root := newFixtureTree(t)
	// Remove policy2 and policy3 to simulate cores without cpufreq exposure.
	require.NoError(t, os.RemoveAll(filepath.Join(root, "sys", "devices/system/cpu/cpufreq/policy2")))
	require.NoError(t, os.RemoveAll(filepath.Join(root, "sys", "devices/system/cpu/cpufreq/policy3")))

	c := newCollectorWithFixture(t, root, cpu.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []uint32{2400, 2400, 0, 0}, snap.PerCoreFreqMHz)
}

func TestSample_FreqDisabled(t *testing.T) {
	root := newFixtureTree(t)
	c := newCollectorWithFixture(t, root, cpu.Options{DisableFreq: true})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Empty(t, snap.PerCoreFreqMHz)
}

func TestSample_RAPL_DeltaWatts(t *testing.T) {
	root := newFixtureTree(t)

	now := time.Unix(0, 0)
	c := newCollectorWithFixture(t, root, cpu.Options{
		NowFunc: func() time.Time { return now },
	})

	// First sample primes RAPL prev state.
	_, err := c.Sample(context.Background())
	require.NoError(t, err)

	// Advance time by 1s and counter by 10 J = 10_000_000 uJ.
	now = now.Add(time.Second)
	rewriteRAPL(t, root, "11000000")

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.True(t, snap.UsageWattsAvailable)
	assert.InDelta(t, 10.0, snap.UsageWatts, 0.0001)
}

func TestSample_RAPL_PathMissing_NotAvailable(t *testing.T) {
	root := newFixtureTree(t)
	require.NoError(t, os.RemoveAll(filepath.Join(root, "sys", "class/powercap")))

	c := newCollectorWithFixture(t, root, cpu.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.False(t, snap.UsageWattsAvailable)
	assert.Equal(t, float32(0), snap.UsageWatts)
}

func TestSample_RAPL_Disabled(t *testing.T) {
	root := newFixtureTree(t)
	c := newCollectorWithFixture(t, root, cpu.Options{DisableRAPL: true})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.False(t, snap.UsageWattsAvailable)
}

func TestSample_ActiveCPUs(t *testing.T) {
	root := newFixtureTree(t)
	c := newCollectorWithFixture(t, root, cpu.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []int32{0, 1, 2, 3}, snap.ActiveCPUs)
}

func TestSample_ActiveCPUs_Mixed(t *testing.T) {
	root := newFixtureTree(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "sys", "fs/cgroup/cpuset.cpus.effective"),
		[]byte("0,2,4-5\n"), 0o644))

	c := newCollectorWithFixture(t, root, cpu.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []int32{0, 2, 4, 5}, snap.ActiveCPUs)
}

func TestSample_NoCGroup(t *testing.T) {
	root := newFixtureTree(t)
	require.NoError(t, os.RemoveAll(filepath.Join(root, "sys", "fs/cgroup")))

	c := newCollectorWithFixture(t, root, cpu.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Nil(t, snap.ActiveCPUs)
}

func TestSample_LiveProc_Smoke(t *testing.T) {
	if _, err := os.Stat("/proc/stat"); err != nil {
		t.Skipf("no live /proc/stat: %v", err)
	}
	c, err := cpu.New(cpu.Options{})
	require.NoError(t, err)

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	// Plausibility: shape.
	assert.Greater(t, snap.LogicalCores, int32(0))
	assert.NotEmpty(t, snap.ModelName)
	assert.GreaterOrEqual(t, len(snap.PerCorePercent), int(snap.LogicalCores))
	assert.Equal(t, len(snap.PerCorePercent), len(snap.PerCoreFreqMHz),
		"per-core slices must align")

	// Plausibility: values within physical bounds.
	assert.LessOrEqual(t, snap.TotalPercent, uint8(100))
	for i, p := range snap.PerCorePercent {
		assert.LessOrEqual(t, p, uint8(100), "core %d busy%% > 100", i)
	}
	for i, f := range snap.PerCoreFreqMHz {
		// Modern x86 CPUs sit between ~400 MHz idle and ~6.5 GHz boost.
		// Allow 0 (cpufreq missing for that core).
		if f != 0 {
			assert.Greater(t, f, uint32(100), "core %d freq implausibly low: %d MHz", i, f)
			assert.Less(t, f, uint32(7000), "core %d freq implausibly high: %d MHz", i, f)
		}
	}
	// Load averages are non-negative.
	assert.GreaterOrEqual(t, snap.LoadAvg1, float32(0))
	assert.GreaterOrEqual(t, snap.LoadAvg5, float32(0))
	assert.GreaterOrEqual(t, snap.LoadAvg15, float32(0))
	// RAPL is opportunistic; if available, watts should be reasonable.
	if snap.UsageWattsAvailable {
		assert.GreaterOrEqual(t, snap.UsageWatts, float32(0))
		assert.Less(t, snap.UsageWatts, float32(2000), "package power implausibly high: %v W", snap.UsageWatts)
	}
}

// --- helpers -------------------------------------------------------------

func newFixtureTree(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()

	procPath := filepath.Join(root, "proc")
	require.NoError(t, os.MkdirAll(procPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(procPath, "cpuinfo"), []byte(cpuinfoFixture), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(procPath, "stat"), []byte(statFixtureT0), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(procPath, "loadavg"), []byte(loadavgFixture), 0o644))

	sysPath := filepath.Join(root, "sys")
	for i := 0; i < 4; i++ {
		dir := filepath.Join(sysPath, "devices/system/cpu/cpufreq", fmt.Sprintf("policy%d", i))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "scaling_cur_freq"), []byte("2400000\n"), 0o644))
	}
	rapl := filepath.Join(sysPath, "class/powercap/intel-rapl:0")
	require.NoError(t, os.MkdirAll(rapl, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rapl, "energy_uj"), []byte("1000000\n"), 0o644))

	cgroup := filepath.Join(sysPath, "fs/cgroup")
	require.NoError(t, os.MkdirAll(cgroup, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cgroup, "cpuset.cpus.effective"), []byte("0-3\n"), 0o644))

	return
}

func newCollectorWithFixture(t *testing.T, root string, opts cpu.Options) (inst *cpu.Collector) {
	t.Helper()
	if opts.Proc == nil {
		opts.Proc = procfs.New(filepath.Join(root, "proc"))
	}
	if opts.Sys == nil {
		opts.Sys = sysfs.New(filepath.Join(root, "sys"))
	}
	c, err := cpu.New(opts)
	require.NoError(t, err)
	return c
}

func rewriteRAPL(t *testing.T, root, value string) {
	t.Helper()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "sys", "class/powercap/intel-rapl:0/energy_uj"),
		[]byte(value+"\n"), 0o644))
}
