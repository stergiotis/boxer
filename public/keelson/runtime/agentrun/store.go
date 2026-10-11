package agentrun

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// StoreI is the seam between the writer and the record (ADR-0308 §SD1):
// a run row created and updated, entries appended and flushed before the
// act leaves, and the reads the service, the fold and the introspection
// tables need. Safe for concurrent use.
type StoreI interface {
	// CreateRun writes a new run row; a duplicate id is refused.
	CreateRun(ctx context.Context, r Run) (err error)
	// UpdateRun rewrites the run row: state, label, fingerprint, closed,
	// deletion. The row's version orders the writes.
	UpdateRun(ctx context.Context, r Run) (err error)
	// GetRun reads one run row.
	GetRun(ctx context.Context, id string) (r Run, found bool, err error)
	// ListRuns lists the runs an owner app wrote, newest first, at most
	// limit (zero: the store's bound); an empty owner lists every run.
	ListRuns(ctx context.Context, ownerApp string, limit int) (runs []Run, err error)
	// Append writes entries, in order, and flushes; a failed flush leaves
	// nothing of the batch behind that a later read could mistake for the
	// act having happened. The run row advances with them: last ordinal,
	// chain head, and the label joined with theirs.
	Append(ctx context.Context, entries []Entry) (err error)
	// Entries reads a run's entries from an ordinal on, in order.
	Entries(ctx context.Context, run string, from uint32) (entries []Entry, err error)
	// Tombstone empties every body of the run's entries, keeping their
	// metadata, labels and digests (ADR-0308 §SD8); a head entry's body,
	// a digest the anchor law reads, stays (ADR-0314 §SD5).
	Tombstone(ctx context.Context, run string) (err error)
}

// AllEntriesI is a store that reads every run's entries in one pass.
type AllEntriesI interface {
	AllEntries(ctx context.Context) (entries []Entry, err error)
}

// EntriesOfI is a store that reads the entries of several runs in one
// pass: what the introspection table uses over the newest runs.
type EntriesOfI interface {
	EntriesOf(ctx context.Context, runs []string) (entries []Entry, err error)
}

// DefaultListLimit bounds a list that names no limit.
const DefaultListLimit = 200

// MemStore is StoreI with no server: the test double, and the store of a
// host without a persist executor, whose runs then last the process.
type MemStore struct {
	mu      sync.Mutex
	runs    map[string]Run
	order   []string
	entries map[string][]Entry
}

var _ StoreI = (*MemStore)(nil)
var _ AllEntriesI = (*MemStore)(nil)
var _ EntriesOfI = (*MemStore)(nil)

// NewMemStore is an empty store.
func NewMemStore() (inst *MemStore) {
	return &MemStore{runs: make(map[string]Run), entries: make(map[string][]Entry)}
}

func (inst *MemStore) CreateRun(_ context.Context, r Run) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, dup := inst.runs[r.Id]; dup {
		return eb.Build().Str("run", r.Id).Errorf("duplicate run id")
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
		return eb.Build().Str("run", r.Id).Errorf("no such run")
	}
	if r.Version <= was.Version {
		return eb.Build().Str("run", r.Id).Uint32("version", r.Version).Uint32("stored", was.Version).Errorf("not a newer version of the run row")
	}
	inst.runs[r.Id] = r
	return
}

func (inst *MemStore) GetRun(_ context.Context, id string) (r Run, found bool, err error) {
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

func (inst *MemStore) Append(_ context.Context, entries []Entry) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, e := range entries {
		if _, ok := inst.runs[e.Run]; !ok {
			return eb.Build().Str("run", e.Run).Errorf("no such run")
		}
	}
	for _, e := range entries {
		inst.entries[e.Run] = append(inst.entries[e.Run], e)
		inst.runs[e.Run] = advanced(inst.runs[e.Run], []Entry{e})
	}
	return
}

func (inst *MemStore) Entries(_ context.Context, run string, from uint32) (entries []Entry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, e := range inst.entries[run] {
		if e.Ordinal >= from {
			entries = append(entries, e)
		}
	}
	slices.SortFunc(entries, func(a, b Entry) int { return int(a.Ordinal) - int(b.Ordinal) })
	return
}

func (inst *MemStore) AllEntries(_ context.Context) (entries []Entry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, id := range inst.order {
		entries = append(entries, inst.entries[id]...)
	}
	return
}

func (inst *MemStore) EntriesOf(_ context.Context, runs []string) (entries []Entry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, id := range runs {
		entries = append(entries, inst.entries[id]...)
	}
	return
}

func (inst *MemStore) Tombstone(_ context.Context, run string) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	es := inst.entries[run]
	for i := range es {
		if es[i].Kind == KindHead {
			continue
		}
		es[i].Body, es[i].Calls = "", ""
	}
	return
}

// Now is the clock the writer stamps entries with; a test replaces it.
var Now = func() time.Time { return time.Now().UTC() }

// zeroTime is the unset instant.
var zeroTime time.Time
