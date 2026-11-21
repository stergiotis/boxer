package example

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// GetWidgetSchemaInManipulator builds the pass-through reference schema: the
// device schema's sections and roles (id = Key, ts = Order, lifecycle = the u8
// state-view marker) plus a backbone richer than that role model, so the extra
// plain columns exercise the generated envelope (ADR-0100 Update 2026-07-09).
// The second id (alt), a routing scalar (region) and a routing set (tags) are
// not roles — they ride through WidgetEnvelope, written on Begin and read back
// onto WidgetEntity. It reuses loadDeviceSchema so the driven DML / read-access
// generators see the same section shapes device does (a plain-only table leaves
// several of their imports unused); no component binds those sections here.
func GetWidgetSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("widget")
	manip.SetTableComment("ADR-0100 pass-through envelope reference schema")
	loadDeviceSchema(manip)

	// The pass-through backbone: a second EntityId, plus a routing scalar and
	// a routing set. None is a role, so each becomes a WidgetEnvelope field.
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "alt", ctabb.U64).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityRouting, "region", ctabb.U64).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityRouting, "tags", ctabb.Sm).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	return
}
