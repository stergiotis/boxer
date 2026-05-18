package good

type ReaderI interface {
	Read(p []byte) (n int, err error)
}

type writerI interface {
	Write(p []byte) (n int, err error)
}

type ComparableI[T comparable] interface {
	Equal(other T) (ok bool)
}

type Point struct {
	X, Y int
}

type alias = ReaderI

func use(_ interface{ Close() }) {}
