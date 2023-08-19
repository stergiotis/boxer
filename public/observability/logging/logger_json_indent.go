package logging

import (
	"encoding/json"
	"github.com/stergiotis/boxer/public/observability/eh"
	"io"
)

type JsonIndentLogger struct {
	Out    io.Writer
	Prefix string
	Indent string
}

func (inst *JsonIndentLogger) Write(p []byte) (n int, err error) {
	var v interface{}
	v, err = unmarshallZerologMsg(p)
	if err != nil {
		err = eh.Errorf("unable to unmarshall zerolog msg: %w", err)
		return
	}

	var b []byte
	b, err = json.MarshalIndent(v, inst.Prefix, inst.Indent)
	if err != nil {
		err = eh.Errorf("unable to encode to json: %w", err)
		return
	}
	_, err = inst.Out.Write(b)
	if err != nil {
		err = eh.Errorf("unable to write to output: %w", err)
		return
	}

	n = len(p)
	return
}

var _ io.Writer = (*JsonIndentLogger)(nil)

func NewJsonIndentLogger(out io.Writer) *JsonIndentLogger {
	return &JsonIndentLogger{
		Out:    out,
		Prefix: "",
		Indent: "  ",
	}
}
