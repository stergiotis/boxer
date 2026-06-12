// The conformance suite consumed by its own reference codec, plus a
// broken-codec smoke proving the suite bites.
package codectest

import (
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
)

func TestJSONV1Conformance(tt *testing.T) {
	Run(tt, envelope.JSONV1{})
}

// lossyCodec drops the producer — CheckRoundTrip must reject it.
type lossyCodec struct{ envelope.JSONV1 }

func (lossyCodec) Name() string { return "lossy1" }

func (l lossyCodec) Encode(env envelope.EnvelopeV1) ([]byte, error) {
	env.Producer = ""
	return l.JSONV1.Encode(env)
}

func TestSuiteBites_LossyCodec(tt *testing.T) {
	if err := CheckRoundTrip(lossyCodec{}); err == nil {
		tt.Fatal("conformance suite failed to detect a lossy codec")
	}
}
