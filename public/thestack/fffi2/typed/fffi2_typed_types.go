//go:build llm_generated_opus47

package typed

import (
	"encoding/binary"
	"unique"

	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
)

type RetainedElementId uint64
type RetainedFffiHolder struct {
	interned          unique.Handle[string] // keeps the intern entry alive
	content           []byte
	retainedElementId RetainedElementId
	widgetIdOffset    uint32
}
type RetainedFffiHolderTyped[T any] struct {
	_                 T
	interned          unique.Handle[string]
	content           []byte
	retainedElementId RetainedElementId
	widgetIdOffset    uint32
}
type RetainedFffiBuilder struct {
	builder        *retainedFffiBuilderPooled
	widgetIdOffset uint32
}

var _ runtime.MarshallWriterI = (*RetainedFffiBuilder)(nil)

// MarkWidgetIdOffset records the current write position as the location of
// the widget ID in the buffer. Must be called immediately before WriteUint64
// writes the widget ID.
func (inst *RetainedFffiBuilder) MarkWidgetIdOffset() {
	inst.widgetIdOffset = uint32(inst.builder.buf.Len())
}

// WriteWidgetId records the current buffer position as the widget ID offset
// and then writes the ID. Generated factory code should call this instead of
// a bare WriteUint64 for the widget ID argument.
func (inst *RetainedFffiBuilder) WriteWidgetId(id uint64) {
	inst.widgetIdOffset = uint32(inst.builder.buf.Len())
	inst.builder.marshaller.WriteUint64(id)
}

// GetWidgetHandle returns a WidgetHandle for the retained holder's widget ID.
// Returns widgethandle.NoWidget if this holder does not contain a widget ID
// (widgetIdOffset == 0 and the bytes at that offset are not a valid ID).
func (inst RetainedFffiHolderTyped[T]) GetWidgetHandle() widgethandle.WidgetHandle {
	off := inst.widgetIdOffset
	if int(off)+8 > len(inst.content) {
		return widgethandle.NoWidget
	}
	id := binary.LittleEndian.Uint64(inst.content[off : off+8])
	return widgethandle.Make(id)
}

// GetWidgetHandle returns a WidgetHandle for the retained holder's widget ID.
func (inst *RetainedFffiHolder) GetWidgetHandle() widgethandle.WidgetHandle {
	off := inst.widgetIdOffset
	if int(off)+8 > len(inst.content) {
		return widgethandle.NoWidget
	}
	id := binary.LittleEndian.Uint64(inst.content[off : off+8])
	return widgethandle.Make(id)
}
