package gocodegen

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"golang.org/x/exp/constraints"
)

func numDigits[T constraints.Integer | constraints.Unsigned](n T) (nDigits int) {
	nDigits = len(fmt.Sprintf("%d", n))
	return
}
func (inst *DefaultGoClassNamer) ComposeEntityReadAccessClassName(tableName naming.StylableName) (className string, err error) {
	className = "ReadAccess" + tableName.Convert(naming.UpperCamelCase).String()
	return
}
func (inst *DefaultGoClassNamer) ComposeSectionReadAccessOuterSingleAttributeClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error) {
	className, err = inst.ComposeSectionReadAccessOuterClassName(tableName, itemType, sectionName)
	if err != nil {
		return
	}
	className += "Row"
	return
}
func (inst *DefaultGoClassNamer) ComposeSectionReadAccessInnerClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName, subType common.IntermediateColumnSubTypeE) (className string, err error) {
	className, err = inst.ComposeSectionReadAccessOuterClassName(tableName, itemType, sectionName)
	if err != nil {
		err = eh.Errorf("unable to generate outer class name: %w", err)
		return
	}
	className += naming.MustBeValidStylableName(subType.String()).Convert(naming.UpperCamelCase).String()
	return
}
func (inst *DefaultGoClassNamer) ComposeSectionReadAccessOuterClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error) {
	var suffix string
	if itemType == common.PlainItemTypeNone {
		suffix = "Tagged" + sectionName.Convert(naming.UpperCamelCase).String()
	} else {
		suffix = "Plain" + naming.MustBeValidStylableName(itemType.String()).Convert(naming.UpperCamelCase).String()
	}
	className = "ReadAccess" + suffix
	return
}

func (inst *DefaultGoClassNamer) ComposeSectionMembershipPackClassName(tableName naming.StylableName, sectionName naming.StylableName) (className string, err error) {
	if !tableName.IsValid() {
		err = eb.Build().Stringer("tableName", tableName).Errorf("tableName is invalid")
		return
	}
	if !sectionName.IsValid() {
		err = eb.Build().Stringer("sectionName", sectionName).Errorf("sectionName is invalid")
		return
	}
	className = fmt.Sprintf("MembershipPack%s%s", tableName.Convert(naming.UpperCamelCase), sectionName.Convert(naming.UpperCamelCase))
	return
}
func (inst *DefaultGoClassNamer) ComposeSharedMembershipPackClassName(tableName naming.StylableName, membershipSpec common.MembershipSpecE, i int, total int) (className string, err error) {
	if !tableName.IsValid() {
		err = eb.Build().Stringer("tableName", tableName).Errorf("tableName is invalid")
		return
	}
	className = fmt.Sprintf(fmt.Sprintf("MembershipPack%%sShared%%0%dd", numDigits(total)), tableName.Convert(naming.UpperCamelCase), i)
	return
}
func (inst *DefaultGoClassNamer) ComposeValueField(fieldNameIn string) (fieldNameOut string) {
	fieldNameOut = "Value" + fieldNameIn
	return
}
func (inst *DefaultGoClassNamer) ComposeValueFieldElementAccessor(fieldNameIn string) (fieldNameOut string) {
	fieldNameOut = "Value" + fieldNameIn + "Elements"
	return
}
func (inst *DefaultGoClassNamer) ComposeColumnIndexFieldName(fieldNameIn string) (fieldNameOut string) {
	fieldNameOut = "ColumnIndex" + fieldNameIn
	return
}
func (inst *DefaultGoClassNamer) ComposeAccelFieldName(fieldNameIn string) (fieldNameOut string) {
	fieldNameOut = "Accel" + fieldNameIn
	return
}

func (inst *MultiTablePerPackageClassNamer) ComposeSectionReadAccessInnerClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName, subType common.IntermediateColumnSubTypeE) (className string, err error) {
	className, err = inst.ComposeSectionReadAccessOuterClassName(tableName, itemType, sectionName)
	if err != nil {
		err = eh.Errorf("unable to generate outer class name: %w", err)
		return
	}
	className += naming.MustBeValidStylableName(subType.String()).Convert(naming.UpperCamelCase).String()
	return
}
func (inst *MultiTablePerPackageClassNamer) ComposeSectionReadAccessOuterClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error) {
	var suffix string
	if itemType == common.PlainItemTypeNone {
		suffix = "Tagged" + sectionName.Convert(naming.UpperCamelCase).String()
	} else {
		suffix = "Plain" + naming.MustBeValidStylableName(itemType.String()).Convert(naming.UpperCamelCase).String()
	}
	className = "ReadAccess" + tableName.Convert(naming.UpperCamelCase).String() + suffix
	return
}
func (inst *MultiTablePerPackageClassNamer) ComposeEntityReadAccessClassName(tableName naming.StylableName) (className string, err error) {
	className = "ReadAccess" + tableName.Convert(naming.UpperCamelCase).String()
	return
}
func (inst *MultiTablePerPackageClassNamer) ComposeSectionMembershipPackClassName(tableName naming.StylableName, sectionName naming.StylableName) (className string, err error) {
	if !tableName.IsValid() {
		err = eb.Build().Stringer("tableName", tableName).Errorf("tableName is invalid")
		return
	}
	if !sectionName.IsValid() {
		err = eb.Build().Stringer("sectionName", sectionName).Errorf("sectionName is invalid")
		return
	}
	className = fmt.Sprintf("MembershipPack%s%s", tableName.Convert(naming.UpperCamelCase), sectionName.Convert(naming.UpperCamelCase))
	return
}
func (inst *MultiTablePerPackageClassNamer) ComposeSharedMembershipPackClassName(tableName naming.StylableName, membershipSpec common.MembershipSpecE, i int, total int) (className string, err error) {
	if !tableName.IsValid() {
		err = eb.Build().Stringer("tableName", tableName).Errorf("tableName is invalid")
		return
	}
	className = fmt.Sprintf(fmt.Sprintf("MembershipPack%%sShared%%0%dd", numDigits(total)), tableName.Convert(naming.UpperCamelCase), i)
	return
}
func (inst *MultiTablePerPackageClassNamer) ComposeValueField(fieldNameIn string) (fieldNameOut string) {
	fieldNameOut = "Value" + fieldNameIn
	return
}
func (inst *MultiTablePerPackageClassNamer) ComposeValueFieldElementAccessor(fieldNameIn string) (fieldNameOut string) {
	fieldNameOut = "Value" + fieldNameIn + "Elements"
	return
}
func (inst *MultiTablePerPackageClassNamer) ComposeColumnIndexFieldName(fieldNameIn string) (fieldNameOut string) {
	fieldNameOut = "ColumnIndex" + fieldNameIn
	return
}
func (inst *MultiTablePerPackageClassNamer) ComposeAccelFieldName(fieldNameIn string) (fieldNameOut string) {
	fieldNameOut = "Accel" + fieldNameIn
	return
}
