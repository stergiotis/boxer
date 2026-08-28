package streamreadaccess

import (
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// Driver walks a Leeway TableDesc + arrow.RecordBatch and drives a StructuredOutput2I.
// All tagged-value columns are List<X>. Entity row i → list offsets → flat inner array.
// Cardinality columns (List<Uint64>) partition inner arrays per attribute.
//
// Errors are accumulated in inst.errs (up to MaxErrorsToMerge, with
// consecutive duplicates deduped) and surfaced by DriveRecordBatch.
// The driver does not panic on malformed input — including an unknown column
// role or a cardinality column of the wrong Arrow type (review F-4).
type Driver struct {
	tblDesc *common.TableDesc
	ir      *common.IntermediateTableRepresentation
	fmts    Formatters

	plainSections    []plainSectionLayout
	sections         []sectionLayout
	coGroups         []coGroupLayout
	sectionInCoGroup map[int]int // sectionIdx → coGroupIdx; -1 = standalone

	// Optional sink capabilities, resolved once per entity in driveEntity;
	// nil when the sink does not implement them.
	arrowSink ArrowValueSinkI
	coTagSink CoSectionTagSinkI

	errs []error
}

// PhysicalColumnAddr identifies a physical Arrow column.
type PhysicalColumnAddr struct {
	Index          int
	FullColumnName string
}

// SinkI is the semantic canvas driven by the Driver.
// Error-as-state: errors are recorded internally and returned from
// EndTaggedValue(), EndSection(), EndCoSectionGroup(), EndEntity(), or EndBatch().
type SinkI interface {
	BeginBatch()
	EndBatch() (err error)

	BeginEntity()
	EndEntity() (err error)

	BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int)
	EndPlainSection() (err error)

	BeginPlainValue()
	EndPlainValue() (err error)

	BeginTaggedSections()
	EndTaggedSections() (err error)

	BeginCoSectionGroup(name naming.Key)
	EndCoSectionGroup() (err error)

	// BeginSection signals the start of a tagged section. useAspects carries
	// the section's UseAspects from the IR, enabling consumers (notably the
	// membership-role classifier and the schema-document emitter) to honour
	// uniformity hints without re-reading the IR.
	BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, useAspects useaspects.AspectSet, nAttrs int)
	EndSection() (err error)

	BeginTaggedValue()
	EndTaggedValue() (err error)

	// BeginColumn signals the start of a value column. valueSemantics is the
	// column's ValueSemantics aspect set from the IR, enabling consumers (e.g.
	// human-readable renderers) to filter columns by aspects such as
	// AspectHumanReadable / AspectMachineReadable without re-reading the IR.
	BeginColumn(colAddr PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI, valueSemantics valueaspects.AspectSet)
	EndColumn()

	BeginScalarValue()
	EndScalarValue() (err error)
	BeginHomogenousArrayValue(card int)
	EndHomogenousArrayValue()
	BeginSetValue(card int)
	EndSetValue()

	BeginValueItem(index int)
	EndValueItem()

	io.Writer
	io.StringWriter

	BeginTags(nTags int)
	EndTags()
}

// MembershipSinkI is the optional membership-rendering capability of a SinkI.
// Per ADR-0070 membership identity is orthogonal to the structural/value
// protocol (carriage ⟂ meaning ⟂ representation), so rendering per-tag
// membership is a separable concern. Sinks that visualise memberships implement
// it; non-rendering sinks (the sparkline / treemap / schema emitters) omit it
// entirely rather than stubbing five no-ops. The Driver type-asserts for it
// once per membership emission and skips membership when the sink lacks it — a
// dropped implementation therefore fails silently, so renderers pin the
// capability with a compile-time `var _ MembershipSinkI` assertion.
type MembershipSinkI interface {
	AddMembershipRef(lowCard bool, ref uint64)
	AddMembershipVerbatim(lowCard bool, verbatim string)
	AddMembershipRefParametrized(lowCard bool, ref uint64, params string)
	AddMembershipMixedLowCardRefHighCardParam(ref uint64, params string)
	AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, params string)
}

// ArrowValueSinkI is the optional typed-value capability of a SinkI — the
// MembershipSinkI pattern (ADR-0072) applied to values. A sink that
// implements it receives, inside the same BeginColumn / BeginScalarValue /
// BeginHomogenousArrayValue / BeginSetValue frames, a VIEW of the Arrow data
// in place of the formatted-text lane: the inner array and the flat index
// of a scalar, or the index range of a container. The text writes and the
// per-item BeginValueItem / EndValueItem frames are NOT driven for such a
// sink; it reads the elements itself, in whatever order it needs, while the
// RecordBatch is retained. Views cost no copy and no allocation.
//
// The text lane formats through arrow.Array.ValueStr — except for the network
// types, which it writes out as addresses — and ValueStr renders a Float32
// with 'g'/-1/32: a consumer that reparses that as float64 gets a different
// number than the column holds. Consumers that need the exact value
// (ADR-0201: the canonical record form) implement this; rendering sinks keep
// the text lane.
type ArrowValueSinkI interface {
	// WriteArrowScalar delivers the scalar of the current column: element
	// flatIdx of arr — the inner array of a List column for tagged and
	// non-scalar plain columns, the column itself for a scalar plain column.
	WriteArrowScalar(arr arrow.Array, flatIdx int)
	// WriteArrowRange delivers the elements of the current homogenous-array or
	// set value: arr[start:end] of the inner array, card = end-start.
	WriteArrowRange(arr arrow.Array, start int, end int)
}

// CoSectionTagSinkI is the optional capability that tells a membership-
// rendering sink which section of a co-section group the following tags
// belong to. driveCoGroup merges a group into one tagged value whose single
// tag frame carries the tags of every section (the merged BeginSection
// names the first section only); before driving each section's tags it
// calls BeginCoSectionTags with that section's name and use-aspects, so a
// consumer that classifies memberships per section (membershiprole, which
// honours the section-level uniformity hints) can do so for an annotation
// overlay as well as for the primary section. Standalone sections do not
// receive the call — BeginSection already carries their context.
type CoSectionTagSinkI interface {
	BeginCoSectionTags(sectionName naming.StylableName, useAspects useaspects.AspectSet)
}

// --- Value formatter. The membership formatters (ref / verbatim / params)
// moved to the membership package per ADR-0072: the driver no longer formats
// memberships produce-side; consumers render them at read time via a
// membership.Renderer. Only value formatting stays here. ---

type ValueFormatterI interface {
	FormatValue(arrowValueStr string, canonicalType canonicaltypes.PrimitiveAstNodeI) (formatted string)
}

type Formatters struct {
	ValueFormatter ValueFormatterI
}
