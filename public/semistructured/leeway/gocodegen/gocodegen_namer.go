package gocodegen

import (
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func NewDefaultGoClassNamer() *DefaultGoClassNamer {
	return &DefaultGoClassNamer{}
}
func (inst *DefaultGoClassNamer) PromiseToBeReferentialTransparent() (_ functional.InterfaceIsReferentialTransparentType) {
	return
}

func NewMultiTablePerPackageGoClassNamer() *MultiTablePerPackageClassNamer {
	return &MultiTablePerPackageClassNamer{}
}
func NewClassNamesEntityOnly(classNamer GoClassNamerI, tableName naming.StylableName) (clsNames ClassNames, err error) {
	return NewClassNames(classNamer, tableName, "", -1, -1)
}
func NewClassNames(classNamer GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, totalSections int) (clsNames ClassNames, err error) {
	clsNames.ReadAccessEntityClassName, err = classNamer.ComposeEntityReadAccessClassName(tableName)
	if err != nil {
		err = eh.Errorf("unable to generate entity read access class name: %w", err)
		return
	}
	clsNames.InEntityClassName, err = classNamer.ComposeEntityDmlClassName(tableName)
	if err != nil {
		err = eh.Errorf("unable to generate entity class name: %w", err)
		return
	}
	if sectionName != "" {
		clsNames.InSectionClassName, err = classNamer.ComposeSectionDmlClassName(tableName, sectionName, sectionIndex, totalSections)
		if err != nil {
			err = eh.Errorf("unable to generate section class name: %w", err)
			return
		}
		clsNames.InAttributeClassName, err = classNamer.ComposeAttributeDmlClassName(tableName, sectionName, sectionIndex, totalSections)
		if err != nil {
			err = eh.Errorf("unable to generate attribute class name: %w", err)
			return
		}
	}
	return
}
