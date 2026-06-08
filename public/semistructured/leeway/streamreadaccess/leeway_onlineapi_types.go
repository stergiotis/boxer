package streamreadaccess

import (
	"io"

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
// Errors are accumulated in inst.err (first-error-wins).
// The driver never panics.
type Driver struct {
	tblDesc *common.TableDesc
	ir      *common.IntermediateTableRepresentation
	fmts    Formatters

	plainSections    []plainSectionLayout
	sections         []sectionLayout
	coGroups         []coGroupLayout
	sectionInCoGroup map[int]int // sectionIdx → coGroupIdx; -1 = standalone

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
