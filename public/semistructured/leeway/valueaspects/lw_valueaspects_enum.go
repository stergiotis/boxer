// Package valueaspects is the closed vocabulary of value-semantics aspects
// (ADR-0182). Admission criterion: an aspect family is admissible when its
// meaning is anchored in mathematics, a long-lived open standard, a practice
// predating the current tooling generation, or a format the engine itself
// commits to; its domain is closed under that anchor, or it is a genuinely
// independent boolean. Open-domain technique-, tier- or brand-shaped
// information belongs in canonical types, TableOptions or the catalog — not
// here. Numbering is the wire format (segments in physical column names):
// append-only between migration windows, family-grouped as of v2.
package valueaspects

import (
	"slices"
)

const (
	// Epistemic origin (exclusive): how the stored value came to exist.
	// Proximate origin wins — the aspect describes the step that produced
	// the stored value, decidable per column, no chain-chasing. It licenses
	// no inference: derivation edges live in provenance facts, never here.

	AspectMeasured AspectE = 0 // an observation of the world, captured by an instrument or observing process
	AspectAsserted AspectE = 1 // declared by an agent as a claim or input (user entry, configuration, label)
	AspectDerived  AspectE = 2 // computed from other values

	// Generating agent kind; orthogonal to the epistemic origin (a user-typed
	// name is human-generated + asserted, a sensor reading machine-generated
	// + measured).

	AspectHumanGenerated   AspectE = 3
	AspectMachineGenerated AspectE = 4
	AspectSynthetic        AspectE = 5 // fixture/simulated data, not an observation of the world

	AspectHumanReadable   AspectE = 6
	AspectMachineReadable AspectE = 7

	// Mandatory/Optional declare requiredness at the semantic level
	// (exclusive). This is not the structural presence distinction: e.g. a
	// plain non-scalar value is always structurally present but may be empty.

	AspectMandatory AspectE = 8
	AspectOptional  AspectE = 9

	AspectImmutable       AspectE = 10 // never updated after first write; also where SCD type 0 maps (see useaspects history family)
	AspectSentinelMissing AspectE = 11 // absence encoded in-band by sentinel values (-999, epoch zero, ...)

	// Stevens' scales of measurement (exclusive).

	AspectScaleOfMeasurementNominal        AspectE = 12
	AspectScaleOfMeasurementOrdinal        AspectE = 13
	AspectScaleOfMeasurementMetricInterval AspectE = 14
	AspectScaleOfMeasurementMetricRatio    AspectE = 15

	AspectVectorValue        AspectE = 16
	AspectCanonicalizedValue AspectE = 17

	// Bitemporal roles per SQL:2011 (exclusive): system-maintained
	// transaction time vs application-defined valid time.

	AspectTransactionTime AspectE = 18
	AspectValidTime       AspectE = 19

	// Lifespan buckets (exclusive). Bucket boundaries are deployment-defined
	// and advisory; no validator can check them.

	AspectUltraShortLifespan AspectE = 20
	AspectShortLifespan      AspectE = 21
	AspectMediumLifespan     AspectE = 22
	AspectLongLifespan       AspectE = 23
	AspectUltraLongLifespan  AspectE = 24

	// Identifier kinds. IdDurableSuperNaturalKey follows dimensional
	// modeling's durable supernatural key; IdReference marks a value that
	// references another entity's key (foreign reference) — the others
	// classify keys the entity owns.

	AspectIdNaturalKey             AspectE = 25
	AspectIdSurrogateKey           AspectE = 26
	AspectIdDurableSuperNaturalKey AspectE = 27
	AspectIdContentAddressableKey  AspectE = 28
	AspectIdReference              AspectE = 29

	// De-identification state (Anonymized/Pseudonymized exclusive):
	// pseudonymized data is reversibly de-identified and remains personal
	// data; anonymized data is not. Secret marks credentials, keys and
	// tokens — consumers must mask by default.

	AspectAnonymized    AspectE = 30
	AspectPseudonymized AspectE = 31
	AspectSecret        AspectE = 32

	AspectApplicationLevelEncryption  AspectE = 33
	AspectApplicationLevelCompression AspectE = 34

	AspectUrl AspectE = 35 // follow the WHATWG recommendation to forget URI and use URL (see https://url.spec.whatwg.org/#goals)

	// The Json*/Cbor* value aspects state that the value is (or may be dealt
	// with as) a string/bytes serialization in the given format. Deliberately
	// distinct from the equally named encodingaspects family, which permits
	// the ddl module to use a native database type.

	AspectJsonScalar AspectE = 36
	AspectJsonArray  AspectE = 37
	AspectJsonObject AspectE = 38
	AspectJson       AspectE = 39
	AspectCborScalar AspectE = 40
	AspectCborArray  AspectE = 41
	AspectCborMap    AspectE = 42
	AspectCbor       AspectE = 43

	AspectTextUnicodeNormalizedNfd   AspectE = 44 // Normalization Form Canonical Decomposition
	AspectTextUnicodeNormalizedNfc   AspectE = 45 // Normalization Form Canonical Composition
	AspectTextUnicodeNormalizedNfkd  AspectE = 46 // Normalization Form Compatibility Decomposition
	AspectTextUnicodeNormalizedNfkc  AspectE = 47 // Normalization Form Compatibility Composition
	AspectTextUnicodeCaseFolded      AspectE = 48 // Unicode case folding (not a normalization form)
	AspectTextUnicodeCaseInsensitive AspectE = 49
	AspectTextUnicodeLocaleSensitive AspectE = 50
	AspectTextUnicodeMayBeBidi       AspectE = 51

	AspectGraphVertex    AspectE = 52
	AspectGraphEdge      AspectE = 53
	AspectHyperGraphEdge AspectE = 54

	// The EmulatedMembership* aspects (exclusive) mark values that emulate
	// membership semantics without leeway's native membership machinery,
	// supporting transitions from other EAV systems.

	AspectEmulatedMembershipVerbatim      AspectE = 55
	AspectEmulatedMembershipRef           AspectE = 56
	AspectEmulatedMembershipParams        AspectE = 57
	AspectEmulatedMembershipRefWithParams AspectE = 58
)

var MaxAspectExcl = slices.Max(AllAspects) + 1

var AllAspects = []AspectE{
	AspectMeasured,
	AspectAsserted,
	AspectDerived,
	AspectHumanGenerated,
	AspectMachineGenerated,
	AspectSynthetic,
	AspectHumanReadable,
	AspectMachineReadable,
	AspectMandatory,
	AspectOptional,
	AspectImmutable,
	AspectSentinelMissing,
	AspectScaleOfMeasurementNominal,
	AspectScaleOfMeasurementOrdinal,
	AspectScaleOfMeasurementMetricInterval,
	AspectScaleOfMeasurementMetricRatio,
	AspectVectorValue,
	AspectCanonicalizedValue,
	AspectTransactionTime,
	AspectValidTime,
	AspectUltraShortLifespan,
	AspectShortLifespan,
	AspectMediumLifespan,
	AspectLongLifespan,
	AspectUltraLongLifespan,
	AspectIdNaturalKey,
	AspectIdSurrogateKey,
	AspectIdDurableSuperNaturalKey,
	AspectIdContentAddressableKey,
	AspectIdReference,
	AspectAnonymized,
	AspectPseudonymized,
	AspectSecret,
	AspectApplicationLevelEncryption,
	AspectApplicationLevelCompression,
	AspectUrl,
	AspectJsonScalar,
	AspectJsonArray,
	AspectJsonObject,
	AspectJson,
	AspectCborScalar,
	AspectCborArray,
	AspectCborMap,
	AspectCbor,
	AspectTextUnicodeNormalizedNfd,
	AspectTextUnicodeNormalizedNfc,
	AspectTextUnicodeNormalizedNfkd,
	AspectTextUnicodeNormalizedNfkc,
	AspectTextUnicodeCaseFolded,
	AspectTextUnicodeCaseInsensitive,
	AspectTextUnicodeLocaleSensitive,
	AspectTextUnicodeMayBeBidi,
	AspectGraphVertex,
	AspectGraphEdge,
	AspectHyperGraphEdge,
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
	case AspectMeasured:
		return "measured"
	case AspectAsserted:
		return "asserted"
	case AspectDerived:
		return "derived"
	case AspectHumanGenerated:
		return "human-generated"
	case AspectMachineGenerated:
		return "machine-generated"
	case AspectSynthetic:
		return "synthetic"
	case AspectHumanReadable:
		return "human-readable"
	case AspectMachineReadable:
		return "machine-readable"
	case AspectMandatory:
		return "mandatory"
	case AspectOptional:
		return "optional"
	case AspectImmutable:
		return "immutable"
	case AspectSentinelMissing:
		return "sentinel-missing"
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
	case AspectTransactionTime:
		return "transaction-time"
	case AspectValidTime:
		return "valid-time"
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
	case AspectIdNaturalKey:
		return "id-natural-key"
	case AspectIdSurrogateKey:
		return "id-surrogate-key"
	case AspectIdDurableSuperNaturalKey:
		return "id-durable-super-natural-key"
	case AspectIdContentAddressableKey:
		return "id-content-addressable-key"
	case AspectIdReference:
		return "id-reference"
	case AspectAnonymized:
		return "anonymized"
	case AspectPseudonymized:
		return "pseudonymized"
	case AspectSecret:
		return "secret"
	case AspectApplicationLevelEncryption:
		return "application-level-encryption"
	case AspectApplicationLevelCompression:
		return "application-level-compression"
	case AspectUrl:
		return "url"
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
	case AspectGraphVertex:
		return "graph-vertex"
	case AspectGraphEdge:
		return "graph-edge"
	case AspectHyperGraphEdge:
		return "hyper-graph-edge"
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
