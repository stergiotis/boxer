package bad

import (
	"context"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
)

// A raw request with the subject constant.
func viaConst(ctx context.Context, b app.BusI, text string) {
	_, _ = b.Request(ctx, clipboardbroker.SubjectWrite, []byte(text)) // want a CS015 finding here
}

// The subject spelled out.
func viaLiteral(ctx context.Context, b app.BusI, text string) {
	_, _ = b.Request(ctx, "clipboard.write", []byte(text)) // want a CS015 finding here
}

// The subject through a local constant.
const subject = clipboardbroker.SubjectWrite

func viaLocalConst(ctx context.Context, b app.BusI, text string) {
	_, _ = b.Request(ctx, subject, []byte(text)) // want a CS015 finding here
}

// A Request method on a type of the package's own, same subject.
type requester struct{}

func (requester) Request(subject string, payload []byte) (reply []byte, err error) { return }

func viaOwnType(text string) {
	_, _ = requester{}.Request(clipboardbroker.SubjectWrite, []byte(text)) // want a CS015 finding here
}

// The unlabelled text call, called and taken as a value.
func viaWriteText(ctx context.Context, b app.BusI, text string) {
	_ = clipboardbroker.WriteText(ctx, b, text) // want a CS015 finding here
}

func viaValue() func(context.Context, app.BusI, string) error {
	return clipboardbroker.WriteText // want a CS015 finding here
}
