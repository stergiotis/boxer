//go:build llm_generated_opus47

package battery_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/battery"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

func TestSample_Laptop_EnergyUnits(t *testing.T) {
	root := newSysTree(t, map[string]string{
		// BAT0 — energy units, capacity present, discharging.
		"class/power_supply/BAT0/type":              "Battery",
		"class/power_supply/BAT0/present":           "1",
		"class/power_supply/BAT0/capacity":          "75",
		"class/power_supply/BAT0/status":            "Discharging",
		"class/power_supply/BAT0/energy_now":        "30000000",  // 30 Wh
		"class/power_supply/BAT0/energy_full":       "40000000",  // 40 Wh
		"class/power_supply/BAT0/power_now":         "10000000",  // 10 W
		// AC adapter — disconnected.
		"class/power_supply/AC/type":   "Mains",
		"class/power_supply/AC/online": "0",
	})
	c, _ := battery.New(battery.Options{Sys: sysfs.New(root)})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	require.Len(t, snap.Batteries, 1)
	bat := snap.Batteries[0]
	assert.Equal(t, "BAT0", bat.Name)
	assert.Equal(t, "Battery", bat.Type)
	assert.Equal(t, uint8(75), bat.Percent)
	assert.Equal(t, battery.StateDischarging, bat.State)
	assert.InDelta(t, 10.0, bat.PowerWatts, 0.0001)
	// 30 Wh / 10 W = 3 hours = 10800 s.
	assert.Equal(t, int64(10800), bat.SecondsToEmpty)
	assert.Equal(t, int64(-1), bat.SecondsToFull)

	require.Len(t, snap.ACAdapters, 1)
	assert.Equal(t, "AC", snap.ACAdapters[0].Name)
	assert.False(t, snap.ACAdapters[0].Online)
}

func TestSample_Laptop_ChargeUnits_Charging(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/power_supply/BAT1/type":          "Battery",
		"class/power_supply/BAT1/present":       "1",
		"class/power_supply/BAT1/capacity":      "60",
		"class/power_supply/BAT1/status":        "Charging",
		"class/power_supply/BAT1/charge_now":    "3600000",  // 3.6 Ah
		"class/power_supply/BAT1/charge_full":   "6000000",  // 6.0 Ah
		"class/power_supply/BAT1/current_now":   "2000000",  // 2 A
		"class/power_supply/BAT1/voltage_now":   "12000000", // 12 V
		"class/power_supply/AC/type":            "Mains",
		"class/power_supply/AC/online":          "1",
	})
	c, _ := battery.New(battery.Options{Sys: sysfs.New(root)})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	bat := snap.Batteries[0]
	assert.Equal(t, uint8(60), bat.Percent)
	assert.Equal(t, battery.StateCharging, bat.State)
	// power_now absent; W = (current * voltage) = 2A * 12V = 24W.
	assert.InDelta(t, 24.0, bat.PowerWatts, 0.0001)
	// time-to-full: (charge_full - charge_now) / current_now * 3600.
	// = (6_000_000 - 3_600_000) / 2_000_000 * 3600 = 1.2 * 3600 = 4320s.
	assert.Equal(t, int64(4320), bat.SecondsToFull)
	assert.Equal(t, int64(-1), bat.SecondsToEmpty)

	assert.True(t, snap.ACAdapters[0].Online)
}

func TestSample_Full_StatusIsFull(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/power_supply/BAT0/type":     "Battery",
		"class/power_supply/BAT0/present":  "1",
		"class/power_supply/BAT0/capacity": "100",
		"class/power_supply/BAT0/status":   "Full",
	})
	c, _ := battery.New(battery.Options{Sys: sysfs.New(root)})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Batteries, 1)
	assert.Equal(t, battery.StateFull, snap.Batteries[0].State)
	assert.Equal(t, int64(-1), snap.Batteries[0].SecondsToFull)
	assert.Equal(t, int64(-1), snap.Batteries[0].SecondsToEmpty)
}

func TestSample_StatusUnknown_FallsBackToACOnline(t *testing.T) {
	// status reads "Unknown" — collector must consult sibling AC.
	rootCharging := newSysTree(t, map[string]string{
		"class/power_supply/BAT0/type":     "Battery",
		"class/power_supply/BAT0/present":  "1",
		"class/power_supply/BAT0/capacity": "50",
		"class/power_supply/BAT0/status":   "Unknown",
		"class/power_supply/AC/type":       "Mains",
		"class/power_supply/AC/online":     "1",
	})
	c, _ := battery.New(battery.Options{Sys: sysfs.New(rootCharging)})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, battery.StateCharging, snap.Batteries[0].State)

	// AC offline → Discharging.
	rootDischarging := newSysTree(t, map[string]string{
		"class/power_supply/BAT0/type":     "Battery",
		"class/power_supply/BAT0/present":  "1",
		"class/power_supply/BAT0/capacity": "50",
		"class/power_supply/BAT0/status":   "Unknown",
		"class/power_supply/AC/type":       "Mains",
		"class/power_supply/AC/online":     "0",
	})
	c2, _ := battery.New(battery.Options{Sys: sysfs.New(rootDischarging)})
	snap, err = c2.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, battery.StateDischarging, snap.Batteries[0].State)
}

func TestSample_PercentFallback_NoCapacityFile(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/power_supply/BAT0/type":        "Battery",
		"class/power_supply/BAT0/present":     "1",
		"class/power_supply/BAT0/status":      "Discharging",
		"class/power_supply/BAT0/energy_now":  "20000000", // 20 Wh
		"class/power_supply/BAT0/energy_full": "50000000", // 50 Wh
	})
	c, _ := battery.New(battery.Options{Sys: sysfs.New(root)})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Batteries, 1)
	assert.Equal(t, uint8(40), snap.Batteries[0].Percent) // 20/50 = 40%
}

func TestSample_NotPresent_Skipped(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/power_supply/BAT0/type":     "Battery",
		"class/power_supply/BAT0/present":  "0",
		"class/power_supply/BAT0/capacity": "75",
	})
	c, _ := battery.New(battery.Options{Sys: sysfs.New(root)})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Empty(t, snap.Batteries)
}

func TestSample_NoPercentSource_Skipped(t *testing.T) {
	// No capacity, no energy/charge — battery is unusable, must skip.
	root := newSysTree(t, map[string]string{
		"class/power_supply/BAT0/type":    "Battery",
		"class/power_supply/BAT0/present": "1",
		"class/power_supply/BAT0/status":  "Charging",
	})
	c, _ := battery.New(battery.Options{Sys: sysfs.New(root)})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Empty(t, snap.Batteries)
}

func TestSample_Server_NoPowerSupplyDir(t *testing.T) {
	tmp := t.TempDir()
	c, _ := battery.New(battery.Options{Sys: sysfs.New(tmp)})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Empty(t, snap.Batteries)
	assert.Empty(t, snap.ACAdapters)
}

func TestSample_UPS(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/power_supply/UPS0/type":     "UPS",
		"class/power_supply/UPS0/present":  "1",
		"class/power_supply/UPS0/capacity": "92",
		"class/power_supply/UPS0/status":   "Charging",
	})
	c, _ := battery.New(battery.Options{Sys: sysfs.New(root)})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Batteries, 1)
	assert.Equal(t, "UPS", snap.Batteries[0].Type)
	assert.Equal(t, battery.StateCharging, snap.Batteries[0].State)
}

func TestSample_LiveSys_Smoke(t *testing.T) {
	if _, err := os.Stat("/sys/class/power_supply"); err != nil {
		t.Skipf("no /sys/class/power_supply: %v", err)
	}
	c, _ := battery.New(battery.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	for _, b := range snap.Batteries {
		assert.LessOrEqual(t, b.Percent, uint8(100))
		assert.GreaterOrEqual(t, b.PowerWatts, float32(0))
		assert.Less(t, b.PowerWatts, float32(500), "battery %s power implausibly high", b.Name)
		// SecondsToFull / SecondsToEmpty should be -1 or non-negative.
		assert.True(t, b.SecondsToFull == -1 || b.SecondsToFull >= 0)
		assert.True(t, b.SecondsToEmpty == -1 || b.SecondsToEmpty >= 0)
	}
	// Re-sample to keep the rest of the test exercising the post-sample
	// path; ListLiveACAdapters etc. still work.
	snap, err = c.Sample(context.Background())
	require.NoError(t, err)
	for _, b := range snap.Batteries {
		assert.LessOrEqual(t, b.Percent, uint8(100))
	}
}

func TestStateE_String(t *testing.T) {
	assert.Equal(t, "charging", battery.StateCharging.String())
	assert.Equal(t, "discharging", battery.StateDischarging.String())
	assert.Equal(t, "full", battery.StateFull.String())
	assert.Equal(t, "not_charging", battery.StateNotCharging.String())
	assert.Equal(t, "unknown", battery.StateUnknown.String())
}

func newSysTree(t *testing.T, files map[string]string) (root string) {
	t.Helper()
	root = t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body+"\n"), 0o644))
	}
	return
}
