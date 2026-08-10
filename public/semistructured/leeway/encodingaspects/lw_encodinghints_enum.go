// Package encodingaspects is the closed vocabulary of encoding hints
// (ADR-0182). Every aspect maps to a real storage-encoding capability of a
// target technology (codec chains, native types); intensity scales (light …
// ultra-heavy) are advisory effort levels the technology mapping interprets.
// Numbering is the wire format (segments in physical column names):
// append-only between migration windows, family-grouped as of v2.
package encodingaspects

import "slices"

const (
	AspectIntraRecordLowCardinality AspectE = 0
	AspectInterRecordLowCardinality AspectE = 1

	// General-purpose compression effort (exclusive).

	AspectUltraLightGeneralCompression AspectE = 2
	AspectLightGeneralCompression      AspectE = 3
	AspectHeavyGeneralCompression      AspectE = 4
	AspectUltraHeavyGeneralCompression AspectE = 5

	AspectDeltaEncoding       AspectE = 6
	AspectDoubleDeltaEncoding AspectE = 7

	// Slowly-changing-float compression effort (exclusive).

	AspectUltraLightSlowlyChangingFloat AspectE = 8
	AspectLightSlowlyChangingFloat      AspectE = 9
	AspectHeavySlowlyChangingFloat      AspectE = 10
	AspectUltraHeavySlowlyChangingFloat AspectE = 11

	// Small-integer bias compression effort (exclusive).

	AspectLightBiasSmallInteger AspectE = 12
	AspectHeavyBiasSmallInteger AspectE = 13

	AspectSparse AspectE = 14

	// The Json*/Cbor* encoding aspects permit the ddl module to use a native
	// JSON/CBOR database type for the column. Deliberately distinct from the
	// equally named valueaspects family, which states that the value is a
	// JSON/CBOR string serialization.

	AspectJsonScalar AspectE = 15
	AspectJsonArray  AspectE = 16
	AspectJsonObject AspectE = 17
	AspectJson       AspectE = 18
	AspectCborScalar AspectE = 19
	AspectCborArray  AspectE = 20
	AspectCborMap    AspectE = 21
	AspectCbor       AspectE = 22
)

var MaxAspectExcl = slices.Max(AllAspects) + 1

var AllAspects = []AspectE{
	AspectIntraRecordLowCardinality,
	AspectInterRecordLowCardinality,
	AspectUltraLightGeneralCompression,
	AspectLightGeneralCompression,
	AspectHeavyGeneralCompression,
	AspectUltraHeavyGeneralCompression,
	AspectDeltaEncoding,
	AspectDoubleDeltaEncoding,
	AspectUltraLightSlowlyChangingFloat,
	AspectLightSlowlyChangingFloat,
	AspectHeavySlowlyChangingFloat,
	AspectUltraHeavySlowlyChangingFloat,
	AspectLightBiasSmallInteger,
	AspectHeavyBiasSmallInteger,
	AspectSparse,
	AspectJsonScalar,
	AspectJsonArray,
	AspectJsonObject,
	AspectJson,
	AspectCborScalar,
	AspectCborArray,
	AspectCborMap,
	AspectCbor,
}

const InvalidAspectEnumValueString = "<invalid AspectE>"

func (inst AspectE) IsValid() bool {
	return inst < MaxAspectExcl
}
func (inst AspectE) Value() uint8 {
	return uint8(inst)
}
func (inst AspectE) String() string {
	switch inst {
	case AspectIntraRecordLowCardinality:
		return "intra-record-low-cardinality"
	case AspectInterRecordLowCardinality:
		return "inter-record-low-cardinality"
	case AspectUltraLightGeneralCompression:
		return "ultra-light-general-compression"
	case AspectLightGeneralCompression:
		return "light-general-compression"
	case AspectHeavyGeneralCompression:
		return "heavy-general-compression"
	case AspectUltraHeavyGeneralCompression:
		return "ultra-heavy-general-compression"
	case AspectDeltaEncoding:
		return "delta-encoding"
	case AspectDoubleDeltaEncoding:
		return "double-delta-encoding"
	case AspectUltraLightSlowlyChangingFloat:
		return "ultra-light-slowly-changing-float"
	case AspectLightSlowlyChangingFloat:
		return "light-slowly-changing-float"
	case AspectHeavySlowlyChangingFloat:
		return "heavy-slowly-changing-float"
	case AspectUltraHeavySlowlyChangingFloat:
		return "ultra-heavy-slowly-changing-float"
	case AspectLightBiasSmallInteger:
		return "light-bias-small-integer"
	case AspectHeavyBiasSmallInteger:
		return "heavy-bias-small-integer"
	case AspectSparse:
		return "sparse"
	case AspectJsonScalar:
		return "json-scalar"
	case AspectJsonArray:
		return "json-array"
	case AspectJsonObject:
		return "json-object"
	case AspectJson:
		return "json"
	case AspectCborScalar:
		return "cbor-scalar"
	case AspectCborArray:
		return "cbor-array"
	case AspectCborMap:
		return "cbor-map"
	case AspectCbor:
		return "cbor"
	}
	return InvalidAspectEnumValueString
}
