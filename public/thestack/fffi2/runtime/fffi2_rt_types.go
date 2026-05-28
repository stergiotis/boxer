package runtime

import (
	"bytes"
	"encoding/binary"
	"io"
	"iter"
)

type FuncProcId uint32

type ByteOrderI interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

type ChannelI[U UnmarshallReaderI] interface {
	SyncMultiUseMsg(id uint64, buf []byte)
	SendSingleUseMsg(buf []byte)
	ReceiveMsg() iter.Seq[U]
	FlushMessages()
}

type captureFrame struct {
	buf *bytes.Buffer
	end binary.ByteOrder
}

type Fffi2[U UnmarshallReaderI] struct {
	channel ChannelI[U]
	// Stack of active capture scopes. Pushed by BeginCapture, popped by
	// EndCapture. SendIntermediate writes to the top of the stack, or to
	// the pipe when the stack is empty. Supports nesting deferred-block
	// scopes (e.g. an etable inside a dockArea tab body).
	captureStack []captureFrame
}

type MarshallWriterI interface {
	WriteUint8(v uint8)
	WriteBool(v bool)
	WriteUint16(v uint16)
	WriteUint32(v uint32)
	WriteUint64(v uint64)
	WriteInt8(v int8)
	WriteInt16(v int16)
	WriteInt32(v int32)
	WriteInt64(v int64)
	WriteFloat32(v float32)
	WriteFloat64(v float64)
	WriteComplex64(v complex64)
	WriteComplex128(v complex128)
	WriteString(v string)
	WriteBytes(v []byte)
	WriteSliceLength(l int)
	WriteNilSlice()
}
type UnmarshallReaderI interface {
	SetInput(r io.Reader)
	SetEndianness(endi binary.ByteOrder)
	SetErrorHandler(f func(err error))
	SetAllocateBufferFunc(f func(l uint32) []byte)

	ReadUInt8() (v uint8)
	ReadUInt16() (v uint16)
	ReadUInt32MostLikelyZero() (v uint32)
	ReadUInt32() (v uint32)
	ReadUInt64() (v uint64)
	ReadInt8() (v int8)
	ReadInt16() (v int16)
	ReadInt32() (v int32)
	ReadInt64() (v int64)
	ReadFloat32() (v float32)
	ReadFloat64() (v float64)
	ReadComplex64() (v complex64)
	ReadComplex128() (v complex128)
	ReadUintptr() (v uintptr)
	ReadString() (v string)
	ReadStringMostLikelyEmpty() (v string)
	ReadBytes() (v []byte)
	ReadBool() (v bool)
	ReadSliceLength() (l int, isNil bool)
}
