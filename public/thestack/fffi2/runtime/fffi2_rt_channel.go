package runtime

import (
	"bufio"
	"encoding/binary"
	"iter"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/ea"
)

type InlineIoChannel[U UnmarshallReaderI] struct {
	readOffset     int64
	sz             *ea.SizeMeasureWriter
	in             *bufio.Reader
	out            *bufio.Writer
	marshall       *Marshaller
	unmarshall     U
	bin            binary.ByteOrder
	allocateBuffer func(l uint32) []byte
	errHandler     func(err error)
}

func (inst *InlineIoChannel[U]) ReceiveMsg() iter.Seq[U] {
	return func(yield func(U) bool) {
		//inst.readOffset += int64(inst.sz.Size)
		//inst.sz.Reset()
		yield(inst.unmarshall)
		//delta := inst.sz.Size
		//e := log.Info().Int64("offset", inst.readOffset).
		//	Uint64("deltaBytes", delta)
		//if delta%4 == 0 {
		//	e.Uint64("delta32BitUnits", delta/4)
		//}
		//if delta%8 == 0 {
		//	e.Uint64("delta64BitUnits", delta/8)
		//}
		//e.Msg("received message")
	}
}

var DefaultErrorHandler = func(err error) {
	log.Error().Err(err).Msg("fffi2 channel error")
}

var DefaultAllocator = func(l uint32) []byte { return make([]byte, int(l), int(l)) }

func NewInlineIoChannel[U UnmarshallReaderI](unmarshaller U, in *bufio.Reader, out *bufio.Writer, bin binary.ByteOrder, errHandler func(err error), allocateBuffer func(l uint32) []byte) (inst *InlineIoChannel[U]) {
	if allocateBuffer == nil {
		allocateBuffer = DefaultAllocator
	}
	if errHandler == nil {
		errHandler = DefaultErrorHandler
	}
	inst = &InlineIoChannel[U]{
		readOffset:     0,
		sz:             &ea.SizeMeasureWriter{Size: 0},
		in:             in,
		out:            out,
		marshall:       nil,
		unmarshall:     unmarshaller,
		bin:            bin,
		allocateBuffer: allocateBuffer,
		errHandler:     errHandler,
	}
	inst.SetInOut(in, out)
	return
}

func (inst *InlineIoChannel[U]) SetInOut(in *bufio.Reader, out *bufio.Writer) {
	inst.in = in
	inst.out = out
	inst.marshall = NewMarshaller(out, inst.bin, inst.errHandler)
	inst.unmarshall.SetInput(in)
	// Wire the unmarshaller's error chain to the channel's so read-side
	// failures (EOF, ErrClosed during shutdown, decode errors) flow through
	// the same handler as marshaller failures. Without this the unmarshaller
	// keeps its construction-time default — typically a bare log.Error that
	// floods stderr with a per-Read entry once the pipe breaks.
	inst.unmarshall.SetErrorHandler(inst.errHandler)
	//inst.unmarshall.SetInput(io.TeeReader(in, inst.sz))
}

func (inst *InlineIoChannel[U]) FlushMessages() {
	err := inst.out.Flush()
	if err != nil {
		inst.errHandler(err)
	}
}
func (inst *InlineIoChannel[U]) SyncMultiUseMsg(id uint64, msg []byte) {
	inst.SendSingleUseMsg(msg)
}
func (inst *InlineIoChannel[U]) SendSingleUseMsg(msg []byte) {
	inst.marshall.WriteUint32(uint32(len(msg)))
	inst.marshall.WriteVerbatim(msg)
	inst.FlushMessages()
}

func (inst *InlineIoChannel[U]) Marshaller() *Marshaller {
	return inst.marshall
}

var _ ChannelI[UnmarshallReaderI] = (*InlineIoChannel[UnmarshallReaderI])(nil)
