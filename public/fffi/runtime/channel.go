package runtime

import (
	"bufio"
	"encoding/binary"
)

type ByteOrder interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

type Channel interface {
	Marshaller() *Marshaller
	Unmarshaller() *Unmarshaller
	CallFunction() (err error)
	Flush()
}

type InlineIoChannel struct {
	in             *bufio.Reader
	out            *bufio.Writer
	marshall       *Marshaller
	unmarshall     *Unmarshaller
	bin            binary.ByteOrder
	allocateBuffer func(l uint32) []byte
	errHandler     func(err error)
}

func (inst *InlineIoChannel) Marshaller() *Marshaller {
	return inst.marshall
}

func (inst *InlineIoChannel) Unmarshaller() *Unmarshaller {
	return inst.unmarshall
}

var DefaultErrorHandler = func(err error) {}
var DefaultAllocater = func(l uint32) []byte { return make([]byte, int(l), int(l)) }

func NewInlineChannel(in *bufio.Reader, out *bufio.Writer, bin binary.ByteOrder, errHandler func(err error), allocateBuffer func(l uint32) []byte) (inst *InlineIoChannel) {
	if allocateBuffer == nil {
		allocateBuffer = DefaultAllocater
	}
	if errHandler == nil {
		errHandler = DefaultErrorHandler
	}
	inst = &InlineIoChannel{
		in:             nil,
		out:            nil,
		marshall:       nil,
		unmarshall:     nil,
		bin:            bin,
		allocateBuffer: allocateBuffer,
		errHandler:     errHandler,
	}
	inst.SetInOut(in, out)
	return
}
func (inst *InlineIoChannel) SetInOut(in *bufio.Reader, out *bufio.Writer) {
	inst.in = in
	inst.out = out
	inst.marshall = NewMarshaller(out, inst.bin, inst.errHandler)
	inst.unmarshall = NewUnmarshaller(in, inst.bin, inst.errHandler, inst.allocateBuffer)
}

func (inst *InlineIoChannel) CallFunction() (err error) {
	inst.Flush()
	return
}
func (inst *InlineIoChannel) Flush() {
	err := inst.out.Flush()
	if err != nil {
		inst.errHandler(err)
	}
}

var _ Channel = (*InlineIoChannel)(nil)
