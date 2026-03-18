package streamreadaccess

import (
	"io"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
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

	// Precomputed column names from Arrow schema (set in DriveRecordBatch)
	arrowColNames []string

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

	BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int)
	EndSection() (err error)

	BeginTaggedValue()
	EndTaggedValue() (err error)

	BeginColumn(colAddr PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI)
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

	AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string)
	AddMembershipVerbatim(lowCard bool, verbatim string, humanReadableVerbatim string)
	AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string)
	AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string)
	AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string)
}

// --- Formatter interfaces ---

type ValueFormatterI interface {
	FormatValue(arrowValueStr string, canonicalType canonicaltypes.PrimitiveAstNodeI) (formatted string)
}

type RefFormatterI interface {
	FormatRef(ref uint64) (humanReadable string)
}

type VerbatimFormatterI interface {
	FormatVerbatim(raw []byte) (humanReadable string)
}

type ParamsFormatterI interface {
	FormatParams(raw []byte) (humanReadable string)
}

type Formatters struct {
	ValueFormatter    ValueFormatterI
	RefFormatter      RefFormatterI
	VerbatimFormatter VerbatimFormatterI
	ParamsFormatter   ParamsFormatterI
}
