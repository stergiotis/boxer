package good

type Foo int

type BarS struct {
	X int
	Y int
}

type ReaderI interface {
	Read(p []byte) (n int, err error)
}

type GenericBox[T any] struct {
	V T
}
