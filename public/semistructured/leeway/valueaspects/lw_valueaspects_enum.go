package valueaspects

import (
	"slices"
)

const (
	AspectNone                             AspectE = 0
	AspectScaleOfMeasurementNominal        AspectE = 1
	AspectScaleOfMeasurementOrdinal        AspectE = 2
	AspectScaleOfMeasurementMetricInterval AspectE = 3
	AspectScaleOfMeasurementMetricRatio    AspectE = 4
	AspectScaleOfMeasurementCategorial     AspectE = 5
	AspectVectorValue                      AspectE = 6
	AspectCanonicalizedValue               AspectE = 7
	AspectApplicationLevelEncryption       AspectE = 8
	AspectApplicationLevelCompression      AspectE = 9
	AspectHumanReadable                    AspectE = 10
	AspectMachineReadable                  AspectE = 11
	AspectUltraShortLifespan               AspectE = 12
	AspectShortLifespan                    AspectE = 13
	AspectMediumLifespan                   AspectE = 14
	AspectLongLifespan                     AspectE = 15
	AspectUltraLongLifespan                AspectE = 16
	AspectJsonScalar                       AspectE = 17
	AspectJsonArray                        AspectE = 18
	AspectJsonObject                       AspectE = 19
	AspectJson                             AspectE = 20
	AspectCborScalar                       AspectE = 21
	AspectCborArray                        AspectE = 22
	AspectCborMap                          AspectE = 23
	AspectCbor                             AspectE = 24
	AspectUrl                              AspectE = 25 // follow the WHATWG recommendation to forget URI and use URL (see https://url.spec.whatwg.org/#goals)
	AspectFeature                          AspectE = 26
	AspectFeatureOneHot                    AspectE = 27
	AspectFeatureScalingStandardN01        AspectE = 28
	AspectFeatureScalingMinMax01           AspectE = 29
	AspectFeatureScalingRobust01           AspectE = 30
	AspectFeatureBinarized                 AspectE = 31
	AspectFeatureOrdinal                   AspectE = 32
	AspectFeatureLabel                     AspectE = 33
	AspectMachineLearningEmbedding         AspectE = 34
	AspectIdNaturalKey                     AspectE = 35
	AspectIdSurrogateKey                   AspectE = 36
	AspectIdDurableSuperNaturalKey         AspectE = 37
	AspectIdContentAddressableKey          AspectE = 38
	AspectTextUnicodeNormalizedNfd         AspectE = 39 // Normalization Form Canonical Decomposition
	AspectTextUnicodeNormalizedNfc         AspectE = 40 // Normalization Form Canonical Composition
	AspectTextUnicodeNormalizedNfkd        AspectE = 41 // Normalization Form Compatibility Decomposition
	AspectTextUnicodeNormalizedNfkc        AspectE = 42 // Normalization Form Compatibility Composition
	AspectTextUnicodeCaseFolded            AspectE = 43 // Normalization Form Compatibility Composition
	AspectTextUnicodeCaseInsensitive       AspectE = 44
	AspectTextUnicodeLocaleSensitive       AspectE = 45
	AspectTextUnicodeMayBeBidi             AspectE = 46
	AspectHumanGenerated                   AspectE = 47
	AspectMachineGenerate                  AspectE = 48
	AspectBinaryCodedDecimal               AspectE = 49 // BCD see https://en.wikipedia.org/wiki/Binary-coded_decimal, note that there are many incompatible encodings
	AspectReflectedBinaryCode              AspectE = 50 // see https://en.wikipedia.org/wiki/Gray_code
	AspectTrinaryLogic                     AspectE = 51 // see https://en.wikipedia.org/wiki/Three-valued_logic
	AspectGraphVertex                      AspectE = 52
	AspectGraphEdge                        AspectE = 53
	AspectHyperGraphEdge                   AspectE = 54
	AspectAnonymized                       AspectE = 55
	AspectMandatory                        AspectE = 56
	AspectOptional                         AspectE = 57
	AspectEmulatedMembershipVerbatim       AspectE = 58
	AspectEmulatedMembershipRef            AspectE = 59
	AspectEmulatedMembershipParams         AspectE = 60
	AspectEmulatedMembershipRefWithParams  AspectE = 61
)

var MaxAspectExcl = slices.Max(AllAspects) + 1

var AllAspects = []AspectE{
	AspectNone,
	AspectScaleOfMeasurementNominal,
	AspectScaleOfMeasurementOrdinal,
	AspectScaleOfMeasurementMetricInterval,
	AspectScaleOfMeasurementMetricRatio,
	AspectVectorValue,
	AspectCanonicalizedValue,
	AspectApplicationLevelEncryption,
	AspectApplicationLevelCompression,
	AspectHumanReadable,
	AspectMachineReadable,
	AspectUltraShortLifespan,
	AspectShortLifespan,
	AspectMediumLifespan,
	AspectLongLifespan,
	AspectUltraLongLifespan,
	AspectJsonScalar,
	AspectJsonArray,
	AspectJsonObject,
	AspectJson,
	AspectCborScalar,
	AspectCborArray,
	AspectCborMap,
	AspectCbor,
	AspectUrl,
	AspectFeature,
	AspectFeatureOneHot,
	AspectFeatureScalingStandardN01,
	AspectFeatureScalingMinMax01,
	AspectFeatureScalingRobust01,
	AspectFeatureBinarized,
	AspectFeatureOrdinal,
	AspectFeatureLabel,
	AspectMachineLearningEmbedding,
	AspectIdNaturalKey,
	AspectIdSurrogateKey,
	AspectIdDurableSuperNaturalKey,
	AspectIdContentAddressableKey,
	AspectTextUnicodeNormalizedNfd,
	AspectTextUnicodeNormalizedNfc,
	AspectTextUnicodeNormalizedNfkd,
	AspectTextUnicodeNormalizedNfkc,
	AspectTextUnicodeCaseFolded,
	AspectTextUnicodeCaseInsensitive,
	AspectTextUnicodeLocaleSensitive,
	AspectTextUnicodeMayBeBidi,
	AspectHumanGenerated,
	AspectMachineGenerate,
	AspectBinaryCodedDecimal,
	AspectReflectedBinaryCode,
	AspectTrinaryLogic,
	AspectGraphVertex,
	AspectGraphEdge,
	AspectHyperGraphEdge,
	AspectAnonymized,
	AspectMandatory,
	AspectOptional,
	AspectEmulatedMembershipVerbatim,
	AspectEmulatedMembershipRef,
	AspectEmulatedMembershipParams,
	AspectEmulatedMembershipRefWithParams,
}

const InvalidAspectEnumValueString = "<invalid AspectE>"

func (inst AspectE) IsValid() bool {
	return inst < MaxAspectExcl
}
func (inst AspectE) String() string {
	switch inst {
	case AspectNone:
		return "none"
	case AspectScaleOfMeasurementNominal:
		return "scale-of-measurement-nominal"
	case AspectScaleOfMeasurementOrdinal:
		return "scale-of-measurement-ordinal"
	case AspectScaleOfMeasurementMetricInterval:
		return "scale-of-measurement-metric-interval"
	case AspectScaleOfMeasurementMetricRatio:
		return "scale-of-measurement-metric-ratio"
	case AspectVectorValue:
		return "vector-value"
	case AspectCanonicalizedValue:
		return "canonicalized-value"
	case AspectApplicationLevelEncryption:
		return "application-level-encryption"
	case AspectApplicationLevelCompression:
		return "application-level-compression"
	case AspectHumanReadable:
		return "human-readable"
	case AspectMachineReadable:
		return "machine-readable"
	case AspectUltraShortLifespan:
		return "ultra-short-lifespan"
	case AspectShortLifespan:
		return "short-lifespan"
	case AspectMediumLifespan:
		return "medium-lifespan"
	case AspectLongLifespan:
		return "long-lifespan"
	case AspectUltraLongLifespan:
		return "ultra-long-lifespan"
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
	case AspectUrl:
		return "url"
	case AspectFeature:
		return "feature"
	case AspectFeatureOneHot:
		return "feature-one-hot"
	case AspectFeatureScalingStandardN01:
		return "feature-scaling-standard-n01"
	case AspectFeatureScalingMinMax01:
		return "feature-scaling-min-max01"
	case AspectFeatureScalingRobust01:
		return "feature-scaling-robust01"
	case AspectFeatureBinarized:
		return "feature-binarized"
	case AspectFeatureOrdinal:
		return "feature-ordinal"
	case AspectFeatureLabel:
		return "feature-label"
	case AspectMachineLearningEmbedding:
		return "machine-learning-embedding"
	case AspectIdNaturalKey:
		return "id-natural-key"
	case AspectIdSurrogateKey:
		return "id-surrogate-key"
	case AspectIdDurableSuperNaturalKey:
		return "id-durable-super-natural-key"
	case AspectIdContentAddressableKey:
		return "id-content-addressable-key"
	case AspectTextUnicodeNormalizedNfd:
		return "text-unicode-normalized-nfd"
	case AspectTextUnicodeNormalizedNfc:
		return "text-unicode-normalized-nfc"
	case AspectTextUnicodeNormalizedNfkd:
		return "text-unicode-normalized-nfkd"
	case AspectTextUnicodeNormalizedNfkc:
		return "text-unicode-normalized-nfkc"
	case AspectTextUnicodeCaseFolded:
		return "text-unicode-case-folded"
	case AspectTextUnicodeCaseInsensitive:
		return "text-unicode-case-insensitive"
	case AspectTextUnicodeLocaleSensitive:
		return "text-unicode-locale-sensitive"
	case AspectTextUnicodeMayBeBidi:
		return "text-unicode-maybe-bidi"
	case AspectHumanGenerated:
		return "human-generated"
	case AspectMachineGenerate:
		return "machine-generated"
	case AspectBinaryCodedDecimal:
		return "binary-coded-decimal"
	case AspectReflectedBinaryCode:
		return "reflected-binary-code"
	case AspectTrinaryLogic:
		return "trinary-logic"
	case AspectGraphVertex:
		return "graph-vertex"
	case AspectGraphEdge:
		return "graph-edge"
	case AspectHyperGraphEdge:
		return "hyper-graph-edge"
	case AspectAnonymized:
		return "anonymized"
	case AspectMandatory:
		return "mandatory"
	case AspectOptional:
		return "optional"
	case AspectEmulatedMembershipVerbatim:
		return "emulated-membership-verbatim"
	case AspectEmulatedMembershipRef:
		return "emulated-membership-ref"
	case AspectEmulatedMembershipParams:
		return "emulated-membership-params"
	case AspectEmulatedMembershipRefWithParams:
		return "emulated-membership-ref-with-params"
	}
	return InvalidAspectEnumValueString
}
func (inst AspectE) Value() uint8 {
	return uint8(inst)
}
