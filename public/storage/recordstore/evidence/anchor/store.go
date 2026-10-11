package anchor

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// Head is one head shipped to the store: the scope's ordinal it covers,
// the digest at that ordinal, and the scheme the digest is under. For a
// chained store the ordinal is the row's; for a sealed store it is the
// seal's place in its stream, from 1.
type Head struct {
	Cell    string
	Scope   uint64
	Ordinal uint64
	Scheme  string
	Digest  string
}

// Receipt is the store's answer to an append: an id and the time it
// received the head, what a head row records.
type Receipt struct {
	Id string
	At time.Time
	// By names the sender the store received the head from.
	By string
	// Durable says the store holds the head beyond the process that
	// shipped it: an organisation's store does, the memory store does
	// not, and a head row records which.
	Durable bool
}

// Anchored is one row of the store.
type Anchored struct {
	Head
	Receipt Receipt
}

// StoreI is the contract: append-only, read by (cell, scope).
type StoreI interface {
	Append(ctx context.Context, h Head) (r Receipt, err error)
	Read(ctx context.Context, cell string, scope uint64) (heads []Anchored, err error)
}

// Now is the store's clock; a test replaces it.
var Now = func() time.Time { return time.Now().UTC() }

// MemStore is StoreI in memory: the test double, and the store of a host
// without an organisation's store, which anchors nothing beyond the
// process; its receipts say so. A head already held for an ordinal under
// its scheme is returned as it was received; a different digest there is
// refused, so the earliest receipt is the one that counts.
type MemStore struct {
	mu     sync.Mutex
	rows   []Anchored
	minted uint64
}

var _ StoreI = (*MemStore)(nil)

// NewMemStore is an empty store.
func NewMemStore() (inst *MemStore) { return &MemStore{} }

// All is every head the store holds, in receipt order: what an
// introspection table shows.
func (inst *MemStore) All() (rows []Anchored) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append(rows, inst.rows...)
}

func (inst *MemStore) Append(_ context.Context, h Head) (r Receipt, err error) {
	return inst.AppendBy(h, "")
}

// AppendBy is Append with the sender the receipt names.
func (inst *MemStore) AppendBy(h Head, by string) (r Receipt, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, a := range inst.rows {
		if a.Cell == h.Cell && a.Scope == h.Scope && a.Ordinal == h.Ordinal && a.Scheme == h.Scheme {
			if a.Digest != h.Digest {
				return r, eb.Build().Uint64("scope", h.Scope).Uint64("ordinal", h.Ordinal).Str("scheme", h.Scheme).Errorf("another digest is anchored at the ordinal")
			}
			return a.Receipt, nil
		}
	}
	inst.minted++
	r = Receipt{Id: "anchor-" + strconv.FormatUint(inst.minted, 36), At: Now(), By: by, Durable: false}
	inst.rows = append(inst.rows, Anchored{Head: h, Receipt: r})
	return
}

func (inst *MemStore) Read(_ context.Context, cell string, scope uint64) (heads []Anchored, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, a := range inst.rows {
		if a.Cell == cell && a.Scope == scope {
			heads = append(heads, a)
		}
	}
	return
}

// HeadsOf is the anchored heads of one scheme as the laws read them
// (evidence.VerifyHeads, evidence.VerifySealAnchors); the other schemes'
// rows under the scope are left out.
func HeadsOf(anchored []Anchored, scheme string) (heads []evidence.AnchoredHead) {
	heads = make([]evidence.AnchoredHead, 0, len(anchored))
	for _, a := range anchored {
		if a.Scheme != scheme {
			continue
		}
		heads = append(heads, evidence.AnchoredHead{Ordinal: a.Ordinal, Digest: a.Digest, ReceiptAt: a.Receipt.At, ReceiptId: a.Receipt.Id})
	}
	return
}

// Cadence is when heads are shipped (ADR-0314 §SD3): profile data,
// recorded on each head row. Detection granularity against a rewrite
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
