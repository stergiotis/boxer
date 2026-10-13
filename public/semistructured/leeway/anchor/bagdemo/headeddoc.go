package bagdemo

// HeadedSection is one attribute under the fixed membership headedSection
// on the mixed ref channel, its heading riding as text parameters
// (ADR-0326 §SD1): gov/capmapfacts' shape for a competence's sections.
type HeadedSection struct {
	Heading string `lw:"@params"`
	Text    string `lw:"value,unit"`
}

// HeadedEvent carries its parameters as bytes.
type HeadedEvent struct {
	Phase []byte `lw:"@params"`
	By    string
}

// HeadedMark is a dynamic tuple element on the mixed ref channel without a
// value: an id and a text parameter.
type HeadedMark struct {
	Id    uint64 `lw:"@membership,mixedLowCardRef"`
	Label string `lw:"@params"`
}

// HeadedDoc carries text parameters on all three sections, beside the
// other kinds' attributes.
type HeadedDoc struct {
	_ struct{} `kind:"headedDoc"`

	ID       uint64          `lw:",id"`
	Sections []HeadedSection `lw:"headedSection,stringArray,mixedLowCardRef"`
	Events   []HeadedEvent   `lw:"headedBy,symbol,mixedLowCardRef"`
	Marks    []HeadedMark    `lw:"null"`
}
