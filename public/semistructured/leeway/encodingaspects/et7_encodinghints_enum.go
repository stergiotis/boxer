package encodingaspects

import "slices"

const (
	AspectNone                          AspectE = 0
	AspectIntraRecordLowCardinality     AspectE = 1
	AspectInterRecordLowCardinality     AspectE = 2
	AspectUltraLightGeneralCompression  AspectE = 3
	AspectLightGeneralCompression       AspectE = 4
	AspectHeavyGeneralCompression       AspectE = 5
	AspectUltraHeavyGeneralCompression  AspectE = 6
	AspectDeltaEncoding                 AspectE = 7
	AspectDoubleDeltaEncoding           AspectE = 8
	AspectUltraLightSlowlyChangingFloat AspectE = 9
	AspectLightSlowlyChangingFloat      AspectE = 10
	AspectHeavySlowlyChangingFloat      AspectE = 11
	AspectUltraHeavySlowlyChangingFloat AspectE = 12
	AspectLightBiasSmallInteger         AspectE = 13
	AspectHeavyBiasSmallInteger         AspectE = 14
)

var MaxAspectExcl = slices.Max(AllAspects) + 1

var AllAspects = []AspectE{
	AspectNone,
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
}

const InvalidAspectEnumValueString = "<invalid AspectE>"

func (inst AspectE) IsValid() bool {
	return inst < MaxAspectExcl
}
func (inst AspectE) String() string {
	switch inst {
	case AspectNone:
		return "none"
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
	}
	return InvalidAspectEnumValueString
}
