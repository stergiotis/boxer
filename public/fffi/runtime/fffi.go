//go:build !fffi_debug

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
)

type Fffi2 struct {
	channel      Channel
	marshaller   *Marshaller
	unmarshaller *Unmarshaller
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
	s := GetStringRetrMostLikelyEmpty[string](inst)
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
func (inst *Fffi2) CallFunctionNoThrow() {
	inst.channel.CallFunctionNoThrow()
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
func (inst *Fffi2) CallProcedureNoThrow() {
	// no-op
	inst.channel.Flush()
}
