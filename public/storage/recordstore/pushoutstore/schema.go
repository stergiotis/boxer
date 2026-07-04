// Package pushoutstore adapts a generated recordstore to the pushout
// repo.StorageI persistence seam (ADR-0100 S3, ADR-0079): envelopes,
// the applied log, the snapshot and the retention ledger persist as
// append-only rows of one ClickHouse fact table keyed by a string
// namespace ("env/<hex>", "log", "snapshot", "retention").
//
// The applied log needs no generation marker: ReplaceApplied appends a
// state-view tombstone on the log key followed by the new entries — all
// in one Arrow insert — and LoadApplied replays the key keeping only the
// entries after the last tombstone.
package pushoutstore

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// TableRowConfig matches anchor's: multiple attributes per row.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// GetPushoutSchemaInManipulator builds the pushout fact table. Envelope
// roles: id (string Key — the namespace-prefixed hex hash), ts (Order —
// synthetic per-key sequence nanos), lifecycle (Lifecycle — the log
// tombstone). Every component owns distinct sections so the per-kind
// membership ids cannot collide in storage.
func GetPushoutSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("pushout")
	manip.SetTableComment("pushout repo.StorageI persistence (ADR-0100 S3)")
	loadPushoutSchema(manip)
	return
}

func loadPushoutSchema(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.S).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
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
	section := func(name naming.StylableName, ct canonicaltypes.PrimitiveAstNodeI) {
		sec := manip.TaggedValueSection(name).
			SectionStreamingGroup("data").
			AddSectionMembership(channels...)
		sec.TaggedValueColumn("value", ct).
			AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	}

	u64Arr := canonicaltypes.PromoteScalarPrim(ctabb.U64, canonicaltypes.ScalarModifierHomogenousArray)
	i64Arr := canonicaltypes.PromoteScalarPrim(ctabb.I64, canonicaltypes.ScalarModifierHomogenousArray)
	section("envBlob", ctabb.Y)      // Envelope.Framed (PXE1 bytes)
	section("logHash", ctabb.S)      // LogEntry.Hash (hex, high-card)
	section("snapGraggle", ctabb.Y)  // Snapshot.Graggle (opaque)
	section("snapApplied", ctabb.Sh) // Snapshot.Applied (hex list, ordered)
	section("retHash", ctabb.Sh)     // Retention node patch hashes
	section("retIndex", u64Arr)      // Retention node indices
	section("retTime", i64Arr)       // Retention first-observed-deleted nanos
}
