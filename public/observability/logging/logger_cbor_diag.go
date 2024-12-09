package logging

import (
	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"io"
)

type CborDiagLogger struct {
	Out io.StringWriter
}

func (inst *CborDiagLogger) Write(p []byte) (n int, err error) {
	p, err = convertToCBOR(p)
	if err != nil {
		err = eh.Errorf("unable to convert to cbor: %w", err)
		return
	}
	var diag string
	diag, err = cbor.Diagnose(p)
	if err != nil {
		err = eh.Errorf("unable to convert to cbor diag: %w", err)
		return
	}
	_, _ = inst.Out.WriteString(diag)
	_, _ = inst.Out.WriteString("\n")
	return
}

var _ io.Writer = (*CborDiagLogger)(nil)

func NewCborDiagLogger(out io.StringWriter) *CborDiagLogger {
	return &CborDiagLogger{Out: out}
}
