package egui2test

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

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

// A trace sees the messages a deferred block captured, which a recording
// holds only inside their owner's payload.
func TestTraceSeesMessagesInsideDeferredBlocks(t *testing.T) {
	take, reset := InstallTracing()
	defer reset()
	f := typed.GetCurrentFffiVar()
	var outer, inner bytes.Buffer
	require.NoError(t, f.SendIntermediate(message(c.FuncProcIdLabel, 7)))
	f.BeginCapture(&outer, binary.LittleEndian)
	require.NoError(t, f.SendIntermediate(message(c.FuncProcIdButton)))
	f.BeginCapture(&inner, binary.LittleEndian)
	require.NoError(t, f.SendIntermediate(message(c.FuncProcIdLabel)))
	f.EndCapture()
	f.EndCapture()
	require.NoError(t, f.SendIntermediate(message(c.FuncProcIdButton)))

	sent := take()
	require.Len(t, sent, 4)
	ops, depths := make([]c.FuncProcIdE, 0, 4), make([]int, 0, 4)
	for _, m := range sent {
		ops, depths = append(ops, m.Opcode), append(depths, m.Depth)
	}
	assert.Equal(t, []c.FuncProcIdE{c.FuncProcIdLabel, c.FuncProcIdButton, c.FuncProcIdLabel, c.FuncProcIdButton}, ops)
	assert.Equal(t, []int{0, 1, 2, 0}, depths)
	assert.Equal(t, 2, CountSent(sent, c.FuncProcIdLabel))
	assert.Equal(t, uint32(7), binary.LittleEndian.Uint32(sent[0].Payload))
	assert.Empty(t, sent[1].Payload)
	assert.Empty(t, take(), "take starts a new trace")
}

// Eventually asks on the calling goroutine, which Install bound the frame
// to, so its condition may send.
func TestEventuallyPollsOnTheFrameGoroutine(t *testing.T) {
	defer Install()()
	f := typed.GetCurrentFffiVar()
	n := 0
	assert.True(t, Eventually(func() bool {
		require.NoError(t, f.SendIntermediate(message(c.FuncProcIdLabel)))
		n++
		return n == 3
	}, 5*time.Second, time.Millisecond))
	asked := 0
	assert.False(t, Eventually(func() bool { asked++; return false }, 0, time.Millisecond))
	assert.Equal(t, 1, asked, "a wait of zero asks once")
}
