package good

import (
	"iter"

	"github.com/stergiotis/boxer/public/gov/codelint/testdata/cs010/good/ifacedef"
)

// SourceI fixes the name of its sole iterator; an implementer has no
// choice, so CS010 does not apply to it.
type SourceI interface {
	Rows() iter.Seq[int]
}

var _ SourceI = (*Table)(nil)

type Table struct{}

func (inst *Table) Rows() iter.Seq[int] {
	return func(yield func(int) bool) {}
}

// Imported interface, pointer receiver: the same exemption reaches across
// packages.
var _ ifacedef.BatchesI = (*Sink)(nil)

type Sink struct{}

func (inst *Sink) Batches() iter.Seq2[int, error] {
	return func(yield func(int, error) bool) {}
}
