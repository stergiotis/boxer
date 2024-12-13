// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type CmdSvgPathSubset struct {
	_tab flatbuffers.Table
}

func GetRootAsCmdSvgPathSubset(buf []byte, offset flatbuffers.UOffsetT) *CmdSvgPathSubset {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &CmdSvgPathSubset{}
	x.Init(buf, n+offset)
	return x
}

func FinishCmdSvgPathSubsetBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsCmdSvgPathSubset(buf []byte, offset flatbuffers.UOffsetT) *CmdSvgPathSubset {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &CmdSvgPathSubset{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedCmdSvgPathSubsetBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *CmdSvgPathSubset) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *CmdSvgPathSubset) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *CmdSvgPathSubset) Svg() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *CmdSvgPathSubset) Col() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *CmdSvgPathSubset) MutateCol(n uint32) bool {
	return rcv._tab.MutateUint32Slot(6, n)
}

func (rcv *CmdSvgPathSubset) Stroke() bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
	if o != 0 {
		return rcv._tab.GetBool(o + rcv._tab.Pos)
	}
	return false
}

func (rcv *CmdSvgPathSubset) MutateStroke(n bool) bool {
	return rcv._tab.MutateBoolSlot(8, n)
}

func (rcv *CmdSvgPathSubset) Fill() bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		return rcv._tab.GetBool(o + rcv._tab.Pos)
	}
	return false
}

func (rcv *CmdSvgPathSubset) MutateFill(n bool) bool {
	return rcv._tab.MutateBoolSlot(10, n)
}

func CmdSvgPathSubsetStart(builder *flatbuffers.Builder) {
	builder.StartObject(4)
}
func CmdSvgPathSubsetAddSvg(builder *flatbuffers.Builder, svg flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(0, flatbuffers.UOffsetT(svg), 0)
}
func CmdSvgPathSubsetAddCol(builder *flatbuffers.Builder, col uint32) {
	builder.PrependUint32Slot(1, col, 0)
}
func CmdSvgPathSubsetAddStroke(builder *flatbuffers.Builder, stroke bool) {
	builder.PrependBoolSlot(2, stroke, false)
}
func CmdSvgPathSubsetAddFill(builder *flatbuffers.Builder, fill bool) {
	builder.PrependBoolSlot(3, fill, false)
}
func CmdSvgPathSubsetEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
