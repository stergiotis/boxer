package typed

import (
	"encoding/binary"
	"unique"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
)

type RetainedElementId uint64
type RetainedFffiHolder struct {
	interned          unique.Handle[string] // keeps the intern entry alive
	content           []byte
	retainedElementId RetainedElementId
	widgetIdOffset    uint32
	hasWidgetId       bool
}
type RetainedFffiHolderTyped[T any] struct {
	_                 T
	interned          unique.Handle[string]
	content           []byte
	retainedElementId RetainedElementId
	widgetIdOffset    uint32
	hasWidgetId       bool
}
type RetainedFffiBuilder struct {
	builder        *retainedFffiBuilderPooled
	widgetIdOffset uint32
	hasWidgetId    bool
}

var _ runtime.MarshallWriterI = (*RetainedFffiBuilder)(nil)

// MarkWidgetIdOffset records the current write position as the location of
// the widget ID in the buffer. Must be called immediately before WriteUint64
// writes the widget ID.
func (inst *RetainedFffiBuilder) MarkWidgetIdOffset() {
	inst.widgetIdOffset = uint32(inst.builder.buf.Len())
	inst.hasWidgetId = true
}

// WriteWidgetId records the current buffer position as the widget ID offset
// and then writes the ID. Generated factory code should call this instead of
// a bare WriteUint64 for the widget ID argument.
func (inst *RetainedFffiBuilder) WriteWidgetId(id uint64) {
	inst.widgetIdOffset = uint32(inst.builder.buf.Len())
	inst.hasWidgetId = true
	inst.builder.marshaller.WriteUint64(id)
}

// GetWidgetHandle returns a WidgetHandle for the retained holder's widget ID.
// Returns widgethandle.NoWidget if the builder never recorded a widget ID
// (neither WriteWidgetId nor MarkWidgetIdOffset was called).
func (inst RetainedFffiHolderTyped[T]) GetWidgetHandle() widgethandle.WidgetHandle {
	off := inst.widgetIdOffset
	if !inst.hasWidgetId || int(off)+8 > len(inst.content) {
		return widgethandle.NoWidget
	}
	id := binary.LittleEndian.Uint64(inst.content[off : off+8])
	return widgethandle.Make(id)
}

// GetWidgetHandle returns a WidgetHandle for the retained holder's widget ID,
// or widgethandle.NoWidget if the holder carries none.
func (inst *RetainedFffiHolder) GetWidgetHandle() widgethandle.WidgetHandle {
	off := inst.widgetIdOffset
	if !inst.hasWidgetId || int(off)+8 > len(inst.content) {
		return widgethandle.NoWidget
	}
	id := binary.LittleEndian.Uint64(inst.content[off : off+8])
	return widgethandle.Make(id)
}
