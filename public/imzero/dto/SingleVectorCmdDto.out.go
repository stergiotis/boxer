// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type SingleVectorCmdDto struct {
	_tab flatbuffers.Table
}

func GetRootAsSingleVectorCmdDto(buf []byte, offset flatbuffers.UOffsetT) *SingleVectorCmdDto {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &SingleVectorCmdDto{}
	x.Init(buf, n+offset)
	return x
}

func FinishSingleVectorCmdDtoBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsSingleVectorCmdDto(buf []byte, offset flatbuffers.UOffsetT) *SingleVectorCmdDto {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &SingleVectorCmdDto{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedSingleVectorCmdDtoBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *SingleVectorCmdDto) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *SingleVectorCmdDto) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *SingleVectorCmdDto) ArgType() VectorCmdArg {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return VectorCmdArg(rcv._tab.GetByte(o + rcv._tab.Pos))
	}
	return 0
}

func (rcv *SingleVectorCmdDto) MutateArgType(n VectorCmdArg) bool {
	return rcv._tab.MutateByteSlot(4, byte(n))
}

func (rcv *SingleVectorCmdDto) Arg(obj *flatbuffers.Table) bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		rcv._tab.Union(obj, o)
		return true
	}
	return false
}

func SingleVectorCmdDtoStart(builder *flatbuffers.Builder) {
	builder.StartObject(2)
}
func SingleVectorCmdDtoAddArgType(builder *flatbuffers.Builder, argType VectorCmdArg) {
	builder.PrependByteSlot(0, byte(argType), 0)
}
func SingleVectorCmdDtoAddArg(builder *flatbuffers.Builder, arg flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(1, flatbuffers.UOffsetT(arg), 0)
}
func SingleVectorCmdDtoEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}