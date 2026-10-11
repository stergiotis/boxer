package agentrunstore

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// TableNameBody is the vault table's bare name (ADR-0320 §SD1).
const TableNameBody = "agentrunbody"

// GetBodySchemaInManipulator builds the vault table: envelope id (the run
// id) and ts (the ordinal as an instant, as the entry table), the ordinal
// again as an attribute the deletion names rows by, the opening the
// ledger's commitments open under, and the content. One row per entry
// that carries content; rows are destroyed, never rewritten, so the
// table has no lifecycle column and takes the heavyweight mutation the
// ledger never sees.
func GetBodySchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableNameBody)
	manip.SetTableComment("agent run journal bodies, one row per entry with content, destroyed on erasure (ADR-0320 §SD1)")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.S).AddColumnEncodingHints(plain...)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64).AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	sec := sectionOf(manip)
	sec("bodyOrdinal", ctabb.U32, plain...)
	sec("bodyOpening", ctabb.S, plain...)
	sec("bodyText", ctabb.S, easp.AspectHeavyGeneralCompression)
	sec("bodyCalls", ctabb.S, plain...)
	return
}
