package gocodegen

import (
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func (inst *DefaultGoClassNamer) ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error) {
	return "createRecordBuilder", nil
}

func (inst *DefaultGoClassNamer) ComposeEntityDmlClassName(tableName naming.StylableName) (fullClassName string, err error) {
	return "InEntity", nil
}

func (inst *DefaultGoClassNamer) ComposeSectionDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + sectionName.Convert(naming.UpperCamelCase).String(), nil
}

func (inst *DefaultGoClassNamer) ComposeAttributeDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + sectionName.Convert(naming.UpperCamelCase).String() + "InAttr", nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error) {
	return "CreateSchema" + tableName.Convert(naming.UpperCamelCase).String(), nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeEntityDmlClassName(tableName naming.StylableName) (fullClassName string, err error) {
	return "InEntity" + tableName.Convert(naming.UpperCamelCase).String(), nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeSectionDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + tableName.Convert(naming.UpperCamelCase).String() + "Section" + sectionName.Convert(naming.UpperCamelCase).String(), nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeAttributeDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + tableName.Convert(naming.UpperCamelCase).String() + "Section" + sectionName.Convert(naming.UpperCamelCase).String() + "InAttr", nil
}

func (inst *MultiTablePerPackageClassNamer) PromiseToBeReferentialTransparent() (_ functional.InterfaceIsReferentialTransparentType) {
	return
}
