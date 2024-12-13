// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type CmdRenderUnicodeCodepoint struct {
	_tab flatbuffers.Table
}

func GetRootAsCmdRenderUnicodeCodepoint(buf []byte, offset flatbuffers.UOffsetT) *CmdRenderUnicodeCodepoint {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &CmdRenderUnicodeCodepoint{}
	x.Init(buf, n+offset)
	return x
}

func FinishCmdRenderUnicodeCodepointBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsCmdRenderUnicodeCodepoint(buf []byte, offset flatbuffers.UOffsetT) *CmdRenderUnicodeCodepoint {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &CmdRenderUnicodeCodepoint{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedCmdRenderUnicodeCodepointBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *CmdRenderUnicodeCodepoint) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *CmdRenderUnicodeCodepoint) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *CmdRenderUnicodeCodepoint) Imfont() uint64 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return rcv._tab.GetUint64(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *CmdRenderUnicodeCodepoint) MutateImfont(n uint64) bool {
	return rcv._tab.MutateUint64Slot(4, n)
}

func (rcv *CmdRenderUnicodeCodepoint) Size() float32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.GetFloat32(o + rcv._tab.Pos)
	}
	return 0.0
}

func (rcv *CmdRenderUnicodeCodepoint) MutateSize(n float32) bool {
	return rcv._tab.MutateFloat32Slot(6, n)
}

func (rcv *CmdRenderUnicodeCodepoint) Pos(obj *SingleVec2) *SingleVec2 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
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

func (rcv *CmdRenderUnicodeCodepoint) Col() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *CmdRenderUnicodeCodepoint) MutateCol(n uint32) bool {
	return rcv._tab.MutateUint32Slot(10, n)
}

func (rcv *CmdRenderUnicodeCodepoint) ClipRect(obj *SingleVec4) *SingleVec4 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(12))
	if o != 0 {
		x := o + rcv._tab.Pos
		if obj == nil {
			obj = new(SingleVec4)
		}
		obj.Init(rcv._tab.Bytes, x)
		return obj
	}
	return nil
}

func (rcv *CmdRenderUnicodeCodepoint) Codepoint() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(14))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *CmdRenderUnicodeCodepoint) MutateCodepoint(n uint32) bool {
	return rcv._tab.MutateUint32Slot(14, n)
}

func CmdRenderUnicodeCodepointStart(builder *flatbuffers.Builder) {
	builder.StartObject(6)
}
func CmdRenderUnicodeCodepointAddImfont(builder *flatbuffers.Builder, imfont uint64) {
	builder.PrependUint64Slot(0, imfont, 0)
}
func CmdRenderUnicodeCodepointAddSize(builder *flatbuffers.Builder, size float32) {
	builder.PrependFloat32Slot(1, size, 0.0)
}
func CmdRenderUnicodeCodepointAddPos(builder *flatbuffers.Builder, pos flatbuffers.UOffsetT) {
	builder.PrependStructSlot(2, flatbuffers.UOffsetT(pos), 0)
}
func CmdRenderUnicodeCodepointAddCol(builder *flatbuffers.Builder, col uint32) {
	builder.PrependUint32Slot(3, col, 0)
}
func CmdRenderUnicodeCodepointAddClipRect(builder *flatbuffers.Builder, clipRect flatbuffers.UOffsetT) {
	builder.PrependStructSlot(4, flatbuffers.UOffsetT(clipRect), 0)
}
func CmdRenderUnicodeCodepointAddCodepoint(builder *flatbuffers.Builder, codepoint uint32) {
	builder.PrependUint32Slot(5, codepoint, 0)
}
func CmdRenderUnicodeCodepointEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
