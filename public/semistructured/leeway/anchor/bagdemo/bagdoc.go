package bagdemo

//go:generate sh -c "go run -tags=\"$(cat ../../../../../tags)\" github.com/stergiotis/boxer/public/app keelsoncodec --target=anchor bagdoc.go noteddoc.go plaindoc.go"

// BagString is one attribute of the string-array section: a name and its
// parameters on the mixed verbatim channel, and one value (ADR-0326
// §SD1, §SD2) — the journal's shredded-attribute shape.
type BagString struct {
	Path   string   `lw:"@membership,mixedLowCardVerbatim"`
	Params []uint64 `lw:"@params"`
	Value  string   `lw:"stringArray:value,unit"`
}

// BagSymbol is the same over the symbol section, whose value is scalar.
type BagSymbol struct {
	Path   string   `lw:"@membership,mixedLowCardVerbatim"`
	Params []uint64 `lw:"@params"`
	Value  string   `lw:"symbol:value"`
}

// BagNull is an attribute of the section without a value column: a name
// and its parameters only (ADR-0326 §SD3).
type BagNull struct {
	Path   string   `lw:"@membership,mixedLowCardVerbatim"`
	Params []uint64 `lw:"@params"`
}

// BagDoc is a bag of named, parametrised attributes over three sections.
type BagDoc struct {
	_ struct{} `kind:"bagDoc"`

	ID uint64 `lw:",id"`

	Strings []BagString `lw:"stringArray"`
	Symbols []BagSymbol `lw:"symbol"`
	Nulls   []BagNull   `lw:"null"`
}
