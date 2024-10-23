// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type CmdEllipseFilled struct {
	_tab flatbuffers.Table
}

func GetRootAsCmdEllipseFilled(buf []byte, offset flatbuffers.UOffsetT) *CmdEllipseFilled {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &CmdEllipseFilled{}
	x.Init(buf, n+offset)
	return x
}

func FinishCmdEllipseFilledBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsCmdEllipseFilled(buf []byte, offset flatbuffers.UOffsetT) *CmdEllipseFilled {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &CmdEllipseFilled{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedCmdEllipseFilledBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *CmdEllipseFilled) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *CmdEllipseFilled) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *CmdEllipseFilled) Center(obj *SingleVec2) *SingleVec2 {
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

func (rcv *CmdEllipseFilled) Radius(obj *SingleVec2) *SingleVec2 {
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

func (rcv *CmdEllipseFilled) Col() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *CmdEllipseFilled) MutateCol(n uint32) bool {
	return rcv._tab.MutateUint32Slot(8, n)
}

func (rcv *CmdEllipseFilled) Rot() float32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		return rcv._tab.GetFloat32(o + rcv._tab.Pos)
	}
	return 0.0
}

func (rcv *CmdEllipseFilled) MutateRot(n float32) bool {
	return rcv._tab.MutateFloat32Slot(10, n)
}

func (rcv *CmdEllipseFilled) NumSegments() int32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(12))
	if o != 0 {
		return rcv._tab.GetInt32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *CmdEllipseFilled) MutateNumSegments(n int32) bool {
	return rcv._tab.MutateInt32Slot(12, n)
}

func CmdEllipseFilledStart(builder *flatbuffers.Builder) {
	builder.StartObject(5)
}
func CmdEllipseFilledAddCenter(builder *flatbuffers.Builder, center flatbuffers.UOffsetT) {
	builder.PrependStructSlot(0, flatbuffers.UOffsetT(center), 0)
}
func CmdEllipseFilledAddRadius(builder *flatbuffers.Builder, radius flatbuffers.UOffsetT) {
	builder.PrependStructSlot(1, flatbuffers.UOffsetT(radius), 0)
}
func CmdEllipseFilledAddCol(builder *flatbuffers.Builder, col uint32) {
	builder.PrependUint32Slot(2, col, 0)
}
func CmdEllipseFilledAddRot(builder *flatbuffers.Builder, rot float32) {
	builder.PrependFloat32Slot(3, rot, 0.0)
}
func CmdEllipseFilledAddNumSegments(builder *flatbuffers.Builder, numSegments int32) {
	builder.PrependInt32Slot(4, numSegments, 0)
}
func CmdEllipseFilledEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}