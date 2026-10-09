package introspecthttp

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// InProcess returns a RoundTripper that answers every request for origin —
// scheme and host, as in "http://keelson.invalid" — by calling h in this
// process, and hands every other request to next (ADR-0290 §SD4). It is how
// a host with no listening sockets, a browser tab, serves an HTTP endpoint to
// its own clients: they keep speaking HTTP to a URL and never learn that no
// socket carried it. h runs on the caller's goroutine and its whole response
// is buffered, which suits replies that are built in memory anyway.
func InProcess(origin string, h http.Handler, next http.RoundTripper) (rt http.RoundTripper, err error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return nil, eb.Build().Str("origin", origin).Errorf("introspecthttp: an in-process origin is scheme://host")
	}
	return inProcess{scheme: u.Scheme, host: u.Host, h: h, next: next}, nil
}

type inProcess struct {
	scheme, host string
	h            http.Handler
	next         http.RoundTripper
}

func (inst inProcess) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	if !strings.EqualFold(req.URL.Scheme, inst.scheme) || !strings.EqualFold(req.URL.Host, inst.host) {
		return inst.next.RoundTrip(req)
	}
	in := req.Clone(req.Context())
	in.RequestURI = req.URL.RequestURI()
	if in.Body == nil {
		in.Body = http.NoBody
	}
	w := &recorder{header: make(http.Header)}
	panicked := serve(inst.h, w, in)
	if req.Body != nil {
		_ = req.Body.Close()
	}
	if panicked != nil {
		// A server's net/http recovers a handler's panic and drops the
		// connection; here it would end the host, so it is the request's
		// error instead, as a dropped connection is to a client.
		return nil, eb.Build().Str("url", req.URL.String()).Str("panic", fmt.Sprint(panicked)).Errorf("introspecthttp: the in-process handler panicked")
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	resp = &http.Response{
		Status:        strconv.Itoa(status) + " " + http.StatusText(status),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        w.header,
		Body:          io.NopCloser(bytes.NewReader(w.body.Bytes())),
		ContentLength: int64(w.body.Len()),
		Request:       req,
	}
	return
}

// serve runs h, returning what it panicked with, or nil.
func serve(h http.Handler, w http.ResponseWriter, r *http.Request) (panicked any) {
	defer func() { panicked = recover() }()
	h.ServeHTTP(w, r)
	return
}

// recorder is the ResponseWriter a handler writes into.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (inst *recorder) Header() http.Header { return inst.header }

func (inst *recorder) WriteHeader(status int) {
	if inst.status == 0 {
		inst.status = status
	}
}

func (inst *recorder) Write(b []byte) (int, error) {
	inst.WriteHeader(http.StatusOK)
	return inst.body.Write(b)
}
