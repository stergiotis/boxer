package mem

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// CollectorI is the public surface a memory sampler implements.
type CollectorI interface {
	Sample(ctx context.Context) (snap sysmsnap.MemSnapshot, err error)
}

// Options configures a [Collector].
type Options struct {
	// Proc, when non-nil, overrides the procfs.Reader the Collector reads
	// from. The default is procfs.New("") which resolves "/proc".
	Proc *procfs.Reader

	// EnableZFSArc, when true, reads /proc/spl/kstat/zfs/arcstats and
	// folds the ARC size into Cached / Available per the btop heuristic.
	// When false (the default), ARCSizeBytes / ARCMinBytes stay at zero.
	EnableZFSArc bool

	// NowFunc, when non-nil, overrides the wall-clock used to stamp
	// Snapshot.SampledAtUnixMs. Defaults to [time.Now].
	NowFunc func() time.Time
}

// Collector samples /proc/meminfo and (optionally) ZFS arcstats.
type Collector struct {
	proc   *procfs.Reader
	zfsArc bool
	nowFn  func() time.Time
}

// New returns a memory Collector configured by opts. The returned error
// is always nil today; the signature reserves the slot for forward-
// compatibility (e.g. capability probing in a future version).
func New(opts Options) (inst *Collector, err error) {
	if opts.Proc == nil {
		opts.Proc = procfs.New("")
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	inst = &Collector{
		proc:   opts.Proc,
		zfsArc: opts.EnableZFSArc,
		nowFn:  opts.NowFunc,
	}
	return
}

var _ CollectorI = (*Collector)(nil)

// Sample reads /proc/meminfo (and optionally arcstats) and returns the
// computed Snapshot. The function is cheap (one or two file reads, no
// allocations beyond the byte slices) and may be called at any cadence;
// the Snapshot is point-in-time and carries no implicit prior state.
func (inst *Collector) Sample(ctx context.Context) (snap sysmsnap.MemSnapshot, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}

	var raw []byte
	raw, err = inst.proc.ReadFile("meminfo")
	if err != nil {
		err = eh.Errorf("read /proc/meminfo: %w", err)
		return
	}
	snap, err = parseMeminfo(raw)
	if err != nil {
		return
	}
	snap.SampledAtUnixMs = inst.nowFn().UnixMilli()

	// provenance: btop src/linux/btop_collect.cpp:2177 — old-kernel fallback.
	if snap.AvailableBytes == 0 {
		snap.AvailableBytes = snap.FreeBytes + snap.CachedBytes
	}

	if inst.zfsArc {
		var arc, arcMin uint64
		var arcErr error
		arc, arcMin, arcErr = readArcStats(inst.proc)
		if arcErr == nil {
			snap.ARCSizeBytes = arc
			snap.ARCMinBytes = arcMin
			// provenance: btop src/linux/btop_collect.cpp:2178-2183
			snap.CachedBytes += arc
			if arc > arcMin {
				snap.AvailableBytes += arc - arcMin
			}
		} else if !errors.Is(arcErr, fs.ErrNotExist) {
			// Structural failure (permission, read error). ENOENT is
			// expected when ZFS isn't loaded, and isn't an error.
			err = eh.Errorf("read arcstats: %w", arcErr)
			return
		}
	}

	// provenance: btop src/linux/btop_collect.cpp:2184
	if snap.AvailableBytes <= snap.TotalBytes {
		snap.UsedBytes = snap.TotalBytes - snap.AvailableBytes
	} else {
		snap.UsedBytes = snap.TotalBytes - snap.FreeBytes
	}
	if snap.SwapTotalBytes > 0 {
		snap.SwapUsedBytes = snap.SwapTotalBytes - snap.SwapFreeBytes
	}
	return
}

func parseMeminfo(content []byte) (snap sysmsnap.MemSnapshot, err error) {
	for key, val := range procfs.IterKV(content) {
		v, ok := parseKB(val)
		if !ok {
			continue
		}
		switch string(key) {
		case "MemTotal":
			snap.TotalBytes = v
		case "MemFree":
			snap.FreeBytes = v
		case "MemAvailable":
			snap.AvailableBytes = v
		case "Buffers":
			snap.BuffersBytes = v
		case "Cached":
			snap.CachedBytes = v
		case "SwapTotal":
			snap.SwapTotalBytes = v
		case "SwapFree":
			snap.SwapFreeBytes = v
		}
	}
	if snap.TotalBytes == 0 {
		err = eb.Build().Errorf("missing MemTotal in /proc/meminfo")
		return
	}
	return
}

// parseKB parses a /proc/meminfo value of the form "  N kB" into bytes.
// The "kB" suffix is treated as 1024 (matches kernel convention); a value
// without the suffix is treated as bytes.
func parseKB(val []byte) (n uint64, ok bool) {
	i := 0
	for i < len(val) && (val[i] == ' ' || val[i] == '\t') {
		i++
	}
	j := i
	for j < len(val) && val[j] >= '0' && val[j] <= '9' {
		j++
	}
	if j == i {
		return
	}
	parsed, perr := strconv.ParseUint(string(val[i:j]), 10, 64)
	if perr != nil {
		return
	}
	for j < len(val) && (val[j] == ' ' || val[j] == '\t') {
		j++
	}
	if j+1 < len(val) && (val[j] == 'k' || val[j] == 'K') && val[j+1] == 'B' {
		n = parsed << 10
	} else {
		n = parsed
	}
	ok = true
	return
}

// readArcStats reads /proc/spl/kstat/zfs/arcstats for size and c_min.
//
// arcstats lines have the shape "name type value" — the type column is
// kstat-internal metadata (4 = uint64) we don't care about.
//
// provenance: btop src/linux/btop_collect.cpp:2131-2143.
func readArcStats(proc *procfs.Reader) (size, cMin uint64, err error) {
	var raw []byte
	raw, err = proc.ReadFile("spl/kstat/zfs/arcstats")
	if err != nil {
		return
	}
	for line := range procfs.IterLines(raw) {
		var fields [3][]byte
		var idx int
		for f := range procfs.IterFields(line) {
			if idx == 3 {
				break
			}
			fields[idx] = f
			idx++
		}
		if idx < 3 {
			continue
		}
		v, perr := strconv.ParseUint(string(fields[2]), 10, 64)
		if perr != nil {
			continue
		}
		switch string(fields[0]) {
		case "size":
			size = v
		case "c_min":
			cMin = v
		}
	}
	return
}
