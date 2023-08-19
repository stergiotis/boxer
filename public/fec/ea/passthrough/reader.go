package passthrough

import (
	"github.com/stergiotis/boxer/public/ea"
	ea3 "github.com/stergiotis/boxer/public/fec/ea"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type PassthroughReader struct {
	baseReader *ea3.BaseReader
}

func (inst *PassthroughReader) ReadByte() (b byte, err error) {
	return inst.baseReader.PeekByte()
}

func (inst *PassthroughReader) Read(p []byte) (n int, err error) {
	var b []byte
	b, err = inst.baseReader.Bytes()
	if err != nil {
		err = eh.Errorf("unable to read peeked bytes: %w", err)
		return
	}
	n = copy(p, b)
	_, err = inst.baseReader.DiscardPeeking(n)
	if err != nil {
		err = eh.Errorf("unable to discard after read: %w", err)
		return
	}
	return
}

func (inst *PassthroughReader) Discard(n int) (n2 int, err error) {
	n2, err = inst.baseReader.DiscardPeeking(n)
	if err != nil {
		err = eh.Errorf("unable to discard bytes: %w", err)
		return
	}
	return
}

func (inst *PassthroughReader) BytesRead() int {
	return inst.baseReader.BytesRead()
}

func (inst *PassthroughReader) BytesPeeked() int {
	return inst.baseReader.BytesPeeked()
}

func (inst *PassthroughReader) MessageAccepted() (err error) {
	return inst.baseReader.MessageAccepted()
}
func (inst *PassthroughReader) MessageRejected(reason error) (err error) {
	return inst.baseReader.MessageRejected(reason)
}
func (inst *PassthroughReader) DetectedBitErrors() int {
	return inst.baseReader.DetectedBitErrors()
}

var _ ea3.MessageReader = (*PassthroughReader)(nil)

func NewPassthroughReader(r ea.ByteBlockDiscardReader, nAnchorBytes uint8, maxHammingDistPerByteIncl uint8, maxMessageSize uint32) *PassthroughReader {
	br := ea3.NewBaseReader(r, nAnchorBytes, maxHammingDistPerByteIncl, maxMessageSize)
	return &PassthroughReader{
		baseReader: br,
	}
}
