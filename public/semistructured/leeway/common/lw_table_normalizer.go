package common

import (
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/containers/co"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type TableNormalizer struct {
	namingStyle NamingStyleE
	validator   *TableValidator
}

func NewTableNormalizer(namingStyle NamingStyleE) *TableNormalizer {
	return &TableNormalizer{
		namingStyle: namingStyle,
		validator:   NewTableValidator(),
	}
}
func (inst *TableNormalizer) Equal(other *TableNormalizer) (same bool) {
	return inst.namingStyle == other.namingStyle
}
func (inst *TableNormalizer) Scramble(table *TableDesc, rnd *rand.Rand) {

}
func (inst *TableNormalizer) Normalize(table *TableDesc) (nameChanges bool, reorderPlain bool, reorderTagged bool, err error) {
	err = inst.validator.ValidateTable(table)
	if err != nil {
		err = eh.Errorf("can not normalize invalid table, would be too dangerous: %w", err)
		return
	}
	nameChanges = inst.normalizeNames(table)
	reorderPlain, reorderTagged = inst.normalizeOrder(table)
	return
}
func (inst *TableNormalizer) normalizeOrder(table *TableDesc) (reorderPlain bool, reorderTagged bool) {
	if !slices.IsSorted(table.PlainValuesNames) {
		reorderPlain = true
		co.CoSortSlices(table.PlainValuesNames, func(i int, j int) {
			table.PlainValuesItemTypes[j], table.PlainValuesItemTypes[i] = table.PlainValuesItemTypes[i], table.PlainValuesItemTypes[j]
			table.PlainValuesValueSemantics[j], table.PlainValuesValueSemantics[i] = table.PlainValuesValueSemantics[i], table.PlainValuesValueSemantics[j]
			table.PlainValuesEncodingHints[j], table.PlainValuesEncodingHints[i] = table.PlainValuesEncodingHints[i], table.PlainValuesEncodingHints[j]
			table.PlainValuesTypes[j], table.PlainValuesTypes[i] = table.PlainValuesTypes[i], table.PlainValuesTypes[j]
		})
	}
	if !slices.IsSortedFunc(table.TaggedValuesSections, func(a, b TaggedValuesSection) int {
		// Note: Names are normalized, therefor comparison is correct
		return strings.Compare(string(a.Name), string(b.Name))
	}) {
		reorderTagged = true
		slices.SortFunc(table.TaggedValuesSections, func(a, b TaggedValuesSection) int {
			return strings.Compare(string(a.Name), string(b.Name))
		})
	}
	for k := 0; k < len(table.TaggedValuesSections); k++ {
		if !slices.IsSorted(table.TaggedValuesSections[k].ValueColumnNames) {
			reorderTagged = true
			co.CoSortSlices(table.TaggedValuesSections[k].ValueColumnNames, func(i int, j int) {
				table.TaggedValuesSections[k].ValueSemantics[j], table.TaggedValuesSections[k].ValueSemantics[i] =
					table.TaggedValuesSections[k].ValueSemantics[i], table.TaggedValuesSections[k].ValueSemantics[j]
				table.TaggedValuesSections[k].ValueEncodingHints[j], table.TaggedValuesSections[k].ValueEncodingHints[i] =
					table.TaggedValuesSections[k].ValueEncodingHints[i], table.TaggedValuesSections[k].ValueEncodingHints[j]
				table.TaggedValuesSections[k].ValueColumnTypes[j], table.TaggedValuesSections[k].ValueColumnTypes[i] =
					table.TaggedValuesSections[k].ValueColumnTypes[i], table.TaggedValuesSections[k].ValueColumnTypes[j]
			})
		}
	}
	return
}
func (inst *TableNormalizer) scrambleOrder(table *TableDesc, rnd *rand.Rand) {
	rnd.Shuffle(len(table.PlainValuesNames), func(i, j int) {
		table.PlainValuesNames[j], table.PlainValuesNames[i] = table.PlainValuesNames[i], table.PlainValuesNames[j]
		table.PlainValuesItemTypes[j], table.PlainValuesItemTypes[i] = table.PlainValuesItemTypes[i], table.PlainValuesItemTypes[j]
		table.PlainValuesValueSemantics[j], table.PlainValuesValueSemantics[i] = table.PlainValuesValueSemantics[i], table.PlainValuesValueSemantics[j]
		table.PlainValuesEncodingHints[j], table.PlainValuesEncodingHints[i] = table.PlainValuesEncodingHints[i], table.PlainValuesEncodingHints[j]
		table.PlainValuesTypes[j], table.PlainValuesTypes[i] = table.PlainValuesTypes[i], table.PlainValuesTypes[j]
	})
	rnd.Shuffle(len(table.TaggedValuesSections), func(i, j int) {
		table.TaggedValuesSections[j], table.TaggedValuesSections[i] = table.TaggedValuesSections[i], table.TaggedValuesSections[j]
	})
	for k := 0; k < len(table.TaggedValuesSections); k++ {
		rnd.Shuffle(len(table.TaggedValuesSections[k].ValueColumnNames), func(i, j int) {
			table.TaggedValuesSections[k].ValueColumnNames[j], table.TaggedValuesSections[k].ValueColumnNames[i] =
				table.TaggedValuesSections[k].ValueColumnNames[i], table.TaggedValuesSections[k].ValueColumnNames[j]
			table.TaggedValuesSections[k].ValueSemantics[j], table.TaggedValuesSections[k].ValueSemantics[i] =
				table.TaggedValuesSections[k].ValueSemantics[i], table.TaggedValuesSections[k].ValueSemantics[j]
			table.TaggedValuesSections[k].ValueEncodingHints[j], table.TaggedValuesSections[k].ValueEncodingHints[i] =
				table.TaggedValuesSections[k].ValueEncodingHints[i], table.TaggedValuesSections[k].ValueEncodingHints[j]
			table.TaggedValuesSections[k].ValueColumnTypes[j], table.TaggedValuesSections[k].ValueColumnTypes[i] =
				table.TaggedValuesSections[k].ValueColumnTypes[i], table.TaggedValuesSections[k].ValueColumnTypes[j]
		})
	}
	return
}
func (inst *TableNormalizer) normalizeNames(table *TableDesc) (changes bool) {
	ns := inst.namingStyle
	for i, name := range table.PlainValuesNames {
		newName := ConvertNameStyle(name, ns)
		changes = changes || newName != name
		table.PlainValuesNames[i] = newName
	}
	for i, sec := range table.TaggedValuesSections {
		{
			newName := ConvertNameStyle(sec.Name, ns)
			changes = changes || newName != sec.Name
			sec.Name = newName
		}
		for j, name := range sec.ValueColumnNames {
			newName := ConvertNameStyle(name, ns)
			changes = changes || newName != name
			sec.ValueColumnNames[j] = newName
		}
		table.TaggedValuesSections[i] = sec
	}
	return
}
func (inst *TableNormalizer) scrambleNames(table *TableDesc, rnd *rand.Rand) {
	l := len(AllNamingStyles)
	for i, name := range table.PlainValuesNames {
		ns := AllNamingStyles[rnd.IntN(l)]
		newName := ConvertNameStyle(name, ns)
		table.PlainValuesNames[i] = newName
	}
	for i, sec := range table.TaggedValuesSections {
		{
			ns := AllNamingStyles[rnd.IntN(l)]
			newName := ConvertNameStyle(sec.Name, ns)
			sec.Name = newName
		}
		for j, name := range sec.ValueColumnNames {
			ns := AllNamingStyles[rnd.IntN(l)]
			newName := ConvertNameStyle(name, ns)
			sec.ValueColumnNames[j] = newName
		}
		table.TaggedValuesSections[i] = sec
	}
	return
}
