package bad

import (
	. "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// A direct call.
func copyIt(text string) {
	c.CopyTextToClipboard(text) // want a CS014 finding here
}

type input struct {
	Copy func(string)
}

// The opcode handed to a widget as a function value, which copies when the
// widget calls it.
func viaField() input {
	return input{Copy: c.CopyTextToClipboard} // want a CS014 finding here
}

// The opcode bound to a variable.
func viaVariable(text string) {
	f := c.CopyTextToClipboard // want a CS014 finding here
	f(text)
}

// The opcode through a dot import.
func viaDotImport(text string) {
	CopyTextToClipboard(text) // want a CS014 finding here
}
