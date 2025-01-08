package golay24

import (
	"io"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/fec/code/golay24"
	ea2 "github.com/stergiotis/boxer/public/fec/ea"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type stateE int

const stateNil stateE = 0

const stateDecodeGolay24First stateE = 1

const stateDecodeGolay24Second stateE = 2

const stateDecodeGolay24Third stateE = 3

type Golay24Reader struct {
	baseReader        *ea2.BaseReader
	state             stateE
	detectedBitErrors int
	bufferedByte      byte
}

func (inst *Golay24Reader) BytesRead() int {
	return inst.baseReader.BytesRead()
}

func (inst *Golay24Reader) BytesPeeked() int {
	return inst.baseReader.BytesPeeked()
}

func (inst *Golay24Reader) ReadByte() (b byte, err error) {
	br := inst.baseReader

	if inst.state == stateDecodeGolay24Third {
		b = inst.bufferedByte
		inst.state = stateDecodeGolay24First
		return
	}

	var b0, b1, b2 uint8
	b0, err = br.PeekByte()
	if err != nil {
		return
	}
	b1, err = br.PeekByte()
	if err != nil {
		return
	}
	b2, err = br.PeekByte()
	if err != nil {
		return
	}

	cw := uint32(b0)<<16 | uint32(b1)<<8 | uint32(b2)
	inst.detectedBitErrors += int(golay24.NumberOfBitErrors(cw))
	t := golay24.DecodeSingle(cw)
	switch inst.state {
	case stateDecodeGolay24First:
		b = byte(t >> 4)
		inst.bufferedByte = byte(t & 0x0f)
		inst.state = stateDecodeGolay24Second
		break
	case stateDecodeGolay24Second:
		b = inst.bufferedByte<<4 | byte(t>>8)
		inst.bufferedByte = byte(t)
		inst.state = stateDecodeGolay24Third
		break
	default:
		log.Fatal().Msg("should never get here: invalid state")
	}
	return
}

func (inst *Golay24Reader) Read(p []byte) (n int, err error) {
	return inst.readSlow(p)
}

func (inst *Golay24Reader) readSlow(p []byte) (n int, err error) {
	nBytes := len(p)
	for i := 0; i < nBytes; i++ {
		var b byte
		b, err = inst.ReadByte()
		if err != nil {
			return
		}
		n++
		p[i] = b
	}
	return
}

func (inst *Golay24Reader) Discard(nBytes int) (n int, err error) {
	return inst.discardSlow(nBytes)
}

func (inst *Golay24Reader) discardSlow(nBytes int) (n int, err error) {
	for i := 0; i < nBytes; i++ {
		_, err = inst.ReadByte()
		if err != nil {
			return
		}
		n++
	}
	return
}

func (inst *Golay24Reader) MessageAccepted() (err error) {
	err = inst.skipTrailingBytes()
	if err != nil {
		err = eh.Errorf("unable to skip trailing bytes in state %d: %w", inst.state, err)
		return
	}
	inst.reset()
	return inst.baseReader.MessageAccepted()
}

func (inst *Golay24Reader) MessageRejected(reason error) (err error) {
	err = inst.skipTrailingBytes()
	if err != nil {
		err = eh.Errorf("unable to skip trailing bytes in state %d: %w", inst.state, err)
		return
	}
	inst.reset()
	return inst.baseReader.MessageRejected(reason)
}

func (inst *Golay24Reader) skipTrailingBytes() (err error) {
	switch inst.state {
	case stateDecodeGolay24First:
		break
	case stateDecodeGolay24Second:
		_, err = inst.baseReader.DiscardPeeking(2)
		break
	case stateDecodeGolay24Third:
		_, err = inst.baseReader.DiscardPeeking(1)
		break
	}
	inst.state = stateDecodeGolay24First
	return
}

func (inst *Golay24Reader) reset() {
	inst.detectedBitErrors = 0
}

func (inst *Golay24Reader) DetectedBitErrors() int {
	return inst.baseReader.DetectedBitErrors() + inst.detectedBitErrors
}

var _ ea2.MessageReader = (*Golay24Reader)(nil)

func NewGolay24Reader(r io.Reader, nAnchorBytes uint8, maxHammingDistPerByteIncl uint8, maxMessageSize uint32) *Golay24Reader {
	br := ea2.NewBaseReader(r, nAnchorBytes, maxHammingDistPerByteIncl, maxMessageSize)
	return &Golay24Reader{
		baseReader:        br,
		state:             stateDecodeGolay24First,
		bufferedByte:      0,
		detectedBitErrors: 0,
	}
}
