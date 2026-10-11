package agentrun

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
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
// deferred: ADR-0309 §SD4 asks for async_insert = 0 on journal inserts;
// the executor seam (recordstore.ExecutorI.InsertArrow) carries no
// per-insert settings, so the setting is the executor's session's to
// hold until the seam gains one.
type SqlStore struct {
	mu     sync.Mutex
	exec   recordstore.ExecutorI
	layout agentrunstore.Layout
	st     agentrunstore.Stores
	// Log takes what the store does best-effort and could not: the
	// deletion of a tombstoned run's original rows.
	Log zerolog.Logger
	// live are the run rows as last written or read, by id.
	live map[string]Run
	// stored is the version the table holds for a run, as this store last
	// wrote or read it: the guard of the next compare-and-set. It differs
	// from live's when a repair rebuilt a row it could not write.
	stored map[string]uint32
	// dirty are the runs whose entries went to the record and whose row
	// did not: the row is rebuilt from the entry tail at the next read.
	dirty map[string]bool
}

var _ StoreI = (*SqlStore)(nil)
var _ AllEntriesI = (*SqlStore)(nil)
var _ EntriesOfI = (*SqlStore)(nil)

// NewSqlStore opens the stores over exec in layout; the caller provisions
// first (agentrunstore.ProvisionIn) and closes afterwards.
func NewSqlStore(exec recordstore.ExecutorI, layout agentrunstore.Layout) (inst *SqlStore) {
	return &SqlStore{exec: exec, layout: layout, st: agentrunstore.NewStores(exec, layout), live: make(map[string]Run), stored: make(map[string]uint32), dirty: make(map[string]bool), Log: zerolog.Nop()}
}

// Close releases the generated stores.
func (inst *SqlStore) Close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.st.Close()
}

func (inst *SqlStore) CreateRun(ctx context.Context, r Run) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	_, dup, err := inst.getRunLocked(ctx, r.Id)
	if err != nil {
		return
	}
	if dup {
		return eb.Build().Str("run", r.Id).Errorf("duplicate run id")
	}
	if r.Version == 0 {
		r.Version = 1
	}
	return inst.insertRunLocked(ctx, r)
}

func (inst *SqlStore) UpdateRun(ctx context.Context, r Run) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	was, found, err := inst.getRunLocked(ctx, r.Id)
	if err != nil {
		return
	}
	if !found {
		return eb.Build().Str("run", r.Id).Errorf("no such run")
	}
	if r.Version <= was.Version {
		return eb.Build().Str("run", r.Id).Uint32("version", r.Version).Uint32("stored", was.Version).Errorf("not a newer version of the run row")
	}
	return inst.writeRunLocked(ctx, r)
}

// insertRunLocked writes a new run row, the one row the run has.
func (inst *SqlStore) insertRunLocked(ctx context.Context, r Run) (err error) {
	if err = inst.st.Run.Begin(r.Id, versionTs(r)).AddRun(runRow(r)).Commit(); err != nil {
		inst.st.Run.DiscardPending()
		return eb.Build().Str("run", r.Id).Errorf("buffer run: %w", err)
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
		return eb.Build().Str("run", r.Id).Errorf("the run row is rewritten before it was read")
	}
	if err = inst.st.Run.UpdateIf(ctx, r.Id, agentrunstore.RunGuardVersion(from), agentrunstore.RunSets(runRow(r))...); err != nil {
		return eb.Build().Str("run", r.Id).Errorf("rewrite run: %w", err)
	}
	got, found, err := inst.readRunLocked(ctx, r.Id)
	if err != nil {
		return
	}
	if !found || !sameRow(runRow(got), runRow(r)) {
		delete(inst.live, r.Id)
		delete(inst.stored, r.Id)
		return eb.Build().Str("run", r.Id).Uint32("from", from).Uint32("version", r.Version).Errorf("the run row moved since this store read it: another writer holds the run (ADR-0308 §SD9)")
	}
	inst.live[r.Id], inst.stored[r.Id] = r, r.Version
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

// versionTs is the run row's order instant: its creation plus its first
// version as nanoseconds. Later versions rewrite the row in place.
func versionTs(r Run) time.Time {
	return utc(r.Created).Add(time.Duration(r.Version))
}

func (inst *SqlStore) GetRun(ctx context.Context, id string) (r Run, found bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.getRunLocked(ctx, id)
}

func (inst *SqlStore) getRunLocked(ctx context.Context, id string) (r Run, found bool, err error) {
	if r, found = inst.live[id]; found {
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
func (inst *SqlStore) readRunLocked(ctx context.Context, id string) (r Run, found bool, err error) {
	for ent, serr := range inst.st.Run.ScanLiveRun(ctx, recordstore.ScanOpts{ExtraPredicate: agentrunstore.RunColKey + " = " + marshalling.EscapeString(id)}) {
		if serr != nil {
			return r, false, eb.Build().Str("run", id).Errorf("read run: %w", serr)
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
	for ent, serr := range inst.st.Run.ScanLiveRun(ctx, recordstore.ScanOpts{}) {
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
		if ownerApp == "" || r.OwnerApp == ownerApp {
			runs = append(runs, r)
		}
	}
	slices.SortFunc(runs, func(a, b Run) int { return b.Created.Compare(a.Created) })
	if len(runs) > limit {
		runs = runs[:limit]
	}
	return
}

func (inst *SqlStore) Append(ctx context.Context, entries []Entry) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
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
				err = eb.Build().Str("run", id).Errorf("the entries are recorded and the run row is not: %w", werr)
			}
		}
	}
	return
}

// repairLocked rebuilds a dirty run's row from the entries past the row's
// last ordinal, writes it, and clears the mark on success; the rebuilt
// row is returned either way.
func (inst *SqlStore) repairLocked(ctx context.Context, r Run) (repaired Run, err error) {
	tail, err := inst.entriesLocked(ctx, r.Id, r.LastOrdinal+1)
	if err != nil {
		return r, err
	}
	repaired = advanced(r, tail)
	if werr := inst.writeRunLocked(ctx, repaired); werr != nil {
		inst.live[r.Id] = repaired
		return repaired, nil
	}
	delete(inst.dirty, r.Id)
	return
}

// runsOf is the distinct run ids of entries, in first-seen order.
func runsOf(entries []Entry) (ids []string) {
	for _, e := range entries {
		if !slices.Contains(ids, e.Run) {
			ids = append(ids, e.Run)
		}
	}
	return
}

// revisionOf is the revision an entry row is written at: a tombstone is
// the entry at its next revision, which every read keeps over the original
// (ADR-0308 §SD8, the store's Revision role).
func revisionOf(e Entry) (rev agentrunstore.EntryEnvelope) {
	if e.Tombstoned() {
		rev.Revision = 1
	}
	return
}

func (inst *SqlStore) appendLocked(ctx context.Context, entries []Entry) (err error) {
	for _, e := range entries {
		if err = inst.st.Entry.Begin(e.Run, recordstore.SeqTs(uint64(e.Ordinal)), revisionOf(e)).AddEntry(entryRow(e)).Commit(); err != nil {
			inst.st.Entry.DiscardPending()
			return eb.Build().Str("run", e.Run).Uint32("ordinal", e.Ordinal).Errorf("buffer entry: %w", err)
		}
	}
	if _, err = inst.st.Entry.Flush(ctx); err != nil {
		inst.st.Entry.DiscardPending()
		return eh.Errorf("flush entries: %w", err)
	}
	return
}

func (inst *SqlStore) Entries(ctx context.Context, run string, from uint32) (entries []Entry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.entriesLocked(ctx, run, from)
}

// entriesLocked reads a run's entries from an ordinal on; the bound is in
// the statement, over the order column the ordinal is stored as, so a
// read of the tail costs the tail.
func (inst *SqlStore) entriesLocked(ctx context.Context, run string, from uint32) (entries []Entry, err error) {
	predicate := agentrunstore.EntryColKey + " = " + marshalling.EscapeString(run)
	if from > 0 {
		predicate += " AND " + agentrunstore.EntryColOrder + " >= fromUnixTimestamp64Nano(" + strconv.FormatInt(recordstore.SeqTs(uint64(from)).UnixNano(), 10) + ")"
	}
	entries, err = inst.scanEntriesLocked(ctx, predicate)
	if err != nil {
		return nil, eb.Build().Str("run", run).Errorf("read entries: %w", err)
	}
	return
}

func (inst *SqlStore) EntriesOf(ctx context.Context, runs []string) (entries []Entry, err error) {
	if len(runs) == 0 {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ids := make([]string, 0, len(runs))
	for _, id := range runs {
		ids = append(ids, marshalling.EscapeString(id))
	}
	if entries, err = inst.scanEntriesLocked(ctx, agentrunstore.EntryColKey+" IN ("+strings.Join(ids, ", ")+")"); err != nil {
		err = eh.Errorf("read entries: %w", err)
	}
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
		entries = append(entries, entryOf(ent.Entry.Val, ent.ID))
	}
	// The scan orders by (ordinal, run); a several-run read wants runs whole.
	slices.SortStableFunc(entries, func(a, b Entry) int {
		if c := compareString(a.Run, b.Run); c != 0 {
			return c
		}
		return int(a.Ordinal) - int(b.Ordinal)
	})
	return
}

func compareString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (inst *SqlStore) AllEntries(ctx context.Context) (entries []Entry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if entries, err = inst.scanEntriesLocked(ctx, ""); err != nil {
		err = eh.Errorf("read entries: %w", err)
	}
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
func (inst *SqlStore) Tombstone(ctx context.Context, run string) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	entries, err := inst.entriesLocked(ctx, run, 0)
	if err != nil {
		return
	}
	blanked := make([]Entry, 0, len(entries))
	var ordinals []string
	for _, e := range entries {
		if (e.Body == "" && e.Calls == "") || e.Kind == KindHead {
			continue
		}
		ordinals = append(ordinals, "fromUnixTimestamp64Nano("+strconv.FormatInt(recordstore.SeqTs(uint64(e.Ordinal)).UnixNano(), 10)+")")
		e.Body, e.Calls = "", ""
		blanked = append(blanked, e)
	}
	if len(blanked) == 0 {
		return
	}
	if err = inst.appendLocked(ctx, blanked); err != nil {
		return
	}
	where := agentrunstore.EntryColKey + " = " + marshalling.EscapeString(run) +
		" AND " + agentrunstore.EntryColOrder + " IN (" + strings.Join(ordinals, ", ") + ")" +
		" AND " + agentrunstore.EntryColRevision + " = 0"
	if derr := inst.exec.Exec(ctx, "DELETE FROM "+inst.layout.EntryTable()+" WHERE "+where); derr != nil {
		inst.Log.Warn().Err(derr).Str("run", run).Msg("the tombstoned run's original rows were not deleted; the blanked revisions are the record")
	}
	return
}

func runRow(r Run) agentrunstore.Run {
	return agentrunstore.Run{ID: r.Id, Cell: r.Cell, Conversation: r.Conversation, OwnerApp: r.OwnerApp, Coordinator: r.Coordinator,
		State: r.State.String(), Fingerprint: r.Fingerprint, Build: r.Build, Profile: r.Profile, Settings: r.Settings,
		LabelLevel: uint32(r.Label.Level), LabelTier: uint32(r.Label.Tier), LabelExposure: MarksString(r.Label.Exposure), LabelOrigin: MarksString(r.Label.Origin),
		Created: utc(r.Created), LastOrdinal: r.LastOrdinal, Closed: utc(r.Closed), Digest: r.Digest, Head: r.Head, Version: r.Version,
		DeletedBy: r.DeletedBy, DeletedAt: utc(r.DeletedAt)}
}

func runOf(row agentrunstore.Run) (r Run) {
	st, _ := ParseState(row.State)
	return Run{Id: row.ID, Cell: row.Cell, Conversation: row.Conversation, OwnerApp: row.OwnerApp, Coordinator: row.Coordinator,
		State: st, Fingerprint: row.Fingerprint, Build: row.Build, Profile: row.Profile, Settings: row.Settings,
		Label:   Label{Level: LevelE(row.LabelLevel), Tier: TierE(row.LabelTier), Exposure: ParseMarks(row.LabelExposure), Origin: ParseMarks(row.LabelOrigin)},
		Created: zeroed(row.Created), LastOrdinal: row.LastOrdinal, Closed: zeroed(row.Closed), Digest: row.Digest, Head: row.Head, Version: row.Version,
		DeletedBy: row.DeletedBy, DeletedAt: zeroed(row.DeletedAt)}
}

func entryRow(e Entry) agentrunstore.Entry {
	return agentrunstore.Entry{ID: e.Run, Ordinal: e.Ordinal, At: utc(e.At), Kind: e.Kind.String(), Key: e.Key, Role: e.Role, Body: e.Body, Calls: e.Calls,
		BodyDigest: e.BodyDigest, CallsDigest: e.CallsDigest,
		Name: e.Name, Book: e.Book, Form: e.Form, Target: e.Target, From: e.From, Effect: e.Effect, Turn: e.Turn,
		LabelLevel: uint32(e.Label.Level), LabelTier: uint32(e.Label.Tier), LabelExposure: MarksString(e.Label.Exposure), LabelOrigin: MarksString(e.Label.Origin),
		WriterRun: e.WriterRun, WriterTurn: e.WriterTurn, WriterOrdinal: e.WriterOrdinal, Prev: e.Prev, Digest: e.Digest, KeyId: e.KeyId, Parent: e.Parent, Attachments: e.Attachments}
}

func entryOf(row agentrunstore.Entry, run string) (e Entry) {
	k, _ := ParseKind(row.Kind)
	return Entry{Run: run, Ordinal: row.Ordinal, At: zeroed(row.At), Kind: k, Key: row.Key, Role: row.Role, Body: row.Body, Calls: row.Calls,
		BodyDigest: row.BodyDigest, CallsDigest: row.CallsDigest,
		Name: row.Name, Book: row.Book, Form: row.Form, Target: row.Target, From: row.From, Effect: row.Effect, Turn: row.Turn,
		Label:     Label{Level: LevelE(row.LabelLevel), Tier: TierE(row.LabelTier), Exposure: ParseMarks(row.LabelExposure), Origin: ParseMarks(row.LabelOrigin)},
		WriterRun: row.WriterRun, WriterTurn: row.WriterTurn, WriterOrdinal: row.WriterOrdinal, Prev: row.Prev, Digest: row.Digest, KeyId: row.KeyId, Parent: row.Parent, Attachments: row.Attachments}
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
