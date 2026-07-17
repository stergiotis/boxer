package sockets

import (
	"context"
	"encoding/hex"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// DefaultInterval is the default collection cadence. Listening sockets
// change on the order of service restarts, not ticks; 15 s keeps the
// fd walk (the expensive part) off the per-second path.
const DefaultInterval = 15 * time.Second

// tcpListenState / udpBoundState are the kernel socket states that make
// an inet row a listener: TCP_LISTEN (0x0A) for tcp, and for udp the
// unconnected-but-bound TCP_CLOSE (0x07) every bound datagram socket
// sits in.
const (
	tcpListenState = 0x0A
	udpBoundState  = 0x07
)

// soAcceptCon is the __SO_ACCEPTCON flag in /proc/net/unix — set on
// listening unix sockets.
const soAcceptCon = 0x10000

// CollectorI is the public surface a listening-socket sampler implements.
type CollectorI interface {
	Sample(ctx context.Context) (*sysmsnap.SocketsSnapshot, error)
}

// Options configures a [Collector].
type Options struct {
	// Proc is the procfs reader; nil defaults to procfs.New("") ("/proc").
	Proc *procfs.Reader
	// NowFunc overrides the cadence/stamp clock when non-nil.
	NowFunc func() time.Time
	// Interval is the collector-owned cadence (ADR-0126 §SD4): a real
	// collection runs only when this much time has passed since the last
	// attempt; between due times [Collector.Sample] returns the cached
	// snapshot. 0 means [DefaultInterval].
	Interval time.Duration
}

// Collector samples the listening-socket table. Callers must serialise
// Sample calls (single-goroutine contract, like the other sysmetrics
// collectors) and must not mutate the returned snapshot — between due
// times every caller shares the cached pointer.
type Collector struct {
	proc     *procfs.Reader
	nowFn    func() time.Time
	interval time.Duration

	// scratch backs the /proc/net file reads (procfs.ReadFileInto
	// contract: pass the previous return to stay allocation-flat).
	scratch []byte

	lastAttempt time.Time
	cached      *sysmsnap.SocketsSnapshot
}

// New returns a sockets Collector.
func New(opts Options) (inst *Collector, err error) {
	if opts.Proc == nil {
		opts.Proc = procfs.New("")
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	if opts.Interval == 0 {
		opts.Interval = DefaultInterval
	}
	inst = &Collector{proc: opts.Proc, nowFn: opts.NowFunc, interval: opts.Interval}
	return
}

var _ CollectorI = (*Collector)(nil)

// Sample returns the current listener table, collecting only when the
// interval has elapsed. A failed due-collection returns its error (the
// bundle's per-domain error slot) and keeps serving the previous cache
// on the following ticks until the next due time — consumers hold
// latest, so a transient failure shows once per interval, not per tick.
func (inst *Collector) Sample(ctx context.Context) (snap *sysmsnap.SocketsSnapshot, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}
	now := inst.nowFn()
	if inst.cached != nil && now.Sub(inst.lastAttempt) < inst.interval {
		snap = inst.cached
		return
	}
	inst.lastAttempt = now
	snap, err = inst.collect(now)
	if err != nil {
		snap = nil
		return
	}
	inst.cached = snap
	return
}

// inetTable describes one /proc/net inet file to parse.
type inetTable struct {
	rel       string
	proto     sysmsnap.SocketProto
	wantState uint8
}

var inetTables = []inetTable{
	{rel: "net/tcp", proto: sysmsnap.SocketProtoTCP, wantState: tcpListenState},
	{rel: "net/tcp6", proto: sysmsnap.SocketProtoTCP6, wantState: tcpListenState},
	{rel: "net/udp", proto: sysmsnap.SocketProtoUDP, wantState: udpBoundState},
	{rel: "net/udp6", proto: sysmsnap.SocketProtoUDP6, wantState: udpBoundState},
}

func (inst *Collector) collect(now time.Time) (snap *sysmsnap.SocketsSnapshot, err error) {
	snap = &sysmsnap.SocketsSnapshot{CollectedAtUnixMs: now.UnixMilli()}

	// A single missing table is normal (no IPv6, a network namespace);
	// only when every table is unreadable is the collection itself broken
	// (e.g. /proc/net hidden by a sandbox).
	var readErrs []error
	readable := 0
	for _, tbl := range inetTables {
		raw, rerr := inst.proc.ReadFileInto(tbl.rel, inst.scratch)
		if rerr != nil {
			readErrs = append(readErrs, rerr)
			continue
		}
		inst.scratch = raw
		readable++
		parseInetTable(raw, tbl.proto, tbl.wantState, &snap.Sockets)
	}
	if raw, rerr := inst.proc.ReadFileInto("net/unix", inst.scratch); rerr != nil {
		readErrs = append(readErrs, rerr)
	} else {
		inst.scratch = raw
		readable++
		parseUnixTable(raw, &snap.Sockets)
	}
	if readable == 0 {
		snap = nil
		err = eh.Errorf("sockets: no /proc/net table readable: %w", errors.Join(readErrs...))
		return
	}

	inst.attributePids(snap.Sockets)
	return
}

// parseInetTable appends listener rows of one /proc/net/{tcp,tcp6,udp,udp6}
// table to out. Malformed lines are skipped — the kernel format is stable
// and a partial table beats none.
func parseInetTable(content []byte, proto sysmsnap.SocketProto, wantState uint8, out *[]sysmsnap.SocketInfo) {
	first := true
	for line := range procfs.IterLines(content) {
		if first { // header
			first = false
			continue
		}
		// sl local_address rem_address st tx:rx tr:tm retrnsmt uid timeout inode ...
		var f [10]string
		n := 0
		for field := range procfs.IterFields(line) {
			f[n] = string(field)
			n++
			if n == len(f) {
				break
			}
		}
		if n < 10 {
			continue
		}
		st, serr := strconv.ParseUint(f[3], 16, 8)
		if serr != nil || uint8(st) != wantState {
			continue
		}
		addr, port, ok := parseHexHostPort(f[1])
		if !ok {
			continue
		}
		uid64, uerr := strconv.ParseUint(f[7], 10, 32)
		if uerr != nil {
			continue
		}
		inode, ierr := strconv.ParseUint(f[9], 10, 64)
		if ierr != nil {
			continue
		}
		*out = append(*out, sysmsnap.SocketInfo{
			Proto: proto,
			Addr:  addr,
			Port:  port,
			Inode: inode,
			UID:   uint32(uid64),
		})
	}
}

// parseUnixTable appends listening (accepting) named unix sockets to out.
// Unnamed sockets carry no address to join on and are skipped.
func parseUnixTable(content []byte, out *[]sysmsnap.SocketInfo) {
	first := true
	for line := range procfs.IterLines(content) {
		if first { // header
			first = false
			continue
		}
		// Num RefCount Protocol Flags Type St Inode [Path]
		var f [8]string
		n := 0
		for field := range procfs.IterFields(line) {
			f[n] = string(field)
			n++
			if n == len(f) {
				break
			}
		}
		if n < 8 { // pathless rows are unnamed — nothing to address them by
			continue
		}
		flags, ferr := strconv.ParseUint(f[3], 16, 32)
		if ferr != nil || flags&soAcceptCon == 0 {
			continue
		}
		inode, ierr := strconv.ParseUint(f[6], 10, 64)
		if ierr != nil {
			continue
		}
		*out = append(*out, sysmsnap.SocketInfo{
			Proto: sysmsnap.SocketProtoUnix,
			Addr:  f[7],
			Inode: inode,
		})
	}
}

// parseHexHostPort parses the kernel's local_address column: the IP as
// little-endian 32-bit word(s) in hex (one word for v4, four for v6),
// then ':' and the port in big-endian hex.
func parseHexHostPort(s string) (addr string, port uint16, ok bool) {
	hexIP, hexPort, found := strings.Cut(s, ":")
	if !found {
		return
	}
	p64, perr := strconv.ParseUint(hexPort, 16, 16)
	if perr != nil {
		return
	}
	var raw [16]byte
	if len(hexIP) != 8 && len(hexIP) != 32 {
		return
	}
	if _, derr := hex.Decode(raw[:len(hexIP)/2], []byte(hexIP)); derr != nil {
		return
	}
	switch len(hexIP) {
	case 8:
		var b [4]byte
		for i := range b {
			b[i] = raw[3-i]
		}
		addr = netip.AddrFrom4(b).String()
	case 32:
		var b [16]byte
		for g := range 4 {
			for i := range 4 {
				b[g*4+i] = raw[g*4+3-i]
			}
		}
		addr = netip.AddrFrom16(b).String()
	}
	port = uint16(p64)
	ok = true
	return
}

// attributePids fills SocketInfo.PID by walking /proc/[pid]/fd and
// matching "socket:[inode]" link targets against the collected rows.
// Unreadable fd tables (other uids without privilege, dead-pid races)
// are skipped silently — their rows keep PID 0.
func (inst *Collector) attributePids(rows []sysmsnap.SocketInfo) {
	if len(rows) == 0 {
		return
	}
	wanted := make(map[uint64][]int, len(rows))
	for i := range rows {
		wanted[rows[i].Inode] = append(wanted[rows[i].Inode], i)
	}
	remaining := len(wanted)

	root := inst.proc.Root()
	entries, derr := os.ReadDir(root)
	if derr != nil {
		return
	}
	for _, e := range entries {
		if remaining == 0 {
			return
		}
		name := e.Name()
		if len(name) == 0 || name[0] < '0' || name[0] > '9' {
			continue
		}
		pid64, perr := strconv.ParseUint(name, 10, 32)
		if perr != nil {
			continue
		}
		fdDir := filepath.Join(root, name, "fd")
		fds, ferr := os.ReadDir(fdDir)
		if ferr != nil {
			continue
		}
		for _, fd := range fds {
			target, lerr := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if lerr != nil {
				continue
			}
			inodeStr, isSock := strings.CutPrefix(target, "socket:[")
			if !isSock || len(inodeStr) == 0 || inodeStr[len(inodeStr)-1] != ']' {
				continue
			}
			inode, ierr := strconv.ParseUint(inodeStr[:len(inodeStr)-1], 10, 64)
			if ierr != nil {
				continue
			}
			idxs, want := wanted[inode]
			if !want {
				continue
			}
			for _, i := range idxs {
				if rows[i].PID == 0 {
					rows[i].PID = uint32(pid64)
				}
			}
			delete(wanted, inode)
			remaining--
		}
	}
}
