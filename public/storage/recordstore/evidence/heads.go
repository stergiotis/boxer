package evidence

import (
	"slices"
	"strings"
	"time"
)

// AnchoredHead is one head as the anchor store holds it: the ordinal it
// covers, the digest, and the receipt that orders it against others for
// the same ordinal.
type AnchoredHead struct {
	Ordinal   uint64
	Digest    string
	ReceiptAt time.Time
	ReceiptId string
}

// VerifyHeads is law L32 in both directions: every head row's digest
// equals the chain digest at the ordinal it covers, recomputed from seed
// over rows, and the anchor store holds the same digest for that ordinal;
// and every anchored head is reproduced by the chain and recorded by a
// head row, so a scope whose head rows were dropped, or whose tail past the
// last head was rewritten and re-anchored, breaks. Of several receipts for
// one ordinal the earliest counts, and a later one with another digest
// breaks the ordinal. It returns the ordinals that break it, each once,
// in order: a head row's own ordinal, or the ordinal an anchored head
// covers when no head row stands for it. A row the chain itself breaks at
// (law L31) breaks every head past it.
func VerifyHeads(scheme Scheme, seed string, rows []RowI, anchored []AnchoredHead) (broken []uint64) {
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b RowI) int {
		switch {
		case a.Ordinal() < b.Ordinal():
			return -1
		case a.Ordinal() > b.Ordinal():
			return 1
		}
		return 0
	})
	// The chain as recomputed, ordinal by ordinal, up to the first break.
	recomputed := make(map[uint64]string, len(sorted)+1)
	recomputed[0] = seed
	chainBrokenAt := uint64(0)
	if b, _, _ := Verify(scheme, seed, sorted); b != 0 {
		chainBrokenAt = b
	}
	prevDigest := seed
	for _, r := range sorted {
		if chainBrokenAt != 0 && r.Ordinal() >= chainBrokenAt {
			break
		}
		item, err := r.CanonicalItem()
		if err != nil {
			break
		}
		d := scheme.ChainDigest(prevDigest, item)
		recomputed[r.Ordinal()] = d
		prevDigest = d
	}
	chainHolds := func(ordinal uint64, digest string) bool {
		d, ok := recomputed[ordinal]
		return ok && (chainBrokenAt == 0 || ordinal < chainBrokenAt) && d == digest
	}
	// The store's heads, earliest receipt first; a conflicting later
	// receipt breaks its ordinal.
	byReceipt := slices.Clone(anchored)
	slices.SortStableFunc(byReceipt, func(a, b AnchoredHead) int {
		if c := a.ReceiptAt.Compare(b.ReceiptAt); c != 0 {
			return c
		}
		return strings.Compare(a.ReceiptId, b.ReceiptId)
	})
	stored := make(map[uint64]string, len(byReceipt))
	bad := make(map[uint64]bool)
	for _, a := range byReceipt {
		if first, seen := stored[a.Ordinal]; seen {
			if first != a.Digest {
				bad[a.Ordinal] = true
			}
			continue
		}
		stored[a.Ordinal] = a.Digest
	}
	// Head rows against the chain and the store.
	recorded := make(map[uint64]bool)
	for _, r := range sorted {
		h, ok := r.(HeadRowI)
		if !ok {
			continue
		}
		covered, digest, isHead := h.Head()
		if !isHead {
			continue
		}
		recorded[covered] = true
		if !chainHolds(covered, digest) || stored[covered] != digest {
			bad[r.Ordinal()] = true
		}
	}
	// Anchored heads against the chain and the head rows.
	for ordinal, digest := range stored {
		if !chainHolds(ordinal, digest) || !recorded[ordinal] {
			bad[ordinal] = true
		}
	}
	for ordinal := range bad {
		broken = append(broken, ordinal)
	}
	slices.Sort(broken)
	return
}
