package ea

import (
	"bufio"
	"bytes"
	"github.com/stergiotis/boxer/public/observability/eh"
	"io"
)

type DeferredTee struct {
	r  *bufio.Reader
	w  io.Writer
	b  *bytes.Buffer
	lr io.LimitedReader
}

var _ io.Reader = (*DeferredTee)(nil)

var _ io.ByteReader = (*DeferredTee)(nil)

func NewDeferredTee(r *bufio.Reader, w io.Writer, buf *bytes.Buffer) *DeferredTee {
	return &DeferredTee{
		r: r,
		w: w,
		b: buf,
		lr: io.LimitedReader{R: r,
			N: 0},
	}
}

func (inst *DeferredTee) Read(p []byte) (n int, err error) {
	n, err = inst.r.Read(p)
	if n == 0 || err != nil {
		return
	}
	var nw int
	nw, err = inst.b.Write(p[:n])
	if nw != n {
		err = eh.Errorf("unable to write all read bytes: read %d bytes, wrote %d bytes", n, nw)
		return
	}
	return
}

// Discard needs buffering, use DiscardAndFlush to stream directly to output
func (inst *DeferredTee) Discard(n int) (int, error) {
	b := inst.b
	b.Grow(n)
	lr := inst.lr
	lr.N = int64(n)
	u, err := b.ReadFrom(&lr)
	return int(u), err
}

func (inst *DeferredTee) DiscardAndFlush(n int) (nWritten int, err error) {
	u, err := inst.Flush()
	if err != nil {
		return u, err
	}
	nWritten += u
	lr := inst.lr
	lr.N = int64(n)
	w := inst.w
	u64, err := io.Copy(w, &lr)
	nWritten += int(u64)
	return nWritten, err
}

func (inst *DeferredTee) ReadByte() (b byte, err error) {
	b, err = inst.r.ReadByte()
	if err != nil {
		return
	}
	err = inst.b.WriteByte(b)
	return
}

func (inst *DeferredTee) Flush() (int, error) {
	n, err := inst.b.WriteTo(inst.w)
	return int(n), err
}

func (inst *DeferredTee) Peek(n int) ([]byte, error) {
	return inst.r.Peek(n)
}
