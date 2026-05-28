//go:build linux && gpu_nvml && llm_generated_opus47

package nvml_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu/nvml"
)

func TestNew_LoaderError_ReturnsErrNVMLUnavailable(t *testing.T) {
	c, err := nvml.New(nvml.Options{
		Loader: func() (nvml.NVMLI, error) {
			return nil, errors.New("dlopen libnvidia-ml.so.1: no such file")
		},
	})
	assert.Nil(t, c)
	require.Error(t, err)
	assert.True(t, errors.Is(err, nvml.ErrNVMLUnavailable))
}

func TestSample_NoDevices_Empty(t *testing.T) {
	c, err := nvml.New(nvml.Options{Loader: stubLoader(nil)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Empty(t, snap.Devices)
	assert.Greater(t, snap.SampledAtUnixMs, int64(0))
}

func TestSample_OneDevice_AllFields(t *testing.T) {
	c, err := nvml.New(nvml.Options{Loader: stubLoader([]fakeDev{{
		name:   "GeForce RTX 4090",
		pciID:  "0x2684",
		gpu:    73,
		mem:    42,
		total:  24 << 30, // 24 GiB
		used:   8 << 30,  // 8 GiB
		mw:     350_000,  // 350 W
		tempC:  68,
		gfxMHz: 2520,
	}})})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	d := snap.Devices[0]
	assert.Equal(t, int32(0), d.Index)
	assert.Equal(t, "GeForce RTX 4090", d.Name)
	assert.Equal(t, "0x2684", d.PCIID)
	assert.Equal(t, uint8(73), d.GPUUtilizationPercent)
	assert.Equal(t, uint8(42), d.MemoryUtilizationPercent)
	assert.Equal(t, uint64(24<<30), d.MemoryTotalBytes)
	assert.Equal(t, uint64(8<<30), d.MemoryUsedBytes)
	assert.InDelta(t, 350.0, d.PowerWatts, 0.001)
	assert.InDelta(t, 68.0, d.TempC, 0.001)
	assert.Equal(t, uint32(2520), d.GraphicsClockMHz)
}

func TestSample_PerFieldFailure_DoesNotAbort(t *testing.T) {
	dev := fakeDev{
		name:    "Tesla V100",
		pciID:   "0x1db1",
		gpu:     50,
		mem:     20,
		total:   16 << 30,
		used:    4 << 30,
		mw:      0,
		tempC:   0,
		gfxMHz:  0,
		powerErr: errors.New("nvmlDeviceGetPowerUsage: NVML_ERROR_NOT_SUPPORTED"),
		tempErr:  errors.New("nvmlDeviceGetTemperature: NVML_ERROR_UNKNOWN"),
	}
	c, err := nvml.New(nvml.Options{Loader: stubLoader([]fakeDev{dev})})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	d := snap.Devices[0]
	assert.Equal(t, "Tesla V100", d.Name)
	assert.Equal(t, uint8(50), d.GPUUtilizationPercent)
	assert.Equal(t, float32(0), d.PowerWatts, "missing power telemetry must leave zero")
	assert.Equal(t, float32(0), d.TempC)
}

func TestSample_BoundsClamping(t *testing.T) {
	c, err := nvml.New(nvml.Options{Loader: stubLoader([]fakeDev{{gpu: 150, mem: 200}})})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint8(100), snap.Devices[0].GPUUtilizationPercent)
	assert.Equal(t, uint8(100), snap.Devices[0].MemoryUtilizationPercent)
}

func TestSample_MultipleDevices_SortedByIndex(t *testing.T) {
	c, err := nvml.New(nvml.Options{Loader: stubLoader([]fakeDev{
		{name: "GPU0"},
		{name: "GPU1"},
		{name: "GPU2"},
	})})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 3)
	for i, d := range snap.Devices {
		assert.Equal(t, int32(i), d.Index)
	}
}

func TestSample_CountError_BubblesUp(t *testing.T) {
	loader := func() (nvml.NVMLI, error) {
		return &fakeNVML{countErr: errors.New("nvmlDeviceGetCount: NVML_ERROR_UNINITIALIZED")}, nil
	}
	c, err := nvml.New(nvml.Options{Loader: loader})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	_, err = c.Sample(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device count")
}

func TestClose_PropagatesShutdown(t *testing.T) {
	fake := &fakeNVML{}
	c, err := nvml.New(nvml.Options{Loader: func() (nvml.NVMLI, error) { return fake, nil }})
	require.NoError(t, err)

	require.NoError(t, c.Close())
	assert.True(t, fake.closed, "Close must propagate to underlying NVMLI")
}

func TestGenericSampler_RoundTrip(t *testing.T) {
	loader := stubLoader([]fakeDev{{
		name: "GeForce RTX 4090", pciID: "0x2684",
		gpu: 73, total: 24 << 30, used: 8 << 30, mw: 350_000, tempC: 68, gfxMHz: 2520,
	}})

	g, err := nvml.NewGenericSampler(nvml.Options{Loader: loader})
	require.NoError(t, err)
	t.Cleanup(func() { _ = g.Close() })

	var _ gpu.SamplerI = g

	snap, err := g.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	d := snap.Devices[0]
	assert.Equal(t, "nvidia", d.Vendor)
	assert.Equal(t, "GeForce RTX 4090", d.Name)
	assert.Equal(t, uint8(73), d.BusyPercent)
	assert.InDelta(t, 350.0, d.PowerWatts, 0.001)
}

func TestSample_LiveSystem_Smoke(t *testing.T) {
	c, err := nvml.New(nvml.Options{})
	if errors.Is(err, nvml.ErrNVMLUnavailable) {
		t.Skipf("no NVML on this host: %v", err)
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	t.Logf("live NVML snapshot: %d devices", len(snap.Devices))
	for _, d := range snap.Devices {
		assert.NotEmpty(t, d.Name)
		assert.LessOrEqual(t, d.GPUUtilizationPercent, uint8(100))
		assert.LessOrEqual(t, d.MemoryUtilizationPercent, uint8(100))
		assert.LessOrEqual(t, d.MemoryUsedBytes, d.MemoryTotalBytes)
		assert.GreaterOrEqual(t, d.PowerWatts, float32(0))
		assert.Less(t, d.PowerWatts, float32(800), "device %s power implausibly high", d.Name)
		if d.TempC != 0 {
			assert.Greater(t, d.TempC, float32(-40))
			assert.Less(t, d.TempC, float32(150))
		}
	}
	_ = os.Stdout
}

func TestSample_ContextCancelled(t *testing.T) {
	c, err := nvml.New(nvml.Options{Loader: stubLoader(nil)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Sample(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// --- helpers --------------------------------------------------------

type fakeDev struct {
	name   string
	pciID  string
	gpu    uint32
	mem    uint32
	total  uint64
	free   uint64
	used   uint64
	mw     uint32
	tempC  uint32
	gfxMHz uint32

	nameErr  error
	pciErr   error
	utilErr  error
	memErr   error
	powerErr error
	tempErr  error
	clockErr error
}

type fakeNVML struct {
	devices  []fakeDev
	countErr error
	closed   bool
}

func (f *fakeNVML) DeviceCount() (uint32, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return uint32(len(f.devices)), nil
}

func (f *fakeNVML) DeviceName(idx uint32) (string, error) {
	d := &f.devices[idx]
	if d.nameErr != nil {
		return "", d.nameErr
	}
	return d.name, nil
}

func (f *fakeNVML) DevicePCIID(idx uint32) (string, error) {
	d := &f.devices[idx]
	if d.pciErr != nil {
		return "", d.pciErr
	}
	return d.pciID, nil
}

func (f *fakeNVML) DeviceUtilization(idx uint32) (uint32, uint32, error) {
	d := &f.devices[idx]
	if d.utilErr != nil {
		return 0, 0, d.utilErr
	}
	return d.gpu, d.mem, nil
}

func (f *fakeNVML) DeviceMemory(idx uint32) (uint64, uint64, uint64, error) {
	d := &f.devices[idx]
	if d.memErr != nil {
		return 0, 0, 0, d.memErr
	}
	return d.total, d.free, d.used, nil
}

func (f *fakeNVML) DevicePowerMilliWatts(idx uint32) (uint32, error) {
	d := &f.devices[idx]
	if d.powerErr != nil {
		return 0, d.powerErr
	}
	return d.mw, nil
}

func (f *fakeNVML) DeviceTempC(idx uint32) (uint32, error) {
	d := &f.devices[idx]
	if d.tempErr != nil {
		return 0, d.tempErr
	}
	return d.tempC, nil
}

func (f *fakeNVML) DeviceGraphicsClockMHz(idx uint32) (uint32, error) {
	d := &f.devices[idx]
	if d.clockErr != nil {
		return 0, d.clockErr
	}
	return d.gfxMHz, nil
}

func (f *fakeNVML) Close() error {
	f.closed = true
	return nil
}

func stubLoader(devs []fakeDev) nvml.LoaderFunc {
	return func() (nvml.NVMLI, error) {
		return &fakeNVML{devices: devs}, nil
	}
}
