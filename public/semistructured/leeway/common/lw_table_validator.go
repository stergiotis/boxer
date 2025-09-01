package common

import (
	"errors"

	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func NewTableValidator() *TableValidator {
	return &TableValidator{
		duplicatedNames:  containers.NewHashSet[string](128),
		usedNamingStyles: make([]uint32, len(naming.AllNamingStyles)),
		possibleNames:    make([]string, 0, 128),
		errors:           make([]error, 0, 128),
	}
}
func (inst *TableValidator) Reset() {
	inst.duplicatedNames.Clear()
	clear(inst.usedNamingStyles)
	clear(inst.errors)
	inst.errors = inst.errors[:0]
	inst.possibleNames = inst.possibleNames[:0]
}

func (inst *TableValidator) validateSectionName(name naming.StylableName) (err error) {
	return name.Validate()
}
func (inst *TableValidator) validateColumnName(name naming.StylableName) (err error) {
	return name.Validate()
}
func (inst *TableValidator) validateNames(names []naming.StylableName, nameType string) (err error) {
	d := inst.duplicatedNames
	d.Clear()
	u := inst.usedNamingStyles
	clear(u)
	for _, name := range names {
		err = inst.validateColumnName(name)
		if err != nil {
			return
		}
		matchesNamingStyle := false
		possibleNames := inst.possibleNames[:0]
		for _, s := range naming.AllNamingStyles {
			name2 := naming.ConvertNameStyle(name, s)
			possibleNames = append(possibleNames, string(name2))
			if name == name2 {
				u[s]++
				matchesNamingStyle = true
			}
			has := d.Has(string(name2))
			if has {
				err = eb.Build().Stringer("column", name).Stringer("namingStyle", s).Stringer("columnNameStyled", name2).Errorf("found duplicate %s name, must be unique in all naming styles", nameType)
				return
			}
		}
		for _, s := range naming.AllNamingStyles {
			d.Add(string(naming.ConvertNameStyle(name, s)))
		}
		if !matchesNamingStyle {
			err = eb.Build().Stringer("column", name).Strs("possibleNames", possibleNames).Errorf("found %s name that does not follow any of the supported naming conventions", nameType)
			return
		}
	}
	{
		all := false
		for _, t := range u {
			if t == uint32(len(names)) {
				all = true
				break
			}
		}
		if !all {
			err = eh.Errorf("found %s names in multiple naming styles", nameType)
			return
		}
	}
	return
}
func (inst *TableValidator) validateNamesTypes(names []naming.StylableName, types []canonicalTypes2.PrimitiveAstNodeI) (err error) {
	if len(names) != len(types) {
		err = eb.Build().Int("names", len(names)).Int("types", len(types)).Errorf("names and types do not have the same length")
		return
	}
	err = inst.validateNames(names, "column")
	if err != nil {
		return
	}
	for i, name := range names {
		typ := types[i]
		if !typ.IsValid() {
			err = eb.Build().Stringer("column", name).Errorf("canonical type of column is invalid")
			return
		}
	}
	return
}
func (inst *TableValidator) validateTable(table *TableDesc) {
	errs := inst.errors[:0]
	addErr := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	addErr(inst.validateNamesTypes(table.PlainValuesNames, table.PlainValuesTypes))
	inst.errors = errs
}
func (inst *TableValidator) validateSection(section TaggedValuesSection) {
	errs := inst.errors
	addErr := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	addErr(inst.validateSectionName(section.Name))
	if !section.UseAspects.IsValid() {
		addErr(eb.Build().Stringer("section", section.Name).Errorf("section aspects are not valid"))
	}
	addErr(inst.validateNamesTypes(section.ValueColumnNames, section.ValueColumnTypes))
	for _, t := range section.ValueEncodingHints {
		if !t.IsValid() {
			addErr(eb.Build().Stringer("section", section.Name).Errorf("section value column encoding hints are not valid"))
		}
	}
	err := section.StreamingGroup.Validate()
	if err != nil {
		err = eh.Errorf("streaming group is not a valid key: %w", err)
		addErr(err)
		err = nil
	}
	err = section.CoSectionGroup.Validate()
	if err != nil {
		err = eh.Errorf("co section group is not a valid key: %w", err)
		addErr(err)
		err = nil
	}
	inst.errors = errs
}
func (inst *TableValidator) buildError(errs []error) (err error) {
	if len(errs) > 0 {
		err = errors.Join(errs...)
	}
	return
}
func (inst *TableValidator) ValidateSection(section TaggedValuesSection) (err error) {
	errs := inst.errors[:0]
	inst.validateSection(section)
	return inst.buildError(errs)
}
func (inst *TableValidator) ValidateTable(table *TableDesc) (err error) {
	inst.validateTable(table)
	sectionNames := make([]naming.StylableName, 0, len(table.TaggedValuesSections))
	for _, section := range table.TaggedValuesSections {
		sectionNames = append(sectionNames, section.Name)
		inst.validateSection(section)
	}
	err = inst.validateNames(sectionNames, "section")
	if err != nil {
		inst.errors = append(inst.errors, err)
		err = nil
	}
	err = table.OpaqueStreamingGroup.Validate()
	if err != nil {
		err = eh.Errorf("opaque streaming group is not a valid key: %w", err)
		return
	}
	return inst.buildError(inst.errors)
}
