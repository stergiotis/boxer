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
	AspectVectorValue                      AspectE = 5
	AspectCanonicalizedValue               AspectE = 6
	AspectApplicationLevelEncrypted        AspectE = 7
	AspectHumanReadable                    AspectE = 8
	AspectMachineReadable                  AspectE = 9
	AspectUltraShortLifespan               AspectE = 10
	AspectShortLifespan                    AspectE = 11
	AspectMediumLifespan                   AspectE = 12
	AspectLongLifespan                     AspectE = 13
	AspectUltraLongLifespan                AspectE = 14
	AspectJsonScalar                       AspectE = 15
	AspectJsonArray                        AspectE = 16
	AspectJsonObject                       AspectE = 17
	AspectJson                             AspectE = 18
	AspectCborScalar                       AspectE = 19
	AspectCborArray                        AspectE = 20
	AspectCborMap                          AspectE = 21
	AspectCbor                             AspectE = 22
	AspectUrl                              AspectE = 23 // follow the WHATWG recommendation to forget URI and use URL (see https://url.spec.whatwg.org/#goals)
	AspectFeature                          AspectE = 24
	AspectFeatureOneHot                    AspectE = 25
	AspectFeatureScalingStandardN01        AspectE = 26
	AspectFeatureScalingMinMax01           AspectE = 27
	AspectFeatureScalingRobust01           AspectE = 28
	AspectFeatureBinarized                 AspectE = 29
	AspectFeatureOrdinal                   AspectE = 30
	AspectFeatureLabel                     AspectE = 31
	AspectIdNaturalKey                     AspectE = 32
	AspectIdSurrogateKey                   AspectE = 33
	AspectIdDurableSuperNaturalKey         AspectE = 34
	AspectIdContentAddressableKey          AspectE = 35
	AspectTextUnicodeNormalizedNfd         AspectE = 36 // Normalization Form Canonical Decomposition
	AspectTextUnicodeNormalizedNfc         AspectE = 37 // Normalization Form Canonical Composition
	AspectTextUnicodeNormalizedNfkd        AspectE = 38 // Normalization Form Compatibility Decomposition
	AspectTextUnicodeNormalizedNfkc        AspectE = 39 // Normalization Form Compatibility Composition
	AspectTextUnicodeCaseFolded            AspectE = 40 // Normalization Form Compatibility Composition
	AspectTextUnicodeCaseInsensitive       AspectE = 41
	AspectTextUnicodeLocaleSensitive       AspectE = 42
	AspectTextUnicodeMayBeBidi             AspectE = 43
	AspectHumanGenerated                   AspectE = 44
	AspectMachineGenerate                  AspectE = 45
	AspectBinaryCodedDecimal               AspectE = 46 // BCD see https://en.wikipedia.org/wiki/Binary-coded_decimal, note that there are many incompatible encodings
	AspectReflectedBinaryCode              AspectE = 47 // see https://en.wikipedia.org/wiki/Gray_code
	AspectTrinaryLogic                     AspectE = 48 // see https://en.wikipedia.org/wiki/Three-valued_logic
	AspectGraphVertex                      AspectE = 49
	AspectGraphEdge                        AspectE = 50
	AspectHyperGraphEdge                   AspectE = 51
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
	AspectApplicationLevelEncrypted,
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
	case AspectApplicationLevelEncrypted:
		return "application-level-encrypted"
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
	}
	return InvalidAspectEnumValueString
}
func (inst AspectE) Value() uint8 {
	return uint8(inst)
}
