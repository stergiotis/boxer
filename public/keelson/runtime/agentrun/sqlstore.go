package agentrun

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// SqlStore is StoreI over the generated stores (agentrunstore) and the
// executor they were opened on: the record of ADR-0308 §SD1. The
// generated stores are single-goroutine, so every method takes one mutex,
// and every write flushes before it returns, which is what lets a writer
// refuse an act the record does not hold. A run row is inserted once and
// then rewritten in place by a compare-and-set on its version (the run
// table is generated Cas, through rowcas), so a writer in another process
// that moved the row first makes this one's rewrite fail rather than be
// overwritten (ADR-0308 §SD9). The live run rows are a read cache.
//
// The run row carries a writer lease (ADR-0319 §SD7), held by one process
// of the host while the run is open and released when it leaves the open
// state. Every act claims it first, from the row as the table holds it: a
// run held by a live process is refused with [ErrHeldElsewhere], one that
// is released or whose holder no longer runs is taken over by bumping the
// lease epoch. The
// act's entries are written at revision 2×epoch (a tombstone one above),
// so a batch the dead holder still had in flight loses every ordinal the
// new holder writes; the confirming rewrite is guarded on the holder and
// the epoch as well as the version. Reads stop at the run row's head, so a
// row past it is invisible, and a repair adopts only a tail that chains
// from the head.
//
// Journal inserts carry async_insert = 0 (ADR-0309 §SD4): the entry store
// is generated with it and the Cas run table forces it, and an executor
// without the per-insert seam (recordstore.InsertSettingsI) makes the
// store log a deferred warning instead.
type SqlStore struct {
	mu     sync.Mutex
	exec   recordstore.ExecutorI
	layout agentrunstore.Layout
	st     agentrunstore.Stores
	// ids are the run and entry streams (ADR-0325 §SD5); idsErr is why
	// they could not be built, returned by every mint.
	ids    agentrunstore.Ids
	idsErr error
	// Log takes what the store does best-effort and could not: the
	// deletion of a tombstoned run's original rows.
	Log zerolog.Logger
	// live are the run rows as last written or read, by id.
	live map[uint64]Run
	// stored is the version the table holds for a run, as this store last
	// wrote or read it: the guard of the next compare-and-set. It differs
	// from live's when a repair rebuilt a row it could not write.
	stored map[uint64]uint32
	// dirty are the runs whose entries went to the record and whose row
	// did not: the row is rebuilt from the entry tail at the next read.
	dirty map[uint64]bool
	// epoch is the lease epoch this store holds a run at; a run is in it
	// exactly while this store holds the lease.
	epoch map[uint64]uint32
	// Holder is who this store writes as, ThisProcess by default; Liveness
	// says whether another holder still runs, LocalLiveness by default.
	Holder   Holder
	Liveness HolderLivenessI
}

// ErrHeldElsewhere is the refusal of an act on a run another live process
// holds (ADR-0319 §SD7).
var ErrHeldElsewhere = errors.New("the run is open in another process")

var _ StoreI = (*SqlStore)(nil)
var _ AllEntriesI = (*SqlStore)(nil)
var _ EntriesOfI = (*SqlStore)(nil)

// NewSqlStore opens the stores over exec in layout; the caller provisions
// first (agentrunstore.ProvisionIn) and closes afterwards.
func NewSqlStore(exec recordstore.ExecutorI, layout agentrunstore.Layout) (inst *SqlStore) {
	ids, idsErr := agentrunstore.LeasedIds(exec, layout)
	return &SqlStore{exec: exec, layout: layout, st: agentrunstore.NewStores(exec, layout, ids.Runs), ids: ids, idsErr: idsErr, live: make(map[uint64]Run), stored: make(map[uint64]uint32), dirty: make(map[uint64]bool), epoch: make(map[uint64]uint32),
		Log: zerolog.Nop(), Holder: ThisProcess(), Liveness: LocalLiveness{}}
}

// Close releases the generated stores.
func (inst *SqlStore) Close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.st.Close()
}

// MintRun draws the run id from the run store's KeyStream, leased through
// the layout's lease table.
func (inst *SqlStore) MintRun(ctx context.Context) (id uint64, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.idsErr != nil {
		return 0, inst.idsErr
	}
	return inst.st.Run.MintKey(ctx)
}

func (inst *SqlStore) CreateRun(ctx context.Context, r Run) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	_, dup, err := inst.getRunLocked(ctx, r.Id)
	if err != nil {
		return
	}
	if dup {
		return eb.Build().Uint64("run", r.Id).Errorf("duplicate run id")
	}
	if r.Version == 0 {
		r.Version = 1
	}
	r.Holder, r.HolderPid, r.Epoch = inst.Holder.Run, uint32(inst.Holder.Pid), 1
	if err = inst.insertRunLocked(ctx, r); err != nil {
		return
	}
	inst.epoch[r.Id] = 1
	return
}

func (inst *SqlStore) UpdateRun(ctx context.Context, r Run) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, err = inst.leaseLocked(ctx, r.Id); err != nil {
		return
	}
	was, found, err := inst.getRunLocked(ctx, r.Id)
	if err != nil {
		return
	}
	if !found {
		return eb.Build().Uint64("run", r.Id).Errorf("no such run")
	}
	if r.Version <= was.Version {
		return eb.Build().Uint64("run", r.Id).Uint32("version", r.Version).Uint32("stored", was.Version).Errorf("not a newer version of the run row")
	}
	return inst.writeRunLocked(ctx, r)
}

// insertRunLocked writes a new run row, the one row the run has.
func (inst *SqlStore) insertRunLocked(ctx context.Context, r Run) (err error) {
	if err = inst.st.Run.Begin(r.Id, versionTs(r)).AddRun(runRow(r)).Commit(); err != nil {
		inst.st.Run.DiscardPending()
		return eb.Build().Uint64("run", r.Id).Errorf("buffer run: %w", err)
	}
	if _, err = inst.st.Run.Flush(ctx); err != nil {
		inst.st.Run.DiscardPending()
		return eh.Errorf("flush run: %w", err)
	}
	inst.live[r.Id], inst.stored[r.Id] = r, r.Version
	return
}

// writeRunLocked rewrites a run row in place: one compare-and-set guarded
// on the version the table held when this store last saw it, then the
// read-back, which is the answer. The rewrite won when the row reads back
// as exactly the row written; a version match alone would not say so, since
// two writers moving one version to the next both read the next back.
// A lost rewrite drops the cached row, so the next read sees the record.
func (inst *SqlStore) writeRunLocked(ctx context.Context, r Run) (err error) {
	from, ok := inst.stored[r.Id]
	if !ok {
		return eb.Build().Uint64("run", r.Id).Errorf("the run row is rewritten before it was read")
	}
	epoch, held := inst.epoch[r.Id]
	if !held {
		return eb.Build().Uint64("run", r.Id).Errorf("the run row is rewritten without its lease")
	}
	guard := agentrunstore.RunGuardVersion(from) + " AND " + agentrunstore.RunGuardHolder(inst.Holder.Run) + " AND " + agentrunstore.RunGuardEpoch(epoch)
	r.Holder, r.HolderPid, r.Epoch = inst.Holder.Run, uint32(inst.Holder.Pid), epoch
	release := r.State != StateOpen
	if release {
		// A run that leaves the open state has no writer: the lease is
		// released with the rewrite, so any process may resume, erase or
		// delete it (ADR-0319 §SD7).
		r.Holder, r.HolderPid = "", 0
	}
	if err = inst.st.Run.UpdateIf(ctx, r.Id, guard, agentrunstore.RunSets(runRow(r))...); err != nil {
		return eb.Build().Uint64("run", r.Id).Errorf("rewrite run: %w", err)
	}
	got, found, err := inst.readRunLocked(ctx, r.Id)
	if err != nil {
		return
	}
	if !found || !sameRow(runRow(got), runRow(r)) {
		delete(inst.live, r.Id)
		delete(inst.stored, r.Id)
		delete(inst.epoch, r.Id)
		return eb.Build().Uint64("run", r.Id).Uint32("from", from).Uint32("version", r.Version).Errorf("the run row moved since this store read it: another writer holds the run (ADR-0308 §SD9)")
	}
	inst.live[r.Id], inst.stored[r.Id] = r, r.Version
	if release {
		delete(inst.epoch, r.Id)
	}
	return
}

// sameRow compares two run rows as the table stores them, instants by
// their nanoseconds.
func sameRow(a, b agentrunstore.Run) bool {
	norm := func(r agentrunstore.Run) agentrunstore.Run {
		r.Created, r.Closed, r.DeletedAt = nanos(r.Created), nanos(r.Closed), nanos(r.DeletedAt)
		return r
	}
	return norm(a) == norm(b)
}

func nanos(t time.Time) time.Time { return time.Unix(0, t.UnixNano()).UTC() }

// leaseLocked claims the run for an act (ADR-0319 §SD7): the row as the
// table holds it, held by this store, taken over first when its holder no
// longer runs. A run a live process holds is refused. adopted reports that
// the takeover found the dead holder's last batch in the record past the
// head and adopted what of it chains: the run moved under the act, which
// its writer must reopen to see.
func (inst *SqlStore) leaseLocked(ctx context.Context, id uint64) (adopted bool, err error) {
	r, found, err := inst.readRunLocked(ctx, id)
	if err != nil {
		return
	}
	if !found {
		return false, eb.Build().Uint64("run", id).Errorf("no such run")
	}
	inst.stored[id] = r.Version
	if !inst.dirty[id] {
		inst.live[id] = r
	}
	if r.Holder == inst.Holder.Run && int(r.HolderPid) == inst.Holder.Pid {
		inst.epoch[id] = r.Epoch
		return
	}
	delete(inst.epoch, id)
	if r.Holder != "" {
		alive, lerr := inst.Liveness.Alive(ctx, Holder{Run: r.Holder, Pid: int(r.HolderPid)})
		if lerr != nil {
			return false, eb.Build().Uint64("run", id).Errorf("holder liveness: %w", lerr)
		}
		if alive {
			return false, eb.Build().Uint64("run", id).Str("holder", r.Holder).Uint32("pid", r.HolderPid).Errorf("%w", ErrHeldElsewhere)
		}
	}
	guard := agentrunstore.RunGuardHolder(r.Holder) + " AND " + agentrunstore.RunGuardEpoch(r.Epoch)
	if err = inst.st.Run.UpdateIf(ctx, id, guard, agentrunstore.RunSetHolder(inst.Holder.Run), agentrunstore.RunSetHolderPid(uint32(inst.Holder.Pid)), agentrunstore.RunSetEpoch(r.Epoch+1)); err != nil {
		return false, eb.Build().Uint64("run", id).Errorf("take over the run: %w", err)
	}
	got, found, err := inst.readRunLocked(ctx, id)
	if err != nil {
		return
	}
	if !found || got.Holder != inst.Holder.Run || got.Epoch != r.Epoch+1 {
		delete(inst.live, id)
		return false, eb.Build().Uint64("run", id).Errorf("another process took the run over first: %w", ErrHeldElsewhere)
	}
	inst.live[id], inst.stored[id], inst.epoch[id] = got, got.Version, got.Epoch
	// The dead holder's last batch may be in the record past the head; the
	// run stays dirty until a repair has written what of it chains.
	inst.dirty[id] = true
	repaired, err := inst.repairLocked(ctx, got)
	if err != nil {
		return
	}
	adopted = repaired.LastOrdinal != got.LastOrdinal
	return
}

// versionTs is the run row's order instant: its creation plus its first
// version as nanoseconds. Later versions rewrite the row in place.
func versionTs(r Run) time.Time {
	return utc(r.Created).Add(time.Duration(r.Version))
}

func (inst *SqlStore) GetRun(ctx context.Context, id uint64) (r Run, found bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.getRunLocked(ctx, id)
}

// getRunLocked is the run row: from the cache while this store holds the
// run's lease or has a repair pending, since then no one else moves the
// row; from the table otherwise, since its holder may have.
func (inst *SqlStore) getRunLocked(ctx context.Context, id uint64) (r Run, found bool, err error) {
	_, held := inst.epoch[id]
	if r, found = inst.live[id]; found && (held || inst.dirty[id]) {
		if inst.dirty[id] {
			r, err = inst.repairLocked(ctx, r)
		}
		return
	}
	if r, found, err = inst.readRunLocked(ctx, id); err != nil {
		return
	}
	if found {
		inst.live[id], inst.stored[id] = r, r.Version
	}
	return
}

// readRunLocked reads one run row from the table, past the cache.
func (inst *SqlStore) readRunLocked(ctx context.Context, id uint64) (r Run, found bool, err error) {
	for ent, serr := range inst.st.Run.ScanLiveRun(ctx, recordstore.ScanOpts{ExtraPredicate: agentrunstore.RunColKey + " = " + strconv.FormatUint(id, 10)}) {
		if serr != nil {
			return r, false, eb.Build().Uint64("run", id).Errorf("read run: %w", serr)
		}
		if ent != nil && ent.Run.Has {
			r, found = runOf(ent.Run.Val), true
		}
	}
	return
}

func (inst *SqlStore) ListRuns(ctx context.Context, ownerApp string, limit int) (runs []Run, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if limit <= 0 {
		limit = DefaultListLimit
	}
	// Newest first, bounded in the statement: a run row's Order is its
	// creation instant (versionTs at version 1), and a rewrite in place
	// never moves it.
	opts := recordstore.ScanOpts{Descending: true, Limit: limit}
	if ownerApp != "" {
		opts.ExtraPredicate = agentrunstore.RunGuardOwnerApp(ownerApp)
	}
	for ent, serr := range inst.st.Run.ScanLiveRun(ctx, opts) {
		if serr != nil {
			return nil, eh.Errorf("list runs: %w", serr)
		}
		if ent == nil || !ent.Run.Has {
			continue
		}
		r := runOf(ent.Run.Val)
		if cached, ok := inst.live[r.Id]; ok && cached.Version >= r.Version {
			r = cached
		} else {
			inst.live[r.Id], inst.stored[r.Id] = r, r.Version
		}
		runs = append(runs, r)
	}
	return
}

func (inst *SqlStore) Append(ctx context.Context, entries []Entry) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	// Claim every run of the batch before anything is inserted.
	for _, id := range runsOf(entries) {
		adopted, lerr := inst.leaseLocked(ctx, id)
		if lerr != nil {
			return lerr
		}
		if adopted {
			return eb.Build().Uint64("run", id).Errorf("the run's last holder left entries this one adopted; reopen the run")
		}
	}
	if err = inst.appendLocked(ctx, entries); err != nil {
		return
	}
	// The run rows advance with their entries. The entries are the
	// record; a row write that fails after them leaves the run dirty, and
	// the row is rebuilt from the entry tail at the next read, so the next
	// append and a Reopen see the ordinals the record holds. The error is
	// returned: the writer then refuses later acts until Reopen, which
	// reads the repaired row.
	for _, id := range runsOf(entries) {
		r, found, gerr := inst.getRunLocked(ctx, id)
		if gerr != nil || !found {
			continue
		}
		for _, e := range entries {
			if e.Run == id {
				r = advanced(r, []Entry{e})
			}
		}
		if werr := inst.writeRunLocked(ctx, r); werr != nil {
			inst.dirty[id] = true
			if err == nil {
				err = eb.Build().Uint64("run", id).Errorf("the entries are recorded and the run row is not: %w", werr)
			}
		}
	}
	return
}

// repairLocked rebuilds a dirty run's row from the entries past the row's
// last ordinal, writes it, and clears the mark on success; the rebuilt
// row is returned either way.
func (inst *SqlStore) repairLocked(ctx context.Context, r Run) (repaired Run, err error) {
	tail, err := inst.entriesLocked(ctx, r.Id, r.LastOrdinal+1, 0)
	if err != nil {
		return r, err
	}
	tail = chained(r, tail)
	if len(tail) == 0 {
		delete(inst.dirty, r.Id)
		return r, nil
	}
	if _, held := inst.epoch[r.Id]; !held {
		// Not this store's to write: the tail is the holder's.
		return r, nil
	}
	repaired = advanced(r, tail)
	if werr := inst.writeRunLocked(ctx, repaired); werr != nil {
		inst.live[r.Id] = repaired
		return repaired, nil
	}
	delete(inst.dirty, r.Id)
	return
}

// chained is the prefix of tail that continues the run's chain from its
// head: contiguous ordinals, each entry's digest over its predecessor's.
// A row that does not chain — a stale writer's — ends the prefix and is
// never adopted (ADR-0319 §SD7).
func chained(r Run, tail []Entry) (prefix []Entry) {
	prevOrd, prevDigest := r.LastOrdinal, r.Head
	for _, e := range tail {
		form, ferr := e.CanonicalForm()
		if ferr != nil || e.Ordinal != prevOrd+1 || e.Prev != prevOrd || e.Digest != ChainDigest(prevDigest, form) {
			return
		}
		prefix = append(prefix, e)
		prevOrd, prevDigest = e.Ordinal, e.Digest
	}
	return
}

// runsOf is the distinct run ids of entries, in first-seen order.
func runsOf(entries []Entry) (ids []uint64) {
	for _, e := range entries {
		if !slices.Contains(ids, e.Run) {
			ids = append(ids, e.Run)
		}
	}
	return
}

// revisionOf is the revision an entry row is written at under a lease
// epoch: 2×epoch, and a tombstone one above, which every read keeps over
// the original (ADR-0308 §SD8). A later epoch's rows win every ordinal an
// earlier holder's in-flight batch also wrote (ADR-0319 §SD7).
func revisionOf(e Entry, epoch uint32) (rev agentrunstore.EntryEnvelope) {
	rev.Revision = 2 * epoch
	if e.Tombstoned() {
		rev.Revision++
	}
	return
}

func (inst *SqlStore) appendLocked(ctx context.Context, entries []Entry) (err error) {
	// An entry retried with its id keeps it: one ordinal, one id.
	for i := range entries {
		if entries[i].EntryId != 0 {
			continue
		}
		if inst.idsErr != nil {
			return inst.idsErr
		}
		if entries[i].EntryId, err = agentrunstore.Mint(ctx, inst.ids.Entries); err != nil {
			return eb.Build().Uint64("run", entries[i].Run).Uint32("ordinal", entries[i].Ordinal).Errorf("entry id: %w", err)
		}
	}
	for _, e := range entries {
		b := inst.st.Entry.Begin(e.Run, uint64(e.Ordinal), revisionOf(e, inst.epoch[e.Run]))
		if err = fillLedger(b, e); err == nil {
			err = b.Commit()
		} else {
			_ = b.Rollback()
		}
		if err != nil {
			inst.st.Entry.DiscardPending()
			return eb.Build().Uint64("run", e.Run).Uint32("ordinal", e.Ordinal).Errorf("buffer entry: %w", err)
		}
	}
	if _, err = inst.st.Entry.Flush(ctx); err != nil {
		inst.st.Entry.DiscardPending()
		return eh.Errorf("flush entries: %w", err)
	}
	return
}

func (inst *SqlStore) Entries(ctx context.Context, run uint64, from uint32) (entries []Entry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	heads, err := inst.headsLocked(ctx, []uint64{run})
	if err != nil {
		return
	}
	head, ok := heads[run]
	if !ok || head == 0 || from > head {
		return
	}
	return inst.entriesLocked(ctx, run, from, head)
}

// headsLocked is the last ordinal each run's row confirms: the bound every
// read stops at (ADR-0319 §SD7). A run this store holds reads its cached
// row, repaired when dirty; any other run's row is read from the table,
// since its holder may have moved it. A run with no row has no entry.
func (inst *SqlStore) headsLocked(ctx context.Context, runs []uint64) (heads map[uint64]uint32, err error) {
	heads = make(map[uint64]uint32, len(runs))
	var others []uint64
	for _, id := range runs {
		if _, held := inst.epoch[id]; held {
			r, found, gerr := inst.getRunLocked(ctx, id)
			if gerr != nil {
				return nil, gerr
			}
			if found {
				heads[id] = r.LastOrdinal
			}
			continue
		}
		others = append(others, id)
	}
	if len(others) == 0 && runs != nil {
		return
	}
	opts := recordstore.ScanOpts{}
	if runs != nil {
		lits := make([]string, 0, len(others))
		for _, id := range others {
			lits = append(lits, strconv.FormatUint(id, 10))
		}
		opts.ExtraPredicate = agentrunstore.RunColKey + " IN (" + strings.Join(lits, ", ") + ")"
	}
	for ent, serr := range inst.st.Run.ScanLiveRun(ctx, opts) {
		if serr != nil {
			return nil, eh.Errorf("read run heads: %w", serr)
		}
		if ent == nil || !ent.Run.Has {
			continue
		}
		if _, held := inst.epoch[ent.ID]; held {
			continue
		}
		heads[ent.ID] = ent.Run.Val.LastOrdinal
	}
	if runs == nil {
		for id := range inst.epoch {
			if r, found, gerr := inst.getRunLocked(ctx, id); gerr == nil && found {
				heads[id] = r.LastOrdinal
			}
		}
	}
	return
}

// bounded drops the entries past their run's head.
func bounded(entries []Entry, heads map[uint64]uint32) (kept []Entry) {
	kept = entries[:0]
	for _, e := range entries {
		if head, ok := heads[e.Run]; ok && e.Ordinal <= head {
			kept = append(kept, e)
		}
	}
	return
}

// entriesLocked reads a run's entries from an ordinal on, through upTo
// when it is not zero; the bounds are in the statement, over the order
// column the ordinal is stored as, so a read of the tail costs the tail.
func (inst *SqlStore) entriesLocked(ctx context.Context, run uint64, from uint32, upTo uint32) (entries []Entry, err error) {
	predicate := agentrunstore.EntryColKey + " = " + strconv.FormatUint(run, 10)
	if from > 0 {
		predicate += " AND " + agentrunstore.EntryColOrder + " >= " + strconv.FormatUint(uint64(from), 10)
	}
	if upTo > 0 {
		predicate += " AND " + agentrunstore.EntryColOrder + " <= " + strconv.FormatUint(uint64(upTo), 10)
	}
	entries, err = inst.scanEntriesLocked(ctx, predicate)
	if err != nil {
		return nil, eb.Build().Uint64("run", run).Errorf("read entries: %w", err)
	}
	return
}

func (inst *SqlStore) EntriesOf(ctx context.Context, runs []uint64) (entries []Entry, err error) {
	if len(runs) == 0 {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ids := make([]string, 0, len(runs))
	for _, id := range runs {
		ids = append(ids, strconv.FormatUint(id, 10))
	}
	if entries, err = inst.scanEntriesLocked(ctx, agentrunstore.EntryColKey+" IN ("+strings.Join(ids, ", ")+")"); err != nil {
		err = eh.Errorf("read entries: %w", err)
		return
	}
	heads, err := inst.headsLocked(ctx, runs)
	if err != nil {
		return nil, err
	}
	entries = bounded(entries, heads)
	return
}

// scanEntriesLocked reads the entry rows a predicate selects, in (run,
// ordinal) order. The generated scan keeps one row per ordinal, its
// highest revision, so a tombstone hides the entry it blanks (ADR-0308
// §SD8); the predicate must select whole ordinals, as a run and an
// ordinal bound do.
func (inst *SqlStore) scanEntriesLocked(ctx context.Context, predicate string) (entries []Entry, err error) {
	for ent, serr := range inst.st.Entry.ScanEntry(ctx, recordstore.ScanOpts{ExtraPredicate: predicate}) {
		if serr != nil {
			return nil, serr
		}
		if ent == nil || !ent.Entry.Has {
			continue
		}
		entries = append(entries, entryOfLedger(ent))
	}
	// The scan orders by (ordinal, run); a several-run read wants runs whole.
	slices.SortStableFunc(entries, func(a, b Entry) int {
		if c := cmp.Compare(a.Run, b.Run); c != 0 {
			return c
		}
		return int(a.Ordinal) - int(b.Ordinal)
	})
	return
}

func (inst *SqlStore) AllEntries(ctx context.Context) (entries []Entry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if entries, err = inst.scanEntriesLocked(ctx, ""); err != nil {
		err = eh.Errorf("read entries: %w", err)
		return
	}
	heads, err := inst.headsLocked(ctx, nil)
	if err != nil {
		return nil, err
	}
	entries = bounded(entries, heads)
	return
}

// Tombstone appends every entry of the run at its next revision with Body
// and Calls empty, which the reads keep over the original at the same
// ordinal, so the chain's digests and the metadata stay and no row is
// removed first (ADR-0308 §SD8). One flush, so a failure leaves the run as
// it was. The originals, their bodies still in the parts, are then deleted
// best-effort by their revision: the blanked revisions are the record
// either way, and a deletion that fails is logged and retried by nothing
// until the next tombstone of the run. Head entries keep their body, a
// digest the anchor law reads (ADR-0314 §SD5), not content.
func (inst *SqlStore) Tombstone(ctx context.Context, run uint64) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, err = inst.leaseLocked(ctx, run); err != nil {
		return
	}
	r, _, err := inst.getRunLocked(ctx, run)
	if err != nil || r.LastOrdinal == 0 {
		return
	}
	entries, err := inst.entriesLocked(ctx, run, 0, r.LastOrdinal)
	if err != nil {
		return
	}
	blanked := make([]Entry, 0, len(entries))
	var ordinals []string
	for _, e := range entries {
		if !e.HasContent() || e.Kind == KindHead {
			continue
		}
		ordinals = append(ordinals, strconv.FormatUint(uint64(e.Ordinal), 10))
		blanked = append(blanked, e.Erased())
	}
	if len(blanked) == 0 {
		return
	}
	if err = inst.appendLocked(ctx, blanked); err != nil {
		return
	}
	where := agentrunstore.EntryColKey + " = " + strconv.FormatUint(run, 10) +
		" AND " + agentrunstore.EntryColOrder + " IN (" + strings.Join(ordinals, ", ") + ")" +
		" AND " + agentrunstore.EntryColRevision + " % 2 = 0"
	if derr := inst.exec.Exec(ctx, "DELETE FROM "+inst.layout.EntryTable()+" WHERE "+where); derr != nil {
		inst.Log.Warn().Err(derr).Uint64("run", run).Msg("the tombstoned run's original rows were not deleted; the blanked revisions are the record")
	}
	return
}

func runRow(r Run) agentrunstore.Run {
	return agentrunstore.Run{ID: r.Id, Cell: r.Cell, Conversation: r.Conversation, OwnerApp: r.OwnerApp, Coordinator: r.Coordinator,
		State: r.State.String(), Fingerprint: r.Fingerprint, Build: r.Build, Profile: r.Profile, Settings: r.Settings,
		LabelLevel: uint32(r.Label.Level), LabelTier: uint32(r.Label.Tier), LabelExposure: MarksString(r.Label.Exposure), LabelOrigin: MarksString(r.Label.Origin),
		Created: utc(r.Created), LastOrdinal: r.LastOrdinal, Closed: utc(r.Closed), Digest: r.Digest, Head: r.Head, Version: r.Version,
		DeletedBy: r.DeletedBy, DeletedAt: utc(r.DeletedAt), Holder: r.Holder, HolderPid: r.HolderPid, Epoch: r.Epoch}
}

func runOf(row agentrunstore.Run) (r Run) {
	st, _ := ParseState(row.State)
	return Run{Id: row.ID, Cell: row.Cell, Conversation: row.Conversation, OwnerApp: row.OwnerApp, Coordinator: row.Coordinator,
		State: st, Fingerprint: row.Fingerprint, Build: row.Build, Profile: row.Profile, Settings: row.Settings,
		Label:   Label{Level: LevelE(row.LabelLevel), Tier: TierE(row.LabelTier), Exposure: ParseMarks(row.LabelExposure), Origin: ParseMarks(row.LabelOrigin)},
		Created: zeroed(row.Created), LastOrdinal: row.LastOrdinal, Closed: zeroed(row.Closed), Digest: row.Digest, Head: row.Head, Version: row.Version,
		DeletedBy: row.DeletedBy, DeletedAt: zeroed(row.DeletedAt), Holder: row.Holder, HolderPid: row.HolderPid, Epoch: row.Epoch}
}

// utc is t in UTC, the epoch for a zero time, so a DateTime64 column holds
// one value for "unset".
func utc(t time.Time) time.Time {
	if t.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return t.UTC()
}

// zeroed is the inverse of utc.
func zeroed(t time.Time) time.Time {
	if t.IsZero() || t.Unix() == 0 {
		return time.Time{}
	}
	return t.UTC()
}
