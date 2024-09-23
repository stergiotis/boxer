// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type CmdPath struct {
	_tab flatbuffers.Table
}

func GetRootAsCmdPath(buf []byte, offset flatbuffers.UOffsetT) *CmdPath {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &CmdPath{}
	x.Init(buf, n+offset)
	return x
}

func FinishCmdPathBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsCmdPath(buf []byte, offset flatbuffers.UOffsetT) *CmdPath {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &CmdPath{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedCmdPathBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *CmdPath) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *CmdPath) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *CmdPath) Offset(obj *SingleVec2) *SingleVec2 {
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

func (rcv *CmdPath) Verbs(j int) PathVerb {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		a := rcv._tab.Vector(o)
		return PathVerb(rcv._tab.GetByte(a + flatbuffers.UOffsetT(j*1)))
	}
	return 0
}

func (rcv *CmdPath) VerbsLength() int {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.VectorLen(o)
	}
	return 0
}

func (rcv *CmdPath) VerbsBytes() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *CmdPath) MutateVerbs(j int, n PathVerb) bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		a := rcv._tab.Vector(o)
		return rcv._tab.MutateByte(a+flatbuffers.UOffsetT(j*1), byte(n))
	}
	return false
}

func (rcv *CmdPath) PointsXy(j int) float32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
	if o != 0 {
		a := rcv._tab.Vector(o)
		return rcv._tab.GetFloat32(a + flatbuffers.UOffsetT(j*4))
	}
	return 0
}

func (rcv *CmdPath) PointsXyLength() int {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
	if o != 0 {
		return rcv._tab.VectorLen(o)
	}
	return 0
}

func (rcv *CmdPath) MutatePointsXy(j int, n float32) bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
	if o != 0 {
		a := rcv._tab.Vector(o)
		return rcv._tab.MutateFloat32(a+flatbuffers.UOffsetT(j*4), n)
	}
	return false
}

func (rcv *CmdPath) ConicWeights(j int) float32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		a := rcv._tab.Vector(o)
		return rcv._tab.GetFloat32(a + flatbuffers.UOffsetT(j*4))
	}
	return 0
}

func (rcv *CmdPath) ConicWeightsLength() int {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		return rcv._tab.VectorLen(o)
	}
	return 0
}

func (rcv *CmdPath) MutateConicWeights(j int, n float32) bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		a := rcv._tab.Vector(o)
		return rcv._tab.MutateFloat32(a+flatbuffers.UOffsetT(j*4), n)
	}
	return false
}

func (rcv *CmdPath) Col() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(12))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *CmdPath) MutateCol(n uint32) bool {
	return rcv._tab.MutateUint32Slot(12, n)
}

func (rcv *CmdPath) Stroke() bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(14))
	if o != 0 {
		return rcv._tab.GetBool(o + rcv._tab.Pos)
	}
	return false
}

func (rcv *CmdPath) MutateStroke(n bool) bool {
	return rcv._tab.MutateBoolSlot(14, n)
}

func (rcv *CmdPath) Fill() bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(16))
	if o != 0 {
		return rcv._tab.GetBool(o + rcv._tab.Pos)
	}
	return false
}

func (rcv *CmdPath) MutateFill(n bool) bool {
	return rcv._tab.MutateBoolSlot(16, n)
}

func (rcv *CmdPath) FillType() PathFillType {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(18))
	if o != 0 {
		return PathFillType(rcv._tab.GetByte(o + rcv._tab.Pos))
	}
	return 0
}

func (rcv *CmdPath) MutateFillType(n PathFillType) bool {
	return rcv._tab.MutateByteSlot(18, byte(n))
}

func CmdPathStart(builder *flatbuffers.Builder) {
	builder.StartObject(8)
}
func CmdPathAddOffset(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.PrependStructSlot(0, flatbuffers.UOffsetT(offset), 0)
}
func CmdPathAddVerbs(builder *flatbuffers.Builder, verbs flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(1, flatbuffers.UOffsetT(verbs), 0)
}
func CmdPathStartVerbsVector(builder *flatbuffers.Builder, numElems int) flatbuffers.UOffsetT {
	return builder.StartVector(1, numElems, 1)
}
func CmdPathAddPointsXy(builder *flatbuffers.Builder, pointsXy flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(2, flatbuffers.UOffsetT(pointsXy), 0)
}
func CmdPathStartPointsXyVector(builder *flatbuffers.Builder, numElems int) flatbuffers.UOffsetT {
	return builder.StartVector(4, numElems, 4)
}
func CmdPathAddConicWeights(builder *flatbuffers.Builder, conicWeights flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(3, flatbuffers.UOffsetT(conicWeights), 0)
}
func CmdPathStartConicWeightsVector(builder *flatbuffers.Builder, numElems int) flatbuffers.UOffsetT {
	return builder.StartVector(4, numElems, 4)
}
func CmdPathAddCol(builder *flatbuffers.Builder, col uint32) {
	builder.PrependUint32Slot(4, col, 0)
}
func CmdPathAddStroke(builder *flatbuffers.Builder, stroke bool) {
	builder.PrependBoolSlot(5, stroke, false)
}
func CmdPathAddFill(builder *flatbuffers.Builder, fill bool) {
	builder.PrependBoolSlot(6, fill, false)
}
func CmdPathAddFillType(builder *flatbuffers.Builder, fillType PathFillType) {
	builder.PrependByteSlot(7, byte(fillType), 0)
}
func CmdPathEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
