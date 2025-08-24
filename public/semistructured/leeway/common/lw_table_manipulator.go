package common

import (
	"bytes"
	"iter"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	useaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"golang.org/x/exp/maps"
)

func NewTableManipulator() (inst *TableManipulator, err error) {
	const estPlainValues = 4
	const estSections = 8
	var marshaller *TableMarshaller
	marshaller, err = NewTableMarshaller()
	if err != nil {
		err = eh.Errorf("unable to marshall table")
		return
	}
	plu := make([]map[string]int, 0, MaxPlainItemTypeExcl)
	for i := 0; i < int(MaxPlainItemTypeExcl); i++ {
		plu = append(plu, make(map[string]int, estPlainValues))
	}
	inst = &TableManipulator{
		marshaller:                marshaller,
		buffer:                    bytes.NewBuffer(make([]byte, 0, 4096*4)),
		receivedInvalidAspects:    false,
		upsertedCount:             0,
		plainValueItemNameToIndex: plu,
		sectionNameToIndex:        make(map[string]int, estSections),
		table:                     NewTableDesc(),
	}
	return
}

func (inst *TableManipulator) Reset() {
	inst.buffer.Reset()
	inst.table.Reset()
	for _, lu := range inst.plainValueItemNameToIndex {
		maps.Clear(lu)
	}
	maps.Clear(inst.sectionNameToIndex)
	inst.upsertedCount = 0
	inst.receivedInvalidAspects = false
}
func (inst *TableManipulator) BuildTableDesc() (tbl TableDesc, err error) {
	b := inst.buffer
	b.Reset()
	err = inst.marshaller.EncodeTableCbor(b, inst.table)
	if err != nil {
		err = eh.Errorf("unable to marshalling table: %w", err)
		return
	}
	err = inst.marshaller.DecodeTableCbor(b, &tbl)
	if err != nil {
		err = eh.Errorf("unable to unmarshalling table: %w", err)
		return
	}
	return
}
func (inst *TableManipulator) BuildTableDescDto() (dto TableDescDto, err error) {
	b := inst.buffer
	b.Reset()
	err = inst.marshaller.EncodeTableCbor(b, inst.table)
	if err != nil {
		err = eh.Errorf("unable to marshalling table: %w", err)
		return
	}
	dto.Reset() // reset to normalize representation ([]string{} vs []string(nil))
	err = inst.marshaller.DecodeDtoCbor(b, &dto)
	if err != nil {
		err = eh.Errorf("unable to unmarshalling table: %w", err)
		return
	}
	return
}

func (inst *TableManipulator) SetTableName(name naming.StylableName) *TableManipulator {
	inst.table.DictionaryEntry.Name = name
	return inst
}
func (inst *TableManipulator) SetTableComment(comment string) *TableManipulator {
	inst.table.DictionaryEntry.Comment = comment
	return inst
}
func (inst *TableManipulator) TaggedValueSection(sectionName naming.StylableName) TaggedValueSectionMerger {
	idx := inst.mergeTaggedValueSection(sectionName, useaspects2.EmptyAspectSet, MembershipSpecNone, "", "")
	return TaggedValueSectionMerger{
		table:        inst.table,
		manip:        inst,
		sectionIndex: idx,
	}
}
func (inst TaggedValueSectionMerger) TaggedValueColumn(name naming.StylableName, canonicalType canonicalTypes2.PrimitiveAstNodeI) TaggedValueColumnMerger {
	sectionIdx, columnIdx := inst.manip.mergeTaggedValueColumn(inst.table.TaggedValuesSections[inst.sectionIndex].Name, name, canonicalType, encodingaspects2.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects2.EmptyAspectSet, MembershipSpecNone, "", "")
	if sectionIdx != inst.sectionIndex {
		log.Panic().Stringer("name", name).Int("sectionIdx1", sectionIdx).Int("sectionIdx2", inst.sectionIndex).Msg("section index do not match, something is fundamentally wrong")
	}
	return TaggedValueColumnMerger{
		table:        inst.table,
		sectionIndex: sectionIdx,
		columnIndex:  columnIdx,
	}
}

func (inst *TableManipulator) PlainValueColumn(itemType PlainItemTypeE, name naming.StylableName, canonicalType canonicalTypes2.PrimitiveAstNodeI) PlainValueColumnMerger {
	idx := inst.addPlainValueItem(itemType, name, canonicalType, encodingaspects2.EmptyAspectSet, valueaspects.EmptyAspectSet)
	return PlainValueColumnMerger{
		table:       inst.table,
		columnIndex: idx,
	}
}
func (inst *TableManipulator) AddPlainValueItem(itemType PlainItemTypeE, name naming.StylableName, ct canonicalTypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet) *TableManipulator {
	_ = inst.addPlainValueItem(itemType, name, ct, hints, valueSemantics)
	return inst
}
func (inst *TableManipulator) addPlainValueItem(itemType PlainItemTypeE, name naming.StylableName, ct canonicalTypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet) (idx int) {
	lu := inst.plainValueItemNameToIndex[itemType]
	var has bool
	idx, has = lu[string(name)]
	if has {
		inst.upsertedCount++
		inst.table.PlainValuesTypes[idx] = ct
		inst.table.PlainValuesEncodingHints[idx] = encodingaspects2.UnionAspectsIgnoreInvalid(inst.table.PlainValuesEncodingHints[idx], hints)
		inst.table.PlainValuesValueSemantics[idx] = valueaspects.UnionAspectsIgnoreInvalid(inst.table.PlainValuesValueSemantics[idx], valueSemantics)
	} else {
		idx = len(inst.table.PlainValuesNames)
		lu[string(name)] = idx
		inst.table.PlainValuesNames = append(inst.table.PlainValuesNames, name)
		inst.table.PlainValuesTypes = append(inst.table.PlainValuesTypes, ct)
		inst.table.PlainValuesEncodingHints = append(inst.table.PlainValuesEncodingHints, hints)
		inst.table.PlainValuesItemTypes = append(inst.table.PlainValuesItemTypes, itemType)
		inst.table.PlainValuesValueSemantics = append(inst.table.PlainValuesValueSemantics, valueSemantics)
	}
	return idx
}
func (inst *TableManipulator) MergeTaggedValueSection(sectionName naming.StylableName, aspectSet useaspects2.AspectSet, membership MembershipSpecE, coSectionGroup naming.Key, streamingGroup naming.Key) *TableManipulator {
	_ = inst.mergeTaggedValueSection(sectionName, aspectSet, membership, coSectionGroup, streamingGroup)
	return inst
}
func (inst *TableManipulator) mergeTaggedValueSection(sectionName naming.StylableName, aspectSet useaspects2.AspectSet, membership MembershipSpecE, coSectionGroup naming.Key, streamingGroup naming.Key) (idx int) {
	const estColumns = 8
	var has bool
	idx, has = inst.sectionNameToIndex[string(sectionName)]
	if has {
		inst.table.TaggedValuesSections[idx].MembershipSpec |= membership
		inst.table.TaggedValuesSections[idx].UseAspects = useaspects2.UnionAspectsIgnoreInvalid(inst.table.TaggedValuesSections[idx].UseAspects, aspectSet)
		if coSectionGroup != "" {
			inst.table.TaggedValuesSections[idx].CoSectionGroup = coSectionGroup
		}
		if streamingGroup != "" {
			inst.table.TaggedValuesSections[idx].StreamingGroup = streamingGroup
		}
	} else {
		idx = len(inst.table.TaggedValuesSections)
		inst.sectionNameToIndex[string(sectionName)] = idx
		inst.table.TaggedValuesSections = append(inst.table.TaggedValuesSections, TaggedValuesSection{
			Name:               sectionName,
			MembershipSpec:     membership,
			ValueColumnNames:   make([]naming.StylableName, 0, estColumns),
			ValueColumnTypes:   make([]canonicalTypes2.PrimitiveAstNodeI, 0, estColumns),
			ValueEncodingHints: make([]encodingaspects2.AspectSet, 0, estColumns),
			ValueSemantics:     make([]valueaspects.AspectSet, 0, estColumns),
			UseAspects:         aspectSet,
			CoSectionGroup:     coSectionGroup,
			StreamingGroup:     streamingGroup,
		})
	}
	return
}
func (inst *TableManipulator) SetOpaqueColumnStreamingGroup(streamingGroup naming.Key) *TableManipulator {
	inst.table.OpaqueStreamingGroup = streamingGroup
	return inst
}
func (inst *TableManipulator) MergeTaggedValueColumn(sectionName naming.StylableName, columnName naming.StylableName, ct canonicalTypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet, aspectSet useaspects2.AspectSet, membership MembershipSpecE, coSectionGroup naming.Key, streamingGroup naming.Key) *TableManipulator {
	_, _ = inst.mergeTaggedValueColumn(sectionName, columnName, ct, hints, valueSemantics, aspectSet, membership, coSectionGroup, streamingGroup)
	return inst
}
func (inst *TableManipulator) mergeTaggedValueColumn(sectionName naming.StylableName, columnName naming.StylableName, ct canonicalTypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet, aspectSet useaspects2.AspectSet, membership MembershipSpecE, coSectionGroup naming.Key, streamingGroup naming.Key) (sectionIdx int, columnIdx int) {
	sectionIdx = inst.mergeTaggedValueSection(sectionName, aspectSet, membership, coSectionGroup, streamingGroup)
	columnIdx = slices.Index(inst.table.TaggedValuesSections[sectionIdx].ValueColumnNames, columnName)
	if columnIdx >= 0 {
		inst.upsertedCount++
		inst.table.TaggedValuesSections[sectionIdx].ValueColumnTypes[columnIdx] = ct
		inst.table.TaggedValuesSections[sectionIdx].ValueEncodingHints[columnIdx] = encodingaspects2.UnionAspectsIgnoreInvalid(inst.table.TaggedValuesSections[sectionIdx].ValueEncodingHints[columnIdx], hints)
		inst.table.TaggedValuesSections[sectionIdx].ValueSemantics[columnIdx] = valueaspects.UnionAspectsIgnoreInvalid(inst.table.TaggedValuesSections[sectionIdx].ValueSemantics[columnIdx], valueSemantics)
	} else {
		columnIdx = len(inst.table.TaggedValuesSections[sectionIdx].ValueColumnNames)
		inst.table.TaggedValuesSections[sectionIdx].ValueColumnNames = append(inst.table.TaggedValuesSections[sectionIdx].ValueColumnNames, columnName)
		inst.table.TaggedValuesSections[sectionIdx].ValueColumnTypes = append(inst.table.TaggedValuesSections[sectionIdx].ValueColumnTypes, ct)
		inst.table.TaggedValuesSections[sectionIdx].ValueEncodingHints = append(inst.table.TaggedValuesSections[sectionIdx].ValueEncodingHints, hints)
		inst.table.TaggedValuesSections[sectionIdx].ValueSemantics = append(inst.table.TaggedValuesSections[sectionIdx].ValueSemantics, valueSemantics)
	}
	return
}
func (inst *TableManipulator) LoadFromIntermediates(it iter.Seq2[IntermediateColumnContext, *IntermediateColumnProps]) (err error) {
	tmpStatesPlain := make([]intermediateTmpState, MaxPlainItemTypeExcl)
	tmpStatesTagged := make(map[string]intermediateTmpState, 128)
	sectionNames := make([]naming.StylableName, 0, 128)
	for cc, cp := range it {
		if cc.PlainItemType == PlainItemTypeNone {
			tmp, has := tmpStatesTagged[string(cc.SectionName)]
			if !has {
				sectionNames = append(sectionNames, cc.SectionName)
			}
			membership := tmp.membership
			for i, r := range cp.Roles {
				switch r {
				case ColumnRoleHighCardRef:
					membership = membership.AddHighCardRefOnly()
					break
				case ColumnRoleHighCardRefParametrized:
					membership = membership.AddHighCardRefParametrized()
					break
				case ColumnRoleHighCardVerbatim:
					membership = membership.AddHighCardVerbatim()
					break
				case ColumnRoleLowCardRef:
					membership = membership.AddLowCardRefOnly()
					break
				case ColumnRoleLowCardRefParametrized:
					membership = membership.AddLowCardRefParametrized()
					break
				case ColumnRoleLowCardVerbatim:
					membership = membership.AddLowCardVerbatim()
					break
				case ColumnRoleMixedLowCardVerbatim:
					membership = membership.AddMixedLowCardVerbatimHighCardParameters()
					break
				case ColumnRoleMixedRefHighCardParameters, ColumnRoleMixedVerbatimHighCardParameters:
					// mixed, trigger on other column
					break
				case ColumnRoleMixedLowCardRef:
					membership = membership.AddMixedLowCardRefHighCardParameters()
					break
				case ColumnRoleValue:
					switch cc.SubType {
					case IntermediateColumnsSubTypeHomogenousArraySupport,
						IntermediateColumnsSubTypeSetSupport,
						IntermediateColumnsSubTypeMembershipSupport:
						break
					case IntermediateColumnsSubTypeMembership:
						break
					case IntermediateColumnsSubTypeScalar,
						IntermediateColumnsSubTypeSet,
						IntermediateColumnsSubTypeHomogenousArray:
						tmp.names = append(tmp.names, cp.Names[i])
						tmp.cts = append(tmp.cts, cp.CanonicalType[i])
						tmp.hints = append(tmp.hints, cp.EncodingHints[i])
						tmp.valueSemantics = append(tmp.valueSemantics, cp.ValueSemantics[i])
						break
					default:
						err = eb.Build().Stringer("sectionName", cc.SectionName).Stringer("subType", cc.SubType).Stringer("plainItemType", cc.PlainItemType).Errorf("unhandled subtype")
						return
					}
					break
				case ColumnRoleHighCardRefCardinality,
					ColumnRoleHighCardRefParametrizedCardinality,
					ColumnRoleHighCardVerbatimCardinality,
					ColumnRoleLowCardRefCardinality,
					ColumnRoleLowCardRefParametrizedCardinality,
					ColumnRoleLowCardVerbatimCardinality,
					ColumnRoleMixedLowCardRefCardinality,
					ColumnRoleMixedLowCardVerbatimCardinality:
					break
				case ColumnRoleCardinality, ColumnRoleLength:
					break
				default:
					err = eb.Build().Stringer("role", r).Errorf("encountered unhandled column role")
					return
				}
			}
			tmp.membership = membership
			tmp.aspects = cc.UseAspects
			tmp.coSectionGroup = cc.CoSectionGroup
			tmp.streamingGroup = cc.StreamingGroup
			tmpStatesTagged[string(cc.SectionName)] = tmp
		} else {
			switch cc.PlainItemType {
			case PlainItemTypeOpaque:
				inst.SetOpaqueColumnStreamingGroup(cc.StreamingGroup)
				break
			}
			for i, role := range cp.Roles {
				if role == ColumnRoleValue {
					tmp := tmpStatesPlain[cc.PlainItemType]
					tmp.names = append(tmp.names, cp.Names[i])
					tmp.cts = append(tmp.cts, cp.CanonicalType[i])
					tmp.hints = append(tmp.hints, cp.EncodingHints[i])
					tmp.valueSemantics = append(tmp.valueSemantics, cp.ValueSemantics[i])
					tmp.streamingGroup = ""
					tmp.coSectionGroup = ""
					tmpStatesPlain[cc.PlainItemType] = tmp
				}
			}
		}
	}
	for _, sectionName := range sectionNames {
		tmp := tmpStatesTagged[string(sectionName)]
		inst.MergeTaggedValueSection(sectionName, tmp.aspects, tmp.membership, tmp.coSectionGroup, tmp.streamingGroup)
		for i, colName := range tmp.names {
			inst.MergeTaggedValueColumn(sectionName, colName, tmp.cts[i], tmp.hints[i], tmp.valueSemantics[i], tmp.aspects, tmp.membership, tmp.coSectionGroup, tmp.streamingGroup)
		}
	}

	for t, tmp := range tmpStatesPlain {
		if len(tmp.names) > 0 {
			for i, name := range tmp.names {
				inst.AddPlainValueItem(PlainItemTypeE(t), name, tmp.cts[i], tmp.hints[i], tmp.valueSemantics[i])
			}
		}
	}

	return
}
func (inst *TableManipulator) MergeTable(tbl *TableDesc) (err error) {
	for i, name := range tbl.PlainValuesNames {
		inst.AddPlainValueItem(tbl.PlainValuesItemTypes[i], name, tbl.PlainValuesTypes[i], tbl.PlainValuesEncodingHints[i], tbl.PlainValuesValueSemantics[i])
	}
	inst.SetOpaqueColumnStreamingGroup(tbl.OpaqueStreamingGroup)
	for _, sec := range tbl.TaggedValuesSections {
		inst.MergeTaggedValueSection(sec.Name, sec.UseAspects, sec.MembershipSpec, sec.CoSectionGroup, sec.StreamingGroup)
		for j, name := range sec.ValueColumnNames {
			inst.MergeTaggedValueColumn(sec.Name, name, sec.ValueColumnTypes[j], sec.ValueEncodingHints[j], sec.ValueSemantics[j], sec.UseAspects, sec.MembershipSpec, sec.CoSectionGroup, sec.StreamingGroup)
		}
	}
	return
}

func (inst TaggedValueSectionMerger) SectionName(sectionName naming.StylableName) TaggedValueSectionMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].Name = sectionName
	return inst
}
func (inst TaggedValueSectionMerger) AddSectionUseAspectSet(aspects useaspects2.AspectSet) TaggedValueSectionMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].UseAspects =
		inst.table.TaggedValuesSections[inst.sectionIndex].UseAspects.UnionAspectsIgnoreInvalid(aspects)
	return inst
}
func (inst TaggedValueSectionMerger) AddSectionUseAspects(aspects ...useaspects2.AspectE) TaggedValueSectionMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].UseAspects =
		inst.table.TaggedValuesSections[inst.sectionIndex].UseAspects.UnionAspectsIgnoreInvalid(useaspects2.EncodeAspectsIgnoreInvalid(aspects...))
	return inst
}
func (inst TaggedValueSectionMerger) ResetSectionUseAspects() TaggedValueSectionMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].UseAspects = useaspects2.EmptyAspectSet
	return inst
}
func (inst TaggedValueSectionMerger) AddSectionMembership(memberships ...MembershipSpecE) TaggedValueSectionMerger {
	for _, membership := range memberships {
		inst.table.TaggedValuesSections[inst.sectionIndex].MembershipSpec =
			inst.table.TaggedValuesSections[inst.sectionIndex].MembershipSpec | membership
	}
	return inst
}
func (inst TaggedValueSectionMerger) ClearSectionMembership(memberships ...MembershipSpecE) TaggedValueSectionMerger {
	for _, membership := range memberships {
		inst.table.TaggedValuesSections[inst.sectionIndex].MembershipSpec =
			inst.table.TaggedValuesSections[inst.sectionIndex].MembershipSpec & ^membership
	}
	return inst
}
func (inst TaggedValueSectionMerger) ResetSectionMembership() TaggedValueSectionMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].MembershipSpec = MembershipSpecNone
	return inst
}
func (inst TaggedValueSectionMerger) SectionCoSectionGroup(coSectionGroup naming.Key) TaggedValueSectionMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].CoSectionGroup = coSectionGroup
	return inst
}
func (inst TaggedValueSectionMerger) SectionStreamingGroup(streamingGroup naming.Key) TaggedValueSectionMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].StreamingGroup = streamingGroup
	return inst
}

func (inst TaggedValueColumnMerger) Section() TaggedValueSectionMerger {
	return TaggedValueSectionMerger{
		table:        inst.table,
		sectionIndex: inst.sectionIndex,
	}
}
func (inst TaggedValueColumnMerger) SetColumnName(columnName naming.StylableName) TaggedValueColumnMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].ValueColumnNames[inst.columnIndex] = columnName
	return inst
}
func (inst TaggedValueColumnMerger) SetColumnCanonicalType(ct canonicalTypes2.PrimitiveAstNodeI) TaggedValueColumnMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].ValueColumnTypes[inst.columnIndex] = ct
	return inst
}
func (inst TaggedValueColumnMerger) AddColumnEncodingHintSet(aspects encodingaspects2.AspectSet) TaggedValueColumnMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].ValueEncodingHints[inst.columnIndex] =
		inst.table.TaggedValuesSections[inst.sectionIndex].ValueEncodingHints[inst.columnIndex].UnionAspectsIgnoreInvalid(aspects)
	return inst
}
func (inst TaggedValueColumnMerger) AddColumnEncodingHints(aspects ...encodingaspects2.AspectE) TaggedValueColumnMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].ValueEncodingHints[inst.columnIndex] =
		inst.table.TaggedValuesSections[inst.sectionIndex].ValueEncodingHints[inst.columnIndex].UnionAspectsIgnoreInvalid(encodingaspects2.EncodeAspectsIgnoreInvalid(aspects...))
	return inst
}
func (inst TaggedValueColumnMerger) AddColumnValueSemanticSet(semantics valueaspects.AspectSet) TaggedValueColumnMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].ValueSemantics[inst.columnIndex] =
		inst.table.TaggedValuesSections[inst.sectionIndex].ValueSemantics[inst.columnIndex].UnionAspectsIgnoreInvalid(semantics)
	return inst
}
func (inst TaggedValueColumnMerger) AddColumnValueSemantics(semantics ...valueaspects.AspectE) TaggedValueColumnMerger {
	inst.table.TaggedValuesSections[inst.sectionIndex].ValueSemantics[inst.columnIndex] =
		inst.table.TaggedValuesSections[inst.sectionIndex].ValueSemantics[inst.columnIndex].UnionAspectsIgnoreInvalid(valueaspects.EncodeAspectsIgnoreInvalid(semantics...))
	return inst
}

func (inst PlainValueColumnMerger) SetColumnName(columnName naming.StylableName) PlainValueColumnMerger {
	inst.table.PlainValuesNames[inst.columnIndex] = columnName
	return inst
}
func (inst PlainValueColumnMerger) SetColumnCanonicalType(ct canonicalTypes2.PrimitiveAstNodeI) PlainValueColumnMerger {
	inst.table.PlainValuesTypes[inst.columnIndex] = ct
	return inst
}
func (inst PlainValueColumnMerger) AddColumnEncodingHintSet(aspects encodingaspects2.AspectSet) PlainValueColumnMerger {
	inst.table.PlainValuesEncodingHints[inst.columnIndex] =
		inst.table.PlainValuesEncodingHints[inst.columnIndex].UnionAspectsIgnoreInvalid(aspects)
	return inst
}
func (inst PlainValueColumnMerger) AddColumnEncodingHints(aspects ...encodingaspects2.AspectE) PlainValueColumnMerger {
	inst.table.PlainValuesEncodingHints[inst.columnIndex] =
		inst.table.PlainValuesEncodingHints[inst.columnIndex].UnionAspectsIgnoreInvalid(encodingaspects2.EncodeAspectsIgnoreInvalid(aspects...))
	return inst
}
func (inst PlainValueColumnMerger) AddColumnValueSemanticSet(semantics valueaspects.AspectSet) PlainValueColumnMerger {
	inst.table.PlainValuesValueSemantics[inst.columnIndex] =
		inst.table.PlainValuesValueSemantics[inst.columnIndex].UnionAspectsIgnoreInvalid(semantics)
	return inst
}
func (inst PlainValueColumnMerger) AddColumnValueSemantics(semantics ...valueaspects.AspectE) PlainValueColumnMerger {
	inst.table.PlainValuesValueSemantics[inst.columnIndex] =
		inst.table.PlainValuesValueSemantics[inst.columnIndex].UnionAspectsIgnoreInvalid(valueaspects.EncodeAspectsIgnoreInvalid(semantics...))
	return inst
}

type intermediateTmpState struct {
	aspects        useaspects2.AspectSet
	membership     MembershipSpecE
	coSectionGroup naming.Key
	streamingGroup naming.Key
	names          []naming.StylableName
	cts            []canonicalTypes2.PrimitiveAstNodeI
	hints          []encodingaspects2.AspectSet
	valueSemantics []valueaspects.AspectSet
}
