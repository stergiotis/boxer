package runtime

import (
	"bytes"
	"encoding/binary"
	"iter"
)

func NewFffi2[U UnmarshallReaderI](channel ChannelI[U]) *Fffi2[U] {
	return &Fffi2[U]{
		channel: channel,
	}
}

//func (inst *Fffi2[U]) readError() (err error) {
//	s := GetStringRetrMostLikelyEmpty[*Unmarshaller, string](inst.unmarshaller)
//	if s != "" {
//		err = eh.New(s)
//	}
//	return
//}

func (inst *Fffi2[U]) SyncRetained(id uint64, buf []byte) (err error) {
	//return inst.channel.SyncRetained(id, buf)
	return inst.SendIntermediate(buf)
}
func (inst *Fffi2[U]) SendIntermediate(buf []byte) (err error) {
	if n := len(inst.captureStack); n > 0 {
		// During deferred block capture: write framed message to the innermost
		// capture buffer. Wire format matches what Rust's begin_consume_message
		// expects: [u32 msg_len][payload].
		top := &inst.captureStack[n-1]
		_ = binary.Write(top.buf, top.end, uint32(len(buf)))
		_, _ = top.buf.Write(buf)
		return
	}
	inst.channel.SendSingleUseMsg(buf)
	return
}

// IsCapturing reports whether the current goroutine is inside a
// deferred-block capture scope — i.e. SendIntermediate is buffering
// into a capture frame rather than flushing to the IPC pipe. Read by
// Fetcher.invoke as a runtime guard: fetchers must not run inside
// captures (the fetch request would be buffered while the response
// read blocks on the pipe — mutual deadlock). See
// doc/skills/imzero2-fetchers/SKILLS.md.
func (inst *Fffi2[U]) IsCapturing() (capturing bool) {
	capturing = len(inst.captureStack) > 0
	return
}

// BeginCapture redirects SendIntermediate to write into buf instead of the
// IPC pipe. Nested calls are supported — each Begin pushes a new capture
// frame; SendIntermediate writes to the innermost (top of stack), so an
// etable inside a dockArea tab body correctly nests its cell bodies
// inside the tab body bytes.
func (inst *Fffi2[U]) BeginCapture(buf *bytes.Buffer, endianness binary.ByteOrder) {
	inst.captureStack = append(inst.captureStack, captureFrame{buf: buf, end: endianness})
}

// EndCapture pops the innermost capture scope. After the outermost pop the
// stack is empty and SendIntermediate resumes sending to the pipe.
func (inst *Fffi2[U]) EndCapture() {
	n := len(inst.captureStack)
	if n == 0 {
		panic("EndCapture without matching BeginCapture")
	}
	inst.captureStack = inst.captureStack[:n-1]
}

// AppendRawToCapture writes raw bytes directly to the innermost capture
// buffer without adding a frame header. Use this only to re-emit bytes
// that were already captured with framing in a detached buffer — the
// DockArea iter wrapper uses it to flush buffered tab bodies into its
// deferred block scope at Send time.
func (inst *Fffi2[U]) AppendRawToCapture(raw []byte) {
	n := len(inst.captureStack)
	if n == 0 {
		panic("AppendRawToCapture requires an active capture scope")
	}
	top := &inst.captureStack[n-1]
	_, _ = top.buf.Write(raw)
}
func (inst *Fffi2[U]) ReceiveMsg() iter.Seq[U] {
	return inst.channel.ReceiveMsg()
}

func (inst *Fffi2[U]) CallFunctionMayThrow() (err error) {
	inst.channel.FlushMessages()
	//err = inst.readError()
	return
}
func (inst *Fffi2[U]) CallFunctionNoThrow() {
	// no-op
}

func (inst *Fffi2[U]) PipelineProcedureNoThrow() {
	// no-op
}
