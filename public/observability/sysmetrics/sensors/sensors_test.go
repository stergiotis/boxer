package sensors_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sensors"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

func TestSample_Coretemp(t *testing.T) {
	c, _ := sensors.New(sensors.Options{Sys: sysfs.New("testdata/coretemp")})
	readings, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, readings, 3)

	byName := map[string]sysmsnap.TempReading{}
	for _, r := range readings {
		byName[r.Name] = r
	}

	pkg, ok := byName["coretemp/Package id 0"]
	require.True(t, ok, "Package id 0 reading present")
	assert.InDelta(t, 45.0, pkg.TempC, 0.001)
	assert.InDelta(t, 100.0, pkg.CriticalC, 0.001)
	assert.True(t, pkg.KindCPUPackage)
	assert.False(t, pkg.KindCPUCore)

	core, ok := byName["coretemp/Core 0"]
	require.True(t, ok)
	assert.True(t, core.KindCPUCore)
	assert.False(t, core.KindCPUPackage)
	assert.InDelta(t, 40.0, core.TempC, 0.001)

	systin, ok := byName["nct6776/SYSTIN"]
	require.True(t, ok)
	assert.False(t, systin.KindCPUPackage)
	assert.False(t, systin.KindCPUCore)
}

func TestSample_DeviceSubdirFallback(t *testing.T) {
	c, _ := sensors.New(sensors.Options{Sys: sysfs.New("testdata/device_subdir")})
	readings, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, readings, 2)

	byName := map[string]sysmsnap.TempReading{}
	for _, r := range readings {
		byName[r.Name] = r
	}
	// Tdie matches CPU package heuristic.
	tdie := byName["k10temp/Tdie"]
	assert.True(t, tdie.KindCPUPackage)
	assert.InDelta(t, 50.0, tdie.TempC, 0.001)

	// Tctl is not a CPU package label per btop (Tctl != Tdie).
	tctl := byName["k10temp/Tctl"]
	assert.False(t, tctl.KindCPUPackage)
	assert.InDelta(t, 55.0, tctl.TempC, 0.001)
}

func TestSample_LabelFallback(t *testing.T) {
	c, _ := sensors.New(sensors.Options{Sys: sysfs.New("testdata/no_label")})
	readings, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, readings, 1)
	assert.Equal(t, "mychip/temp3", readings[0].Name, "missing _label must fall back to derived 'tempN'")
}

func TestSample_CritFallback(t *testing.T) {
	c, _ := sensors.New(sensors.Options{Sys: sysfs.New("testdata/no_label")})
	readings, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, readings, 1)
	assert.Equal(t, sensors.DefaultCriticalC, readings[0].CriticalC, "missing _crit must yield 95°C default")
}

func TestSample_NoHwmonClass(t *testing.T) {
	tmp := t.TempDir()
	c, _ := sensors.New(sensors.Options{Sys: sysfs.New(tmp)})
	readings, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Empty(t, readings)
}

func TestSample_LiveSys_Smoke(t *testing.T) {
	if _, statErr := os.Stat("/sys/class/hwmon"); statErr != nil {
		t.Skipf("no /sys/class/hwmon: %v", statErr)
	}
	c, _ := sensors.New(sensors.Options{})
	readings, err := c.Sample(context.Background())
	require.NoError(t, err)
	for _, r := range readings {
		assert.NotEmpty(t, r.Name)
		assert.NotEmpty(t, r.Path)
		// Plausibility: silicon temperature must be between cryogenic
		// (-40°C is an aggressive automotive lower bound) and the
		// silicon-physical upper limit (~150°C; thermal-throttle
		// protection kicks in well below).
		assert.Greater(t, r.TempC, float32(-40), "sensor %s temp %v°C implausibly low", r.Name, r.TempC)
		assert.Less(t, r.TempC, float32(150), "sensor %s temp %v°C implausibly high", r.Name, r.TempC)
		// CriticalC defaults to 95; some sensors report 105 or 110.
		assert.GreaterOrEqual(t, r.CriticalC, float32(50))
		assert.LessOrEqual(t, r.CriticalC, float32(120))
	}
}

func TestAll_BreakEarly(t *testing.T) {
	c, _ := sensors.New(sensors.Options{Sys: sysfs.New("testdata/coretemp")})
	var got []sysmsnap.TempReading
	for r, err := range c.All(context.Background()) {
		require.NoError(t, err)
		got = append(got, r)
		if len(got) == 1 {
			break
		}
	}
	assert.Len(t, got, 1)
}
