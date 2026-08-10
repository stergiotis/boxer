// Package aspectcodec implements the v2 aspect-segment codec (ADR-0182 SD1),
// shared by the three aspect vocabularies.
//
// A segment is the ascending list of a set's aspect indices, one alphabet
// char per index at alphabet[i+1]. The empty set is the empty string; the
// legacy v1 empty marker "0" is accepted on decode but never produced.
// '0' (alphabet position 0) never occurs in a v2 segment; 'z' (position 61)
// is an escape prefix, each occurrence adding +60 to the digit it precedes,
// chainable. Single chars therefore cover indices 0..59. Canonical form is
// strictly ascending indices; decoders reject anything else, per element.
package aspectcodec

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const escapeByte = 'z'
const indicesPerLevel = 60

const legacyEmptySegment = "0"

var ErrInvalidEncoding = eh.Errorf("encoding is wrong")
var ErrEmptySet = eh.Errorf("encoding contains empty set")

type IndexI interface {
	~uint8
}

// digitPos returns the alphabet position of c, or -1 when c is not an
// alphabet char. Position 0 is the reserved '0', position 61 the escape.
func digitPos(c byte) (p int) {
	switch {
	case c >= '0' && c <= '9':
		p = int(c - '0')
	case c >= 'A' && c <= 'Z':
		p = int(c-'A') + 10
	case c >= 'a' && c <= 'z':
		p = int(c-'a') + 36
	default:
		p = -1
	}
	return
}

func appendIndex(b *strings.Builder, idx int) {
	for k := 0; k < idx/indicesPerLevel; k++ {
		b.WriteByte(escapeByte)
	}
	b.WriteByte(alphabet[idx%indicesPerLevel+1])
}

// Encode returns the canonical v2 segment for the given aspects. Input may
// be unsorted and may contain duplicates; the result is their set. Every
// value of an ~uint8 index type is encodable, so Encode is total.
func Encode[E IndexI](indices []E) (seg string) {
	if len(indices) == 0 {
		return ""
	}
	var present [256]bool
	for _, i := range indices {
		present[i] = true
	}
	var b strings.Builder
	for i := range present {
		if present[i] {
			appendIndex(&b, i)
		}
	}
	seg = b.String()
	return
}

// decodeRaw parses a segment into its strictly ascending index list without
// bounding indices to any vocabulary. It is the single structural
// authority: bad chars, the reserved '0' (except as the whole legacy empty
// segment), duplicates, non-ascending order and dangling escapes all fail.
func decodeRaw(seg string) (indices []int, err error) {
	if seg == "" || seg == legacyEmptySegment {
		return
	}
	indices = make([]int, 0, len(seg))
	level := 0
	prev := -1
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if c == escapeByte {
			level++
			continue
		}
		p := digitPos(c)
		if p < 0 {
			err = eb.Build().Str("segment", seg).Int("position", i).Errorf("char is not part of the codec alphabet: %w", ErrInvalidEncoding)
			indices = nil
			return
		}
		if p == 0 {
			err = eb.Build().Str("segment", seg).Int("position", i).Errorf("reserved char '0' inside a segment: %w", ErrInvalidEncoding)
			indices = nil
			return
		}
		idx := level*indicesPerLevel + (p - 1)
		level = 0
		if idx <= prev {
			err = eb.Build().Str("segment", seg).Int("position", i).Int("index", idx).Int("previousIndex", prev).Errorf("indices must be strictly ascending: %w", ErrInvalidEncoding)
			indices = nil
			return
		}
		prev = idx
		indices = append(indices, idx)
	}
	if level > 0 {
		err = eb.Build().Str("segment", seg).Errorf("dangling escape at end of segment: %w", ErrInvalidEncoding)
		indices = nil
		return
	}
	return
}

// Decode splits a structurally valid segment into the indices known to a
// vocabulary of size maxExcl and a count of unknown (too-new) indices.
// Unknown elements are individually skippable; they never poison the known
// part.
func Decode[E IndexI](seg string, maxExcl E) (known []E, unknownCount int, err error) {
	var raw []int
	raw, err = decodeRaw(seg)
	if err != nil {
		return
	}
	if len(raw) == 0 {
		return
	}
	known = make([]E, 0, len(raw))
	for _, idx := range raw {
		if idx < int(maxExcl) {
			known = append(known, E(idx))
		} else {
			unknownCount++
		}
	}
	return
}

// Contains reports whether the segment's set contains target. It returns
// false on structurally invalid input.
func Contains[E IndexI](seg string, target E) (has bool) {
	if seg == "" || seg == legacyEmptySegment {
		return
	}
	level := 0
	prev := -1
	want := int(target)
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if c == escapeByte {
			level++
			continue
		}
		p := digitPos(c)
		if p <= 0 {
			return false
		}
		idx := level*indicesPerLevel + (p - 1)
		level = 0
		if idx <= prev {
			return false
		}
		prev = idx
		if idx == want {
			// keep scanning: the contract is false on any structural
			// invalidity, even after a hit
			has = true
		}
	}
	if level > 0 {
		has = false
	}
	return
}

// Union merges two segments into one canonical segment. Indices unknown to
// the running binary are preserved, never dropped.
func Union(a string, b string) (seg string, err error) {
	var ra, rb []int
	ra, err = decodeRaw(a)
	if err != nil {
		return
	}
	rb, err = decodeRaw(b)
	if err != nil {
		return
	}
	if len(ra) == 0 && len(rb) == 0 {
		return "", nil
	}
	var sb strings.Builder
	i, j := 0, 0
	for i < len(ra) || j < len(rb) {
		var idx int
		switch {
		case j >= len(rb) || (i < len(ra) && ra[i] < rb[j]):
			idx = ra[i]
			i++
		case i >= len(ra) || rb[j] < ra[i]:
			idx = rb[j]
			j++
		default:
			idx = ra[i]
			i++
			j++
		}
		appendIndex(&sb, idx)
	}
	seg = sb.String()
	return
}

// Count returns the number of aspects in the segment's set.
func Count(seg string) (n int, err error) {
	var raw []int
	raw, err = decodeRaw(seg)
	if err != nil {
		return
	}
	n = len(raw)
	return
}

// MaxIndex returns the highest index in the segment's set; ErrEmptySet on
// an empty set.
func MaxIndex(seg string) (max int, err error) {
	var raw []int
	raw, err = decodeRaw(seg)
	if err != nil {
		return
	}
	if len(raw) == 0 {
		err = ErrEmptySet
		return
	}
	max = raw[len(raw)-1]
	return
}

// Validate checks structural validity and canonicality.
func Validate(seg string) (err error) {
	_, err = decodeRaw(seg)
	return
}

// IsEmpty reports whether the segment denotes the empty set ("" or the
// legacy v1 marker "0").
func IsEmpty(seg string) (empty bool) {
	return seg == "" || seg == legacyEmptySegment
}

// Family groups related aspects for documentation and validation
// (ADR-0182 SD3). An exclusive family admits at most one member per set.
type Family[E IndexI] struct {
	Name      string
	Members   []E
	Exclusive bool
}

// FirstExclusivityViolation returns the name of the first exclusive family
// with more than one member present (per contains), or "" when none is
// violated.
func FirstExclusivityViolation[E IndexI](families []Family[E], contains func(E) bool) (name string) {
	for _, f := range families {
		if !f.Exclusive {
			continue
		}
		n := 0
		for _, m := range f.Members {
			if contains(m) {
				n++
			}
		}
		if n > 1 {
			name = f.Name
			return
		}
	}
	return
}

// KeepFirstPerExclusiveFamily drops all but the first-encountered member of
// each exclusive family, preserving input order; aspects outside any
// exclusive family pass through.
func KeepFirstPerExclusiveFamily[E IndexI](families []Family[E], aspects []E) (out []E) {
	famOf := make(map[E]int, 16)
	for fi, f := range families {
		if !f.Exclusive {
			continue
		}
		for _, m := range f.Members {
			famOf[m] = fi
		}
	}
	seen := make(map[int]bool, 8)
	out = make([]E, 0, len(aspects))
	for _, a := range aspects {
		if fi, in := famOf[a]; in {
			if seen[fi] {
				continue
			}
			seen[fi] = true
		}
		out = append(out, a)
	}
	return
}
