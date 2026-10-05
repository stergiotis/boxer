package runtime

import (
	"bytes"
	"encoding/binary"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pipeChannel keeps what reaches the pipe, framed as InlineIoChannel frames it.
type pipeChannel struct{ sent bytes.Buffer }

func (inst *pipeChannel) SyncMultiUseMsg(id uint64, buf []byte) { inst.SendSingleUseMsg(buf) }
func (inst *pipeChannel) SendSingleUseMsg(buf []byte) {
	_ = binary.Write(&inst.sent, binary.LittleEndian, uint32(len(buf)))
	inst.sent.Write(buf)
}
func (inst *pipeChannel) ReceiveMsg() iter.Seq[*Unmarshaller] { return nil }
func (inst *pipeChannel) FlushMessages()                      {}

func TestRecordingKeepsWhatReachesThePipeFramedAsSent(t *testing.T) {
	ch := &pipeChannel{}
	f := NewFffi2[*Unmarshaller](ch)
	require.Equal(t, -1, f.RecordingPosition())

	require.NoError(t, f.SendIntermediate([]byte("before")))
	f.BeginRecording()
	start := len(ch.sent.Bytes())
	require.Equal(t, 0, f.RecordingPosition())
	require.NoError(t, f.SendIntermediate([]byte("a")))
	afterA := f.RecordingPosition()
	assert.Equal(t, 5, afterA)

	// A message inside a deferred-block capture does not reach the pipe on
	// its own; there is no boundary to mark until the scope ends.
	var block bytes.Buffer
	f.BeginCapture(&block, binary.LittleEndian)
	assert.Equal(t, -1, f.RecordingPosition())
	require.NoError(t, f.SendIntermediate([]byte("cell")))
	f.EndCapture()
	require.NoError(t, f.SendIntermediate(block.Bytes()))
	rec := f.EndRecording()

	assert.Equal(t, ch.sent.Bytes()[start:], rec)
	assert.Equal(t, -1, f.RecordingPosition())
	require.NoError(t, f.SendIntermediate([]byte("after")))
	assert.Len(t, rec, afterA+4+len(block.Bytes()))
}
