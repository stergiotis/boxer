package identifier

import (
	"math/bits"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
)

// Fibonacci-coded tags (ADR-0106 SD1/SD2): a TaggedId is tagBits | body. The
// tag bits are the fibonacci code of tagValue-1 — Zeckendorf representation,
// reversed, terminated by the "11" comma — placed MSB-aligned; the body
// occupies the remaining low bits. The code is self-delimiting (scanning from
// the MSB, the first adjacent 11 pair is always the tag's comma) and
// prefix-free (no tag's code is a prefix of another's), so ids need no
// out-of-band width to split and distinct tags can never collide.

// MaxTagValue is the largest usable tag value. Every non-zero uint32 tag
// value is encodable (code widths 2..47, leaving at least 17 body bits);
// the zero TagValue is reserved as invalid, mirroring the reserved zero body.
const MaxTagValue TagValue = ^TagValue(0)

// splitWidth returns the full tag width of v in bits, including the trailing
// comma bit, or 0 when v carries no "11" pair and is therefore not a tagged
// id. This is the Go half of the ADR-0106 SD2 split contract; the SQL half
// computes the same value as 66 - popcount(smeared pairs).
func splitWidth(v uint64) (width int) {
	pairs := v & (v << 1)
	if pairs == 0 {
		return 0
	}
	return bits.LeadingZeros64(pairs) + 2
}

// tagMaskOfWidth covers the top w bits; w must be in [2, 64].
func tagMaskOfWidth(w int) (mask uint64) {
	return ^uint64(0) << (TotalIdWidth - w)
}

func bodyMaskOfWidth(w int) (mask uint64) {
	return ^tagMaskOfWidth(w)
}

func (inst TagValue) IsValid() bool {
	return inst != 0
}

// GetTag encodes the tag value as an MSB-aligned fibonacci code. The zero
// (invalid) TagValue yields the zero (invalid) IdTag.
func (inst TagValue) GetTag() (tag IdTag) {
	if inst == 0 {
		return 0
	}
	code, _ := fibonaccicode.EncodeFibonacciCode(uint64(inst) - 1)
	return IdTag(code)
}

// IsValid reports whether inst is a well-formed tag: it carries a comma and
// no stray bits below it.
func (inst IdTag) IsValid() bool {
	w := splitWidth(uint64(inst))
	return w != 0 && uint64(inst)&bodyMaskOfWidth(w) == 0
}

// GetTagWidth returns the tag's full width in bits, including the trailing
// fibonacci code comma bit; 0 for an invalid (comma-less) tag.
func (inst IdTag) GetTagWidth() (nBits int) {
	return splitWidth(uint64(inst))
}

// GetValue decodes the tag back to its TagValue. It returns 0 (the invalid
// TagValue) when inst carries no comma or decodes beyond the uint32 tag-value
// domain (possible only for raw bit patterns never produced by
// TagValue.GetTag). Bits below the comma are ignored, so a full TaggedId
// decodes to its tag's value directly.
func (inst IdTag) GetValue() (v TagValue) {
	n, ok := fibonaccicode.DecodeFibonacciCode(uint64(inst))
	if !ok || n+1 > uint64(MaxTagValue) {
		return 0
	}
	return TagValue(n + 1)
}

// GetMaxPossibleIdIncl returns the largest body value that fits below this
// tag (its body mask); 0 for an invalid tag.
func (inst IdTag) GetMaxPossibleIdIncl() (maxId UntaggedId) {
	w := splitWidth(uint64(inst))
	if w == 0 {
		return 0
	}
	return UntaggedId(bodyMaskOfWidth(w))
}

func (inst IdTag) ComposeId(id UntaggedId) (taggedId TaggedId) {
	return id.AddTag(inst)
}

// SameTag reports whether id carries exactly this tag. It is a mask compare:
// for a known tag no per-id comma scan is needed.
func (inst IdTag) SameTag(id TaggedId) bool {
	w := splitWidth(uint64(inst))
	if w == 0 {
		return false
	}
	return uint64(id)&tagMaskOfWidth(w) == uint64(inst)
}

// AddTag composes a tagged id. It panics when t is not a valid tag or when
// the body has bits inside t's tag region — an oversized body would silently
// decode to a different tag, so the guard is always on (ADR-0106 SD1; the
// former ExtraChecks-only overlap test missed bodies that cleared the tag's
// set bits).
func (inst UntaggedId) AddTag(t IdTag) TaggedId {
	w := splitWidth(uint64(t))
	if w == 0 || uint64(t)&bodyMaskOfWidth(w) != 0 {
		log.Panic().Uint64("tag", uint64(t)).Msg("invalid tag")
	}
	if uint64(inst)&tagMaskOfWidth(w) != 0 {
		log.Panic().Uint64("tag", uint64(t)).Uint64("untagged", uint64(inst)).Int("tagWidth", w).Msg("untagged id does not fit below the tag")
	}
	return TaggedId(uint64(t) | uint64(inst))
}

// GetTagWidth returns the full width of the id's tag, including the trailing
// fibonacci code comma bit; 0 for an invalid (comma-less) id.
func (inst TaggedId) GetTagWidth() (nBits int) {
	return splitWidth(uint64(inst))
}

func (inst TaggedId) GetTagMask() (mask TaggedId) {
	w := splitWidth(uint64(inst))
	if w == 0 {
		return 0
	}
	return TaggedId(tagMaskOfWidth(w))
}

// Split separates the id into its tag and body. An invalid (comma-less) id
// yields (0, 0) — both parts invalid — rather than garbage.
func (inst TaggedId) Split() (tag IdTag, untaggedId UntaggedId) {
	w := splitWidth(uint64(inst))
	if w == 0 {
		return 0, 0
	}
	tm := tagMaskOfWidth(w)
	tag = IdTag(uint64(inst) & tm)
	untaggedId = UntaggedId(uint64(inst) &^ tm)
	return
}

func (inst TaggedId) GetTag() (tag IdTag) {
	tag, _ = inst.Split()
	return
}

func (inst TaggedId) RemoveTag() (untaggedId UntaggedId) {
	_, untaggedId = inst.Split()
	return
}

// IsValid reports whether the id carries a fibonacci comma (a structural
// check; it does not prove the id was minted by a generator).
func (inst TaggedId) IsValid() bool {
	return splitWidth(uint64(inst)) != 0
}

func (inst UntaggedId) IsValid() bool {
	return inst != 0
}

func (inst TaggedId) Value() uint64 {
	return uint64(inst)
}

func (inst UntaggedId) Value() uint64 {
	return uint64(inst)
}

func (inst TagValue) Value() uint32 {
	return uint32(inst)
}

func (inst IdTag) Value() uint64 {
	return uint64(inst)
}
