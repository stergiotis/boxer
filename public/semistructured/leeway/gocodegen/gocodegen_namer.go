package gocodegen

import (
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/observability/eh"
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

func NewClassNamesEntityOnly(classNamer GoClassNamerI, tableName naming.StylableName) (clsNames ClassNames, err error) {
	return NewClassNames(classNamer, tableName, "", -1, -1)
}
func NewClassNames(classNamer GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, totalSections int) (clsNames ClassNames, err error) {
	clsNames.InEntityClassName, err = classNamer.ComposeEntityClassName(tableName)
	if err != nil {
		err = eh.Errorf("unable to generate entity class name: %w", err)
		return
	}
	if sectionName != "" {
		clsNames.InSectionClassName, err = classNamer.ComposeSectionClassName(tableName, sectionName, sectionIndex, totalSections)
		if err != nil {
			err = eh.Errorf("unable to generate section class name: %w", err)
			return
		}
		clsNames.InAttributeClassName, err = classNamer.ComposeAttributeClassName(tableName, sectionName, sectionIndex, totalSections)
		if err != nil {
			err = eh.Errorf("unable to generate attribute class name: %w", err)
			return
		}
	}
	return
}
