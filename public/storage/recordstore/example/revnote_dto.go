package example

// RevNote is the one component of the Revision-role fixture store.
type RevNote struct {
	_    struct{} `kind:"revNote"`
	ID   string   `lw:",id"`
	Text string   `lw:"text,note,lowCardVerbatim"`
}
