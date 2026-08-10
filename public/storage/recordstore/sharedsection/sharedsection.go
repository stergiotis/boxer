// Package sharedsection is the ADR-0105 D2 worked example: two components
// (Label, State) binding ONE tagged section, generated under a
// caller-assigned membership-id wrapper (marshallgen.FixedIdsWrapper)
// instead of the default per-plan declaration-order ids. With
// registry-stable unique ids the disjoint-sections gate relaxes to
// id-level disjointness (ADR-0100 SD6, as corrected 2026-08-10), and the
// membership match — not a section partition — keeps the co-resident
// kinds apart. The generated files (*.out.go, *.out.sql) come from
// gen_test.go, like recordstore/example.
package sharedsection

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// TableRowConfig matches anchor's: multiple attributes per row — the shared
// section carries one attribute per resident component.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// AssetMembershipIdAssignment is the caller-assigned membership → id
// snapshot the store generates under, standing in for a registry
// resolution (vdd-style stable TaggedIds). Deliberately far from 1..N so
// a declaration-order id leaking into any artefact could not accidentally
// match.
var AssetMembershipIdAssignment = map[string]uint64{
	"assetName":  7001,
	"assetPhase": 7002,
}

// GetAssetSchemaInManipulator builds the asset table: plain id (Key) and
// ts (Order) envelope columns, and one tagged section `symbol` that BOTH
// components bind — the layout the default id regime must refuse and the
// fixed-ids regime supports.
func GetAssetSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("asset")
	manip.SetTableComment("ADR-0105 D2 shared-section example schema")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64)
	sec := manip.TaggedValueSection("symbol").
		SectionStreamingGroup("data").
		AddSectionMembership(common.MembershipSpecLowCardRef)
	sec.TaggedValueColumn("value", ctabb.S)
	return
}
