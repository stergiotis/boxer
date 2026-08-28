package envelope

import (
	"github.com/fxamacker/cbor/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// CBORV1 is the reference wire codec: deterministic CBOR over
// [EnvelopeV1] under RFC 8949 §4.2 core deterministic encoding —
// shortest-argument heads, definite lengths, bytewise-sorted map keys —
// the same rule set the leeway canonical record form pins (ADR-0201).
//
// Shape follows the Go structs: fxamacker maps a struct to a CBOR map
// keyed by field name (falling back to the `json:` tags [EnvelopeV1]
// already carries), patch hashes and change contents ride as byte
// strings rather than hex/base64 text, and unknown fields are
// ignored on decode. That keeps the codec a thin, evolvable projection
// of the domain types — adding a field to Change extends the map instead
// of shifting a frozen layout. Patch identity is NOT this encoding
// (see [patch.Patch.ComputeHash]); the wire form is free to move as long
// as it round-trips, and the frame name changes if it ever moves
// incompatibly.
//
// The payload is not text, but it is inspectable without bespoke tools:
// strip the [Frame] header and pipe the rest through
// `boxer.sh cbor diagnostics` for RFC 8949 §8 diagnostic notation.
type CBORV1 struct{}

var _ CodecI = CBORV1{}

// CBORV1Name is the frame name of the reference codec. Wire-frozen.
const CBORV1Name = "cbor1"

// cborV1EncMode is the shared encoder. Built once: EncMode values are
// immutable and safe for concurrent use, which is what CodecI promises.
//
// Time rides as an RFC 3339 string with nanosecond precision under tag 0
// — matching buscodec (ADR-0036) — because the CBOR default of integer
// Unix seconds truncates sub-second instants, and the float variants
// cannot hold nanoseconds at current epochs. RFC 3339 renders the zone,
// so the same instant in two locations would encode to different bytes;
// Encode normalises to UTC first to keep determinism, which the codec
// contract permits (only the instant must survive).
var cborV1EncMode = func() cbor.EncMode {
	opts := cbor.CoreDetEncOptions()
	opts.Time = cbor.TimeRFC3339Nano
	opts.TimeTag = cbor.EncTagRequired
	em, err := opts.EncMode()
	if err != nil {
		panic(eh.Errorf("envelope: build cbor1 encode mode: %w", err))
	}
	return em
}()

// cborV1DecMode rejects duplicate map keys: a payload that binds a field
// twice has no single reading, and this is the untrusted-bytes path.
// Unknown fields stay ignored so a peer on a newer build can still be
// decoded by an older one.
var cborV1DecMode = func() cbor.DecMode {
	opts := cbor.DecOptions{DupMapKey: cbor.DupMapKeyEnforcedAPF}
	dm, err := opts.DecMode()
	if err != nil {
		panic(eh.Errorf("envelope: build cbor1 decode mode: %w", err))
	}
	return dm
}()

func (CBORV1) Name() (n string) {
	n = CBORV1Name
	return
}

func (CBORV1) Encode(env EnvelopeV1) (payload []byte, err error) {
	if env.Patch == nil {
		err = eh.Errorf("cannot encode an envelope with a nil patch")
		return
	}
	env.Timestamp = env.Timestamp.UTC() // env is a copy; the caller's value is untouched
	payload, err = cborV1EncMode.Marshal(env)
	if err != nil {
		err = eh.Errorf("marshal: %w", err)
	}
	return
}

func (CBORV1) Decode(payload []byte) (env EnvelopeV1, err error) {
	if err = cborV1DecMode.Unmarshal(payload, &env); err != nil {
		err = eh.Errorf("unmarshal: %w", err)
		return
	}
	if env.Patch == nil {
		err = eh.Errorf("missing patch")
		return
	}
	return
}
