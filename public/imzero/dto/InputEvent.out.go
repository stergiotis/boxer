// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type InputEvent struct {
	_tab flatbuffers.Table
}

func GetRootAsInputEvent(buf []byte, offset flatbuffers.UOffsetT) *InputEvent {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &InputEvent{}
	x.Init(buf, n+offset)
	return x
}

func FinishInputEventBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsInputEvent(buf []byte, offset flatbuffers.UOffsetT) *InputEvent {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &InputEvent{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedInputEventBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *InputEvent) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *InputEvent) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *InputEvent) EventType() UserInteraction {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return UserInteraction(rcv._tab.GetByte(o + rcv._tab.Pos))
	}
	return 0
}

func (rcv *InputEvent) MutateEventType(n UserInteraction) bool {
	return rcv._tab.MutateByteSlot(4, byte(n))
}

func (rcv *InputEvent) Event(obj *flatbuffers.Table) bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		rcv._tab.Union(obj, o)
		return true
	}
	return false
}

func InputEventStart(builder *flatbuffers.Builder) {
	builder.StartObject(2)
}
func InputEventAddEventType(builder *flatbuffers.Builder, eventType UserInteraction) {
	builder.PrependByteSlot(0, byte(eventType), 0)
}
func InputEventAddEvent(builder *flatbuffers.Builder, event flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(1, flatbuffers.UOffsetT(event), 0)
}
func InputEventEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}