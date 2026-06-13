package sysmetrics

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/battery"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/container"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/cpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/disk"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/mem"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/net"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/psi"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sensors"
)

// Domain identifies one collector slot in [BundleOptions]. Used as the
// key in [BundleSnapshot.Errors].
type Domain string

const (
	DomainCPU       Domain = "cpu"
	DomainMem       Domain = "mem"
	DomainDisk      Domain = "disk"
	DomainNet       Domain = "net"
	DomainBattery   Domain = "battery"
	DomainProc      Domain = "proc"
	DomainSensors   Domain = "sensors"
	DomainContainer Domain = "container"
	DomainGPU       Domain = "gpu"
	DomainPSI       Domain = "psi"
)

// BundleOptions wires per-domain collectors into a [Bundle]. Any nil
// field is skipped — its corresponding [BundleSnapshot] field stays at
// the zero value and no entry appears in [BundleSnapshot.Errors].
type BundleOptions struct {
	CPU       *cpu.Collector
	Mem       *mem.Collector
	Disk      *disk.Collector
	Net       *net.Collector
	Battery   *battery.Collector
	Proc      *proc.Collector
	Sensors   sensors.CollectorI
	Container container.DetectorI

	// GPU is the vendor-neutral GPU sampler. Vendor-specific adapters
	// such as intel.GenericSampler (build-tag `gpu_intel`) satisfy this
	// interface; a default-tagged build leaves the slot empty and the
	// snapshot's GPU field stays nil. The Bundle's Close releases
	// resources held by the wired GPU sampler.
	GPU gpu.SamplerI

	// PSI is the Pressure Stall Information sampler (/proc/pressure).
	PSI *psi.Collector

	// NowFunc, when non-nil, overrides the wall clock used to stamp
	// [BundleSnapshot.SampledAtUnixMs]. Defaults to [time.Now].
	NowFunc func() time.Time
}

// BundleSnapshot is the per-domain union of all configured collectors'
// outputs. Fields are nil / empty when their collector was not wired.
type BundleSnapshot struct {
	SampledAtUnixMs int64

	CPU       *cpu.Snapshot
	Mem       *mem.Snapshot
	Disk      *disk.Snapshot
	Net       *net.Snapshot
	Battery   *battery.Snapshot
	Container *container.Info
	GPU       *gpu.Snapshot
	PSI       *psi.Snapshot

	// Procs is the slice form of the process table. Empty when the proc
	// collector was not wired.
	Procs []proc.Info

	// Sensors is the temperature reading list. Empty when the sensors
	// collector was not wired (or when the cpu collector is wired and
	// already includes per-cpu temperatures inline).
	Sensors []sensors.TempReading

	// Errors maps the domain that failed to its error. Empty (not nil)
	// when every wired collector succeeded; consumers can iterate to
	// log per-domain failures without aborting on partial success.
	Errors map[Domain]error
}

// Bundle orchestrates a set of per-domain collectors, running each
// domain concurrently and collecting per-domain errors into the
// [BundleSnapshot.Errors] map.
//
// Hard errors — [context.Canceled] and [context.DeadlineExceeded] —
// are returned as the [Sample] function's error and also recorded in
// the per-domain error map. All other domain errors are partial:
// [Sample] returns them in the Errors map but does not abort.
type Bundle struct {
	opts BundleOptions
}

// NewBundle returns a Bundle configured by opts. The returned error is
// always nil today; the signature reserves the slot for forward-
// compatibility (capability probing on the wired collectors).
func NewBundle(opts BundleOptions) (inst *Bundle, err error) {
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	inst = &Bundle{opts: opts}
	return
}

// Sample fans out to every wired collector concurrently and returns
// once all goroutines have settled. The first ctx-cancellation seen by
// any goroutine is returned as err; per-domain non-ctx errors do not
// abort the others and are collected in [BundleSnapshot.Errors].
func (inst *Bundle) Sample(ctx context.Context) (snap BundleSnapshot, err error) {
	snap.SampledAtUnixMs = inst.opts.NowFunc().UnixMilli()
	snap.Errors = map[Domain]error{}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	captureErr := func(d Domain, e error) {
		if e == nil {
			return
		}
		mu.Lock()
		snap.Errors[d] = e
		mu.Unlock()
	}

	if inst.opts.CPU != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := inst.opts.CPU.Sample(ctx)
			if e == nil {
				mu.Lock()
				snap.CPU = &s
				mu.Unlock()
			}
			captureErr(DomainCPU, e)
		}()
	}
	if inst.opts.Mem != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := inst.opts.Mem.Sample(ctx)
			if e == nil {
				mu.Lock()
				snap.Mem = &s
				mu.Unlock()
			}
			captureErr(DomainMem, e)
		}()
	}
	if inst.opts.PSI != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := inst.opts.PSI.Sample(ctx)
			if e == nil {
				mu.Lock()
				snap.PSI = &s
				mu.Unlock()
			}
			captureErr(DomainPSI, e)
		}()
	}
	if inst.opts.Disk != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := inst.opts.Disk.Sample(ctx)
			if e == nil {
				mu.Lock()
				snap.Disk = &s
				mu.Unlock()
			}
			captureErr(DomainDisk, e)
		}()
	}
	if inst.opts.Net != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := inst.opts.Net.Sample(ctx)
			if e == nil {
				mu.Lock()
				snap.Net = &s
				mu.Unlock()
			}
			captureErr(DomainNet, e)
		}()
	}
	if inst.opts.Battery != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := inst.opts.Battery.Sample(ctx)
			if e == nil {
				mu.Lock()
				snap.Battery = &s
				mu.Unlock()
			}
			captureErr(DomainBattery, e)
		}()
	}
	if inst.opts.Proc != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			infos, e := inst.opts.Proc.Sample(ctx)
			if e == nil {
				mu.Lock()
				snap.Procs = infos
				mu.Unlock()
			}
			captureErr(DomainProc, e)
		}()
	}
	if inst.opts.Sensors != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			readings, e := inst.opts.Sensors.Sample(ctx)
			if e == nil {
				mu.Lock()
				snap.Sensors = readings
				mu.Unlock()
			}
			captureErr(DomainSensors, e)
		}()
	}
	if inst.opts.Container != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info, e := inst.opts.Container.Detect(ctx)
			if e == nil {
				mu.Lock()
				snap.Container = &info
				mu.Unlock()
			}
			captureErr(DomainContainer, e)
		}()
	}
	if inst.opts.GPU != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := inst.opts.GPU.Sample(ctx)
			if e == nil {
				mu.Lock()
				snap.GPU = &s
				mu.Unlock()
			}
			captureErr(DomainGPU, e)
		}()
	}

	wg.Wait()

	// Surface ctx errors as the function-level err.
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = ctxErr
	}
	return
}

// Close releases resources held by any wired collector that implements
// [io.Closer]. Today only [gpu.SamplerI] requires this (perf-event fds
// for Intel, NVML/ROCm-SMI handles for the future M6 vendor packages),
// but the implementation walks every slot so future per-domain Closers
// participate without a Bundle code change.
//
// When multiple closers fail, the returned error is the [errors.Join]
// of every failure.
func (inst *Bundle) Close() (err error) {
	candidates := []any{
		inst.opts.CPU,
		inst.opts.Mem,
		inst.opts.Disk,
		inst.opts.Net,
		inst.opts.Battery,
		inst.opts.Proc,
		inst.opts.Sensors,
		inst.opts.Container,
		inst.opts.GPU,
		inst.opts.PSI,
	}
	var errs []error
	for _, c := range candidates {
		if c == nil {
			continue
		}
		if closer, ok := c.(io.Closer); ok {
			if cerr := closer.Close(); cerr != nil {
				errs = append(errs, cerr)
			}
		}
	}
	if len(errs) > 0 {
		err = errors.Join(errs...)
	}
	return
}
