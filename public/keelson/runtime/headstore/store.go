package headstore

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// Head is one chain head shipped to the store: the ordinal it covers and
// the chain digest at that ordinal (ADR-0314 §SD2).
type Head struct {
	Cell    string
	Run     uint64
	Ordinal uint32
	Digest  string
}

// Receipt is the store's answer to an append: an id and the time it
// received the head, what the journal's head entry records.
type Receipt struct {
	Id string
	At time.Time
	// By names the sender the store received the head from.
	By string
	// Durable says the store holds the head beyond the process that
	// shipped it: an organisation's store does, the memory store does
	// not, and a head entry records which (ADR-0314 §SD2).
	Durable bool
}

// Anchored is one row of the store.
type Anchored struct {
	Head
	Receipt Receipt
}

// StoreI is the contract: append-only, read by (cell, run).
type StoreI interface {
	Append(ctx context.Context, h Head) (r Receipt, err error)
	Read(ctx context.Context, cell string, run uint64) (heads []Anchored, err error)
}

// Now is the store's clock; a test replaces it.
var Now = func() time.Time { return time.Now().UTC() }

// MemStore is StoreI in memory: the test double, and the store of a host
// without an organisation's store, which anchors nothing beyond the
// process; its receipts say so. A head already held for an ordinal is
// returned as it was received; a different digest for the same ordinal
// is refused, so the earliest receipt is the one that counts.
type MemStore struct {
	mu     sync.Mutex
	rows   []Anchored
	minted uint64
}

var _ StoreI = (*MemStore)(nil)

// NewMemStore is an empty store.
func NewMemStore() (inst *MemStore) { return &MemStore{} }

// All is every head the store holds, in receipt order: what the
// introspection table shows.
func (inst *MemStore) All() (rows []Anchored) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append(rows, inst.rows...)
}

func (inst *MemStore) Append(_ context.Context, h Head) (r Receipt, err error) {
	return inst.appendBy(h, "")
}

func (inst *MemStore) appendBy(h Head, by string) (r Receipt, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, a := range inst.rows {
		if a.Cell == h.Cell && a.Run == h.Run && a.Ordinal == h.Ordinal {
			if a.Digest != h.Digest {
				return r, eb.Build().Uint64("run", h.Run).Uint32("ordinal", h.Ordinal).Errorf("another digest is anchored at the ordinal")
			}
			return a.Receipt, nil
		}
	}
	inst.minted++
	r = Receipt{Id: "anchor-" + strconv.FormatUint(inst.minted, 36), At: Now(), By: by, Durable: false}
	inst.rows = append(inst.rows, Anchored{Head: h, Receipt: r})
	return
}

func (inst *MemStore) Read(_ context.Context, cell string, run uint64) (heads []Anchored, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, a := range inst.rows {
		if a.Cell == cell && a.Run == run {
			heads = append(heads, a)
		}
	}
	return
}

// Cadence is when heads are shipped (ADR-0314 §SD3): profile data,
// recorded on each head entry. Detection granularity against a rewrite
// equals it.
type Cadence struct {
	PerTurn     bool
	PerRunClose bool
	PerSync     bool
}

// DefaultCadence is one head per turn end and one at run close.
func DefaultCadence() (c Cadence) { return Cadence{PerTurn: true, PerRunClose: true} }

func (inst Cadence) String() (s string) {
	var parts []string
	if inst.PerTurn {
		parts = append(parts, "turn")
	}
	if inst.PerRunClose {
		parts = append(parts, "close")
	}
	if inst.PerSync {
		parts = append(parts, "sync")
	}
	if len(parts) == 0 {
		return "never"
	}
	return strings.Join(parts, ",")
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
	r, err := store.Append(ctx, Head{Cell: run.Cell, Run: run.Id, Ordinal: ordinal, Digest: digest})
	if err != nil {
		return
	}
	if e, err = HeadEntry(Head{Cell: run.Cell, Run: run.Id, Ordinal: ordinal, Digest: digest}, r, c); err != nil {
		return
	}
	written, err := w.Append(ctx, e)
	if err != nil {
		return
	}
	return written[0], nil
}

// VerifyHeads is law L32 (ADR-0314 §SD2), in both directions: every head
// entry's digest equals the chain digest at the ordinal it covers,
// recomputed from the run row's digest over the entries, and the anchor
// store holds the same digest for that ordinal; and every anchored head
// is reproduced by the chain and recorded by a head entry, so a journal
// whose head entries were dropped, or whose tail past the last head was
// rewritten and re-anchored, breaks. Of several receipts for one ordinal
// the earliest counts, and a later one with another digest breaks the
// ordinal. It returns the ordinals that break it, each once, in order:
// a head entry's own ordinal, or the ordinal an anchored head covers when
// no head entry stands for it. An entry the chain itself breaks at (law
// L31) breaks every head past it.
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

// LawL32SQL is L32 over the journal and an anchor table for the
// integration lane, in both directions: head entries whose digest is not
// the entry's digest at the ordinal they cover, or not what the anchor
// store holds; and anchored heads the journal does not reproduce or no
// head entry records.
const LawL32SQL = `
SELECT h.run, h.ordinal
FROM keelson('agent_run_entries') AS h
LEFT JOIN keelson('agent_run_entries') AS c ON c.run = h.run AND c.ordinal = JSONExtractUInt(h.body, 'ordinal')
LEFT JOIN keelson('anchors') AS a ON a.run = h.run AND a.ordinal = JSONExtractUInt(h.body, 'ordinal')
WHERE h.kind = 'head'
  AND (c.digest != JSONExtractString(h.body, 'digest') OR a.digest != JSONExtractString(h.body, 'digest'))
UNION ALL
SELECT a.run, a.ordinal
FROM keelson('anchors') AS a
LEFT JOIN keelson('agent_run_entries') AS c ON c.run = a.run AND c.ordinal = a.ordinal
LEFT JOIN keelson('agent_run_entries') AS h ON h.run = a.run AND h.kind = 'head' AND JSONExtractUInt(h.body, 'ordinal') = a.ordinal
WHERE c.digest != a.digest OR h.ordinal = 0`
