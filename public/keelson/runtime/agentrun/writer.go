package agentrun

import (
	"context"
	"slices"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Writer is a run's single writer (ADR-0308 §SD9): it assigns ordinals,
// chains each entry to the one before, appends through the store and
// flushes before the act the entry records leaves (§SD2), and keeps the
// run's entries in memory — the prefix the projection reads and the cache
// law L4 checks. One Writer per run; the coordinator holds it.
type Writer struct {
	store StoreI
	// writeMu serialises the writes: appends, updates, deletion. It is
	// held across the store call, a bus round trip, so the state lock is
	// not: a reader on the UI thread never waits behind an in-flight
	// write.
	writeMu sync.Mutex
	// mu guards run, entries and failed, briefly.
	mu  sync.Mutex
	run Run
	// entries are every entry of the run in ordinal order; last is the
	// chain's head. Copy on write: a slice handed out is never changed
	// behind the reader.
	entries []Entry
	// failed is set when an append failed after it may have reached the
	// record: the journal does not know of an act, and every later act is
	// refused until the run is reopened from the record.
	failed error
}

// Open creates the run row and returns its writer. The row the writer
// holds is the row as stored: the service sets the owner, and the seed
// digest follows it.
func Open(ctx context.Context, store StoreI, r Run) (w *Writer, err error) {
	if r.Id == "" {
		err = eh.Errorf("a run needs an id")
		return
	}
	if r.Created.IsZero() {
		r.Created = Now()
	}
	if r.State == StateUnspecified {
		r.State = StateOpen
	}
	r.Digest = RunDigestSeed(r)
	r.Head = r.Digest
	r.Version = 1
	if err = store.CreateRun(ctx, r); err != nil {
		err = eb.Build().Str("run", r.Id).Errorf("create run: %w", err)
		return
	}
	stored, found, err := store.GetRun(ctx, r.Id)
	if err != nil {
		err = eb.Build().Str("run", r.Id).Errorf("read the run back: %w", err)
		return
	}
	if !found {
		err = eb.Build().Str("run", r.Id).Errorf("the run was created and is not found")
		return
	}
	w = &Writer{store: store, run: stored}
	return
}

// Reopen reads a run and its entries back from the store and verifies the
// chain from the run row (law L31); the writer continues after the last
// entry. A deleted run reopens: its chain verifies over the digests its
// tombstone kept, and the writer refuses to append to it.
func Reopen(ctx context.Context, store StoreI, id string) (w *Writer, err error) {
	r, found, err := store.GetRun(ctx, id)
	if err != nil {
		err = eb.Build().Str("run", id).Errorf("read run: %w", err)
		return
	}
	if !found {
		err = eb.Build().Str("run", id).Errorf("no such run")
		return
	}
	entries, err := store.Entries(ctx, id, 0)
	if err != nil {
		err = eb.Build().Str("run", id).Errorf("read entries: %w", err)
		return
	}
	broken, err := VerifyRun(r, entries)
	if err != nil {
		err = eb.Build().Str("run", id).Errorf("verify: %w", err)
		return
	}
	if broken != 0 {
		err = eb.Build().Str("run", id).Uint32("ordinal", broken).Errorf("the chain does not verify")
		return
	}
	w = &Writer{store: store, run: r, entries: entries}
	w.run.LastOrdinal, w.run.Head = w.headLocked()
	// A deletion the process did not finish, the deleted entry written
	// and the tombstone or the row not: finish it, so the record never
	// holds a deleted entry with the bodies still behind it.
	if last, ok := w.lastLocked(); ok && last.Kind == KindDeleted && r.State != StateDeleted {
		if err = w.finishDelete(ctx, last.Name); err != nil {
			err = eb.Build().Str("run", id).Errorf("finish the deletion: %w", err)
			return nil, err
		}
	}
	return
}

// lastLocked is the chain's last entry, when there is one.
func (inst *Writer) lastLocked() (e Entry, ok bool) {
	if len(inst.entries) == 0 {
		return
	}
	return inst.entries[len(inst.entries)-1], true
}

// Run is the run row as the writer last wrote it.
func (inst *Writer) Run() (r Run) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.run
}

// Entries are the run's entries in order, a copy.
func (inst *Writer) Entries() (entries []Entry) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return slices.Clone(inst.entries)
}

// Len is the run's last ordinal.
func (inst *Writer) Len() (n uint32) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return uint32(len(inst.entries))
}

// Head is the chain's head: the last ordinal and its digest.
func (inst *Writer) Head() (ordinal uint32, digest string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.headLocked()
}

func (inst *Writer) headLocked() (ordinal uint32, digest string) {
	if len(inst.entries) == 0 {
		return 0, inst.run.Digest
	}
	last := inst.entries[len(inst.entries)-1]
	return last.Ordinal, last.Digest
}

// Append gives the entries their ordinals, times, digests and chain
// digests, writes them through the store and flushes. It returns the
// entries as written. A failed write refuses the act: the error is
// returned and the writer refuses every later append until Reopen. A
// context already done before anything was sent refuses the act alone;
// nothing reached the record, so later appends stand.
func (inst *Writer) Append(ctx context.Context, entries ...Entry) (written []Entry, err error) {
	inst.writeMu.Lock()
	defer inst.writeMu.Unlock()
	return inst.append(ctx, false, entries...)
}

// append is Append under the write lock; closing lets a closed or
// stranded run take the entry, which only the tombstone does.
func (inst *Writer) append(ctx context.Context, closing bool, entries ...Entry) (written []Entry, err error) {
	inst.mu.Lock()
	run := inst.run
	last, _ := inst.lastLocked()
	prevOrd, prevDigest := inst.headLocked()
	failed := inst.failed
	inst.mu.Unlock()
	if failed != nil {
		err = eb.Build().Str("run", run.Id).Errorf("the journal refused an earlier write: %w", failed)
		return
	}
	switch {
	case run.State == StateDeleted || last.Kind == KindDeleted:
		err = eb.Build().Str("run", run.Id).Errorf("the run is deleted")
		return
	case run.State != StateOpen && !closing:
		err = eb.Build().Str("run", run.Id).Str("state", run.State.String()).Errorf("the run is not open")
		return
	}
	written = make([]Entry, 0, len(entries))
	for _, e := range entries {
		if err = checkEntry(e, prevOrd+1); err != nil {
			return nil, err
		}
		e.Run, e.Ordinal, e.At = run.Id, prevOrd+1, Now()
		if e, err = e.chained(prevOrd, prevDigest); err != nil {
			return nil, err
		}
		prevOrd, prevDigest = e.Ordinal, e.Digest
		written = append(written, e)
	}
	if err = ctx.Err(); err != nil {
		return nil, eb.Build().Str("run", run.Id).Errorf("append: %w", err)
	}
	err = inst.store.Append(ctx, written)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err != nil {
		inst.failed = err
		err = eb.Build().Str("run", run.Id).Errorf("append: %w", err)
		return nil, err
	}
	inst.entries = append(slices.Clip(inst.entries), written...)
	inst.run = advanced(inst.run, written)
	return
}

// checkEntry is what every entry must satisfy before it is chained at
// ordinal: a known kind, a compaction or edit aimed at an earlier entry,
// and marks the columns can hold (MarkValid).
func checkEntry(e Entry, ordinal uint32) (err error) {
	if !e.Kind.IsValid() {
		return eb.Build().Uint8("kind", uint8(e.Kind)).Errorf("no such entry kind")
	}
	if (e.Kind == KindCompact || e.Kind == KindEdit) && (e.Target == 0 || e.Target >= ordinal) {
		return eb.Build().Str("kind", e.Kind.String()).Uint32("target", e.Target).Uint32("ordinal", ordinal).Errorf("a compaction or edit aims at an earlier entry")
	}
	for _, m := range append(slices.Clone(e.Label.Exposure), e.Label.Origin...) {
		if m != "" && !MarkValid(m) {
			return eb.Build().Str("mark", m).Errorf("a mark cannot carry a comma")
		}
	}
	return
}

// advanced is the run row after entries were appended: the last ordinal,
// the chain head, and the label joined with theirs (ADR-0311 §SD2). The
// stores apply it in Append; the writer mirrors it.
func advanced(r Run, entries []Entry) Run {
	for _, e := range entries {
		r.LastOrdinal, r.Head = e.Ordinal, e.Digest
		r.Label = r.Label.Join(e.Label)
		r.Version++
	}
	return r
}

// Update rewrites the run row's mutable fields: state, fingerprint,
// profile, settings, closed. The label can only rise. The row is written
// at the next version over the one the writer holds: a store refuses it
// when another writer moved the row since.
func (inst *Writer) Update(ctx context.Context, change func(r *Run)) (err error) {
	inst.writeMu.Lock()
	defer inst.writeMu.Unlock()
	return inst.update(ctx, change)
}

// update is Update under the write lock.
func (inst *Writer) update(ctx context.Context, change func(r *Run)) (err error) {
	inst.mu.Lock()
	was := inst.run
	inst.mu.Unlock()
	r := was
	change(&r)
	r.Id, r.Created, r.Digest, r.Head, r.LastOrdinal = was.Id, was.Created, was.Digest, was.Head, was.LastOrdinal
	r.Label = was.Label.Join(r.Label)
	r.Version = was.Version + 1
	if err = inst.store.UpdateRun(ctx, r); err != nil {
		err = eb.Build().Str("run", was.Id).Errorf("update run: %w", err)
		return
	}
	inst.mu.Lock()
	inst.run = r
	inst.mu.Unlock()
	return
}

// Raise joins the run's label with l: the run's confidentiality floats
// up as it reads and its integrity down (ADR-0311 §SD2). The stores raise
// the label with every entry appended; Raise is for what is read without
// an entry.
func (inst *Writer) Raise(ctx context.Context, l Label) (err error) {
	return inst.Update(ctx, func(r *Run) { r.Label = r.Label.Join(l) })
}

// Close ends the run in state: closed, or stranded. A close the store
// refuses because another writer moved the row since (a resume of the
// run in another window, say) is not an error of this writer: the row as
// stored is adopted and the close stands down, so a stale writer's late
// close never takes a run another writer reopened.
func (inst *Writer) Close(ctx context.Context, state StateE) (err error) {
	inst.writeMu.Lock()
	defer inst.writeMu.Unlock()
	if err = inst.update(ctx, func(r *Run) { r.State, r.Closed = state, Now() }); err == nil {
		return
	}
	stored, found, gerr := inst.store.GetRun(ctx, inst.Run().Id)
	if gerr != nil || !found {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if stored.Version > inst.run.Version {
		inst.run = stored
		return nil
	}
	return
}

// Resume takes a closed run back to open, so it continues (ADR-0308
// §SD4); a stranded or deleted run does not resume.
func (inst *Writer) Resume(ctx context.Context) (err error) {
	inst.writeMu.Lock()
	defer inst.writeMu.Unlock()
	switch inst.Run().State {
	case StateOpen:
		return
	case StateClosed:
	default:
		return eb.Build().Str("run", inst.Run().Id).Str("state", inst.Run().State.String()).Errorf("the run does not resume")
	}
	return inst.update(ctx, func(r *Run) { r.State, r.Closed = StateOpen, zeroTime })
}

// Delete tombstones the run, whatever its state short of deleted
// (ADR-0308 §SD8): a deleted entry naming who in its Name, outside the
// bodies; then every body emptied through the store, the digests the
// chain commits to staying and the head entries' bodies with them, so
// the anchors still verify (law L32); then the row marked deleted. A failure after
// the deleted entry leaves the writer failed: the record holds a deletion
// it has not finished, which Reopen completes.
func (inst *Writer) Delete(ctx context.Context, by string) (err error) {
	inst.writeMu.Lock()
	defer inst.writeMu.Unlock()
	if _, err = inst.append(ctx, true, Entry{Kind: KindDeleted, Name: by, Label: Label{Level: LevelPerson}}); err != nil {
		return
	}
	if err = inst.finishDelete(ctx, by); err != nil {
		inst.mu.Lock()
		inst.failed = err
		inst.mu.Unlock()
	}
	return
}

// finishDelete is the deletion after its entry, under the write lock:
// the tombstone and the row.
func (inst *Writer) finishDelete(ctx context.Context, by string) (err error) {
	id := inst.Run().Id
	if err = inst.store.Tombstone(ctx, id); err != nil {
		return eb.Build().Str("run", id).Errorf("tombstone: %w", err)
	}
	inst.mu.Lock()
	emptied := slices.Clone(inst.entries)
	for i := range emptied {
		if emptied[i].Kind == KindHead {
			continue
		}
		emptied[i].Body, emptied[i].Calls = "", ""
	}
	inst.entries = emptied
	inst.mu.Unlock()
	if err = inst.update(ctx, func(r *Run) { r.State, r.DeletedBy, r.DeletedAt = StateDeleted, by, Now() }); err != nil {
		return eb.Build().Str("run", id).Errorf("mark deleted: %w", err)
	}
	return
}
