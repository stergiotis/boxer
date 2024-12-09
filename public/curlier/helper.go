package curlier

import (
	"net/http"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

func IsTransientError(code int) bool {
	switch code {
	case 408, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

func ParseHeaderArgument(header *http.Header, h string) (key string, err error) {
	k, v, ok := strings.Cut(h, ":")
	key = k
	if !ok {
		err = eh.Errorf("invalid header %q, expecting colon character", h)
		return
	}
	k = http.CanonicalHeaderKey(k)
	v = strings.TrimLeft(v, " \t")
	switch v {
	case "":
		header.Del(k)
		break
	case ";":
		header.Set(k, "")
		break
	default:
		vs := strings.Split(v, ",")
		for _, v := range vs {
			v = strings.TrimLeft(v, " \t")
			header.Add(k, v)
		}
	}
	return
}
