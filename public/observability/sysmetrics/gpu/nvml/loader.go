//go:build linux && gpu_nvml

package nvml

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// NVML return codes (subset; see nvml.h NVML_SUCCESS, NVML_ERROR_*).
const (
	nvmlSuccess uint32 = 0
)

// NVML clock and sensor type constants. Matches nvml.h enums.
const (
	nvmlClockGraphics      uint32 = 0
	nvmlTemperatureGPU     uint32 = 0
	nvmlSensorTempCount    uint32 = 1
	nvmlNameBufferSize            = 96 // generous; nvmlDeviceGetName uses NVML_DEVICE_NAME_V2_BUFFER_SIZE = 96
)

// nvmlPciInfo mirrors the binary layout of nvmlPciInfo_t through the
// fields we read. The trailing fields beyond pciSubSystemId are
// dropped — different NVML versions extend the struct, but our offsets
// 0..36 are stable.
type nvmlPciInfo struct {
	BusIDLegacy    [16]byte
	Domain         uint32
	Bus            uint32
	Device         uint32
	PciDeviceID    uint32
	PciSubSystemID uint32
	BusID          [32]byte
}

// nvmlMemory mirrors nvmlMemory_t.
type nvmlMemory struct {
	Total uint64
	Free  uint64
	Used  uint64
}

// nvmlUtilization mirrors nvmlUtilization_t.
type nvmlUtilization struct {
	GPU    uint32
	Memory uint32
}

// realNVML implements [NVMLI] via purego-loaded libnvidia-ml symbols.
type realNVML struct {
	mu     sync.Mutex
	handle uintptr
	closed bool

	nvmlInit                  func() uint32
	nvmlShutdown              func() uint32
	nvmlDeviceGetCount        func(*uint32) uint32
	nvmlDeviceGetHandleByIdx  func(uint32, *uintptr) uint32
	nvmlDeviceGetName         func(uintptr, *byte, uint32) uint32
	nvmlDeviceGetUtilization  func(uintptr, *nvmlUtilization) uint32
	nvmlDeviceGetMemoryInfo   func(uintptr, *nvmlMemory) uint32
	nvmlDeviceGetPowerUsage   func(uintptr, *uint32) uint32
	nvmlDeviceGetTemperature  func(uintptr, uint32, *uint32) uint32
	nvmlDeviceGetClockInfo    func(uintptr, uint32, *uint32) uint32
	nvmlDeviceGetPciInfo      func(uintptr, *nvmlPciInfo) uint32

	deviceCount uint32
	handles     []uintptr
}

// candidateLibs lists the dlopen targets in priority order; matches
// btop_collect.cpp:1235-1238 (libNvAlts).
var candidateLibs = []string{"libnvidia-ml.so.1", "libnvidia-ml.so"}

// DefaultLoader opens libnvidia-ml.so via purego, registers the symbol
// set we need, calls nvmlInit_v2, and caches per-device handles.
func DefaultLoader() (nvml NVMLI, err error) {
	var handle uintptr
	var lastErr error
	for _, lib := range candidateLibs {
		h, derr := purego.Dlopen(lib, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if derr == nil {
			handle = h
			break
		}
		lastErr = derr
	}
	if handle == 0 {
		err = fmt.Errorf("dlopen %v: %w", candidateLibs, lastErr)
		return
	}

	r := &realNVML{handle: handle}
	purego.RegisterLibFunc(&r.nvmlInit, handle, "nvmlInit_v2")
	purego.RegisterLibFunc(&r.nvmlShutdown, handle, "nvmlShutdown")
	purego.RegisterLibFunc(&r.nvmlDeviceGetCount, handle, "nvmlDeviceGetCount_v2")
	purego.RegisterLibFunc(&r.nvmlDeviceGetHandleByIdx, handle, "nvmlDeviceGetHandleByIndex_v2")
	purego.RegisterLibFunc(&r.nvmlDeviceGetName, handle, "nvmlDeviceGetName")
	purego.RegisterLibFunc(&r.nvmlDeviceGetUtilization, handle, "nvmlDeviceGetUtilizationRates")
	purego.RegisterLibFunc(&r.nvmlDeviceGetMemoryInfo, handle, "nvmlDeviceGetMemoryInfo")
	purego.RegisterLibFunc(&r.nvmlDeviceGetPowerUsage, handle, "nvmlDeviceGetPowerUsage")
	purego.RegisterLibFunc(&r.nvmlDeviceGetTemperature, handle, "nvmlDeviceGetTemperature")
	purego.RegisterLibFunc(&r.nvmlDeviceGetClockInfo, handle, "nvmlDeviceGetClockInfo")
	purego.RegisterLibFunc(&r.nvmlDeviceGetPciInfo, handle, "nvmlDeviceGetPciInfo_v3")

	if rc := r.nvmlInit(); rc != nvmlSuccess {
		_ = purego.Dlclose(handle)
		err = fmt.Errorf("nvmlInit_v2: rc=%d", rc)
		return
	}

	if rc := r.nvmlDeviceGetCount(&r.deviceCount); rc != nvmlSuccess {
		_ = r.nvmlShutdown()
		_ = purego.Dlclose(handle)
		err = fmt.Errorf("nvmlDeviceGetCount: rc=%d", rc)
		return
	}
	r.handles = make([]uintptr, r.deviceCount)
	for i := uint32(0); i < r.deviceCount; i++ {
		if rc := r.nvmlDeviceGetHandleByIdx(i, &r.handles[i]); rc != nvmlSuccess {
			_ = r.nvmlShutdown()
			_ = purego.Dlclose(handle)
			err = fmt.Errorf("nvmlDeviceGetHandleByIndex(%d): rc=%d", i, rc)
			return
		}
	}

	nvml = r
	return
}

func (r *realNVML) DeviceCount() (count uint32, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		err = errors.New("nvml: closed")
		return
	}
	return r.deviceCount, nil
}

func (r *realNVML) handleFor(idx uint32) (h uintptr, err error) {
	if r.closed {
		err = errors.New("nvml: closed")
		return
	}
	if idx >= r.deviceCount {
		err = fmt.Errorf("nvml: device index %d out of range (count=%d)", idx, r.deviceCount)
		return
	}
	return r.handles[idx], nil
}

func (r *realNVML) DeviceName(idx uint32) (name string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, herr := r.handleFor(idx)
	if herr != nil {
		err = herr
		return
	}
	var buf [nvmlNameBufferSize]byte
	if rc := r.nvmlDeviceGetName(h, &buf[0], nvmlNameBufferSize); rc != nvmlSuccess {
		err = fmt.Errorf("nvmlDeviceGetName: rc=%d", rc)
		return
	}
	// NUL-terminate.
	end := 0
	for end < len(buf) && buf[end] != 0 {
		end++
	}
	name = string(buf[:end])
	return
}

func (r *realNVML) DevicePCIID(idx uint32) (pciID string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, herr := r.handleFor(idx)
	if herr != nil {
		err = herr
		return
	}
	var info nvmlPciInfo
	if rc := r.nvmlDeviceGetPciInfo(h, &info); rc != nvmlSuccess {
		err = fmt.Errorf("nvmlDeviceGetPciInfo_v3: rc=%d", rc)
		return
	}
	pciID = formatPCIDevice(info.PciDeviceID)
	_ = unsafe.Sizeof(info) // keep unsafe import live
	return
}

func (r *realNVML) DeviceUtilization(idx uint32) (gpuPct, memPct uint32, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, herr := r.handleFor(idx)
	if herr != nil {
		err = herr
		return
	}
	var u nvmlUtilization
	if rc := r.nvmlDeviceGetUtilization(h, &u); rc != nvmlSuccess {
		err = fmt.Errorf("nvmlDeviceGetUtilizationRates: rc=%d", rc)
		return
	}
	gpuPct = u.GPU
	memPct = u.Memory
	return
}

func (r *realNVML) DeviceMemory(idx uint32) (total, free, used uint64, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, herr := r.handleFor(idx)
	if herr != nil {
		err = herr
		return
	}
	var m nvmlMemory
	if rc := r.nvmlDeviceGetMemoryInfo(h, &m); rc != nvmlSuccess {
		err = fmt.Errorf("nvmlDeviceGetMemoryInfo: rc=%d", rc)
		return
	}
	total = m.Total
	free = m.Free
	used = m.Used
	return
}

func (r *realNVML) DevicePowerMilliWatts(idx uint32) (mw uint32, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, herr := r.handleFor(idx)
	if herr != nil {
		err = herr
		return
	}
	if rc := r.nvmlDeviceGetPowerUsage(h, &mw); rc != nvmlSuccess {
		err = fmt.Errorf("nvmlDeviceGetPowerUsage: rc=%d", rc)
		return
	}
	return
}

func (r *realNVML) DeviceTempC(idx uint32) (c uint32, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, herr := r.handleFor(idx)
	if herr != nil {
		err = herr
		return
	}
	if rc := r.nvmlDeviceGetTemperature(h, nvmlTemperatureGPU, &c); rc != nvmlSuccess {
		err = fmt.Errorf("nvmlDeviceGetTemperature: rc=%d", rc)
		return
	}
	return
}

func (r *realNVML) DeviceGraphicsClockMHz(idx uint32) (mhz uint32, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, herr := r.handleFor(idx)
	if herr != nil {
		err = herr
		return
	}
	if rc := r.nvmlDeviceGetClockInfo(h, nvmlClockGraphics, &mhz); rc != nvmlSuccess {
		err = fmt.Errorf("nvmlDeviceGetClockInfo: rc=%d", rc)
		return
	}
	return
}

// Close calls nvmlShutdown and dlcloses the library. Idempotent.
func (r *realNVML) Close() (err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var errs []error
	if r.nvmlShutdown != nil {
		if rc := r.nvmlShutdown(); rc != nvmlSuccess {
			errs = append(errs, fmt.Errorf("nvmlShutdown: rc=%d", rc))
		}
	}
	if r.handle != 0 {
		if cerr := purego.Dlclose(r.handle); cerr != nil {
			errs = append(errs, fmt.Errorf("dlclose: %w", cerr))
		}
		r.handle = 0
	}
	if len(errs) > 0 {
		err = errors.Join(errs...)
	}
	return
}
