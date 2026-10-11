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
	Run       uint64
	Ordinal   uint32
	Opening   []byte
	Body      string
	Calls     string
	Arguments string
	// The vault's records (ADR-0325 §SD1), and an approval's content part.
	Endorse  *Endorsement
	Grant    *GrantRecord
	Question *ExitQuestion
	Answer   *ExitAnswer
	Copy     *CopyRecord
	Approval *ApprovalPart
}

// ApprovalPart is the part of an approval the vault holds: the
// justification given and the payload's content digest.
type ApprovalPart struct {
	Justification string
	Digest        string
}

// bodyOf is the vault row an entry's content makes; empty for an entry
// without content.
func bodyOf(e Entry) (b Body, ok bool) {
	if !e.HasContent() {
		return
	}
	b = Body{Run: e.Run, Ordinal: e.Ordinal, Opening: e.Opening, Body: e.Body, Calls: e.Calls, Arguments: e.Arguments,
		Endorse: e.Endorse, Grant: e.Grant, Question: e.Question, Answer: e.Answer, Copy: e.Copy}
	if e.Approval != nil {
		b.Approval = &ApprovalPart{Justification: e.Approval.Justification, Digest: e.Approval.Digest}
	}
	return b, true
}

// withBody is the entry joined with its vault row.
func withBody(e Entry, b Body) Entry {
	e.Opening, e.Body, e.Calls, e.Arguments = b.Opening, b.Body, b.Calls, b.Arguments
	e.Endorse, e.Grant, e.Question, e.Answer, e.Copy = b.Endorse, b.Grant, b.Question, b.Answer, b.Copy
	if e.Approval != nil && b.Approval != nil {
		a := *e.Approval
		a.Justification, a.Digest = b.Approval.Justification, b.Approval.Digest
		e.Approval = &a
	}
	return e
}

// BodyStoreI is the vault's seam (ADR-0320 §SD7): the one place a body
// is written, read or destroyed, so an event per call has one place to
// hook. Safe for concurrent use.
type BodyStoreI interface {
	// Put writes rows and flushes.
	Put(ctx context.Context, bodies []Body) (err error)
	// Get reads a run's rows from an ordinal on.
	Get(ctx context.Context, run uint64, from uint32) (bodies []Body, err error)
	// GetOf reads the rows of several runs.
	GetOf(ctx context.Context, runs []uint64) (bodies []Body, err error)
	// Destroy removes a run's rows except those at keep, so that nothing
	// of them remains in the table: a deletion that rewrites, not a
	// mask, where the backend distinguishes the two.
	Destroy(ctx context.Context, run uint64, keep []uint32) (err error)
	// Erase removes the rows at ordinals.
	Erase(ctx context.Context, run uint64, ordinals []uint32) (err error)
}

// EraserI is a store that erases the content of named entries of a run
// after their erased entries are in the chain (ADR-0320 §SD4).
type EraserI interface {
	Erase(ctx context.Context, run uint64, ordinals []uint32) (err error)
}

// MemBodyStore is BodyStoreI with no server.
type MemBodyStore struct {
	mu   sync.Mutex
	rows map[uint64]map[uint32]Body
}

var _ BodyStoreI = (*MemBodyStore)(nil)

// NewMemBodyStore is an empty vault.
func NewMemBodyStore() *MemBodyStore { return &MemBodyStore{rows: make(map[uint64]map[uint32]Body)} }

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

func (inst *MemBodyStore) Get(_ context.Context, run uint64, from uint32) (bodies []Body, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, b := range inst.rows[run] {
		if b.Ordinal >= from {
			bodies = append(bodies, b)
		}
	}
	return
}

func (inst *MemBodyStore) GetOf(_ context.Context, runs []uint64) (bodies []Body, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, run := range runs {
		for _, b := range inst.rows[run] {
			bodies = append(bodies, b)
		}
	}
	return
}

func (inst *MemBodyStore) Destroy(_ context.Context, run uint64, keep []uint32) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for ord := range inst.rows[run] {
		if !slices.Contains(keep, ord) {
			delete(inst.rows[run], ord)
		}
	}
	return
}

func (inst *MemBodyStore) Erase(_ context.Context, run uint64, ordinals []uint32) (err error) {
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

func (inst *VaultedStore) MintRun(ctx context.Context) (id uint64, err error) {
	return inst.ledger.MintRun(ctx)
}
func (inst *VaultedStore) CreateRun(ctx context.Context, r Run) (err error) {
	return inst.ledger.CreateRun(ctx, r)
}
func (inst *VaultedStore) UpdateRun(ctx context.Context, r Run) (err error) {
	return inst.ledger.UpdateRun(ctx, r)
}
func (inst *VaultedStore) GetRun(ctx context.Context, id uint64) (r Run, found bool, err error) {
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
				return eb.Build().Uint64("run", e.Run).Uint32("ordinal", e.Ordinal).Errorf("the entry's content does not open its commitments")
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
	if err = inst.ledger.Append(ctx, stripped); err != nil {
		return
	}
	for i := range entries {
		entries[i].EntryId = stripped[i].EntryId
	}
	return
}

func (inst *VaultedStore) Entries(ctx context.Context, run uint64, from uint32) (entries []Entry, err error) {
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

func (inst *VaultedStore) EntriesOf(ctx context.Context, runs []uint64) (entries []Entry, err error) {
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
		run uint64
		ord uint32
	}
	// An ordinal can hold more than one row — an append retried after its
	// ledger write failed, or a batch in flight when another process took
	// the run over (ADR-0319 §SD7) — so every row is tried and the one
	// that opens is kept.
	byKey := make(map[key][]Body, len(bodies))
	for _, b := range bodies {
		k := key{b.Run, b.Ordinal}
		byKey[k] = append(byKey[k], b)
	}
	out = make([]Entry, 0, len(entries))
	for _, e := range entries {
		if rows, ok := byKey[key{e.Run, e.Ordinal}]; ok {
			opened := false
			for _, b := range rows {
				with := withBody(e, b)
				if with.BodiesMatch() {
					e, opened = with, true
					break
				}
			}
			if !opened {
				inst.Log.Warn().Uint64("run", e.Run).Uint32("ordinal", e.Ordinal).Msg("a vault row does not open its entry's commitments; the entry reads as erased")
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
func (inst *VaultedStore) Tombstone(ctx context.Context, run uint64) (err error) {
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
func (inst *VaultedStore) Erase(ctx context.Context, run uint64, ordinals []uint32) (err error) {
	if len(ordinals) == 0 {
		return
	}
	if err = inst.vault.Erase(ctx, run, ordinals); err != nil {
		return eh.Errorf("vault: %w", err)
	}
	return
}
