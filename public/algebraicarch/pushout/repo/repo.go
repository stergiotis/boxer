// Package repo is the domain-neutral pushout engine: a patch log plus a
// graggle, persisted through a pluggable [StorageI], serialized through
// a pluggable wire-codec [envelope.Registry], and synchronized through
// the transport-agnostic exchange package. It speaks patches and hashes
// only — domain adapters (e.g. the pijul KV demo) translate their nouns
// into changes and read back through [Repo.View].
//
// Concurrency: verbs take an exclusive lock and are transactional
// (clone-and-swap in memory; snapshot-before-log-before-commit on
// disk); reads run under a shared lock and never mutate. Time enters
// only through Options.Clock.
package repo

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/algo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/store"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// PatchInfo is the logical view of a stored envelope.
type PatchInfo struct {
	Patch     *patch.Patch
	Producer  string
	Timestamp time.Time
	Codec     string // wire codec the envelope is persisted with
}

// Hooks are nil-safe observability callbacks, invoked synchronously
// after the corresponding operation committed. Keep them fast; they run
// under the engine's write lock.
type Hooks struct {
	OnApplied   func(ev AppliedEvent)
	OnUnrecord  func(ev UnrecordEvent)
	OnSwept     func(ev SweptEvent)
	OnRecovered func(ev RecoveredEvent)
}

type AppliedEvent struct {
	Hash          t.PatchHash
	Producer      string
	NewlyRecorded bool // true for local Record, false for ApplyEnvelope
}

type UnrecordEvent struct {
	Hash t.PatchHash
}

type SweptEvent struct {
	Now     time.Time
	Horizon time.Duration
	Purged  []t.NodeID
}

type RecoveredEvent struct {
	Applied      int  // total applied patches after recovery
	FromSnapshot bool // snapshot prefix was usable
	Replayed     int  // envelopes replayed on top
}

// Options configures Open. Storage, Codecs, Wire, and Producer are
// required; Clock defaults to time.Now.
type Options struct {
	Storage  StorageI
	Codecs   *envelope.Registry
	Wire     string // codec name used for locally recorded envelopes
	Producer string
	Clock    func() time.Time
	Hooks    Hooks
}

// Repo is one participant's repository.
type Repo struct {
	mu sync.RWMutex

	st       StorageI
	reg      *envelope.Registry
	wire     string
	producer string
	clock    func() time.Time
	hooks    Hooks

	g          *store.Graggle
	applied    []t.PatchHash
	appliedSet map[t.PatchHash]struct{}

	metaMu sync.Mutex
	meta   map[t.PatchHash]PatchInfo // lazy cache over storage envelopes

	closed bool
}

// Open recovers (or freshly initialises) a repo from storage: load the
// applied log; if a snapshot exists whose Applied list is a prefix of
// the log, restore it and replay only the suffix envelopes, otherwise
// replay everything from an empty graggle. Replay enforces the engine's
// own guarantees (envelope present and decodable, identity matches the
// log entry, dependencies precede dependents) and refuses to open a
// store that violates them (ErrCorruptStore).
func Open(ctx context.Context, opts Options) (r *Repo, err error) {
	if opts.Storage == nil || opts.Codecs == nil {
		err = eh.Errorf("Options.Storage and Options.Codecs are required")
		return
	}
	if _, err = opts.Codecs.Lookup(opts.Wire); err != nil {
		return
	}
	if opts.Producer == "" {
		err = eh.Errorf("Options.Producer is required")
		return
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}

	applied, err := opts.Storage.LoadApplied(ctx)
	if err != nil {
		return
	}
	snap, haveSnap, err := opts.Storage.LoadSnapshot(ctx)
	if err != nil {
		return
	}

	r0 := &Repo{
		st:         opts.Storage,
		reg:        opts.Codecs,
		wire:       opts.Wire,
		producer:   opts.Producer,
		clock:      opts.Clock,
		hooks:      opts.Hooks,
		appliedSet: make(map[t.PatchHash]struct{}, len(applied)),
		meta:       make(map[t.PatchHash]PatchInfo),
	}

	replayFrom := 0
	fromSnapshot := false
	if haveSnap && isPrefix(snap.Applied, applied) {
		g, derr := store.DecodeSnapshot(snap.Graggle)
		if derr != nil {
			err = eh.Errorf("snapshot: %w", errors.Join(ErrCorruptStore, derr))
			return
		}
		r0.g = g
		replayFrom = len(snap.Applied)
		fromSnapshot = true
	} else {
		r0.g = store.New()
	}
	r0.g.SetClock(opts.Clock)
	for _, h := range applied[:replayFrom] {
		r0.appliedSet[h] = struct{}{}
	}

	for i := replayFrom; i < len(applied); i++ {
		if err = ctx.Err(); err != nil {
			return
		}
		h := applied[i]
		framed, gerr := opts.Storage.GetEnvelope(ctx, h)
		if gerr != nil {
			err = eh.Errorf("applied %s has no envelope: %w", h, errors.Join(ErrCorruptStore, gerr))
			return
		}
		env, codecName, derr := opts.Codecs.Decode(framed)
		if derr != nil {
			err = eh.Errorf("applied %s: %w", h, errors.Join(ErrCorruptStore, derr))
			return
		}
		if env.Patch.Hash != h {
			err = eh.Errorf("envelope for %s carries patch %s: %w", h, env.Patch.Hash, ErrCorruptStore)
			return
		}
		for _, dep := range env.Patch.Dependencies {
			if _, ok := r0.appliedSet[dep]; !ok {
				err = eh.Errorf("applied %s precedes its dependency %s: %w", h, dep, ErrCorruptStore)
				return
			}
		}
		if aerr := env.Patch.Apply(r0.g); aerr != nil {
			err = eh.Errorf("replay %s: %w", h, errors.Join(ErrCorruptStore, aerr))
			return
		}
		r0.appliedSet[h] = struct{}{}
		r0.meta[h] = PatchInfo{Patch: env.Patch, Producer: env.Producer, Timestamp: env.Timestamp, Codec: codecName}
	}
	r0.applied = slices.Clone(applied)

	// Seed replay-stable retention horizons from the durable ledger, then
	// persist the reconciled set. Full replay re-stamped tombstoneAt to
	// replay time, so without this the horizon would reset on every
	// snapshot-less or non-prefix open (ADR-0079). The reconcile adopts the
	// ledger's stamp where present, keeps the decode/replay stamp for
	// tombstones new to the ledger, and drops entries for nodes no longer
	// tombstoned — writing back only when something changed.
	retEntries, lerr := opts.Storage.LoadRetention(ctx)
	if lerr != nil {
		err = lerr
		return
	}
	ledger := make(map[t.NodeID]time.Time, len(retEntries))
	for _, e := range retEntries {
		ledger[e.Node] = time.Unix(0, e.UnixNano)
	}
	r0.g.SeedTombstoneStamps(ledger)
	if retentionChanged(r0.g, ledger) {
		if serr := r0.saveRetentionLocked(ctx, r0.g); serr != nil {
			err = serr
			return
		}
	}

	if r0.hooks.OnRecovered != nil {
		r0.hooks.OnRecovered(RecoveredEvent{
			Applied:      len(applied),
			FromSnapshot: fromSnapshot,
			Replayed:     len(applied) - replayFrom,
		})
	}
	r = r0
	return
}

func isPrefix(prefix, full []t.PatchHash) bool {
	if len(prefix) > len(full) {
		return false
	}
	for i := range prefix {
		if prefix[i] != full[i] {
			return false
		}
	}
	return true
}

// Record builds a patch from the changes (computing dependencies from
// the referenced nodes), disambiguates its identity against the applied
// set, applies it transactionally, and persists envelope + log entry.
//
// Identity collision: identical changes against identical anchors
// reproduce the hash of an applied patch (typical when re-creating
// previously deleted content). The placeholder index space is shifted
// deterministically — but only relative to the LOCAL applied set, so the
// shift count depends on which colliding patches this repo happens to
// hold. Two repos converge on one patch for an identical re-creation only
// when their colliding histories agree; with divergent histories they may
// land on different shift counts and thus different hashes, and the
// re-creation surfaces post-sync as a benign duplicate (a fork conflict,
// indistinguishable from two actors independently typing the same line).
// Fleet-wide single-identity for identical re-creation is not a guarantee
// the local shift can make — see ADR-0079.
func (inst *Repo) Record(ctx context.Context, author, message string, changes []patch.Change) (h t.PatchHash, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.checkOpenLocked(); err != nil {
		return
	}
	if len(changes) == 0 {
		err = eh.Errorf("%w", ErrNoChanges)
		return
	}

	deps := patch.ComputeDependencies(changes)
	p := patch.NewPatch(author, message, deps, changes)
	for attempt := uint64(1); ; attempt++ {
		if _, clash := inst.appliedSet[p.Hash]; !clash {
			break
		}
		if attempt > 16 {
			err = eh.Errorf("applied patch %s: %w", p.Hash, ErrIdentityExhausted)
			return
		}
		p = patch.NewPatch(author, message, deps, shiftPlaceholderIndexes(changes, attempt<<32))
	}

	env := envelope.EnvelopeV1{Patch: p, Producer: inst.producer, Timestamp: inst.clock()}
	framed, err := inst.reg.Encode(inst.wire, env)
	if err != nil {
		return
	}
	info := PatchInfo{Patch: p, Producer: env.Producer, Timestamp: env.Timestamp, Codec: inst.wire}
	if err = inst.commitPatchLocked(ctx, p, framed, info); err != nil {
		return
	}
	if inst.hooks.OnApplied != nil {
		inst.hooks.OnApplied(AppliedEvent{Hash: p.Hash, Producer: env.Producer, NewlyRecorded: true})
	}
	h = p.Hash
	return
}

// ApplyEnvelope ingests a framed envelope (idempotently: a duplicate of
// an applied patch returns applied=false, nil). Dependencies must be in
// the APPLIED set — merely having seen an envelope is not enough.
// The bytes are persisted as received, so the envelope re-ships in its
// original codec.
func (inst *Repo) ApplyEnvelope(ctx context.Context, framed []byte) (h t.PatchHash, applied bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.checkOpenLocked(); err != nil {
		return
	}
	env, codecName, err := inst.reg.Decode(framed)
	if err != nil {
		return
	}
	h = env.Patch.Hash
	if _, dup := inst.appliedSet[h]; dup {
		return
	}
	for _, dep := range env.Patch.Dependencies {
		if _, ok := inst.appliedSet[dep]; !ok {
			err = eh.Errorf("patch %s needs %s: %w", h, dep, ErrMissingDependency)
			return
		}
	}
	info := PatchInfo{Patch: env.Patch, Producer: env.Producer, Timestamp: env.Timestamp, Codec: codecName}
	if err = inst.commitPatchLocked(ctx, env.Patch, slices.Clone(framed), info); err != nil {
		return
	}
	applied = true
	if inst.hooks.OnApplied != nil {
		inst.hooks.OnApplied(AppliedEvent{Hash: h, Producer: env.Producer, NewlyRecorded: false})
	}
	return
}

// commitPatchLocked is the shared transactional tail of Record and
// ApplyEnvelope: graph-apply on a clone, then envelope, then log append,
// then in-memory commit — the ack-ordering that makes crashes safe.
func (inst *Repo) commitPatchLocked(ctx context.Context, p *patch.Patch, framed []byte, info PatchInfo) (err error) {
	next := inst.g.Clone()
	if aerr := p.Apply(next); aerr != nil {
		err = eh.Errorf("apply %s: %w", p.Hash, aerr)
		return
	}
	if err = inst.st.PutEnvelope(ctx, p.Hash, framed); err != nil {
		return
	}
	// When this patch can create tombstones, persist the retention ledger
	// before the commit point (AppendApplied) so the horizon is durable
	// before the verb acks. A failure fails the verb; the orphan envelope
	// and any orphan ledger entry are harmless — recovery reconciles.
	if patchTombstones(p) {
		if err = inst.saveRetentionLocked(ctx, next); err != nil {
			return
		}
	}
	if err = inst.st.AppendApplied(ctx, p.Hash); err != nil {
		return
	}
	inst.g = next
	inst.applied = append(inst.applied, p.Hash)
	inst.appliedSet[p.Hash] = struct{}{}
	inst.metaMu.Lock()
	inst.meta[p.Hash] = info
	inst.metaMu.Unlock()
	return
}

// Unrecord backs out an applied patch. It refuses while any applied
// patch declares the target among its dependencies (ErrDependentExists)
// and when a retention sweep made the patch permanent
// (ErrRetentionBlocked; also matches patch.ErrRetentionPermanent). The
// envelope is kept — a later Pull or ApplyEnvelope reapplies cleanly.
//
// Disk ordering: snapshot of the post-unrecord state first, then the
// atomic log rewrite, then the in-memory commit. A crash between the
// two leaves a non-prefix snapshot that recovery discards.
func (inst *Repo) Unrecord(ctx context.Context, h t.PatchHash) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.checkOpenLocked(); err != nil {
		return
	}
	idx := slices.Index(inst.applied, h)
	if idx < 0 {
		err = eh.Errorf("patch %s: %w", h, ErrNotApplied)
		return
	}
	for _, other := range inst.applied {
		if other == h {
			continue
		}
		oInfo, ierr := inst.patchInfo(ctx, other)
		if ierr != nil {
			err = ierr
			return
		}
		if slices.Contains(oInfo.Patch.Dependencies, h) {
			err = eh.Errorf("patch %s is required by %s: %w", h, other, ErrDependentExists)
			return
		}
	}
	info, err := inst.patchInfo(ctx, h)
	if err != nil {
		return
	}

	next := inst.g.Clone()
	if uerr := info.Patch.Unapply(next); uerr != nil {
		if errors.Is(uerr, patch.ErrRetentionPermanent) {
			err = eh.Errorf("unrecord %s: %w", h, errors.Join(ErrRetentionBlocked, uerr))
			return
		}
		err = eh.Errorf("unrecord %s: %w", h, uerr)
		return
	}
	newApplied := slices.Delete(slices.Clone(inst.applied), idx, idx+1)

	if err = inst.saveSnapshotLocked(ctx, next, newApplied); err != nil {
		return
	}
	// Unrecord can resurrect a node (dropping its tombstoneAt), so refresh
	// the ledger before the commit point (ReplaceApplied).
	if err = inst.saveRetentionLocked(ctx, next); err != nil {
		return
	}
	if err = inst.st.ReplaceApplied(ctx, newApplied); err != nil {
		return
	}
	inst.g = next
	inst.applied = newApplied
	delete(inst.appliedSet, h)
	// meta keeps the entry: PatchInfo stays answerable for unrecorded
	// patches, and the kept envelope makes re-apply cheap.
	if inst.hooks.OnUnrecord != nil {
		inst.hooks.OnUnrecord(UnrecordEvent{Hash: h})
	}
	return
}

// SweepReport describes one retention sweep.
type SweepReport struct {
	Purged []t.NodeID
}

// Sweep destroys tombstone content older than now-horizon and makes the
// purge DURABLE before returning: the swept state is snapshotted to
// storage first, then committed in memory. A sweep that purges nothing
// performs no disk write.
func (inst *Repo) Sweep(ctx context.Context, now time.Time, horizon time.Duration) (report SweepReport, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.checkOpenLocked(); err != nil {
		return
	}
	next := inst.g.Clone()
	count, purged := next.SweepTombstones(now, horizon)
	if count == 0 {
		return
	}
	if err = inst.saveSnapshotLocked(ctx, next, inst.applied); err != nil {
		return
	}
	inst.g = next
	report.Purged = purged
	if inst.hooks.OnSwept != nil {
		inst.hooks.OnSwept(SweptEvent{Now: now, Horizon: horizon, Purged: purged})
	}
	return
}

// Checkpoint persists a snapshot of the current state, bounding the
// replay work of the next Open. Safe to call at any time.
func (inst *Repo) Checkpoint(ctx context.Context) (err error) {
	inst.mu.RLock()
	if err = inst.checkOpenLocked(); err != nil {
		inst.mu.RUnlock()
		return
	}
	g := inst.g
	applied := slices.Clone(inst.applied)
	data, err := g.EncodeSnapshot()
	inst.mu.RUnlock()
	if err != nil {
		return
	}
	err = inst.st.SaveSnapshot(ctx, Snapshot{Applied: applied, Graggle: data})
	return
}

// saveSnapshotLocked snapshots the GIVEN state (typically the
// about-to-be-committed clone) under the already-held write lock.
func (inst *Repo) saveSnapshotLocked(ctx context.Context, g *store.Graggle, applied []t.PatchHash) (err error) {
	data, err := g.EncodeSnapshot()
	if err != nil {
		return
	}
	err = inst.st.SaveSnapshot(ctx, Snapshot{Applied: slices.Clone(applied), Graggle: data})
	return
}

// saveRetentionLocked persists the durable retention ledger from the
// GIVEN graggle's tombstone stamps. Called on tombstone-changing commits
// and at Open so a full replay cannot reset retention horizons (ADR-0079).
func (inst *Repo) saveRetentionLocked(ctx context.Context, g *store.Graggle) (err error) {
	stamps := g.TombstoneStamps()
	entries := make([]RetentionEntry, 0, len(stamps))
	for id, when := range stamps {
		entries = append(entries, RetentionEntry{Node: id, UnixNano: when.UnixNano()})
	}
	err = inst.st.SaveRetention(ctx, entries)
	return
}

// patchTombstones reports whether applying p can change the tombstone set
// (it carries a DeleteNode change), so the caller knows to refresh the
// retention ledger.
func patchTombstones(p *patch.Patch) bool {
	for _, c := range p.Changes {
		if c.Kind == patch.ChangeKindDeleteNode {
			return true
		}
	}
	return false
}

// retentionChanged reports whether g's current tombstone stamps differ
// from the loaded ledger (a node added, dropped, or re-stamped), so Open
// rewrites the ledger only when the reconcile actually changed it.
func retentionChanged(g *store.Graggle, ledger map[t.NodeID]time.Time) bool {
	current := g.TombstoneStamps()
	if len(current) != len(ledger) {
		return true
	}
	for id, when := range current {
		if lw, ok := ledger[id]; !ok || !lw.Equal(when) {
			return true
		}
	}
	return false
}

// Close checkpoints and releases the storage. The repo is unusable
// afterwards.
func (inst *Repo) Close(ctx context.Context) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return
	}
	data, eerr := inst.g.EncodeSnapshot()
	if eerr == nil {
		eerr = inst.st.SaveSnapshot(ctx, Snapshot{Applied: slices.Clone(inst.applied), Graggle: data})
	}
	cerr := inst.st.Close()
	inst.closed = true
	err = errors.Join(eerr, cerr)
	return
}

func (inst *Repo) checkOpenLocked() (err error) {
	if inst.closed {
		err = eh.Errorf("%w", ErrClosed)
	}
	return
}

// Applied returns a copy of the applied log, in apply order.
func (inst *Repo) Applied(ctx context.Context) (hs []t.PatchHash, err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if err = inst.checkOpenLocked(); err != nil {
		return
	}
	hs = slices.Clone(inst.applied)
	return
}

// RetentionStamps returns the durable retention horizon: each tombstoned
// node's first-observed-deleted time on THIS replica, in CompareNodeID
// order. Replica-local policy (see ADR-0079), for audit/observability and
// crash-recovery tests; it is the in-memory view of the same data the
// engine persists as the retention ledger.
func (inst *Repo) RetentionStamps(ctx context.Context) (entries []RetentionEntry, err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if err = inst.checkOpenLocked(); err != nil {
		return
	}
	stamps := inst.g.TombstoneStamps()
	entries = make([]RetentionEntry, 0, len(stamps))
	for id, when := range stamps {
		entries = append(entries, RetentionEntry{Node: id, UnixNano: when.UnixNano()})
	}
	slices.SortFunc(entries, func(a, b RetentionEntry) int { return t.CompareNodeID(a.Node, b.Node) })
	return
}

// EncodedEnvelope returns the framed envelope bytes for shipping. Works
// for unrecorded patches too — envelopes are kept.
func (inst *Repo) EncodedEnvelope(ctx context.Context, h t.PatchHash) (framed []byte, err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if err = inst.checkOpenLocked(); err != nil {
		return
	}
	framed, err = inst.st.GetEnvelope(ctx, h)
	return
}

// PatchInfo returns the logical envelope for h (applied or merely seen).
func (inst *Repo) PatchInfo(ctx context.Context, h t.PatchHash) (info PatchInfo, err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if err = inst.checkOpenLocked(); err != nil {
		return
	}
	info, err = inst.patchInfo(ctx, h)
	return
}

// patchInfo serves from the meta cache, lazily decoding from storage on
// miss (snapshot-covered history is not decoded at Open). Guarded by
// metaMu so it is callable under either engine lock.
func (inst *Repo) patchInfo(ctx context.Context, h t.PatchHash) (info PatchInfo, err error) {
	inst.metaMu.Lock()
	defer inst.metaMu.Unlock()
	if cached, ok := inst.meta[h]; ok {
		info = cached
		return
	}
	framed, err := inst.st.GetEnvelope(ctx, h)
	if err != nil {
		return
	}
	env, codecName, err := inst.reg.Decode(framed)
	if err != nil {
		return
	}
	if env.Patch.Hash != h {
		err = eh.Errorf("envelope for %s carries patch %s: %w", h, env.Patch.Hash, ErrCorruptStore)
		return
	}
	info = PatchInfo{Patch: env.Patch, Producer: env.Producer, Timestamp: env.Timestamp, Codec: codecName}
	inst.meta[h] = info
	return
}

// ViewI is a consistent read transaction. Values obtained through it
// (the graph, node ids, slices) must not be retained or used after the
// View callback returns.
type ViewI interface {
	Graph() t.GraphReaderI
	Visualizable() t.VisualizableI
	Applied() []t.PatchHash
	PatchInfo(h t.PatchHash) (PatchInfo, bool)
	LinearOrder() []t.NodeID
	Conflicts() []algo.ConflictInfo
}

// View runs fn under a shared lock against a resolved-at-rest graggle.
// Reads never mutate: every verb leaves the graggle resolved, which
// View asserts instead of defensively re-resolving.
func (inst *Repo) View(ctx context.Context, fn func(v ViewI) error) (err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if err = inst.checkOpenLocked(); err != nil {
		return
	}
	if n := inst.g.DirtyRepCount(); n != 0 {
		err = eh.Errorf("graggle at rest has %d dirty pseudo-edge components — engine bug", n)
		return
	}
	err = fn(&view{r: inst, ctx: ctx})
	return
}

type view struct {
	r   *Repo
	ctx context.Context
}

func (inst *view) Graph() t.GraphReaderI         { return inst.r.g }
func (inst *view) Visualizable() t.VisualizableI { return inst.r.g }

func (inst *view) Applied() []t.PatchHash {
	return slices.Clone(inst.r.applied)
}

func (inst *view) PatchInfo(h t.PatchHash) (info PatchInfo, ok bool) {
	info, err := inst.r.patchInfo(inst.ctx, h)
	ok = err == nil
	return
}

func (inst *view) LinearOrder() []t.NodeID {
	return algo.LinearOrder(inst.r.g)
}

func (inst *view) Conflicts() []algo.ConflictInfo {
	return algo.DetectConflicts(inst.r.g)
}

// shiftPlaceholderIndexes returns a copy of changes with every
// placeholder NodeID's Index raised by offset — including placeholder
// references inside contexts and edge endpoints, so chained inserts stay
// consistent. Node identities change while content, anchors, and
// dependencies stay put.
func shiftPlaceholderIndexes(changes []patch.Change, offset uint64) (out []patch.Change) {
	shift := func(id t.NodeID) t.NodeID {
		if id.Patch.IsPlaceholder() {
			id.Index += offset
		}
		return id
	}
	out = make([]patch.Change, len(changes))
	for i, c := range changes {
		out[i] = c
		out[i].NodeID = shift(c.NodeID)
		out[i].Src = shift(c.Src)
		out[i].Dest = shift(c.Dest)
		out[i].UpContext = slices.Clone(c.UpContext)
		for j := range out[i].UpContext {
			out[i].UpContext[j] = shift(out[i].UpContext[j])
		}
		out[i].DownContext = slices.Clone(c.DownContext)
		for j := range out[i].DownContext {
			out[i].DownContext[j] = shift(out[i].DownContext[j])
		}
	}
	return
}
