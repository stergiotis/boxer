//go:build linux && gpu_intel

package intel

import (
	"encoding/binary"
	"errors"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// PMU config-id encoding constants from drivers/gpu/drm/i915/i915_pmu.h.
//
//   config = (class << I915_PMU_CLASS_SHIFT)
//          | (instance << I915_PMU_SAMPLE_BITS)
//          | sample_kind
//
// where:
//   I915_PMU_SAMPLE_BITS = 4
//   I915_PMU_SAMPLE_INSTANCE_BITS = 8
//   I915_PMU_CLASS_SHIFT = SAMPLE_BITS + SAMPLE_INSTANCE_BITS = 12
//
// Sample kinds: 0=BUSY, 1=WAIT, 2=SEMA. We expose only BUSY.
//
// Non-engine ("other") counters live above the highest engine config id:
//   ___I915_PMU_OTHER(gt, x) = ((__I915_PMU_ENGINE(0xff,0xff,0xf) + 1) + x) | (gt << 60)
//
// I915_PMU_ENGINE(0xff, 0xff, 0xf) = (0xff << 12) | (0xff << 4) | 0xf = 0xfffff,
// so the "other base" is 0x100000.
const (
	configFreqActual    uint64 = 0x100000
	configFreqRequested uint64 = 0x100001
	configRC6Residency  uint64 = 0x100003

	i915SampleBits     uint64 = 4
	i915ClassShift     uint64 = 12
	i915SampleBusy     uint64 = 0
)

// configEngineBusy returns the perf config id for the engine-busy
// counter on (class, instance) of GT 0.
//
// EngineRender / Copy / Video / VideoEnhance map to classes 0..3.
func configEngineBusy(class, instance uint64) (config uint64) {
	return (class << i915ClassShift) | (instance << i915SampleBits) | i915SampleBusy
}

// DefaultCounterOpener opens an i915 PMU perf event via [unix.PerfEventOpen].
//
// The kernel surfaces EACCES for paranoid > 1, ENODEV when i915 isn't
// loaded, and EINVAL for an unsupported config — the caller (typically
// [New]) treats any of these as "this counter not available" and keeps
// going with the rest.
func DefaultCounterOpener(pmuType uint32, config uint64) (counter Counter, err error) {
	attr := unix.PerfEventAttr{
		Type:   pmuType,
		Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
		Config: config,
	}
	fd, oerr := unix.PerfEventOpen(&attr, -1, 0, -1, 0)
	if oerr != nil {
		err = eb.Build().
			Uint32("pmuType", pmuType).
			Uint64("config", config).
			Errorf("perf_event_open: %w", oerr)
		return
	}
	counter = &perfCounter{fd: fd}
	return
}

type perfCounter struct {
	fd     int
	closed bool
}

func (inst *perfCounter) Read() (value uint64, err error) {
	if inst.closed {
		err = errors.New("perf counter closed")
		return
	}
	var buf [8]byte
	n, rerr := unix.Read(inst.fd, buf[:])
	if rerr != nil {
		err = eb.Build().Int("fd", inst.fd).Errorf("read perf counter: %w", rerr)
		return
	}
	if n != 8 {
		err = eb.Build().Int("fd", inst.fd).Int("n", n).Errorf("short perf counter read")
		return
	}
	value = binary.LittleEndian.Uint64(buf[:])
	return
}

func (inst *perfCounter) Close() (err error) {
	if inst.closed {
		return nil
	}
	inst.closed = true
	cerr := unix.Close(inst.fd)
	if cerr != nil {
		err = eb.Build().Int("fd", inst.fd).Errorf("close perf counter: %w", cerr)
	}
	return
}
