// Package stage2 is the leeway-backed mirror of the json/v2 ecsdemo (stage 1).
// It expresses the same drone entity through a bespoke leeway TableDesc, a
// marshallgen-generated codec, and a real ClickHouse roundtrip (clickhouse-local
// over an Arrow batch), so that "can this be unserialized?" is answered the same
// way over columnar storage as stage 1 answers it over json. It lives in its own
// package so stage 1 stays free of any leeway dependency.
//
// See ../EXPLANATION.md for the ECS background and the stage-1 <-> stage-2 map.
package stage2

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// TableRowConfig matches anchor's: multiple attributes per row.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// GetDroneSchemaInManipulator builds the bespoke drone schema: a dedicated,
// minimal leeway table carrying only the sections the DroneEntity DTO needs —
// symbol (status), u64Array (battery, written single-valued via the unit
// modifier) and symbolArray (tags) — plus the id plain column. It is the
// stage-2 analogue of stage-1's World schema.
func GetDroneSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("drone")
	manip.SetTableComment("bespoke leeway schema for the ecsdemo drone entity")
	loadDroneSchema(manip)
	return
}

func loadDroneSchema(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)

	channels := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecLowCardVerbatim,
		common.MembershipSpecHighCardRef,
		common.MembershipSpecMixedLowCardRefHighCardParameters,
	}

	secSymbol := manip.TaggedValueSection("symbol").
		SectionStreamingGroup("data").
		AddSectionMembership(channels...)
	secSymbol.TaggedValueColumn("value", ctabb.S).
		AddColumnValueSemantics(valueaspects.AspectCanonicalizedValue).
		AddColumnEncodingHints(easp.AspectInterRecordLowCardinality,
			easp.AspectIntraRecordLowCardinality,
			easp.AspectLightGeneralCompression)

	u64h := canonicaltypes.PromoteScalarPrim(ctabb.U64, canonicaltypes.ScalarModifierHomogenousArray)
	secU64 := manip.TaggedValueSection("u64Array").
		SectionStreamingGroup("data").
		AddSectionMembership(channels...)
	secU64.TaggedValueColumn("value", u64h).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)

	secSymArray := manip.TaggedValueSection("symbolArray").
		SectionStreamingGroup("data").
		AddSectionMembership(channels...)
	secSymArray.TaggedValueColumn("value", ctabb.Sh).
		AddColumnValueSemantics(valueaspects.AspectCanonicalizedValue).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
}
