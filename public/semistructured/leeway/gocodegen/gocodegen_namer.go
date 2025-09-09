package gocodegen

import (
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func NewDefaultGoClassNamer() *DefaultGoClassNamer {
	return &DefaultGoClassNamer{}
}

func (inst *DefaultGoClassNamer) ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error) {
	return "createRecordBuilder", nil
}

func (inst *DefaultGoClassNamer) ComposeEntityClassName(tableName naming.StylableName) (fullClassName string, err error) {
	return "InEntity", nil
}

func (inst *DefaultGoClassNamer) ComposeSectionClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + sectionName.Convert(naming.NamingStyleUpperCamelCase).String(), nil
}

func (inst *DefaultGoClassNamer) ComposeAttributeClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + sectionName.Convert(naming.NamingStyleUpperCamelCase).String() + "InAttr", nil
}

func (inst *DefaultGoClassNamer) PromiseToBeReferentialTransparent() (_ functional.InterfaceIsReferentialTransparentType) {
	return
}

func NewMultiTablePerPackageGoClassNamer() *MultiTablePerPackageClassNamer {
	return &MultiTablePerPackageClassNamer{}
}

func (inst *MultiTablePerPackageClassNamer) ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error) {
	return "CreateSchema" + tableName.Convert(naming.NamingStyleUpperCamelCase).String(), nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeEntityClassName(tableName naming.StylableName) (fullClassName string, err error) {
	return "InEntity" + tableName.Convert(naming.NamingStyleUpperCamelCase).String(), nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeSectionClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + tableName.Convert(naming.NamingStyleUpperCamelCase).String() + "Section" + sectionName.Convert(naming.NamingStyleUpperCamelCase).String(), nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeAttributeClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + tableName.Convert(naming.NamingStyleUpperCamelCase).String() + "Section" + sectionName.Convert(naming.NamingStyleUpperCamelCase).String() + "InAttr", nil
}

func (inst *MultiTablePerPackageClassNamer) PromiseToBeReferentialTransparent() (_ functional.InterfaceIsReferentialTransparentType) {
	return
}
