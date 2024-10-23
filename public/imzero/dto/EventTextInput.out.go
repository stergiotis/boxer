// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type EventTextInput struct {
	_tab flatbuffers.Table
}

func GetRootAsEventTextInput(buf []byte, offset flatbuffers.UOffsetT) *EventTextInput {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &EventTextInput{}
	x.Init(buf, n+offset)
	return x
}

func FinishEventTextInputBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsEventTextInput(buf []byte, offset flatbuffers.UOffsetT) *EventTextInput {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &EventTextInput{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedEventTextInputBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *EventTextInput) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *EventTextInput) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *EventTextInput) Text() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func EventTextInputStart(builder *flatbuffers.Builder) {
	builder.StartObject(1)
}
func EventTextInputAddText(builder *flatbuffers.Builder, text flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(0, flatbuffers.UOffsetT(text), 0)
}
func EventTextInputEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}