package identcontainer

import (
	"io"

	"iter"

	"github.com/stergiotis/boxer/public/identity/identifier"
)

type commonInterface interface {
	Length() uint64
	Optimize()
	IsEmpty() bool
	Clear()
	Iterate() iter.Seq[identifier.TaggedId]
	// FIXME iterator
}

type Roaring64Bytes []byte

type Roaring32Bytes []byte

type Roaring64Serializable interface {
	Optimize()
	WriteRoaring64(dest io.Writer) (n int64, err error)
	Serialize() (r Roaring64Bytes, err error)
}

type Roaring32Serializable interface {
	Optimize()
	WriteRoaring32(dest io.Writer) (n int64, err error)
	Serialize() (r Roaring32Bytes, err error)
}
