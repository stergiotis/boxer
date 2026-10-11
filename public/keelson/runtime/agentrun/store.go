package agentrun

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// StoreI is the seam between the writer and the record (ADR-0308 §SD1):
// a run row created and updated, entries appended and flushed before the
// act leaves, and the reads the service, the fold and the introspection
// tables need. Safe for concurrent use.
type StoreI interface {
	// MintRun draws a run id from the store's run stream (ADR-0325 §SD5):
	// the id a new run row is created under.
	MintRun(ctx context.Context) (id uint64, err error)
	// CreateRun writes a new run row; a duplicate id is refused.
	CreateRun(ctx context.Context, r Run) (err error)
	// UpdateRun rewrites the run row: state, label, fingerprint, closed,
	// deletion. The row's version orders the writes.
	UpdateRun(ctx context.Context, r Run) (err error)
	// GetRun reads one run row.
	GetRun(ctx context.Context, id uint64) (r Run, found bool, err error)
	// ListRuns lists the runs an owner app wrote, newest first, at most
	// limit (zero: the store's bound); an empty owner lists every run.
	ListRuns(ctx context.Context, ownerApp string, limit int) (runs []Run, err error)
	// Append prepares and writes entries, in order, and flushes; a failed
	// flush leaves nothing of the batch behind that a later read could
	// mistake for the act having happened. The store assigns each entry's
	// EntryId from its entry stream, draws the openings, commits to the
	// content and chains each entry to the run's head (ADR-0327 §SD9),
	// under the identity on ctx; a writer hands it entries with their
	// ordinals and the head it expects (Prev) and keeps what comes back,
	// in the slice. The run row advances with them: last ordinal, chain
	// head, and the label joined with theirs.
	Append(ctx context.Context, entries []Entry) (err error)
	// Entries reads a run's entries from an ordinal on, in order.
	Entries(ctx context.Context, run uint64, from uint32) (entries []Entry, err error)
	// Tombstone destroys the content and openings of every entry of the
	// run, keeping metadata, labels and commitments (ADR-0308 §SD8,
	// ADR-0320 §SD4); a head entry's content, a digest the anchor law
	// reads, stays (ADR-0314 §SD5). The content never reached the ledger;
	// the vault rows are destroyed.
	Tombstone(ctx context.Context, run uint64) (err error)
}

// AllEntriesI is a store that reads every run's entries in one pass.
type AllEntriesI interface {
	AllEntries(ctx context.Context) (entries []Entry, err error)
}

// EntriesOfI is a store that reads the entries of several runs in one
// pass: what the introspection table uses over the newest runs.
type EntriesOfI interface {
	EntriesOf(ctx context.Context, runs []uint64) (entries []Entry, err error)
}

// DefaultListLimit bounds a list that names no limit.
const DefaultListLimit = 200

// RunOfConversation is the newest run an owner app wrote for a
// conversation: the app names its conversations, the store names its runs
// (ADR-0325 §SD5). It reads the owner's list, so a conversation whose
// newest run is past the store's list bound is not found.
func RunOfConversation(ctx context.Context, store StoreI, ownerApp string, conversation string) (r Run, found bool, err error) {
	runs, err := store.ListRuns(ctx, ownerApp, 0)
	if err != nil {
		return
	}
	for _, c := range runs {
		if c.Conversation == conversation {
			return c, true, nil
		}
	}
	return
}

// MemStore is StoreI with no server: the test double, and the store of a
// host without a persist executor, whose runs then last the process. It
// keeps the ledger and the vault apart as the SQL store does (ADR-0320
// §SD1): an entry without its content in one map, its content and
// openings in another, joined on read and opened.
type MemStore struct {
	mu      sync.Mutex
	ids     agentrunstore.Ids
	runs    map[uint64]Run
	order   []uint64
	entries map[uint64][]Entry
	bodies  map[uint64]map[uint32]Body
	// Log takes what a read found and could not use: a vault row that
	// does not open its entry's commitments.
	Log zerolog.Logger
}

var _ StoreI = (*MemStore)(nil)
var _ AllEntriesI = (*MemStore)(nil)
var _ EntriesOfI = (*MemStore)(nil)
var _ EraserI = (*MemStore)(nil)

// NewMemStore is an empty store.
func NewMemStore() (inst *MemStore) {
	return &MemStore{ids: agentrunstore.MemIds(), runs: make(map[uint64]Run), entries: make(map[uint64][]Entry), bodies: make(map[uint64]map[uint32]Body), Log: zerolog.Nop()}
}

func (inst *MemStore) MintRun(ctx context.Context) (id uint64, err error) {
	return agentrunstore.Mint(ctx, inst.ids.Runs)
}

func (inst *MemStore) CreateRun(_ context.Context, r Run) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, dup := inst.runs[r.Id]; dup {
		return eb.Build().Uint64("run", r.Id).Errorf("duplicate run id")
	}
	inst.runs[r.Id] = r
	inst.order = append(inst.order, r.Id)
	return
}

func (inst *MemStore) UpdateRun(_ context.Context, r Run) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	was, ok := inst.runs[r.Id]
	if !ok {
		return eb.Build().Uint64("run", r.Id).Errorf("no such run")
	}
	if r.Version <= was.Version {
		return eb.Build().Uint64("run", r.Id).Uint32("version", r.Version).Uint32("stored", was.Version).Errorf("not a newer version of the run row")
	}
	inst.runs[r.Id] = r
	return
}

func (inst *MemStore) GetRun(_ context.Context, id uint64) (r Run, found bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	r, found = inst.runs[id]
	return
}

func (inst *MemStore) ListRuns(_ context.Context, ownerApp string, limit int) (runs []Run, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if limit <= 0 {
		limit = DefaultListLimit
	}
	for i := len(inst.order) - 1; i >= 0 && len(runs) < limit; i-- {
		r := inst.runs[inst.order[i]]
		if ownerApp == "" || r.OwnerApp == ownerApp {
			runs = append(runs, r)
		}
	}
	return
}

// Append prepares the entries after each run's head and keeps them, the
// content in the vault and the rest in the ledger: the store assigns the
// ids, draws the openings, commits to the content and chains each entry to
// the one before (ADR-0327 §SD9), under the identity on ctx. The entries
// are updated in place.
func (inst *MemStore) Append(ctx context.Context, entries []Entry) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, e := range entries {
		if _, ok := inst.runs[e.Run]; !ok {
			return eb.Build().Uint64("run", e.Run).Errorf("no such run")
		}
	}
	for i := range entries {
		if entries[i].EntryId, err = agentrunstore.Mint(ctx, inst.ids.Entries); err != nil {
			return
		}
	}
	heads := make(map[uint64]evidence.Head)
	for i := range entries {
		r := inst.runs[entries[i].Run]
		head, ok := heads[r.Id]
		if !ok {
			head = headOf(r)
		}
		if entries[i], err = entries[i].prepared(ctx, head); err != nil {
			return
		}
		heads[r.Id] = evidence.Head{Ordinal: uint64(entries[i].Ordinal), Digest: entries[i].Digest}
	}
	for _, e := range entries {
		if b, ok := bodyOf(e); ok {
			if inst.bodies[e.Run] == nil {
				inst.bodies[e.Run] = make(map[uint32]Body)
			}
			b.Openings = maps.Clone(b.Openings)
			inst.bodies[e.Run][e.Ordinal] = b
		}
		inst.entries[e.Run] = append(inst.entries[e.Run], e.Erased())
		inst.runs[e.Run] = advanced(inst.runs[e.Run], []Entry{e})
	}
	return
}

// headOf is the run's chain head as its row says: the last ordinal and the
// head digest, the seed while there is no entry.
func headOf(r Run) (h evidence.Head) {
	h = evidence.Head{Ordinal: uint64(r.LastOrdinal), Digest: r.Head}
	if h.Digest == "" {
		h.Digest = r.Digest
	}
	return
}

// ledgerEntries are a run's entries as the ledger holds them: no content.
func (inst *MemStore) ledgerEntries(run uint64, from uint32) (entries []Entry) {
	for _, e := range inst.entries[run] {
		if e.Ordinal >= from {
			entries = append(entries, e)
		}
	}
	slices.SortFunc(entries, func(a, b Entry) int { return int(a.Ordinal) - int(b.Ordinal) })
	return
}

// vaultRows are the vault's rows for a run from an ordinal on.
func (inst *MemStore) vaultRows(run uint64, from uint32) (bodies []Body) {
	for _, b := range inst.bodies[run] {
		if b.Ordinal >= from {
			bodies = append(bodies, b)
		}
	}
	return
}

func (inst *MemStore) Entries(_ context.Context, run uint64, from uint32) (entries []Entry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return joined(inst.Log, inst.ledgerEntries(run, from), inst.vaultRows(run, from)), nil
}

func (inst *MemStore) AllEntries(_ context.Context) (entries []Entry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, id := range inst.order {
		entries = append(entries, joined(inst.Log, inst.ledgerEntries(id, 0), inst.vaultRows(id, 0))...)
	}
	return
}

func (inst *MemStore) EntriesOf(_ context.Context, runs []uint64) (entries []Entry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, id := range runs {
		entries = append(entries, joined(inst.Log, inst.ledgerEntries(id, 0), inst.vaultRows(id, 0))...)
	}
	return
}

// Tombstone destroys the run's vault rows but the head entries', whose
// content is a digest the anchor law reads (ADR-0314 §SD5).
func (inst *MemStore) Tombstone(_ context.Context, run uint64) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	keep := make(map[uint32]bool)
	for _, e := range inst.entries[run] {
		if e.Kind == KindHead {
			keep[e.Ordinal] = true
		}
	}
	for ord := range inst.bodies[run] {
		if !keep[ord] {
			delete(inst.bodies[run], ord)
		}
	}
	return
}

// Erase destroys the vault rows at ordinals (ADR-0320 §SD4).
func (inst *MemStore) Erase(_ context.Context, run uint64, ordinals []uint32) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, ord := range ordinals {
		delete(inst.bodies[run], ord)
	}
	return
}

// Now is the clock the writer stamps entries with; a test replaces it.
var Now = func() time.Time { return time.Now().UTC() }

// zeroTime is the unset instant.
var zeroTime time.Time
