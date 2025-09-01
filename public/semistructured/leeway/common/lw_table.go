package common

import (
	"slices"

	"github.com/stergiotis/boxer/public/containers/co"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicaltypes3 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

const plainItemEst = 8
const taggedSecEst = 4

func NewTableDesc() *TableDesc {
	inst := &TableDesc{}
	inst.Reset()
	return inst
}
func (inst *TableDesc) CountStructuredItemsByType(itemType PlainItemTypeE) (n int) {
	for _, t := range inst.PlainValuesItemTypes {
		if itemType == t {
			n++
		}
	}
	return
}
func (inst *TableDesc) Reset() {
	inst.PlainValuesNames = initSlice(inst.PlainValuesNames, plainItemEst)
	inst.PlainValuesTypes = initSlice(inst.PlainValuesTypes, plainItemEst)
	inst.PlainValuesEncodingHints = initSlice(inst.PlainValuesEncodingHints, plainItemEst)
	inst.PlainValuesItemTypes = initSlice(inst.PlainValuesItemTypes, plainItemEst)
	inst.PlainValuesValueSemantics = initSlice(inst.PlainValuesValueSemantics, plainItemEst)

	inst.DictionaryEntry.Name = ""
	inst.DictionaryEntry.Comment = ""
	inst.DictionaryEntry.Name = ""
	inst.DictionaryEntry.Comment = ""
	clear(inst.TaggedValuesSections)
	inst.TaggedValuesSections = initSlice(inst.TaggedValuesSections, taggedSecEst)
}
func (inst *TableDesc) AddPlainColumns(itemType PlainItemTypeE, names []naming.StylableName, canonicalTypes []string, encodingHints []encodingaspects.AspectSet, valueSemantics []valueaspects.AspectSet) (err error) {
	l := len(names)
	if l != len(encodingHints) || l != len(encodingHints) || l != len(canonicalTypes) || l != len(valueSemantics) {
		err = eh.Errorf("invalid arguments: all slices must be co-slices")
		return
	}
	if l == 0 {
		return
	}
	inst.PlainValuesNames = append(inst.PlainValuesNames, names...)
	inst.PlainValuesEncodingHints = append(inst.PlainValuesEncodingHints, encodingHints...)
	p := canonicaltypes3.NewParser()
	inst.PlainValuesTypes, err = parseAndCopyTypes(p, inst.PlainValuesTypes, canonicalTypes)
	if err != nil {
		err = eh.Errorf("unable to parse canonical types: %w", err)
		return
	}
	inst.PlainValuesItemTypes = slices.Grow(inst.PlainValuesItemTypes, l)
	for i := 0; i < l; i++ {
		inst.PlainValuesItemTypes = append(inst.PlainValuesItemTypes, itemType)
	}
	inst.PlainValuesValueSemantics = append(inst.PlainValuesValueSemantics, valueSemantics...)

	return
}
func (inst *TableDesc) AddTaggedValuesSections(secs []TaggedValuesSectionDto) (err error) {
	p := canonicaltypes3.NewParser()
	inst.TaggedValuesSections = slices.Grow(inst.TaggedValuesSections, len(secs))
	for _, sec := range secs {
		var types []canonicaltypes3.PrimitiveAstNodeI
		types, err = parseAndCopyTypes(p, nil, sec.ValueColumnTypes)
		if err != nil {
			err = eh.Errorf("unable to copy entity id types: %w", err)
			return
		}
		inst.TaggedValuesSections = append(inst.TaggedValuesSections, TaggedValuesSection{
			Name:               sec.Name,
			MembershipSpec:     sec.MembershipSpec,
			ValueColumnNames:   sec.ValueColumnNames,
			ValueColumnTypes:   types,
			ValueEncodingHints: sec.ValueColumnEncodingHints,
			ValueSemantics:     sec.ValueSemantics,
			UseAspects:         sec.UseAspects,
			CoSectionGroup:     "",
			StreamingGroup:     "",
		})
	}
	return
}

func (inst *TableDesc) LoadFrom(dto *TableDescDto) (err error) {
	inst.DictionaryEntry = dto.DictionaryEntry

	for _, t := range AllPlainItemTypes {
		if t == PlainItemTypeNone {
			continue
		}
		err = inst.AddPlainColumns(t, dto.GetPlainItemNames(t), dto.GetPlainItemTypes(t), dto.GetPlainItemEncodingHints(t), dto.GetPlainItemValueSemantics(t))
		if err != nil {
			err = eb.Build().Stringer("plainItemType", t).Errorf("unable to add structured items: %w", err)
			return
		}
	}
	err = inst.AddTaggedValuesSections(dto.TaggedValuesSections)
	if err != nil {
		err = eh.Errorf("unable to add semi structured sections: %w", err)
		return
	}
	return
}
func (inst *TableDesc) GetPlainItemNames(itemType PlainItemTypeE, in []naming.StylableName) (out []naming.StylableName) {
	out = inst.copyPlainItemNames(itemType, in)
	return
}
func (inst *TableDesc) GetPlainItemTypesStr(itemType PlainItemTypeE, in []string) (out []string, err error) {
	out, err = inst.serializeAndCopyStructuredTypes(itemType, in)
	return
}
func (inst *TableDesc) GetPlainItemTypes(itemType PlainItemTypeE, in []canonicaltypes3.AstNodeI) (out []canonicaltypes3.AstNodeI) {
	out = inst.copyPlainItemTypes(itemType, in)
	return
}
func (inst *TableDesc) GetPlainItemEncodingHints(itemType PlainItemTypeE, in []encodingaspects.AspectSet) (out []encodingaspects.AspectSet, err error) {
	out = inst.copyPlainItemEncodingHints(itemType, in)
	return
}
func (inst *TableDesc) GetPlainItemValueSemantics(itemType PlainItemTypeE, in []valueaspects.AspectSet) (out []valueaspects.AspectSet, err error) {
	out = inst.copyPlainItemValueSemantics(itemType, in)
	return
}
func (inst *TableDesc) LoadTo(dto *TableDescDto) (err error) {
	dto.DictionaryEntry = inst.DictionaryEntry
	tmp := make([]string, 0, 10)
	tmp2 := make([]encodingaspects.AspectSet, 0, 10)
	tmp3 := make([]valueaspects.AspectSet, 0, 10)
	tmp4 := make([]naming.StylableName, 0, 10)
	for _, t := range AllPlainItemTypes {
		if t == PlainItemTypeNone {
			continue
		}
		tmp4 = inst.copyPlainItemNames(t, tmp4[:0])
		dto.AddPlainItemNames(t, tmp4)

		tmp2 = inst.copyPlainItemEncodingHints(t, tmp2[:0])
		dto.AddPlainItemEncodingHints(t, tmp2)

		tmp3 = inst.copyPlainItemValueSemantics(t, tmp3[:0])
		dto.AddPlainItemValueSemantics(t, tmp3)

		tmp, err = inst.serializeAndCopyStructuredTypes(t, tmp[:0])
		if err != nil {
			// FIXME will render dto object invalid (destroy co-array structure)
			err = eb.Build().Stringer("plainItemType", t).Errorf("unable to copy types: %w", err)
			return
		}
		dto.AddPlainItemTypes(t, tmp)
	}

	dto.TaggedValuesSections = slices.Grow(dto.TaggedValuesSections, len(inst.TaggedValuesSections))
	for _, sec := range inst.TaggedValuesSections {
		types := make([]string, 0, len(sec.ValueColumnNames))
		for _, t := range sec.ValueColumnTypes {
			if !t.IsValid() {
				err = eb.Build().Stringer("section", sec.Name).Errorf("encountered invalid canonical type")
				return
			}
			types = append(types, t.String())
		}
		dto.TaggedValuesSections = append(dto.TaggedValuesSections, TaggedValuesSectionDto{
			Name:                     sec.Name,
			MembershipSpec:           sec.MembershipSpec,
			ValueColumnNames:         slices.Clone(sec.ValueColumnNames),
			ValueColumnTypes:         types,
			ValueColumnEncodingHints: slices.Clone(sec.ValueEncodingHints),
			ValueSemantics:           slices.Clone(sec.ValueSemantics),
			UseAspects:               sec.UseAspects,
			CoSectionGroup:           sec.CoSectionGroup,
			StreamingGroup:           sec.StreamingGroup,
		})
	}
	return
}
func (inst *TableDesc) serializeAndCopyStructuredTypes(itemType PlainItemTypeE, in []string) (out []string, err error) {
	out = in
	for _, t := range co.CoIterateFilter(inst.PlainValuesItemTypes, itemType, inst.PlainValuesTypes) {
		if !t.IsValid() {
			err = ErrInvalidType
			return
		}
		out = append(out, t.String())
	}
	return
}
func (inst *TableDesc) copyPlainItemNames(itemType PlainItemTypeE, in []naming.StylableName) (out []naming.StylableName) {
	out = in
	for _, n := range co.CoIterateFilter(inst.PlainValuesItemTypes, itemType, inst.PlainValuesNames) {
		out = append(out, n)
	}
	return
}
func (inst *TableDesc) copyPlainItemEncodingHints(itemType PlainItemTypeE, in []encodingaspects.AspectSet) (out []encodingaspects.AspectSet) {
	out = in
	for _, n := range co.CoIterateFilter(inst.PlainValuesItemTypes, itemType, inst.PlainValuesEncodingHints) {
		out = append(out, n)
	}
	return
}
func (inst *TableDesc) copyPlainItemValueSemantics(itemType PlainItemTypeE, in []valueaspects.AspectSet) (out []valueaspects.AspectSet) {
	out = in
	for _, n := range co.CoIterateFilter(inst.PlainValuesItemTypes, itemType, inst.PlainValuesValueSemantics) {
		out = append(out, n)
	}
	return
}
func (inst *TableDesc) copyPlainItemTypes(itemType PlainItemTypeE, in []canonicaltypes3.AstNodeI) (out []canonicaltypes3.AstNodeI) {
	out = in
	for _, n := range co.CoIterateFilter(inst.PlainValuesItemTypes, itemType, inst.PlainValuesTypes) {
		out = append(out, n)
	}
	return
}
func parseAndCopyTypes(p *canonicaltypes3.Parser, in []canonicaltypes3.PrimitiveAstNodeI, types []string) (out []canonicaltypes3.PrimitiveAstNodeI, err error) {
	if len(types) == 0 {
		out = []canonicaltypes3.PrimitiveAstNodeI{}
		return
	}
	out = slices.Grow(in, len(types))
	for _, t := range types {
		var a canonicaltypes3.PrimitiveAstNodeI
		a, err = p.ParsePrimitiveTypeAst(t)
		if err != nil {
			err = eh.Errorf("%w: %w", ErrInvalidType, err)
			return
		}
		out = append(out, a)
	}
	return
}
