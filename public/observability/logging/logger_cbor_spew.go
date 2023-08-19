package logging

import (
	"github.com/davecgh/go-spew/spew"
	cbor2 "github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"io"
)

type CborSpewLogger struct {
	Out io.Writer
}

func (inst *CborSpewLogger) Write(p []byte) (n int, err error) {
	p, err = convertToCBOR(p)
	if err != nil {
		err = eh.Errorf("unable to convert to cbor: %w", err)
		return
	}
	var v interface{}
	err = cbor2.Unmarshal(p, &v)
	if err != nil {
		err = eh.Errorf("unable to unmarshall cbor: %w", err)
	}

	spew.Fdump(inst.Out, v)
	return
}

var _ io.Writer = (*CborSpewLogger)(nil)

func NewCborSpewLogger(out io.Writer) *CborSpewLogger {
	return &CborSpewLogger{Out: out}
}
