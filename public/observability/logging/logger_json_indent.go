package logging

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"sync"

	"github.com/stergiotis/boxer/public/ea"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// JsonIndentLogger re-encodes each zerolog event through one jsontext
// encoder. Write is safe for concurrent use — zerolog calls it from every
// goroutine that logs, and the encoder is a state machine that interleaved
// writes would leave mid-value for good.
type JsonIndentLogger struct {
	mu     sync.Mutex
	Out    io.Writer
	enc    *jsontext.Encoder
	szW    *ea.SizeMeasureWriter
	Prefix string
	Indent string
}

func (inst *JsonIndentLogger) Write(p []byte) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	var v any
	v, err = UnmarshallZerologMsg(p)
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
	if err != nil {
		// An event that failed part-way leaves the encoder inside it; start
		// the next one on a fresh encoder rather than failing it too.
		inst.enc, inst.szW = nil, nil
	}
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
