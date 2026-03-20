//go:build llm_generated_opus46

package card

import (
	"encoding/binary"
	"math"
	"slices"

	"github.com/klauspost/compress/zstd"
	"github.com/stergiotis/boxer/public/ea"
	entropy2 "github.com/stergiotis/boxer/public/math/numerical/entropy"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// EntityFeatures holds the 16 computed features for a single entity.
type EntityFeatures struct {
	// Scale
	TotalAttributeCount float64 // F01: sum of nAttrs across all tagged sections
	TotalValueBytes     float64 // F02: sum of bytes written via Write/WriteString

	// Shape
	GiniAttrsPerSection float64 // F03: Gini coefficient of attribute distribution across sections
	MaxToMeanAttrRatio  float64 // F04: max(nAttrs) / mean(nAttrs)

	// Membership topology
	MeanTagsPerAttr      float64 // F05: total tags / total attributes
	TagCountVariance     float64 // F06: variance of per-attribute tag counts
	UntaggedAttrFraction float64 // F07: fraction of attributes with zero tags

	// Membership role diversity
	MembershipRoleEntropy float64 // F08: Shannon entropy over role type distribution

	// Value structure
	NonScalarValueFraction float64 // F09: (array + set emissions) / total value emissions
	MeanNonScalarCard      float64 // F10: mean element count of array/set values

	// Structural complexity
	EffectiveSectionCount  float64 // F11: exp(Shannon entropy of attribute distribution)
	CoGroupSectionFraction float64 // F12: sections in co-groups / total tagged sections

	// Compression-based complexity
	TopologyCompressionRatio float64 // F13: zstd(topology_bytes) / len(topology_bytes)
	ValueCompressionRatio    float64 // F14: zstd(value_bytes) / len(value_bytes)

	// Content characteristics
	MeanValueLength      float64 // F15: mean byte length of values
	ValueRepetitionRatio float64 // F16: 1 - distinct_values / total_values
}

// AsSlice returns the 16 features as a dense float64 slice.
func (f *EntityFeatures) AsSlice() []float64 {
	return []float64{
		f.TotalAttributeCount,
		f.TotalValueBytes,
		f.GiniAttrsPerSection,
		f.MaxToMeanAttrRatio,
		f.MeanTagsPerAttr,
		f.TagCountVariance,
		f.UntaggedAttrFraction,
		f.MembershipRoleEntropy,
		f.NonScalarValueFraction,
		f.MeanNonScalarCard,
		f.EffectiveSectionCount,
		f.CoGroupSectionFraction,
		f.TopologyCompressionRatio,
		f.ValueCompressionRatio,
		f.MeanValueLength,
		f.ValueRepetitionRatio,
	}
}

// FeatureNames returns the names of the 16 features in slice order.
func FeatureNames() []string {
	return []string{
		"total_attribute_count",
		"total_value_bytes",
		"gini_attrs_per_section",
		"max_to_mean_attr_ratio",
		"mean_tags_per_attr",
		"tag_count_variance",
		"untagged_attr_fraction",
		"membership_role_entropy",
		"non_scalar_value_fraction",
		"mean_non_scalar_card",
		"effective_section_count",
		"co_group_section_fraction",
		"topology_compression_ratio",
		"value_compression_ratio",
		"mean_value_length",
		"value_repetition_ratio",
	}
}

// FeatureExtractor is an SinkI that computes 16 numerical
// features per entity. Call Results() after driving to get the features.
type FeatureExtractor struct {
	sizeWriter ea.SizeMeasureWriter
	zstdEnc    *zstd.Encoder
	results    []EntityFeatures

	// --- Per-entity accumulators ---

	// Section tracking
	sectionAttrCounts []int // nAttrs per tagged section
	nTaggedSections   int
	nCoGroupSections  int
	inCoGroup         bool

	// Per-attribute tag counts (accumulated across all sections)
	attrTagCounts []int

	// Membership role counting (10 distinct roles)
	roleCounts [10]int
	totalTags  int

	// Value structure counters
	scalarValueCount    int
	nonScalarValueCount int
	nonScalarElemSum    int // total elements across all arrays/sets

	// Value content tracking
	totalValueBytes int
	valueCount      int
	valueLengths    []int
	distinctValues  map[string]struct{}

	// Topology serialization buffer (for compression)
	topoBuf []byte

	// Value content buffer (for compression)
	valueBuf []byte

	// Current attribute state
	curAttrTags   int
	inTaggedValue bool
	curValueLen   int
}

func NewFeatureExtractor() (*FeatureExtractor, error) {
	d := ea.SizeMeasureWriter{
		Size: 0,
	}
	enc, err := zstd.NewWriter(&d, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	return &FeatureExtractor{
		sizeWriter:     d,
		zstdEnc:        enc,
		distinctValues: make(map[string]struct{}, 64),
	}, nil
}

// Results returns the computed features for all entities in the last batch.
func (f *FeatureExtractor) Results() []EntityFeatures {
	return f.results
}

func (f *FeatureExtractor) resetEntity() {
	f.sectionAttrCounts = f.sectionAttrCounts[:0]
	f.nTaggedSections = 0
	f.nCoGroupSections = 0
	f.attrTagCounts = f.attrTagCounts[:0]
	f.roleCounts = [10]int{}
	f.totalTags = 0
	f.scalarValueCount = 0
	f.nonScalarValueCount = 0
	f.nonScalarElemSum = 0
	f.totalValueBytes = 0
	f.valueCount = 0
	f.valueLengths = f.valueLengths[:0]
	clear(f.distinctValues)
	f.topoBuf = f.topoBuf[:0]
	f.valueBuf = f.valueBuf[:0]
	f.inCoGroup = false
	f.inTaggedValue = false
}

func (f *FeatureExtractor) computeFeatures() EntityFeatures {
	var feat EntityFeatures

	// F01: Total attribute count
	totalAttrs := 0
	for _, n := range f.sectionAttrCounts {
		totalAttrs += n
	}
	feat.TotalAttributeCount = float64(totalAttrs)

	// F02: Total value bytes
	feat.TotalValueBytes = float64(f.totalValueBytes)

	// F03: Gini coefficient of attributes-per-section
	feat.GiniAttrsPerSection = gini(f.sectionAttrCounts)

	// F04: Max-to-mean attribute ratio
	if len(f.sectionAttrCounts) > 0 && totalAttrs > 0 {
		maxAttrs := slices.Max(f.sectionAttrCounts)
		mean := float64(totalAttrs) / float64(len(f.sectionAttrCounts))
		if mean > 0 {
			feat.MaxToMeanAttrRatio = float64(maxAttrs) / mean
		}
	}

	// F05: Mean tags per attribute
	if totalAttrs > 0 {
		feat.MeanTagsPerAttr = float64(f.totalTags) / float64(totalAttrs)
	}

	// F06: Tag count variance
	feat.TagCountVariance = variance(f.attrTagCounts)

	// F07: Untagged attribute fraction
	if totalAttrs > 0 {
		untagged := 0
		for _, tc := range f.attrTagCounts {
			if tc == 0 {
				untagged++
			}
		}
		feat.UntaggedAttrFraction = float64(untagged) / float64(totalAttrs)
	}

	// F08: Membership role entropy
	feat.MembershipRoleEntropy = shannonEntropyFromCounts(f.roleCounts[:])

	// F09: Non-scalar value fraction
	totalValueEmissions := f.scalarValueCount + f.nonScalarValueCount
	if totalValueEmissions > 0 {
		feat.NonScalarValueFraction = float64(f.nonScalarValueCount) / float64(totalValueEmissions)
	}

	// F10: Mean non-scalar cardinality
	if f.nonScalarValueCount > 0 {
		feat.MeanNonScalarCard = float64(f.nonScalarElemSum) / float64(f.nonScalarValueCount)
	}

	// F11: Effective section count = exp(Shannon entropy of attribute distribution)
	if len(f.sectionAttrCounts) > 0 && totalAttrs > 0 {
		feat.EffectiveSectionCount = math.Exp(shannonEntropyFromCounts(f.sectionAttrCounts))
	}

	// F12: Co-group section fraction
	if f.nTaggedSections > 0 {
		feat.CoGroupSectionFraction = float64(f.nCoGroupSections) / float64(f.nTaggedSections)
	}

	// F13: Topology compression ratio
	feat.TopologyCompressionRatio = f.compressionRatio(f.topoBuf)

	// F14: Value compression ratio
	feat.ValueCompressionRatio = f.compressionRatio(f.valueBuf)

	// F15: Mean value length
	if f.valueCount > 0 {
		feat.MeanValueLength = float64(f.totalValueBytes) / float64(f.valueCount)
	}

	// F16: Value repetition ratio
	if f.valueCount > 0 {
		distinct := len(f.distinctValues)
		feat.ValueRepetitionRatio = 1.0 - float64(distinct)/float64(f.valueCount)
	}

	return feat
}

// --- Topology serialization helpers ---

func (f *FeatureExtractor) topoWriteTag(tag byte) {
	f.topoBuf = append(f.topoBuf, tag)
}

func (f *FeatureExtractor) topoWriteInt(v int) {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutVarint(buf[:], int64(v))
	f.topoBuf = append(f.topoBuf, buf[:n]...)
}

func (f *FeatureExtractor) topoWriteString(s string) {
	f.topoWriteInt(len(s))
	f.topoBuf = append(f.topoBuf, s...)
}

// --- Compression ---

func (f *FeatureExtractor) compressionRatio(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}
	f.sizeWriter.Size = 0
	_, err := f.zstdEnc.Write(data)
	if err != nil {
		return 0.0
	}
	return float64(f.sizeWriter.Size) / float64(len(data))
}

// --- Batch ---

func (f *FeatureExtractor) BeginBatch() {
	f.results = f.results[:0]
}

func (f *FeatureExtractor) EndBatch() error {
	return nil
}

// --- Entity ---

func (f *FeatureExtractor) BeginEntity() {
	f.resetEntity()
}

func (f *FeatureExtractor) EndEntity() error {
	feat := f.computeFeatures()
	f.results = append(f.results, feat)
	return nil
}

// --- Plain section ---

func (f *FeatureExtractor) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	f.topoWriteTag('P')
	f.topoWriteInt(int(itemType))
	f.topoWriteInt(len(valueNames))
}

func (f *FeatureExtractor) EndPlainSection() error { return nil }
func (f *FeatureExtractor) BeginPlainValue()       {}
func (f *FeatureExtractor) EndPlainValue() error   { return nil }

// --- Tagged sections ---

func (f *FeatureExtractor) BeginTaggedSections()     {}
func (f *FeatureExtractor) EndTaggedSections() error { return nil }

// --- Co-section group ---

func (f *FeatureExtractor) BeginCoSectionGroup(name naming.Key) {
	f.inCoGroup = true
	f.topoWriteTag('G')
	f.topoWriteString(name.String())
}

func (f *FeatureExtractor) EndCoSectionGroup() error {
	f.inCoGroup = false
	f.topoWriteTag('g')
	return nil
}

// --- Section ---

func (f *FeatureExtractor) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	f.sectionAttrCounts = append(f.sectionAttrCounts, nAttrs)
	f.nTaggedSections++
	if f.inCoGroup {
		f.nCoGroupSections++
	}

	f.topoWriteTag('S')
	f.topoWriteString(name.String())
	f.topoWriteInt(nAttrs)
	f.topoWriteInt(len(valueNames))
}

func (f *FeatureExtractor) EndSection() error {
	f.topoWriteTag('s')
	return nil
}

// --- Tagged value ---

func (f *FeatureExtractor) BeginTaggedValue() {
	f.inTaggedValue = true
	f.curAttrTags = 0
	f.topoWriteTag('A')
}

func (f *FeatureExtractor) EndTaggedValue() error {
	f.attrTagCounts = append(f.attrTagCounts, f.curAttrTags)
	f.inTaggedValue = false
	f.topoWriteTag('a')
	f.topoWriteInt(f.curAttrTags)
	return nil
}

// --- Column ---

func (f *FeatureExtractor) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	f.curValueLen = 0
	f.topoWriteTag('C')
}

func (f *FeatureExtractor) EndColumn() {
	f.topoWriteTag('c')
}

// --- Value shapes ---

func (f *FeatureExtractor) BeginScalarValue() {
	f.scalarValueCount++
	f.topoWriteTag('V')
}

func (f *FeatureExtractor) EndScalarValue() error {
	f.topoWriteTag('v')
	return nil
}

func (f *FeatureExtractor) BeginHomogenousArrayValue(card int) {
	f.nonScalarValueCount++
	f.nonScalarElemSum += card
	f.topoWriteTag('H')
	f.topoWriteInt(card)
}

func (f *FeatureExtractor) EndHomogenousArrayValue() {
	f.topoWriteTag('h')
}

func (f *FeatureExtractor) BeginSetValue(card int) {
	f.nonScalarValueCount++
	f.nonScalarElemSum += card
	f.topoWriteTag('M')
	f.topoWriteInt(card)
}

func (f *FeatureExtractor) EndSetValue() {
	f.topoWriteTag('m')
}

func (f *FeatureExtractor) BeginValueItem(index int) {}
func (f *FeatureExtractor) EndValueItem()            {}

// --- Write ---

func (f *FeatureExtractor) Write(p []byte) (n int, err error) {
	n = len(p)
	f.totalValueBytes += n
	f.curValueLen += n
	f.valueBuf = append(f.valueBuf, p...)
	f.valueCount++
	f.valueLengths = append(f.valueLengths, n)
	f.distinctValues[string(p)] = struct{}{}
	return
}

func (f *FeatureExtractor) WriteString(s string) (n int, err error) {
	n = len(s)
	f.totalValueBytes += n
	f.curValueLen += n
	f.valueBuf = append(f.valueBuf, s...)
	f.valueCount++
	f.valueLengths = append(f.valueLengths, n)
	f.distinctValues[s] = struct{}{}
	return
}

// --- Tags ---

func (f *FeatureExtractor) BeginTags(nTags int) {
	f.topoWriteTag('T')
	f.topoWriteInt(nTags)
}

func (f *FeatureExtractor) EndTags() {
	f.topoWriteTag('t')
}

// Role index mapping for the 10 distinct membership call types.
const (
	roleIdxHighCardRef      = 0
	roleIdxLowCardRef       = 1
	roleIdxHighCardVerbatim = 2
	roleIdxLowCardVerbatim  = 3
	roleIdxHighCardRefParam = 4
	roleIdxLowCardRefParam  = 5
	roleIdxMixedLowCardRef  = 6
	roleIdxMixedLowCardVerb = 7
	roleIdxMixedRefHCParam  = 8
	roleIdxMixedVerbHCParam = 9
)

func (f *FeatureExtractor) addTag(roleIdx int) {
	f.curAttrTags++
	f.totalTags++
	f.roleCounts[roleIdx]++
}

func (f *FeatureExtractor) AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string) {
	if lowCard {
		f.addTag(roleIdxLowCardRef)
	} else {
		f.addTag(roleIdxHighCardRef)
	}
	f.topoWriteTag('R')
}

func (f *FeatureExtractor) AddMembershipVerbatim(lowCard bool, verbatim string, humanReadableVerbatim string) {
	if lowCard {
		f.addTag(roleIdxLowCardVerbatim)
	} else {
		f.addTag(roleIdxHighCardVerbatim)
	}
	f.topoWriteTag('W')
}

func (f *FeatureExtractor) AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	if lowCard {
		f.addTag(roleIdxLowCardRefParam)
	} else {
		f.addTag(roleIdxHighCardRefParam)
	}
	f.topoWriteTag('Q')
}

func (f *FeatureExtractor) AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	f.addTag(roleIdxMixedLowCardRef)
	f.topoWriteTag('X')
}

func (f *FeatureExtractor) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string) {
	f.addTag(roleIdxMixedLowCardVerb)
	f.topoWriteTag('Y')
}

// ============================================================================
// Statistical helper functions
// ============================================================================

// gini computes the Gini coefficient for a slice of non-negative integers.
// Returns 0 for empty or uniform inputs, approaches 1 for maximal inequality.
func gini(values []int) float64 {
	n := len(values)
	if n <= 1 {
		return 0
	}

	sorted := make([]int, n)
	copy(sorted, values)
	slices.Sort(sorted)

	sum := 0
	for _, v := range sorted {
		sum += v
	}
	if sum == 0 {
		return 0
	}

	// Gini = (2 * Σ i*x_i) / (n * Σ x_i) - (n+1)/n
	numerator := 0.0
	for i, v := range sorted {
		numerator += float64(i+1) * float64(v)
	}
	return (2*numerator)/(float64(n)*float64(sum)) - float64(n+1)/float64(n)
}

// variance computes the population variance of integer values.
func variance(values []int) float64 {
	// TODO use streaming
	n := len(values)
	if n == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += float64(v)
	}
	mean := sum / float64(n)

	sumSq := 0.0
	for _, v := range values {
		d := float64(v) - mean
		sumSq += d * d
	}
	return sumSq / float64(n)
}

// shannonEntropyFromCounts computes Shannon entropy (in nats) from a count vector
func shannonEntropyFromCounts(counts []int) float64 {
	return entropy2.ShannonNats(func(yield func(float64) bool) {
		for _, c := range counts {
			if !yield(float64(c)) {
				return
			}
		}
	})
}
