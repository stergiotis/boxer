package ea

import (
	"github.com/stergiotis/boxer/public/ea"
	"io"
)

type MessageReader interface {
	ea.ByteBlockDiscardReader
	// BytesRead return how many bytes have been consumed from the underlying reader
	BytesRead() int
	// BytesPeeked return how many bytes have been peeked from the underlying reader (in-message)
	BytesPeeked() int
	Discard(nBytes int) (n int, err error)
	MessageAccepted() (err error)
	MessageRejected(reason error) (err error)
	DetectedBitErrors() int
}

type MessageWriter interface {
	io.ByteWriter
	io.Writer
	BeginMessage() (n int, err error)
	EndMessage() (paddingBitsBeforeEncoding int, err error)
}
