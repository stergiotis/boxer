//go:build llm_generated_opus46

package common

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

const SystemTableColumnsTableRowConfig = TableRowConfigMultiAttributesPerRow

func GetSystemTableColumnsManipulator() (manip *TableManipulator, err error) {
	manip, err = NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator: %w", err)
		return
	}
	manip.SetTableName("systemTableColumns")
	manip.SetTableComment("Leeway schema catalog: one entity per physical column of a described table")
	LoadSystemTableColumnsSchema(manip)
	return
}

func LoadSystemTableColumnsSchema(manip TableManipulatorFluidI) {
	membershipSpec := []MembershipSpecE{
		MembershipSpecLowCardRef,
		MembershipSpecHighCardRef,
		MembershipSpecMixedLowCardRefHighCardParameters,
	}
	{ // plain values
		manip.PlainValueColumn(PlainItemTypeEntityId, "tableHash", ctabb.U64).
			AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
		manip.PlainValueColumn(PlainItemTypeEntityId, "columnIndex", ctabb.U64).
			AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
		manip.PlainValueColumn(PlainItemTypeEntityRouting, "tableName", ctabb.S).
			AddColumnEncodingHints(easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	}
	{ // symbol section — low-cardinality categorical column metadata
		sec := manip.TaggedValueSection("symbol").
			SectionStreamingGroup("meta").
			AddSectionMembership(membershipSpec...)
		sec.TaggedValueColumn("value", ctabb.S).
			AddColumnValueSemantics(valueaspects.AspectCanonicalizedValue).
			AddColumnEncodingHints(easp.AspectInterRecordLowCardinality,
				easp.AspectIntraRecordLowCardinality,
				easp.AspectLightGeneralCompression)
		// Attributes stored here:
		// scope (entity/transaction/opaque/tagged)
		// itemType (entity-id/entity-timestamp/entity-routing/entity-lifecycle/transaction/opaque)
		// columnRole (val/hr/hp/hv/lr/lp/lv/lmv/lmr/len/card/etc.)
		// subType (scalar/homogenous-array/set/membership/etc.)
		// membershipSpec (none/low-card-ref/high-card-ref/mixed/etc.)
	}
	{ // string section — variable-length string metadata
		sec := manip.TaggedValueSection("string").
			SectionStreamingGroup("meta").
			AddSectionMembership(membershipSpec...)
		sec.TaggedValueColumn("value", ctabb.S).
			AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		// Attributes stored here:
		// sectionName
		// logicalColumnName
		// canonicalType (serialized CT string, e.g. "u64", "f32h", "s")
		// coSectionGroup
		// streamingGroup
		// tableComment
	}
	{ // u64 section — numeric metadata
		sec := manip.TaggedValueSection("u64").
			SectionStreamingGroup("meta").
			AddSectionMembership(membershipSpec...)
		sec.TaggedValueColumn("value", ctabb.U64).
			AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		// Attributes stored here:
		// localMonotonicIndex
		// logicalIndex
	}
	{ // text section — aspect sets (one attribute per aspect in set)
		sec := manip.TaggedValueSection("text").
			SectionStreamingGroup("aspects").
			AddSectionMembership(membershipSpec...).
			AddSectionUseAspects(useaspects.AspectDocumentation)
		sec.TaggedValueColumn("value", ctabb.S).
			AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		// Attributes stored here:
		// encodingHint/<aspectName> — each encoding aspect in the column's set
		// valueSemantic/<aspectName> — each value aspect in the column's set
		// useAspect/<aspectName> — each use aspect in the section's set
	}
}
