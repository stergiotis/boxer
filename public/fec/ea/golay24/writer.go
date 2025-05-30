package golay24

import (
	"io"

	"github.com/stergiotis/boxer/public/fec/anchor"
	"github.com/stergiotis/boxer/public/fec/code/golay24"
	"github.com/stergiotis/boxer/public/fec/ea"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type writerStateE int

const stateInit writerStateE = 0

const stateFirst writerStateE = 1

const stateSecond writerStateE = 2

const stateThird writerStateE = 3

type Writer struct {
	w                 io.Writer
	anchor            []byte
	state             writerStateE
	buffered          byte
	totalBytesWritten int
}

var _ ea.MessageWriter = (*Writer)(nil)

func NewWriter(w io.Writer, nAnchorBytes uint8) *Writer {
	return &Writer{
		w:                 w,
		anchor:            anchor.MakeAnchor(int(nAnchorBytes)),
		state:             stateInit,
		totalBytesWritten: 0,
	}
}

func (inst *Writer) WriteByte(c byte) (err error) {
	var t uint16
	switch inst.state {
	case stateFirst:
		inst.buffered = c
		inst.state = stateSecond
		return nil
	case stateSecond:
		t = 3 * ((uint16(inst.buffered) << 4) | uint16(c>>4))
		inst.state = stateThird
		inst.buffered = c & 0x0f
		break
	case stateThird:
		t = 3 * ((uint16(inst.buffered) << 8) | uint16(c))
		inst.state = stateFirst
		break
	default:
		err = eh.Errorf("wrong state, call BeginMessage befor writing")
		return
	}
	buf := []byte{0, 0, 0}
	buf[0] = golay24.EncodingUint8Triples[t]
	buf[1] = golay24.EncodingUint8Triples[t+1]
	buf[2] = golay24.EncodingUint8Triples[t+2]
	var n int
	n, err = inst.w.Write(buf)
	inst.totalBytesWritten += n
	if err != nil {
		return err
	}
	return nil
}

func (inst *Writer) Write(p []byte) (n int, err error) {
	switch len(p) {
	case 0:
		return 0, nil
	case 1:
		err = inst.WriteByte(p[0])
		if err != nil {
			return
		}
		return 1, nil
	}
	s := inst.totalBytesWritten

	switch inst.state {
	case stateSecond:
		err = inst.WriteByte(p[0])
		if err != nil {
			return
		}
		n += 3
		err = inst.WriteByte(p[1])
		if err != nil {
			return
		}
		n += 3
		p = p[2:]
		break
	case stateThird:
		err = inst.WriteByte(p[0])
		if err != nil {
			return
		}
		n += 3
		p = p[1:]
		break
	}

	l := len(p)
	if l == 0 {
		return
	}

	l3 := (l / 3) * 3
	buf := []byte{0, 0, 0, 0, 0, 0}
	w := inst.w
	var u int
	for i := 0; i < l3; i += 3 {
		b0 := p[i]
		b1 := p[i+1]
		b2 := p[i+2]
		t0 := 3 * ((uint16(b0) << 4) | uint16(b1>>4))
		t1 := 3 * ((uint16(b1&0x0f) << 8) | uint16(b2))
		buf[0] = golay24.EncodingUint8Triples[t0]
		buf[1] = golay24.EncodingUint8Triples[t0+1]
		buf[2] = golay24.EncodingUint8Triples[t0+2]
		buf[3] = golay24.EncodingUint8Triples[t1]
		buf[4] = golay24.EncodingUint8Triples[t1+1]
		buf[5] = golay24.EncodingUint8Triples[t1+2]
		u, err = w.Write(buf)
		n += u
		if err != nil {
			inst.totalBytesWritten += n
			return
		}
	}
	for i := l3; i < l; i++ {
		err = inst.WriteByte(p[i])
		if err != nil {
			return
		}
		n += 3
	}
	n = inst.totalBytesWritten - s
	return
}

func (inst *Writer) BeginMessage() (n int, err error) {
	if inst.state != stateInit {
		err = eh.Errorf("message nesting detected, call EndMessage before BeginMessage")
		return
	}
	inst.state = stateFirst
	n, err = inst.w.Write(inst.anchor)
	inst.totalBytesWritten += n
	return
}

func (inst *Writer) EndMessage() (paddingBitsBeforeEncoding int, err error) {
	switch inst.state {
	case stateInit:
		err = eh.Errorf("no message to end, call BeginMessage before EndMessage")
		return
	case stateFirst:
		paddingBitsBeforeEncoding = 0
		break
	case stateSecond:
		err = inst.WriteByte(0x00)
		if err == nil {
			paddingBitsBeforeEncoding = 4
		}
		break
	case stateThird:
		err = inst.WriteByte(0x00)
		if err == nil {
			paddingBitsBeforeEncoding = 8
		}
		break
	}
	inst.state = stateInit
	return
}
