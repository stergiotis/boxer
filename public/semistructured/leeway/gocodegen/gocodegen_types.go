package gocodegen

import (
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

type GoClassNamerReadAccessI interface {
	ComposeEntityReadAccessClassName(tableName naming.StylableName) (className string, err error)
	ComposeSectionReadAccessOuterClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error)
	ComposeSectionReadAccessInnerClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName, subType common.IntermediateColumnSubTypeE) (className string, err error)
	ComposeSectionMembershipPackClassName(tableName naming.StylableName, sectionName naming.StylableName) (className string, err error)
	ComposeSharedMembershipPackClassName(tableName naming.StylableName, membershipSpec common.MembershipSpecE, i int, total int) (className string, err error)

	ComposeValueField(fieldNameIn string) (fieldNameOut string)
	ComposeColumnIndexFieldName(fieldNameIn string) (fieldNameOut string)
	ComposeAccelFieldName(fieldNameIn string) (fieldNameOut string)
}
type GoClassNamerDmlI interface {
	ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error)
	ComposeEntityDmlClassName(tableName naming.StylableName) (fullClassName string, err error)
	ComposeSectionDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
	ComposeAttributeDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
}

type GoClassNamerI interface {
	GoClassNamerReadAccessI
	GoClassNamerDmlI
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
