package envelope

import (
	"encoding/json"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// JSONV1 is the reference wire codec: indented encoding/json, struct
// fields in declaration order, PatchHash as hex via MarshalText. The
// payload is human-readable, which keeps on-disk envelopes debuggable.
// Deterministic for equal inputs.
type JSONV1 struct{}

var _ CodecI = JSONV1{}

// JSONV1Name is the frame name of the reference codec.
const JSONV1Name = "json1"

func (JSONV1) Name() (n string) {
	n = JSONV1Name
	return
}

func (JSONV1) Encode(env EnvelopeV1) (payload []byte, err error) {
	if env.Patch == nil {
		err = eh.Errorf("cannot encode an envelope with a nil patch")
		return
	}
	payload, err = json.MarshalIndent(env, "", "  ")
	if err != nil {
		err = eh.Errorf("marshal: %w", err)
	}
	return
}

func (JSONV1) Decode(payload []byte) (env EnvelopeV1, err error) {
	if err = json.Unmarshal(payload, &env); err != nil {
		err = eh.Errorf("unmarshal: %w", err)
		return
	}
	if env.Patch == nil {
		err = eh.Errorf("missing patch")
		return
	}
	return
}
