package fixture

// Foo describes Foo.
type Foo struct{}

// Bar references [Foo] which is in the same package.
func Bar() *Foo { return nil }

// Baz references [Bar] (function) and [Foo] (type).
func Baz() {}
