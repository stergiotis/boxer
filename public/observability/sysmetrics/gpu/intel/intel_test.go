//go:build linux && gpu_intel

package intel_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu/intel"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

func TestNew_NoI915Type_ReturnsErrPMUUnavailable(t *testing.T) {
	root := t.TempDir()
	c, err := intel.New(intel.Options{Sys: sysfs.New(root)})
	assert.Nil(t, c)
	require.Error(t, err)
	assert.True(t, errors.Is(err, intel.ErrPMUUnavailable))
}

func TestNew_NoIntelDrm_ReturnsErrPMUUnavailable(t *testing.T) {
	root := newSysTree(t)
	writeI915Type(t, root, "42")
	// /sys/class/drm has only AMD vendor IDs.
	writeDRMCard(t, root, "card0", "0x1002", "0x1234") // AMD vendor

	c, err := intel.New(intel.Options{Sys: sysfs.New(root)})
	assert.Nil(t, c)
	require.Error(t, err)
	assert.True(t, errors.Is(err, intel.ErrPMUUnavailable))
}

func TestNew_OpenerAlwaysFails_ReturnsErrPMUUnavailable(t *testing.T) {
	root := newSysTree(t)
	writeI915Type(t, root, "42")
	writeDRMCard(t, root, "card0", "0x8086", "0x9a49")

	c, err := intel.New(intel.Options{
		Sys: sysfs.New(root),
		Opener: func(uint32, uint64) (intel.Counter, error) {
			return nil, errors.New("EACCES (perf_event_paranoid > 1)")
		},
	})
	assert.Nil(t, c)
	require.Error(t, err)
	assert.True(t, errors.Is(err, intel.ErrPMUUnavailable))
}

func TestSample_FirstCallZeroBusy(t *testing.T) {
	root := newSysTree(t)
	writeI915Type(t, root, "42")
	writeDRMCard(t, root, "card0", "0x8086", "0x9a49")

	op := newFakeOpener()
	op.counter(intel.ConfigEngineBusyTest(0, 0), 0) // render
	op.counter(intel.ConfigEngineBusyTest(1, 0), 0) // copy
	op.counter(intel.ConfigEngineBusyTest(2, 0), 0) // video
	op.counter(intel.ConfigEngineBusyTest(3, 0), 0) // video-enhance
	op.counter(intel.ConfigFreqActualTest(), 0)
	op.counter(intel.ConfigFreqRequestedTest(), 0)

	c, err := intel.New(intel.Options{Sys: sysfs.New(root), Opener: op.Open})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	d := snap.Devices[0]
	assert.Equal(t, "card0", d.Card)
	assert.Equal(t, "0x9a49", d.PCIID)
	assert.Equal(t, "Tiger Lake-H GT2", d.Name)
	assert.Equal(t, uint8(0), d.RenderBusyPercent)
	assert.Equal(t, uint32(0), d.ActualFreqMHz)
}

func TestSample_SecondCall_BusyAndFreqComputed(t *testing.T) {
	root := newSysTree(t)
	writeI915Type(t, root, "42")
	writeDRMCard(t, root, "card0", "0x8086", "0x9a49")

	// Configure each counter to advance between samples.
	// Render: 250 ms busy of a 1-second window → 25%.
	// Copy: 750 ms busy → 75%.
	// Video: 0.
	// Frequency: 1500 MHz×s delta over 1 s → 1500 MHz.
	op := newFakeOpener()
	op.scripted(intel.ConfigEngineBusyTest(0, 0), []uint64{0, 250_000_000})
	op.scripted(intel.ConfigEngineBusyTest(1, 0), []uint64{0, 750_000_000})
	op.scripted(intel.ConfigEngineBusyTest(2, 0), []uint64{0, 0})
	op.scripted(intel.ConfigEngineBusyTest(3, 0), []uint64{0, 0})
	op.scripted(intel.ConfigFreqActualTest(), []uint64{0, 1500})
	op.scripted(intel.ConfigFreqRequestedTest(), []uint64{0, 1700})

	now := time.Unix(0, 0)
	c, err := intel.New(intel.Options{
		Sys:     sysfs.New(root),
		Opener:  op.Open,
		NowFunc: func() time.Time { return now },
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	_, err = c.Sample(context.Background())
	require.NoError(t, err)

	now = now.Add(time.Second)
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	d := snap.Devices[0]
	assert.Equal(t, uint8(25), d.RenderBusyPercent)
	assert.Equal(t, uint8(75), d.CopyBusyPercent)
	assert.Equal(t, uint8(0), d.VideoBusyPercent)
	assert.Equal(t, uint32(1500), d.ActualFreqMHz)
	assert.Equal(t, uint32(1700), d.RequestedFreqMHz)
}

func TestSample_BusyClampsAt100(t *testing.T) {
	root := newSysTree(t)
	writeI915Type(t, root, "42")
	writeDRMCard(t, root, "card0", "0x8086", "0x9a49")

	op := newFakeOpener()
	op.scripted(intel.ConfigEngineBusyTest(0, 0), []uint64{0, 1_500_000_000}) // 1.5s busy in 1s window — implausible
	op.scripted(intel.ConfigEngineBusyTest(1, 0), []uint64{0, 0})
	op.scripted(intel.ConfigEngineBusyTest(2, 0), []uint64{0, 0})
	op.scripted(intel.ConfigEngineBusyTest(3, 0), []uint64{0, 0})

	now := time.Unix(0, 0)
	c, err := intel.New(intel.Options{
		Sys:     sysfs.New(root),
		Opener:  op.Open,
		NowFunc: func() time.Time { return now },
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	_, _ = c.Sample(context.Background())
	now = now.Add(time.Second)
	snap, _ := c.Sample(context.Background())
	assert.Equal(t, uint8(100), snap.Devices[0].RenderBusyPercent, "must clamp at 100")
}

func TestSample_MultipleDevices_SortedByCard(t *testing.T) {
	root := newSysTree(t)
	writeI915Type(t, root, "42")
	writeDRMCard(t, root, "card1", "0x8086", "0x9a49")
	writeDRMCard(t, root, "card0", "0x8086", "0x56a0")

	op := newFakeOpener()
	op.counter(intel.ConfigEngineBusyTest(0, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(1, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(2, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(3, 0), 0)
	op.counter(intel.ConfigFreqActualTest(), 0)
	op.counter(intel.ConfigFreqRequestedTest(), 0)

	c, err := intel.New(intel.Options{Sys: sysfs.New(root), Opener: op.Open})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 2)
	assert.Equal(t, "card0", snap.Devices[0].Card)
	assert.Equal(t, "Arc A770 (DG2-512)", snap.Devices[0].Name)
	assert.Equal(t, "card1", snap.Devices[1].Card)
}

func TestSample_PartialEngineSupport(t *testing.T) {
	// Some kernels don't expose the video-enhance engine on iGPU. Verify
	// missing engines stay at 0% without breaking the snapshot.
	root := newSysTree(t)
	writeI915Type(t, root, "42")
	writeDRMCard(t, root, "card0", "0x8086", "0x9a49")

	op := newFakeOpener()
	op.scripted(intel.ConfigEngineBusyTest(0, 0), []uint64{0, 100_000_000})
	op.scripted(intel.ConfigEngineBusyTest(1, 0), []uint64{0, 100_000_000})
	op.scripted(intel.ConfigEngineBusyTest(2, 0), []uint64{0, 100_000_000})
	// video-enhance not registered → opener returns error → engine skipped.
	op.scripted(intel.ConfigFreqActualTest(), []uint64{0, 0})
	op.scripted(intel.ConfigFreqRequestedTest(), []uint64{0, 0})

	now := time.Unix(0, 0)
	c, err := intel.New(intel.Options{
		Sys:     sysfs.New(root),
		Opener:  op.Open,
		NowFunc: func() time.Time { return now },
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	_, _ = c.Sample(context.Background())
	now = now.Add(time.Second)
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1)
	assert.Equal(t, uint8(10), snap.Devices[0].RenderBusyPercent)
	assert.Equal(t, uint8(0), snap.Devices[0].VideoEnhanceBusyPercent, "missing engine stays at 0")
}

func TestSample_SkipsRenderNodes(t *testing.T) {
	// /sys/class/drm has cardN, renderD128, and card0-DP-1 entries.
	// We accept only the cardN ones.
	root := newSysTree(t)
	writeI915Type(t, root, "42")
	writeDRMCard(t, root, "card0", "0x8086", "0x9a49")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "class/drm/renderD128/device"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "class/drm/renderD128/device/vendor"), []byte("0x8086\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "class/drm/renderD128/device/device"), []byte("0x9a49\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "class/drm/card0-DP-1"), 0o755))

	op := newFakeOpener()
	op.counter(intel.ConfigEngineBusyTest(0, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(1, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(2, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(3, 0), 0)
	op.counter(intel.ConfigFreqActualTest(), 0)
	op.counter(intel.ConfigFreqRequestedTest(), 0)

	c, err := intel.New(intel.Options{Sys: sysfs.New(root), Opener: op.Open})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Devices, 1, "must filter renderD128 and card0-DP-1")
	assert.Equal(t, "card0", snap.Devices[0].Card)
}

func TestClose_ReleasesAllCounters(t *testing.T) {
	root := newSysTree(t)
	writeI915Type(t, root, "42")
	writeDRMCard(t, root, "card0", "0x8086", "0x9a49")

	op := newFakeOpener()
	op.counter(intel.ConfigEngineBusyTest(0, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(1, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(2, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(3, 0), 0)
	op.counter(intel.ConfigFreqActualTest(), 0)
	op.counter(intel.ConfigFreqRequestedTest(), 0)

	c, err := intel.New(intel.Options{Sys: sysfs.New(root), Opener: op.Open})
	require.NoError(t, err)
	require.NoError(t, c.Close())

	for _, fc := range op.opened {
		assert.True(t, fc.closed, "every opened counter must be closed")
	}
}

func TestSample_ContextCancelled(t *testing.T) {
	root := newSysTree(t)
	writeI915Type(t, root, "42")
	writeDRMCard(t, root, "card0", "0x8086", "0x9a49")

	op := newFakeOpener()
	op.counter(intel.ConfigEngineBusyTest(0, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(1, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(2, 0), 0)
	op.counter(intel.ConfigEngineBusyTest(3, 0), 0)
	op.counter(intel.ConfigFreqActualTest(), 0)
	op.counter(intel.ConfigFreqRequestedTest(), 0)

	c, err := intel.New(intel.Options{Sys: sysfs.New(root), Opener: op.Open})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Sample(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestSample_LiveSystem_Smoke(t *testing.T) {
	if _, err := os.Stat("/sys/devices/i915/type"); err != nil {
		t.Skipf("no live i915 PMU: %v", err)
	}
	c, err := intel.New(intel.Options{})
	if errors.Is(err, intel.ErrPMUUnavailable) {
		t.Skipf("i915 present but PMU not openable (perf_event_paranoid?): %v", err)
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	t.Logf("live intel GPU sample: %+v", snap)
	for _, d := range snap.Devices {
		assert.NotEmpty(t, d.Card)
		assert.NotEmpty(t, d.PCIID)
		assert.LessOrEqual(t, d.RenderBusyPercent, uint8(100))
		assert.LessOrEqual(t, d.CopyBusyPercent, uint8(100))
		assert.LessOrEqual(t, d.VideoBusyPercent, uint8(100))
		assert.LessOrEqual(t, d.VideoEnhanceBusyPercent, uint8(100))
		// Frequency 0 is allowed (counter not yet primed); when set,
		// modern Intel iGPUs sit between ~100 MHz idle and ~3 GHz boost.
		if d.ActualFreqMHz != 0 {
			assert.Less(t, d.ActualFreqMHz, uint32(3500), "actual freq implausibly high: %d MHz", d.ActualFreqMHz)
		}
	}
}

func TestEngineE_String(t *testing.T) {
	cases := map[intel.EngineE]string{
		intel.EngineRender:       "render",
		intel.EngineCopy:         "copy",
		intel.EngineVideo:        "video",
		intel.EngineVideoEnhance: "video-enhance",
	}
	for e, want := range cases {
		assert.Equal(t, want, e.String())
	}
}

// --- helpers --------------------------------------------------------

func newSysTree(t *testing.T) (root string) {
	t.Helper()
	return t.TempDir()
}

func writeI915Type(t *testing.T, root, value string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "devices/i915"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "devices/i915/type"), []byte(value+"\n"), 0o644))
}

func writeDRMCard(t *testing.T, root, card, vendor, deviceID string) {
	t.Helper()
	dir := filepath.Join(root, "class/drm", card, "device")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vendor"), []byte(vendor+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "device"), []byte(deviceID+"\n"), 0o644))
}

type fakeCounter struct {
	mu     sync.Mutex
	values []uint64
	idx    int
	closed bool
}

func (fc *fakeCounter) Read() (value uint64, err error) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.closed {
		err = errors.New("counter closed")
		return
	}
	if fc.idx >= len(fc.values) {
		// Static counter: keep returning the last value rather than EOF.
		if len(fc.values) == 0 {
			return
		}
		value = fc.values[len(fc.values)-1]
		return
	}
	value = fc.values[fc.idx]
	fc.idx++
	return
}

func (fc *fakeCounter) Close() (err error) {
	fc.mu.Lock()
	fc.closed = true
	fc.mu.Unlock()
	return
}

type fakeOpener struct {
	mu       sync.Mutex
	counters map[uint64]*fakeCounter
	opened   []*fakeCounter
}

func newFakeOpener() *fakeOpener {
	return &fakeOpener{counters: map[uint64]*fakeCounter{}}
}

func (fo *fakeOpener) counter(config, val uint64) {
	fo.counters[config] = &fakeCounter{values: []uint64{val}}
}

func (fo *fakeOpener) scripted(config uint64, values []uint64) {
	fo.counters[config] = &fakeCounter{values: values}
}

func (fo *fakeOpener) Open(pmuType uint32, config uint64) (counter intel.Counter, err error) {
	fo.mu.Lock()
	defer fo.mu.Unlock()
	c, ok := fo.counters[config]
	if !ok {
		err = fmt.Errorf("no fake counter for config 0x%x", config)
		return
	}
	fo.opened = append(fo.opened, c)
	return c, nil
}
