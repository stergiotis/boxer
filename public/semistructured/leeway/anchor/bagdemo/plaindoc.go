package bagdemo

// PlainDoc is a flat component on the same sections, under the reference
// channel: its attributes are neither BagDoc's nor NotedDoc's.
type PlainDoc struct {
	_ struct{} `kind:"plainDoc"`

	ID uint64 `lw:",id"`

	Title string   `lw:"plainTitle,symbol"`
	Tags  []string `lw:"plainTag,stringArray"`
}
