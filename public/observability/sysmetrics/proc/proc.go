//go:build llm_generated_opus47

package proc

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"iter"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
)

const (
	// DefaultClkTckHz is the kernel USER_HZ tick rate. The Linux default
	// has been 100 since 2.4.x; tunables (CONFIG_HZ_250, CONFIG_HZ_1000)
	// are rare and the consumer can override via [Options.ClkTckHz].
	DefaultClkTckHz uint32 = 100

	// KernelThreadDaemonPID is the well-known kthreadd pid; any process
	// with PPid == 2 is a kernel thread.
	KernelThreadDaemonPID uint32 = 2

	// CmdMaxBytes is the per-process limit on Cmd field length. Matches
	// btop's 1000-byte cap (btop_collect.cpp:3126) so very long
	// command lines do not blow up Snapshot memory.
	CmdMaxBytes = 1000
)

// Info is one process's sample.
type Info struct {
	PID  uint32
	PPID uint32

	// Name is /proc/[pid]/comm — the kernel-truncated binary name (15
	// chars max).
	Name string

	// Cmd is /proc/[pid]/cmdline with NUL separators converted to spaces
	// and trailing NUL stripped. Empty for kernel threads.
	Cmd string

	// State is the single-letter Linux process state (R/S/D/Z/T/I/...).
	State byte

	UID  uint32
	GID  uint32
	User string // resolved name; empty when uid is unknown to NSS

	// StartedAtUnixMs is the wall-clock process start time, derived from
	// /proc/uptime + /proc/[pid]/stat starttime.
	StartedAtUnixMs int64

	// CPUPercent is the per-CPU CPU usage. A process pegging one core
	// reads 100; pegging N cores reads N*100. 0 on first sample (no
	// prior tick to delta against).
	//
	// Formula: deltaPidTicks * 100 * NumCPUs / deltaGlobalTicks
	// (matches btop src/linux/btop_collect.cpp:3250).
	CPUPercent float32

	RSSBytes    uint64
	VMSizeBytes uint64

	NumThreads int32
	Nice       int32
	Priority   int32

	// KernelThread is true when PID == 2 or PPID == 2.
	KernelThread bool
}

// CollectorI is the public surface a process-table sampler implements.
type CollectorI interface {
	All(ctx context.Context) iter.Seq2[Info, error]
	Sample(ctx context.Context) (infos []Info, err error)
}

// UserLookupFunc maps a numeric uid to a username. The default
// implementation uses [os/user.LookupId] (Linux: pure-Go NSS via
// /etc/passwd, /etc/nsswitch.conf, sssd if configured).
type UserLookupFunc func(uid uint32) (name string)

// Options configures a [Collector].
type Options struct {
	Proc *procfs.Reader

	// NowFunc, when non-nil, overrides the wall+monotonic clock.
	NowFunc func() time.Time

	// UserLookup, when non-nil, overrides uid → username resolution.
	UserLookup UserLookupFunc

	// ClkTckHz is the kernel USER_HZ tick rate. 0 means use
	// [DefaultClkTckHz] (100).
	ClkTckHz uint32

	// PageSize is the kernel page size in bytes. 0 means use
	// [os.Getpagesize].
	PageSize uint32

	// NumCPUs is the logical CPU count used in the per-CPU view of
	// CPUPercent. 0 means use [runtime.NumCPU].
	NumCPUs int32

	// MaxProcs caps the number of processes returned per [Collector.Sample]
	// call. 0 means unlimited (every visible PID is fully sampled). When
	// >0, every PID is still measured via the cheap pair (comm + stat) so
	// CPU% and RSS are available, then the slice is truncated to MaxProcs
	// entries ordered by CPU% desc (RSS desc tiebreak — keeps freshly-
	// spawned heavyweights visible before their first CPU delta).
	// /proc/[pid]/cmdline and /proc/[pid]/status are only read for the
	// survivors, which is where most of the per-tick syscall and heap
	// pressure live on a busy host.
	MaxProcs uint32

	// IncludeKernelThreads, when true, includes kthreadd (pid 2) and its
	// children in the snapshot. Default false.
	IncludeKernelThreads bool
}

// Collector samples the Linux process table.
type Collector struct {
	proc          *procfs.Reader
	nowFn         func() time.Time
	userLookup    UserLookupFunc
	clkTck        uint64
	pageSize      uint64
	numCPUs       uint64
	maxProcs      uint32
	includeKernel bool

	userCache map[uint32]string

	// scratch is the per-Collector read buffer reused by every
	// [procfs.Reader.ReadFileInto] call inside a [Sample] pass. /proc
	// files stat as size 0, so os.ReadFile-style code grows a fresh
	// []byte per call — at ~4 reads × ~600 PIDs × ~1 Hz on a desktop
	// that allocates >1 GiB/s of garbage, every tick triggers a GC, and
	// every GC drags the renderer into mark-assist. A single scratch
	// stabilises at the largest /proc/[pid]/status seen and serves
	// every subsequent read for free.
	//
	// Single-goroutine contract: the Collector's other mutable fields
	// (prev, userCache) are already unsynchronised, so callers must
	// serialise [Sample] / [All]. scratch piggybacks on that contract.
	scratch []byte

	primed     bool
	prev       map[uint32]procTick
	prevGlobal uint64
}

type procTick struct {
	cpuTotal uint64 // utime + stime in clock ticks
}

// New returns a process Collector configured by opts. The returned
// error is always nil today; the signature reserves the slot for
// forward-compatibility.
func New(opts Options) (inst *Collector, err error) {
	if opts.Proc == nil {
		opts.Proc = procfs.New("")
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	if opts.UserLookup == nil {
		opts.UserLookup = defaultUserLookup
	}
	if opts.ClkTckHz == 0 {
		opts.ClkTckHz = DefaultClkTckHz
	}
	pageSize := uint64(opts.PageSize)
	if pageSize == 0 {
		pageSize = uint64(os.Getpagesize())
	}
	numCPUs := uint64(opts.NumCPUs)
	if numCPUs == 0 {
		numCPUs = uint64(runtime.NumCPU())
	}
	inst = &Collector{
		proc:          opts.Proc,
		nowFn:         opts.NowFunc,
		userLookup:    opts.UserLookup,
		clkTck:        uint64(opts.ClkTckHz),
		pageSize:      pageSize,
		numCPUs:       numCPUs,
		maxProcs:      opts.MaxProcs,
		includeKernel: opts.IncludeKernelThreads,
		userCache:     map[uint32]string{},
		prev:          map[uint32]procTick{},
	}
	return
}

var _ CollectorI = (*Collector)(nil)

// Sample returns every visible process as a slice. Equivalent to
// `slices.Collect2(c.All(ctx))` but skips the iterator wrapper.
func (inst *Collector) Sample(ctx context.Context) (infos []Info, err error) {
	infos, err = inst.collectAll(ctx)
	return
}

// All returns an iterator that yields every visible process. The
// underlying snapshot is materialized eagerly when iteration begins, so
// breaking out of the loop early does not corrupt the prior-tick state.
func (inst *Collector) All(ctx context.Context) (seq iter.Seq2[Info, error]) {
	return func(yield func(Info, error) bool) {
		infos, err := inst.collectAll(ctx)
		if err != nil {
			yield(Info{}, err)
			return
		}
		for _, info := range infos {
			if !yield(info, nil) {
				return
			}
		}
	}
}

func (inst *Collector) collectAll(ctx context.Context) (infos []Info, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}

	now := inst.nowFn()

	uptimeSec, _ := inst.readUptime()
	bootTime := now
	if uptimeSec > 0 {
		bootTime = now.Add(-time.Duration(uptimeSec * float64(time.Second)))
	}

	globalCPU, gerr := inst.readGlobalCPUTicks()
	if gerr != nil {
		err = eh.Errorf("read /proc/stat: %w", gerr)
		return
	}

	pids, lerr := inst.listPIDs()
	if lerr != nil {
		err = eh.Errorf("list /proc: %w", lerr)
		return
	}

	infos = make([]Info, 0, len(pids))
	seen := make(map[uint32]struct{}, len(pids))

	var deltaGlobal uint64
	if inst.primed && globalCPU > inst.prevGlobal {
		deltaGlobal = globalCPU - inst.prevGlobal
	}

	// Phase 1: read comm + stat for every PID. Cheapest pair, enough for
	// CPU% / RSS / KernelThread / sort keys. cmdline + status (the bulk of
	// per-tick I/O) are deferred to phase 3 so a MaxProcs cap can suppress
	// them.
	for _, pid := range pids {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}
		seen[pid] = struct{}{}
		info, ok := inst.readBasicPID(pid, bootTime, deltaGlobal)
		if !ok {
			continue
		}
		if !inst.includeKernel && info.KernelThread {
			continue
		}
		infos = append(infos, info)
	}

	// Phase 2: cap by CPU% desc, RSS desc tiebreak. The tiebreak keeps a
	// freshly-spawned heavyweight visible even when its first sample has
	// no prior tick to delta against (CPUPercent==0 on first sighting).
	if inst.maxProcs > 0 && uint32(len(infos)) > inst.maxProcs {
		slices.SortFunc(infos, func(a, b Info) int {
			switch {
			case a.CPUPercent > b.CPUPercent:
				return -1
			case a.CPUPercent < b.CPUPercent:
				return 1
			case a.RSSBytes > b.RSSBytes:
				return -1
			case a.RSSBytes < b.RSSBytes:
				return 1
			}
			return 0
		})
		infos = infos[:inst.maxProcs]
	}

	// Phase 3: fill in Cmd + UID/GID/User for the survivors only.
	for i := range infos {
		inst.enrichDetailPID(&infos[i])
	}

	// Update global state and prune stale per-pid state.
	inst.prevGlobal = globalCPU
	inst.primed = true
	for k := range inst.prev {
		if _, ok := seen[k]; !ok {
			delete(inst.prev, k)
		}
	}

	slices.SortFunc(infos, func(a, b Info) int {
		switch {
		case a.PID < b.PID:
			return -1
		case a.PID > b.PID:
			return 1
		}
		return 0
	})
	return
}

// readBasicPID reads /proc/[pid]/{comm,stat} and fills every Info field
// that does not require /proc/[pid]/{cmdline,status}. The CPU% delta map
// is also updated here — phase 2's CPU%-desc cap relies on a single
// linear scan to set the sort key. Returns ok=false on dead-pid race
// (ENOENT on comm), missing stat, or malformed stat.
func (inst *Collector) readBasicPID(pid uint32, bootTime time.Time, deltaGlobal uint64) (info Info, ok bool) {
	dir := strconv.FormatUint(uint64(pid), 10)
	info.PID = pid

	commRaw, cerr := inst.proc.ReadFileInto(filepath.Join(dir, "comm"), inst.scratch)
	if cerr == nil {
		inst.scratch = commRaw
		info.Name = strings.TrimRight(string(commRaw), "\n")
	} else if errors.Is(cerr, fs.ErrNotExist) {
		return
	}

	statRaw, serr := inst.proc.ReadFileInto(filepath.Join(dir, "stat"), inst.scratch)
	if serr != nil {
		return
	}
	inst.scratch = statRaw
	parsed, perr := parseStat(statRaw)
	if perr != nil {
		return
	}
	info.PPID = parsed.ppid
	info.State = parsed.state
	info.NumThreads = parsed.numThreads
	info.Nice = parsed.nice
	info.Priority = parsed.priority
	info.RSSBytes = parsed.rssPages * inst.pageSize
	info.VMSizeBytes = parsed.vsize
	info.KernelThread = parsed.ppid == KernelThreadDaemonPID || pid == KernelThreadDaemonPID

	if inst.clkTck > 0 {
		startSec := float64(parsed.starttimeTicks) / float64(inst.clkTck)
		info.StartedAtUnixMs = bootTime.Add(time.Duration(startSec * float64(time.Second))).UnixMilli()
	}

	cpuTotal := parsed.utime + parsed.stime
	if prev, primedPid := inst.prev[pid]; primedPid && deltaGlobal > 0 && cpuTotal >= prev.cpuTotal {
		// Per-CPU view: pct = deltaPid * 100 * N / deltaGlobal.
		// Provenance: btop src/linux/btop_collect.cpp:3250.
		dPid := cpuTotal - prev.cpuTotal
		pct := float64(dPid) * 100 * float64(inst.numCPUs) / float64(deltaGlobal)
		max := 100 * float64(inst.numCPUs)
		switch {
		case pct < 0:
			info.CPUPercent = 0
		case pct > max:
			info.CPUPercent = float32(max)
		default:
			info.CPUPercent = float32(pct)
		}
	}
	inst.prev[pid] = procTick{cpuTotal: cpuTotal}

	ok = true
	return
}

// enrichDetailPID fills Cmd + UID/GID/User from /proc/[pid]/{cmdline,
// status}. Failures are silent: dead-PID races and missing files leave
// the fields at their zero values, matching the pre-split behaviour.
func (inst *Collector) enrichDetailPID(info *Info) {
	dir := strconv.FormatUint(uint64(info.PID), 10)

	if cmdRaw, cerr := inst.proc.ReadFileInto(filepath.Join(dir, "cmdline"), inst.scratch); cerr == nil {
		inst.scratch = cmdRaw
		info.Cmd = formatCmdline(cmdRaw)
	}

	if statusRaw, sterr := inst.proc.ReadFileInto(filepath.Join(dir, "status"), inst.scratch); sterr == nil {
		inst.scratch = statusRaw
		uid, gid, hasUid := parseStatusUidGid(statusRaw)
		if hasUid {
			info.UID = uid
			info.GID = gid
			info.User = inst.lookupUser(uid)
		}
	}
}

func (inst *Collector) lookupUser(uid uint32) (name string) {
	if cached, ok := inst.userCache[uid]; ok {
		return cached
	}
	name = inst.userLookup(uid)
	inst.userCache[uid] = name
	return
}

func defaultUserLookup(uid uint32) (name string) {
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return ""
	}
	return u.Username
}

func (inst *Collector) listPIDs() (pids []uint32, err error) {
	root := inst.proc.Root()
	entries, derr := os.ReadDir(root)
	if derr != nil {
		err = eb.Build().Str("path", root).Errorf("read /proc dir: %w", derr)
		return
	}
	pids = make([]uint32, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if len(name) == 0 || name[0] < '0' || name[0] > '9' {
			continue
		}
		v, perr := strconv.ParseUint(name, 10, 32)
		if perr != nil {
			continue
		}
		pids = append(pids, uint32(v))
	}
	return
}

func (inst *Collector) readUptime() (uptimeSec float64, err error) {
	raw, rerr := inst.proc.ReadFileInto("uptime", inst.scratch)
	if rerr != nil {
		err = rerr
		return
	}
	inst.scratch = raw
	for f := range procfs.IterFields(bytes.TrimRight(raw, "\n")) {
		v, perr := strconv.ParseFloat(string(f), 64)
		if perr == nil {
			uptimeSec = v
		}
		return
	}
	return
}

func (inst *Collector) readGlobalCPUTicks() (total uint64, err error) {
	raw, rerr := inst.proc.ReadFileInto("stat", inst.scratch)
	if rerr != nil {
		err = rerr
		return
	}
	inst.scratch = raw
	for line := range procfs.IterLines(raw) {
		if !bytes.HasPrefix(line, []byte("cpu ")) && !bytes.HasPrefix(line, []byte("cpu\t")) {
			break
		}
		var idx int
		for f := range procfs.IterFields(line) {
			idx++
			if idx == 1 {
				continue
			}
			if idx > 9 {
				// Stop before guest fields (already counted in user, per
				// btop's "i < 8" loop at btop_collect.cpp:3073).
				break
			}
			v, perr := strconv.ParseUint(string(f), 10, 64)
			if perr != nil {
				break
			}
			total += v
		}
		return
	}
	err = eb.Build().Errorf("/proc/stat had no aggregate cpu line")
	return
}

// statFields is the subset of /proc/[pid]/stat fields we expose.
type statFields struct {
	state          byte
	ppid           uint32
	utime          uint64
	stime          uint64
	priority       int32
	nice           int32
	numThreads     int32
	starttimeTicks uint64
	vsize          uint64
	rssPages       uint64
}

// parseStat parses /proc/[pid]/stat. The comm field can contain spaces
// and (matched/unmatched) parentheses; we anchor on the LAST ')' to get
// past it.
//
// provenance: btop src/linux/btop_collect.cpp:3171-3247.
func parseStat(content []byte) (out statFields, err error) {
	open := bytes.IndexByte(content, '(')
	closeP := bytes.LastIndexByte(content, ')')
	if open < 0 || closeP < 0 || closeP < open {
		err = eb.Build().Errorf("malformed /proc/pid/stat: no paren-delimited comm")
		return
	}
	rest := bytes.TrimLeft(content[closeP+1:], " ")
	fields := bytes.Fields(rest)
	// Need at least 22 fields after comm to cover state..rss.
	if len(fields) < 22 {
		err = eb.Build().Int("fields", len(fields)).Errorf("malformed /proc/pid/stat: too few fields after comm")
		return
	}
	if len(fields[0]) > 0 {
		out.state = fields[0][0]
	}
	if v, perr := strconv.ParseUint(string(fields[1]), 10, 32); perr == nil {
		out.ppid = uint32(v)
	}
	if v, perr := strconv.ParseUint(string(fields[11]), 10, 64); perr == nil {
		out.utime = v
	}
	if v, perr := strconv.ParseUint(string(fields[12]), 10, 64); perr == nil {
		out.stime = v
	}
	if v, perr := strconv.ParseInt(string(fields[15]), 10, 32); perr == nil {
		out.priority = int32(v)
	}
	if v, perr := strconv.ParseInt(string(fields[16]), 10, 32); perr == nil {
		out.nice = int32(v)
	}
	if v, perr := strconv.ParseInt(string(fields[17]), 10, 32); perr == nil {
		out.numThreads = int32(v)
	}
	if v, perr := strconv.ParseUint(string(fields[19]), 10, 64); perr == nil {
		out.starttimeTicks = v
	}
	if v, perr := strconv.ParseUint(string(fields[20]), 10, 64); perr == nil {
		out.vsize = v
	}
	if v, perr := strconv.ParseUint(string(fields[21]), 10, 64); perr == nil {
		out.rssPages = v
	}
	return
}

// parseStatusUidGid extracts the *real* UID and GID from /proc/[pid]/status.
//
// The "Uid:" and "Gid:" lines have four whitespace-separated values:
// real, effective, saved-set, fs. We take the first.
func parseStatusUidGid(content []byte) (uid, gid uint32, ok bool) {
	var sawUid, sawGid bool
	for k, v := range procfs.IterKV(content) {
		switch string(k) {
		case "Uid":
			for f := range procfs.IterFields(v) {
				if n, err := strconv.ParseUint(string(f), 10, 32); err == nil {
					uid = uint32(n)
					sawUid = true
				}
				break
			}
		case "Gid":
			for f := range procfs.IterFields(v) {
				if n, err := strconv.ParseUint(string(f), 10, 32); err == nil {
					gid = uint32(n)
					sawGid = true
				}
				break
			}
		}
		if sawUid && sawGid {
			break
		}
	}
	ok = sawUid
	return
}

// formatCmdline converts NUL-separated cmdline bytes to a space-joined
// string, capped at [CmdMaxBytes].
func formatCmdline(content []byte) (out string) {
	content = bytes.TrimRight(content, "\x00")
	if len(content) == 0 {
		return ""
	}
	if len(content) > CmdMaxBytes {
		content = content[:CmdMaxBytes]
	}
	for i, b := range content {
		if b == 0 {
			content[i] = ' '
		}
	}
	return string(content)
}
