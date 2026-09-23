// Package stevedoreitem is the leeway-coded wire form of the stevedore item
// envelope (ADR-0252 §SD2): what one message of a fan-out carries beside the
// application's payload. A processor host emits one per item, a lander decodes
// one per message; neither reads the payload, which is the application's own
// kind or bytes.
//
// The codec is generated over
// [github.com/stergiotis/boxer/public/streaming/stevedore/stevedorevocab];
// the golden test beside this file regenerates it.
package stevedoreitem

import "time"

// Item is the flat wire form of one envelope.
type Item struct {
	_ struct{} `kind:"stevedoreItem"`

	// FactId is the per-row event id; zero on the wire.
	FactId uint64 `lw:",id"`
	// NaturalKey stays nil on the wire; a lander derives its own keys from
	// Ref and Ordinal (ADR-0252 §SD5).
	NaturalKey []byte `lw:",naturalKey"`
	// At is when the host produced the item.
	At time.Time `lw:",ts"`

	Kind string `lw:"stevedoreKindItem,symbol"`

	// Ref is the file reference: a tagged id under the stevedore tag whose
	// body hashes the request's origin, so a redelivery repeats it.
	Ref uint64 `lw:"stevedoreRef,u64Array"`
	// Origin is the request's origin string, verbatim.
	Origin string `lw:"stevedoreOrigin,stringArray"`
	// Ordinal is the item's position among its request's items, from zero.
	Ordinal uint64 `lw:"stevedoreOrdinal,u64Array"`
	// Line and Offset locate the item in its body when the handler knows;
	// zero otherwise.
	Line   uint64 `lw:"stevedoreLine,u64Array"`
	Offset uint64 `lw:"stevedoreOffset,u64Array"`
	// Split marks one part of a body the pipeline split into messages
	// (ADR-0252 §SD4); Part, Parts and Last then give the part's index, the
	// count once known, and the last marker. All zero for an unsplit body.
	Split bool   `lw:"stevedoreSplit,bool"`
	Part  uint32 `lw:"stevedorePart,u32Array"`
	Parts uint32 `lw:"stevedoreParts,u32Array"`
	Last  bool   `lw:"stevedoreLast,bool"`
	// PayloadKind is the vocabulary kind name Payload's bytes claim, or ""
	// for bytes that are not a kind.
	PayloadKind string `lw:"stevedorePayloadKind,symbol"`
	// Payload is the application's payload, verbatim.
	Payload []byte `lw:"stevedorePayload,blobArray"`
}
