package stevedore

import (
	"context"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedoreitem"
)

// Emit is what a handler hands back per item: the application's payload and
// what the handler knows about where it came from. The host adds the
// reference, the origin, the ordinal and the split fields.
type Emit struct {
	// PayloadKind is the vocabulary kind name Payload's bytes claim, or ""
	// for bytes that are not a kind.
	PayloadKind string
	Payload     []byte
	// Line and Offset locate the item in the body when the handler knows;
	// zero otherwise.
	Line   uint64
	Offset uint64
}

// HandlerI turns one request into its items (ADR-0252 §SD2). It runs inside
// the host's deadline, its panics are recovered, and an error it returns is
// classified (ADR-0252 §SD3): a transient one is retried in place, a
// permanent one is reported; items emitted before an error are discarded,
// so a handler is free to fail late.
type HandlerI interface {
	Handle(ctx context.Context, req Request, emit func(Emit) error) error
}

// HandlerFunc adapts a function to HandlerI.
type HandlerFunc func(ctx context.Context, req Request, emit func(Emit) error) error

// Handle calls the function.
func (inst HandlerFunc) Handle(ctx context.Context, req Request, emit func(Emit) error) error {
	return inst(ctx, req, emit)
}

var _ HandlerI = HandlerFunc(nil)

// Item is one message of a fan-out as both hosts see it (ADR-0252 §SD2): the
// envelope the wire kind carries, with the reference typed.
type Item struct {
	Ref     identifier.TaggedId
	Origin  string
	Ordinal uint64
	Line    uint64
	Offset  uint64
	Split   bool
	Part    uint32
	Parts   uint32
	Last    bool
	// PayloadKind is the vocabulary kind name Payload's bytes claim, or "".
	PayloadKind string
	Payload     []byte
}

// KindItem is the kind label every envelope carries.
const KindItem = "stevedoreItem"

// EncodeItem renders an item as its wire envelope, facts-CBOR of the
// stevedoreItem kind.
func EncodeItem(item Item) (b []byte, err error) {
	// The envelope carries no timestamp: a redelivered request must yield
	// byte-identical items, and a lander stamps rows with its own clock.
	b, err = buscodec.Encode(stevedoreitem.Item{
		Kind:        KindItem,
		Ref:         item.Ref.Value(),
		Origin:      item.Origin,
		Ordinal:     item.Ordinal,
		Line:        item.Line,
		Offset:      item.Offset,
		Split:       item.Split,
		Part:        item.Part,
		Parts:       item.Parts,
		Last:        item.Last,
		PayloadKind: item.PayloadKind,
		Payload:     item.Payload,
	})
	if err != nil {
		err = eh.Errorf("encode item envelope: %w", err)
	}
	return
}

// DecodeItem reads a wire envelope. A failure is permanent: bytes that are
// not an envelope will not become one.
func DecodeItem(b []byte) (item Item, err error) {
	w, err := buscodec.Decode[stevedoreitem.Item](b)
	if err != nil {
		return item, Permanent(eh.Errorf("decode item envelope: %w", err))
	}
	if w.Kind != KindItem {
		return item, Permanentf("bytes decode as another kind, not an item envelope")
	}
	item = Item{
		Ref:         identifier.TaggedId(w.Ref),
		Origin:      w.Origin,
		Ordinal:     w.Ordinal,
		Line:        w.Line,
		Offset:      w.Offset,
		Split:       w.Split,
		Part:        w.Part,
		Parts:       w.Parts,
		Last:        w.Last,
		PayloadKind: w.PayloadKind,
		Payload:     w.Payload,
	}
	return
}

// SinkI lands items as rows (ADR-0252 §SD5). Its rows must be keyed by the
// item's Ref and Ordinal, so a redelivered item is a rewrite and not a
// duplicate; a lander commits its offset only after Flush returns nil. An
// error from either is classified: transient retries, permanent dead-letters
// the item and moves on.
type SinkI interface {
	Land(ctx context.Context, item Item) error
	Flush(ctx context.Context) error
}
