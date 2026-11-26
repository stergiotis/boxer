package applink

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// Codec spells a value of type T — an operation's arguments, or a
// launch kind's config — as the tail of a `boxer:` link (ADR-0409
// §SD3). Set it as an operation's app.OperationSpec.Link, or
// register it for a kind with RegisterKind. Format and Parse are
// the app's spelling; ParseTail and Tail give them the URL
// pieces. Parse is strict: an unknown key or a malformed value is an
// error. Examples are values the codec spells; the conformance test
// round-trips each.
type Codec[T any] struct {
	Format   func(v T) (tail string, err error)
	Parse    func(tail string) (v T, err error)
	Examples []T
}

var _ app.LinkCodecI = Codec[struct{}]{}

// FormatLink implements app.LinkCodecI.
func (inst Codec[T]) FormatLink(args []byte) (tail string, err error) {
	v, err := buscodec.Decode[T](args)
	if err != nil {
		err = eb.Build().Errorf("applink: codec: decode the arguments: %w", err)
		return
	}
	return inst.Format(v)
}

// ParseLink implements app.LinkCodecI.
func (inst Codec[T]) ParseLink(tail string) (args []byte, err error) {
	v, err := inst.Parse(tail)
	if err != nil {
		return
	}
	args, err = buscodec.Encode(v)
	if err != nil {
		err = eb.Build().Errorf("applink: codec: encode the arguments: %w", err)
	}
	return
}

// LinkExamples implements app.LinkCodecI.
func (inst Codec[T]) LinkExamples() (examples [][]byte) {
	for _, ex := range inst.Examples {
		b, err := buscodec.Encode(ex)
		if err != nil {
			continue
		}
		examples = append(examples, b)
	}
	return
}
