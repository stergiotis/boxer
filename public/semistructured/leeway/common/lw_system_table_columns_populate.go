package common

import (
	"encoding/binary"
	"hash/fnv"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// Well-known low-card ref IDs for the symbol section attributes.
// These are FNV-1a hashes of the attribute names to ensure stable, collision-free identifiers.
var (
	lcrScope          = stableRef("scope")
	lcrItemType       = stableRef("itemType")
	lcrColumnRole     = stableRef("columnRole")
	lcrSubType        = stableRef("subType")
	lcrMembershipSpec = stableRef("membershipSpec")
)

// Well-known low-card ref IDs for the string section attributes.
var (
	lcrSectionName        = stableRef("sectionName")
	lcrLogicalColumnName  = stableRef("logicalColumnName")
	lcrCanonicalType      = stableRef("canonicalType")
	lcrCoSectionGroup     = stableRef("coSectionGroup")
	lcrStreamingGroup     = stableRef("streamingGroup")
	lcrTableComment       = stableRef("tableComment")
	lcrValueSemantics     = stableRef("valueSemantics")
)

// Well-known low-card ref IDs for the u64 section attributes.
var (
	lcrLocalMonotonicIndex = stableRef("localMonotonicIndex")
	lcrLogicalIndex        = stableRef("logicalIndex")
)

// Well-known low-card ref IDs for the text section attributes.
var (
	lcrEncodingHint   = stableRef("encodingHint")
	lcrValueSemantic  = stableRef("valueSemantic")
	lcrUseAspect      = stableRef("useAspect")
)

func stableRef(name string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return h.Sum64()
}

func ordinalParam(ordinal int) []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(ordinal))
	return buf
}

// PopulateSchemaTable populates a system-table-columns InEntity from an IntermediateTableRepresentation.
// Each physical column in the described table becomes one entity.
func PopulateSchemaTable(entity *InEntitySystemTableColumns, ir *IntermediateTableRepresentation, tableName naming.StylableName, tableComment string) (err error) {
	tableHash := stableRef(string(tableName))

	for cc, cp := range ir.IterateColumnProps() {
		for i, name := range cp.Names {
			globalIndex := cc.IndexOffset + uint32(i)

			entity.BeginEntity()
			entity.SetId(tableHash, uint64(globalIndex))
			entity.SetRouting(string(tableName))

			{ // symbol section — categorical metadata
				sym := entity.GetSectionSymbol()
				sym.BeginAttribute(cc.Scope.String()).AddMembershipLowCardRef(lcrScope).EndAttribute()
				sym.BeginAttribute(cc.PlainItemType.String()).AddMembershipLowCardRef(lcrItemType).EndAttribute()
				sym.BeginAttribute(string(cp.Roles[i])).AddMembershipLowCardRef(lcrColumnRole).EndAttribute()
				sym.BeginAttribute(cc.SubType.String()).AddMembershipLowCardRef(lcrSubType).EndAttribute()
			}

			{ // string section — variable-length string metadata
				str := entity.GetSectionString()
				str.BeginAttribute(cc.SectionName.String()).AddMembershipLowCardRef(lcrSectionName).EndAttribute()
				str.BeginAttribute(name.String()).AddMembershipLowCardRef(lcrLogicalColumnName).EndAttribute()
				str.BeginAttribute(cp.CanonicalType[i].String()).AddMembershipLowCardRef(lcrCanonicalType).EndAttribute()
				if cc.CoSectionGroup != "" {
					str.BeginAttribute(string(cc.CoSectionGroup)).AddMembershipLowCardRef(lcrCoSectionGroup).EndAttribute()
				}
				if cc.StreamingGroup != "" {
					str.BeginAttribute(string(cc.StreamingGroup)).AddMembershipLowCardRef(lcrStreamingGroup).EndAttribute()
				}
				if tableComment != "" {
					str.BeginAttribute(tableComment).AddMembershipLowCardRef(lcrTableComment).EndAttribute()
				}
			}

			{ // u64 section — numeric metadata
				u64sec := entity.GetSectionU64()
				u64sec.BeginAttribute(uint64(i)).AddMembershipLowCardRef(lcrLocalMonotonicIndex).EndAttribute()
			}

			{ // text section — aspect sets
				txt := entity.GetSectionText()
				{ // encoding hints
					for j, hint := range cp.EncodingHints[i].IterateAspects() {
						txt.BeginAttribute(hint.String()).AddMembershipMixedLowCardRef(lcrEncodingHint, ordinalParam(j)).EndAttribute()
					}
				}
				{ // value semantics
					for j, sem := range cp.ValueSemantics[i].IterateAspects() {
						txt.BeginAttribute(sem.String()).AddMembershipMixedLowCardRef(lcrValueSemantic, ordinalParam(j)).EndAttribute()
					}
				}
				{ // use aspects (section-level)
					if cc.UseAspects.IsValid() {
						for j, asp := range cc.UseAspects.IterateAspects() {
							txt.BeginAttribute(asp.String()).AddMembershipMixedLowCardRef(lcrUseAspect, ordinalParam(j)).EndAttribute()
						}
					}
				}
			}

			err = entity.CommitEntity()
			if err != nil {
				err = eh.Errorf("unable to commit entity for column %s at index %d: %w", name, globalIndex, err)
				return
			}
		}
	}
	return
}

