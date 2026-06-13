package persist

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/persistreply"
)

// Subject grammar for runtime.persist per ADR-0026 §SD3:
//
//	runtime.persist.{alias}.{key}.{op}
//
// Cap pattern for an app to access its own state:
//
//	runtime.persist.{ownAlias}.>     (Direction: Pub)
const (
	SubjectPrefix = "runtime.persist."
	OpGet         = "get"
	OpSet         = "set"
	OpDelete      = "delete"
)

// SubjectFor builds the canonical request subject for (alias, key, op).
func SubjectFor(alias, key, op string) (subject string) {
	subject = SubjectPrefix + alias + "." + key + "." + op
	return
}

// PersistReply is the response envelope returned on every operation.
// Found and Value populate on a successful Get; Error populates on any
// failure. Set and Delete return an empty reply on success.
type PersistReply struct {
	Found bool   `json:"found,omitempty"`
	Value []byte `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

// MarshalReply encodes a reply for transmission as Msg.Payload. The
// codec wire form (persistreply.PersistReply) lives in
// keelson/runtime/codec/persistreply; this helper handles the
// boundary translation so callers keep using the broker's native
// shape. The Error field maps to the cross-DTO `reason` vocabulary
// column.
func MarshalReply(r PersistReply) (b []byte, err error) {
	wire := persistreply.PersistReply{
		Found:  r.Found,
		Value:  r.Value,
		Reason: r.Error,
	}
	b, err = buscodec.Encode(wire)
	if err != nil {
		err = eh.Errorf("marshal persist reply: %w", err)
	}
	return
}

// UnmarshalReply is the inverse of MarshalReply.
func UnmarshalReply(b []byte) (r PersistReply, err error) {
	var wire persistreply.PersistReply
	wire, err = buscodec.Decode[persistreply.PersistReply](b)
	if err != nil {
		err = eh.Errorf("unmarshal persist reply: %w", err)
		return
	}
	r = PersistReply{
		Found: wire.Found,
		Value: wire.Value,
		Error: wire.Reason,
	}
	return
}
