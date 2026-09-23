package stevedore

import (
	"encoding/binary"
	"encoding/json/v2"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorevocab"
	"github.com/stergiotis/boxer/public/streaming/stevedore/wire"
)

// Request is what one framed message hands a handler (ADR-0252 §SD2): a
// header the pipeline composed, and the body.
//
// On the wire a request is the framework's part archive of two parts — the
// header as JSON, then the body — or of one part, the body alone, for a
// pipeline that has nothing to say about it. The header's fields are the ones
// below; unknown fields are ignored so a pipeline can carry its own.
type Request struct {
	// Origin names where the request came from — an input's topic, partition
	// and offset, a path, a message id — in whatever spelling the pipeline
	// chooses, and is what the file reference hashes. Empty means the body
	// is its own origin.
	Origin string `json:"origin,omitzero"`
	// Hint is what the pipeline knows about the body's format, for the
	// handler; "" when it knows nothing.
	Hint string `json:"hint,omitzero"`
	// Attributes carries whatever else the pipeline wants the handler to
	// see, by name.
	Attributes map[string]string `json:"attributes,omitzero"`
	// Split says the body is one part of a larger one the pipeline split
	// into messages before this one (ADR-0252 §SD4); Part, Parts and Last
	// then give the part's index from zero, the count once known, and the
	// last marker. A host copies all four onto every item.
	Split bool   `json:"split,omitzero"`
	Part  uint32 `json:"part,omitzero"`
	Parts uint32 `json:"parts,omitzero"`
	Last  bool   `json:"last,omitzero"`
	// Body is the bytes to process. It aliases the frame it arrived in.
	Body []byte `json:"-"`
}

// ErrRequestShape is wrapped when a frame is not a one- or two-part archive.
var ErrRequestShape = eh.Errorf("a request is a part archive of a header and a body, or of a body alone")

// DecodeRequest reads a framed request. Every failure is permanent: a
// request that does not parse will not parse next time either.
func DecodeRequest(frame []byte) (req Request, err error) {
	parts, err := wire.DeserializeParts(frame)
	if err != nil {
		return req, Permanent(eh.Errorf("request archive: %w", err))
	}
	switch len(parts) {
	case 1:
		req.Body = parts[0]
	case 2:
		if len(parts[0]) > 0 {
			err = json.Unmarshal(parts[0], &req)
			if err != nil {
				return Request{}, Permanent(eh.Errorf("request header: %w", err))
			}
		}
		req.Body = parts[1]
	default:
		return req, Permanent(eb.Build().Int("parts", len(parts)).Errorf("request: %w", ErrRequestShape))
	}
	return
}

// EncodeRequest frames a request the way DecodeRequest reads it: two parts
// when the header says anything, one when it does not. A pipeline composes
// the same shape with its archive processor; this is for the other side of a
// test, and for a pipeline authored in Go.
func EncodeRequest(req Request) (frame []byte, err error) {
	header := req
	header.Body = nil
	if header.Origin == "" && header.Hint == "" && len(header.Attributes) == 0 &&
		!header.Split && header.Part == 0 && header.Parts == 0 && !header.Last {
		return wire.SerializeParts([][]byte{req.Body}), nil
	}
	h, err := json.Marshal(header)
	if err != nil {
		return nil, eh.Errorf("request header: %w", err)
	}
	return wire.SerializeParts([][]byte{h, req.Body}), nil
}

// ReferenceOf derives the file reference of a request (ADR-0252 §SD4): a
// tagged id under the stevedore tag whose body is a BLAKE3 prefix of the
// origin, or of the body when the request names no origin. The same request
// yields the same reference on every redelivery; content is never identity,
// because a body may be split before it is whole anywhere.
func ReferenceOf(req Request) identifier.TaggedId {
	var sum [32]byte
	if req.Origin != "" {
		sum = blake3.Sum256([]byte(req.Origin))
	} else {
		sum = blake3.Sum256(req.Body)
	}
	return referenceFromDigest(sum)
}

func referenceFromDigest(sum [32]byte) identifier.TaggedId {
	tag := stevedorevocab.TagValueClaim.Tag()
	body := identifier.UntaggedId(binary.BigEndian.Uint64(sum[:8])) & tag.GetMaxPossibleIdIncl()
	return tag.ComposeId(body)
}
