//go:build llm_generated_opus47

package buscodec

import (
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// cborCodec is the default CBOR codec. Uses CanonicalEncOptions so the
// same Go value always produces the same wire bytes — load-bearing for
// replay diffing and content-addressed caching downstream.
//
// Time is encoded as RFC3339 with nanosecond precision (not the CBOR
// default of integer Unix seconds, which truncates sub-second). DTO
// timestamp fields are time.Time and several carry sub-second capture
// instants that must survive the bus round-trip. RFC3339-nano is
// location-sensitive, so the same instant in two zones encodes to
// different bytes; producers normalise to UTC to keep the canonical
// encoding deterministic.
type cborCodec struct {
	enc cbor.EncMode
}

// NewCBOR constructs the default CBOR codec.
func NewCBOR() (c CodecI) {
	opts := cbor.CanonicalEncOptions()
	opts.Time = cbor.TimeRFC3339Nano
	enc, err := opts.EncMode()
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
