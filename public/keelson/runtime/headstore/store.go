package headstore

import (
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// Head is one journal chain head as the journal ships it: the run, the
// ordinal it covers and the chain digest at that ordinal (ADR-0314 §SD2).
// In the anchor store it is the head of scope `run` under the journal's
// scheme (ADR-0327 §SD7).
type Head struct {
	Cell    string
	Run     uint64
	Ordinal uint32
	Digest  string
}

// Receipt is the anchor store's answer to an append.
type Receipt = anchor.Receipt

// Anchored is one journal head the store holds.
type Anchored struct {
	Head
	Receipt Receipt
}

// Cadence is when heads are shipped (ADR-0314 §SD3).
type Cadence = anchor.Cadence

// DefaultCadence is one head per turn end and one at run close.
func DefaultCadence() (c Cadence) { return anchor.DefaultCadence() }

// StoreI is the journal's view of the anchor store: append a run's head,
// read a run's heads. The bus client is one; Over makes one of any
// anchor.StoreI in-process.
type StoreI interface {
	Append(ctx context.Context, h Head) (r Receipt, err error)
	Read(ctx context.Context, cell string, run uint64) (heads []Anchored, err error)
}

// anchorHead is h as the anchor store holds it.
func anchorHead(h Head) anchor.Head {
	return anchor.Head{Cell: h.Cell, Scope: h.Run, Ordinal: uint64(h.Ordinal), Scheme: agentrun.ChainScheme.Name(), Digest: h.Digest}
}

// journalHeads are the journal's heads among a scope's anchored rows: the
// rows under the journal's scheme.
func journalHeads(rows []anchor.Anchored) (heads []Anchored) {
	for _, a := range rows {
		if a.Scheme != agentrun.ChainScheme.Name() {
			continue
		}
		heads = append(heads, Anchored{Head: Head{Cell: a.Cell, Run: a.Scope, Ordinal: uint32(a.Ordinal), Digest: a.Digest}, Receipt: a.Receipt})
	}
	return
}

// Over is StoreI over an anchor store in-process: what a test, and a host
// shipping its own journal, use.
func Over(st anchor.StoreI) StoreI { return over{st: st} }

type over struct{ st anchor.StoreI }

func (inst over) Append(ctx context.Context, h Head) (r Receipt, err error) {
	return inst.st.Append(ctx, anchorHead(h))
}

func (inst over) Read(ctx context.Context, cell string, run uint64) (heads []Anchored, err error) {
	rows, err := inst.st.Read(ctx, cell, run)
	if err != nil {
		return
	}
	return journalHeads(rows), nil
}

// HeadRecordOf is the journal's head entry record for a shipped head
// (ADR-0308 §SD2, a ledger component since ADR-0325 §SD1): the ordinal
// covered, the digest, the receipt, and the cadence it was shipped under.
func HeadRecordOf(h Head, r Receipt, c Cadence) agentrun.HeadRecord {
	return agentrun.HeadRecord{Ordinal: h.Ordinal, Digest: h.Digest, ReceiptId: r.Id, ReceiptAt: r.At.UTC(), Cadence: c.String(), Durable: r.Durable}
}

// HeadEntry is the journal entry that records a shipped head.
func HeadEntry(h Head, r Receipt, c Cadence) (e agentrun.Entry, err error) {
	rec := HeadRecordOf(h, r, c)
	return agentrun.Entry{Kind: agentrun.KindHead, Key: r.Id, Target: h.Ordinal, Head: &rec, Label: agentrun.Label{Level: agentrun.LevelSystem}}, nil
}

// Ship appends the writer's current head to the store and journals the
// head entry: what the coordinator does at each point the cadence names.
func Ship(ctx context.Context, store StoreI, w *agentrun.Writer, c Cadence) (e agentrun.Entry, err error) {
	run := w.Run()
	ordinal, digest := w.Head()
	h := Head{Cell: run.Cell, Run: run.Id, Ordinal: ordinal, Digest: digest}
	r, err := store.Append(ctx, h)
	if err != nil {
		return
	}
	if e, err = HeadEntry(h, r, c); err != nil {
		return
	}
	written, err := w.Append(ctx, e)
	if err != nil {
		return
	}
	return written[0], nil
}

// VerifyHeads is law L32 (ADR-0314 §SD2) over a journal run, in both
// directions: every head entry's digest equals the chain digest at the
// ordinal it covers, recomputed from the run row's digest over the
// entries, and the anchor store holds the same digest for that ordinal;
// and every anchored head is reproduced by the chain and recorded by a
// head entry, so a journal whose head entries were dropped, or whose tail
// past the last head was rewritten and re-anchored, breaks. Of several
// receipts for one ordinal the earliest counts, and a later one with
// another digest breaks the ordinal. It returns the ordinals that break
// it, each once, in order: a head entry's own ordinal, or the ordinal an
// anchored head covers when no head entry stands for it. An entry the
// chain itself breaks at (law L31) breaks every head past it.
func VerifyHeads(runDigest string, entries []agentrun.Entry, anchored []Anchored) (broken []uint32) {
	rows, err := agentrun.Rows(entries)
	if err != nil {
		// A row whose form cannot be computed breaks at itself and every
		// head past it: the generic law reports what it can over nothing.
		for _, e := range entries {
			broken = append(broken, e.Ordinal)
		}
		slices.Sort(broken)
		return
	}
	heads := make([]evidence.AnchoredHead, 0, len(anchored))
	for _, a := range anchored {
		heads = append(heads, evidence.AnchoredHead{Ordinal: uint64(a.Ordinal), Digest: a.Digest, ReceiptAt: a.Receipt.At, ReceiptId: a.Receipt.Id})
	}
	for _, o := range evidence.VerifyHeads(agentrun.ChainScheme, runDigest, rows, heads) {
		broken = append(broken, uint32(o))
	}
	return
}

// LawL32SQL is L32 over the journal and the anchor relation for the
// integration lane, in both directions: head entries whose digest is not
// the entry's digest at the ordinal they cover, or not what the anchor
// store holds under the journal's scheme; and anchored journal heads the
// journal does not reproduce or no head entry records.
const LawL32SQL = `
SELECT h.run, h.ordinal
FROM keelson('agent_run_entries') AS h
LEFT JOIN keelson('agent_run_entries') AS c ON c.run = h.run AND c.ordinal = JSONExtractUInt(h.body, 'ordinal')
LEFT JOIN keelson('anchors') AS a ON a.scope = h.run AND a.ordinal = JSONExtractUInt(h.body, 'ordinal') AND a.scheme = c.scheme
WHERE h.kind = 'head'
  AND (c.digest != JSONExtractString(h.body, 'digest') OR a.digest != JSONExtractString(h.body, 'digest'))
UNION ALL
SELECT a.scope, a.ordinal
FROM keelson('anchors') AS a
LEFT JOIN keelson('agent_run_entries') AS c ON c.run = a.scope AND c.ordinal = a.ordinal
LEFT JOIN keelson('agent_run_entries') AS h ON h.run = a.scope AND h.kind = 'head' AND JSONExtractUInt(h.body, 'ordinal') = a.ordinal
WHERE a.scheme = c.scheme AND (c.digest != a.digest OR h.ordinal = 0)`

// MemStore is the journal's view over an anchor.MemStore: the test
// double, and the store of a host without an organisation's store.
type MemStore struct {
	over
	// Mem is the anchor store underneath, for the service and the relation.
	Mem *anchor.MemStore
}

// NewMemStore is an empty memory store under the journal's view.
func NewMemStore() (inst *MemStore) {
	m := anchor.NewMemStore()
	return &MemStore{over: over{st: m}, Mem: m}
}

// All is every head the store holds, in receipt order.
func (inst *MemStore) All() (rows []anchor.Anchored) { return inst.Mem.All() }
