package passthrough

import (
	"bufio"
	"io"

	"github.com/stergiotis/boxer/public/anchor"
	"github.com/stergiotis/boxer/public/fec/ea"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type PassthroughWriter struct {
	w         *bufio.Writer
	anchor    []byte
	inMessage bool
}

func NewWriter(w io.Writer, nAnchorBytes uint8) *PassthroughWriter {
	return &PassthroughWriter{
		w:         bufio.NewWriter(w),
		anchor:    anchor.MakeAnchor(int(nAnchorBytes)),
		inMessage: false,
	}
}

func (inst *PassthroughWriter) WriteByte(c byte) error {
	return inst.w.WriteByte(c)
}

func (inst *PassthroughWriter) Write(p []byte) (n int, err error) {
	return inst.w.Write(p)
}

func (inst *PassthroughWriter) BeginMessage() (n int, err error) {
	if inst.inMessage {
		err = eh.Errorf("message nesting detected, call EndMessage before BeginMessage")
		return
	}
	inst.inMessage = true
	n, err = inst.w.Write(inst.anchor)
	return
}

func (inst *PassthroughWriter) EndMessage() (paddingBitsBeforeEncoding int, err error) {
	if inst.inMessage == false {
		err = eh.Errorf("no message to end, call BeginMessage before EndMessage")
		return
	}
	err = inst.w.Flush()
	if err != nil {
		err = eh.Errorf("unable to flush buffer: %w", err)
		return
	}
	inst.inMessage = false
	return
}

var _ ea.MessageWriter = (*PassthroughWriter)(nil)
