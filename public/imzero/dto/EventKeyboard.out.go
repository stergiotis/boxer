// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package dto

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type EventKeyboard struct {
	_tab flatbuffers.Table
}

func GetRootAsEventKeyboard(buf []byte, offset flatbuffers.UOffsetT) *EventKeyboard {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &EventKeyboard{}
	x.Init(buf, n+offset)
	return x
}

func FinishEventKeyboardBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsEventKeyboard(buf []byte, offset flatbuffers.UOffsetT) *EventKeyboard {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &EventKeyboard{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedEventKeyboardBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *EventKeyboard) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *EventKeyboard) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *EventKeyboard) Modifiers() KeyModifiers {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return KeyModifiers(rcv._tab.GetUint16(o + rcv._tab.Pos))
	}
	return 0
}

func (rcv *EventKeyboard) MutateModifiers(n KeyModifiers) bool {
	return rcv._tab.MutateUint16Slot(4, uint16(n))
}

func (rcv *EventKeyboard) Code() KeyCode {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return KeyCode(rcv._tab.GetUint32(o + rcv._tab.Pos))
	}
	return 0
}

func (rcv *EventKeyboard) MutateCode(n KeyCode) bool {
	return rcv._tab.MutateUint32Slot(6, uint32(n))
}

func (rcv *EventKeyboard) IsDown() bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
	if o != 0 {
		return rcv._tab.GetBool(o + rcv._tab.Pos)
	}
	return false
}

func (rcv *EventKeyboard) MutateIsDown(n bool) bool {
	return rcv._tab.MutateBoolSlot(8, n)
}

func (rcv *EventKeyboard) NativeSym() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *EventKeyboard) MutateNativeSym(n uint32) bool {
	return rcv._tab.MutateUint32Slot(10, n)
}

func (rcv *EventKeyboard) Scancode() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(12))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *EventKeyboard) MutateScancode(n uint32) bool {
	return rcv._tab.MutateUint32Slot(12, n)
}

func EventKeyboardStart(builder *flatbuffers.Builder) {
	builder.StartObject(5)
}
func EventKeyboardAddModifiers(builder *flatbuffers.Builder, modifiers KeyModifiers) {
	builder.PrependUint16Slot(0, uint16(modifiers), 0)
}
func EventKeyboardAddCode(builder *flatbuffers.Builder, code KeyCode) {
	builder.PrependUint32Slot(1, uint32(code), 0)
}
func EventKeyboardAddIsDown(builder *flatbuffers.Builder, isDown bool) {
	builder.PrependBoolSlot(2, isDown, false)
}
func EventKeyboardAddNativeSym(builder *flatbuffers.Builder, nativeSym uint32) {
	builder.PrependUint32Slot(3, nativeSym, 0)
}
func EventKeyboardAddScancode(builder *flatbuffers.Builder, scancode uint32) {
	builder.PrependUint32Slot(4, scancode, 0)
}
func EventKeyboardEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
