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
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// BundleOptions wires per-domain collectors into a [Bundle]. Any nil
// field is skipped — its corresponding [sysmsnap.BundleSnapshot] field stays at
// the zero value and no entry appears in [sysmsnap.BundleSnapshot.Errors].
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
	// [sysmsnap.BundleSnapshot.SampledAtUnixMs]. Defaults to [time.Now].
	NowFunc func() time.Time
}

// Bundle orchestrates a set of per-domain collectors, running each
// domain concurrently and collecting per-domain errors into the
// [sysmsnap.BundleSnapshot.Errors] map.
//
// Hard errors — [context.Canceled] and [context.DeadlineExceeded] —
// are returned as the [Sample] function's error and also recorded in
// the per-domain error map. All other domain errors are partial:
// [Sample] returns them in the Errors map but does not abort.
type Bundle struct {
	opts BundleOptions
	// topo is the static CPU containment hierarchy, read once at NewBundle
	// (best-effort) and stamped onto every snapshot so the metric plane carries
	// it (ADR-0090 SD6). nil when the CPU domain was not wired or the one-shot
	// sysfs read failed; the consumer then renders the topology panel as
	// unavailable.
	topo *sysmsnap.Topology
}

// NewBundle returns a Bundle configured by opts. The returned error is
// always nil today; the signature reserves the slot for forward-
// compatibility (capability probing on the wired collectors).
func NewBundle(opts BundleOptions) (inst *Bundle, err error) {
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	inst = &Bundle{opts: opts}
	// Read the static CPU topology once (best-effort). It does not change while
	// the machine runs, so the same pointer rides every snapshot; a read failure
	// (no sysfs, a sandbox) leaves Topology nil. Only attempted when the CPU
	// domain is wired, since topology is CPU-domain data.
	if opts.CPU != nil {
		if topo, terr := cpu.ReadTopology(cpu.TopologyOptions{}); terr == nil {
			inst.topo = &topo
		}
	}
	return
}

// Sample fans out to every wired collector concurrently and returns
// once all goroutines have settled. The first ctx-cancellation seen by
// any goroutine is returned as err; per-domain non-ctx errors do not
// abort the others and are collected in [sysmsnap.BundleSnapshot.Errors].
func (inst *Bundle) Sample(ctx context.Context) (snap sysmsnap.BundleSnapshot, err error) {
	snap.SampledAtUnixMs = inst.opts.NowFunc().UnixMilli()
	snap.Topology = inst.topo
	snap.Errors = map[sysmsnap.Domain]error{}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	captureErr := func(d sysmsnap.Domain, e error) {
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
			captureErr(sysmsnap.DomainCPU, e)
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
			captureErr(sysmsnap.DomainMem, e)
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
			captureErr(sysmsnap.DomainPSI, e)
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
			captureErr(sysmsnap.DomainDisk, e)
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
			captureErr(sysmsnap.DomainNet, e)
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
			captureErr(sysmsnap.DomainBattery, e)
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
			captureErr(sysmsnap.DomainProc, e)
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
			captureErr(sysmsnap.DomainSensors, e)
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
			captureErr(sysmsnap.DomainContainer, e)
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
			captureErr(sysmsnap.DomainGPU, e)
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
