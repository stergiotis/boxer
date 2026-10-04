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
	inst.checkOwner()
	inst.msgs++
	if n := len(inst.captureStack); n > 0 {
		// During deferred block capture: write framed message to the innermost
		// capture buffer. Wire format matches what Rust's begin_consume_message
		// expects: [u32 msg_len][payload].
		top := &inst.captureStack[n-1]
		_ = binary.Write(top.buf, top.end, uint32(len(buf)))
		_, _ = top.buf.Write(buf)
		return
	}
	if inst.recordingOn {
		inst.recording = binary.LittleEndian.AppendUint32(inst.recording, uint32(len(buf)))
		inst.recording = append(inst.recording, buf...)
	}
	inst.channel.SendSingleUseMsg(buf)
	return
}

// BeginRecording starts keeping a copy of every message sent to the pipe,
// framed as the peer reads it ([u32 len][payload], little-endian, as
// SendSingleUseMsg writes it). Messages taken by a deferred-block capture
// are recorded when the block reaches the pipe inside its owner's message.
// A capture replays spans of the recording (ADR-0281 §SD4).
func (inst *Fffi2[U]) BeginRecording() {
	inst.checkOwner()
	inst.recording = inst.recording[:0]
	inst.recordingOn = true
}

// RecordingPosition is the length of the open recording: a message
// boundary, since messages are recorded whole. It is -1 while no recording
// is open, or inside a deferred-block capture, where the next message does
// not reach the pipe on its own.
func (inst *Fffi2[U]) RecordingPosition() (pos int) {
	if !inst.recordingOn || len(inst.captureStack) > 0 {
		return -1
	}
	return len(inst.recording)
}

// EndRecording closes the recording and returns it. The slice is the
// recorder's own buffer: it is valid until the next BeginRecording.
func (inst *Fffi2[U]) EndRecording() (recording []byte) {
	inst.checkOwner()
	inst.recordingOn = false
	return inst.recording
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
	inst.checkOwner()
	inst.captureStack = append(inst.captureStack, captureFrame{buf: buf, end: endianness})
}

// EndCapture pops the innermost capture scope. After the outermost pop the
// stack is empty and SendIntermediate resumes sending to the pipe.
func (inst *Fffi2[U]) EndCapture() {
	inst.checkOwner()
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
	inst.checkOwner()
	n := len(inst.captureStack)
	if n == 0 {
		panic("AppendRawToCapture requires an active capture scope")
	}
	top := &inst.captureStack[n-1]
	_, _ = top.buf.Write(raw)
}
func (inst *Fffi2[U]) ReceiveMsg() iter.Seq[U] {
	inst.checkOwner()
	return inst.channel.ReceiveMsg()
}

func (inst *Fffi2[U]) CallFunctionMayThrow() (err error) {
	inst.checkOwner()
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
