// Package example is the ADR-0100 slice-S1 worked example: a small
// "device" fact table with three ECS-style components (Identity, Battery,
// Tagged), the reference store over it, and the clickhouse-local round-trip
// test. Its generated files (*.out.go, *.out.sql) come from the existing
// leeway generators, driven by gen_test.go the same way
// leeway/anchor/ecsdemo/stage2 drives them; the hand-written store here is
// the reference the recordstore/gen emitter must reproduce.
package example

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

// GetDeviceSchemaInManipulator builds the device fact table. The three
// envelope plain columns carry the ADR-0100 roles — id (Key), ts (Order),
// lifecycle (Lifecycle; 0 = live, 1 = tombstone) — and each component owns
// one tagged section: symbol (Identity.Status), u64Array (Battery.Charge,
// single-valued via the unit modifier) and symbolArray (Tagged.Tags).
func GetDeviceSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("device")
	manip.SetTableComment("ADR-0100 recordstore example schema")
	loadDeviceSchema(manip)
	return
}

func loadDeviceSchema(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityLifecycle, "lifecycle", ctabb.U8).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)

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
