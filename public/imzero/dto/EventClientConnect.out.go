// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type EventClientConnect struct {
	_tab flatbuffers.Table
}

func GetRootAsEventClientConnect(buf []byte, offset flatbuffers.UOffsetT) *EventClientConnect {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &EventClientConnect{}
	x.Init(buf, n+offset)
	return x
}

func FinishEventClientConnectBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsEventClientConnect(buf []byte, offset flatbuffers.UOffsetT) *EventClientConnect {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &EventClientConnect{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedEventClientConnectBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *EventClientConnect) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *EventClientConnect) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *EventClientConnect) Desc() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func EventClientConnectStart(builder *flatbuffers.Builder) {
	builder.StartObject(1)
}
func EventClientConnectAddDesc(builder *flatbuffers.Builder, desc flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(0, flatbuffers.UOffsetT(desc), 0)
}
func EventClientConnectEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
