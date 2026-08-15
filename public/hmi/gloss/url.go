package gloss

// MediaTypeURL shows a URL as a link: the text in the accent tone inline
// (a table cell click selects the row, so the cell itself is not a link),
// and a hyperlink block face in the host. Its affinity is the aspect
// vocabulary's `sem:url`.
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
