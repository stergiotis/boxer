package fixture

// ExcludedForms exercises bracket patterns DL008 must NOT flag.
//
// Lowercase identifiers like [foo] are not exported and not checked.
// Single-letter brackets like [T] (generic type parameter context)
// are too ambiguous and skipped.
// Slice / array brackets [3]int and []byte are not identifiers.
// Qualified references like [pkg.Sym] are reserved for v2.
func ExcludedForms() {}
