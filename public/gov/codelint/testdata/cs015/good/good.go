package good

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
)

// The labelled call, whose record the caller shows.
func copyIt(b app.BusI, text string) (rec clipboardbroker.CopyRecord, err error) {
	rec, err = clipboardbroker.Write(b, clipboardbroker.Payload{Text: text, Source: "fixture"})
	return
}

// A request on the broker's other subject.
func resolve(b app.BusI, handle string) {
	_, _ = b.Request(clipboardbroker.SubjectResolve, []byte(handle))
}

// The subject named in a capability declaration, not requested.
var caps = []app.SubjectFilter{{Pattern: clipboardbroker.SubjectWrite, Direction: app.CapDirectionPub}}

// A request with a subject that is not a constant.
func dynamic(b app.BusI, subject string) {
	_, _ = b.Request(subject, nil)
}
