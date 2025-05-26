package encodingaspects

import "slices"

const (
	DataAspectNone                         DataAspectE = 0
	DataAspectIntraRecordLowCardinality    DataAspectE = 1
	DataAspectInterRecordLowCardinality    DataAspectE = 2
	DataAspectUltraLightGeneralCompression DataAspectE = 3
	DataAspectLightGeneralCompression      DataAspectE = 4
	DataAspectHeavyGeneralCompression      DataAspectE = 5
	DataAspectUltraHeavyGeneralCompression DataAspectE = 6
	DataAspectDeltaEncoding                DataAspectE = 7
	DataAspectDoubleDeltaEncoding          DataAspectE = 8
)

var MaxDataAspectExcl = slices.Max(AllDataAspects) + 1

var AllDataAspects = []DataAspectE{
	DataAspectNone,
	DataAspectIntraRecordLowCardinality,
	DataAspectInterRecordLowCardinality,
	DataAspectUltraLightGeneralCompression,
	DataAspectLightGeneralCompression,
	DataAspectHeavyGeneralCompression,
	DataAspectUltraHeavyGeneralCompression,
	DataAspectDeltaEncoding,
	DataAspectDoubleDeltaEncoding,
}

const InvalidDataAspectEnumValueString = "<invalid DataAspectE>"

func (inst DataAspectE) IsValid() bool {
	return inst < MaxDataAspectExcl
}
func (inst DataAspectE) String() string {
	switch inst {
	case DataAspectNone:
		return "none"
	case DataAspectIntraRecordLowCardinality:
		return "intra-record-low-cardinality"
	case DataAspectInterRecordLowCardinality:
		return "inter-record-low-cardinality"
	case DataAspectUltraLightGeneralCompression:
		return "ultra-light-general-compression"
	case DataAspectLightGeneralCompression:
		return "light-general-compression"
	case DataAspectHeavyGeneralCompression:
		return "heavy-general-compression"
	case DataAspectUltraHeavyGeneralCompression:
		return "ultra-heavy-general-compression"
	case DataAspectDeltaEncoding:
		return "delta-encoding"
	case DataAspectDoubleDeltaEncoding:
		return "double-delta-encoding"
	}
	return InvalidDataAspectEnumValueString
}
