package ea

import "io"

type ByteBlockWriterI interface {
	io.ByteWriter
	io.Writer
}

type ByteReadReaderI interface {
	io.Reader
	io.ByteReader
}

type ByteBlockDiscardReaderI interface {
	ByteReadReaderI
	Discard(n int) (int, error)
}
