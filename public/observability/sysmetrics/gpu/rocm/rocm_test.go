//go:build linux && gpu_rocm && llm_generated_opus47

package rocm_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu/rocm"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

func TestSample_FullSysfs(t *testing.T) {
	root := newSysTree(t)
	writeAMDCard(t, root, "card0", amdFixture{
		pciID:      "0x1586",
		busyPct:    "37",
		vramTotal:  "17179869184", // 16 GiB
		vramUsed:   "1073741824",  // 1 GiB
		hwmonName:  "amdgpu",
		tempMilliC: "65000",       // 65°C
		powerUW:    "12000000",    // 12 W
		freqHz:     "1500000000",  // 1500 MHz
	})

	c, err := rocm.New(rocm.Options{Sys: sysfs.New(root)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	d := snap.Devices[0]
	assert.Equal(t, "card0", d.Card)
	assert.Equal(t, "0x1586", d.PCIID)
	assert.Equal(t, "Strix Halo (Radeon 8060S)", d.Name)
	assert.Equal(t, uint8(37), d.BusyPercent)
	assert.Equal(t, uint64(17179869184), d.MemoryTotalBytes)
	assert.Equal(t, uint64(1073741824), d.MemoryUsedBytes)
	assert.InDelta(t, 65.0, d.TempC, 0.001)
	assert.InDelta(t, 12.0, d.PowerWatts, 0.001)
	assert.Equal(t, uint32(1500), d.GraphicsClockMHz)
}

func TestSample_FreqFallback_FromPpDpmSclk(t *testing.T) {
	root := newSysTree(t)
	writeAMDCard(t, root, "card0", amdFixture{
		pciID:    "0x1586",
		ppDpmSclk: "0: 600Mhz \n1: 845Mhz *\n2: 2900Mhz \n",
	})
	c, err := rocm.New(rocm.Options{Sys: sysfs.New(root)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	assert.Equal(t, uint32(845), snap.Devices[0].GraphicsClockMHz)
}

func TestSample_NonAMDVendor_Skipped(t *testing.T) {
	root := newSysTree(t)
	writeRawCard(t, root, "card0", "0x10de", "0x2230") // NVIDIA
	writeAMDCard(t, root, "card1", amdFixture{pciID: "0x1586"})

	c, err := rocm.New(rocm.Options{Sys: sysfs.New(root)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1, "only the AMD card should be returned")
	assert.Equal(t, "card1", snap.Devices[0].Card)
}

func TestSample_RenderNodes_Skipped(t *testing.T) {
	root := newSysTree(t)
	writeAMDCard(t, root, "card0", amdFixture{pciID: "0x1586"})
	require.NoError(t, os.MkdirAll(filepath.Join(root, "class/drm/renderD128/device"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "class/drm/renderD128/device/vendor"), []byte("0x1002\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "class/drm/renderD128/device/device"), []byte("0x1586\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "class/drm/card0-DP-1"), 0o755))

	c, err := rocm.New(rocm.Options{Sys: sysfs.New(root)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	assert.Equal(t, "card0", snap.Devices[0].Card)
}

func TestSample_NoDRMClass_EmptySnapshot(t *testing.T) {
	c, err := rocm.New(rocm.Options{Sys: sysfs.New(t.TempDir())})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Empty(t, snap.Devices)
}

func TestSample_UnknownPCIID_FallsBackToGenericName(t *testing.T) {
	root := newSysTree(t)
	writeAMDCard(t, root, "card0", amdFixture{pciID: "0xdead"})

	c, err := rocm.New(rocm.Options{Sys: sysfs.New(root)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	assert.Equal(t, "AMD Graphics", snap.Devices[0].Name)
}

func TestSample_PartialFields_DontAbort(t *testing.T) {
	// Only vendor + device + busy are present; mem/hwmon missing.
	root := newSysTree(t)
	writeRawCard(t, root, "card0", "0x1002", "0x1586")
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "class/drm/card0/device/gpu_busy_percent"),
		[]byte("50\n"), 0o644))

	c, err := rocm.New(rocm.Options{Sys: sysfs.New(root)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	assert.Equal(t, uint8(50), snap.Devices[0].BusyPercent)
	assert.Equal(t, uint64(0), snap.Devices[0].MemoryTotalBytes)
	assert.Equal(t, float32(0), snap.Devices[0].TempC)
}

func TestGenericSampler_RoundTrip(t *testing.T) {
	root := newSysTree(t)
	writeAMDCard(t, root, "card0", amdFixture{
		pciID:    "0x1586",
		busyPct:  "20",
		vramUsed: "1024", vramTotal: "4096",
		hwmonName: "amdgpu",
		tempMilliC: "55000",
	})

	g, err := rocm.NewGenericSampler(rocm.Options{Sys: sysfs.New(root)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = g.Close() })

	var _ gpu.SamplerI = g

	snap, err := g.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	assert.Equal(t, "amd", snap.Devices[0].Vendor)
	assert.Equal(t, uint8(20), snap.Devices[0].BusyPercent)
	assert.InDelta(t, 55.0, snap.Devices[0].TempC, 0.001)
}

func TestSample_LiveSystem_Smoke(t *testing.T) {
	c, err := rocm.New(rocm.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	for _, d := range snap.Devices {
		assert.NotEmpty(t, d.Card)
		assert.NotEmpty(t, d.PCIID)
		assert.LessOrEqual(t, d.BusyPercent, uint8(100))
		// VRAM consistency.
		assert.LessOrEqual(t, d.MemoryUsedBytes, d.MemoryTotalBytes,
			"device %s used VRAM > total", d.Card)
		// Temperature within silicon bounds (allow 0 when sensor missing).
		if d.TempC != 0 {
			assert.Greater(t, d.TempC, float32(-40))
			assert.Less(t, d.TempC, float32(150))
		}
		// Power up to 600 W covers consumer + Instinct datacenter.
		assert.Less(t, d.PowerWatts, float32(600), "device %s power implausibly high", d.Card)
		// Graphics clock up to 4 GHz covers RDNA 4 boost.
		assert.Less(t, d.GraphicsClockMHz, uint32(4000), "device %s clock implausibly high", d.Card)
	}
}

func TestSample_ContextCancelled(t *testing.T) {
	c, err := rocm.New(rocm.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Sample(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// --- helpers --------------------------------------------------------

type amdFixture struct {
	pciID      string
	busyPct    string
	vramTotal  string
	vramUsed   string
	hwmonName  string
	tempMilliC string
	powerUW    string
	freqHz     string
	ppDpmSclk  string
}

func newSysTree(t *testing.T) (root string) {
	t.Helper()
	return t.TempDir()
}

func writeRawCard(t *testing.T, root, card, vendor, deviceID string) {
	t.Helper()
	dir := filepath.Join(root, "class/drm", card, "device")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vendor"), []byte(vendor+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "device"), []byte(deviceID+"\n"), 0o644))
}

func writeAMDCard(t *testing.T, root, card string, f amdFixture) {
	t.Helper()
	if f.pciID == "" {
		f.pciID = "0x1586"
	}
	writeRawCard(t, root, card, "0x1002", f.pciID)
	base := filepath.Join(root, "class/drm", card, "device")

	if f.busyPct != "" {
		require.NoError(t, os.WriteFile(filepath.Join(base, "gpu_busy_percent"), []byte(f.busyPct+"\n"), 0o644))
	}
	if f.vramTotal != "" {
		require.NoError(t, os.WriteFile(filepath.Join(base, "mem_info_vram_total"), []byte(f.vramTotal+"\n"), 0o644))
	}
	if f.vramUsed != "" {
		require.NoError(t, os.WriteFile(filepath.Join(base, "mem_info_vram_used"), []byte(f.vramUsed+"\n"), 0o644))
	}
	if f.ppDpmSclk != "" {
		require.NoError(t, os.WriteFile(filepath.Join(base, "pp_dpm_sclk"), []byte(f.ppDpmSclk), 0o644))
	}
	if f.hwmonName != "" || f.tempMilliC != "" || f.powerUW != "" || f.freqHz != "" {
		hwmonDir := filepath.Join(base, "hwmon", "hwmon4")
		require.NoError(t, os.MkdirAll(hwmonDir, 0o755))
		if f.hwmonName != "" {
			require.NoError(t, os.WriteFile(filepath.Join(hwmonDir, "name"), []byte(f.hwmonName+"\n"), 0o644))
		}
		if f.tempMilliC != "" {
			require.NoError(t, os.WriteFile(filepath.Join(hwmonDir, "temp1_input"), []byte(f.tempMilliC+"\n"), 0o644))
		}
		if f.powerUW != "" {
			require.NoError(t, os.WriteFile(filepath.Join(hwmonDir, "power1_average"), []byte(f.powerUW+"\n"), 0o644))
		}
		if f.freqHz != "" {
			require.NoError(t, os.WriteFile(filepath.Join(hwmonDir, "freq1_input"), []byte(f.freqHz+"\n"), 0o644))
		}
	}
}
