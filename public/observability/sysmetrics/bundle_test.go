package sysmetrics_test

import (
	"context"
	"errors"
	stdnet "net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/battery"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/container"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/cpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/disk"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/mem"
	pnet "github.com/stergiotis/boxer/public/observability/sysmetrics/net"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sensors"
)

// fakeGPUSampler is a deterministic test double for gpu.SamplerI.
type fakeGPUSampler struct {
	snap     gpu.Snapshot
	sampleErr error
	closed   bool
	closeErr error
}

func (f *fakeGPUSampler) Sample(ctx context.Context) (gpu.Snapshot, error) {
	return f.snap, f.sampleErr
}
func (f *fakeGPUSampler) Close() error {
	f.closed = true
	return f.closeErr
}

func TestBundle_NoCollectors_EmptySnapshot(t *testing.T) {
	b, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{})
	snap, err := b.Sample(context.Background())
	require.NoError(t, err)
	assert.Greater(t, snap.SampledAtUnixMs, int64(0))
	assert.Empty(t, snap.Errors)
	assert.Nil(t, snap.CPU)
	assert.Nil(t, snap.Mem)
}

func TestBundle_AllDomains_FromFixtures(t *testing.T) {
	root := newFullFixtureTree(t)
	procRdr := procfs.New(filepath.Join(root, "proc"))
	sysRdr := sysfs.New(filepath.Join(root, "sys"))

	cpuC, err := cpu.New(cpu.Options{Proc: procRdr, Sys: sysRdr})
	require.NoError(t, err)
	memC, _ := mem.New(mem.Options{Proc: procRdr})
	diskC, _ := disk.New(disk.Options{
		Proc: procRdr, Sys: sysRdr,
		StatfsFunc:     stubStatfs,
		DeviceResolver: deviceBasename,
	})
	netC, _ := pnet.New(pnet.Options{
		Sys:    sysRdr,
		Lister: staticLister([]stdnet.Interface{{Index: 1, Name: "eth0"}}),
	})
	batC, _ := battery.New(battery.Options{Sys: sysRdr})
	procC, _ := proc.New(proc.Options{
		Proc:       procRdr,
		UserLookup: func(uid uint32) string { return "test" },
		ClkTckHz:   100, NumCPUs: 4,
	})
	contC, _ := container.New(container.Options{Proc: procRdr, MarkerRoot: t.TempDir()})

	b, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{
		CPU: cpuC, Mem: memC, Disk: diskC, Net: netC,
		Battery: batC, Proc: procC, Container: contC,
	})

	snap, err := b.Sample(context.Background())
	require.NoError(t, err)
	require.Empty(t, snap.Errors, "all domains should succeed against fixture tree")

	require.NotNil(t, snap.CPU)
	require.NotNil(t, snap.Mem)
	require.NotNil(t, snap.Disk)
	require.NotNil(t, snap.Net)
	require.NotNil(t, snap.Battery)
	require.NotNil(t, snap.Container)
	assert.Equal(t, container.EngineNone, snap.Container.Engine)

	assert.Greater(t, snap.Mem.TotalBytes, uint64(0))
	assert.NotEmpty(t, snap.Net.Interfaces)
}

func TestBundle_PartialFailure_DoesNotAbortOthers(t *testing.T) {
	// CPU collector points at an empty proc dir → fails on first read.
	// Mem collector points at a valid fixture → succeeds.
	emptyProc := procfs.New(t.TempDir())
	root := newFullFixtureTree(t)
	memC, _ := mem.New(mem.Options{Proc: procfs.New(filepath.Join(root, "proc"))})

	cpuC, err := cpu.New(cpu.Options{
		Proc: procfs.New(filepath.Join(root, "proc")),
		Sys:  sysfs.New(filepath.Join(root, "sys")),
	})
	require.NoError(t, err)

	// Replace cpu's proc reader with an empty one to force a Sample-time
	// failure (Sample reads /proc/stat live; New only read /proc/cpuinfo).
	_ = emptyProc
	failingCPU, err := cpu.New(cpu.Options{
		Proc: procfs.New(filepath.Join(root, "proc")),
		Sys:  sysfs.New(filepath.Join(root, "sys")),
	})
	require.NoError(t, err)
	// Now break /proc/stat after construction.
	require.NoError(t, os.Remove(filepath.Join(root, "proc", "stat")))

	b, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{
		CPU: failingCPU,
		Mem: memC,
	})
	snap, err := b.Sample(context.Background())
	require.NoError(t, err, "ctx is fine — partial failure must not bubble up as Sample err")

	require.Contains(t, snap.Errors, sysmetrics.DomainCPU)
	require.NotContains(t, snap.Errors, sysmetrics.DomainMem)
	require.NotNil(t, snap.Mem)
	assert.Nil(t, snap.CPU)
	_ = cpuC // silence unused
}

func TestBundle_ContextCancellation_PropagatesAsSampleErr(t *testing.T) {
	root := newFullFixtureTree(t)
	memC, _ := mem.New(mem.Options{Proc: procfs.New(filepath.Join(root, "proc"))})

	b, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{Mem: memC})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.Sample(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestBundle_GPU_Wired(t *testing.T) {
	fake := &fakeGPUSampler{snap: gpu.Snapshot{
		SampledAtUnixMs: 999,
		Devices: []gpu.Device{
			{Vendor: "intel", Index: 0, Name: "Tiger Lake-H GT2", BusyPercent: 42},
		},
	}}
	b, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{GPU: fake})
	snap, err := b.Sample(context.Background())
	require.NoError(t, err)
	require.NotNil(t, snap.GPU)
	require.Len(t, snap.GPU.Devices, 1)
	assert.Equal(t, uint8(42), snap.GPU.Devices[0].BusyPercent)
	assert.Empty(t, snap.Errors)
}

func TestBundle_GPU_SampleError_LandsInErrors(t *testing.T) {
	fake := &fakeGPUSampler{sampleErr: errors.New("kaboom")}
	b, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{GPU: fake})
	snap, err := b.Sample(context.Background())
	require.NoError(t, err)
	require.Contains(t, snap.Errors, sysmetrics.DomainGPU)
	assert.Nil(t, snap.GPU)
}

func TestBundle_Close_ReleasesGPU(t *testing.T) {
	fake := &fakeGPUSampler{}
	b, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{GPU: fake})
	require.NoError(t, b.Close())
	assert.True(t, fake.closed, "Close must propagate to wired GPU sampler")
}

func TestBundle_Close_NoOpWhenNothingCloseable(t *testing.T) {
	root := newFullFixtureTree(t)
	memC, _ := mem.New(mem.Options{Proc: procfs.New(filepath.Join(root, "proc"))})
	b, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{Mem: memC})
	require.NoError(t, b.Close())
}

func TestBundle_Close_AggregatesErrors(t *testing.T) {
	a := &fakeGPUSampler{closeErr: errors.New("a-fail")}
	b1, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{GPU: a})
	err := b1.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a-fail")
}

func TestBundle_NowFunc_OverridesTimestamp(t *testing.T) {
	fixed := time.Unix(1_700_000_000, 0)
	b, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{
		NowFunc: func() time.Time { return fixed },
	})
	snap, err := b.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, fixed.UnixMilli(), snap.SampledAtUnixMs)
}

func TestBundle_LiveSystem_Smoke(t *testing.T) {
	if _, statErr := os.Stat("/proc/stat"); statErr != nil {
		t.Skipf("no live /proc/stat: %v", statErr)
	}
	cpuC, err := cpu.New(cpu.Options{})
	require.NoError(t, err)
	memC, _ := mem.New(mem.Options{})

	b, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{
		CPU: cpuC,
		Mem: memC,
	})
	snap, err := b.Sample(context.Background())
	require.NoError(t, err)
	require.NotNil(t, snap.CPU)
	require.NotNil(t, snap.Mem)
	assert.Empty(t, snap.Errors)
}

// BenchmarkBundle_LiveSystem documents the M4 done-when criterion
// ("under 5 ms on a 16-core laptop"). Run with:
//
//	go test -tags="$(cat tags|tr -d $'\n')" -bench=BenchmarkBundle ./public/observability/sysmetrics/
func BenchmarkBundle_LiveSystem(b *testing.B) {
	if _, err := os.Stat("/proc/stat"); err != nil {
		b.Skipf("no live /proc/stat: %v", err)
	}
	cpuC, err := cpu.New(cpu.Options{})
	if err != nil {
		b.Fatal(err)
	}
	memC, err := mem.New(mem.Options{})
	if err != nil {
		b.Fatal(err)
	}
	netC, err := pnet.New(pnet.Options{})
	if err != nil {
		b.Fatal(err)
	}
	bundle, _ := sysmetrics.NewBundle(sysmetrics.BundleOptions{
		CPU: cpuC,
		Mem: memC,
		Net: netC,
	})
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bundle.Sample(ctx)
	}
}

// --- helpers --------------------------------------------------------

func newFullFixtureTree(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()

	// /proc tree
	procDir := filepath.Join(root, "proc")
	require.NoError(t, os.MkdirAll(procDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "stat"),
		[]byte("cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 25 0 25 200 0 0 0 0 0 0\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "meminfo"),
		[]byte("MemTotal: 16384000 kB\nMemFree: 8192000 kB\nMemAvailable: 12288000 kB\nBuffers: 0 kB\nCached: 4096000 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "loadavg"), []byte("0.1 0.2 0.3 1/100 9999\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "cpuinfo"), []byte("processor\t: 0\nmodel name\t: BundleTest CPU\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "uptime"), []byte("100.00 100.00\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "filesystems"), []byte("nodev\ttmpfs\n\text4\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(procDir, "self"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "self/mounts"), []byte("/dev/sda1 / ext4 rw 0 0\n"), 0o644))

	// /sys tree
	sysDir := filepath.Join(root, "sys")
	require.NoError(t, os.MkdirAll(filepath.Join(sysDir, "class/net/eth0/statistics"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sysDir, "class/net/eth0/statistics/rx_bytes"), []byte("100\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sysDir, "class/net/eth0/statistics/tx_bytes"), []byte("200\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(sysDir, "class/block/sda1"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sysDir, "class/block/sda1/stat"), []byte("0 0 0 0 0 0 0 0 0 0 0\n"), 0o644))
	return
}

func stubStatfs(path string) (cap disk.Capacity, err error) {
	cap = disk.Capacity{TotalBytes: 1 << 20, FreeBytes: 1 << 19, UsedBytes: 1 << 19, UsedPercent: 50}
	return
}

func deviceBasename(device string) (blockName string) {
	if device == "" || !filepath.IsAbs(device) {
		return ""
	}
	return filepath.Base(device)
}

func staticLister(ifs []stdnet.Interface) pnet.InterfaceLister {
	return func() ([]stdnet.Interface, error) { return ifs, nil }
}

// silence unused checks for sensors helper alias (used elsewhere only when
// sensor-specific assertions land — keeps the import live).
var _ sensors.CollectorI = (*sensors.Collector)(nil)
