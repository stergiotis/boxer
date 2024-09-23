// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type CmdLine struct {
	_tab flatbuffers.Table
}

func GetRootAsCmdLine(buf []byte, offset flatbuffers.UOffsetT) *CmdLine {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &CmdLine{}
	x.Init(buf, n+offset)
	return x
}

func FinishCmdLineBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsCmdLine(buf []byte, offset flatbuffers.UOffsetT) *CmdLine {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &CmdLine{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedCmdLineBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *CmdLine) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *CmdLine) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *CmdLine) P1(obj *SingleVec2) *SingleVec2 {
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

func (rcv *CmdLine) P2(obj *SingleVec2) *SingleVec2 {
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

func (rcv *CmdLine) Col() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *CmdLine) MutateCol(n uint32) bool {
	return rcv._tab.MutateUint32Slot(8, n)
}

func (rcv *CmdLine) Thickness() float32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		return rcv._tab.GetFloat32(o + rcv._tab.Pos)
	}
	return 0.0
}

func (rcv *CmdLine) MutateThickness(n float32) bool {
	return rcv._tab.MutateFloat32Slot(10, n)
}

func CmdLineStart(builder *flatbuffers.Builder) {
	builder.StartObject(4)
}
func CmdLineAddP1(builder *flatbuffers.Builder, p1 flatbuffers.UOffsetT) {
	builder.PrependStructSlot(0, flatbuffers.UOffsetT(p1), 0)
}
func CmdLineAddP2(builder *flatbuffers.Builder, p2 flatbuffers.UOffsetT) {
	builder.PrependStructSlot(1, flatbuffers.UOffsetT(p2), 0)
}
func CmdLineAddCol(builder *flatbuffers.Builder, col uint32) {
	builder.PrependUint32Slot(2, col, 0)
}
func CmdLineAddThickness(builder *flatbuffers.Builder, thickness float32) {
	builder.PrependFloat32Slot(3, thickness, 0.0)
}
func CmdLineEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
