package logging

import (
	"io"

	cbor2 "github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/yassinebenaid/godump"
)

type CborSpewLogger struct {
	Out    io.Writer
	dumper *godump.Dumper
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

	err = inst.dumper.Fprint(inst.Out, v)
	return
}

var _ io.Writer = (*CborSpewLogger)(nil)

func NewCborGodumpLogger(out io.Writer) *CborSpewLogger {
	dumper := &godump.Dumper{
		Indentation:             "  ",
		ShowPrimitiveNamedTypes: false,
		HidePrivateFields:       false,
		Theme:                   godump.DefaultTheme,
	}
	return &CborSpewLogger{Out: out, dumper: dumper}
}
