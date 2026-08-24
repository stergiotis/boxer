package streamreadaccess

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

// plainColKindE distinguishes scalar from non-scalar plain value columns.
type plainColKindE int

const (
	plainColScalar plainColKindE = iota
	plainColArray
	plainColSet
)

type plainColLayout struct {
	valueColLayout
	kind plainColKindE
}

type plainSectionLayout struct {
	itemType  common.PlainItemTypeE
	valueCols []plainColLayout
	// Precomputed for BeginPlainSection (avoids per-entity allocation)
	valueNames []naming.StylableName
	valueTypes []canonicaltypes.PrimitiveAstNodeI
}

type sectionLayout struct {
	sectionIdx     int
	name           naming.StylableName
	membershipSpec common.MembershipSpecE
	useAspects     useaspects.AspectSet

	scalarCols    []valueColLayout
	arrayCols     []valueColLayout
	arrayCardCols []int // Arrow indices: List<Uint64>, role ColumnRoleCardinality
	setCols       []valueColLayout
	setCardCols   []int // Arrow indices: List<Uint64>, role ColumnRoleCardinality

	memberCols        []memberColLayout
	memberCardDetails []memberCardDetail

	// Precomputed for BeginSection (avoids per-entity allocation)
	valueNames []naming.StylableName
	valueTypes []canonicaltypes.PrimitiveAstNodeI
}

type memberCardDetail struct {
	arrowIdx int
	role     common.ColumnRoleE
}

type valueColLayout struct {
	arrowIdx       int
	name           naming.StylableName
	canonicalType  canonicaltypes.PrimitiveAstNodeI
	valueSemantics valueaspects.AspectSet
}

type memberColLayout struct {
	arrowIdx int
	role     common.ColumnRoleE
	name     naming.StylableName

	// Mixed channels split one membership across two columns — the identity
	// half (lmr / lmv) and the params half (mrhp / mvhp), co-indexed 1:1 and
	// counted by the same cardinality column. linkMixedPartners pairs them:
	// the identity half carries paramsArrowIdx (-1 when the params column is
	// absent) and emits the pair in one call; the params half is marked
	// paramsOnly and skipped, unless its identity half is missing, in which
	// case it is emitted alone as before.
	paramsArrowIdx int
	paramsOnly     bool
}

type coGroupLayout struct {
	key        naming.Key
	sectionIds []int
	// Precomputed merged names/types for BeginSection
	mergedNames []naming.StylableName
	mergedTypes []canonicaltypes.PrimitiveAstNodeI
}

const MaxErrorsToMerge = 255

func NewDriver(tblDesc *common.TableDesc, ir *common.IntermediateTableRepresentation, fmts Formatters) (inst *Driver, err error) {
	inst = &Driver{
		tblDesc:          tblDesc,
		ir:               ir,
		fmts:             fmts,
		sectionInCoGroup: make(map[int]int, len(ir.TaggedValueDesc)),
		errs:             make([]error, 0, 8),
	}
	err = inst.prepare()
	return
}

// NewDriverFromSchema creates a Driver that resolves Arrow column indices
// by matching physical column names (produced via the naming convention)
// against the provided Arrow schema. This handles reordered, sparse, or
// subsetted RecordBatches where IR column order ≠ Arrow column order.
//
// Columns present in the IR but absent from the schema are silently skipped
// (arrowIdx = -1). The driving code must tolerate this.
func NewDriverFromSchema(
	tblDesc *common.TableDesc,
	ir *common.IntermediateTableRepresentation,
	fmts Formatters,
	schema *arrow.Schema,
	conv common.NamingConventionFwdI,
	tableRowConfig common.TableRowConfigE,
) (inst *Driver, err error) {
	inst = &Driver{
		tblDesc:          tblDesc,
		ir:               ir,
		fmts:             fmts,
		sectionInCoGroup: make(map[int]int, len(ir.TaggedValueDesc)),
		errs:             make([]error, 0, 8),
	}
	err = inst.prepareFromSchema(schema, conv, tableRowConfig)
	return
}

func (inst *Driver) handleError(err error) {
	if err != nil {
		errs := inst.errs
		l := len(errs)
		if l >= MaxErrorsToMerge || (l > 0 && errs[l-1] == err) {
			return
		}
		inst.errs = append(errs, err)
	}
}
func (inst *Driver) mergeAndClearError() (err error) {
	if len(inst.errs) == 0 {
		return
	}
	err = errors.Join(inst.errs...)
	return
}
func (inst *Driver) resetError() {
	clear(inst.errs)
	inst.errs = inst.errs[:0]
}
func (inst *Driver) hasError() (has bool) {
	return len(inst.errs) > 0
}

// --- Preparation (runs once) ---

func (inst *Driver) prepare() (err error) {
	inst.plainSections = make([]plainSectionLayout, 0, len(inst.ir.PlainValueDesc))
	inst.sections = make([]sectionLayout, 0, len(inst.ir.TaggedValueDesc))

	// Maps for detecting section boundaries during iteration.
	plainMap := make(map[common.PlainItemTypeE]int, len(inst.ir.PlainValueDesc))
	taggedOrd := 0

	for cc, cp := range inst.ir.IterateColumnProps() {
		if cc.IsPlainColumn() {
			psIdx, ok := plainMap[cc.PlainItemType]
			if !ok {
				psIdx = len(inst.plainSections)
				inst.plainSections = append(inst.plainSections, plainSectionLayout{
					itemType: cc.PlainItemType,
				})
				plainMap[cc.PlainItemType] = psIdx
			}
			ps := &inst.plainSections[psIdx]

			switch cc.SubType {
			case common.IntermediateColumnsSubTypeScalar:
				inst.appendPlainCols(ps, cp, cc.IndexOffset, plainColScalar)
			case common.IntermediateColumnsSubTypeHomogenousArray:
				inst.appendPlainCols(ps, cp, cc.IndexOffset, plainColArray)
			case common.IntermediateColumnsSubTypeSet:
				inst.appendPlainCols(ps, cp, cc.IndexOffset, plainColSet)
			default:
				// Support columns — skip (no values to emit)
			}

		} else {
			// Tagged column. The iterator visits sections in order,
			// so a new SectionName means a new section.
			if len(inst.sections) == 0 || inst.sections[len(inst.sections)-1].name != cc.SectionName {
				sec := sectionLayout{
					sectionIdx: taggedOrd,
					name:       cc.SectionName,
					useAspects: cc.UseAspects,
				}
				if taggedOrd < len(inst.tblDesc.TaggedValuesSections) {
					sec.membershipSpec = inst.tblDesc.TaggedValuesSections[taggedOrd].MembershipSpec
				}
				inst.sections = append(inst.sections, sec)
				taggedOrd++
			}
			sec := &inst.sections[len(inst.sections)-1]

			switch cc.SubType {
			case common.IntermediateColumnsSubTypeScalar:
				appendValueCols(&sec.scalarCols, cp, cc.IndexOffset)
			case common.IntermediateColumnsSubTypeHomogenousArray:
				appendValueCols(&sec.arrayCols, cp, cc.IndexOffset)
			case common.IntermediateColumnsSubTypeHomogenousArraySupport:
				appendCardCols(&sec.arrayCardCols, cp, cc.IndexOffset)
			case common.IntermediateColumnsSubTypeSet:
				appendValueCols(&sec.setCols, cp, cc.IndexOffset)
			case common.IntermediateColumnsSubTypeSetSupport:
				appendCardCols(&sec.setCardCols, cp, cc.IndexOffset)
			case common.IntermediateColumnsSubTypeMembership:
				appendMemberCols(&sec.memberCols, cp, cc.IndexOffset)
			case common.IntermediateColumnsSubTypeMembershipSupport:
				appendMemberCardDetails(&sec.memberCardDetails, cp, cc.IndexOffset)
			}
		}
	}

	inst.buildCoGroups()
	inst.precomputeNamesTypes()
	return
}

// linkMixedPartners pairs the identity and params halves of the mixed
// membership channels within each section (see memberColLayout). Runs after
// either preparation path, so a params column whose identity half was
// dropped by schema-resolved subsetting keeps emitting on its own.
func (inst *Driver) linkMixedPartners() {
	for i := range inst.sections {
		sec := &inst.sections[i]
		idxByRole := make(map[common.ColumnRoleE]int, len(sec.memberCols))
		for j := range sec.memberCols {
			sec.memberCols[j].paramsArrowIdx = -1
			sec.memberCols[j].paramsOnly = false
			idxByRole[sec.memberCols[j].role] = j
		}
		link := func(identRole, paramsRole common.ColumnRoleE) {
			ij, okI := idxByRole[identRole]
			pj, okP := idxByRole[paramsRole]
			if okI && okP {
				sec.memberCols[ij].paramsArrowIdx = sec.memberCols[pj].arrowIdx
				sec.memberCols[pj].paramsOnly = true
			}
		}
		link(common.ColumnRoleMixedLowCardRef, common.ColumnRoleMixedRefHighCardParameters)
		link(common.ColumnRoleMixedLowCardVerbatim, common.ColumnRoleMixedVerbatimHighCardParameters)
	}
}

func (inst *Driver) precomputeNamesTypes() {
	inst.linkMixedPartners()
	for i := range inst.plainSections {
		ps := &inst.plainSections[i]
		ps.valueNames = make([]naming.StylableName, len(ps.valueCols))
		ps.valueTypes = make([]canonicaltypes.PrimitiveAstNodeI, len(ps.valueCols))
		for j := range ps.valueCols {
			ps.valueNames[j] = ps.valueCols[j].name
			ps.valueTypes[j] = ps.valueCols[j].canonicalType
		}
	}
	for i := range inst.sections {
		sec := &inst.sections[i]
		total := len(sec.scalarCols) + len(sec.arrayCols) + len(sec.setCols)
		sec.valueNames = make([]naming.StylableName, 0, total)
		sec.valueTypes = make([]canonicaltypes.PrimitiveAstNodeI, 0, total)
		for _, c := range sec.scalarCols {
			sec.valueNames = append(sec.valueNames, c.name)
			sec.valueTypes = append(sec.valueTypes, c.canonicalType)
		}
		for _, c := range sec.arrayCols {
			sec.valueNames = append(sec.valueNames, c.name)
			sec.valueTypes = append(sec.valueTypes, c.canonicalType)
		}
		for _, c := range sec.setCols {
			sec.valueNames = append(sec.valueNames, c.name)
			sec.valueTypes = append(sec.valueTypes, c.canonicalType)
		}
	}
	for i := range inst.coGroups {
		g := &inst.coGroups[i]
		for _, sIdx := range g.sectionIds {
			sec := &inst.sections[sIdx]
			g.mergedNames = append(g.mergedNames, sec.valueNames...)
			g.mergedTypes = append(g.mergedTypes, sec.valueTypes...)
		}
	}
}

func (inst *Driver) appendPlainCols(ps *plainSectionLayout, cp *common.IntermediateColumnProps, baseOffset uint32, kind plainColKindE) {
	for j, name := range cp.Names {
		ps.valueCols = append(ps.valueCols, plainColLayout{
			valueColLayout: valueColLayout{
				arrowIdx:       int(baseOffset) + j,
				name:           name,
				canonicalType:  cp.CanonicalType[j],
				valueSemantics: cp.ValueSemantics[j],
			},
			kind: kind,
		})
	}
}

func appendValueCols(out *[]valueColLayout, cp *common.IntermediateColumnProps, baseOffset uint32) {
	for j, name := range cp.Names {
		*out = append(*out, valueColLayout{
			arrowIdx:       int(baseOffset) + j,
			name:           name,
			canonicalType:  cp.CanonicalType[j],
			valueSemantics: cp.ValueSemantics[j],
		})
	}
}

// isPerAttributeCountRole reports whether a support column holds the
// per-attribute element count a cardCursor walks: the IR emits it as
// ColumnRoleCardinality for sets and ColumnRoleLength for homogenous arrays
// (common.addHomogenousArraySupportColumn; the generated read access loads
// its array accelerator from that same column). Until 2026-08-21 only the
// set role was registered, so every homogenous array was driven with card=1
// and silently truncated to its first element.
func isPerAttributeCountRole(role common.ColumnRoleE) bool {
	return role == common.ColumnRoleCardinality || role == common.ColumnRoleLength
}

func appendCardCols(out *[]int, cp *common.IntermediateColumnProps, baseOffset uint32) {
	for j := range cp.Names {
		if isPerAttributeCountRole(cp.Roles[j]) {
			*out = append(*out, int(baseOffset)+j)
		}
	}
}

func appendMemberCols(out *[]memberColLayout, cp *common.IntermediateColumnProps, baseOffset uint32) {
	for j, name := range cp.Names {
		*out = append(*out, memberColLayout{
			arrowIdx: int(baseOffset) + j,
			role:     cp.Roles[j],
			name:     name,
		})
	}
}

func appendMemberCardDetails(out *[]memberCardDetail, cp *common.IntermediateColumnProps, baseOffset uint32) {
	for j := range cp.Names {
		*out = append(*out, memberCardDetail{
			arrowIdx: int(baseOffset) + j,
			role:     cp.Roles[j],
		})
	}
}

// --- Preparation from Arrow schema (name-based resolution) ---

// prepareFromSchema populates the same layout structs as prepare(), but
// resolves Arrow column indices by matching physical column names against
// the Arrow schema rather than assuming dense/contiguous layout.
func (inst *Driver) prepareFromSchema(
	schema *arrow.Schema,
	conv common.NamingConventionFwdI,
	tableRowConfig common.TableRowConfigE,
) (err error) {
	// Build name → Arrow index lookup from schema. Each schema column
	// contributes up to two keys: the raw column name, and (when it
	// differs) the canonical form produced by conv.CanonicalizeSchemaName.
	// resolveArrowIdx maps a PhysicalColumnDesc to an Arrow column index, or -1.
	// The resolver indexes the schema by both raw and canonicalized field names
	// (see newArrowColumnResolver): the canonical form re-styles StylableName
	// components — section names, column names — to match what
	// MapIntermediateToPhysicalColumns emits from the IR; without it a section
	// authored as "geoPoint" would round-trip as "geo-point" and silently fail
	// every value column lookup. Shared with ClassifyArrowColumns so the two
	// resolutions cannot drift.
	resolver := newArrowColumnResolver(schema, conv)
	resolveArrowIdx := resolver.resolve

	inst.plainSections = make([]plainSectionLayout, 0, len(inst.ir.PlainValueDesc))
	inst.sections = make([]sectionLayout, 0, len(inst.ir.TaggedValueDesc))

	plainMap := make(map[common.PlainItemTypeE]int, len(inst.ir.PlainValueDesc))
	taggedOrd := 0

	var physBuf []common.PhysicalColumnDesc
	for cc, cp := range inst.ir.IterateColumnProps() {
		// Map IR columns to physical column descriptors.
		physBuf, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, physBuf[:0], tableRowConfig)
		if err != nil {
			return
		}

		if cc.IsPlainColumn() {
			psIdx, ok := plainMap[cc.PlainItemType]
			if !ok {
				psIdx = len(inst.plainSections)
				inst.plainSections = append(inst.plainSections, plainSectionLayout{
					itemType: cc.PlainItemType,
				})
				plainMap[cc.PlainItemType] = psIdx
			}
			ps := &inst.plainSections[psIdx]

			switch cc.SubType {
			case common.IntermediateColumnsSubTypeScalar:
				appendPlainColsResolved(ps, cp, physBuf, resolveArrowIdx, plainColScalar)
			case common.IntermediateColumnsSubTypeHomogenousArray:
				appendPlainColsResolved(ps, cp, physBuf, resolveArrowIdx, plainColArray)
			case common.IntermediateColumnsSubTypeSet:
				appendPlainColsResolved(ps, cp, physBuf, resolveArrowIdx, plainColSet)
			default:
				// Support columns — skip
			}

		} else {
			if len(inst.sections) == 0 || inst.sections[len(inst.sections)-1].name != cc.SectionName {
				sec := sectionLayout{
					sectionIdx: taggedOrd,
					name:       cc.SectionName,
					useAspects: cc.UseAspects,
				}
				if taggedOrd < len(inst.tblDesc.TaggedValuesSections) {
					sec.membershipSpec = inst.tblDesc.TaggedValuesSections[taggedOrd].MembershipSpec
				}
				inst.sections = append(inst.sections, sec)
				taggedOrd++
			}
			sec := &inst.sections[len(inst.sections)-1]

			switch cc.SubType {
			case common.IntermediateColumnsSubTypeScalar:
				appendValueColsResolved(&sec.scalarCols, cp, physBuf, resolveArrowIdx)
			case common.IntermediateColumnsSubTypeHomogenousArray:
				appendValueColsResolved(&sec.arrayCols, cp, physBuf, resolveArrowIdx)
			case common.IntermediateColumnsSubTypeHomogenousArraySupport:
				appendCardColsResolved(&sec.arrayCardCols, cp, physBuf, resolveArrowIdx)
			case common.IntermediateColumnsSubTypeSet:
				appendValueColsResolved(&sec.setCols, cp, physBuf, resolveArrowIdx)
			case common.IntermediateColumnsSubTypeSetSupport:
				appendCardColsResolved(&sec.setCardCols, cp, physBuf, resolveArrowIdx)
			case common.IntermediateColumnsSubTypeMembership:
				appendMemberColsResolved(&sec.memberCols, cp, physBuf, resolveArrowIdx)
			case common.IntermediateColumnsSubTypeMembershipSupport:
				appendMemberCardDetailsResolved(&sec.memberCardDetails, cp, physBuf, resolveArrowIdx)
			}
		}
	}

	inst.buildCoGroups()
	inst.precomputeNamesTypes()
	return
}

// --- Name-resolving append helpers ---

func appendPlainColsResolved(ps *plainSectionLayout, cp *common.IntermediateColumnProps, phys []common.PhysicalColumnDesc, resolve func(common.PhysicalColumnDesc) int, kind plainColKindE) {
	for j, name := range cp.Names {
		arrowIdx := resolve(phys[j])
		if arrowIdx < 0 {
			continue
		}
		ps.valueCols = append(ps.valueCols, plainColLayout{
			valueColLayout: valueColLayout{
				arrowIdx:       arrowIdx,
				name:           name,
				canonicalType:  cp.CanonicalType[j],
				valueSemantics: cp.ValueSemantics[j],
			},
			kind: kind,
		})
	}
}

func appendValueColsResolved(out *[]valueColLayout, cp *common.IntermediateColumnProps, phys []common.PhysicalColumnDesc, resolve func(common.PhysicalColumnDesc) int) {
	for j, name := range cp.Names {
		arrowIdx := resolve(phys[j])
		if arrowIdx < 0 {
			continue
		}
		*out = append(*out, valueColLayout{
			arrowIdx:       arrowIdx,
			name:           name,
			canonicalType:  cp.CanonicalType[j],
			valueSemantics: cp.ValueSemantics[j],
		})
	}
}

func appendCardColsResolved(out *[]int, cp *common.IntermediateColumnProps, phys []common.PhysicalColumnDesc, resolve func(common.PhysicalColumnDesc) int) {
	for j := range cp.Names {
		if !isPerAttributeCountRole(cp.Roles[j]) {
			continue
		}
		arrowIdx := resolve(phys[j])
		if arrowIdx < 0 {
			continue
		}
		*out = append(*out, arrowIdx)
	}
}

func appendMemberColsResolved(out *[]memberColLayout, cp *common.IntermediateColumnProps, phys []common.PhysicalColumnDesc, resolve func(common.PhysicalColumnDesc) int) {
	for j, name := range cp.Names {
		arrowIdx := resolve(phys[j])
		if arrowIdx < 0 {
			continue
		}
		*out = append(*out, memberColLayout{
			arrowIdx: arrowIdx,
			role:     cp.Roles[j],
			name:     name,
		})
	}
}

func appendMemberCardDetailsResolved(out *[]memberCardDetail, cp *common.IntermediateColumnProps, phys []common.PhysicalColumnDesc, resolve func(common.PhysicalColumnDesc) int) {
	for j := range cp.Names {
		arrowIdx := resolve(phys[j])
		if arrowIdx < 0 {
			continue
		}
		*out = append(*out, memberCardDetail{
			arrowIdx: arrowIdx,
			role:     cp.Roles[j],
		})
	}
}

func (inst *Driver) buildCoGroups() {
	groupMap := make(map[naming.Key]*coGroupLayout, 4)
	for i := range inst.sections {
		key := inst.ir.TaggedValueDesc[i].CoSectionGroup
		if key == "" {
			inst.sectionInCoGroup[i] = -1
			continue
		}
		g, ok := groupMap[key]
		if !ok {
			g = &coGroupLayout{key: key}
			groupMap[key] = g
		}
		g.sectionIds = append(g.sectionIds, i)
	}
	inst.coGroups = make([]coGroupLayout, 0, len(groupMap))
	for _, g := range groupMap {
		if len(g.sectionIds) < 2 {
			for _, sid := range g.sectionIds {
				inst.sectionInCoGroup[sid] = -1
			}
			continue
		}
		gIdx := len(inst.coGroups)
		inst.coGroups = append(inst.coGroups, *g)
		for _, sid := range g.sectionIds {
			inst.sectionInCoGroup[sid] = gIdx
		}
	}
	slices.SortFunc(inst.coGroups, func(a, b coGroupLayout) int {
		return strings.Compare(a.key.String(), b.key.String())
	})
}

// --- Driving ---

func (inst *Driver) DriveRecordBatch(sink SinkI, rec arrow.RecordBatch) (err error) {
	inst.resetError()

	nEntities := int(rec.NumRows())
	sink.BeginBatch()

	for entityIdx := range nEntities {
		inst.driveEntity(sink, rec, entityIdx)
		if inst.hasError() {
			break
		}
	}

	err = sink.EndBatch()
	inst.handleError(err)

	err = inst.mergeAndClearError()
	return
}

func (inst *Driver) driveEntity(sink SinkI, rec arrow.RecordBatch, entityIdx int) {
	// Optional capabilities (ADR-0072 pattern): resolved once per entity, not
	// per value, and cleared on exit so a later drive with another sink
	// cannot inherit them.
	inst.arrowSink, _ = sink.(ArrowValueSinkI)
	inst.coTagSink, _ = sink.(CoSectionTagSinkI)
	defer func() {
		inst.arrowSink = nil
		inst.coTagSink = nil
	}()

	sink.BeginEntity()

	{ // Plain sections
		for ps := range inst.plainSections {
			inst.drivePlainSection(sink, rec, entityIdx, ps)
			if inst.hasError() {
				break
			}
		}
	}

	{ // Tagged sections (co-groups + standalone)
		sink.BeginTaggedSections()

		{ // Co-section groups
			if !inst.hasError() {
				for gIdx := range inst.coGroups {
					inst.driveCoGroup(sink, rec, entityIdx, gIdx)
					if inst.hasError() {
						break
					}
				}
			}
		}

		{ // Standalone tagged sections
			if !inst.hasError() {
				for sIdx := range inst.sections {
					gIdx := inst.sectionInCoGroup[sIdx]
					if gIdx >= 0 {
						continue
					}
					inst.driveSection(sink, rec, entityIdx, sIdx)
					if inst.hasError() {
						break
					}
				}
			}
		}

		err := sink.EndTaggedSections()
		inst.handleError(err)
	}

	err := sink.EndEntity()
	inst.handleError(err)
}

// --- Plain section driving ---

func (inst *Driver) drivePlainSection(sink SinkI, rec arrow.RecordBatch, entityIdx int, psIdx int) {
	ps := &inst.plainSections[psIdx]
	if len(ps.valueCols) == 0 {
		return
	}

	// Plain sections are 1:1 with entity rows — always 1 row of values
	sink.BeginPlainSection(ps.itemType, ps.valueNames, ps.valueTypes, 1)

	sink.BeginPlainValue()
	valueFmt := inst.fmts.ValueFormatter
	av := inst.arrowSink // typed lane (ArrowValueSinkI), nil for text sinks

	for _, col := range ps.valueCols {
		addr := PhysicalColumnAddr{Index: col.arrowIdx, FullColumnName: rec.ColumnName(col.arrowIdx)}
		sink.BeginColumn(addr, col.name, col.canonicalType, col.valueSemantics)

		switch col.kind {
		case plainColScalar:
			sink.BeginScalarValue()
			if av != nil {
				av.WriteArrowScalar(rec.Column(col.arrowIdx), entityIdx)
			} else {
				text := inst.readPlainScalar(rec, col.arrowIdx, entityIdx)
				_, err := sink.WriteString(valueFmt.FormatValue(text, col.canonicalType))
				inst.handleError(err)
			}
			err := sink.EndScalarValue()
			inst.handleError(err)

		case plainColArray:
			elemStart, elemEnd := inst.listOffsets(rec, col.arrowIdx, entityIdx)
			card := elemEnd - elemStart
			sink.BeginHomogenousArrayValue(card)
			if av != nil {
				av.WriteArrowRange(inst.listInnerArray(rec, col.arrowIdx), elemStart, elemEnd)
			} else {
				for elemIdx := range card {
					sink.BeginValueItem(elemIdx)
					text := inst.readListInnerValue(rec, col.arrowIdx, elemStart+elemIdx)
					_, err := sink.WriteString(valueFmt.FormatValue(text, col.canonicalType))
					inst.handleError(err)
					sink.EndValueItem()
				}
			}
			sink.EndHomogenousArrayValue()

		case plainColSet:
			elemStart, elemEnd := inst.listOffsets(rec, col.arrowIdx, entityIdx)
			card := elemEnd - elemStart
			sink.BeginSetValue(card)
			if av != nil {
				av.WriteArrowRange(inst.listInnerArray(rec, col.arrowIdx), elemStart, elemEnd)
			} else {
				for elemIdx := range card {
					sink.BeginValueItem(elemIdx)
					text := inst.readListInnerValue(rec, col.arrowIdx, elemStart+elemIdx)
					_, err := sink.WriteString(valueFmt.FormatValue(text, col.canonicalType))
					inst.handleError(err)
					sink.EndValueItem()
				}
			}
			sink.EndSetValue()
		}

		sink.EndColumn()
	}

	err := sink.EndPlainValue()
	inst.handleError(err)
	err = sink.EndPlainSection()
	inst.handleError(err)
}

// readPlainScalar reads a scalar value from a non-list column at row entityIdx.
func (inst *Driver) readPlainScalar(rec arrow.RecordBatch, colIdx int, rowIdx int) (text string) {
	col := rec.Column(colIdx)
	text = col.ValueStr(rowIdx)
	return
}

// --- Tagged section driving ---

// cardCursor walks one List<Uint64> cardinality column for a single entity,
// maintaining a running prefix sum across sequential attrIdx values.
// step(attrIdx) returns (relOff, card) for the current position and
// advances the cursor. attrIdx must increase monotonically by 1.
//
// When inner is nil (card column missing or non-uint64), the cursor falls
// back to "one element per attribute": each step returns card=1 and relOff
// equal to the prior sum (so the first call yields relOff=0, the next 1, …),
// matching the legacy fallback in nonScalarElemRange/memberColElemRange.
type cardCursor struct {
	inner       *array.Uint64
	entityStart int
	relOff      int
}

func (inst *Driver) newCardCursor(rec arrow.RecordBatch, cardArrowIdx int, entityIdx int) cardCursor {
	if cardArrowIdx < 0 {
		return cardCursor{}
	}
	cardEntityStart, _ := inst.listOffsets(rec, cardArrowIdx, entityIdx)
	cardInner := inst.listInnerArray(rec, cardArrowIdx)
	u64, ok := cardInner.(*array.Uint64)
	if !ok {
		// The cardinality column is present but not the expected Uint64 list.
		// Falling back to card=1 here mis-slices every multi-element attribute
		// and produces silently wrong output (review F-4); surface an error so
		// the malformed batch is rejected rather than mis-decoded.
		inst.handleError(eh.Errorf("cardinality column %d is %s, expected a Uint64 list", cardArrowIdx, cardInner.DataType()))
		return cardCursor{}
	}
	return cardCursor{
		inner:       u64,
		entityStart: cardEntityStart,
	}
}

// step returns (relOff, card) at the current position and advances by card.
// Callers must invoke step exactly once per attrIdx, in increasing order.
func (c *cardCursor) step(attrIdx int) (relOff, card int) {
	relOff = c.relOff
	if c.inner == nil {
		card = 1
	} else {
		card = int(c.inner.Value(c.entityStart + attrIdx))
	}
	c.relOff = relOff + card
	return
}

// attrCardSlot is the (relOff, card) snapshot a cursor produces for one attrIdx.
type attrCardSlot struct {
	relOff int
	card   int
}

func cardArrowIdxOrSentinel(cardCols []int) int {
	if len(cardCols) == 0 {
		return -1
	}
	return cardCols[0]
}

// buildMemberCursors creates one cursor per entry in sec.memberCardDetails.
// memberSlots is allocated alongside; reused across attrIdx steps.
func (inst *Driver) buildMemberCursors(rec arrow.RecordBatch, sec *sectionLayout, entityIdx int) (cursors []cardCursor, slots []attrCardSlot) {
	n := len(sec.memberCardDetails)
	if n == 0 {
		return
	}
	cursors = make([]cardCursor, n)
	slots = make([]attrCardSlot, n)
	for i, mcd := range sec.memberCardDetails {
		cursors[i] = inst.newCardCursor(rec, mcd.arrowIdx, entityIdx)
	}
	return
}

func (inst *Driver) driveSection(sink SinkI, rec arrow.RecordBatch, entityIdx int, sIdx int) {
	sec := &inst.sections[sIdx]
	nAttrs := inst.sectionAttrCount(rec, entityIdx, sec)
	sink.BeginSection(sec.name, sec.valueNames, sec.valueTypes, sec.useAspects, nAttrs)

	arrayCur := inst.newCardCursor(rec, cardArrowIdxOrSentinel(sec.arrayCardCols), entityIdx)
	setCur := inst.newCardCursor(rec, cardArrowIdxOrSentinel(sec.setCardCols), entityIdx)
	memberCurs, memberSlots := inst.buildMemberCursors(rec, sec, entityIdx)

	for attrIdx := range nAttrs {
		sink.BeginTaggedValue()
		arrayRel, arrayCard := arrayCur.step(attrIdx)
		setRel, setCard := setCur.step(attrIdx)
		for i := range memberCurs {
			memberSlots[i].relOff, memberSlots[i].card = memberCurs[i].step(attrIdx)
		}
		inst.emitValueColumns(sink, rec, entityIdx, attrIdx, sec, attrCardSlot{arrayRel, arrayCard}, attrCardSlot{setRel, setCard})
		inst.emitMemberships(sink, rec, entityIdx, attrIdx, sec, memberSlots)
		err := sink.EndTaggedValue()
		if err != nil {
			inst.handleError(err)
			break
		}
	}

	err := sink.EndSection()
	inst.handleError(err)
}

func (inst *Driver) driveCoGroup(sink SinkI, rec arrow.RecordBatch, entityIdx int, gIdx int) {
	group := &inst.coGroups[gIdx]
	sink.BeginCoSectionGroup(group.key)

	firstSec := &inst.sections[group.sectionIds[0]]
	nAttrs := inst.sectionAttrCount(rec, entityIdx, firstSec)

	// Use first section's name for the merged section
	sink.BeginSection(firstSec.name, group.mergedNames, group.mergedTypes, firstSec.useAspects, nAttrs)

	type secCursors struct {
		arrayCur    cardCursor
		setCur      cardCursor
		memberCurs  []cardCursor
		memberSlots []attrCardSlot
	}
	perSec := make([]secCursors, len(group.sectionIds))
	for i, sIdx := range group.sectionIds {
		sec := &inst.sections[sIdx]
		perSec[i] = secCursors{
			arrayCur: inst.newCardCursor(rec, cardArrowIdxOrSentinel(sec.arrayCardCols), entityIdx),
			setCur:   inst.newCardCursor(rec, cardArrowIdxOrSentinel(sec.setCardCols), entityIdx),
		}
		perSec[i].memberCurs, perSec[i].memberSlots = inst.buildMemberCursors(rec, sec, entityIdx)
	}
	ms, _ := sink.(MembershipSinkI)

	for attrIdx := range nAttrs {
		sink.BeginTaggedValue()
		for i, sIdx := range group.sectionIds {
			sec := &inst.sections[sIdx]
			arrayRel, arrayCard := perSec[i].arrayCur.step(attrIdx)
			setRel, setCard := perSec[i].setCur.step(attrIdx)
			inst.emitValueColumns(sink, rec, entityIdx, attrIdx, sec, attrCardSlot{arrayRel, arrayCard}, attrCardSlot{setRel, setCard})
		}
		// One tag frame for the merged tagged value, fed by EVERY section of
		// the group: co-sections share topology, not membership columns, so a
		// membership-only co-section (the annotation-overlay pattern) carries
		// tags the first section does not. Until 2026-08-21 only the first
		// section's membership columns were driven here and those tags were
		// silently dropped.
		nTags := 0
		for i, sIdx := range group.sectionIds {
			ps := &perSec[i]
			for j := range ps.memberCurs {
				ps.memberSlots[j].relOff, ps.memberSlots[j].card = ps.memberCurs[j].step(attrIdx)
			}
			nTags += inst.sectionTagCount(&inst.sections[sIdx], ps.memberSlots)
		}
		sink.BeginTags(nTags)
		for i, sIdx := range group.sectionIds {
			sec := &inst.sections[sIdx]
			if inst.coTagSink != nil {
				inst.coTagSink.BeginCoSectionTags(sec.name, sec.useAspects)
			}
			inst.emitSectionTags(ms, rec, entityIdx, attrIdx, sec, perSec[i].memberSlots)
		}
		sink.EndTags()
		err := sink.EndTaggedValue()
		if err != nil {
			inst.handleError(err)
			break
		}
	}

	err := sink.EndSection()
	inst.handleError(err)
	err = sink.EndCoSectionGroup()
	inst.handleError(err)
}

// --- Value emission ---

func (inst *Driver) emitValueColumns(sink SinkI, rec arrow.RecordBatch, entityIdx int, attrIdx int, sec *sectionLayout, arraySlot, setSlot attrCardSlot) {
	valueFmt := inst.fmts.ValueFormatter

	av := inst.arrowSink // typed lane (ArrowValueSinkI), nil for text sinks

	{ // Scalar columns
		for _, col := range sec.scalarCols {
			flatIdx := inst.listFlatIndex(rec, col.arrowIdx, entityIdx, attrIdx)
			addr := PhysicalColumnAddr{Index: col.arrowIdx, FullColumnName: rec.ColumnName(col.arrowIdx)}
			sink.BeginColumn(addr, col.name, col.canonicalType, col.valueSemantics)
			sink.BeginScalarValue()
			if av != nil {
				av.WriteArrowScalar(inst.listInnerArray(rec, col.arrowIdx), flatIdx)
			} else {
				text := inst.readListInnerValue(rec, col.arrowIdx, flatIdx)
				_, err := sink.WriteString(valueFmt.FormatValue(text, col.canonicalType))
				inst.handleError(err)
			}
			err := sink.EndScalarValue()
			inst.handleError(err)
			sink.EndColumn()
		}
	}

	{ // Array columns
		for _, col := range sec.arrayCols {
			valueEntityStart := inst.listStart(rec, col.arrowIdx, entityIdx)
			elemStart := valueEntityStart + arraySlot.relOff
			card := arraySlot.card
			addr := PhysicalColumnAddr{Index: col.arrowIdx, FullColumnName: rec.ColumnName(col.arrowIdx)}
			sink.BeginColumn(addr, col.name, col.canonicalType, col.valueSemantics)
			sink.BeginHomogenousArrayValue(card)
			if av != nil {
				av.WriteArrowRange(inst.listInnerArray(rec, col.arrowIdx), elemStart, elemStart+card)
			} else {
				for elemIdx := range card {
					sink.BeginValueItem(elemIdx)
					text := inst.readListInnerValue(rec, col.arrowIdx, elemStart+elemIdx)
					_, err := sink.WriteString(valueFmt.FormatValue(text, col.canonicalType))
					inst.handleError(err)
					sink.EndValueItem()
				}
			}
			sink.EndHomogenousArrayValue()
			sink.EndColumn()
		}
	}

	{ // Set columns
		for _, col := range sec.setCols {
			valueEntityStart := inst.listStart(rec, col.arrowIdx, entityIdx)
			elemStart := valueEntityStart + setSlot.relOff
			card := setSlot.card
			addr := PhysicalColumnAddr{Index: col.arrowIdx, FullColumnName: rec.ColumnName(col.arrowIdx)}
			sink.BeginColumn(addr, col.name, col.canonicalType, col.valueSemantics)
			sink.BeginSetValue(card)
			if av != nil {
				av.WriteArrowRange(inst.listInnerArray(rec, col.arrowIdx), elemStart, elemStart+card)
			} else {
				for elemIdx := range card {
					sink.BeginValueItem(elemIdx)
					text := inst.readListInnerValue(rec, col.arrowIdx, elemStart+elemIdx)
					_, err := sink.WriteString(valueFmt.FormatValue(text, col.canonicalType))
					inst.handleError(err)
					sink.EndValueItem()
				}
			}
			sink.EndSetValue()
			sink.EndColumn()
		}
	}
}

// --- Membership emission ---

// memberSlotForRole returns the precomputed (relOff, card) for the given
// membership role by scanning sec.memberCardDetails. Returns (-1, 0) if no
// matching cardinality column was registered for the role.
func memberSlotForRole(sec *sectionLayout, memberSlots []attrCardSlot, memberRole common.ColumnRoleE) (slot attrCardSlot, found bool) {
	expectedCardRole, err := common.GetCardinalityRoleByMembershipRole(memberRole)
	if err != nil {
		return
	}
	for i, mcd := range sec.memberCardDetails {
		if mcd.role == expectedCardRole {
			return memberSlots[i], true
		}
	}
	return
}

// emitMemberships drives the tag frame of one attribute of a standalone
// section: BeginTags(n), one AddMembership* call per membership, EndTags.
// Co-section groups share one tag frame across their sections and are
// driven by driveCoGroup through the same two helpers.
func (inst *Driver) emitMemberships(sink SinkI, rec arrow.RecordBatch, entityIdx int, attrIdx int, sec *sectionLayout, memberSlots []attrCardSlot) {
	// Membership rendering is an optional sink capability (ADR-0072): the tag
	// frame (BeginTags/EndTags) is always driven so structural sinks see the
	// tag count, but per-tag identities are emitted only when the sink
	// implements MembershipSinkI. Non-rendering sinks leave ms nil and
	// emitSectionTags skips the identities.
	ms, _ := sink.(MembershipSinkI)
	sink.BeginTags(inst.sectionTagCount(sec, memberSlots))
	inst.emitSectionTags(ms, rec, entityIdx, attrIdx, sec, memberSlots)
	sink.EndTags()
}

// sectionTagCount is the number of tags one section contributes to the tag
// frame of one attribute — what BeginTags announces: no membership columns →
// 0; membership columns without cardinality support → one tag per column;
// otherwise the sum of the attribute's per-role cardinalities, read from the
// precomputed slots rather than the Uint64 inner array.
func (inst *Driver) sectionTagCount(sec *sectionLayout, memberSlots []attrCardSlot) (n int) {
	if len(sec.memberCols) == 0 {
		return
	}
	if len(sec.memberCardDetails) == 0 {
		// One tag per membership column, a mixed pair counting once.
		for _, mc := range sec.memberCols {
			if !mc.paramsOnly {
				n++
			}
		}
		return
	}
	for _, s := range memberSlots {
		n += s.card
	}
	return
}

// emitSectionTags emits the per-tag identities of one section's membership
// columns for one attribute, inside a tag frame the caller has already opened.
// ms is nil when the sink does not render memberships; the frame is still
// driven by the caller, the identities are skipped here.
func (inst *Driver) emitSectionTags(ms MembershipSinkI, rec arrow.RecordBatch, entityIdx int, attrIdx int, sec *sectionLayout, memberSlots []attrCardSlot) {
	if ms == nil || len(sec.memberCols) == 0 {
		return
	}
	if len(sec.memberCardDetails) == 0 {
		for _, mc := range sec.memberCols {
			flatIdx := inst.listFlatIndex(rec, mc.arrowIdx, entityIdx, attrIdx)
			inst.emitOneMembership(ms, rec, mc, flatIdx)
		}
		return
	}
	for _, mc := range sec.memberCols {
		entityStart := inst.listStart(rec, mc.arrowIdx, entityIdx)
		slot, found := memberSlotForRole(sec, memberSlots, mc.role)
		var mbrStart, mbrEnd int
		if !found {
			mbrStart = entityStart + attrIdx
			mbrEnd = mbrStart + 1
		} else {
			mbrStart = entityStart + slot.relOff
			mbrEnd = mbrStart + slot.card
		}
		for flatIdx := mbrStart; flatIdx < mbrEnd; flatIdx++ {
			inst.emitOneMembership(ms, rec, mc, flatIdx)
		}
	}
}

func (inst *Driver) emitOneMembership(ms MembershipSinkI, rec arrow.RecordBatch, mc memberColLayout, flatIdx int) {
	if ms == nil {
		return // sink does not implement MembershipSinkI (ADR-0072)
	}
	// Identities only — the driver no longer formats memberships; consumers
	// render them at read time via a membership.Renderer (ADR-0072).
	switch mc.role {
	case common.ColumnRoleHighCardRef:
		ref := inst.readListInnerUint64(rec, mc.arrowIdx, flatIdx)
		ms.AddMembershipRef(false, ref)

	case common.ColumnRoleLowCardRef:
		ref := inst.readListInnerUint64(rec, mc.arrowIdx, flatIdx)
		ms.AddMembershipRef(true, ref)

	case common.ColumnRoleHighCardVerbatim:
		raw := inst.readListInnerBytes(rec, mc.arrowIdx, flatIdx)
		ms.AddMembershipVerbatim(false, unsafeperf.UnsafeBytesToString(raw))

	case common.ColumnRoleLowCardVerbatim:
		raw := inst.readListInnerBytes(rec, mc.arrowIdx, flatIdx)
		ms.AddMembershipVerbatim(true, unsafeperf.UnsafeBytesToString(raw))

	case common.ColumnRoleHighCardRefParametrized:
		// Canonical type (lw_ddl_tech_common.go: membershipSerializedType) is
		// Binary, not Uint64 — the comment there reads "parametrization is
		// always high-card, even when the ref is low-card", i.e. the
		// payload-bearing column is bytes for both High and Low variants and
		// carries the params half of the (ref, params) pair. Ref is not
		// stored separately for this role; emit ref=0 and forward the bytes
		// as params, mirroring the params half of the Mixed pattern below.
		raw := inst.readListInnerBytes(rec, mc.arrowIdx, flatIdx)
		ms.AddMembershipRefParametrized(false, 0, unsafeperf.UnsafeBytesToString(raw))

	case common.ColumnRoleLowCardRefParametrized:
		raw := inst.readListInnerBytes(rec, mc.arrowIdx, flatIdx)
		ms.AddMembershipRefParametrized(true, 0, unsafeperf.UnsafeBytesToString(raw))

	// The mixed channels: one membership = (identity, params) across two
	// co-indexed columns. The identity half emits the PAIR, reading the params
	// half at the same flat index (linkMixedPartners); the params half is
	// skipped unless it is on its own. Until 2026-08-21 the two halves were
	// emitted as two half-populated calls — (ref, "") then (0, params) — which
	// the streamreadaccess EXPLANATION recorded as a known issue.
	case common.ColumnRoleMixedLowCardRef:
		ref := inst.readListInnerUint64(rec, mc.arrowIdx, flatIdx)
		var params []byte
		if mc.paramsArrowIdx >= 0 {
			params = inst.readListInnerBytes(rec, mc.paramsArrowIdx, flatIdx)
		}
		ms.AddMembershipMixedLowCardRefHighCardParam(ref, unsafeperf.UnsafeBytesToString(params))

	case common.ColumnRoleMixedLowCardVerbatim:
		raw := inst.readListInnerBytes(rec, mc.arrowIdx, flatIdx)
		var params []byte
		if mc.paramsArrowIdx >= 0 {
			params = inst.readListInnerBytes(rec, mc.paramsArrowIdx, flatIdx)
		}
		ms.AddMembershipMixedLowCardVerbatimHighCardParam(unsafeperf.UnsafeBytesToString(raw), unsafeperf.UnsafeBytesToString(params))

	case common.ColumnRoleMixedVerbatimHighCardParameters:
		if mc.paramsOnly {
			return // emitted with its identity half
		}
		raw := inst.readListInnerBytes(rec, mc.arrowIdx, flatIdx)
		ms.AddMembershipMixedLowCardVerbatimHighCardParam("", unsafeperf.UnsafeBytesToString(raw))

	case common.ColumnRoleMixedRefHighCardParameters:
		if mc.paramsOnly {
			return // emitted with its identity half
		}
		raw := inst.readListInnerBytes(rec, mc.arrowIdx, flatIdx)
		ms.AddMembershipMixedLowCardRefHighCardParam(0, unsafeperf.UnsafeBytesToString(raw))

	default:
		// Honour the driver's no-panic contract: an unknown role (e.g. written
		// by a future binary) is surfaced as an error, not a crash (review F-4).
		inst.handleError(eh.Errorf("unimplemented column role: %s", mc.role))
	}
}

// --- Arrow List<X> access primitives ---

func (inst *Driver) listOffsets(rec arrow.RecordBatch, arrowColIdx int, entityIdx int) (start int, end int) {
	col := rec.Column(arrowColIdx)
	listArr, ok := col.(*array.List)
	if !ok {
		start = entityIdx
		end = entityIdx + 1
		return
	}
	s, e := listArr.ValueOffsets(entityIdx)
	start = int(s)
	end = int(e)
	return
}

func (inst *Driver) listStart(rec arrow.RecordBatch, arrowColIdx int, entityIdx int) (start int) {
	start, _ = inst.listOffsets(rec, arrowColIdx, entityIdx)
	return
}

func (inst *Driver) listInnerArray(rec arrow.RecordBatch, arrowColIdx int) arrow.Array {
	col := rec.Column(arrowColIdx)
	listArr, ok := col.(*array.List)
	if !ok {
		return col
	}
	return listArr.ListValues()
}

func (inst *Driver) listFlatIndex(rec arrow.RecordBatch, arrowColIdx int, entityIdx int, attrIdx int) (flatIdx int) {
	start := inst.listStart(rec, arrowColIdx, entityIdx)
	flatIdx = start + attrIdx
	return
}

func (inst *Driver) readListInnerValue(rec arrow.RecordBatch, arrowColIdx int, flatIdx int) (text string) {
	inner := inst.listInnerArray(rec, arrowColIdx)
	text = inner.ValueStr(flatIdx)
	return
}

func (inst *Driver) readListInnerUint64(rec arrow.RecordBatch, arrowColIdx int, flatIdx int) (val uint64) {
	inner := inst.listInnerArray(rec, arrowColIdx)
	if flatIdx >= inner.Len() || inner.IsNull(flatIdx) {
		return
	}
	switch arr := inner.(type) {
	case *array.Uint64:
		val = arr.Value(flatIdx)
	case *array.Int64:
		val = uint64(arr.Value(flatIdx))
	default:
		// This reader has no error channel, and 0 is a valid reference, so a
		// dropped scan error is a wrong id rather than a missing one. Warn in
		// the shape the "should never get here" branch below already uses.
		text := inner.ValueStr(flatIdx)
		if _, serr := fmt.Sscanf(text, "%d", &val); serr != nil {
			val = 0
			log.Warn().Caller(0).Err(serr).Str("value", text).Msg("unable to parse list inner value as uint64")
		}
	}
	return
}

func (inst *Driver) readListInnerBytes(rec arrow.RecordBatch, arrowColIdx int, flatIdx int) (val []byte) {
	inner := inst.listInnerArray(rec, arrowColIdx)
	if flatIdx >= inner.Len() || inner.IsNull(flatIdx) {
		return
	}
	switch arr := inner.(type) {
	case *array.String:
		val = unsafeperf.UnsafeStringToBytes(arr.Value(flatIdx))
	case *array.Binary:
		val = arr.Value(flatIdx)
	default:
		log.Warn().Caller(0).Msg("should never get here")
		val = unsafeperf.UnsafeStringToBytes(inner.ValueStr(flatIdx))
	}
	return
}

// --- Cardinality computation ---

func (inst *Driver) sectionAttrCount(rec arrow.RecordBatch, entityIdx int, sec *sectionLayout) (n int) {
	if len(sec.scalarCols) > 0 {
		start, end := inst.listOffsets(rec, sec.scalarCols[0].arrowIdx, entityIdx)
		n = end - start
		return
	}
	if len(sec.arrayCardCols) > 0 {
		start, end := inst.listOffsets(rec, sec.arrayCardCols[0], entityIdx)
		n = end - start
		return
	}
	if len(sec.setCardCols) > 0 {
		start, end := inst.listOffsets(rec, sec.setCardCols[0], entityIdx)
		n = end - start
		return
	}
	if len(sec.memberCardDetails) > 0 {
		start, end := inst.listOffsets(rec, sec.memberCardDetails[0].arrowIdx, entityIdx)
		n = end - start
		return
	}
	if len(sec.memberCols) > 0 {
		start, end := inst.listOffsets(rec, sec.memberCols[0].arrowIdx, entityIdx)
		n = end - start
		return
	}
	return
}
