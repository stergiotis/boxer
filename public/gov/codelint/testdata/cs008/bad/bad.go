package bad

import "io"

type IntAlias = int     // want CS008 here
type StringAlias = string // want CS008 here

type ReaderAlias = io.Reader // want CS008 here

type Suppressed = int //boxer:lint disable=CS008 reason="testdata coverage of suppression"

type Wrapper[T any] struct {
	V T
}

type WrapperAlias[T any] = Wrapper[T] // want CS008 here
