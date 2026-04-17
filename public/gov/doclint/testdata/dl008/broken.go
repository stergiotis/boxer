package fixture

// Quux references [DoesNotExist] which is not in this package.
// It also references [Foo] which is fine, and [AlsoMissing] which is not.
func Quux() {}
