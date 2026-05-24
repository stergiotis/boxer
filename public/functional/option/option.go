//go:build llm_generated_opus47

// Package option provides a typed Some/None carrier for fields that
// may be present or absent. Pure value semantics — no pointer, no nil.
// Read Val only when Has is true.
//
// Idiomatic uses include marshalling fields that bind to a ZeroToOne
// cardinality, configuration values that may be unset, and any API
// where the difference between "absent" and "zero value" matters.
package option

// Option is the typed Some/None carrier.
type Option[T any] struct {
	Val T
	Has bool
}

// Some wraps a value in a present Option[T].
func Some[T any](v T) (opt Option[T]) {
	opt = Option[T]{Val: v, Has: true}
	return
}

// None returns an absent Option[T] — Has is false and Val carries the
// zero value of T.
func None[T any]() (opt Option[T]) {
	return
}
