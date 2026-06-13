//go:build linux && gpu_intel

package intel_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu/intel"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

func TestGeneric_BusyTakesEngineMax(t *testing.T) {
	s := intel.Snapshot{
		SampledAtUnixMs: 12345,
		Devices: []intel.Device{{
			Card:                    "card0",
			PCIID:                   "0x9a49",
			Name:                    "Tiger Lake-H GT2",
			RenderBusyPercent:       30,
			CopyBusyPercent:         60,
			VideoBusyPercent:        45,
			VideoEnhanceBusyPercent: 10,
			ActualFreqMHz:           1500,
			RequestedFreqMHz:        1700,
		}},
	}
	g := s.Generic()

	require.Len(t, g.Devices, 1)
	d := g.Devices[0]
	assert.Equal(t, "intel", d.Vendor)
	assert.Equal(t, int32(0), d.Index)
	assert.Equal(t, "Tiger Lake-H GT2", d.Name)
	assert.Equal(t, "0x9a49", d.PCIID)
	assert.Equal(t, uint8(60), d.BusyPercent, "must be max(30,60,45,10)=60")
	assert.Equal(t, uint32(1500), d.FreqMHz, "passes through ActualFreqMHz")
	assert.Equal(t, int64(12345), g.SampledAtUnixMs)
	// Per-engine fields without a generic equivalent are zero.
	assert.Equal(t, uint64(0), d.MemoryUsedBytes)
	assert.Equal(t, float32(0), d.PowerWatts)
}

func TestGeneric_MultipleDevicesPreserveIndex(t *testing.T) {
	s := intel.Snapshot{Devices: []intel.Device{
		{Card: "card0", PCIID: "0x9a49"},
		{Card: "card1", PCIID: "0x56a0"},
	}}
	g := s.Generic()
	require.Len(t, g.Devices, 2)
	assert.Equal(t, int32(0), g.Devices[0].Index)
	assert.Equal(t, int32(1), g.Devices[1].Index)
}

func TestGenericSampler_SatisfiesSamplerI(t *testing.T) {
	root := newSysTreeForGeneric(t)
	op := newFakeOpenerForGeneric()

	s, err := intel.NewGenericSampler(intel.Options{Sys: sysfs.New(root), Opener: op.Open})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	var _ gpu.SamplerI = s

	snap, err := s.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	assert.Equal(t, "intel", snap.Devices[0].Vendor)
}

func TestGenericSampler_New_PropagatesPMUUnavailable(t *testing.T) {
	_, err := intel.NewGenericSampler(intel.Options{Sys: sysfs.New(t.TempDir())})
	require.Error(t, err)
	assert.True(t, errors.Is(err, intel.ErrPMUUnavailable))
}

// helpers to keep this file self-contained

func newSysTreeForGeneric(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	writeI915Type(t, root, "42")
	writeDRMCard(t, root, "card0", "0x8086", "0x9a49")
	return
}

func newFakeOpenerForGeneric() *fakeOpener {
	op := newFakeOpener()
	op.counter(intel.ConfigEngineBusyTest(0, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(1, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(2, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(3, 0), 0)
	op.counter(intel.ConfigFreqActualTest(), 0)
	op.counter(intel.ConfigFreqRequestedTest(), 0)
	return op
}
