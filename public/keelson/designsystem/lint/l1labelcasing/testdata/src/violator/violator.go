package violator

import "c"

func raw() {
	c.Label("save changes").Send()        // want `L1: label .* lowercase`
	c.Label("open file").Send()           // want `L1: label .* lowercase`
	c.Label("hello").Send()               // want `L1: label .* lowercase`
}

// First Unicode letter is lowercase even though leading char is not a letter
// — but only the FIRST LETTER counts, and non-letters are skipped over.
func skipLeadingNonLetters() {
	c.Label("  trim me").Send()           // want `L1: label .* lowercase`
	c.Label("\t indented").Send()         // want `L1: label .* lowercase`
}

func passes() {
	c.Label("Save Changes").Send()        // Title Case — passes
	c.Label("Open file").Send()           // Sentence case — passes
	c.Label("OK").Send()                  // all-caps — passes (v1 only flags lowercase-first)
	c.Label("404 — not found").Send()     // leading digit — passes (status fragment)
	c.Label("(loading...)").Send()        // leading paren — passes
	c.Label("…").Send()                   // no letters at all — passes
	c.Label("").Send()                    // empty — passes
	c.Label("3.14").Send()                // pure numeric — passes
}
