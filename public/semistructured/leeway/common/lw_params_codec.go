package common

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

// ParamsCodecE names the encoding of a section's membership params blobs —
// the high-cardinality half of an attribute locator on the hp, lp, mrhp and
// mvhp channels. The params column is declared as opaque bytes, so the wire
// format states nothing about it; a section declares its codec as a use-aspect
// (ADR-0233), read back with DeclaredParamsCodec. Undeclared is a fact about
// the schema, not a third codec: a reader that needs the encoding refuses an
// undeclared section rather than guessing.
type ParamsCodecE uint8

const (
	// ParamsCodecUndeclared: the section states no codec.
	ParamsCodecUndeclared ParamsCodecE = iota
	// ParamsCodecFixedWidthHex: membership.{Append,Encode,Decode}Params —
	// fixed-width lowercase hex, four digits per index, '.'-separated, in
	// path order, the empty tuple as the empty blob.
	ParamsCodecFixedWidthHex
)

// AllParamsCodecs lists the declarable codecs.
var AllParamsCodecs = []ParamsCodecE{ParamsCodecFixedWidthHex}

// MembershipSpecParamsBearing is the union of the channels that carry a
// params column; a codec declaration is meaningful only beside one of them.
const MembershipSpecParamsBearing = MembershipSpecHighCardRefParametrized |
	MembershipSpecLowCardRefParametrized |
	MembershipSpecMixedLowCardRefHighCardParameters |
	MembershipSpecMixedLowCardVerbatimHighCardParameters

func (inst ParamsCodecE) String() string {
	switch inst {
	case ParamsCodecUndeclared:
		return "undeclared"
	case ParamsCodecFixedWidthHex:
		return "fixed-width-hex"
	}
	return "<invalid ParamsCodecE>"
}

// Aspect is the section use-aspect that declares the codec. ok is false for
// ParamsCodecUndeclared, which no aspect spells.
func (inst ParamsCodecE) Aspect() (a useaspects.AspectE, ok bool) {
	ok = true
	switch inst {
	case ParamsCodecFixedWidthHex:
		a = useaspects.AspectSectionParamsFixedWidthHex
	default:
		ok = false
	}
	return
}

// GetParamsCodecByAspect is the inverse of ParamsCodecE.Aspect. ok is false
// for every aspect that is not a params-codec declaration.
func GetParamsCodecByAspect(a useaspects.AspectE) (codec ParamsCodecE, ok bool) {
	ok = true
	switch a {
	case useaspects.AspectSectionParamsFixedWidthHex:
		codec = ParamsCodecFixedWidthHex
	default:
		ok = false
	}
	return
}

// DeclaredParamsCodec reads a section's use-aspect set for its params-codec
// declaration (ADR-0233). The family is exclusive, so at most one member is
// present in a valid set; ParamsCodecUndeclared when none is.
func DeclaredParamsCodec(aspects useaspects.AspectSet) (codec ParamsCodecE) {
	for _, c := range AllParamsCodecs {
		if a, ok := c.Aspect(); ok && aspects.Contains(a) {
			return c
		}
	}
	return ParamsCodecUndeclared
}
