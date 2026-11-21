package good

import (
	"context"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
)

// The labelled call, whose record the caller shows.
func copyIt(ctx context.Context, b app.BusI, text string) (rec clipboardbroker.CopyRecord, err error) {
	rec, err = clipboardbroker.Write(ctx, b, clipboardbroker.Payload{Text: text, Source: "fixture"})
	return
}

// A request on the broker's other subject.
func resolve(ctx context.Context, b app.BusI, handle string) {
	_, _ = b.Request(ctx, clipboardbroker.SubjectResolve, []byte(handle))
}

// The subject named in a capability declaration, not requested.
var caps = []app.SubjectFilter{{Pattern: clipboardbroker.SubjectWrite, Direction: app.CapDirectionPub}}

// A request with a subject that is not a constant.
func dynamic(ctx context.Context, b app.BusI, subject string) {
	_, _ = b.Request(ctx, subject, nil)
}
