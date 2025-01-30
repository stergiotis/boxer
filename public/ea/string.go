package ea

import (
	"github.com/stergiotis/boxer/public/unsafeperf"
	"io"
)

func ReadAllString(reader io.Reader) (out string, err error) {
	var b []byte
	b, err = io.ReadAll(reader)
	if err != nil {
		return
	}
	out = unsafeperf.UnsafeBytesToString(b)
	return
}
