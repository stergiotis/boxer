package envelope

import (
	"encoding/binary"
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// CodecI is a pure wire serialization for EnvelopeV1. Implementations do
// ser/de ONLY — semantic validation (hash, dependency declarations,
// placeholder rejection) is owned by [Validate] and applied uniformly on
// the Registry decode path, so a codec author cannot forget it.
//
// Patch identity is computed over the canonical internal form
// ([patch.Patch.ComputeHash]) and is therefore independent of the wire
// codec: re-encoding an envelope with a different codec preserves
// identity, and mixed-codec fleets stay content-addressed.
//
// Implementations must be deterministic (equal envelope ⇒ equal bytes)
// and safe for concurrent use. Conformance: envelope/codectest.
type CodecI interface {
	// Name identifies the codec inside the wire frame, e.g. "json1".
	// Allowed: 1–32 bytes, no NUL. Names are forever — they are written
	// into persisted envelopes.
	Name() string
	Encode(env EnvelopeV1) (payload []byte, err error)
	Decode(payload []byte) (env EnvelopeV1, err error)
}

// Sentinel errors for the codec/frame layer.
var (
	// ErrUnknownCodec: the frame names a codec this registry lacks.
	ErrUnknownCodec = errors.New("unknown envelope codec")
	// ErrBadFrame: bytes are not a valid PXE1 frame.
	ErrBadFrame = errors.New("malformed envelope frame")
	// ErrCodecName: a codec violates the name constraints.
	ErrCodecName = errors.New("invalid codec name")
)

// frameMagic prefixes every framed envelope: the wire form is
// self-describing so heterogeneous peers can exchange envelopes as long
// as their registries know the named codec.
//
//	"PXE1" | uvarint(len(name)) | name | payload
const frameMagic = "PXE1"

const maxCodecNameLen = 32

// Frame wraps a codec payload in the self-describing header.
func Frame(codecName string, payload []byte) (framed []byte, err error) {
	if err = checkCodecName(codecName); err != nil {
		return
	}
	framed = make([]byte, 0, len(frameMagic)+1+len(codecName)+len(payload))
	framed = append(framed, frameMagic...)
	framed = binary.AppendUvarint(framed, uint64(len(codecName)))
	framed = append(framed, codecName...)
	framed = append(framed, payload...)
	return
}

// Unframe splits a framed envelope into codec name and payload. The
// payload aliases the input.
func Unframe(framed []byte) (codecName string, payload []byte, err error) {
	if len(framed) < len(frameMagic) || string(framed[:len(frameMagic)]) != frameMagic {
		err = eh.Errorf("missing %q magic: %w", frameMagic, ErrBadFrame)
		return
	}
	rest := framed[len(frameMagic):]
	n, used := binary.Uvarint(rest)
	if used <= 0 || n == 0 || n > maxCodecNameLen || uint64(len(rest)-used) < n {
		err = eh.Errorf("bad codec-name length: %w", ErrBadFrame)
		return
	}
	codecName = string(rest[used : used+int(n)])
	if err = checkCodecName(codecName); err != nil {
		return
	}
	payload = rest[used+int(n):]
	return
}

func checkCodecName(name string) (err error) {
	if len(name) == 0 || len(name) > maxCodecNameLen {
		err = eh.Errorf("name %q length %d (want 1..%d): %w", name, len(name), maxCodecNameLen, ErrCodecName)
		return
	}
	for i := 0; i < len(name); i++ {
		if name[i] == 0 {
			err = eh.Errorf("name contains NUL: %w", ErrCodecName)
			return
		}
	}
	return
}

// Registry maps codec names to implementations. The zero value is
// unusable; construct with NewRegistry. Registries are immutable after
// construction and safe for concurrent use.
type Registry struct {
	codecs map[string]CodecI
}

// NewRegistry builds a registry over the given codecs.
func NewRegistry(codecs ...CodecI) (reg *Registry, err error) {
	r := &Registry{codecs: make(map[string]CodecI, len(codecs))}
	for _, c := range codecs {
		name := c.Name()
		if err = checkCodecName(name); err != nil {
			return
		}
		if _, dup := r.codecs[name]; dup {
			err = eh.Errorf("duplicate codec %q: %w", name, ErrCodecName)
			return
		}
		r.codecs[name] = c
	}
	reg = r
	return
}

// Lookup returns the named codec.
func (inst *Registry) Lookup(name string) (c CodecI, err error) {
	c, ok := inst.codecs[name]
	if !ok {
		err = eh.Errorf("codec %q: %w", name, ErrUnknownCodec)
	}
	return
}

// Encode validates the envelope, serializes it with the named codec, and
// frames the result. Validation on the write path keeps a repo from ever
// persisting or shipping an envelope its peers must reject.
func (inst *Registry) Encode(codecName string, env EnvelopeV1) (framed []byte, err error) {
	c, err := inst.Lookup(codecName)
	if err != nil {
		return
	}
	if err = Validate(env); err != nil {
		return
	}
	payload, err := c.Encode(env)
	if err != nil {
		return
	}
	framed, err = Frame(codecName, payload)
	return
}

// Decode unframes, dispatches to the named codec, and validates. This is
// the only sanctioned read path for untrusted envelope bytes.
func (inst *Registry) Decode(framed []byte) (env EnvelopeV1, codecName string, err error) {
	codecName, payload, err := Unframe(framed)
	if err != nil {
		return
	}
	c, err := inst.Lookup(codecName)
	if err != nil {
		return
	}
	env, err = c.Decode(payload)
	if err != nil {
		return
	}
	err = Validate(env)
	return
}
