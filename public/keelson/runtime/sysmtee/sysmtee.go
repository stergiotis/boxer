// Package sysmtee writes the system-metrics plane into `boxer.facts`.
//
// It is ADR-0090 P5 — the persistence tee reserved when the plane was designed
// and left unbuilt until something wanted history — in the shape ADR-0184
// settles: a subscriber, not a fork of the producer. It consumes the same
// bundles any other consumer sees and writes them through the generated record
// store in
// [github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts]. Producer and
// consumers are untouched by its presence.
//
// # It re-models rather than tees bytes
//
// ADR-0090 §SD4 anticipated a tee that forwarded the wire bytes unchanged,
// which was possible while the wire was expected to be the facts codec. The
// plane shipped a CBOR codec instead and the swap never happened, so this
// decodes to [github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap]
// structs and builds rows from them. That is the honest cost of the interim
// codec, not a design choice made here.
//
// # Losing samples is preferred to blocking the plane
//
// The bus delivers on its own goroutine and a record store is single-goroutine,
// so bundles hand off to an owner goroutine through a bounded queue. When the
// queue is full — ClickHouse slow or down — the incoming bundle is dropped and
// counted rather than blocking delivery. The metric plane is one-way and
// unacknowledged by design (ADR-0090 §SD4 rejects JetStream for it), so a gap
// in stored history is the correct failure: the alternative is a stalled
// subscription that also delays every other consumer on an in-process bus.
//
// # Append-only
//
// Every kind is append-shaped and the store has no state view to misuse — see
// [github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts]. Re-writing a
// descriptor row is therefore harmless, which is what makes "on first sight of
// a host" a cheap heuristic rather than a correctness requirement.
package sysmtee

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// Options configures a tee. Bus, Store and Host are required.
type Options struct {
	// Bus is the metric plane to subscribe on — the same client the scraper
	// publishes through when co-located.
	Bus app.BusI
	// Store is the facts-bound record store rows are written to. The tee takes
	// ownership: it is used only from the tee's own goroutine, satisfying the
	// single-goroutine contract, and Close is the caller's after Stop returns.
	Store *sysmfacts.SysmetricsStore
	// Host is the token stored on every row.
	//
	// It is configured rather than read from the message because the host lives
	// in the subject and [sysmetricsbus.ConsumerOptions.Handler] receives only
	// the snapshot. A tee co-located with its scraper knows the token; a
	// standalone sink subscribing to several hosts would need the subject in
	// the handler, which is a sysmetricsbus change deferred until such a sink
	// exists (ADR-0184 §SD8).
	Host string
	// Subject defaults to this host's bundle subject. Set it to the wildcard
	// only when every publisher on it is the host named above.
	Subject string
	// QueueDepth bounds the handoff between the bus goroutine and the writer.
	// Zero takes DefaultQueueDepth.
	QueueDepth int
	// FlushInterval bounds how long a row waits before becoming durable. Zero
	// takes DefaultFlushInterval.
	FlushInterval time.Duration
	// FlushRows flushes early once this many rows are buffered, so a fast plane
	// does not build an unbounded batch between ticks. Zero takes
	// DefaultFlushRows.
	FlushRows int
	// PersistProcCmd opts into storing process command lines, user names and
	// uid/gid — the ADR-0090 §SD8 sensitive class, which
	// [sysmfacts.SysProcCmd] keeps in its own kind.
	//
	// Off by default, and deliberately not merely a masking flag: §SD8's
	// accepted exposure was scoped to a single-tenant, localhost-bound bus,
	// where a command line lives as long as its subscriber. A row in
	// `boxer.facts` outlives the process, is readable by anything with database
	// access, and is backed up with everything else. The masking switch §SD8
	// defers does not exist, so not writing is the only control that currently
	// enforces anything.
	//
	// The rest of the process table — pids, names, cpu, memory, the ADR-0126
	// topology marks — is stored either way.
	PersistProcCmd bool
	Log            zerolog.Logger
}

// Defaults for [Options]. The flush interval is the dominant one: it is the
// window of samples lost if the process dies, and the rate at which small
// inserts reach ClickHouse.
const (
	DefaultQueueDepth    = 64
	DefaultFlushInterval = 10 * time.Second
	DefaultFlushRows     = 256
)

// Stats reports what the tee has done. Counters are cumulative and safe to read
// from any goroutine.
type Stats struct {
	// Bundles received from the plane.
	Bundles uint64
	// Dropped bundles — the queue was full when they arrived.
	Dropped uint64
	// Rows handed to the store.
	Rows uint64
	// Flushed rows made durable.
	Flushed uint64
	// FlushErrors counts failed flushes. The store keeps the batch pending and
	// the next flush reships it, so this is not a row-loss count.
	FlushErrors uint64
}

// Tee is a running subscriber. Use [Start] to build one.
type Tee struct {
	opts     Options
	consumer *sysmetricsbus.Consumer
	queue    chan *sysmsnap.BundleSnapshot
	done     chan struct{}
	stopOnce sync.Once

	bundles, dropped, rows, flushed, flushErrors atomic.Uint64

	// seenHosts is the descriptor split: a host's static CPU facts are written
	// on first sight rather than every tick. Owned by the writer goroutine.
	seenHosts map[string]struct{}
	// lastSocketsStamp dedupes the sockets table, which arrives repeated on the
	// slower cadence its collector samples at. Owned by the writer goroutine.
	lastSocketsStamp int64
}

// Start subscribes and begins writing. The returned Tee runs until Stop.
func Start(opts Options) (inst *Tee, err error) {
	if opts.Bus == nil {
		err = eh.Errorf("sysmtee: needs a Bus")
		return
	}
	if opts.Store == nil {
		err = eh.Errorf("sysmtee: needs a Store")
		return
	}
	if opts.Host == "" {
		err = eh.Errorf("sysmtee: needs a Host token — it is stored on every row and cannot be recovered from the message")
		return
	}
	if opts.Subject == "" {
		opts.Subject = sysmetricsbus.BundleSubject(opts.Host)
	}
	if opts.QueueDepth <= 0 {
		opts.QueueDepth = DefaultQueueDepth
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = DefaultFlushInterval
	}
	if opts.FlushRows <= 0 {
		opts.FlushRows = DefaultFlushRows
	}

	inst = &Tee{
		opts:      opts,
		queue:     make(chan *sysmsnap.BundleSnapshot, opts.QueueDepth),
		done:      make(chan struct{}),
		seenHosts: make(map[string]struct{}, 1),
	}

	inst.consumer, err = sysmetricsbus.NewConsumer(sysmetricsbus.ConsumerOptions{
		Bus:     opts.Bus,
		Subject: opts.Subject,
		Codec:   sysmetricsbus.NewCBORCodec(),
		Handler: inst.enqueue,
		Log:     opts.Log,
	})
	if err != nil {
		err = eh.Errorf("sysmtee: consumer: %w", err)
		inst = nil
		return
	}
	err = inst.consumer.Start()
	if err != nil {
		err = eh.Errorf("sysmtee: subscribe %s: %w", opts.Subject, err)
		inst = nil
		return
	}
	go inst.run()
	return
}

// enqueue runs on the bus goroutine. It never blocks: a full queue means the
// writer is behind, and stalling here would delay every other subscriber on an
// in-process bus.
func (inst *Tee) enqueue(snap *sysmsnap.BundleSnapshot) {
	if snap == nil {
		return
	}
	inst.bundles.Add(1)
	select {
	case inst.queue <- snap:
	default:
		n := inst.dropped.Add(1)
		// One line per drop would itself be a load problem on a stalled sink.
		if n == 1 || n%100 == 0 {
			inst.opts.Log.Warn().Uint64("dropped", n).
				Msg("sysmtee: writer behind, dropping bundle")
		}
	}
}

// run owns the store. Every store call in the package happens here.
func (inst *Tee) run() {
	defer close(inst.done)
	ticker := time.NewTicker(inst.opts.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case snap, ok := <-inst.queue:
			if !ok {
				inst.flush()
				return
			}
			inst.ingest(snap)
			if inst.opts.Store.Buffered() >= inst.opts.FlushRows {
				inst.flush()
			}
		case <-ticker.C:
			inst.flush()
		}
	}
}

// ingest converts one bundle into rows.
//
// A domain whose collector was not wired is absent from the bundle and
// contributes nothing — absence stays absence rather than becoming a row of
// zeros, which would be indistinguishable from a genuinely idle machine.
//
// One domain failing does not stop the others: a bundle is a union of
// independent collectors, and dropping the whole tick because one of them
// produced an unencodable row would lose data the rest of the plane got right.
func (inst *Tee) ingest(snap *sysmsnap.BundleSnapshot) {
	ts := sampleTime(snap)
	host := inst.opts.Host
	store := inst.opts.Store

	if snap.CPU != nil {
		// Static CPU facts once per host, not per tick.
		if _, seen := inst.seenHosts[host]; !seen {
			if ingestKind(inst, "cpuInfo", ts,
				func() (sysmfacts.SysCpuInfo, error) { return cpuInfoRow(host, snap.CPU, ts) },
				store.IngestSysCpuInfo) {
				inst.seenHosts[host] = struct{}{}
			}
		}
		ingestKind(inst, "cpu", ts,
			func() (sysmfacts.SysCpu, error) { return cpuRow(host, snap.CPU, ts) },
			store.IngestSysCpu)
	}
	if snap.Mem != nil {
		ingestKind(inst, "mem", ts,
			func() (sysmfacts.SysMem, error) { return memRow(host, snap.Mem, ts) },
			store.IngestSysMem)
	}
	if snap.PSI != nil {
		ingestKind(inst, "psi", ts,
			func() (sysmfacts.SysPsi, error) { return psiRow(host, snap.PSI, ts) },
			store.IngestSysPsi)
	}
	if snap.Net != nil {
		ingestKind(inst, "net", ts,
			func() (sysmfacts.SysNet, error) { return netRow(host, snap.Net, ts) },
			store.IngestSysNet)
	}
	if snap.Disk != nil {
		// Two kinds from one snapshot: the mount table and the device list have
		// independent lengths, so they cannot share an entity's arrays.
		ingestKind(inst, "diskMount", ts,
			func() (sysmfacts.SysDiskMount, error) { return diskMountRow(host, snap.Disk, ts) },
			store.IngestSysDiskMount)
		ingestKind(inst, "diskIo", ts,
			func() (sysmfacts.SysDiskIo, error) { return diskIoRow(host, snap.Disk, ts) },
			store.IngestSysDiskIo)
	}
	if snap.Battery != nil {
		ingestKind(inst, "battery", ts,
			func() (sysmfacts.SysBattery, error) { return batteryRow(host, snap.Battery, ts) },
			store.IngestSysBattery)
	}
	if snap.GPU != nil {
		ingestKind(inst, "gpu", ts,
			func() (sysmfacts.SysGpu, error) { return gpuRow(host, snap.GPU, ts) },
			store.IngestSysGpu)
	}
	if len(snap.Procs) > 0 {
		ingestKind(inst, "proc", ts,
			func() (sysmfacts.SysProc, error) { return procRow(host, snap.Procs, ts) },
			store.IngestSysProc)
		// Command lines, user names and ids only when a deployment asked for
		// them (ADR-0090 §SD8; see SysProcCmd for why this is a kind and not a
		// tag).
		if inst.opts.PersistProcCmd {
			ingestKind(inst, "procCmd", ts,
				func() (sysmfacts.SysProcCmd, error) { return procCmdRow(host, snap.Procs, ts) },
				store.IngestSysProcCmd)
		}
	}
	// The sockets collector runs on its own slower cadence, so consecutive
	// bundles carry the same snapshot. Writing one row per bundle would store
	// the same observation many times over and put a rising Order on rows that
	// never changed; the stamp is what tells one observation from a re-send.
	if snap.Sockets != nil && snap.Sockets.CollectedAtUnixMs != inst.lastSocketsStamp {
		if ingestKind(inst, "sockets", ts,
			func() (sysmfacts.SysSocket, error) { return socketRow(host, snap.Sockets) },
			store.IngestSysSocket) {
			inst.lastSocketsStamp = snap.Sockets.CollectedAtUnixMs
		}
	}
}

// ingestKind builds one row and hands it to the store, reporting whether it
// landed. Generic because the nine kinds differ only in their row type and
// their Ingest verb, and spelling each out invited the copy-paste error of
// pairing one domain's builder with another's verb.
func ingestKind[T any](
	inst *Tee,
	domain string,
	ts time.Time,
	build func() (T, error),
	ingest func(time.Time, []T) error,
) (ok bool) {
	row, err := build()
	if err == nil {
		err = ingest(ts, []T{row})
	}
	if err != nil {
		inst.opts.Log.Warn().Err(err).Str("domain", domain).
			Msg("sysmtee: ingest failed, sample dropped")
		return
	}
	inst.rows.Add(1)
	ok = true
	return
}

// flush makes buffered rows durable. A failure leaves the batch pending in the
// store, so the next flush reships it — no row is dropped here, only delayed.
func (inst *Tee) flush() {
	if inst.opts.Store.Buffered() == 0 {
		return
	}
	// Bounded so a hung server cannot wedge the writer goroutine past Stop.
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	n, err := inst.opts.Store.Flush(ctx)
	if err != nil {
		inst.flushErrors.Add(1)
		inst.opts.Log.Warn().Err(err).Msg("sysmtee: flush failed, batch stays pending")
		return
	}
	inst.flushed.Add(uint64(n))
}

// flushTimeout bounds one insert. Longer than a healthy insert by a wide
// margin; short enough that Stop returns.
const flushTimeout = 30 * time.Second

// Stop unsubscribes, drains what is already queued, and makes it durable.
// Safe to call more than once.
func (inst *Tee) Stop() (err error) {
	inst.stopOnce.Do(func() {
		err = inst.consumer.Close()
		close(inst.queue)
		<-inst.done
	})
	return
}

// Stats snapshots the counters.
func (inst *Tee) Stats() (s Stats) {
	s = Stats{
		Bundles:     inst.bundles.Load(),
		Dropped:     inst.dropped.Load(),
		Rows:        inst.rows.Load(),
		Flushed:     inst.flushed.Load(),
		FlushErrors: inst.flushErrors.Load(),
	}
	return
}

// sampleTime prefers the bundle's own stamp, falling back to now when a
// producer left it unset. It is the store's Order lane, so it must be
// monotonic per key in practice — the scraper's tick provides that.
func sampleTime(snap *sysmsnap.BundleSnapshot) (ts time.Time) {
	if snap.SampledAtUnixMs > 0 {
		ts = time.UnixMilli(snap.SampledAtUnixMs).UTC()
		return
	}
	ts = time.Now().UTC()
	return
}
