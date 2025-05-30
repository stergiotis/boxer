package ea

import (
	"bufio"
	"io"

	"github.com/stergiotis/boxer/public/fec/anchor"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type BaseReader struct {
	r                         *bufio.Reader
	peeked                    []byte
	totalBytesRead            int
	bytesPeeked               int
	detectedBitErrors         int
	maxMessageSize            int
	consecutiveAnchor         bool
	nAnchorBytes              uint8
	maxHammingDistPerByteIncl uint8
	inMessage                 bool
	buffered                  byte
}

var ErrTooFar = eh.Errorf("unable to peek byte, peeked more bytes than maxMessageSize")

// BytesRead return how many bytes have been consumed from the underlying reader
func (inst *BaseReader) BytesRead() int {
	return inst.totalBytesRead
}

// BytesPeeked return how many bytes have been peeked from the underlying reader (in-message)
func (inst *BaseReader) BytesPeeked() int {
	return inst.bytesPeeked
}

func (inst *BaseReader) PeekByte() (b byte, err error) {
	err = inst.EnsureSkippedPastAnchor()
	if err != nil {
		err = eh.Errorf("unable to skip past anchor: %w", err)
		return
	}
	p := inst.bytesPeeked
	peeked := inst.peeked
	if p >= len(peeked) {
		return 0, ErrTooFar
	}
	b = peeked[p]
	inst.bytesPeeked = p + 1
	return
}

func (inst *BaseReader) Bytes() (r []byte, err error) {
	err = inst.EnsureSkippedPastAnchor()
	if err != nil {
		err = eh.Errorf("unable to skip past anchor: %w", err)
		return
	}
	r = inst.peeked[inst.bytesPeeked:]
	return
}

func (inst *BaseReader) DiscardPeeking(nBytes int) (n int, err error) {
	if nBytes == 0 {
		return
	}
	err = inst.EnsureSkippedPastAnchor()
	if err != nil {
		err = eh.Errorf("unable to skip past anchor: %w", err)
		return
	}
	d := len(inst.peeked) - inst.bytesPeeked
	if nBytes > d {
		inst.bytesPeeked = inst.maxMessageSize
		n = d
		err = ErrTooFar
		return
	}
	inst.bytesPeeked += nBytes
	n = nBytes
	return
}

func (inst *BaseReader) InMessage() bool {
	return inst.inMessage
}

func (inst *BaseReader) EnsureSkippedPastAnchor() (err error) {
	if inst.inMessage {
		return
	}
	if inst.nAnchorBytes > 0 {
		var nBytesRead uint64
		var dist int
		if inst.consecutiveAnchor {
			nBytesRead, dist, err = anchor.SkipPastAnchorConsecutive(inst.r, int(inst.nAnchorBytes), int(inst.maxHammingDistPerByteIncl))
		} else {
			nBytesRead, dist, err = anchor.SkipPastAnchorInitial(inst.r, int(inst.nAnchorBytes), int(inst.maxHammingDistPerByteIncl))
		}
		inst.totalBytesRead += int(nBytesRead)
		inst.detectedBitErrors = dist
		if err != nil {
			return eh.Errorf("error skipping past anchor: %w", err)
		}
	}

	// read enough bytes to cover a full message
	inst.peeked, err = inst.r.Peek(inst.maxMessageSize)
	inst.bytesPeeked = 0
	if err != nil && err != io.EOF {
		return eh.Errorf("unable to peek from reader: %w", err)
	}
	err = nil

	inst.inMessage = true

	return
}

func (inst *BaseReader) MessageAccepted() (err error) {
	var n int
	n, err = inst.r.Discard(inst.bytesPeeked)
	inst.totalBytesRead += n
	inst.reset()
	if err != nil {
		return eh.Errorf("error while discarding from reader: %w", err)
	}
	return nil
}

func (inst *BaseReader) MessageRejected(reason error) (err error) {
	//log.Trace().Bytes("bytes", inst.peeked[:100]).Str("reason", reason.Error()).Msg("rejecting message")
	var n int
	n, err = inst.r.Discard(1)
	inst.totalBytesRead += n
	inst.reset()
	if err != nil {
		return eh.Errorf("error while discarding from reader: %w", err)
	}
	return
}

func (inst *BaseReader) reset() {
	inst.inMessage = false
	inst.detectedBitErrors = 0
	inst.peeked = nil
	inst.bytesPeeked = 0
}

func (inst *BaseReader) DetectedBitErrors() int {
	return inst.detectedBitErrors
}

func NewBaseReader(r io.Reader, nAnchorBytes uint8, maxHammingDistPerByteIncl uint8, maxMessageSize uint32) *BaseReader {
	b, ok := r.(*bufio.Reader)
	if !ok || uint32(b.Size()) < maxMessageSize {
		b = bufio.NewReaderSize(r, int(maxMessageSize))
	}
	return &BaseReader{
		consecutiveAnchor:         false,
		r:                         b,
		nAnchorBytes:              nAnchorBytes,
		maxHammingDistPerByteIncl: maxHammingDistPerByteIncl,
		inMessage:                 false,
		totalBytesRead:            0,
		bytesPeeked:               0,
		detectedBitErrors:         0,
		buffered:                  0,
		peeked:                    nil,
		maxMessageSize:            int(maxMessageSize),
	}
}
