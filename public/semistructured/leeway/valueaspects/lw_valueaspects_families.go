package valueaspects

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/aspectcodec"
)

// Families is the registry of aspect families (ADR-0182 SD3): documentation
// of record and the source for exclusivity validation and, later, DQL
// authoring diagnostics. Exclusive families admit at most one member per set.
var Families = []aspectcodec.Family[AspectE]{
	{Name: "epistemic-origin", Members: []AspectE{AspectMeasured, AspectAsserted, AspectDerived}, Exclusive: true},
	{Name: "generating-agent", Members: []AspectE{AspectHumanGenerated, AspectMachineGenerated}, Exclusive: false},
	{Name: "readability", Members: []AspectE{AspectHumanReadable, AspectMachineReadable}, Exclusive: false},
	{Name: "requiredness", Members: []AspectE{AspectMandatory, AspectOptional}, Exclusive: true},
	{Name: "scale-of-measurement", Members: []AspectE{AspectScaleOfMeasurementNominal, AspectScaleOfMeasurementOrdinal, AspectScaleOfMeasurementMetricInterval, AspectScaleOfMeasurementMetricRatio}, Exclusive: true},
	{Name: "temporal-role", Members: []AspectE{AspectTransactionTime, AspectValidTime}, Exclusive: true},
	{Name: "lifespan", Members: []AspectE{AspectUltraShortLifespan, AspectShortLifespan, AspectMediumLifespan, AspectLongLifespan, AspectUltraLongLifespan}, Exclusive: true},
	{Name: "identifier-kind", Members: []AspectE{AspectIdNaturalKey, AspectIdSurrogateKey, AspectIdDurableSuperNaturalKey, AspectIdContentAddressableKey, AspectIdReference}, Exclusive: false},
	{Name: "de-identification", Members: []AspectE{AspectAnonymized, AspectPseudonymized}, Exclusive: true},
	{Name: "json-form", Members: []AspectE{AspectJsonScalar, AspectJsonArray, AspectJsonObject, AspectJson}, Exclusive: false},
	{Name: "cbor-form", Members: []AspectE{AspectCborScalar, AspectCborArray, AspectCborMap, AspectCbor}, Exclusive: false},
	{Name: "unicode-text", Members: []AspectE{AspectTextUnicodeNormalizedNfd, AspectTextUnicodeNormalizedNfc, AspectTextUnicodeNormalizedNfkd, AspectTextUnicodeNormalizedNfkc, AspectTextUnicodeCaseFolded, AspectTextUnicodeCaseInsensitive, AspectTextUnicodeLocaleSensitive, AspectTextUnicodeMayBeBidi}, Exclusive: false},
	{Name: "graph-role", Members: []AspectE{AspectGraphVertex, AspectGraphEdge, AspectHyperGraphEdge}, Exclusive: false},
	{Name: "emulated-membership", Members: []AspectE{AspectEmulatedMembershipVerbatim, AspectEmulatedMembershipRef, AspectEmulatedMembershipParams, AspectEmulatedMembershipRefWithParams}, Exclusive: true},
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
