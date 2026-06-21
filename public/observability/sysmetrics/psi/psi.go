package psi

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// Options configures a [Collector].
type Options struct {
	// Proc is the procfs reader; nil defaults to procfs.New("") ("/proc").
	Proc *procfs.Reader
	// NowFunc overrides the sample clock when non-nil.
	NowFunc func() time.Time
}

// CollectorI is the public surface a PSI sampler implements.
type CollectorI interface {
	Sample(ctx context.Context) (sysmsnap.PSISnapshot, error)
}

// Collector samples Linux Pressure Stall Information. It is stateless beyond
// its readers — every field is an absolute reading, so no priming is needed.
type Collector struct {
	proc  *procfs.Reader
	nowFn func() time.Time
}

// New returns a PSI Collector.
func New(opts Options) (inst *Collector, err error) {
	if opts.Proc == nil {
		opts.Proc = procfs.New("")
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	inst = &Collector{proc: opts.Proc, nowFn: opts.NowFunc}
	return
}

var _ CollectorI = (*Collector)(nil)

// Sample reads the three pressure files. A missing /proc/pressure tree is not
// an error — it yields Available=false so callers can degrade gracefully on
// kernels without PSI.
func (inst *Collector) Sample(ctx context.Context) (snap sysmsnap.PSISnapshot, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}

	snap.SampledAtUnixMs = inst.nowFn().UnixMilli()
	cpuR, ok1 := inst.readResource("pressure/cpu")
	memR, ok2 := inst.readResource("pressure/memory")
	ioR, ok3 := inst.readResource("pressure/io")
	snap.CPU, snap.Memory, snap.IO = cpuR, memR, ioR
	snap.Available = ok1 || ok2 || ok3
	return
}

// readResource parses one /proc/pressure/<name> file. ok is false when the
// file is absent or unreadable (PSI disabled, or the resource unsupported).
func (inst *Collector) readResource(rel string) (r sysmsnap.PSIResource, ok bool) {
	raw, e := inst.proc.ReadFile(rel)
	if e != nil {
		return sysmsnap.PSIResource{}, false
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		p := parsePressure(fields[1:])
		switch fields[0] {
		case "some":
			r.Some = p
		case "full":
			r.Full = p
		}
	}
	return r, true
}

// parsePressure reads the "avg10=… avg60=… avg300=… total=…" key=value fields.
func parsePressure(kvs []string) (p sysmsnap.PSIPressure) {
	for _, kv := range kvs {
		k, v, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		switch k {
		case "avg10":
			p.Avg10 = parseFloat32(v)
		case "avg60":
			p.Avg60 = parseFloat32(v)
		case "avg300":
			p.Avg300 = parseFloat32(v)
		case "total":
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				p.TotalUs = n
			}
		}
	}
	return
}

func parseFloat32(s string) (f float32) {
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0
	}
	return float32(v)
}
