package gocodegen

import (
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

type GoClassNamerReadAccessI interface {
	ComposeEntityReadAccessClassName(tableName naming.StylableName) (className string, err error)
	ComposeSectionReadAccessOuterClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error)
	ComposeSectionReadAccessAttributeClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error)
	ComposeSectionMembershipPackClassName(tableName naming.StylableName, sectionName naming.StylableName) (className string, err error)
	ComposeSharedMembershipPackClassName(tableName naming.StylableName, membershipSpec common.MembershipSpecE, i int, total int) (className string, err error)

	ComposeValueField(fieldNameIn string) (fieldNameOut string)
	ComposeValueFieldElementAccessor(fieldNameIn string) (fieldNameOut string)
	ComposeColumnIndexFieldName(fieldNameIn string) (fieldNameOut string)
	ComposeAccelFieldName(fieldNameIn string) (fieldNameOut string)
}
type GoClassNamerDmlI interface {
	ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error)
	ComposeEntityDmlClassName(tableName naming.StylableName) (fullClassName string, err error)
	ComposeSectionDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
	ComposeAttributeDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
}

// GoClassNamerCanonWireI names the classes of the leeway canonical-wire
// generator (ADR-0210 SD6): the per-table encoder and decoder, the
// tagger/dispatcher pair the ambiguous signatures need, and the slot enum with
// its per-slot signature and slot constants.
//
// A slot is identified by its ordinal in the generator's slot table and by the
// sections it joins, in signature order — one for a standalone section, k for a
// co-section group. An implementation may use either; the sections are what
// make the name readable, the ordinal is a fallback that is always unique.
type GoClassNamerCanonWireI interface {
	ComposeCanonWireEncoderClassName(tableName naming.StylableName) (className string, err error)
	ComposeCanonWireDecoderClassName(tableName naming.StylableName) (className string, err error)
	ComposeCanonWireTaggerInterfaceName(tableName naming.StylableName) (interfaceName string, err error)
	ComposeCanonWireDispatcherInterfaceName(tableName naming.StylableName) (interfaceName string, err error)
	ComposeCanonWireSlotEnumName(tableName naming.StylableName) (enumName string, err error)
	ComposeCanonWireSlotConstName(tableName naming.StylableName, slotOrdinal int, sectionNames []naming.StylableName) (constName string, err error)
	ComposeCanonWireSignatureConstName(tableName naming.StylableName, slotOrdinal int, sectionNames []naming.StylableName) (constName string, err error)
	ComposeCanonWirePlainGroupConstName(tableName naming.StylableName, itemType common.PlainItemTypeE) (constName string, err error)
}

type GoClassNamerI interface {
	GoClassNamerReadAccessI
	GoClassNamerDmlI
	GoClassNamerCanonWireI
	functional.PromiseReferentialTransparentI
}

type DefaultGoClassNamer struct {
}

var _ GoClassNamerI = (*DefaultGoClassNamer)(nil)

type MultiTablePerPackageClassNamer struct {
}

var _ GoClassNamerI = (*MultiTablePerPackageClassNamer)(nil)

type ClassNames struct {
	ReadAccessEntityClassName string
	InEntityClassName         string
	InSectionClassName        string
	InAttributeClassName      string
}

type CodeComposerI interface {
	PrepareCodeComposition()
	ComposeNamingConventionDependentCode(tableName naming.StylableName, ir *common.IntermediateTableRepresentation, namingConvention common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer GoClassNamerI) (err error)
	ComposeEntityClassAndFactoryCode(clsNamer GoClassNamerI, tableName naming.StylableName,
		sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error)
	ComposeEntityCode(clsNamer GoClassNamerI, tableName naming.StylableName,
		sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error)
	ComposeSectionClassAndFactoryCode(
		clsNamer GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int,
		sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error)
	ComposeSectionCode(
		clsNamer GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int,
		sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error)
	ComposeAttributeClassAndFactoryCode(
		clsNamer GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int,
		sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error)
	ComposeAttributeCode(
		clsNamer GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int,
		sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error)
}
