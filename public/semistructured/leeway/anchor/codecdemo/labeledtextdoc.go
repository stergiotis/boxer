package codecdemo

// LabeledText is the tuple element of LabeledTextDoc's dynamic-membership
// mapping onto anchor's mixed-shape `text` section (ADR-0103): every
// element emits ONE `text` attribute — BeginAttribute(Text) + one
// AddToCoContainersP(WordLength[k], WordBag[k]) per k — carrying its own
// verbatim membership from Label. The `@membership` marker designates the
// per-attribute membership field (string or []byte; the channel flag is
// mandatory and must be a verbatim channel); the remaining fields spell
// `<section>:<column>` like any mixed-shape DTO. WordLength and WordBag
// must have equal length per element (co-containers advance in lockstep);
// both may be empty — an element always emits, its presence in the slice
// is the signal.
type LabeledText struct {
	Label      string   `lw:"@membership,verbatim"`
	Text       string   `lw:"text:text"`
	WordLength []uint32 `lw:"text:wordLength"`
	WordBag    []string `lw:"text:wordBag"`
}

// LabeledTextDoc exercises the dynamic-membership tuple path (ADR-0103):
// a slice-of-struct field maps MANY attributes into the ONE
// multi-sub-column `text` section, each attribute with its own
// membership — the shape the static grammar rejects ("multi-sub-column
// section with multiple memberships not supported"). Elements land on
// the wire in slice order; zero elements emit zero attributes; reading
// back yields one element per attribute in wire order.
//
// labeledtextdoc.out.go is regenerated from labeledtextdoc.go by:
//
//	./boxer.sh keelsoncodec --target=anchor \
//	    public/semistructured/leeway/anchor/codecdemo/labeledtextdoc.go
type LabeledTextDoc struct {
	_ struct{} `kind:"labeledTextDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	// Texts maps one attribute per element into the `text` section, each
	// with its own membership (dynamic, per element).
	Texts []LabeledText `lw:"text"`
}
