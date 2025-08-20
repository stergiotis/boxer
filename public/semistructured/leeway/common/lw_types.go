package common

import (
	"bytes"
	"fmt"
	"iter"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

type NamingStyleE uint8

var _ fmt.Stringer = NamingStyleE(0)

type IntermediateColumnScopeE string
type IntermediateColumnSubTypeE string

type IntermediateColumnContext struct {
	Scope         IntermediateColumnScopeE
	SubType       IntermediateColumnSubTypeE
	PlainItemType PlainItemTypeE
	IndexOffset   uint32

	StreamingGroup Key

	SectionName    StylableName
	UseAspects     useaspects.AspectSet
	CoSectionGroup Key
}

type IntermediateColumnProps struct {
	Names []StylableName `cbor:"names"`
	Roles []ColumnRoleE  `cbor:"roles"`
	// original canonical type, for membership columns: scalar type
	CanonicalType  []canonicalTypes.PrimitiveAstNodeI `cbor:"canonicalType"`
	EncodingHints  []encodingaspects.AspectSet        `cbor:"encodingHints"`
	ValueSemantics []valueaspects.AspectSet           `cbor:"valueSemantics"`
}
type IntermediateTaggedValuesDesc struct {
	SectionName                     StylableName             `cbor:"sectionName"`
	UseAspects                      useaspects.AspectSet     `cbor:"useAspects"`
	Scalar                          *IntermediateColumnProps `cbor:"scalar"`
	NonScalarHomogenousArray        *IntermediateColumnProps `cbor:"nonScalarHomogenousArray"`
	NonScalarHomogenousArraySupport *IntermediateColumnProps `cbor:"nonScalarHomogenousArraySupport"`
	NonScalarSet                    *IntermediateColumnProps `cbor:"nonScalarSet"`
	NonScalarSetSupport             *IntermediateColumnProps `cbor:"nonScalarSetSupport"`
	Membership                      *IntermediateColumnProps `cbor:"membership"`
	MembershipSupport               *IntermediateColumnProps `cbor:"membershipSupport"`
	CoSectionGroup                  Key                      `cbor:"coSectionGroup"`
	StreamingGroup                  Key                      `cbor:"streamingGroup"`
}
type IntermediatePlainValuesDesc struct {
	ItemType                        PlainItemTypeE           `cbor:"itemType"`
	Scalar                          *IntermediateColumnProps `cbor:"scalar"`
	NonScalarHomogenousArray        *IntermediateColumnProps `cbor:"nonScalarHomogenousArray"`
	NonScalarHomogenousArraySupport *IntermediateColumnProps `cbor:"nonScalarHomogenousArraySupport"`
	NonScalarSet                    *IntermediateColumnProps `cbor:"nonScalarSet"`
	NonScalarSetSupport             *IntermediateColumnProps `cbor:"nonScalarSetSupport"`
	StreamingGroup                  Key                      `cbor:"streamingGroup"`
}
type IntermediateColumnIterator = iter.Seq2[IntermediateColumnContext, *IntermediateColumnProps]
type IntermediateTableRepresentation struct {
	PlainValueDesc  []*IntermediatePlainValuesDesc  `cbor:"plainValueDesc"`
	TaggedValueDesc []*IntermediateTaggedValuesDesc `cbor:"taggedValueDesc"`
}

var ErrNotImplemented = eh.Errorf("not implemented")
var ErrNoBuilder = eh.Errorf("no builder to write code to")

type CodeBuilderHolderI interface {
	SetCodeBuilder(s *strings.Builder)
	GetCode() (code string, err error)
	ResetCodeBuilder()
}
type GeneratorHolderI interface {
	SetGenerator(generator TechnologySpecificGeneratorI)
}
type NamingConventionHolderI interface {
	SetNamingConvention(convention NamingConventionI)
}
type ColumnRoleE string

var _ fmt.Stringer = ColumnRoleE("")

type TableRowConfigE uint8

var _ fmt.Stringer = TableRowConfigE(0)

type MembershipSpecE uint8

var _ fmt.Stringer = MembershipSpecE(0)

type PlainItemTypeE uint8

var _ fmt.Stringer = PlainItemTypeE(0)

var ErrNoCodebuilder = eh.Errorf("no codebuilder set")

type TableDictionaryEntryDescDto struct {
	Name    StylableName
	Comment string
}
type TableDesc struct {
	DictionaryEntry TableDictionaryEntryDescDto

	PlainValuesNames          []StylableName
	PlainValuesTypes          []canonicalTypes.PrimitiveAstNodeI
	PlainValuesEncodingHints  []encodingaspects.AspectSet
	PlainValuesItemTypes      []PlainItemTypeE
	PlainValuesValueSemantics []valueaspects.AspectSet
	OpaqueStreamingGroup      Key

	TaggedValuesSections []TaggedValuesSection
}

type TableDescDto struct {
	DictionaryEntry TableDictionaryEntryDescDto `cbor:"dictionaryEntry" json:"dictionaryEntry"`

	EntityIdNames                 [] /*i*/ StylableName              `cbor:"entityIdNames" json:"entityIdNames"`
	EntityIdTypes                 [] /*i*/ string                    `cbor:"entityIdTypes" json:"entityIdTypes"`
	EntityIdEncodingHints         [] /*i*/ encodingaspects.AspectSet `cbor:"entityIdEncodingHints" json:"entityIdEncodingHints"`
	EntityIdValueSemantics        [] /*i*/ valueaspects.AspectSet    `cbor:"entityIdValueSemantics" json:"entityIdValueSemantics"`
	EntityTimestampNames          [] /*j*/ StylableName              `cbor:"entityTimestampNames" json:"entityTimestampNames"`
	EntityTimestampTypes          [] /*j*/ string                    `cbor:"entityTimestampTypes" json:"entityTimestampTypes"`
	EntityTimestampEncodingHints  [] /*j*/ encodingaspects.AspectSet `cbor:"entityTimestampEncodingHints" json:"entityTimestampEncodingHints"`
	EntityTimestampValueSemantics [] /*i*/ valueaspects.AspectSet    `cbor:"entityTimestampValueSemantics" json:"entityTimestampValueSemantics"`
	EntityRoutingNames            [] /*k*/ StylableName              `cbor:"entityRoutingNames" json:"entityRoutingNames"`
	EntityRoutingTypes            [] /*k*/ string                    `cbor:"entityRoutingTypes" json:"entityRoutingTypes"`
	EntityRoutingEncodingHints    [] /*k*/ encodingaspects.AspectSet `cbor:"entityRoutingEncodingHints" json:"entityRoutingEncodingHints"`
	EntityRoutingValueSemantics   [] /*i*/ valueaspects.AspectSet    `cbor:"entityRoutingValueSemantics" json:"entityRoutingValueSemantics"`
	EntityLifecycleNames          [] /*l*/ StylableName              `cbor:"entityLifecycleNames" json:"entityLifecycleNames"`
	EntityLifecycleTypes          [] /*l*/ string                    `cbor:"entityLifecycleTypes" json:"entityLifecycleTypes"`
	EntityLifecycleEncodingHints  [] /*l*/ encodingaspects.AspectSet `cbor:"entityLifecycleEncodingHints" json:"entityLifecycleEncodingHints"`
	EntityLifecycleValueSemantics [] /*i*/ valueaspects.AspectSet    `cbor:"entityLifecycleValueSemantics" json:"entityLifecycleValueSemantics"`

	TaggedValuesSections []TaggedValuesSectionDto `cbor:"taggedValuesSections" json:"TaggedValuesSections"`

	TransactionNames          [] /*m*/ StylableName              `cbor:"transactionNames" json:"transactionNames"`
	TransactionTypes          [] /*m*/ string                    `cbor:"transactionTypes" json:"transactionTypes"`
	TransactionEncodingHints  [] /*m*/ encodingaspects.AspectSet `cbor:"transactionEncodingHints" json:"transactionEncodingHints"`
	TransactionValueSemantics [] /*i*/ valueaspects.AspectSet    `cbor:"transactionValueSemantics" json:"transactionValueSemantics"`

	OpaqueColumnNames          [] /*n*/ StylableName              `cbor:"opaqueColumnNames" json:"opaqueColumnNames"`
	OpaqueColumnTypes          [] /*n*/ string                    `cbor:"opaqueColumnTypes" json:"opaqueColumnTypes"`
	OpaqueColumnEncodingHints  [] /*n*/ encodingaspects.AspectSet `cbor:"opaqueColumnEncodingHints" json:"opaqueColumnEncodingHints"`
	OpaqueColumnValueSemantics [] /*i*/ valueaspects.AspectSet    `cbor:"opaqueColumnValueSemantics" json:"opaqueColumnValueSemantics"`
	OpaqueColumnStreamingGroup Key                                `cbor:"opaqueColumnStreamingGroup" json:"opaqueColumnStreamingGroup"`
}

type TaggedValuesSectionDto struct {
	Name                     StylableName                       `cbor:"name" json:"name"`
	MembershipSpec           MembershipSpecE                    `cbor:"membershipSpec" json:"membershipSpec"`
	ValueColumnNames         [] /*i*/ StylableName              `cbor:"valueColumnNames" json:"valueColumnNames"`
	ValueColumnTypes         [] /*i*/ string                    `cbor:"valueColumnTypes" json:"valueColumnTypes"`
	ValueColumnEncodingHints [] /*i*/ encodingaspects.AspectSet `cbor:"valueColumnEncodingHints" json:"valueColumnEncodingHints"`
	ValueSemantics           [] /*i*/ valueaspects.AspectSet    `cbor:"valueSemantics" json:"ValueSemantics"`
	UseAspects               useaspects.AspectSet               `cbor:"useAspects" json:"useAspects"`
	CoSectionGroup           Key                                `cbor:"coSectionGroup" json:"coSectionGroup"`
	StreamingGroup           Key                                `cbor:"streamingGroup" json:"streamingGroup"`
}

// TaggedValuesSection Note: If multiple, non-scalar columns are given they must have the same length and have co-array semantics
type TaggedValuesSection struct {
	Name               StylableName
	MembershipSpec     MembershipSpecE
	ValueColumnNames   [] /*i*/ StylableName
	ValueColumnTypes   [] /*i*/ canonicalTypes.PrimitiveAstNodeI
	ValueEncodingHints [] /*i*/ encodingaspects.AspectSet
	ValueSemantics     [] /*i*/ valueaspects.AspectSet
	UseAspects         useaspects.AspectSet
	CoSectionGroup     Key
	StreamingGroup     Key
}
type PhysicalColumnDesc struct {
	NameComponents             []string `cbor:"nameComponents"`
	NameComponentsExplanation  []string `cbor:"nameComponentsExplanation"`
	Comment                    string   `cbor:"comment"`
	GeneratingNamingConvention NamingConventionI
}

var _ fmt.Stringer = PhysicalColumnDesc{}

type TechnologySpecificMembershipSetGenI interface {
	GetMembershipSetCanonicalType(s MembershipSpecE) (ct1 canonicalTypes.PrimitiveAstNodeI, hint1 encodingaspects.AspectSet, colRole1 ColumnRoleE, ct2 canonicalTypes.PrimitiveAstNodeI, hint2 encodingaspects.AspectSet, colRole2 ColumnRoleE, err error)
}
type TechnologySpecificCodeGeneratorFwdI interface {
	GenerateColumnCode(idx int, phy PhysicalColumnDesc) (err error)
	GenerateType(canonicalType canonicalTypes.PrimitiveAstNodeI) (err error)
}
type TechnologySpecificCompatibilityI interface {
	CheckTypeCompatibility(canonicalType canonicalTypes.PrimitiveAstNodeI) (compatible bool, msg string)
	GetEncodingHintImplementationStatus(hint encodingaspects.AspectE) (status ImplementationStatusE, msg string)
}

type TechnologySpecificGeneratorI interface {
	CodeBuilderHolderI
	TechnologySpecificMembershipSetGenI
	TechnologySpecificCodeGeneratorFwdI
	TechnologySpecificCompatibilityI

	// GetTechnology stateless
	GetTechnology() (tech TechnologyDto)
}

var ErrInvalidMembershipSpec = eh.Errorf("invalid membership spec")

type ImplementationStatusE uint8

var _ fmt.Stringer = ImplementationStatusE(0)

type NamingConventionFwdI interface {
	MapIntermediateToPhysicalColumns(cc IntermediateColumnContext, cp IntermediateColumnProps, in []PhysicalColumnDesc, tableRowConfig TableRowConfigE) (out []PhysicalColumnDesc, err error)
}
type NamingConventionBwdI interface {
	ExtractCanonicalType(column PhysicalColumnDesc) (ct canonicalTypes.PrimitiveAstNodeI, err error)
	ExtractEncodingHints(column PhysicalColumnDesc) (hints encodingaspects.AspectSet, err error)
	ExtractValueSemantics(column PhysicalColumnDesc) (semantics valueaspects.AspectSet, err error)
	ExtractTableRowConfig(column PhysicalColumnDesc) (tableRowConfig TableRowConfigE, err error)
	ExtractPlainItemType(column PhysicalColumnDesc) (plainItemType PlainItemTypeE, err error)
	ParseColumn(fullColumnName string) (column PhysicalColumnDesc, err error)

	DiscoverTableFromPhysicalColumns(phys []PhysicalColumnDesc) (table TableDesc, tableRowConfig TableRowConfigE, err error)
	DiscoverTableFromColumnNames(columnNames []string) (table TableDesc, tableRowConfig TableRowConfigE, err error)
}

type NamingConventionI interface {
	NamingConventionFwdI
	NamingConventionBwdI
}
type TableValidator struct {
	duplicatedNames  *containers.HashSet[string]
	usedNamingStyles []uint32
	possibleNames    []string
	errors           []error
}
type TableMarshaller struct {
	enc cbor.EncMode
	dec cbor.DecMode
	dto *TableDescDto
}
type TableManipulator struct {
	marshaller                *TableMarshaller
	buffer                    *bytes.Buffer
	validator                 *TableValidator
	receivedInvalidAspects    bool
	upsertedCount             int
	plainValueItemNameToIndex []map[string]int
	sectionNameToIndex        map[string]int
	table                     *TableDesc
}

var _ TableManipulatorFluidI = (*TableManipulator)(nil)

type TableManipulatorFluidI interface {
	//SetTableName(name StylableName) TableManipulatorFluidI
	//SetTableComment(comment string) TableManipulatorFluidI
	TaggedValueSection(sectionName StylableName) TaggedValueSectionMerger
	PlainValueColumn(itemType PlainItemTypeE, name StylableName) PlainValueColumnMerger
	Reset()
}

type IntermediatePairHolder struct {
	ccs []IntermediateColumnContext
	cps []*IntermediateColumnProps
}

type TaggedValueSectionMerger struct {
	table        *TableDesc
	manip        *TableManipulator
	sectionIndex int
}
type TaggedValueColumnMerger struct {
	table        *TableDesc
	sectionIndex int
	columnIndex  int
}
type PlainValueColumnMerger struct {
	table       *TableDesc
	columnIndex int
}

// StylableName a name that can be transformed to other naming styles without loosing is descriptive, referencing and uniqueness properties
type StylableName string

var _ fmt.Stringer = StylableName("")

type Key string

var _ fmt.Stringer = Key("")
