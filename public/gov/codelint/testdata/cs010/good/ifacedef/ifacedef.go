// Package ifacedef declares an interface whose iterator method name an
// implementer in a sibling package must adopt.
package ifacedef

import "iter"

type BatchesI interface {
	Batches() iter.Seq2[int, error]
}
