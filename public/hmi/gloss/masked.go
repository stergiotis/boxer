package gloss

// MediaTypeMasked masks a value: six bullets, whatever the value's length —
// a mask that grew with its input would leak the one thing it hides. It is
// the aspect vocabulary's `sem:secret` made visible (ADR-0182: "consumers
// must mask by default"), and its affinity says so.
const MediaTypeMasked = "gloss/masked"

// MaskedFace is the fixed inline face of a masked value.
const MaskedFace = "••••••"

func maskedGloss() GlossI {
	return &simpleGloss{
		mediaType:  MediaTypeMasked,
		doc:        "masked; the same six bullets for every value",
		affinities: []string{`\bsem:secret\b`},
		accepts:    AllValueKinds,
		inline:     func(CellI) Inline { return Inline{Text: MaskedFace} },
	}
}
