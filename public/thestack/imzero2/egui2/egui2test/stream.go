package egui2test

import (
	"encoding/binary"
	"iter"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// A recorded stream is what runtime.Fffi2's BeginRecording keeps: each
// message framed as [u32 length][payload], little-endian, and each payload
// starting with the opcode (FuncProcIdE) of the call it carries. An opcode
// is the position of its call in the sorted IDL, so it moves whenever the
// IDL gains a call, and a payload word can equal an opcode by chance: a
// test that counts an opcode by matching raw 4-byte words in the stream
// breaks on the first and miscounts on the second. The functions below walk
// the frames and compare the bindings' constants, so a test never matches
// bytes.
//
// Only the message's own opcode is read. Calls spliced into a message (a
// retained builder's argument, a deferred block's captured messages) are
// part of its payload and are not visited: where they sit in the payload
// depends on the owner's other arguments. A test that asks about the calls
// inside deferred blocks — a table's cells, a tab's body — uses
// InstallTracing, which sees each message as it is sent.

// Message is one message of a recorded stream.
type Message struct {
	// Opcode is the call the message carries.
	Opcode c.FuncProcIdE
	// Offset and Len are the payload's extent in the recording, opcode
	// included and length prefix excluded.
	Offset int
	Len    int
}

// Payload is the message's bytes after the opcode.
func (inst Message) Payload(recording []byte) (b []byte) {
	return recording[inst.Offset+4 : inst.Offset+inst.Len]
}

// Messages iterates the messages of a recorded stream in order. A frame
// that runs past the end of the recording, or a payload too short to hold
// an opcode, ends the iteration with an error.
func Messages(recording []byte) iter.Seq2[Message, error] {
	return func(yield func(Message, error) bool) {
		pos := 0
		for pos < len(recording) {
			if len(recording)-pos < 4 {
				yield(Message{}, eb.Build().Int("offset", pos).Errorf("recorded stream ends inside a length prefix"))
				return
			}
			n := int(binary.LittleEndian.Uint32(recording[pos:]))
			pos += 4
			if n < 4 || len(recording)-pos < n {
				yield(Message{}, eb.Build().Int("offset", pos).Int("length", n).Errorf("recorded message is truncated or holds no opcode"))
				return
			}
			m := Message{Opcode: c.FuncProcIdE(binary.LittleEndian.Uint32(recording[pos:])), Offset: pos, Len: n}
			pos += n
			if !yield(m, nil) {
				return
			}
		}
	}
}

// FindOpcode returns the messages carrying op, in order.
func FindOpcode(recording []byte, op c.FuncProcIdE) (msgs []Message, err error) {
	for m, merr := range Messages(recording) {
		if merr != nil {
			err = merr
			return
		}
		if m.Opcode == op {
			msgs = append(msgs, m)
		}
	}
	return
}

// CountOpcode is the number of messages carrying op.
func CountOpcode(recording []byte, op c.FuncProcIdE) (n int, err error) {
	msgs, err := FindOpcode(recording, op)
	n = len(msgs)
	return
}

// Opcodes is the opcode of every message, in order.
func Opcodes(recording []byte) (ops []c.FuncProcIdE, err error) {
	for m, merr := range Messages(recording) {
		if merr != nil {
			err = merr
			return
		}
		ops = append(ops, m.Opcode)
	}
	return
}

// InstallRecording is Install with a recording of everything sent: take
// returns a copy of the stream recorded since the last take (or the
// install) and starts a new recording. Decode it with Messages,
// FindOpcode or CountOpcode.
func InstallRecording() (take func() []byte, reset func()) {
	f := runtime.NewFffi2[*runtime.Unmarshaller](discardChannel{})
	typed.SetCurrentFffiVar(f)
	f.BeginRecording()
	sm := c.CurrentApplicationState.StateManager
	sm.ScriptReset()
	take = func() (stream []byte) {
		stream = slices.Clone(f.EndRecording())
		f.BeginRecording()
		return
	}
	return take, sm.ScriptReset
}

// Sent is one message as the bindings sent it.
type Sent struct {
	// Opcode is the call the message carries.
	Opcode c.FuncProcIdE
	// Depth is the number of deferred-block captures open around the
	// call: 0 for a message that reaches the host on its own, n for one
	// taken into a block n captures deep.
	Depth int
	// Payload is a copy of the message's bytes after the opcode.
	Payload []byte
}

// InstallTracing is Install with a trace of every message in the order the
// calls were made, the ones a deferred block captured included: a block's
// messages come before the message of the call that owns the block, since
// the owner is sent once its blocks are closed. take returns the messages
// sent since the last take (or the install). A call written into another's
// payload without being sent (a retained builder's argument) is not a
// message and is not seen.
func InstallTracing() (take func() []Sent, reset func()) {
	f := runtime.NewFffi2[*runtime.Unmarshaller](discardChannel{})
	typed.SetCurrentFffiVar(f)
	var sent []Sent
	f.SetSendTrace(func(payload []byte, depth int) {
		if len(payload) < 4 {
			return
		}
		sent = append(sent, Sent{
			Opcode:  c.FuncProcIdE(binary.LittleEndian.Uint32(payload)),
			Depth:   depth,
			Payload: slices.Clone(payload[4:]),
		})
	})
	sm := c.CurrentApplicationState.StateManager
	sm.ScriptReset()
	take = func() (out []Sent) {
		out, sent = sent, nil
		return
	}
	return take, sm.ScriptReset
}

// CountSent is the number of traced messages carrying op, at any depth.
func CountSent(sent []Sent, op c.FuncProcIdE) (n int) {
	for _, m := range sent {
		if m.Opcode == op {
			n++
		}
	}
	return
}
