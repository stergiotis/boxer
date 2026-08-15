package gloss

// MediaTypeURL shows a URL as a link: the inline face is the URL's first
// line in the accent tone — the caption a host puts on the hyperlink it
// renders in its grids and in Detail — and the value itself is what the link
// opens. Its affinity is the aspect vocabulary's `sem:url`.
const MediaTypeURL = "gloss/url"

func urlGloss() GlossI {
	return &simpleGloss{
		mediaType:  MediaTypeURL,
		doc:        "a URL — accent-toned inline, a hyperlink in Detail",
		affinities: []string{`\bsem:url\b`},
		accepts:    []ValueKindE{ValueKindText},
		inline:     func(cell CellI) Inline { return Inline{Text: FirstLine(rawOrText(cell)), Tone: ToneAccent} },
	}
}
