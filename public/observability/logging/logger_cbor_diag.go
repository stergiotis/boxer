package logging

import (
	"bytes"
	ea2 "github.com/stergiotis/boxer/public/ea"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/cbor"
	"io"
)

type CborDiagLogger struct {
	Out  io.StringWriter
	diag *cbor.Diagnostics
}

func (inst *CborDiagLogger) Write(p []byte) (n int, err error) {
	p, err = convertToCBOR(p)
	if err != nil {
		err = eh.Errorf("unable to convert to cbor: %w", err)
		return
	}

	r := bytes.NewReader(p)
	var rb ea2.ByteBlockDiscardReader
	rb, err = ea2.NewByteBlockReaderDiscardReader(r)
	if err != nil {
		err = eh.Errorf("unable to wrap reader: %w", err)
		return
	}
	err = inst.diag.RunIndent(inst.Out, rb, "  ")
	if err == nil {
		n = len(p)
	}
	_, _ = inst.Out.WriteString("\n")
	return
}

var _ io.Writer = (*CborDiagLogger)(nil)

func NewCborDiagLogger(out io.StringWriter) *CborDiagLogger {
	return &CborDiagLogger{Out: out, diag: cbor.NewDiagnostics()}
}
