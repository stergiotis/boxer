//go:build fffi_debug

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
	"fmt"
	"runtime"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type Fffi2 struct {
	channel      Channel
	marshaller   *Marshaller
	unmarshaller *Unmarshaller
	lastCalls    [64]FuncProcId
	lastCallsPos int32
	stackBuf     []byte
}

func NewFffi2(channel Channel) (inst *Fffi2) {

	inst = &Fffi2{
		channel:      channel,
		marshaller:   channel.Marshaller(),
		unmarshaller: channel.Unmarshaller(),
		lastCalls:    [64]FuncProcId{},
		lastCallsPos: 0,
		stackBuf:     make([]byte, 4096),
	}

	return inst
}
func (inst *Fffi2) ReportLastCalls() {
	lastCalls := make([]uint32, 0, 64)
	for i := inst.lastCallsPos; i < 64; i++ {
		lastCalls = append(lastCalls, uint32(inst.lastCalls[i]))
	}
	for i := int32(0); i < inst.lastCallsPos; i++ {
		lastCalls = append(lastCalls, uint32(inst.lastCalls[i]))
	}
	log.Info().Interface("lastCalls", lastCalls).Str("lastCallSite", string(inst.stackBuf)).Msg("fffi2 last calls")
}
func (inst *Fffi2) recordCallId(id FuncProcId) {
	if id != FlushFuncProcId {
		inst.lastCalls[inst.lastCallsPos%64] = id
		inst.lastCallsPos++
		buf := inst.stackBuf
		n := runtime.Stack(buf, false)
		buf = buf[:n]
		log.Trace().Str("funcProcId", fmt.Sprintf("0x%08x", id)).Strs("stack2", strings.Split(string(buf), "\n")).Msg("fffi call")
	}
}

func (inst *Fffi2) AddFunctionId(id FuncProcId) {
	inst.recordCallId(id)
	AddUint32Arg(inst, id)
}

func (inst *Fffi2) AddProcedureId(id FuncProcId) {
	inst.recordCallId(id)
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

func (inst *Fffi2) Flush() {
	inst.AddProcedureId(FlushFuncProcId)
	inst.CallProcedure()
	inst.channel.Flush()
}

func (inst *Fffi2) CallProcedure() {
	// no-op
	inst.channel.Flush()
}
