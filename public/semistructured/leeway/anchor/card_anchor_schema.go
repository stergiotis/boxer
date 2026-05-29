package anchor

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

func GetSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator: %w", err)
		return
	}
	manip.SetTableName("example")
	manip.SetTableComment("an example leeway table")
	LoadExampleSchema(manip)
	return
}

func LoadExampleSchema(manip common.TableManipulatorFluidI) {
	ctId := ctabb.U64

	{ // plain
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctId).
			AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "naturalKey", ctabb.Y).
			AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	}
	{ // relation
		sec := manip.TaggedValueSection("foreignKey").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			SectionStreamingGroup("foreignKey").
			AddSectionUseAspects(useaspects.AspectLinking)
		sec.TaggedValueColumn("value", ctId).
			AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	}
	membershipSpec := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecLowCardVerbatim,
		common.MembershipSpecHighCardRef,
		common.MembershipSpecMixedLowCardRefHighCardParameters,
	}
	{ // data
		{
			sec := manip.TaggedValueSection("symbol").
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("value", ctabb.S).
				AddColumnValueSemantics(valueaspects.AspectCanonicalizedValue).
				AddColumnEncodingHints(easp.AspectInterRecordLowCardinality,
					easp.AspectIntraRecordLowCardinality,
					easp.AspectLightGeneralCompression)
		}
		{
			sec := manip.TaggedValueSection("symbolArray").
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("value", ctabb.Sh).
				AddColumnValueSemantics(valueaspects.AspectCanonicalizedValue).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		}
		{
			{
				sec := manip.TaggedValueSection("stringArray").
					SectionStreamingGroup("data").
					AddSectionMembership(membershipSpec...)
				sec.TaggedValueColumn("value", ctabb.Sh).
					AddColumnEncodingHints(easp.AspectLightGeneralCompression)
			}
			sec := manip.TaggedValueSection("text").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("text", ctabb.S)
			sec.TaggedValueColumn("wordLength", ctabb.U32h)
			sec.TaggedValueColumn("wordBag", ctabb.Sh)
		}
		{
			sec := manip.TaggedValueSection("blobArray").
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("value", ctabb.Yh).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		}
		for _, ct := range []canonicaltypes.PrimitiveAstNodeI{
			ctabb.U8, ctabb.U16, ctabb.U32, ctabb.U64,
			ctabb.I8, ctabb.I16, ctabb.I32, ctabb.I64,
		} {
			name := strings.ToLower(ct.String())
			{
				cth := canonicaltypes.PromoteScalarPrim(ct, canonicaltypes.ScalarModifierHomogenousArray)
				sec := manip.TaggedValueSection(naming.StylableName(name + "Array")).
					SectionStreamingGroup("data").
					AddSectionMembership(membershipSpec...)
				sec.TaggedValueColumn("value", cth).
					AddColumnEncodingHints(easp.AspectLightGeneralCompression)
			}
			switch ct {
			case ctabb.U32, ctabb.U64:
				{
					ctm := canonicaltypes.PromoteScalarPrim(ct, canonicaltypes.ScalarModifierSet)
					sec := manip.TaggedValueSection(naming.StylableName(name + "Set")).
						SectionStreamingGroup("data").
						AddSectionMembership(membershipSpec...)
					sec.TaggedValueColumn("value", ctm).
						AddColumnEncodingHints(easp.AspectLightGeneralCompression)
				}
			}
		}
		{
			sec := manip.TaggedValueSection("timeRange").
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("beginIncl", ctabb.Z64).
				AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
			sec.TaggedValueColumn("endExcl", ctabb.Z64).
				AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
		}
		for _, ct := range []canonicaltypes.PrimitiveAstNodeI{
			ctabb.F32, ctabb.F64,
		} {
			name := strings.ToLower(ct.String())
			{
				cth := canonicaltypes.PromoteScalarPrim(ct, canonicaltypes.ScalarModifierHomogenousArray)
				sec := manip.TaggedValueSection(naming.StylableName(name + "Array")).
					SectionStreamingGroup("data").
					AddSectionMembership(membershipSpec...)
				sec.TaggedValueColumn("value", cth).
					AddColumnEncodingHints(easp.AspectLightGeneralCompression, easp.AspectLightSlowlyChangingFloat)
			}
		}

		{
			// geoPoint and geoArea share a *streaming* group ("geo") for row
			// transport, but are deliberately NOT a co-section group: the data
			// generators give them independent per-entity cardinality (every
			// entity has a GeoPoint; only some have a GeoArea), so they are not
			// co-aligned. SectionCoSectionGroup requires equal attribute counts
			// per entity — driveCoGroup would misread otherwise. (Contrast the
			// leewaywidgets fixture, where geo IS a co-section.) See SKILLS.md.
			sec := manip.TaggedValueSection("geoPoint").
				SectionStreamingGroup("geo").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("pointLat", ctabb.F32).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
			sec.TaggedValueColumn("pointLng", ctabb.F32).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
			sec.TaggedValueColumn("h3", ctabb.U64).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		}
		{
			sec := manip.TaggedValueSection("geoArea").
				SectionStreamingGroup("geo").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("polyLat", ctabb.F32h).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
			sec.TaggedValueColumn("polyLng", ctabb.F32h).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
			sec.TaggedValueColumn("h3", ctabb.U64m).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		}
		{
			sec := manip.TaggedValueSection(naming.StylableName("timeArray")).
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("value", ctabb.Z64h).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		}
	}
}
