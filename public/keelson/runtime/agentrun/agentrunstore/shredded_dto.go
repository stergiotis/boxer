package agentrunstore

// Shredded is a vault row's shredded attributes (ADR-0325 §SD1): a tool
// call's arguments and a reply's calls, exploded by jsonshred into one
// tuple element per leaf, on the facts section of its value, with the
// JSON Pointer as the mixed-channel membership's name and the array
// indices as its parameters (ADR-0326). semi.go converts jsonshred's
// attributes to and from it.
type Shredded struct {
	_ struct{} `kind:"agentrunShredded"`

	ID uint64 `lw:",id"`

	Strings      []ShreddedString `lw:"stringArray"`
	Symbols      []ShreddedSymbol `lw:"symbol"`
	I64s         []ShreddedI64    `lw:"i64Array"`
	F64s         []ShreddedF64    `lw:"f64Array"`
	Bools        []ShreddedBool   `lw:"bool"`
	Nulls        []ShreddedLeaf   `lw:"null"`
	EmptyObjects []ShreddedLeaf   `lw:"emptyObject"`
	EmptyArrays  []ShreddedLeaf   `lw:"emptyArray"`
}

// ShreddedString is a string leaf.
type ShreddedString struct {
	Path   string   `lw:"@membership,mixedLowCardVerbatim"`
	Params []uint64 `lw:"@params"`
	Value  string   `lw:"stringArray:value,unit"`
}

// ShreddedSymbol is a string leaf a schema hint marked low-cardinality.
type ShreddedSymbol struct {
	Path   string   `lw:"@membership,mixedLowCardVerbatim"`
	Params []uint64 `lw:"@params"`
	Value  string   `lw:"symbol:value"`
}

// ShreddedI64 is an integer leaf.
type ShreddedI64 struct {
	Path   string   `lw:"@membership,mixedLowCardVerbatim"`
	Params []uint64 `lw:"@params"`
	Value  int64    `lw:"i64Array:value,unit"`
}

// ShreddedF64 is a number leaf that is not an integer.
type ShreddedF64 struct {
	Path   string   `lw:"@membership,mixedLowCardVerbatim"`
	Params []uint64 `lw:"@params"`
	Value  float64  `lw:"f64Array:value,unit"`
}

// ShreddedBool is a boolean leaf.
type ShreddedBool struct {
	Path   string   `lw:"@membership,mixedLowCardVerbatim"`
	Params []uint64 `lw:"@params"`
	Value  bool     `lw:"bool:value"`
}

// ShreddedLeaf is a null, {} or [] leaf, which carries no value.
type ShreddedLeaf struct {
	Path   string   `lw:"@membership,mixedLowCardVerbatim"`
	Params []uint64 `lw:"@params"`
}
