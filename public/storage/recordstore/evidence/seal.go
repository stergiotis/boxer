package evidence

import (
	"slices"
	"strconv"
	"strings"
)

// SealedRow is one row a seal names: its key and its row digest, the
// digest of its canonical item (Scheme.RowDigest).
type SealedRow struct {
	Id     uint64
	Digest string
}

// Seal is one seal of a sealed store (ADR-0327 (proposed) §SD5): the row
// it is, the seal it follows in its stream (zero for the stream's first),
// its digest, and the rows it names. A store's seal component converts to
// it.
type Seal struct {
	Id     uint64
	Prev   uint64
	Digest string
	Rows   []SealedRow
}

// SealItem is what a seal's digest is over: the rows sorted by id, one
// line each, `id digest`. Rows that repeat an id collapse to one line, so
// a seal cannot name a row twice.
func SealItem(rows []SealedRow) (item []byte) {
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b SealedRow) int {
		if a.Id != b.Id {
			if a.Id < b.Id {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Digest, b.Digest)
	})
	sorted = slices.Compact(sorted)
	var sb strings.Builder
	for _, r := range sorted {
		sb.WriteString(strconv.FormatUint(r.Id, 10))
		sb.WriteByte(' ')
		sb.WriteString(r.Digest)
		sb.WriteByte('\n')
	}
	return []byte(sb.String())
}

// SealDigest is the digest of a seal that follows the seal at prevDigest
// — the seed for a stream's first — and names rows.
func (inst Scheme) SealDigest(prevDigest string, rows []SealedRow) string {
	return inst.ChainDigest(prevDigest, SealItem(rows))
}

// SealBreakE says why a seal stream breaks.
type SealBreakE uint8

const (
	// SealBreakNone is a stream that holds.
	SealBreakNone SealBreakE = iota
	// SealBreakOrder is a fork: a second seal following the same seal.
	SealBreakOrder
	// SealBreakPrev is a seal that follows no seal of the stream.
	SealBreakPrev
	// SealBreakDigest is a seal whose digest is not over its rows and its
	// predecessor.
	SealBreakDigest
	// SealBreakRowMissing is a named row that is not on the store.
	SealBreakRowMissing
	// SealBreakRowDigest is a named row whose digest on the store is not
	// the one the seal names.
	SealBreakRowDigest
	// SealBreakRowTwice is a row two seals name.
	SealBreakRowTwice
)

var sealBreakNames = []string{"none", "order", "prev", "digest", "row-missing", "row-digest", "row-twice"}

func (inst SealBreakE) String() (s string) {
	if int(inst) < len(sealBreakNames) {
		return sealBreakNames[inst]
	}
	return "unknown"
}

// SealBreak is where a seal stream breaks: the seal, why, and the row
// when the reason names one.
type SealBreak struct {
	Seal uint64
	Why  SealBreakE
	Row  uint64
}

// VerifySeals is law L34 over one seal stream: seals, in any order, that
// chain by Prev from the stream's first (Prev zero) under seed; present
// the store's rows other than seals, by id, with their row digests. It
// returns the first break walking the chain, or a zero SealBreak when the
// stream holds, and the present rows no seal names — the gaps a sealed
// store reports, each either a row that never landed with its seal or a
// row something else wrote.
func VerifySeals(scheme Scheme, seed string, seals []Seal, present map[uint64]string) (broken SealBreak, unnamed []uint64) {
	byPrev := make(map[uint64]Seal, len(seals))
	for _, s := range seals {
		if first, forked := byPrev[s.Prev]; forked {
			return SealBreak{Seal: max(first.Id, s.Id), Why: SealBreakOrder}, nil
		}
		byPrev[s.Prev] = s
	}
	named := make(map[uint64]bool, len(present))
	reached := make(map[uint64]bool, len(seals))
	prevId, prevDigest := uint64(0), seed
	for s, ok := byPrev[prevId]; ok && !reached[s.Id]; s, ok = byPrev[prevId] {
		reached[s.Id] = true
		if s.Digest != scheme.SealDigest(prevDigest, s.Rows) {
			return SealBreak{Seal: s.Id, Why: SealBreakDigest}, nil
		}
		for _, r := range s.Rows {
			got, ok := present[r.Id]
			switch {
			case !ok:
				return SealBreak{Seal: s.Id, Why: SealBreakRowMissing, Row: r.Id}, nil
			case got != r.Digest:
				return SealBreak{Seal: s.Id, Why: SealBreakRowDigest, Row: r.Id}, nil
			case named[r.Id]:
				return SealBreak{Seal: s.Id, Why: SealBreakRowTwice, Row: r.Id}, nil
			}
			named[r.Id] = true
		}
		prevId, prevDigest = s.Id, s.Digest
	}
	if len(reached) < len(seals) {
		// An orphan: a seal the walk from the first never reached, the
		// lowest id among them.
		orphan := uint64(0)
		for _, s := range seals {
			if !reached[s.Id] && (orphan == 0 || s.Id < orphan) {
				orphan = s.Id
			}
		}
		return SealBreak{Seal: orphan, Why: SealBreakPrev}, nil
	}
	for id := range present {
		if !named[id] {
			unnamed = append(unnamed, id)
		}
	}
	slices.Sort(unnamed)
	return
}
