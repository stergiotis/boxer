package sysmreplay

import (
	"context"
	"iter"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Options configures a [Reader]. Store and Host are required.
type Options struct {
	// Store is the facts-bound record store to read. The reader only ever
	// SELECTs through it.
	Store *sysmfacts.SysmetricsStore
	// Host is the token whose series to replay. It joins the entity key, so a
	// reader is bound to one host; a multi-host view is several readers.
	Host string
	// Exec runs the coverage query, which is ordinary SQL over the envelope
	// columns rather than one of the store's generated verbs (ADR-0197 §SD10).
	// It must be the executor the Store was built on: the two would otherwise
	// answer about different servers, and nothing would say so.
	//
	// Optional. Without it [Reader.All] works and [Reader.Coverage] refuses.
	Exec recordstore.ExecutorI
	Log  zerolog.Logger
}

// Window selects a replay range on the order column.
type Window struct {
	// From is the inclusive lower bound. Zero replays from the beginning of
	// stored history.
	From time.Time
	// To is the exclusive upper bound. Zero is unbounded.
	To time.Time
	// Limit caps the number of bundles emitted; zero means no cap. It also
	// bounds the per-tick fetch, since one bundle contributes at most one row
	// per kind — the carry-forward kinds are fetched over the whole window
	// regardless, because truncating them would break the carry chain.
	Limit int
	// Decimate restricts the read to one recorded bundle per bin, from a plan
	// built by [Reader.PlanDecimation]. It is what lets a range longer than the
	// consumer can hold be replayed without reading a row per tick (ADR-0197
	// §SD6, closed by §SD11).
	//
	// Empty replays the window whole. The plan's instants are recorded ones and
	// the rows come back unchanged: decimation drops resolution, never fidelity.
	Decimate []int64
}

// Reader replays stored bundles for one host.
//
// It is a pure consumer of the store: nothing here writes, and nothing here
// publishes on the metric plane (ADR-0197 §SD1).
type Reader struct {
	store *sysmfacts.SysmetricsStore
	exec  recordstore.ExecutorI
	host  string
	log   zerolog.Logger
}

// New validates opts and returns a Reader.
func New(opts Options) (inst *Reader, err error) {
	if opts.Store == nil {
		err = eh.Errorf("sysmreplay: reader needs a Store")
		return
	}
	if opts.Host == "" {
		err = eh.Errorf("sysmreplay: reader needs a Host")
		return
	}
	inst = &Reader{store: opts.Store, exec: opts.Exec, host: opts.Host, log: opts.Log}
	return
}

// Key is the store key this reader reads for one domain — exposed so a caller
// can check what a replay would look at without running one.
func (inst *Reader) Key(domain string) (key uint64) {
	key = EntityKey(inst.host, domain)
	return
}

// cursor is one per-tick kind's position in its ascending row stream.
type cursor struct {
	domain string
	next   func() (*sysmfacts.SysmetricsEntity, error, bool)
	stop   func()
	cur    *sysmfacts.SysmetricsEntity
	live   bool
}

// advance pulls the next row, clearing live at the end of the stream.
func (inst *cursor) advance() (err error) {
	ent, nerr, ok := inst.next()
	if nerr != nil {
		inst.live = false
		err = eh.Errorf("sysmreplay: reading %s: %w", inst.domain, nerr)
		return
	}
	if !ok || ent == nil {
		inst.live = false
		inst.cur = nil
		return
	}
	inst.cur = ent
	inst.live = true
	return
}

// carry holds a non-per-tick kind's rows and the position the emitting tick has
// reached in them, so the most recent row at or before a tick can be handed to
// every tick that follows it (see the package doc).
type carry struct {
	rows []*sysmfacts.SysmetricsEntity
	i    int
}

// at returns the newest row at or before ts, or nil when none has been reached.
func (inst *carry) at(ts time.Time) (ent *sysmfacts.SysmetricsEntity) {
	for inst.i+1 < len(inst.rows) && !inst.rows[inst.i+1].Ts.After(ts) {
		inst.i++
	}
	if inst.i < 0 {
		return
	}
	ent = inst.rows[inst.i]
	return
}

// perTickDomains are the kinds the tee writes once per bundle. They share the
// bundle's timestamp, so they merge by an exact match on the order column.
var perTickDomains = []string{
	DomainCPU, DomainMem, DomainPSI, DomainNet,
	DomainDiskMnt, DomainDiskIO, DomainBattery, DomainGPU,
	DomainProc, DomainProcCmd,
}

// All replays the window as [sysmsnap.BundleSnapshot] values in ascending time
// order — the same struct a live subscriber receives.
//
// The sequence is single-use; ctx must stay valid until iteration completes; an
// error ends it as a final (nil, err) pair. Reads see only flushed rows.
//
// Errors is always empty: the tee does not persist per-domain scrape failures,
// so a domain that failed then is indistinguishable here from one that was
// never wired (ADR-0197 §SD8).
func (inst *Reader) All(ctx context.Context, w Window) iter.Seq2[*sysmsnap.BundleSnapshot, error] {
	return func(yield func(*sysmsnap.BundleSnapshot, error) bool) {
		cursors := make([]*cursor, 0, len(perTickDomains))
		defer func() {
			for _, c := range cursors {
				c.stop()
			}
		}()
		for _, domain := range perTickDomains {
			seq := inst.perTickSeq(ctx, domain, w)
			next, stop := iter.Pull2(seq)
			c := &cursor{domain: domain, next: next, stop: stop}
			cursors = append(cursors, c)
			if err := c.advance(); err != nil {
				yield(nil, err)
				return
			}
		}

		cpuInfo, err := inst.fetchCarry(ctx, DomainCPUInfo, w)
		if err != nil {
			yield(nil, err)
			return
		}
		sockets, err := inst.fetchCarry(ctx, DomainSockets, w)
		if err != nil {
			yield(nil, err)
			return
		}
		topology, err := inst.fetchCarry(ctx, DomainTopology, w)
		if err != nil {
			yield(nil, err)
			return
		}

		emitted := 0
		for {
			tick, ok := nextTick(cursors)
			if !ok {
				return
			}
			snap, berr := inst.bundleAt(tick, cursors, cpuInfo, sockets, topology)
			// Advance past this tick before deciding what to do with the
			// result, so a malformed row cannot wedge the merge on it.
			for _, c := range cursors {
				if c.live && c.cur.Ts.Equal(tick) {
					if aerr := c.advance(); aerr != nil {
						yield(nil, aerr)
						return
					}
				}
			}
			if berr != nil {
				if !yield(nil, berr) {
					return
				}
				continue
			}
			if !yield(snap, nil) {
				return
			}
			emitted++
			if w.Limit > 0 && emitted >= w.Limit {
				return
			}
		}
	}
}

// perTickSeq opens one per-tick kind's row stream for the window: the whole
// range, or only the plan's instants when the caller decimated.
//
// The decimated form goes through Scan rather than Replay because the
// restriction is a set of stamps rather than a lower bound, and Scan is the
// verb that takes a predicate. Both are the store's own generated verbs, so
// neither invents a second read path beside them.
func (inst *Reader) perTickSeq(ctx context.Context, domain string, w Window) (seq iter.Seq2[*sysmfacts.SysmetricsEntity, error]) {
	if len(w.Decimate) == 0 {
		seq = inst.store.Replay(ctx, EntityKey(inst.host, domain), w.From,
			recordstore.ReplayOpts{To: w.To, Limit: w.Limit})
		return
	}
	opts := recordstore.ScanOpts{ExtraPredicate: inst.decimatedPredicate(domain, w.Decimate)}
	seq = inst.scanFor(ctx, domain, opts)
	return
}

// scanFor dispatches to the generated Scan verb for a per-tick kind.
func (inst *Reader) scanFor(ctx context.Context, domain string, opts recordstore.ScanOpts) (seq iter.Seq2[*sysmfacts.SysmetricsEntity, error]) {
	switch domain {
	case DomainCPU:
		return inst.store.ScanSysCpu(ctx, opts)
	case DomainMem:
		return inst.store.ScanSysMem(ctx, opts)
	case DomainPSI:
		return inst.store.ScanSysPsi(ctx, opts)
	case DomainNet:
		return inst.store.ScanSysNet(ctx, opts)
	case DomainDiskMnt:
		return inst.store.ScanSysDiskMount(ctx, opts)
	case DomainDiskIO:
		return inst.store.ScanSysDiskIo(ctx, opts)
	case DomainBattery:
		return inst.store.ScanSysBattery(ctx, opts)
	case DomainGPU:
		return inst.store.ScanSysGpu(ctx, opts)
	case DomainProc:
		return inst.store.ScanSysProc(ctx, opts)
	case DomainProcCmd:
		return inst.store.ScanSysProcCmd(ctx, opts)
	}
	// Not reachable from perTickDomains; an empty sequence is the honest
	// answer for a domain this function does not know.
	return func(yield func(*sysmfacts.SysmetricsEntity, error) bool) {}
}

// nextTick reports the earliest order value any live cursor is sitting on.
func nextTick(cursors []*cursor) (tick time.Time, ok bool) {
	for _, c := range cursors {
		if !c.live {
			continue
		}
		if !ok || c.cur.Ts.Before(tick) {
			tick = c.cur.Ts
			ok = true
		}
	}
	return
}

// bundleAt assembles the bundle for one tick from the cursors sitting on it and
// the carried state of the three non-per-tick kinds.
func (inst *Reader) bundleAt(tick time.Time, cursors []*cursor, cpuInfo, sockets, topology *carry) (snap *sysmsnap.BundleSnapshot, err error) {
	at := map[string]*sysmfacts.SysmetricsEntity{}
	for _, c := range cursors {
		if c.live && c.cur.Ts.Equal(tick) {
			at[c.domain] = c.cur
		}
	}

	// Errors is empty rather than nil, matching the live bundle's shape.
	snap = &sysmsnap.BundleSnapshot{
		SampledAtUnixMs: tick.UnixMilli(),
		Errors:          map[sysmsnap.Domain]error{},
	}

	if ent := at[DomainCPU]; ent != nil && ent.SysCpu.Has {
		var info *sysmfacts.SysCpuInfo
		if ie := cpuInfo.at(tick); ie != nil && ie.SysCpuInfo.Has {
			info = &ie.SysCpuInfo.Val
		}
		snap.CPU, err = CPUFrom(ent.SysCpu.Val, info)
		if err != nil {
			return nil, err
		}
	}
	if ent := at[DomainMem]; ent != nil && ent.SysMem.Has {
		snap.Mem, err = MemFrom(ent.SysMem.Val)
		if err != nil {
			return nil, err
		}
	}
	if ent := at[DomainPSI]; ent != nil && ent.SysPsi.Has {
		snap.PSI, err = PSIFrom(ent.SysPsi.Val)
		if err != nil {
			return nil, err
		}
	}
	if ent := at[DomainNet]; ent != nil && ent.SysNet.Has {
		snap.Net, err = NetFrom(ent.SysNet.Val)
		if err != nil {
			return nil, err
		}
	}
	// One DiskSnapshot, two kinds: the mount table and the block-device list
	// have independent lengths, which is why the tee splits them.
	var mounts *sysmfacts.SysDiskMount
	var diskIO *sysmfacts.SysDiskIo
	if ent := at[DomainDiskMnt]; ent != nil && ent.SysDiskMount.Has {
		mounts = &ent.SysDiskMount.Val
	}
	if ent := at[DomainDiskIO]; ent != nil && ent.SysDiskIo.Has {
		diskIO = &ent.SysDiskIo.Val
	}
	if mounts != nil || diskIO != nil {
		snap.Disk, err = DiskFrom(mounts, diskIO)
		if err != nil {
			return nil, err
		}
	}
	if ent := at[DomainBattery]; ent != nil && ent.SysBattery.Has {
		snap.Battery, err = BatteryFrom(ent.SysBattery.Val)
		if err != nil {
			return nil, err
		}
	}
	if ent := at[DomainGPU]; ent != nil && ent.SysGpu.Has {
		snap.GPU, err = GPUFrom(ent.SysGpu.Val)
		if err != nil {
			return nil, err
		}
	}
	if ent := at[DomainProc]; ent != nil && ent.SysProc.Has {
		var cmd *sysmfacts.SysProcCmd
		if ce := at[DomainProcCmd]; ce != nil && ce.SysProcCmd.Has {
			cmd = &ce.SysProcCmd.Val
		}
		snap.Procs, err = ProcsFrom(ent.SysProc.Val, cmd)
		if err != nil {
			return nil, err
		}
	}
	if se := sockets.at(tick); se != nil && se.SysSocket.Has {
		snap.Sockets, err = SocketsFrom(se.SysSocket.Val)
		if err != nil {
			return nil, err
		}
	}
	if te := topology.at(tick); te != nil && te.SysTopology.Has {
		snap.Topology, err = TopologyFrom(te.SysTopology.Val)
		if err != nil {
			return nil, err
		}
	}
	return
}

// fetchCarry loads one non-per-tick kind: the rows inside the window, preceded
// by the newest row before it.
//
// The seed is what makes the carry work on a window that opens after the row
// was written — the usual case, since the descriptor and the topology are
// written once when the tee first sees the host and a replay is normally of
// some later hour.
func (inst *Reader) fetchCarry(ctx context.Context, domain string, w Window) (c *carry, err error) {
	key := EntityKey(inst.host, domain)
	c = &carry{i: -1}
	if !w.From.IsZero() {
		var seed *sysmfacts.SysmetricsEntity
		seed, err = inst.fetchAsOf(ctx, domain, key, w.From)
		if err != nil {
			c = nil
			return
		}
		if seed != nil {
			c.rows = append(c.rows, seed)
		}
	}
	for ent, rerr := range inst.store.Replay(ctx, key, w.From, recordstore.ReplayOpts{To: w.To}) {
		if rerr != nil {
			c = nil
			err = eh.Errorf("sysmreplay: reading %s: %w", domain, rerr)
			return
		}
		c.rows = append(c.rows, ent)
	}
	return
}

// fetchAsOf returns the newest row for key strictly before bound, or nil.
//
// The generated verbs cannot express it directly: Replay is ascending, so its
// Limit takes the earliest rows rather than the latest, and Latest has no upper
// bound. The predicate below is the one-row equivalent — trusted SQL over the
// envelope columns, which is what ScanOpts.ExtraPredicate is for; no leeway
// section is touched, so this is not the hand-written array arithmetic the
// read-surface page warns against.
func (inst *Reader) fetchAsOf(ctx context.Context, domain string, key uint64, bound time.Time) (ent *sysmfacts.SysmetricsEntity, err error) {
	pred := asOfPredicate(key, bound)
	opts := recordstore.ScanOpts{ExtraPredicate: pred, Limit: 1}
	var seq iter.Seq2[*sysmfacts.SysmetricsEntity, error]
	switch domain {
	case DomainCPUInfo:
		seq = inst.store.ScanSysCpuInfo(ctx, opts)
	case DomainSockets:
		seq = inst.store.ScanSysSocket(ctx, opts)
	case DomainTopology:
		seq = inst.store.ScanSysTopology(ctx, opts)
	default:
		err = eh.Errorf("sysmreplay: %s is not a carried kind", domain)
		return
	}
	for got, serr := range seq {
		if serr != nil {
			ent = nil
			err = eh.Errorf("sysmreplay: seeding %s: %w", domain, serr)
			return
		}
		ent = got
		break
	}
	return
}

// asOfPredicate restricts a scan to the single newest row for key before bound.
func asOfPredicate(key uint64, bound time.Time) (pred string) {
	k := strconv.FormatUint(key, 10)
	lit := "fromUnixTimestamp64Nano(" + strconv.FormatInt(bound.UnixNano(), 10) + ")"
	pred = sysmfacts.SysmetricsColKey + " = " + k +
		" AND " + sysmfacts.SysmetricsColOrder + " = (SELECT max(" + sysmfacts.SysmetricsColOrder + ")" +
		" FROM " + sysmfacts.SysmetricsTableName +
		" WHERE " + sysmfacts.SysmetricsColKey + " = " + k +
		" AND " + sysmfacts.SysmetricsColOrder + " < " + lit + ")"
	return
}
