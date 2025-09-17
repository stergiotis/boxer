package common

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

func extractScalarModifier(ct canonicaltypes2.PrimitiveAstNodeI) (scalarModifier canonicaltypes2.ScalarModifierE, err error) {
	switch ctt := ct.(type) {
	case canonicaltypes2.StringAstNode:
		scalarModifier = ctt.ScalarModifier
		break
	case canonicaltypes2.MachineNumericTypeAstNode:
		scalarModifier = ctt.ScalarModifier
		break
	case canonicaltypes2.TemporalTypeAstNode:
		scalarModifier = ctt.ScalarModifier
		break
	default:
		err = eb.Build().Type("ct", ct).Errorf("unable to extract scalar modifier")
		return
	}
	return
}
func addHomogenousArraySupportColumn(dest *IntermediateColumnProps) (err error) {
	hints := encodingaspects.EncodeAspectsMustValidate(encodingaspects.AspectLightGeneralCompression, encodingaspects.AspectLightBiasSmallInteger)
	dest.Add(naming.MustBeValidStylableName(ColumnRoleLength.String()), ColumnRoleLength, canonicaltypes2.MachineNumericTypeAstNode{
		BaseType:          canonicaltypes2.BaseTypeMachineNumericUnsigned,
		Width:             64,
		ByteOrderModifier: canonicaltypes2.ByteOrderModifierNone,
		ScalarModifier:    canonicaltypes2.ScalarModifierNone,
	}, hints, valueaspects.EmptyAspectSet)
	return
}
func addSetSupportColumn(dest *IntermediateColumnProps, role ColumnRoleE) (err error) {
	hints := encodingaspects.EncodeAspectsMustValidate(encodingaspects.AspectLightGeneralCompression, encodingaspects.AspectHeavyBiasSmallInteger)
	dest.Add(naming.MustBeValidStylableName(role.String()), role, canonicaltypes2.MachineNumericTypeAstNode{
		BaseType:          canonicaltypes2.BaseTypeMachineNumericUnsigned,
		Width:             64,
		ByteOrderModifier: canonicaltypes2.ByteOrderModifierNone,
		ScalarModifier:    canonicaltypes2.ScalarModifierNone,
	}, hints, valueaspects.EmptyAspectSet)
	return
}
func NewIntermediateColumnsProps() *IntermediateColumnProps {
	inst := &IntermediateColumnProps{}
	inst.Reserve(8)
	return inst
}
func (inst *IntermediateColumnProps) Reset() {
	inst.Names = inst.Names[:0]
	inst.Roles = inst.Roles[:0]
	inst.CanonicalType = inst.CanonicalType[:0]
	inst.EncodingHints = inst.EncodingHints[:0]
	inst.ValueSemantics = inst.ValueSemantics[:0]
}
func (inst *IntermediateColumnProps) Reserve(n int) {
	inst.Names = slices.Grow(inst.Names, n)
	inst.Roles = slices.Grow(inst.Roles, n)
	inst.CanonicalType = slices.Grow(inst.CanonicalType, n)
	inst.EncodingHints = slices.Grow(inst.EncodingHints, n)
	inst.ValueSemantics = slices.Grow(inst.ValueSemantics, n)
}
func (inst *IntermediateColumnProps) Slice(beginIncl int, endExcl int) (sliced IntermediateColumnProps) {
	sliced = IntermediateColumnProps{
		Names:          inst.Names[beginIncl:endExcl],
		Roles:          inst.Roles[beginIncl:endExcl],
		CanonicalType:  inst.CanonicalType[beginIncl:endExcl],
		EncodingHints:  inst.EncodingHints[beginIncl:endExcl],
		ValueSemantics: inst.ValueSemantics[beginIncl:endExcl],
	}
	return
}
func (inst *IntermediateColumnProps) Add(name naming.StylableName, role ColumnRoleE, ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects.AspectSet, valueSemantics valueaspects.AspectSet) {
	inst.Names = append(inst.Names, name)
	inst.Roles = append(inst.Roles, role)
	inst.CanonicalType = append(inst.CanonicalType, ct)
	inst.EncodingHints = append(inst.EncodingHints, hints)
	inst.ValueSemantics = append(inst.ValueSemantics, valueSemantics)
}
func (inst *IntermediateColumnProps) Length() int {
	return len(inst.Names)
}
func (inst *IntermediateColumnProps) IsEmpty() (empty bool) {
	return len(inst.Names) == 0
}
func NewIntermediateTaggedValueDesc() *IntermediateTaggedValuesDesc {
	return &IntermediateTaggedValuesDesc{
		SectionName:                     "",
		UseAspects:                      "",
		Scalar:                          NewIntermediateColumnsProps(),
		NonScalarHomogenousArray:        NewIntermediateColumnsProps(),
		NonScalarHomogenousArraySupport: NewIntermediateColumnsProps(),
		NonScalarSet:                    NewIntermediateColumnsProps(),
		NonScalarSetSupport:             NewIntermediateColumnsProps(),
		Membership:                      NewIntermediateColumnsProps(),
		MembershipSupport:               NewIntermediateColumnsProps(),
		CoSectionGroup:                  "",
	}
}
func (inst *IntermediateTaggedValuesDesc) Reset() {
	inst.SectionName = ""
	inst.UseAspects = ""
	inst.Scalar.Reset()
	inst.NonScalarHomogenousArray.Reset()
	inst.NonScalarHomogenousArraySupport.Reset()
	inst.NonScalarSet.Reset()
	inst.NonScalarSetSupport.Reset()
	inst.Membership.Reset()
	inst.MembershipSupport.Reset()
	inst.CoSectionGroup = ""
}
func (inst *IntermediateTaggedValuesDesc) LoadSection(sec *TaggedValuesSection, tech TechnologySpecificMembershipSetGenI) (err error) {
	inst.CoSectionGroup = sec.CoSectionGroup
	err = inst.loadSectionValue(sec)
	if err != nil {
		return
	}
	err = inst.loadSectionMembership(sec, tech)
	if err != nil {
		return
	}
	return
}

var ErrUnhandledMembershipSpec = eh.Errorf("unhandled membership specification")

func (inst *IntermediateTaggedValuesDesc) loadSectionMembership(sec *TaggedValuesSection, tech TechnologySpecificMembershipSetGenI) (err error) {
	inst.Membership.Reserve(sec.MembershipSpec.Count() + 2)
	for m := range sec.MembershipSpec.Iterate() {
		var ct1, ct2 canonicaltypes2.PrimitiveAstNodeI
		var hints1, hints2 encodingaspects.AspectSet
		var role1, role2, cardRole ColumnRoleE
		ct1, hints1, role1, ct2, hints2, role2, cardRole, err = tech.ResolveMembership(m)
		if err != nil {
			err = eh.Errorf("unable to get membership column canonical type: %w", err)
			return
		}
		if !ct1.IsScalar() || (m.ContainsMixed() && !ct2.IsScalar()) {
			err = eb.Build().Stringer("sectionName", sec.Name).Stringer("membership", m).Errorf("currently only scalar membership values are possible")
			return
		}
		inst.Membership.Add(naming.StylableName(role1.String()), role1, ct1, hints1, valueaspects.EmptyAspectSet)
		if m.ContainsMixed() {
			inst.Membership.Add(naming.StylableName(role2.String()), role2, ct2, hints2, valueaspects.EmptyAspectSet)
		}
		err = addSetSupportColumn(inst.MembershipSupport, cardRole)
		if err != nil {
			err = eh.Errorf("unable to add support column: %w", err)
			return
		}
	}
	return
}
func (inst *IntermediateTaggedValuesDesc) loadSectionValue(sec *TaggedValuesSection) (err error) {
	inst.SectionName = sec.Name
	inst.UseAspects = sec.UseAspects
	for i, name := range sec.ValueColumnNames {
		cts := sec.ValueColumnTypes[i]
		hints := sec.ValueEncodingHints[i]
		for ct := range cts.IterateMembers() {
			var scalarModifier canonicaltypes2.ScalarModifierE
			scalarModifier, err = extractScalarModifier(ct)
			if err != nil {
				return
			}
			valueSemantics := sec.ValueSemantics[i]
			switch scalarModifier {
			case canonicaltypes2.ScalarModifierNone:
				inst.Scalar.Add(name, ColumnRoleValue, ct, hints, valueSemantics)
				break
			case canonicaltypes2.ScalarModifierHomogenousArray:
				inst.NonScalarHomogenousArray.Add(name, ColumnRoleValue, ct, hints, valueSemantics)
				break
			case canonicaltypes2.ScalarModifierSet:
				inst.NonScalarSet.Add(name, ColumnRoleValue, ct, hints, valueSemantics)
				break
			default:
				err = eb.Build().Stringer("scalarModifier", scalarModifier).Errorf("unhandled scalar modifier")
				return
			}
		}
	}
	if !inst.NonScalarHomogenousArray.IsEmpty() {
		err = addHomogenousArraySupportColumn(inst.NonScalarHomogenousArraySupport)
		if err != nil {
			err = eh.Errorf("unable to add support column: %w", err)
			return
		}
	}
	if !inst.NonScalarSet.IsEmpty() {
		err = addSetSupportColumn(inst.NonScalarSetSupport, ColumnRoleCardinality)
		if err != nil {
			err = eh.Errorf("unable to add support column: %w", err)
			return
		}
	}

	return
}

func NewIntermediatePlainValueDesc() *IntermediatePlainValuesDesc {
	return &IntermediatePlainValuesDesc{
		ItemType:                        PlainItemTypeNone,
		Scalar:                          NewIntermediateColumnsProps(),
		NonScalarHomogenousArray:        NewIntermediateColumnsProps(),
		NonScalarHomogenousArraySupport: NewIntermediateColumnsProps(),
		NonScalarSet:                    NewIntermediateColumnsProps(),
		NonScalarSetSupport:             NewIntermediateColumnsProps(),
		StreamingGroup:                  "",
	}
}
func (inst *IntermediatePlainValuesDesc) Reset() {
	inst.Scalar.Reset()
	inst.NonScalarHomogenousArray.Reset()
	inst.NonScalarHomogenousArraySupport.Reset()
	inst.NonScalarSet.Reset()
	inst.NonScalarSetSupport.Reset()
	inst.StreamingGroup = ""
}
func (inst *IntermediatePlainValuesDesc) Load(names []naming.StylableName, ctss []canonicaltypes2.AstNodeI, hintss []encodingaspects.AspectSet, ss []valueaspects.AspectSet, streamingGroup naming.Key) (err error) {
	inst.StreamingGroup = streamingGroup
	for i, attrName := range names {
		cts := ctss[i]
		hints := hintss[i]
		for ct := range cts.IterateMembers() {
			var scalarModifier canonicaltypes2.ScalarModifierE
			scalarModifier, err = extractScalarModifier(ct)
			if err != nil {
				return
			}
			switch scalarModifier {
			case canonicaltypes2.ScalarModifierNone:
				inst.Scalar.Add(attrName, ColumnRoleValue, ct, hints, ss[i])
				break
			case canonicaltypes2.ScalarModifierHomogenousArray:
				inst.NonScalarHomogenousArray.Add(attrName, ColumnRoleValue, ct, hints, ss[i])
				break
			case canonicaltypes2.ScalarModifierSet:
				inst.NonScalarSet.Add(attrName, ColumnRoleValue, ct, hints, ss[i])
				break
			default:
				err = eb.Build().Stringer("scalarModifier", scalarModifier).Errorf("unhandled scalar modifier")
			}
		}
	}
	err = inst.addSupportColumns()
	if err != nil {
		return
	}
	return
}
func (inst *IntermediatePlainValuesDesc) LoadSingle(name naming.StylableName, ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects.AspectSet, vs valueaspects.AspectSet, streamingGroup naming.Key) (err error) {
	inst.StreamingGroup = streamingGroup
	var scalarModifier canonicaltypes2.ScalarModifierE
	scalarModifier, err = extractScalarModifier(ct)
	if err != nil {
		return
	}
	switch scalarModifier {
	case canonicaltypes2.ScalarModifierNone:
		inst.Scalar.Add(name, ColumnRoleValue, ct, hints, vs)
		break
	case canonicaltypes2.ScalarModifierHomogenousArray:
		inst.NonScalarHomogenousArray.Add(name, ColumnRoleValue, ct, hints, vs)
		break
	case canonicaltypes2.ScalarModifierSet:
		inst.NonScalarSet.Add(name, ColumnRoleValue, ct, hints, vs)
		break
	default:
		err = eb.Build().Stringer("scalarModifier", scalarModifier).Errorf("unhandled scalar modifier")
		return
	}
	return
}
func (inst *IntermediatePlainValuesDesc) addSupportColumns() (err error) {
	if !inst.NonScalarHomogenousArray.IsEmpty() {
		err = addHomogenousArraySupportColumn(inst.NonScalarHomogenousArraySupport)
		if err != nil {
			return
		}
	}
	if !inst.NonScalarSet.IsEmpty() {
		err = addSetSupportColumn(inst.NonScalarSetSupport, ColumnRoleCardinality)
		if err != nil {
			return
		}
	}
	return
}
func (inst *IntermediatePlainValuesDesc) Length() int {
	return inst.Scalar.Length() +
		inst.NonScalarHomogenousArray.Length() +
		inst.NonScalarSet.Length() +
		inst.NonScalarHomogenousArraySupport.Length() +
		inst.NonScalarSetSupport.Length()
}

func NewIntermediateTableRepresentation() *IntermediateTableRepresentation {
	inst := &IntermediateTableRepresentation{}
	inst.Reset()
	return inst
}
func (inst *IntermediateTableRepresentation) Reset() {
	clear(inst.PlainValueDesc)
	clear(inst.TaggedValueDesc)
	inst.PlainValueDesc = initSlice(inst.PlainValueDesc, int(MaxPlainItemTypeExcl))
	inst.TaggedValueDesc = initSlice(inst.TaggedValueDesc, 128)
}
func (inst *IntermediateTableRepresentation) plainValueDescByItemType(itemType PlainItemTypeE) (r *IntermediatePlainValuesDesc) {
	for _, p := range inst.PlainValueDesc {
		if p.ItemType == itemType {
			r = p
			return
		}
	}
	r = NewIntermediatePlainValueDesc()
	r.ItemType = itemType
	inst.PlainValueDesc = append(inst.PlainValueDesc, r)
	return
}
func (inst *IntermediateTableRepresentation) LoadFromTable(table *TableDesc, tech TechnologySpecificMembershipSetGenI) (err error) {
	for i, itemType := range table.PlainValuesItemTypes {
		dest := inst.plainValueDescByItemType(itemType)
		var streamingGroup naming.Key
		switch itemType {
		case PlainItemTypeOpaque:
			streamingGroup = table.OpaqueStreamingGroup
			break
		}
		err = dest.LoadSingle(table.PlainValuesNames[i], table.PlainValuesTypes[i], table.PlainValuesEncodingHints[i], table.PlainValuesValueSemantics[i], streamingGroup)
		if err != nil {
			return err
		}
	}

	for _, sec := range table.TaggedValuesSections {
		r := NewIntermediateTaggedValueDesc()
		err = r.LoadSection(&sec, tech)
		if err != nil {
			err = eb.Build().Stringer("sectionName", sec.Name).Errorf("unable to load section: %w", err)
			return
		}
		inst.TaggedValueDesc = append(inst.TaggedValueDesc, r)
	}
	return
}

func (inst *IntermediateTableRepresentation) IterateColumnProps() IntermediateColumnIterator {
	return func(yield func(IntermediateColumnContext, *IntermediateColumnProps) bool) {
		var indexOffset uint32
		for _, p := range inst.PlainValueDesc {
			for cc, cp := range p.IterateColumnProps(p.ItemType, indexOffset) {
				if !yield(cc, cp) {
					return
				}
			}
			indexOffset += uint32(p.Length())
		}
		for _, t := range inst.TaggedValueDesc {
			for cc, cp := range t.IterateColumnProps(t.SectionName, t.UseAspects, indexOffset) {
				if !yield(cc, cp) {
					return
				}
			}
			indexOffset += uint32(t.Length())
		}
	}
}
func (inst *IntermediateTableRepresentation) Length() (nPlain int, nTagged int) {
	for _, p := range inst.PlainValueDesc {
		nPlain += p.Length()
	}
	for _, t := range inst.TaggedValueDesc {
		nTagged += t.Length()
	}
	return
}
func (inst *IntermediateTableRepresentation) TotalLength() (nPlainPlusTagged int) {
	nPlain, nTagged := inst.Length()
	nPlainPlusTagged = nPlain + nTagged
	return
}
func (inst *IntermediateTaggedValuesDesc) Length() int {
	return inst.Scalar.Length() +
		inst.NonScalarHomogenousArray.Length() +
		inst.NonScalarSet.Length() +
		inst.Membership.Length() +
		inst.NonScalarHomogenousArraySupport.Length() +
		inst.NonScalarSetSupport.Length() +
		inst.MembershipSupport.Length()
}

func (inst *IntermediateTaggedValuesDesc) IterateColumnProps(sectionName naming.StylableName, asp useaspects.AspectSet, indexOffset uint32) IntermediateColumnIterator {
	return func(yield func(IntermediateColumnContext, *IntermediateColumnProps) bool) {
		cc := IntermediateColumnContext{
			Scope:          IntermediateColumnScopeTagged,
			SubType:        IntermediateColumnsSubTypeScalar,
			PlainItemType:  PlainItemTypeNone,
			IndexOffset:    0,
			SectionName:    sectionName,
			UseAspects:     asp,
			CoSectionGroup: inst.CoSectionGroup,
			StreamingGroup: inst.StreamingGroup,
		}
		if inst.Scalar != nil && inst.Scalar.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeScalar
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.Scalar) {
				return
			}
			indexOffset += uint32(inst.Scalar.Length())
		}
		if inst.NonScalarHomogenousArray != nil && inst.NonScalarHomogenousArray.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeHomogenousArray
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.NonScalarHomogenousArray) {
				return
			}
			indexOffset += uint32(inst.NonScalarHomogenousArray.Length())
		}
		if inst.NonScalarSet != nil && inst.NonScalarSet.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeSet
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.NonScalarSet) {
				return
			}
			indexOffset += uint32(inst.NonScalarSet.Length())
		}
		if inst.Membership != nil && inst.Membership.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeMembership
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.Membership) {
				return
			}
			indexOffset += uint32(inst.Membership.Length())
		}
		if inst.NonScalarHomogenousArraySupport != nil && inst.NonScalarHomogenousArraySupport.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeHomogenousArraySupport
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.NonScalarHomogenousArraySupport) {
				return
			}
			indexOffset += uint32(inst.NonScalarHomogenousArraySupport.Length())
		}
		if inst.NonScalarSetSupport != nil && inst.NonScalarSetSupport.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeSetSupport
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.NonScalarSetSupport) {
				return
			}
			indexOffset += uint32(inst.NonScalarSetSupport.Length())
		}
		if inst.MembershipSupport != nil && inst.MembershipSupport.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeMembershipSupport
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.MembershipSupport) {
				return
			}
			indexOffset += uint32(inst.MembershipSupport.Length())
		}
	}
}

func (inst *IntermediatePlainValuesDesc) IterateColumnProps(itemType PlainItemTypeE, indexOffset uint32) IntermediateColumnIterator {
	return func(yield func(IntermediateColumnContext, *IntermediateColumnProps) bool) {
		cc := IntermediateColumnContext{
			Scope:          inst.ItemType.GetIntermediateColumnScope(),
			SubType:        IntermediateColumnsSubTypeScalar,
			PlainItemType:  itemType,
			IndexOffset:    indexOffset,
			SectionName:    "",
			UseAspects:     useaspects.EmptyAspectSet,
			CoSectionGroup: "",
			StreamingGroup: "",
		}
		switch itemType {
		case PlainItemTypeOpaque:
			cc.StreamingGroup = inst.StreamingGroup
			break
		}
		if inst.Scalar != nil && inst.Scalar.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeScalar
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.Scalar) {
				return
			}
			indexOffset += uint32(inst.Scalar.Length())
		}
		if inst.NonScalarHomogenousArray != nil && inst.NonScalarHomogenousArray.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeHomogenousArray
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.NonScalarHomogenousArray) {
				return
			}
			indexOffset += uint32(inst.NonScalarHomogenousArray.Length())
		}
		if inst.NonScalarSet != nil && inst.NonScalarSet.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeSet
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.NonScalarSet) {
				return
			}
			indexOffset += uint32(inst.NonScalarSet.Length())
		}
		if inst.NonScalarHomogenousArraySupport != nil && inst.NonScalarHomogenousArraySupport.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeHomogenousArraySupport
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.NonScalarHomogenousArraySupport) {
				return
			}
			indexOffset += uint32(inst.NonScalarHomogenousArraySupport.Length())
		}
		if inst.NonScalarSetSupport != nil && inst.NonScalarSetSupport.Length() > 0 {
			cc.SubType = IntermediateColumnsSubTypeSetSupport
			cc.IndexOffset = indexOffset
			if !yield(cc, inst.NonScalarSetSupport) {
				return
			}
			indexOffset += uint32(inst.NonScalarSetSupport.Length())
		}
	}
}
func (inst IntermediateColumnContext) IsPlainColumn() bool {
	return inst.PlainItemType != PlainItemTypeNone
}
func (inst IntermediateColumnContext) IsTaggedColumn() bool {
	return inst.PlainItemType == PlainItemTypeNone
}
