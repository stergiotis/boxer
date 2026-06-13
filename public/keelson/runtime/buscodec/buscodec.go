// Package buscodec is the runtime's canonical message-bus codec. Every
// thestack broker MUST encode bus payloads through this package and MUST
// NOT import encoding/json or call cbor.Marshal/Unmarshal directly. The
// default codec is CBOR (fxamacker/cbor with canonical encoding); a
// CodecI seam allows swapping for replay or debug builds.
//
// Field tags: the default CBOR codec (fxamacker/cbor) honours `cbor:` tags
// and otherwise uses the Go field name — it does NOT read `json:` tags.
// Encode/Decode are symmetric through a single codec, so this is internally
// consistent; the `json:` tags several DTOs carry only take effect under the
// opt-in jsonCodec (NewJSON, for human-readable replay) and do not change the
// CBOR wire. Do not assume a `json:`-named key appears on the CBOR wire.
//
// Versioning: this package does NOT impose envelope versioning on the
// wire. Per-message versioning is a payload concern — brokers that need
// it (e.g. chlocalbroker) carry a uint8 V field on the struct.
package buscodec

import (
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// CodecI is the wire-format contract. Implementations must be goroutine-
// safe. Encode may pool buffers internally but the returned slice is
// caller-owned: callers may retain it past the call.
type CodecI interface {
	// Name returns a short stable identifier (e.g. "cbor", "json"). Used
	// for diagnostics and content-type emission.
	Name() (n string)
	// ContentType returns the MIME type for the wire format.
	ContentType() (ct string)
	// Encode serialises v into a freshly-allocated slice.
	Encode(v any) (b []byte, err error)
	// Decode populates v from b. v MUST be a non-nil pointer.
	Decode(b []byte, v any) (err error)
}

// PublishFunc matches inprocbus.Client.Publish so brokers can pass their
// client's Publish into Reply without an adapter.
type PublishFunc func(subject string, payload []byte) (err error)

type codecHolder struct {
	codec CodecI
}

var defaultCodec atomic.Pointer[codecHolder]

func init() {
	defaultCodec.Store(&codecHolder{codec: NewCBOR()})
}

// Default returns the process-wide default codec.
func Default() (c CodecI) {
	c = defaultCodec.Load().codec
	return
}

// SetDefault swaps the process-wide codec. Intended for init-time use by
// tests, debug builds, or capture-replay tools. Reads after the call
// observe the new codec; concurrent SetDefault calls race with each
// other but not with Default — Default is always safe.
func SetDefault(c CodecI) {
	if c == nil {
		return
	}
	defaultCodec.Store(&codecHolder{codec: c})
}

// --- Per-type codec registry (ADR-0042 M12). ---
//
// Fact-row payload types can register a CodecI specialised for their
// shape — typically a sparse-CBOR codec that piggybacks
// on the runtime.facts dml builder with precomputed active-* hints
// (see ADR-0042's M10 Updates entry). Unregistered types continue to
// hit Default() — CBOR by default — so the migration is opt-in per
// payload type.

var perTypeCodecs sync.Map // map[reflect.Type]CodecI

// Register installs a per-type codec for T. Subsequent Encode[T] /
// Decode[T] / Reply[T] calls route through `codec` instead of
// Default(). Passing nil unregisters T (so the next call falls back
// to Default()).
//
// Goroutine-safe; intended for package-init or test setup.
// Re-registering overwrites the previous codec without warning.
func Register[T any](codec CodecI) {
	t := reflect.TypeFor[T]()
	if codec == nil {
		perTypeCodecs.Delete(t)
		return
	}
	perTypeCodecs.Store(t, codec)
}

// Lookup returns the codec routed to T — the per-type registration if
// one exists, else Default(). Exposed so tests + diagnostics can
// observe the dispatch without going through Encode/Decode.
func Lookup[T any]() (c CodecI) {
	if v, ok := perTypeCodecs.Load(reflect.TypeFor[T]()); ok {
		c = v.(CodecI)
		return
	}
	c = Default()
	return
}

// Encode is the generic call-site helper. Routes through the per-type
// registry if T is registered, else through Default(). Centralises the
// eh.Errorf wrap so brokers don't repeat it per payload type.
func Encode[T any](v T) (b []byte, err error) {
	b, err = Lookup[T]().Encode(v)
	if err != nil {
		err = eh.Errorf("buscodec: encode %T: %w", v, err)
		return
	}
	return
}

// Decode is the generic inverse of Encode.
func Decode[T any](b []byte) (v T, err error) {
	err = Lookup[T]().Decode(b, &v)
	if err != nil {
		err = eh.Errorf("buscodec: decode %T: %w", v, err)
		return
	}
	return
}

// Reply folds the broker-side "encode v and publish on subject" pattern
// — replaces the per-broker replyJSON helpers.
func Reply[T any](pub PublishFunc, subject string, v T) (err error) {
	var b []byte
	b, err = Encode(v)
	if err != nil {
		return
	}
	err = pub(subject, b)
	if err != nil {
		err = eh.Errorf("buscodec: publish %T on %s: %w", v, subject, err)
		return
	}
	return
}
