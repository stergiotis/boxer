package bindings

import (
	"fmt"
	"iter"

	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
)

const FuncProcIdOffset FuncProcIdE = 0

type Fetcher struct {
}

func NewFetcher() (inst *Fetcher) {
	return &Fetcher{}
}

// invoke writes a Fetch* opcode and is followed by a synchronous read
// of the response. For the read to terminate, the opcode must reach
// Rust — which means it must NOT be buffered into a deferred-block
// capture frame. The runtime guard panics with a clear, debuggable
// message when a caller breaks the "fetchers run only from
// StateManager.Sync" rule (see
// doc/skills/imzero2-fetchers/SKILLS.md). Negligible cost — one bool
// check per fetch; fetches happen O(10) times per frame.
func (inst *Fetcher) invoke(f FuncProcIdE) {
	if cap_ := typed.GetCurrentFffiCapture(); cap_ != nil && cap_.IsCapturing() {
		panic(fmt.Sprintf(
			"fffi2: fetcher opcode %d invoked inside a deferred-block capture; "+
				"this deadlocks (request buffered, response read blocks on empty pipe). "+
				"Fetchers must run only from StateManager.Sync — see "+
				"doc/skills/imzero2-fetchers/SKILLS.md.", f))
	}
	b := typed.NewRetainedFffiBuilder()
	b.WriteUint32(uint32(f))
	b.SendIntermediate()
}
func (inst *Fetcher) readU32h() (r []uint32) {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.GetUint32SliceRetr[*runtime.Unmarshaller, uint32](u)
	}
	return
}
func (inst *Fetcher) readU64h() (r []uint64) {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.GetUint64SliceRetr[*runtime.Unmarshaller, uint64](u)
	}
	return
}
func (inst *Fetcher) readI64h() (r []int64) {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.GetInt64SliceRetr[*runtime.Unmarshaller, int64](u)
	}
	return
}
func (inst *Fetcher) readF64h() (r []float64) {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.GetFloat64SliceRetr[*runtime.Unmarshaller, float64](u)
	}
	return
}
func (inst *Fetcher) iterateU64h() iter.Seq[uint64] {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.IterateUint64SliceRetr[*runtime.Unmarshaller, uint64](u)
	}
	return nil
}
func (inst *Fetcher) iterateI64h() iter.Seq[int64] {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.IterateInt64SliceRetr[*runtime.Unmarshaller, int64](u)
	}
	return nil
}
func (inst *Fetcher) iterateSh() iter.Seq[string] {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.IterateStringSliceRetr[*runtime.Unmarshaller, string](u)
	}
	return nil
}
func (inst *Fetcher) iterateU32h() iter.Seq[uint32] {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.IterateUint32SliceRetr[*runtime.Unmarshaller, uint32](u)
	}
	return nil
}
func (inst *Fetcher) iterateF64h() iter.Seq[float64] {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.IterateFloat64SliceRetr[*runtime.Unmarshaller, float64](u)
	}
	return nil
}
func (inst *Fetcher) readF32h() (r []float32) {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.GetFloat32SliceRetr[*runtime.Unmarshaller, float32](u)
	}
	return
}
func (inst *Fetcher) iterateF32h() iter.Seq[float32] {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.IterateFloat32SliceRetr[*runtime.Unmarshaller, float32](u)
	}
	return nil
}
func (inst *Fetcher) readF32() float32 {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.GetFloat32Retr[*runtime.Unmarshaller, float32](u)
	}
	return 0
}
func (inst *Fetcher) readF64() float64 {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.GetFloat64Retr[*runtime.Unmarshaller, float64](u)
	}
	return 0
}
func (inst *Fetcher) readU64() uint64 {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.GetUint64Retr[*runtime.Unmarshaller, uint64](u)
	}
	return 0
}
func (inst *Fetcher) readB() bool {
	for u := range typed.GetCurrentFffiVar().ReceiveMsg() {
		return runtime.GetBoolRetr[*runtime.Unmarshaller, bool](u)
	}
	return false
}
