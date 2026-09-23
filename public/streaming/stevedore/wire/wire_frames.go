package wire

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultMaxFrame is the frame bound a reader takes when given none. It
// matches the buffer a framework's subprocess processor reads with by
// default, so a processor that does not raise it fails on the same frames the
// framework would.
const DefaultMaxFrame = 64 * 1024

// netstringMaxDigits bounds the decimal length of a netstring; ten digits
// cover every 32-bit length and reject a runaway header early.
const netstringMaxDigits = 10

// ErrFrameTooLarge is wrapped by a reader refusing a frame above its bound.
var ErrFrameTooLarge = eh.Errorf("frame exceeds the reader's bound")

// FrameReader reads one frame at a time from a pipe under one codec.
//
// A reader holds a frame at most once: the slice Read returns is owned by the
// caller and is not reused. A clean end of the pipe between frames is io.EOF;
// an end inside a frame is an error, because the far side died mid-message.
type FrameReader struct {
	r        *bufio.Reader
	codec    CodecE
	maxFrame int
	scratch  [4]byte
}

// NewFrameReader wraps r. maxFrame bounds a frame's payload; zero takes
// DefaultMaxFrame.
func NewFrameReader(r io.Reader, codec CodecE, maxFrame int) *FrameReader {
	if maxFrame <= 0 {
		maxFrame = DefaultMaxFrame
	}
	return &FrameReader{r: bufio.NewReader(r), codec: codec, maxFrame: maxFrame}
}

// Read returns the next frame's payload, or io.EOF at a clean end.
func (inst *FrameReader) Read() (frame []byte, err error) {
	switch inst.codec {
	case CodecLines:
		return inst.readLine()
	case CodecLengthPrefixedUint32BE:
		return inst.readLengthPrefixed()
	case CodecNetstring:
		return inst.readNetstring()
	default:
		err = eb.Build().Uint8("codec", uint8(inst.codec)).Errorf("unknown frame codec")
		return
	}
}

func (inst *FrameReader) readLine() (frame []byte, err error) {
	line, err := inst.r.ReadSlice('\n')
	if err != nil {
		if err == bufio.ErrBufferFull {
			// Drain the rest of the line so the next Read starts on a frame.
			for err == bufio.ErrBufferFull {
				_, err = inst.r.ReadSlice('\n')
			}
			err = eb.Build().Int("max", inst.maxFrame).Errorf("line frame: %w", ErrFrameTooLarge)
			return
		}
		if err == io.EOF && len(line) > 0 {
			err = eh.Errorf("pipe ended inside a line frame")
		}
		return nil, err
	}
	line = line[:len(line)-1]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	if len(line) > inst.maxFrame {
		err = eb.Build().Int("len", len(line)).Int("max", inst.maxFrame).Errorf("line frame: %w", ErrFrameTooLarge)
		return
	}
	frame = make([]byte, len(line))
	copy(frame, line)
	return
}

func (inst *FrameReader) readLengthPrefixed() (frame []byte, err error) {
	_, err = io.ReadFull(inst.r, inst.scratch[:])
	if err != nil {
		if err == io.ErrUnexpectedEOF {
			err = eh.Errorf("pipe ended inside a length prefix")
		}
		return nil, err
	}
	n := binary.BigEndian.Uint32(inst.scratch[:])
	return inst.readPayload(int64(n))
}

func (inst *FrameReader) readNetstring() (frame []byte, err error) {
	var n int64
	digits := 0
	for {
		var c byte
		c, err = inst.r.ReadByte()
		if err != nil {
			if err == io.EOF && digits == 0 {
				return nil, io.EOF
			}
			if err == io.EOF {
				err = eh.Errorf("pipe ended inside a netstring length")
			}
			return nil, err
		}
		if c == ':' {
			if digits == 0 {
				err = eh.Errorf("netstring has no length digits")
				return
			}
			break
		}
		if c < '0' || c > '9' {
			err = eb.Build().Str("byte", strconv.QuoteRune(rune(c))).Errorf("netstring length holds a non-digit")
			return
		}
		digits++
		if digits > netstringMaxDigits {
			err = eb.Build().Int("digits", digits).Errorf("netstring length has too many digits")
			return
		}
		n = n*10 + int64(c-'0')
	}
	frame, err = inst.readPayload(n)
	if err != nil {
		if errors.Is(err, ErrFrameTooLarge) {
			// The payload was drained; take the comma too so the next Read
			// starts on a frame. A missing comma is reported by that Read.
			_, _ = inst.r.ReadByte()
		}
		return
	}
	c, err := inst.r.ReadByte()
	if err != nil {
		if err == io.EOF {
			err = eh.Errorf("pipe ended before the netstring's trailing comma")
		}
		return nil, err
	}
	if c != ',' {
		err = eb.Build().Str("byte", strconv.QuoteRune(rune(c))).Errorf("netstring trailing comma is missing")
		return nil, err
	}
	return
}

// readPayload reads exactly n bytes. A frame above the bound is skipped
// rather than held — its bytes are drained so the next Read starts on a
// frame — and refused, so an oversize frame costs no memory and no sync.
func (inst *FrameReader) readPayload(n int64) (frame []byte, err error) {
	if n > int64(inst.maxFrame) {
		_, derr := io.CopyN(io.Discard, inst.r, n)
		if derr != nil {
			err = eb.Build().Int64("len", n).Errorf("pipe ended inside an oversize frame: %w", derr)
			return
		}
		err = eb.Build().Int64("len", n).Int("max", inst.maxFrame).Errorf("frame: %w", ErrFrameTooLarge)
		return
	}
	frame = make([]byte, n)
	_, err = io.ReadFull(inst.r, frame)
	if err != nil {
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			err = eb.Build().Int64("len", n).Errorf("pipe ended inside a frame")
		}
		return nil, err
	}
	return
}

// FrameWriter writes one frame at a time to a pipe under one codec, flushing
// after each so the far side never waits on a buffered frame.
type FrameWriter struct {
	w       *bufio.Writer
	codec   CodecE
	scratch [netstringMaxDigits + 1]byte
}

// NewFrameWriter wraps w.
func NewFrameWriter(w io.Writer, codec CodecE) *FrameWriter {
	return &FrameWriter{w: bufio.NewWriter(w), codec: codec}
}

// Write frames payload and flushes it.
func (inst *FrameWriter) Write(payload []byte) (err error) {
	switch inst.codec {
	case CodecLines:
		for _, c := range payload {
			if c == '\n' {
				err = eh.Errorf("a payload holding a newline cannot travel under the lines codec")
				return
			}
		}
		_, err = inst.w.Write(payload)
		if err == nil {
			err = inst.w.WriteByte('\n')
		}
	case CodecLengthPrefixedUint32BE:
		if int64(len(payload)) > int64(^uint32(0)) {
			err = eb.Build().Int("len", len(payload)).Errorf("payload exceeds a 32-bit length prefix")
			return
		}
		binary.BigEndian.PutUint32(inst.scratch[:4], uint32(len(payload)))
		_, err = inst.w.Write(inst.scratch[:4])
		if err == nil {
			_, err = inst.w.Write(payload)
		}
	case CodecNetstring:
		head := strconv.AppendInt(inst.scratch[:0], int64(len(payload)), 10)
		head = append(head, ':')
		_, err = inst.w.Write(head)
		if err == nil {
			_, err = inst.w.Write(payload)
		}
		if err == nil {
			err = inst.w.WriteByte(',')
		}
	default:
		err = eb.Build().Uint8("codec", uint8(inst.codec)).Errorf("unknown frame codec")
		return
	}
	if err != nil {
		err = eh.Errorf("write frame: %w", err)
		return
	}
	err = inst.w.Flush()
	if err != nil {
		err = eh.Errorf("flush frame: %w", err)
	}
	return
}
