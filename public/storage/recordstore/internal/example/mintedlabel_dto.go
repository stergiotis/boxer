package example

// MintedLabel is the one component of the KeyStream fixture store.
type MintedLabel struct {
	_    struct{} `kind:"mintedLabel"`
	ID   uint64   `lw:",id"`
	Text string   `lw:"text,label,lowCardVerbatim"`
}
