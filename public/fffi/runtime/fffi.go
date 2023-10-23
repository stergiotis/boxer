package runtime

/*
 Idea:
- Batch all consecutive procedure calls
- One round-trip per function call
- Frame based
. Batches of procedures are diffed against last frame???
. Use arena allocator
*/

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"io"
	"math"
)

type FuncProcId uint32

const FlushFuncProcId = FuncProcId(math.MaxUint32)

type Fffi2 struct {
	bin          ByteOrder
	channel      Channel
	marshaller   *Marshaller
	unmarshaller *Unmarshaller
	signalOut    io.ByteWriter
	signalIn     io.ByteReader
}

func NewFffi2(channel Channel) *Fffi2 {
	return &Fffi2{
		channel:      channel,
		marshaller:   channel.Marshaller(),
		unmarshaller: channel.Unmarshaller(),
	}
}

func (inst *Fffi2) AddFunctionId(id FuncProcId) {
	//log.Trace().Str("id", fmt.Sprintf("%08x", id)).Msg("calling function")
	AddUint32Arg(inst, id)
}
func (inst *Fffi2) AddProcedureId(id FuncProcId) {
	//log.Trace().Str("id", fmt.Sprintf("%08x", id)).Msg("calling procedure")
	AddUint32Arg(inst, id)
}
func (inst *Fffi2) readError() (err error) {
	s := GetStringRetr[string](inst)
	if s != "" {
		err = eh.New(s)
	}
	return
}
func (inst *Fffi2) CallFunction() (err error) {
	err = inst.channel.CallFunction()
	if err != nil {
		return err
	}
	err = inst.readError()
	return
}
func (inst *Fffi2) Flush() {
	inst.AddProcedureId(FlushFuncProcId)
	inst.CallProcedure()
	inst.channel.Flush()
}
func (inst *Fffi2) CallProcedure() {
	// no-op
	inst.channel.Flush()
}
