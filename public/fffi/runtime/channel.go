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
	in         *bufio.Reader
	out        *bufio.Writer
	marshall   *Marshaller
	unmarshall *Unmarshaller
	errHandler func(err error)
}

func (inst *InlineIoChannel) Marshaller() *Marshaller {
	return inst.marshall
}

func (inst *InlineIoChannel) Unmarshaller() *Unmarshaller {
	return inst.unmarshall
}

var DefaultErrorHandler = func(err error) {}
var DefaultAllocater = func(l uint32) []byte { return make([]byte, int(l), int(l)) }

func NewInlineChannel(out *bufio.Writer, in *bufio.Reader, bin binary.ByteOrder, errHandler func(err error), allocateBuffer func(l uint32) []byte) *InlineIoChannel {
	if allocateBuffer == nil {
		allocateBuffer = DefaultAllocater
	}
	marshaller := NewMarshaller(out, bin, errHandler)
	unmarshaller := NewUnmarshaller(in, bin, errHandler, allocateBuffer)
	if errHandler == nil {
		errHandler = DefaultErrorHandler
	}
	return &InlineIoChannel{
		in:         in,
		out:        out,
		marshall:   marshaller,
		unmarshall: unmarshaller,
		errHandler: errHandler,
	}
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
