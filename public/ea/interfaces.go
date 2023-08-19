package ea

import "io"

type ByteBlockWriter interface {
	io.ByteWriter
	io.Writer
}

type ByteReadReader interface {
	io.Reader
	io.ByteReader
}

type ByteBlockDiscardReader interface {
	ByteReadReader
	Discard(n int) (int, error)
}
