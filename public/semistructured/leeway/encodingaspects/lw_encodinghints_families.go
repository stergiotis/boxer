package encodingaspects

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/aspectcodec"
)

// Families is the registry of aspect families (ADR-0182 SD3): documentation
// of record and the source for exclusivity validation and, later, DQL
// authoring diagnostics. Exclusive families admit at most one member per set.
var Families = []aspectcodec.Family[AspectE]{
	{Name: "record-cardinality", Members: []AspectE{AspectIntraRecordLowCardinality, AspectInterRecordLowCardinality}, Exclusive: false},
	{Name: "general-compression", Members: []AspectE{AspectUltraLightGeneralCompression, AspectLightGeneralCompression, AspectHeavyGeneralCompression, AspectUltraHeavyGeneralCompression}, Exclusive: true},
	{Name: "delta-encoding", Members: []AspectE{AspectDeltaEncoding, AspectDoubleDeltaEncoding}, Exclusive: true},
	{Name: "slowly-changing-float", Members: []AspectE{AspectUltraLightSlowlyChangingFloat, AspectLightSlowlyChangingFloat, AspectHeavySlowlyChangingFloat, AspectUltraHeavySlowlyChangingFloat}, Exclusive: true},
	{Name: "bias-small-integer", Members: []AspectE{AspectLightBiasSmallInteger, AspectHeavyBiasSmallInteger}, Exclusive: true},
	{Name: "json-native", Members: []AspectE{AspectJsonScalar, AspectJsonArray, AspectJsonObject, AspectJson}, Exclusive: false},
	{Name: "cbor-native", Members: []AspectE{AspectCborScalar, AspectCborArray, AspectCborMap, AspectCbor}, Exclusive: false},
}

// CheckFamilyExclusivity rejects sets carrying more than one member of an
// exclusive family.
func CheckFamilyExclusivity(set AspectSet) (err error) {
	name := aspectcodec.FirstExclusivityViolation(Families, set.Contains)
	if name != "" {
		err = eb.Build().Str("family", name).Str("set", string(set)).Errorf("aspect family admits at most one member per set")
	}
	return
}

// SanitizeFamilyExclusivity drops all but the first-encountered member of
// each exclusive family; sample generators use it to produce valid sets.
func SanitizeFamilyExclusivity(aspects []AspectE) (out []AspectE) {
	return aspectcodec.KeepFirstPerExclusiveFamily(Families, aspects)
}
