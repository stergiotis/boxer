// Schema for the leewaywidgets fixture: a single in-memory leeway table that
// the demo + play app drive through streamreadaccess.Driver. The TableDesc
// is the source of truth — fixture_dml.out.go is regenerated from it via
// `go generate ./...` (see fixture_gen_test.go) and the populator in
// fixture.go uses the generated InEntityFixture to produce sample
// arrow.RecordBatches.

package leewaywidgets

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// FixtureTableName is the name passed to the DML / RA code generator and to
// driver construction. It also keys the generated InEntity<Name> type:
// InEntityFixture.
const FixtureTableName = "fixture"

// FixtureTableRowConfig is the row-config the demo runs against. Multi-attrs
// per row matches the streaming convention used elsewhere in the project.
const FixtureTableRowConfig = common.TableRowConfigMultiAttributesPerRow

// LoadFixtureSchema declares the leeway TableDesc that backs the demo. Three
// section shapes are exercised:
//
//   - A plain section ("entity-id") with three columns: a u64 id, a string
//     internal-key tagged AspectMachineReadable only (hidden by the Table
//     emitter), and a string natural-key with both readable aspects.
//   - A tagged section "metric" carrying all five MembershipSpec slots
//     (LowCardRef / LowCardVerbatim / LowCardRefParametrized /
//     MixedLowCardRefHighCardParameters /
//     MixedLowCardVerbatimHighCardParameters) so every AddMembership* path
//     in the SinkI surface gets traffic. One column ("rawBlob") is
//     MachineReadable-only and is hidden by the Table emitter.
//   - A co-section group "geo" of two sections (geoPoint, geoArea) sharing
//     a streaming-group key, so the driver merges them into one wide
//     section under BeginCoSectionGroup.
func LoadFixtureSchema(manip common.TableManipulatorFluidI) {
	// --- plain section: entity identity ---
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "internalKey", ctabb.S).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectMachineReadable)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "naturalKey", ctabb.S).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)

	// --- tagged section: metric (all five membership shapes) ---
	metricSpec := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecLowCardVerbatim,
		common.MembershipSpecLowCardRefParametrized,
		common.MembershipSpecMixedLowCardRefHighCardParameters,
		common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
	}
	metric := manip.TaggedValueSection("metric").
		SectionStreamingGroup("data").
		AddSectionMembership(metricSpec...)
	metric.TaggedValueColumn("value", ctabb.F64).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
	metric.TaggedValueColumn("bins", ctabb.U32m).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
	metric.TaggedValueColumn("tags", ctabb.Sh).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
	metric.TaggedValueColumn("rawBlob", ctabb.S).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectMachineReadable)

	// --- co-section group: geo (geoPoint + geoArea, flattened) ---
	// CoSectionGroup is what makes the driver merge the two sections into one
	// wide BeginSection (buildCoGroups keys off it). StreamingGroup is the
	// orthogonal row-transport concern; both happen to be "geo" here. Without
	// the CoSectionGroup the sections render standalone, not merged.
	geoPoint := manip.TaggedValueSection("geoPoint").
		SectionCoSectionGroup("geo").
		SectionStreamingGroup("geo").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim)
	geoPoint.TaggedValueColumn("lat", ctabb.F32).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
	geoPoint.TaggedValueColumn("lng", ctabb.F32).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)

	geoArea := manip.TaggedValueSection("geoArea").
		SectionCoSectionGroup("geo").
		SectionStreamingGroup("geo")
	geoArea.TaggedValueColumn("poly", ctabb.F32h).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
	geoArea.TaggedValueColumn("code", ctabb.S).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
}

// BuildFixtureTableDesc materialises a TableDesc from LoadFixtureSchema.
// Used by the codegen test (fixture_gen_test.go) and by fixture.go's
// driver setup so both consumers see exactly the same schema.
func BuildFixtureTableDesc() (tbl common.TableDesc, err error) {
	manip, err := common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator: %w", err)
		return
	}
	manip.SetTableName(FixtureTableName)
	manip.SetTableComment("leewaywidgets fixture — exercises all SinkI pathways")
	LoadFixtureSchema(manip)
	tbl, err = manip.BuildTableDesc()
	if err != nil {
		err = eh.Errorf("unable to build fixture table desc: %w", err)
		return
	}
	return
}
