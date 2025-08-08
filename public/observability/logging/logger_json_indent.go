package logging

import (
	"io"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/stergiotis/boxer/public/ea"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type JsonIndentLogger struct {
	Out    io.Writer
	Prefix string
	Indent string
	enc    *jsontext.Encoder
	szW    *ea.SizeMeasureWriter
}

func (inst *JsonIndentLogger) Write(p []byte) (n int, err error) {
	var v interface{}
	v, err = unmarshallZerologMsg(p)
	if err != nil {
		err = eh.Errorf("unable to unmarshall zerolog msg: %w", err)
		return
	}

	enc := inst.enc
	szW := inst.szW
	if enc == nil {
		szW = &ea.SizeMeasureWriter{
			Size: 0,
		}
		if inst.Indent != "" || inst.Prefix != "" {
			enc = jsontext.NewEncoder(io.MultiWriter(inst.Out, szW),
				jsontext.EscapeForHTML(false),
				jsontext.EscapeForJS(false),
				jsontext.Multiline(true),
				jsontext.WithIndent(inst.Indent),
				jsontext.WithIndentPrefix(inst.Prefix),
			)
		} else {
			enc = jsontext.NewEncoder(io.MultiWriter(inst.Out, szW),
				jsontext.EscapeForHTML(false),
				jsontext.EscapeForJS(false),
			)
		}
		inst.enc = enc
		inst.szW = szW
	} else {
		szW.Size = 0
	}

	err = json.MarshalEncode(enc,
		v,
		json.DefaultOptionsV2())
	n = int(szW.Size)
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
