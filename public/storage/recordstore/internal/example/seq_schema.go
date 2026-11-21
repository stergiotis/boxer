package example

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// GetSeqSchemaInManipulator builds the u64-Order, predicate-state-view
// reference schema (ADR-0100 Updates 2026-08-30): the Key is the leading
// EntityId (id), the Order is a SECOND EntityId column (eid) bound
// explicitly through gen.Input.Roles — the shape positional binding cannot
// produce — and there is deliberately NO u8 Lifecycle column: the state
// view is generated with TombstoneView, driven by the tombstone pair the
// store's constructor requires. The Order values are a caller-supplied
// sequence, not a clock; the SD2 contract (strictly monotonic per key) is
// the caller's obligation here exactly as on the timestamp lane.
func GetSeqSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("seq")
	manip.SetTableComment("ADR-0100 u64-Order reference schema")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "eid", ctabb.U64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)

	sec := manip.TaggedValueSection("measure").
		SectionStreamingGroup("data").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim)
	sec.TaggedValueColumn("value", ctabb.U64).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	return
}
