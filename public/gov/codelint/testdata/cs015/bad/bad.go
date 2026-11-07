package bad

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
)

// A raw request with the subject constant.
func viaConst(b app.BusI, text string) {
	_, _ = b.Request(clipboardbroker.SubjectWrite, []byte(text)) // want a CS015 finding here
}

// The subject spelled out.
func viaLiteral(b app.BusI, text string) {
	_, _ = b.Request("clipboard.write", []byte(text)) // want a CS015 finding here
}

// The subject through a local constant.
const subject = clipboardbroker.SubjectWrite

func viaLocalConst(b app.BusI, text string) {
	_, _ = b.Request(subject, []byte(text)) // want a CS015 finding here
}

// A Request method on a type of the package's own, same subject.
type requester struct{}

func (requester) Request(subject string, payload []byte) (reply []byte, err error) { return }

func viaOwnType(text string) {
	_, _ = requester{}.Request(clipboardbroker.SubjectWrite, []byte(text)) // want a CS015 finding here
}

// The unlabelled text call, called and taken as a value.
func viaWriteText(b app.BusI, text string) {
	_ = clipboardbroker.WriteText(b, text) // want a CS015 finding here
}

func viaValue() func(app.BusI, string) error {
	return clipboardbroker.WriteText // want a CS015 finding here
}
