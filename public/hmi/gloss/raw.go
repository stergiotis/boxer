package gloss

// MediaTypeRaw is the identity gloss: the plain rendering, no block face. It
// exists so a rule can override an affinity — `-- play: gloss gloss/raw
// sem:secret` — without the directive grammar needing a reserved word.
const MediaTypeRaw = "gloss/raw"

func rawGloss() GlossI {
	return &simpleGloss{
		mediaType: MediaTypeRaw,
		doc:       "the plain rendering; overrides an affinity when ruled",
		accepts:   AllValueKinds,
		inline:    func(cell CellI) Inline { return Inline{Text: cell.Text()} },
	}
}

// presentationFamily lists the `gloss/*` members in catalog order — the
// order affinities are tried in and the reject message lists.
func presentationFamily() []GlossI {
	return []GlossI{
		temperatureGloss{},
		lengthGloss{},
		epochGloss{},
		durationGloss{},
		bytesGloss(),
		taggedIdGloss(),
		luhnGloss(),
		maskedGloss(),
		urlGloss(),
		rawGloss(),
	}
}
