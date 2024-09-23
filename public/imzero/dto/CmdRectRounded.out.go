// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type CmdRectRounded struct {
	_tab flatbuffers.Table
}

func GetRootAsCmdRectRounded(buf []byte, offset flatbuffers.UOffsetT) *CmdRectRounded {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &CmdRectRounded{}
	x.Init(buf, n+offset)
	return x
}

func FinishCmdRectRoundedBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsCmdRectRounded(buf []byte, offset flatbuffers.UOffsetT) *CmdRectRounded {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &CmdRectRounded{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedCmdRectRoundedBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *CmdRectRounded) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *CmdRectRounded) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *CmdRectRounded) PMin(obj *SingleVec2) *SingleVec2 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		x := o + rcv._tab.Pos
		if obj == nil {
			obj = new(SingleVec2)
		}
		obj.Init(rcv._tab.Bytes, x)
		return obj
	}
	return nil
}

func (rcv *CmdRectRounded) PMax(obj *SingleVec2) *SingleVec2 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		x := o + rcv._tab.Pos
		if obj == nil {
			obj = new(SingleVec2)
		}
		obj.Init(rcv._tab.Bytes, x)
		return obj
	}
	return nil
}

func (rcv *CmdRectRounded) Col() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *CmdRectRounded) MutateCol(n uint32) bool {
	return rcv._tab.MutateUint32Slot(8, n)
}

func (rcv *CmdRectRounded) Rounding() float32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		return rcv._tab.GetFloat32(o + rcv._tab.Pos)
	}
	return 0.0
}

func (rcv *CmdRectRounded) MutateRounding(n float32) bool {
	return rcv._tab.MutateFloat32Slot(10, n)
}

func (rcv *CmdRectRounded) Thickness() float32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(12))
	if o != 0 {
		return rcv._tab.GetFloat32(o + rcv._tab.Pos)
	}
	return 0.0
}

func (rcv *CmdRectRounded) MutateThickness(n float32) bool {
	return rcv._tab.MutateFloat32Slot(12, n)
}

func CmdRectRoundedStart(builder *flatbuffers.Builder) {
	builder.StartObject(5)
}
func CmdRectRoundedAddPMin(builder *flatbuffers.Builder, pMin flatbuffers.UOffsetT) {
	builder.PrependStructSlot(0, flatbuffers.UOffsetT(pMin), 0)
}
func CmdRectRoundedAddPMax(builder *flatbuffers.Builder, pMax flatbuffers.UOffsetT) {
	builder.PrependStructSlot(1, flatbuffers.UOffsetT(pMax), 0)
}
func CmdRectRoundedAddCol(builder *flatbuffers.Builder, col uint32) {
	builder.PrependUint32Slot(2, col, 0)
}
func CmdRectRoundedAddRounding(builder *flatbuffers.Builder, rounding float32) {
	builder.PrependFloat32Slot(3, rounding, 0.0)
}
func CmdRectRoundedAddThickness(builder *flatbuffers.Builder, thickness float32) {
	builder.PrependFloat32Slot(4, thickness, 0.0)
}
func CmdRectRoundedEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
