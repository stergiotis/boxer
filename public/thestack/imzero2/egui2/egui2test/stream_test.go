package egui2test

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// message is one payload: the opcode, then data words.
func message(op c.FuncProcIdE, words ...uint32) (b []byte) {
	b = binary.LittleEndian.AppendUint32(nil, uint32(op))
	for _, w := range words {
		b = binary.LittleEndian.AppendUint32(b, w)
	}
	return
}

// A data word equal to an opcode is not counted as that opcode.
func TestRecordedStreamCountsMessagesNotWords(t *testing.T) {
	take, reset := InstallRecording()
	defer reset()
	f := typed.GetCurrentFffiVar()
	require.NoError(t, f.SendIntermediate(message(c.FuncProcIdButton, uint32(c.FuncProcIdLabel))))
	require.NoError(t, f.SendIntermediate(message(c.FuncProcIdLabel)))
	require.NoError(t, f.SendIntermediate(message(c.FuncProcIdButton)))
	stream := take()

	n, err := CountOpcode(stream, c.FuncProcIdLabel)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the Label word inside the Button's payload is data")
	ops, err := Opcodes(stream)
	require.NoError(t, err)
	assert.Equal(t, []c.FuncProcIdE{c.FuncProcIdButton, c.FuncProcIdLabel, c.FuncProcIdButton}, ops)
	msgs, err := FindOpcode(stream, c.FuncProcIdButton)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, uint32(c.FuncProcIdLabel), binary.LittleEndian.Uint32(msgs[0].Payload(stream)))

	assert.Empty(t, take(), "take starts a new recording")
}

func TestRecordedStreamRefusesATruncatedFrame(t *testing.T) {
	stream := binary.LittleEndian.AppendUint32(nil, 8)
	stream = append(stream, message(c.FuncProcIdButton)...)
	_, err := Opcodes(stream)
	require.Error(t, err)
	_, err = Opcodes([]byte{1, 0})
	require.Error(t, err)
}
