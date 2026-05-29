//go:build llm_generated_opus47

package buscodec

import (
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// cborCodec is the default CBOR codec. Uses CanonicalEncOptions so the
// same Go value always produces the same wire bytes — load-bearing for
// replay diffing and content-addressed caching downstream.
type cborCodec struct {
	enc cbor.EncMode
}

// NewCBOR constructs the default CBOR codec.
func NewCBOR() (c CodecI) {
	enc, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		panic(fmt.Sprintf("buscodec: cbor canonical enc mode: %v", err))
	}
	c = &cborCodec{enc: enc}
	return
}

var _ CodecI = (*cborCodec)(nil)

func (inst *cborCodec) Name() (n string) {
	n = "cbor"
	return
}

func (inst *cborCodec) ContentType() (ct string) {
	ct = "application/cbor"
	return
}

func (inst *cborCodec) Encode(v any) (b []byte, err error) {
	b, err = inst.enc.Marshal(v)
	return
}

func (inst *cborCodec) Decode(b []byte, v any) (err error) {
	err = cbor.Unmarshal(b, v)
	return
}
