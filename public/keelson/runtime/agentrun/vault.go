package agentrun

import (
	"context"
	"slices"
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The vault (ADR-0320 §SD1): the fields of an entry that could be
// personal, kept in a store of their own beside the ledger, each row with
// the opening the ledger's commitments open under. VaultedStore is the
// StoreI a host runs: a ledger store that never sees a body, and a body
// store that is destroyed row by row when a run is deleted or an entry
// erased (§SD4). Reads join the two and open every commitment (§SD5).

// Body is one vault row: an entry's content and its opening.
type Body struct {
	Run     string
	Ordinal uint32
	Opening []byte
	Body    string
	Calls   string
}

// bodyOf is the vault row an entry's content makes; empty for an entry
// without content.
func bodyOf(e Entry) (b Body, ok bool) {
	if !e.HasContent() {
		return
	}
	return Body{Run: e.Run, Ordinal: e.Ordinal, Opening: e.Opening, Body: e.Body, Calls: e.Calls}, true
}

// BodyStoreI is the vault's seam (ADR-0320 §SD7): the one place a body
// is written, read or destroyed, so an event per call has one place to
// hook. Safe for concurrent use.
type BodyStoreI interface {
	// Put writes rows and flushes.
	Put(ctx context.Context, bodies []Body) (err error)
	// Get reads a run's rows from an ordinal on.
	Get(ctx context.Context, run string, from uint32) (bodies []Body, err error)
	// GetOf reads the rows of several runs.
	GetOf(ctx context.Context, runs []string) (bodies []Body, err error)
	// Destroy removes a run's rows except those at keep, so that nothing
	// of them remains in the table: a deletion that rewrites, not a
	// mask, where the backend distinguishes the two.
	Destroy(ctx context.Context, run string, keep []uint32) (err error)
	// Erase removes the rows at ordinals.
	Erase(ctx context.Context, run string, ordinals []uint32) (err error)
}

// EraserI is a store that erases the content of named entries of a run
// after their erased entries are in the chain (ADR-0320 §SD4).
type EraserI interface {
	Erase(ctx context.Context, run string, ordinals []uint32) (err error)
}

// MemBodyStore is BodyStoreI with no server.
type MemBodyStore struct {
	mu   sync.Mutex
	rows map[string]map[uint32]Body
}

var _ BodyStoreI = (*MemBodyStore)(nil)

// NewMemBodyStore is an empty vault.
func NewMemBodyStore() *MemBodyStore { return &MemBodyStore{rows: make(map[string]map[uint32]Body)} }

func (inst *MemBodyStore) Put(_ context.Context, bodies []Body) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, b := range bodies {
		m := inst.rows[b.Run]
		if m == nil {
			m = make(map[uint32]Body)
			inst.rows[b.Run] = m
		}
		b.Opening = slices.Clone(b.Opening)
		m[b.Ordinal] = b
	}
	return
}

func (inst *MemBodyStore) Get(_ context.Context, run string, from uint32) (bodies []Body, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, b := range inst.rows[run] {
		if b.Ordinal >= from {
			bodies = append(bodies, b)
		}
	}
	return
}

func (inst *MemBodyStore) GetOf(_ context.Context, runs []string) (bodies []Body, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, run := range runs {
		for _, b := range inst.rows[run] {
			bodies = append(bodies, b)
		}
	}
	return
}

func (inst *MemBodyStore) Destroy(_ context.Context, run string, keep []uint32) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for ord := range inst.rows[run] {
		if !slices.Contains(keep, ord) {
			delete(inst.rows[run], ord)
		}
	}
	return
}

func (inst *MemBodyStore) Erase(_ context.Context, run string, ordinals []uint32) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, ord := range ordinals {
		delete(inst.rows[run], ord)
	}
	return
}

// VaultedStore is StoreI over a ledger store and a vault: entries go to
// the ledger without their content, their content to the vault, and come
// back joined, every commitment opened. The ledger store is any StoreI;
// the host's is the SQL store or the in-memory one.
type VaultedStore struct {
	ledger StoreI
	vault  BodyStoreI
	// Log takes what a read found and could not use: a vault row that
	// does not open its entry's commitments.
	Log zerolog.Logger
}

var _ StoreI = (*VaultedStore)(nil)
var _ AllEntriesI = (*VaultedStore)(nil)
var _ EntriesOfI = (*VaultedStore)(nil)
var _ EraserI = (*VaultedStore)(nil)

// NewVaulted is the store a host runs: ledger for everything but content,
// vault for content.
func NewVaulted(ledger StoreI, vault BodyStoreI) *VaultedStore {
	return &VaultedStore{ledger: ledger, vault: vault, Log: zerolog.Nop()}
}

// Ledger is the store underneath, for a caller that reads the ledger as
// the ledger: no content.
func (inst *VaultedStore) Ledger() StoreI { return inst.ledger }

func (inst *VaultedStore) CreateRun(ctx context.Context, r Run) (err error) {
	return inst.ledger.CreateRun(ctx, r)
}
func (inst *VaultedStore) UpdateRun(ctx context.Context, r Run) (err error) {
	return inst.ledger.UpdateRun(ctx, r)
}
func (inst *VaultedStore) GetRun(ctx context.Context, id string) (r Run, found bool, err error) {
	return inst.ledger.GetRun(ctx, id)
}
func (inst *VaultedStore) ListRuns(ctx context.Context, ownerApp string, limit int) (runs []Run, err error) {
	return inst.ledger.ListRuns(ctx, ownerApp, limit)
}

// Append writes the content first, then the ledger rows without it: a
// ledger row whose content is missing reads as erased and breaks law L31
// unless the chain explains it, while a vault row without its ledger row
// is dead weight nothing reads.
func (inst *VaultedStore) Append(ctx context.Context, entries []Entry) (err error) {
	bodies := make([]Body, 0, len(entries))
	stripped := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if b, ok := bodyOf(e); ok {
			if !e.BodiesMatch() {
				return eb.Build().Str("run", e.Run).Uint32("ordinal", e.Ordinal).Errorf("the entry's content does not open its commitments")
			}
			bodies = append(bodies, b)
		}
		stripped = append(stripped, e.Erased())
	}
	if len(bodies) > 0 {
		if err = inst.vault.Put(ctx, bodies); err != nil {
			return eh.Errorf("vault: %w", err)
		}
	}
	return inst.ledger.Append(ctx, stripped)
}

func (inst *VaultedStore) Entries(ctx context.Context, run string, from uint32) (entries []Entry, err error) {
	if entries, err = inst.ledger.Entries(ctx, run, from); err != nil || len(entries) == 0 {
		return
	}
	bodies, err := inst.vault.Get(ctx, run, from)
	if err != nil {
		return nil, eh.Errorf("vault: %w", err)
	}
	return inst.joined(entries, bodies), nil
}

func (inst *VaultedStore) AllEntries(ctx context.Context) (entries []Entry, err error) {
	all, ok := inst.ledger.(AllEntriesI)
	if !ok {
		return nil, eh.Errorf("the ledger store does not read every run")
	}
	if entries, err = all.AllEntries(ctx); err != nil || len(entries) == 0 {
		return
	}
	bodies, err := inst.vault.GetOf(ctx, runsOf(entries))
	if err != nil {
		return nil, eh.Errorf("vault: %w", err)
	}
	return inst.joined(entries, bodies), nil
}

func (inst *VaultedStore) EntriesOf(ctx context.Context, runs []string) (entries []Entry, err error) {
	if len(runs) == 0 {
		return
	}
	if of, ok := inst.ledger.(EntriesOfI); ok {
		entries, err = of.EntriesOf(ctx, runs)
	} else {
		for _, run := range runs {
			var es []Entry
			if es, err = inst.ledger.Entries(ctx, run, 0); err != nil {
				return
			}
			entries = append(entries, es...)
		}
	}
	if err != nil || len(entries) == 0 {
		return
	}
	bodies, err := inst.vault.GetOf(ctx, runs)
	if err != nil {
		return nil, eh.Errorf("vault: %w", err)
	}
	return inst.joined(entries, bodies), nil
}

// joined gives each entry the content of its vault row when the row
// opens the entry's commitments; a row that does not is ignored and
// logged, and the entry reads as erased (ADR-0320 §SD5).
func (inst *VaultedStore) joined(entries []Entry, bodies []Body) (out []Entry) {
	type key struct {
		run string
		ord uint32
	}
	byKey := make(map[key]Body, len(bodies))
	for _, b := range bodies {
		byKey[key{b.Run, b.Ordinal}] = b
	}
	out = make([]Entry, 0, len(entries))
	for _, e := range entries {
		if b, ok := byKey[key{e.Run, e.Ordinal}]; ok {
			with := e
			with.Opening, with.Body, with.Calls = b.Opening, b.Body, b.Calls
			if with.BodiesMatch() {
				e = with
			} else {
				inst.Log.Warn().Str("run", e.Run).Uint32("ordinal", e.Ordinal).Msg("a vault row does not open its entry's commitments; the entry reads as erased")
			}
		}
		out = append(out, e)
	}
	return
}

// Tombstone destroys the run's vault rows but the head entries', whose
// bodies are digests the anchor law reads (ADR-0314 §SD5), then lets the
// ledger do what it does for a deleted run, which for a ledger without
// content is nothing.
func (inst *VaultedStore) Tombstone(ctx context.Context, run string) (err error) {
	entries, err := inst.ledger.Entries(ctx, run, 0)
	if err != nil {
		return
	}
	var keep []uint32
	for _, e := range entries {
		if e.Kind == KindHead {
			keep = append(keep, e.Ordinal)
		}
	}
	if err = inst.vault.Destroy(ctx, run, keep); err != nil {
		return eh.Errorf("vault: %w", err)
	}
	return inst.ledger.Tombstone(ctx, run)
}

// Erase destroys the vault rows at ordinals (ADR-0320 §SD4).
func (inst *VaultedStore) Erase(ctx context.Context, run string, ordinals []uint32) (err error) {
	if len(ordinals) == 0 {
		return
	}
	if err = inst.vault.Erase(ctx, run, ordinals); err != nil {
		return eh.Errorf("vault: %w", err)
	}
	return
}
