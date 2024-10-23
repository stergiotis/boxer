// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type CmdRenderParagraph struct {
	_tab flatbuffers.Table
}

func GetRootAsCmdRenderParagraph(buf []byte, offset flatbuffers.UOffsetT) *CmdRenderParagraph {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &CmdRenderParagraph{}
	x.Init(buf, n+offset)
	return x
}

func FinishCmdRenderParagraphBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsCmdRenderParagraph(buf []byte, offset flatbuffers.UOffsetT) *CmdRenderParagraph {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &CmdRenderParagraph{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedCmdRenderParagraphBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *CmdRenderParagraph) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *CmdRenderParagraph) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *CmdRenderParagraph) Imfont() uint64 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return rcv._tab.GetUint64(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *CmdRenderParagraph) MutateImfont(n uint64) bool {
	return rcv._tab.MutateUint64Slot(4, n)
}

func (rcv *CmdRenderParagraph) Size() float32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.GetFloat32(o + rcv._tab.Pos)
	}
	return 0.0
}

func (rcv *CmdRenderParagraph) MutateSize(n float32) bool {
	return rcv._tab.MutateFloat32Slot(6, n)
}

func (rcv *CmdRenderParagraph) Pos(obj *SingleVec2) *SingleVec2 {
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

func (rcv *CmdRenderParagraph) Col() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *CmdRenderParagraph) MutateCol(n uint32) bool {
	return rcv._tab.MutateUint32Slot(10, n)
}

func (rcv *CmdRenderParagraph) ClipRect(obj *SingleVec4) *SingleVec4 {
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

func (rcv *CmdRenderParagraph) Text() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(14))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *CmdRenderParagraph) WrapWidth() float32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(16))
	if o != 0 {
		return rcv._tab.GetFloat32(o + rcv._tab.Pos)
	}
	return 0.0
}

func (rcv *CmdRenderParagraph) MutateWrapWidth(n float32) bool {
	return rcv._tab.MutateFloat32Slot(16, n)
}

func (rcv *CmdRenderParagraph) LetterSpacing() float32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(18))
	if o != 0 {
		return rcv._tab.GetFloat32(o + rcv._tab.Pos)
	}
	return 0.0
}

func (rcv *CmdRenderParagraph) MutateLetterSpacing(n float32) bool {
	return rcv._tab.MutateFloat32Slot(18, n)
}

func (rcv *CmdRenderParagraph) TextAlign() TextAlignFlags {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(20))
	if o != 0 {
		return TextAlignFlags(rcv._tab.GetByte(o + rcv._tab.Pos))
	}
	return 0
}

func (rcv *CmdRenderParagraph) MutateTextAlign(n TextAlignFlags) bool {
	return rcv._tab.MutateByteSlot(20, byte(n))
}

func (rcv *CmdRenderParagraph) TextDirection() TextDirection {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(22))
	if o != 0 {
		return TextDirection(rcv._tab.GetByte(o + rcv._tab.Pos))
	}
	return 0
}

func (rcv *CmdRenderParagraph) MutateTextDirection(n TextDirection) bool {
	return rcv._tab.MutateByteSlot(22, byte(n))
}

func CmdRenderParagraphStart(builder *flatbuffers.Builder) {
	builder.StartObject(10)
}
func CmdRenderParagraphAddImfont(builder *flatbuffers.Builder, imfont uint64) {
	builder.PrependUint64Slot(0, imfont, 0)
}
func CmdRenderParagraphAddSize(builder *flatbuffers.Builder, size float32) {
	builder.PrependFloat32Slot(1, size, 0.0)
}
func CmdRenderParagraphAddPos(builder *flatbuffers.Builder, pos flatbuffers.UOffsetT) {
	builder.PrependStructSlot(2, flatbuffers.UOffsetT(pos), 0)
}
func CmdRenderParagraphAddCol(builder *flatbuffers.Builder, col uint32) {
	builder.PrependUint32Slot(3, col, 0)
}
func CmdRenderParagraphAddClipRect(builder *flatbuffers.Builder, clipRect flatbuffers.UOffsetT) {
	builder.PrependStructSlot(4, flatbuffers.UOffsetT(clipRect), 0)
}
func CmdRenderParagraphAddText(builder *flatbuffers.Builder, text flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(5, flatbuffers.UOffsetT(text), 0)
}
func CmdRenderParagraphAddWrapWidth(builder *flatbuffers.Builder, wrapWidth float32) {
	builder.PrependFloat32Slot(6, wrapWidth, 0.0)
}
func CmdRenderParagraphAddLetterSpacing(builder *flatbuffers.Builder, letterSpacing float32) {
	builder.PrependFloat32Slot(7, letterSpacing, 0.0)
}
func CmdRenderParagraphAddTextAlign(builder *flatbuffers.Builder, textAlign TextAlignFlags) {
	builder.PrependByteSlot(8, byte(textAlign), 0)
}
func CmdRenderParagraphAddTextDirection(builder *flatbuffers.Builder, textDirection TextDirection) {
	builder.PrependByteSlot(9, byte(textDirection), 0)
}
func CmdRenderParagraphEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}