package common

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"hash/fnv"
)

// Well-known low-card ref IDs for the symbol section attributes.
// These are FNV-1a hashes of the attribute names to ensure stable, collision-free identifiers.
var (
	lcrScope      = stableRef("scope")
	lcrItemType   = stableRef("itemType")
	lcrColumnRole = stableRef("columnRole")
	lcrSubType    = stableRef("subType")
)

// Well-known low-card ref IDs for the string section attributes.
var (
	lcrSectionName       = stableRef("sectionName")
	lcrLogicalColumnName = stableRef("logicalColumnName")
	lcrCanonicalType     = stableRef("canonicalType")
	lcrCoSectionGroup    = stableRef("coSectionGroup")
	lcrStreamingGroup    = stableRef("streamingGroup")
	lcrTableComment      = stableRef("tableComment")
)

// Well-known low-card ref IDs for the u64 section attributes.
var (
	lcrLocalMonotonicIndex = stableRef("localMonotonicIndex")
)

// Well-known low-card ref IDs for the text section attributes.
var (
	lcrEncodingHint  = stableRef("encodingHint")
	lcrValueSemantic = stableRef("valueSemantic")
	lcrUseAspect     = stableRef("useAspect")
)

func stableRef(name string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return h.Sum64()
}

// ordinalParam encodes an aspect's position within its set into the membership
// params channel, in the canonical form ([membership.AppendParams]) rather than
// a private binary layout — a raw little-endian uint64 rendered through
// [membership.DefaultParamsFormatter], which is `string(raw)`, is eight
// unprintable bytes.
func ordinalParam(ordinal int) (raw []byte, err error) {
	raw, err = membership.EncodeParams(uint64(ordinal))
	return
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
				var p []byte
				{ // encoding hints
					for j, hint := range cp.EncodingHints[i].IterateAspects() {
						p, err = ordinalParam(j)
						if err != nil {
							return
						}
						txt.BeginAttribute(hint.String()).AddMembershipMixedLowCardRef(lcrEncodingHint, p).EndAttribute()
					}
				}
				{ // value semantics
					for j, sem := range cp.ValueSemantics[i].IterateAspects() {
						p, err = ordinalParam(j)
						if err != nil {
							return
						}
						txt.BeginAttribute(sem.String()).AddMembershipMixedLowCardRef(lcrValueSemantic, p).EndAttribute()
					}
				}
				{ // use aspects (section-level)
					if cc.UseAspects.IsValid() {
						for j, asp := range cc.UseAspects.IterateAspects() {
							p, err = ordinalParam(j)
							if err != nil {
								return
							}
							txt.BeginAttribute(asp.String()).AddMembershipMixedLowCardRef(lcrUseAspect, p).EndAttribute()
						}
					}
				}
			}

			err = entity.CommitEntity()
			if err != nil {
				err = eb.Build().Stringer("name", name).Uint32("globalIndex", globalIndex).Errorf("unable to commit the entity for this column: %w", err)
				return
			}
		}
	}
	return
}
