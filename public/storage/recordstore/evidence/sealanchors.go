package evidence

import (
	"slices"
	"strings"
)

// OrderSeals walks a stream by predecessor from its first seal (Prev
// zero) and returns the seals in stream order, or where the walk breaks:
// a fork (SealBreakOrder) or a seal the walk never reaches (SealBreakPrev).
// It is the part of VerifySeals that needs no rows.
func OrderSeals(seals []Seal) (ordered []Seal, broken SealBreak) {
	byPrev := make(map[uint64]Seal, len(seals))
	for _, s := range seals {
		if first, forked := byPrev[s.Prev]; forked {
			return nil, SealBreak{Seal: max(first.Id, s.Id), Why: SealBreakOrder}
		}
		byPrev[s.Prev] = s
	}
	reached := make(map[uint64]bool, len(seals))
	ordered = make([]Seal, 0, len(seals))
	for s, ok := byPrev[0]; ok && !reached[s.Id]; s, ok = byPrev[s.Id] {
		reached[s.Id] = true
		ordered = append(ordered, s)
	}
	if len(ordered) < len(seals) {
		orphan := uint64(0)
		for _, s := range seals {
			if !reached[s.Id] && (orphan == 0 || s.Id < orphan) {
				orphan = s.Id
			}
		}
		return nil, SealBreak{Seal: orphan, Why: SealBreakPrev}
	}
	return ordered, SealBreak{}
}

// VerifySealAnchors is law L32 for a sealed store: an anchored head of a
// seal stream names the n-th seal by its ordinal n and must hold its
// digest, and a seal the store anchored must still be the n-th. Of
// several receipts for one ordinal the earliest counts, and a later one
// with another digest breaks the ordinal. ordered is the stream as
// OrderSeals returns it. It returns the ordinals that break, each once,
// in order.
func VerifySealAnchors(ordered []Seal, anchored []AnchoredHead) (broken []uint64) {
	byReceipt := slices.Clone(anchored)
	slices.SortStableFunc(byReceipt, func(a, b AnchoredHead) int {
		if c := a.ReceiptAt.Compare(b.ReceiptAt); c != 0 {
			return c
		}
		return strings.Compare(a.ReceiptId, b.ReceiptId)
	})
	bad := make(map[uint64]bool)
	stored := make(map[uint64]string, len(byReceipt))
	for _, a := range byReceipt {
		if first, seen := stored[a.Ordinal]; seen {
			if first != a.Digest {
				bad[a.Ordinal] = true
			}
			continue
		}
		stored[a.Ordinal] = a.Digest
	}
	for ordinal, digest := range stored {
		if ordinal == 0 || ordinal > uint64(len(ordered)) || ordered[ordinal-1].Digest != digest {
			bad[ordinal] = true
		}
	}
	for ordinal := range bad {
		broken = append(broken, ordinal)
	}
	slices.Sort(broken)
	return
}
