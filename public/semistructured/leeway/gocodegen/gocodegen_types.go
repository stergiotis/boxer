package gocodegen

import (
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

type GoClassNamerI interface {
	ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error)
	ComposeEntityClassName(tableName naming.StylableName) (fullClassName string, err error)
	ComposeSectionClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
	ComposeAttributeClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
	functional.PromiseReferentialTransparentI
}

type DefaultGoClassNamer struct {
}

var _ GoClassNamerI = (*DefaultGoClassNamer)(nil)

type MultiTablePerPackageClassNamer struct {
}

var _ GoClassNamerI = (*MultiTablePerPackageClassNamer)(nil)

type ClassNames struct {
	InEntityClassName    string
	InSectionClassName   string
	InAttributeClassName string
}

type CodeComposerI interface {
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
