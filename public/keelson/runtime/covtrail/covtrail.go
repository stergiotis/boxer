// Package covtrail puts coverage on the audit trail (ADR-0343): the build
// row that anchors a coverage meta hash to the binary and its commit
// (§SD2), the function table once per hash (§SD3), the run's covered-set
// growth from the sampler's ticks (§SD4), and, per agent command, the set
// of units the command's handler executed, read around the handler call
// on the render goroutine (§SD5).
package covtrail

import (
	"bytes"
	"context"
	"encoding/hex"
	"iter"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/coverage"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// AppId is the origin the run-scoped rows carry: the build row, the
// function table and the samples are the coverage service's, on no window.
const AppId app.AppIdT = "runtime.coverage"

// funcFlushEvery bounds how many function-table rows sit in the recorder
// before a flush, well under its backlog cap.
const funcFlushEvery = 4096

// MetaSourceI is what the service needs from the sampler: the build's
// decoded lookup profile. *coverage.Sampler satisfies it.
type MetaSourceI interface {
	Meta() (meta *covsnap.MetaProfile)
}

// CounterReader reads the live counters; the production reader is
// coverage.ReadCounters over the service's buffer, and a test's is a stub.
type CounterReader func(buf *bytes.Buffer) (snap *covsnap.CounterSnapshot, err error)

// Options configures New.
type Options struct {
	// Trail is the recorder the rows go to; without one nothing is written.
	Trail *trail.Recorder
	// Run names the binary and its commit for the build row; nil leaves
	// them empty, an unanchored build.
	Run *runinfo.Inst
	Log zerolog.Logger
	// Read overrides the counter reader; nil is the live one.
	Read CounterReader
	// Clock overrides time.Now.
	Clock func() time.Time
}

// Service is the coverage trail writer of one process. It is built before
// the sampler, so its sink can be handed to the producer, and attached to
// the sampler once that exists; updates that arrive in between are held.
type Service struct {
	rec   *trail.Recorder
	run   *runinfo.Inst
	log   zerolog.Logger
	read  CounterReader
	clock func() time.Time

	mu      sync.Mutex
	meta    *covsnap.MetaProfile
	hash    string
	pending []*covsnap.Update

	// buf is the bracket's read buffer, shared by every window's bracket:
	// they all run on the one render goroutine (ADR-0261), and the lock
	// keeps that an assumption a test can break without a race.
	bufMu sync.Mutex
	buf   bytes.Buffer
}

// New builds the service; it writes nothing until Attach.
func New(opts Options) (inst *Service) {
	inst = &Service{rec: opts.Trail, run: opts.Run, log: opts.Log, read: opts.Read, clock: opts.Clock}
	if inst.read == nil {
		inst.read = coverage.ReadCounters
	}
	if inst.clock == nil {
		inst.clock = time.Now
	}
	return
}

// OnUpdate is the producer's sink (ADR-0343 §SD4): a sample row per tick
// that found newly covered units. Before Attach, updates are held.
func (inst *Service) OnUpdate(upd *covsnap.Update) {
	inst.mu.Lock()
	if inst.meta == nil {
		inst.pending = append(inst.pending, upd)
		inst.mu.Unlock()
		return
	}
	inst.mu.Unlock()
	inst.writeSample(upd)
}

// Attach binds the service to the build the sampler decoded: it writes
// the build row and, when the store holds none for the hash, the function
// table — off the caller's goroutine — then drains the held updates.
func (inst *Service) Attach(ctx context.Context, src MetaSourceI) (err error) {
	meta := src.Meta()
	if meta == nil {
		return eh.Errorf("coverage trail: the sampler has no meta profile")
	}
	inst.mu.Lock()
	if inst.meta != nil {
		inst.mu.Unlock()
		return eh.Errorf("coverage trail: already attached")
	}
	inst.meta = meta
	inst.hash = hex.EncodeToString(meta.Hash[:])
	pending := inst.pending
	inst.pending = nil
	inst.mu.Unlock()
	go inst.writeBuild(ctx, meta)
	for _, upd := range pending {
		inst.writeSample(upd)
	}
	return nil
}

// Hash is the attached build's meta hash, hex; empty before Attach.
func (inst *Service) Hash() (hash string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.hash
}

func (inst *Service) origin() (c trail.Context) {
	return trail.Context{Origin: inst.rec.OriginOf(AppId, 0)}
}

// writeBuild writes the function table when the hash is new to the store,
// then the build row; the build row last, so a run that ends between the
// two leaves a hash the next run writes the table for again.
func (inst *Service) writeBuild(ctx context.Context, meta *covsnap.MetaProfile) {
	if inst.rec == nil {
		return
	}
	at := inst.clock()
	known, err := inst.hashKnown(ctx)
	if err != nil {
		inst.log.Warn().Err(err).Msg("coverage trail: unable to read the build rows; writing the function table")
	}
	funcs := uint64(0)
	if !known {
		funcs, err = inst.writeFuncs(ctx, at, meta)
		if err != nil {
			inst.log.Warn().Err(err).Msg("coverage trail: unable to write the function table")
			return
		}
	} else {
		for i := range meta.Pkgs {
			funcs += uint64(len(meta.Pkgs[i].Funcs))
		}
	}
	row := trail.CoverageBuild{
		MetaHash: inst.hash, Mode: meta.Mode.String(), Granularity: meta.Granularity.String(),
		Units: uint64(meta.TotalUnits), Stmts: uint64(meta.TotalStmts), Funcs: funcs, Pkgs: uint64(len(meta.Pkgs)),
	}
	if inst.run != nil {
		row.BuildId, row.VcsRevision, row.VcsModified = inst.run.BuildId, inst.run.VcsRevision, inst.run.VcsModified
	}
	if err = inst.rec.CoverageBuild(at, inst.origin(), row); err != nil {
		inst.log.Warn().Err(err).Msg("coverage trail: unable to write the build row")
		return
	}
	inst.rec.FlushSoon()
	inst.log.Info().Str("metaHash", inst.hash).Bool("functionTableWritten", !known).Uint64("funcs", funcs).
		Msg("coverage trail: build row written")
}

// hashKnown reports whether a build row with this hash is in the store.
func (inst *Service) hashKnown(ctx context.Context) (known bool, err error) {
	hash := inst.hash
	ents, err := inst.rec.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanCoverageBuild(ctx, recordstore.ScanOpts{})
	})
	if err != nil {
		return false, err
	}
	for _, e := range ents {
		if e.CoverageBuild.Has && e.CoverageBuild.Val.MetaHash == hash {
			return true, nil
		}
	}
	return false, nil
}

func (inst *Service) writeFuncs(ctx context.Context, at time.Time, meta *covsnap.MetaProfile) (funcs uint64, err error) {
	c := inst.origin()
	batch := make([]trail.CoverageFunc, 0, funcFlushEvery)
	flush := func() (err error) {
		if len(batch) == 0 {
			return nil
		}
		if err = inst.rec.CoverageFuncs(at, c, batch); err != nil {
			return
		}
		batch = batch[:0]
		return inst.rec.Flush(ctx)
	}
	for i := range meta.Pkgs {
		pkg := &meta.Pkgs[i]
		for j := range pkg.Funcs {
			fn := &pkg.Funcs[j]
			row := trail.CoverageFunc{
				MetaHash: inst.hash, Pkg: pkg.Path, Name: fn.Name, File: fn.SrcFile, Lit: fn.Lit,
				UnitBase: fn.UnitBase, Units: uint32(len(fn.Units)), Stmts: fn.NumStmts,
				UnitStLine: make([]uint32, len(fn.Units)), UnitEnLine: make([]uint32, len(fn.Units)), UnitStmts: make([]uint32, len(fn.Units)),
			}
			for k, u := range fn.Units {
				row.UnitStLine[k], row.UnitEnLine[k], row.UnitStmts[k] = u.StLine, u.EnLine, u.NxStmts
			}
			batch = append(batch, row)
			funcs++
			if len(batch) == funcFlushEvery {
				if err = flush(); err != nil {
					return
				}
			}
		}
	}
	err = flush()
	return
}

func (inst *Service) writeSample(upd *covsnap.Update) {
	if inst.rec == nil || upd == nil || upd.Units == nil || upd.Units.IsEmpty() {
		return
	}
	row := trail.CoverageSample{
		MetaHash: inst.hash, Seq: upd.Seq, Units: upd.Units.Clone(),
		CoveredUnits: uint64(upd.Status.CoveredUnits), CoveredStmts: uint64(upd.Status.CoveredStmts), CoveredFuncs: uint64(upd.Status.CoveredFuncs),
	}
	at := time.UnixMilli(upd.SampledAtUnixMs)
	if upd.SampledAtUnixMs == 0 {
		at = inst.clock()
	}
	if err := inst.rec.CoverageSample(at, inst.origin(), row); err != nil {
		inst.log.Warn().Err(err).Msg("coverage trail: unable to write a sample row")
		return
	}
	inst.rec.FlushSoon()
}

// Bracket is the window host's factory (ADR-0343 §SD5): one bracket per
// window's engine, writing an apply row per agent command.
func (inst *Service) Bracket(appId app.AppIdT, key uint64) (b opengine.ApplyBracketI) {
	return &bracket{svc: inst, appId: appId, key: key}
}

// readCounters takes one live read under the shared buffer.
func (inst *Service) readCounters() (snap *covsnap.CounterSnapshot, err error) {
	inst.bufMu.Lock()
	defer inst.bufMu.Unlock()
	return inst.read(&inst.buf)
}

// bracket surrounds one window's agent commands. A read after one command
// serves as the read before the next command of the same frame, so a frame
// applying n commands costs n+1 reads.
type bracket struct {
	svc   *Service
	appId app.AppIdT
	key   uint64

	before      *covsnap.CounterSnapshot
	beforeFrame uint64
	beforeFresh bool
	startedAt   time.Time
	spent       time.Duration
	failed      bool
}

// Begin reads the counters before the handler runs, unless the read after
// the previous command of this frame is still current.
func (inst *bracket) Begin(frame uint64) {
	inst.failed = false
	inst.spent = 0
	inst.startedAt = inst.svc.clock()
	if inst.beforeFresh && inst.beforeFrame == frame && inst.before != nil {
		inst.spent = inst.svc.clock().Sub(inst.startedAt)
		return
	}
	snap, err := inst.svc.readCounters()
	if err != nil {
		inst.failed = true
		inst.before = nil
		inst.svc.log.Debug().Err(err).Msg("coverage trail: counter read before the handler failed")
	} else {
		inst.before, inst.beforeFrame = snap, frame
	}
	inst.spent = inst.svc.clock().Sub(inst.startedAt)
}

// End reads the counters after the handler ran, diffs, and writes the row.
func (inst *bracket) End(a opengine.Applied) {
	inst.beforeFresh = false
	if inst.failed || inst.before == nil {
		return
	}
	t0 := inst.svc.clock()
	after, err := inst.svc.readCounters()
	if err != nil {
		inst.svc.log.Debug().Err(err).Msg("coverage trail: counter read after the handler failed")
		inst.before = nil
		return
	}
	inst.svc.mu.Lock()
	meta, hash := inst.svc.meta, inst.svc.hash
	inst.svc.mu.Unlock()
	if meta == nil {
		return
	}
	d, err := coverage.DiffCounters(meta, inst.before, after)
	if err != nil {
		inst.svc.log.Warn().Err(err).Msg("coverage trail: unable to diff the counter reads")
		inst.before = nil
		return
	}
	// The read after this command is the read before the next one of
	// this frame.
	inst.before, inst.beforeFrame, inst.beforeFresh = after, a.Frame, true
	cost := inst.spent + inst.svc.clock().Sub(t0)
	row := trail.CoverageAgentApply{
		MetaHash: hash, Frame: a.Frame, Units: d.Units,
		ChangedUnits: d.Units.GetCardinality(), ChangedFuncs: uint64(d.Funcs),
		Operation: a.Operation, Phase: a.Phase.String(), CostUs: uint64(cost.Microseconds()),
	}
	c := trail.Context{Origin: inst.svc.rec.OriginOf(inst.appId, inst.key)}
	if ob := a.OnBehalfOf; ob != nil {
		c.Delegation = option.Some(trail.Delegation{Task: ob.Task, Epoch: ob.Epoch, Call: option.Some(ob.Call)})
	}
	if inst.svc.rec == nil {
		return
	}
	if err = inst.svc.rec.CoverageAgentApply(t0, c, row); err != nil {
		inst.svc.log.Warn().Err(err).Msg("coverage trail: unable to write an apply row")
		return
	}
	inst.svc.rec.FlushSoon()
}
