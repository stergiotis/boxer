package chserver

import (
	"bytes"
	"io"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// exception.go — a run that fails after its result started streaming.
//
// By then the status line has gone out as 200, so the server cannot say
// "this failed" the way it does for a statement it rejects up front.
// Instead it names a random tag in the response header and, when the run
// breaks, appends a trailer carrying the exception to whatever body bytes
// it had already sent:
//
//	\r\n__exception__\r\n<tag>\r\n<message>\n<len> <tag>\r\n__exception__\r\n
//
// where <len> counts <message> including its newline. The tag is what
// makes the trailer recognisable: it is minted per response, so result
// bytes that happen to spell `__exception__` cannot also spell it.
//
// Without this reader the trailer reaches the decoder as payload. A row
// format shows it as a garbled last row; ArrowStream stops with "could not
// read message metadata: unexpected EOF", and the server's own diagnostic —
// the one sentence that says what went wrong — is never shown to anyone.

// headerExceptionTag names the per-response tag of the trailer.
const headerExceptionTag = "X-ClickHouse-Exception-Tag"

// exceptionMarker opens and closes the trailer.
const exceptionMarker = "\r\n__exception__\r\n"

// maxTrailerBytes is how much of the body's tail is held back until the
// body ends, so a trailer can be removed before any of it is delivered. A
// trailer longer than this is not recognised and the run fails the way it
// did before this reader existed — as a decode error — which loses the
// diagnostic but never invents one. The message part is bounded by the
// same budget as a rejected statement's body.
const maxTrailerBytes = maxErrorBodyBytes + 512

// maxErrorDrainBytes bounds how much of a failed response is read looking
// for its diagnostic. A server compressing its output buffers the result
// and can still answer 500 after the run produced rows; that body is the
// rows followed by the exception, so the diagnostic is at the end.
const maxErrorDrainBytes = 64 << 20

// diagnosticOf returns the server's exception from a failed response body:
// the tagged trailer's message when there is one, else the last
// `Code: <code>.` paragraph when the response named the code, else the body
// as it is. The result is capped at maxErrorBodyBytes from its start.
func diagnosticOf(body []byte, code string, tag string) (detail []byte) {
	detail = body
	if _, message, ok := cutExceptionTrailer(body, tag); ok {
		detail = message
	} else if code != "" {
		if i := bytes.LastIndex(body, []byte("Code: "+code+". ")); i >= 0 {
			detail = body[i:]
		}
	}
	if len(detail) > maxErrorBodyBytes {
		detail = detail[:maxErrorBodyBytes]
	}
	return
}

// exceptionTrailerReader passes a response body through, holding back its
// last maxTrailerBytes. At the end of the body it strips a trailer bearing
// the response's tag and reports the exception in place of the body's own
// ending.
type exceptionTrailerReader struct {
	r    io.Reader
	tag  string
	held []byte
	done bool
	err  error
}

func newExceptionTrailerReader(r io.Reader, tag string) (inst *exceptionTrailerReader) {
	inst = &exceptionTrailerReader{
		r:    r,
		tag:  tag,
		held: make([]byte, 0, 2*maxTrailerBytes),
	}
	return
}

func (inst *exceptionTrailerReader) Read(p []byte) (n int, err error) {
	for {
		release := len(inst.held) - maxTrailerBytes
		if inst.done {
			release = len(inst.held)
		}
		if release > 0 {
			n = copy(p, inst.held[:release])
			inst.held = inst.held[:copy(inst.held, inst.held[n:])]
			return
		}
		if inst.done {
			err = inst.err
			return
		}
		// At most maxTrailerBytes are held here, so the capacity always
		// leaves room for a read.
		read, rerr := inst.r.Read(inst.held[len(inst.held):cap(inst.held)])
		inst.held = inst.held[:len(inst.held)+read]
		if rerr != nil {
			inst.done = true
			// The server closes the connection after a trailer without
			// sending the last chunk, so a chunked body that carried one
			// ends in io.ErrUnexpectedEOF rather than io.EOF. A complete,
			// tagged trailer is the better account of how the run ended;
			// any other ending keeps its own error.
			inst.err = rerr
			if data, message, ok := cutExceptionTrailer(inst.held, inst.tag); ok {
				inst.held = data
				bld := eb.Build().Int("bytesBeforeFailure", len(data)).Str("message", string(message))
				inst.err = bld.Errorf("clickhouse: the query failed after its result started streaming: %s", message) //boxer:lint disable=CS013 reason="the GUI renders err.Error(); the server's diagnostic is the message"
			}
		}
	}
}

// cutExceptionTrailer splits a trailer bearing tag off the end of body,
// returning the bytes before it and the exception message it carried. Every
// length and tag is checked before anything is cut, so a body that merely
// ends in something trailer-shaped reports ok=false and is left alone.
func cutExceptionTrailer(body []byte, tag string) (data []byte, message []byte, ok bool) {
	if tag == "" || !bytes.HasSuffix(body, []byte(exceptionMarker)) {
		return
	}
	rest := body[:len(body)-len(exceptionMarker)]
	nl := bytes.LastIndexByte(rest, '\n')
	if nl < 0 {
		return
	}
	lenStr, gotTag, found := bytes.Cut(rest[nl+1:], []byte{' '})
	if !found || string(gotTag) != tag {
		return
	}
	msgLen, convErr := strconv.Atoi(string(lenStr))
	if convErr != nil || msgLen <= 0 || msgLen > nl+1 {
		return
	}
	msgStart := nl + 1 - msgLen
	opening := exceptionMarker + tag + "\r\n"
	if !bytes.HasSuffix(rest[:msgStart], []byte(opening)) {
		return
	}
	data = rest[:msgStart-len(opening)]
	message = bytes.TrimSpace(rest[msgStart : nl+1])
	ok = true
	return
}
